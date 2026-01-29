package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/control"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/process"
	"github.com/newhook/co/internal/progress"
	"github.com/newhook/co/internal/work"
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

		logging.Debug("spawnPlanSession started", "beadID", beadID, "session", session, "tabName", tabName)

		// Check if session already running for this bead
		running, _ := m.proj.DB.IsPlanSessionRunning(m.ctx, beadID)
		logging.Debug("spawnPlanSession checked if running", "beadID", beadID, "running", running)
		if running {
			// Session exists - just switch to it
			if err := m.zj.Session(session).SwitchToTab(m.ctx, tabName); err != nil {
				return planSessionSpawnedMsg{beadID: beadID, err: err}
			}
			return planSessionSpawnedMsg{beadID: beadID, resumed: true}
		}

		// Initialize zellij session (spawns control plane if new session)
		sessionResult, err := control.InitializeSession(m.ctx, m.proj)
		if err != nil {
			logging.Error("spawnPlanSession InitializeSession failed", "beadID", beadID, "error", err)
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}
		logging.Debug("spawnPlanSession InitializeSession completed",
			"beadID", beadID,
			"sessionCreated", sessionResult != nil && sessionResult.SessionCreated,
			"sessionName", func() string {
				if sessionResult != nil {
					return sessionResult.SessionName
				}
				return ""
			}())

		// Use the helper to spawn the plan session
		if err := claude.SpawnPlanSession(m.ctx, beadID, m.proj.Config.Project.Name, mainRepoPath, io.Discard); err != nil {
			logging.Error("spawnPlanSession SpawnPlanSession failed", "beadID", beadID, "error", err)
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		msg := planSessionSpawnedMsg{beadID: beadID, resumed: false}
		if sessionResult != nil && sessionResult.SessionCreated {
			msg.sessionCreated = true
			msg.sessionName = sessionResult.SessionName
		}
		logging.Debug("spawnPlanSession completed", "beadID", beadID, "sessionCreated", msg.sessionCreated, "sessionName", msg.sessionName)
		return msg
	}
}

// executeCreateWork creates a work unit with the given branch name.
// This uses the async control plane architecture:
// 1. Creates work record in DB (with auto flag)
// 2. Adds beads to work_beads
// 3. Schedules TaskTypeCreateWorktree task
// 4. Returns immediately (control plane handles worktree creation + orchestrator spawning)
func (m *planModel) executeCreateWork(beadID string, branchName string, auto bool, useExistingBranch bool) tea.Cmd {
	return func() tea.Msg {
		logging.Debug("executeCreateWork started", "beadID", beadID, "branchName", branchName, "auto", auto, "useExistingBranch", useExistingBranch)

		// Collect the bead and any transitive dependencies (or children if it has parent-child relationships)
		allIssueIDs, err := work.CollectIssueIDsForAutomatedWorkflow(m.ctx, beadID, m.proj.Beads)
		if err != nil {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("failed to expand bead %s: %w", beadID, err)}
		}

		if len(allIssueIDs) == 0 {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("no beads found for %s", beadID)}
		}

		// Initialize zellij session (spawns control plane if new session)
		sessionResult, err := control.InitializeSession(m.ctx, m.proj)
		if err != nil {
			logging.Warn("executeCreateWork InitializeSession failed", "error", err)
		}

		// Create work asynchronously (DB operations only, schedules tasks for control plane)
		opts := work.CreateWorkAsyncOptions{
			BranchName:        branchName,
			BaseBranch:        m.proj.Config.Repo.GetBaseBranch(),
			RootIssueID:       beadID,
			Auto:              auto,
			UseExistingBranch: useExistingBranch,
		}
		result, err := work.CreateWorkAsyncWithOptions(m.ctx, m.proj, opts)
		if err != nil {
			logging.Error("executeCreateWork CreateWorkWithBranch failed", "beadID", beadID, "error", err)
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("failed to create work: %w", err)}
		}
		logging.Debug("executeCreateWork CreateWorkWithBranch succeeded", "workID", result.WorkID)

		// Add beads to the work
		logging.Debug("executeCreateWork adding beads to work", "workID", result.WorkID, "beadCount", len(allIssueIDs))
		if err := work.AddBeadsToWorkInternal(m.ctx, m.proj, result.WorkID, allIssueIDs); err != nil {
			logging.Error("executeCreateWork addBeadsToWork failed", "workID", result.WorkID, "error", err)
			// Work was created but beads couldn't be added - don't fail completely
			return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID, err: fmt.Errorf("work created but failed to add beads: %w", err)}
		}
		logging.Debug("executeCreateWork beads added successfully", "workID", result.WorkID)

		// Ensure control plane is running to process the worktree creation task
		// Note: InitializeSession spawns control plane for new sessions, but we call
		// EnsureControlPlane for existing sessions that might have a dead control plane
		err = control.EnsureControlPlane(m.ctx, m.proj)
		if err != nil {
			// Non-fatal: work was created but control plane might need manual start
			return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID, err: fmt.Errorf("work created but control plane failed: %w", err)}
		}
		logging.Debug("executeCreateWork completed successfully", "workID", result.WorkID)

		// Include session creation info in the result
		msg := planWorkCreatedMsg{beadID: beadID, workID: result.WorkID}
		if sessionResult != nil && sessionResult.SessionCreated {
			msg.sessionCreated = true
			msg.sessionName = sessionResult.SessionName
		}
		return msg
	}
}

