package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/process"
)

// sessionName returns the zellij session name for this project
func (m *planModel) sessionName() string {
	return fmt.Sprintf("co-%s", m.proj.Config.Project.Name)
}

// spawnPlanSession spawns or resumes a planning session for a specific bead
func (m *planModel) spawnPlanSession(beadID string) tea.Cmd {
	return func() tea.Msg {
		session := m.sessionName()
		tabName := claude.PlanTabName(beadID)
		mainRepoPath := m.proj.MainRepoPath()

		// Check if session already running for this bead
		running, _ := m.proj.DB.IsPlanSessionRunning(m.ctx, beadID)
		if running {
			// Session exists - just switch to it
			if err := m.zj.SwitchToTab(m.ctx, session, tabName); err != nil {
				return planSessionSpawnedMsg{beadID: beadID, err: err}
			}
			return planSessionSpawnedMsg{beadID: beadID, resumed: true}
		}

		// Use the helper to spawn the plan session
		if err := claude.SpawnPlanSession(m.ctx, beadID, m.proj.Config.Project.Name, mainRepoPath, io.Discard); err != nil {
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		return planSessionSpawnedMsg{beadID: beadID, resumed: false}
	}
}

// executeCreateWork creates a work unit with the given branch name.
// This calls internal logic directly instead of shelling out to the CLI.
func (m *planModel) executeCreateWork(beadID string, branchName string, auto bool) tea.Cmd {
	return func() tea.Msg {
		// Expand the bead (handles epics and transitive deps)
		allIssueIDs, err := collectIssueIDsForAutomatedWorkflow(m.ctx, beadID, m.proj.Beads)
		if err != nil {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("failed to expand bead %s: %w", beadID, err)}
		}

		if len(allIssueIDs) == 0 {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("no beads found for %s", beadID)}
		}

		// Create work with branch name (silent to avoid console output in TUI)
		result, err := CreateWorkWithBranch(m.ctx, m.proj, branchName, "main", beadID, WorkCreateOptions{Silent: true, Auto: auto})
		if err != nil {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("failed to create work: %w", err)}
		}

		// Add beads to the work
		if err := addBeadsToWork(m.ctx, m.proj, result.WorkID, allIssueIDs); err != nil {
			// Work was created but beads couldn't be added - don't fail completely
			return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID, err: fmt.Errorf("work created but failed to add beads: %w", err)}
		}

		// Spawn the orchestrator for this work
		if err := claude.SpawnWorkOrchestrator(m.ctx, result.WorkID, m.proj.Config.Project.Name, result.WorktreePath, result.WorkerName, io.Discard); err != nil {
			// Non-fatal: work was created but orchestrator failed to spawn
			return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID, err: fmt.Errorf("work created but orchestrator failed: %w", err)}
		}

		return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID}
	}
}

func (m *planModel) addBeadsToWork(beadIDs []string, workID string) tea.Cmd {
	return func() tea.Msg {
		// Use internal function instead of CLI
		_, err := AddBeadsToWork(m.ctx, m.proj, workID, beadIDs)
		if err != nil {
			beadIDsStr := strings.Join(beadIDs, ", ")
			return beadAddedToWorkMsg{beadID: beadIDsStr, workID: workID, err: fmt.Errorf("failed to add issues to work: %w", err)}
		}

		beadIDsStr := strings.Join(beadIDs, ", ")
		return beadAddedToWorkMsg{beadID: beadIDsStr, workID: workID}
	}
}

// workTilesLoadedMsg indicates work tiles have been loaded
type workTilesLoadedMsg struct {
	works              []*workProgress
	orchestratorHealth map[string]bool // workID -> orchestrator alive
	err                error
}

// loadWorkTiles loads work data for the work tabs bar
func (m *planModel) loadWorkTiles() tea.Cmd {
	return func() tea.Msg {
		works, err := fetchAllWorksPollData(m.ctx, m.proj)
		if err != nil {
			return workTilesLoadedMsg{err: err}
		}

		// Compute orchestrator health for all works (async)
		orchestratorHealth := make(map[string]bool)
		for _, work := range works {
			if work != nil {
				orchestratorHealth[work.work.ID] = checkOrchestratorHealth(m.ctx, work.work.ID)
			}
		}

		return workTilesLoadedMsg{works: works, orchestratorHealth: orchestratorHealth}
	}
}

