package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
)

// sessionName returns the zellij session name for this project
func (m *planModel) sessionName() string {
	return fmt.Sprintf("co-%s", m.proj.Config.Project.Name)
}

// spawnPlanSession spawns or resumes a planning session for a specific bead
func (m *planModel) spawnPlanSession(beadID string) tea.Cmd {
	return func() tea.Msg {
		session := m.sessionName()
		tabName := db.TabNameForBead(beadID)
		mainRepoPath := m.proj.MainRepoPath()

		// Ensure zellij session exists
		if err := m.zj.EnsureSession(m.ctx, session); err != nil {
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		// Check if session already running for this bead
		running, _ := m.proj.DB.IsPlanSessionRunning(m.ctx, beadID)
		if running {
			// Session exists - just switch to it
			if err := m.zj.SwitchToTab(m.ctx, session, tabName); err != nil {
				return planSessionSpawnedMsg{beadID: beadID, err: err}
			}
			return planSessionSpawnedMsg{beadID: beadID, resumed: true}
		}

		// Check if tab exists (might be orphaned)
		exists, _ := m.zj.TabExists(m.ctx, session, tabName)
		if exists {
			// Tab exists but session not registered - terminate and recreate
			_ = m.zj.TerminateAndCloseTab(m.ctx, session, tabName)
			time.Sleep(200 * time.Millisecond)
		}

		// Create new tab for this bead
		if err := m.zj.CreateTab(m.ctx, session, tabName, mainRepoPath); err != nil {
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		// Switch to the tab
		time.Sleep(200 * time.Millisecond)
		if err := m.zj.SwitchToTab(m.ctx, session, tabName); err != nil {
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		// Run co plan with the bead ID
		planCmd := fmt.Sprintf("co plan %s", beadID)
		time.Sleep(200 * time.Millisecond)
		if err := m.zj.ExecuteCommand(m.ctx, session, planCmd); err != nil {
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
		result, err := CreateWorkWithBranch(m.ctx, m.proj, branchName, "main", beadID, WorkCreateOptions{Silent: true})
		if err != nil {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("failed to create work: %w", err)}
		}

		// Add beads to the work
		if err := addBeadsToWork(m.ctx, m.proj, result.WorkID, allIssueIDs); err != nil {
			// Work was created but beads couldn't be added - don't fail completely
			return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID, err: fmt.Errorf("work created but failed to add beads: %w", err)}
		}

		// Spawn the orchestrator for this work (or run automated workflow if auto)
		if auto {
			// Run automated workflow in a separate goroutine since it's long-running
			go func() {
				_ = runAutomatedWorkflowForWork(m.proj, result.WorkID, result.WorktreePath, io.Discard)
			}()
		} else {
			// Spawn the orchestrator
			if err := claude.SpawnWorkOrchestrator(m.ctx, result.WorkID, m.proj.Config.Project.Name, result.WorktreePath, result.WorkerName, io.Discard); err != nil {
				// Non-fatal: work was created but orchestrator failed to spawn
				return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID, err: fmt.Errorf("work created but orchestrator failed: %w", err)}
			}
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

// workTilesLoadedMsg indicates work tiles have been loaded for overlay
type workTilesLoadedMsg struct {
	works []*workProgress
	err   error
}

// loadWorkTiles loads work data for the overlay display
func (m *planModel) loadWorkTiles() tea.Cmd {
	return func() tea.Msg {
		works, err := fetchPollData(m.ctx, m.proj, "", "")
		if err != nil {
			return workTilesLoadedMsg{err: err}
		}
		return workTilesLoadedMsg{works: works}
	}
}

// Helper functions for work commands
func (m *planModel) destroyWork(workID string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Implementation would call the actual destroy work logic
		// For now, just return a simple completion message
		return planWorkCreatedMsg{
			workID: workID,
			beadID: "",
			err:    fmt.Errorf("destroy work not yet implemented"),
		}
	}
}

func (m *planModel) planWork(workID string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Implementation would call the actual plan work logic
		// For now, just return a simple completion message
		return planWorkCreatedMsg{
			workID: workID,
			beadID: "",
			err:    fmt.Errorf("plan work not yet implemented"),
		}
	}
}

func (m *planModel) runWork(workID string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Implementation would call the actual run work logic
		// For now, just return a simple completion message
		return planWorkCreatedMsg{
			workID: workID,
			beadID: "",
			err:    fmt.Errorf("run work not yet implemented"),
		}
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
