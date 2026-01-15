package cmd

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/newhook/co/internal/beads"
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

// updateCreateWorkDialog handles input for the create work dialog
func (m *planModel) updateCreateWorkDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.viewMode = ViewNormal
		m.createWorkBranch.Blur()
		return m, nil
	}

	// Tab cycles between branch(0), buttons(1)
	if msg.Type == tea.KeyTab {
		m.createWorkField = (m.createWorkField + 1) % 2
		if m.createWorkField == 0 {
			m.createWorkBranch.Focus()
		} else {
			m.createWorkBranch.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.createWorkField = 1 - m.createWorkField
		if m.createWorkField == 0 {
			m.createWorkBranch.Focus()
		} else {
			m.createWorkBranch.Blur()
		}
		return m, nil
	}

	// Handle input based on focused field
	var cmd tea.Cmd
	switch m.createWorkField {
	case 0: // Branch name input
		m.createWorkBranch, cmd = m.createWorkBranch.Update(msg)
	case 1: // Buttons
		switch msg.String() {
		case "h", "left":
			m.createWorkButtonIdx--
			if m.createWorkButtonIdx < 0 {
				m.createWorkButtonIdx = 2
			}
		case "l", "right":
			m.createWorkButtonIdx = (m.createWorkButtonIdx + 1) % 3
		case "enter":
			branchName := strings.TrimSpace(m.createWorkBranch.Value())
			if branchName == "" {
				return m, nil
			}
			switch m.createWorkButtonIdx {
			case 0: // Execute
				m.viewMode = ViewNormal
				// Clear selections after work creation
				m.selectedBeads = make(map[string]bool)
				return m, m.executeCreateWork(m.createWorkBeadIDs, branchName, false)
			case 1: // Auto
				m.viewMode = ViewNormal
				// Clear selections after work creation
				m.selectedBeads = make(map[string]bool)
				return m, m.executeCreateWork(m.createWorkBeadIDs, branchName, true)
			case 2: // Cancel
				m.viewMode = ViewNormal
				m.createWorkBranch.Blur()
			}
			return m, nil
		}
	}
	return m, cmd
}

// renderCreateWorkDialogContent renders the create work dialog
func (m *planModel) renderCreateWorkDialogContent() string {
	branchLabel := "Branch:"
	if m.createWorkField == 0 {
		branchLabel = tuiValueStyle.Render("Branch:") + " (editing)"
	}

	// Render buttons
	var execBtn, autoBtn, cancelBtn string
	if m.createWorkField == 1 {
		switch m.createWorkButtonIdx {
		case 0:
			execBtn = tuiSelectedStyle.Render(" Execute ")
			autoBtn = tuiDimStyle.Render(" Auto ")
			cancelBtn = tuiDimStyle.Render(" Cancel ")
		case 1:
			execBtn = tuiDimStyle.Render(" Execute ")
			autoBtn = tuiSelectedStyle.Render(" Auto ")
			cancelBtn = tuiDimStyle.Render(" Cancel ")
		case 2:
			execBtn = tuiDimStyle.Render(" Execute ")
			autoBtn = tuiDimStyle.Render(" Auto ")
			cancelBtn = tuiSelectedStyle.Render(" Cancel ")
		}
	} else {
		execBtn = tuiDimStyle.Render(" Execute ")
		autoBtn = tuiDimStyle.Render(" Auto ")
		cancelBtn = tuiDimStyle.Render(" Cancel ")
	}

	// Show bead IDs or count
	var beadInfo string
	if len(m.createWorkBeadIDs) == 1 {
		beadInfo = issueIDStyle.Render(m.createWorkBeadIDs[0])
	} else {
		beadInfo = fmt.Sprintf("%d issues", len(m.createWorkBeadIDs))
	}

	content := fmt.Sprintf(`  Create Work from %s

  %s
  %s

  %s  %s  %s

  [Tab] Switch field  [Esc] Cancel
`, beadInfo, branchLabel, m.createWorkBranch.View(), execBtn, autoBtn, cancelBtn)

	return tuiDialogStyle.Render(content)
}