// Helper functions for work commands

// destroyWork destroys a work by ID
func (m *planModel) destroyWork(workID string) tea.Cmd {
	return func() tea.Msg {
		if err := DestroyWork(m.ctx, m.proj, workID, io.Discard); err != nil {
			return workCommandMsg{action: "Destroy work", workID: workID, err: err}
		}
		return workCommandMsg{action: "Destroy work", workID: workID}
	}
}

// destroyFocusedWork destroys the currently focused work (used by confirmation dialog)
func (m *planModel) destroyFocusedWork() tea.Cmd {
	return m.destroyWork(m.focusedWorkID)
}

// runFocusedWork creates tasks for the currently focused work and ensures orchestrator is running
func (m *planModel) runFocusedWork(autoGroup bool) tea.Cmd {
	workID := m.focusedWorkID
	return func() tea.Msg {
		if autoGroup {
			// Use auto mode - creates estimate task and lets orchestrator handle grouping
			_, err := RunWorkAuto(m.ctx, m.proj, workID, io.Discard)
			if err != nil {
				return workCommandMsg{action: "Run work", workID: workID, err: err}
			}
		} else {
			// Use direct mode - creates one task per bead
			_, err := RunWork(m.ctx, m.proj, workID, false, io.Discard)
			if err != nil {
				return workCommandMsg{action: "Run work", workID: workID, err: err}
			}
		}
		return workCommandMsg{action: "Run work", workID: workID}
	}
}

// createReviewTask creates a review task for the currently focused work
func (m *planModel) createReviewTask() tea.Cmd {
	workID := m.focusedWorkID
	return func() tea.Msg {
		// Get work details
		work, err := m.proj.DB.GetWork(m.ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Create review", workID: workID, err: fmt.Errorf("failed to get work: %w", err)}
		}
		if work == nil {
			return workCommandMsg{action: "Create review", workID: workID, err: fmt.Errorf("work %s not found", workID)}
		}

		// Get existing tasks to generate unique review ID
		tasks, err := m.proj.DB.GetWorkTasks(m.ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Create review", workID: workID, err: fmt.Errorf("failed to get work tasks: %w", err)}
		}

		// Count existing review tasks
		reviewCount := 0
		reviewPrefix := fmt.Sprintf("%s.review", workID)
		for _, task := range tasks {
			if strings.HasPrefix(task.ID, reviewPrefix) {
				reviewCount++
			}
		}

		// Generate unique review task ID
		reviewTaskID := fmt.Sprintf("%s.review-%d", workID, reviewCount+1)

		// Create the review task
		err = m.proj.DB.CreateTask(m.ctx, reviewTaskID, "review", []string{}, 0, workID)
		if err != nil {
			return workCommandMsg{action: "Create review", workID: workID, err: fmt.Errorf("failed to create review task: %w", err)}
		}

		return workCommandMsg{action: "Create review", workID: workID}
	}
}

// createPRTask creates a PR task for the currently focused work
func (m *planModel) createPRTask() tea.Cmd {
	workID := m.focusedWorkID
	return func() tea.Msg {
		// Get work details
		work, err := m.proj.DB.GetWork(m.ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Create PR", workID: workID, err: fmt.Errorf("failed to get work: %w", err)}
		}
		if work == nil {
			return workCommandMsg{action: "Create PR", workID: workID, err: fmt.Errorf("work %s not found", workID)}
		}

		// Check if work is completed
		if work.Status != db.StatusCompleted {
			return workCommandMsg{action: "Create PR", workID: workID, err: fmt.Errorf("work %s is not completed (status: %s)", workID, work.Status)}
		}

		// Check if PR already exists
		if work.PRURL != "" {
			return workCommandMsg{action: "Create PR", workID: workID, err: fmt.Errorf("PR already exists: %s", work.PRURL)}
		}

		// Generate PR task ID
		prTaskID := fmt.Sprintf("%s.pr", workID)

		// Create the PR task
		err = m.proj.DB.CreateTask(m.ctx, prTaskID, "pr", []string{}, 0, workID)
		if err != nil {
			return workCommandMsg{action: "Create PR", workID: workID, err: fmt.Errorf("failed to create PR task: %w", err)}
		}

		return workCommandMsg{action: "Create PR", workID: workID}
	}
}

