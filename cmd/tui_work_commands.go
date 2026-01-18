package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/process"
)

// Command generator functions (returning tea.Cmd)

func (m *workModel) refreshData() tea.Cmd {
	return func() tea.Msg {
		works, err := fetchPollData(m.ctx, m.proj, "", "")
		if err != nil {
			return workDataMsg{err: err}
		}

		// Also fetch beads for potential assignment
		beads, _ := fetchBeadsWithFilters(m.proj.MainRepoPath(), beadFilters{status: "ready"})

		return workDataMsg{works: works, beads: beads}
	}
}

func (m *workModel) loadBeadsForAssign() tea.Cmd {
	return func() tea.Msg {
		beads, err := fetchBeadsWithFilters(m.proj.MainRepoPath(), beadFilters{status: "ready"})
		if err != nil {
			return workDataMsg{err: err}
		}
		return workDataMsg{beads: beads}
	}
}

func (m *workModel) destroyWork() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Destroy work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		if err := DestroyWork(m.ctx, m.proj, workID, io.Discard); err != nil {
			return workCommandMsg{action: "Destroy work", err: err}
		}
		return workCommandMsg{action: "Destroy work"}
	}
}

func (m *workModel) removeBeadFromWork(beadID string) tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Remove issue", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		// Remove the bead from the work using the database
		if err := m.proj.DB.RemoveWorkBead(m.ctx, workID, beadID); err != nil {
			return workCommandMsg{action: "Remove issue", err: err}
		}
		return workCommandMsg{action: fmt.Sprintf("Removed %s from work", beadID)}
	}
}

func (m *workModel) runWork(usePlan bool) tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Run work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		result, err := RunWork(m.ctx, m.proj, workID, usePlan, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Run work", err: err}
		}

		orchestratorStatus := "running"
		if result.OrchestratorSpawned {
			orchestratorStatus = "spawned"
		}

		var msg string
		modeStr := ""
		if usePlan {
			modeStr = " (with estimation)"
		}
		if result.TasksCreated > 0 {
			msg = fmt.Sprintf("Created %d task(s)%s, orchestrator %s", result.TasksCreated, modeStr, orchestratorStatus)
		} else {
			msg = fmt.Sprintf("Orchestrator %s", orchestratorStatus)
		}
		return workCommandMsg{action: msg}
	}
}

func (m *workModel) restartOrchestrator() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Restart orchestrator", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Get the work details
		work, err := m.proj.DB.GetWork(m.ctx, workID)
		if err != nil || work == nil {
			return workCommandMsg{action: "Restart orchestrator", err: fmt.Errorf("work not found: %w", err)}
		}

		// Kill any existing orchestrator
		projectName := m.proj.Config.Project.Name

		// Check if process is running and kill it
		pattern := fmt.Sprintf("co orchestrate --work %s", workID)
		if running, _ := process.IsProcessRunning(m.ctx, pattern); running {
			// Process is running, kill it
			process.KillProcess(m.ctx, pattern) // Ignore error as process might have already exited
			time.Sleep(500 * time.Millisecond)
		}

		// Ensure the orchestrator is running (will restart if dead)
		spawned, err := claude.EnsureWorkOrchestrator(
			m.ctx,
			workID,
			projectName,
			work.WorktreePath,
			work.Name,
			io.Discard,
		)
		if err != nil {
			return workCommandMsg{action: "Restart orchestrator", err: err}
		}

		status := "already running"
		if spawned {
			status = "restarted"
		}
		return workCommandMsg{action: fmt.Sprintf("Orchestrator %s", status)}
	}
}

func (m *workModel) createReviewTask() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create review", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Get existing tasks to generate unique review task ID
		ctx := m.ctx
		tasks, err := m.proj.DB.GetWorkTasks(ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Create review", err: fmt.Errorf("failed to get work tasks: %w", err)}
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
		err = m.proj.DB.CreateTask(ctx, reviewTaskID, "review", []string{}, 0, workID)
		if err != nil {
			return workCommandMsg{action: "Create review", err: fmt.Errorf("failed to create task: %w", err)}
		}

		// Include iteration count in success message
		maxIterations := m.proj.Config.Workflow.GetMaxReviewIterations()
		var actionMsg string
		if reviewCount >= maxIterations {
			// Already exceeded the limit, just note it
			actionMsg = fmt.Sprintf("Created review task %s (iteration %d, exceeds limit of %d)", reviewTaskID, reviewCount+1, maxIterations)
		} else if reviewCount+1 == maxIterations {
			// At the limit now
			actionMsg = fmt.Sprintf("Created review task %s (%d/%d iterations - at limit)", reviewTaskID, reviewCount+1, maxIterations)
		} else {
			// Still under the limit
			actionMsg = fmt.Sprintf("Created review task %s (%d/%d iterations)", reviewTaskID, reviewCount+1, maxIterations)
		}

		return workCommandMsg{action: actionMsg}
	}
}