func (m *planModel) addBeadsToWork(beadIDs []string, workID string) tea.Cmd {
	return func() tea.Msg {
		// Use internal function instead of CLI
		_, err := work.AddBeadsToWork(m.ctx, m.proj, workID, beadIDs)
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
	works              []*progress.WorkProgress
	orchestratorHealth map[string]bool // workID -> orchestrator alive
	err                error
}

// loadWorkTiles loads work data for the work tabs bar
func (m *planModel) loadWorkTiles() tea.Cmd {
	return func() tea.Msg {
		works, err := progress.FetchAllWorksPollData(m.ctx, m.proj)
		if err != nil {
			return workTilesLoadedMsg{err: err}
		}

		// Compute orchestrator health for all works (async)
		orchestratorHealth := make(map[string]bool)
		for _, work := range works {
			if work != nil {
				orchestratorHealth[work.Work.ID] = checkOrchestratorHealth(m.ctx, m.proj.DB, work.Work.ID)
			}
		}

		return workTilesLoadedMsg{works: works, orchestratorHealth: orchestratorHealth}
	}
}

// Helper functions for work commands

// destroyWork schedules a work destruction task via the control plane
func (m *planModel) destroyWork(workID string) tea.Cmd {
	return func() tea.Msg {
		if err := control.ScheduleDestroyWorktree(m.ctx, m.proj, workID); err != nil {
			return workCommandMsg{action: "Destroy work", workID: workID, err: err}
		}

		// Ensure control plane is running to process the destroy task
		if err := control.EnsureControlPlane(m.ctx, m.proj); err != nil {
			// Non-fatal: task was scheduled but control plane might need manual start
			return workCommandMsg{action: "Destroy work scheduled", workID: workID, err: fmt.Errorf("destroy scheduled but control plane failed: %w", err)}
		}

		return workCommandMsg{action: "Destroy work scheduled", workID: workID}
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
			_, err := work.RunWorkAuto(m.ctx, m.proj, workID, io.Discard)
			if err != nil {
				return workCommandMsg{action: "Run work", workID: workID, err: err}
			}
		} else {
			// Use direct mode - creates one task per bead
			_, err := work.RunWork(m.ctx, m.proj, workID, false, io.Discard)
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

		// Generate task ID for review
		reviewTaskNum, err := m.proj.DB.GetNextTaskNumber(m.ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Create review", workID: workID, err: fmt.Errorf("failed to get next task number: %w", err)}
		}
		reviewTaskID := fmt.Sprintf("%s.%d", workID, reviewTaskNum)

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

		// Generate task ID for PR
		prTaskNum, err := m.proj.DB.GetNextTaskNumber(m.ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Create PR", workID: workID, err: fmt.Errorf("failed to get next task number: %w", err)}
		}
		prTaskID := fmt.Sprintf("%s.%d", workID, prTaskNum)

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

		// Ensure control plane is running
		err = control.EnsureControlPlane(m.ctx, m.proj)
		if err != nil {
			return workCommandMsg{action: "Control plane", workID: workID, err: err}
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

// checkOrchestratorHealth checks if the orchestrator has a recent heartbeat for a work
func checkOrchestratorHealth(ctx context.Context, database *db.DB, workID string) bool {
	// Check if an orchestrator has a recent heartbeat in the database
	alive, err := database.IsOrchestratorAlive(ctx, workID, db.DefaultStalenessThreshold)
	if err != nil {
		return false
	}
	return alive
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

		// Kill any existing orchestrator process using pattern-based kill
		// (we use pattern-based kill since we need to actually terminate the process,
		// database check only tells us if it's alive)
		pattern := fmt.Sprintf("co orchestrate --work %s", workID)
		if alive := checkOrchestratorHealth(m.ctx, m.proj.DB, workID); alive {
			_ = process.KillProcess(m.ctx, pattern)
			time.Sleep(500 * time.Millisecond)
		}

		// Ensure control plane is running (may have been killed along with zellij)
		if err := control.EnsureControlPlane(m.ctx, m.proj); err != nil {
			return workCommandMsg{action: "Restart orchestrator", workID: workID, err: fmt.Errorf("failed to ensure control plane: %w", err)}
		}

		// Spawn a new orchestrator
		spawned, err := claude.EnsureWorkOrchestrator(
			m.ctx,
			m.proj.DB,
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
		if err := control.TriggerPRFeedbackCheck(m.ctx, m.proj, workID); err != nil {
			return workCommandMsg{action: "Check PR feedback", workID: workID, err: err}
		}

		// Ensure control plane is running to process the feedback check
		if err := control.EnsureControlPlane(m.ctx, m.proj); err != nil {
			return workCommandMsg{action: "PR feedback check triggered", workID: workID, err: fmt.Errorf("feedback check scheduled but control plane failed: %w", err)}
		}

		return workCommandMsg{action: "PR feedback check triggered", workID: workID}
	}
}