// executeCreateWork creates a work unit with the given branch name.
// This calls internal logic directly instead of shelling out to the CLI.
func (m *planModel) executeCreateWork(beadIDs []string, branchName string, auto bool) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()
		firstBeadID := beadIDs[0]

		// Expand all beads (handles epics and transitive deps)
		var allBeads []beads.BeadWithDeps
		for _, beadID := range beadIDs {
			expandedBeads, err := collectBeadsForAutomatedWorkflow(m.ctx, beadID, mainRepoPath)
			if err != nil {
				return planWorkCreatedMsg{beadID: firstBeadID, err: fmt.Errorf("failed to expand bead %s: %w", beadID, err)}
			}
			allBeads = append(allBeads, expandedBeads...)
		}

		if len(allBeads) == 0 {
			return planWorkCreatedMsg{beadID: firstBeadID, err: fmt.Errorf("no beads found for %v", beadIDs)}
		}

		// Convert to beadGroup for compatibility with existing code
		// All selected beads go into one group (like comma-separated on CLI)
		var groupBeads []*beads.Bead
		for _, b := range allBeads {
			groupBeads = append(groupBeads, &beads.Bead{
				ID:          b.ID,
				Title:       b.Title,
				Description: b.Description,
			})
		}
		beadGroups := []beadGroup{{beads: groupBeads}}

		// Create work with branch name (silent to avoid console output in TUI)
		result, err := CreateWorkWithBranch(m.ctx, m.proj, branchName, "main", WorkCreateOptions{Silent: true})
		if err != nil {
			return planWorkCreatedMsg{beadID: firstBeadID, err: fmt.Errorf("failed to create work: %w", err)}
		}

		// Add beads to the work
		if err := addBeadGroupsToWork(m.ctx, m.proj, result.WorkID, beadGroups); err != nil {
			// Work was created but beads couldn't be added - don't fail completely
			return planWorkCreatedMsg{beadID: firstBeadID, workID: result.WorkID, err: fmt.Errorf("work created but failed to add beads: %w", err)}
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
				return planWorkCreatedMsg{beadID: firstBeadID, workID: result.WorkID, err: fmt.Errorf("work created but orchestrator failed: %w", err)}
			}
		}

		return planWorkCreatedMsg{beadID: firstBeadID, workID: result.WorkID}
	}
}

// loadAvailableWorks loads the list of available works
func (m *planModel) loadAvailableWorks() tea.Cmd {
	return func() tea.Msg {
		// Empty string means no filter (all statuses)
		works, err := m.proj.DB.ListWorks(m.ctx, "")
		if err != nil {
			return worksLoadedMsg{err: err}
		}

		var items []workItem
		for _, w := range works {
			// Only show pending/processing works (not completed/failed)
			if w.Status == "pending" || w.Status == "processing" {
				items = append(items, workItem{
					id:     w.ID,
					status: w.Status,
					branch: w.BranchName,
				})
			}
		}
		return worksLoadedMsg{works: items}
	}
}

// addBeadToWork adds a bead to an existing work
func (m *planModel) addBeadToWork(beadID, workID string) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()

		// Use co work add command
		cmd := exec.Command("co", "work", "add", beadID, "--work="+workID)
		cmd.Dir = mainRepoPath
		if err := cmd.Run(); err != nil {
			return beadAddedToWorkMsg{beadID: beadID, workID: workID, err: fmt.Errorf("failed to add issue to work: %w", err)}
		}

		return beadAddedToWorkMsg{beadID: beadID, workID: workID}
	}
}

// createChildBead creates a new bead that depends on the parent bead
func (m *planModel) createChildBead(title, beadType string, priority int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		mainRepoPath := m.proj.MainRepoPath()
		parentID := m.parentBeadID

		// Create the new bead
		newBeadID, err := beads.Create(ctx, mainRepoPath, beads.CreateOptions{
			Title:    title,
			Type:     beadType,
			Priority: priority,
		})
		if err != nil {
			return planDataMsg{err: fmt.Errorf("failed to create issue: %w", err)}
		}

		// Add dependency: new bead depends on parent (parent blocks new bead)
		if newBeadID != "" && parentID != "" {
			_ = beads.AddDependency(ctx, newBeadID, parentID, mainRepoPath) // Best effort
		}

		// Refresh after creation
		items, err := m.loadBeads()
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}