func (m *workModel) createPRTask() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create PR", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Check if work is completed
		if wp.work.Status != db.StatusCompleted {
			return workCommandMsg{action: "Create PR", err: fmt.Errorf("work %s is not completed (status: %s)", workID, wp.work.Status)}
		}

		// Check if PR already exists
		if wp.work.PRURL != "" {
			return workCommandMsg{action: fmt.Sprintf("PR already exists: %s", wp.work.PRURL)}
		}

		// Generate task ID for PR creation
		prTaskID := fmt.Sprintf("%s.pr", workID)

		// Create the PR task
		ctx := m.ctx
		err := m.proj.DB.CreateTask(ctx, prTaskID, "pr", []string{}, 0, workID)
		if err != nil {
			return workCommandMsg{action: "Create PR", err: fmt.Errorf("failed to create task: %w", err)}
		}

		return workCommandMsg{action: fmt.Sprintf("Created PR task %s", prTaskID)}
	}
}

func (m *workModel) updatePRDescriptionTask() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Update PR", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Check if work has a PR
		if wp.work.PRURL == "" {
			return workCommandMsg{action: "Update PR", err: fmt.Errorf("work %s has no PR", workID)}
		}

		// Create update-pr-description task
		taskID := fmt.Sprintf("%s.update-pr-%d", workID, time.Now().Unix())
		ctx := context.Background()
		err := m.proj.DB.CreateTask(ctx, taskID, "update-pr-description", []string{}, 0, workID)
		if err != nil {
			return workCommandMsg{action: "Update PR", err: err}
		}

		// Process the task
		cmd := exec.Command("co", "orchestrate", "--task", taskID)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Update PR", err: err}
		}
		return workCommandMsg{action: "Update PR"}
	}
}

func (m *workModel) assignSelectedBeads() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Assign beads", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		// Collect selected bead IDs
		var beadIDs []string
		for id, selected := range m.selectedBeads {
			if selected {
				beadIDs = append(beadIDs, id)
			}
		}

		if len(beadIDs) == 0 {
			return workCommandMsg{action: "Assign beads", err: fmt.Errorf("no beads selected")}
		}

		result, err := AddBeadsToWork(m.ctx, m.proj, workID, beadIDs)
		if err != nil {
			return workCommandMsg{action: "Assign beads", err: err}
		}

		return workCommandMsg{action: fmt.Sprintf("Assigned %d bead(s)", result.BeadsAdded)}
	}
}

func (m *workModel) createBeadAndAssign(title, beadType string, priority int, isEpic bool, description string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID
		mainRepoPath := m.proj.MainRepoPath()

		// Create the bead using beads package
		beadID, err := beads.Create(ctx, mainRepoPath, beads.CreateOptions{
			Title:       title,
			Type:        beadType,
			Priority:    priority,
			IsEpic:      isEpic,
			Description: description,
		})
		if err != nil {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("failed to create issue: %w", err)}
		}

		// Assign the bead to the current work
		_, err = AddBeadsToWork(m.ctx, m.proj, workID, []string{beadID})
		if err != nil {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("created issue %s but failed to assign to work: %w", beadID, err)}
		}

		return workCommandMsg{action: fmt.Sprintf("Created and assigned %s", beadID)}
	}
}

func (m *workModel) openConsole() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Open console", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		err := claude.OpenConsole(m.ctx, workID, m.proj.Config.Project.Name, wp.work.WorktreePath, wp.work.Name, m.proj.Config.Hooks.Env, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Open console", err: err}
		}

		return workCommandMsg{action: fmt.Sprintf("Opened console for %s", workID)}
	}
}

func (m *workModel) openClaude() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Open Claude session", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		err := claude.OpenClaudeSession(m.ctx, workID, m.proj.Config.Project.Name, wp.work.WorktreePath, wp.work.Name, m.proj.Config.Hooks.Env, m.proj.Config, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Open Claude session", err: err}
		}

		return workCommandMsg{action: fmt.Sprintf("Opened Claude session for %s", workID)}
	}
}

// pollPRFeedback triggers a manual PR feedback poll for the selected work
func (m *workModel) pollPRFeedback() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Poll PR feedback", err: fmt.Errorf("no work selected")}
		}

		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Check if work has a PR URL
		if wp.work.PRURL == "" {
			return workCommandMsg{action: "Poll PR feedback", err: fmt.Errorf("work %s has no PR URL", workID)}
		}

		// Create a signal file that the orchestrator will watch for
		signalPath := filepath.Join(m.proj.Root, ".co", fmt.Sprintf("poll-feedback-%s-%d", workID, time.Now().UnixNano()))

		// Write the signal file
		if err := os.WriteFile(signalPath, []byte(wp.work.PRURL), 0644); err != nil {
			return workCommandMsg{action: "Poll PR feedback", err: fmt.Errorf("failed to create poll signal: %w", err)}
		}

		// Clean up the signal file after a short delay
		go func() {
			time.Sleep(2 * time.Second)
			_ = os.Remove(signalPath)
		}()

		return workCommandMsg{action: fmt.Sprintf("Triggered PR feedback poll for %s", workID)}
	}
}