// openConsole opens a terminal/console tab for the focused work
func (m *planModel) openConsole() tea.Cmd {
	workID := m.focusedWorkID
	return func() tea.Msg {
		// Get work details
		work, err := m.proj.DB.GetWork(m.ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Open console", workID: workID, err: fmt.Errorf("failed to get work: %w", err)}
		}
		if work == nil {
			return workCommandMsg{action: "Open console", workID: workID, err: fmt.Errorf("work %s not found", workID)}
		}

		err = claude.OpenConsole(m.ctx, workID, m.proj.Config.Project.Name, work.WorktreePath, work.Name, m.proj.Config.Hooks.Env, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Open console", workID: workID, err: err}
		}

		return workCommandMsg{action: "Open console", workID: workID}
	}
}

// openClaude opens a Claude Code session tab for the focused work
func (m *planModel) openClaude() tea.Cmd {
	workID := m.focusedWorkID
	return func() tea.Msg {
		// Get work details
		work, err := m.proj.DB.GetWork(m.ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Open Claude", workID: workID, err: fmt.Errorf("failed to get work: %w", err)}
		}
		if work == nil {
			return workCommandMsg{action: "Open Claude", workID: workID, err: fmt.Errorf("work %s not found", workID)}
		}

		err = claude.OpenClaudeSession(m.ctx, workID, m.proj.Config.Project.Name, work.WorktreePath, work.Name, m.proj.Config.Hooks.Env, m.proj.Config, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Open Claude", workID: workID, err: err}
		}

		return workCommandMsg{action: "Open Claude", workID: workID}
	}
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	// Handle negative maxLen values
	if maxLen < 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// checkOrchestratorHealth checks if the orchestrator process is running for a work
func checkOrchestratorHealth(ctx context.Context, workID string) bool {
	// Check if an orchestrator process is running for this specific work
	pattern := "co orchestrate --work " + workID
	running, _ := process.IsProcessRunning(ctx, pattern)
	return running
}

// restartOrchestrator kills and restarts the orchestrator for the focused work
func (m *planModel) restartOrchestrator() tea.Cmd {
	workID := m.focusedWorkID
	return func() tea.Msg {
		// Get work details
		work, err := m.proj.DB.GetWork(m.ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Restart orchestrator", workID: workID, err: fmt.Errorf("failed to get work: %w", err)}
		}
		if work == nil {
			return workCommandMsg{action: "Restart orchestrator", workID: workID, err: fmt.Errorf("work %s not found", workID)}
		}

		// Kill any existing orchestrator process
		pattern := fmt.Sprintf("co orchestrate --work %s", workID)
		if running, _ := process.IsProcessRunning(m.ctx, pattern); running {
			process.KillProcess(m.ctx, pattern)
			time.Sleep(500 * time.Millisecond)
		}

		// Spawn a new orchestrator
		spawned, err := claude.EnsureWorkOrchestrator(
			m.ctx,
			workID,
			m.proj.Config.Project.Name,
			work.WorktreePath,
			work.Name,
			io.Discard,
		)
		if err != nil {
			return workCommandMsg{action: "Restart orchestrator", workID: workID, err: err}
		}

		status := "already running"
		if spawned {
			status = "restarted"
		}
		return workCommandMsg{action: fmt.Sprintf("Orchestrator %s", status), workID: workID}
	}
}

// checkPRFeedback triggers an immediate PR feedback check for the focused work
func (m *planModel) checkPRFeedback() tea.Cmd {
	workID := m.focusedWorkID
	return func() tea.Msg {
		if err := TriggerPRFeedbackCheck(m.ctx, m.proj, workID); err != nil {
			return workCommandMsg{action: "Check PR feedback", workID: workID, err: err}
		}
		return workCommandMsg{action: "PR feedback check triggered", workID: workID}
	}
}
