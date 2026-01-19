package cmd

import (
	"fmt"
	"io"
	"os/exec"
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
		case "k", "up":
			m.createWorkButtonIdx--
			if m.createWorkButtonIdx < 0 {
				m.createWorkButtonIdx = 2
			}
		case "j", "down":
			m.createWorkButtonIdx = (m.createWorkButtonIdx + 1) % 3
		case "enter":
			branchName := strings.TrimSpace(m.createWorkBranch.Value())
			if branchName == "" {
				m.statusMessage = "Branch name cannot be empty"
				m.statusIsError = true
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


// renderCreateWorkInlineContent renders the work creation panel inline in the details area.
// This function implements the button position tracking mechanism by:
// 1. Clearing previous button positions at the start (m.dialogButtons = nil)
// 2. Tracking the current line number as content is rendered
// 3. Recording each button's position when it's drawn to the screen
// 4. Storing positions relative to the content area for accurate mouse detection
func (m *planModel) renderCreateWorkInlineContent(visibleLines int, width int) string {
	var content strings.Builder

	// Clear previous button positions to ensure we're tracking the current render state.
	// This is critical for accuracy as button positions may change between renders
	// due to content changes, terminal resizing, or scrolling.
	m.dialogButtons = nil

	// Track current line number (starting at 0, counting lines in the content area)
	currentLine := 0

	// Panel header
	content.WriteString(tuiSuccessStyle.Render("Create Work"))
	content.WriteString("\n\n")
	currentLine += 2 // header + blank line

	// Show bead info
	var beadInfo string
	if len(m.createWorkBeadIDs) == 1 {
		beadInfo = fmt.Sprintf("Creating work from issue: %s", issueIDStyle.Render(m.createWorkBeadIDs[0]))
	} else {
		beadInfo = fmt.Sprintf("Creating work from %d issues", len(m.createWorkBeadIDs))
		// List the first few IDs
		content.WriteString(beadInfo)
		content.WriteString("\n")
		currentLine++ // bead info line
		maxShow := 5
		if len(m.createWorkBeadIDs) < maxShow {
			maxShow = len(m.createWorkBeadIDs)
		}
		for i := 0; i < maxShow; i++ {
			content.WriteString("  • " + issueIDStyle.Render(m.createWorkBeadIDs[i]))
			content.WriteString("\n")
			currentLine++ // each bead ID line
		}
		if len(m.createWorkBeadIDs) > maxShow {
			content.WriteString(fmt.Sprintf("  ... and %d more", len(m.createWorkBeadIDs)-maxShow))
			content.WriteString("\n")
			currentLine++ // "... and N more" line
		}
		content.WriteString("\n")
		currentLine++ // blank line
	}

	if len(m.createWorkBeadIDs) == 1 {
		content.WriteString(beadInfo)
		content.WriteString("\n\n")
		currentLine += 2 // bead info + blank line
	}

	// Branch name input
	branchLabel := "Branch name:"
	if m.createWorkField == 0 {
		branchLabel = tuiSuccessStyle.Render("Branch name:") + " " + tuiDimStyle.Render("(editing)")
	} else {
		branchLabel = tuiLabelStyle.Render("Branch name:")
	}
	content.WriteString(branchLabel)
	content.WriteString("\n")
	currentLine++ // branch label line
	content.WriteString(m.createWorkBranch.View())
	content.WriteString("\n\n")
	currentLine += 2 // branch input + blank line

	// Action buttons with better spacing
	content.WriteString("Actions:\n")
	currentLine++ // "Actions:" label

	// Execute button
	executeStyle := tuiDimStyle
	executePrefix := "  "
	if m.createWorkField == 1 && m.createWorkButtonIdx == 0 {
		executeStyle = tuiSelectedStyle
		executePrefix = "► "
	} else if m.hoveredDialogButton == "execute" {
		executeStyle = tuiSuccessStyle
	}
	// Track Execute button position for mouse click detection.
	// The position is calculated dynamically based on the actual button text,
	// which changes when selected (adds "► " prefix). This ensures accurate
	// click detection regardless of the button's visual state.
	executeButtonText := executePrefix + "Execute"
	m.dialogButtons = append(m.dialogButtons, ButtonRegion{
		ID:     "execute",
		Y:      currentLine,                // Y position relative to content area
		StartX: 2,                           // Button always starts at column 2
		EndX:   2 + len(executeButtonText), // End position based on actual text length
	})
	content.WriteString("  " + executeStyle.Render(executeButtonText))
	content.WriteString(" - Create work and spawn orchestrator\n")
	currentLine++

	// Auto button
	autoStyle := tuiDimStyle
	autoPrefix := "  "
	if m.createWorkField == 1 && m.createWorkButtonIdx == 1 {
		autoStyle = tuiSelectedStyle
		autoPrefix = "► "
	} else if m.hoveredDialogButton == "auto" {
		autoStyle = tuiSuccessStyle
	}
	// Track Auto button position
	autoButtonText := autoPrefix + "Auto"
	m.dialogButtons = append(m.dialogButtons, ButtonRegion{
		ID:     "auto",
		Y:      currentLine,
		StartX: 2,
		EndX:   2 + len(autoButtonText), // Calculate based on actual text length
	})
	content.WriteString("  " + autoStyle.Render(autoButtonText))
	content.WriteString(" - Create work with automated workflow\n")
	currentLine++

	// Cancel button
	cancelStyle := tuiDimStyle
	cancelPrefix := "  "
	if m.createWorkField == 1 && m.createWorkButtonIdx == 2 {
		cancelStyle = tuiSelectedStyle
		cancelPrefix = "► "
	} else if m.hoveredDialogButton == "cancel" {
		cancelStyle = tuiSuccessStyle
	}
	// Track Cancel button position
	cancelButtonText := cancelPrefix + "Cancel"
	m.dialogButtons = append(m.dialogButtons, ButtonRegion{
		ID:     "cancel",
		Y:      currentLine,
		StartX: 2,
		EndX:   2 + len(cancelButtonText), // Calculate based on actual text length
	})
	content.WriteString("  " + cancelStyle.Render(cancelButtonText))
	content.WriteString(" - Cancel work creation\n")
	currentLine++

	// Navigation help
	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("Navigation: [Tab/Shift+Tab] Switch field  [↑↓/jk] Select button  [Enter] Confirm  [Esc] Cancel"))

	return content.String()
}

// executeCreateWork creates a work unit with the given branch name.
// This calls internal logic directly instead of shelling out to the CLI.
func (m *planModel) executeCreateWork(beadIDs []string, branchName string, auto bool) tea.Cmd {
	return func() tea.Msg {
		firstBeadID := beadIDs[0]

		// Expand all beads (handles epics and transitive deps)
		var allIssueIDs []string
		for _, beadID := range beadIDs {
			expandedIDs, err := collectIssueIDsForAutomatedWorkflow(m.ctx, beadID, m.proj.Beads)
			if err != nil {
				return planWorkCreatedMsg{beadID: firstBeadID, err: fmt.Errorf("failed to expand bead %s: %w", beadID, err)}
			}
			allIssueIDs = append(allIssueIDs, expandedIDs...)
		}

		if len(allIssueIDs) == 0 {
			return planWorkCreatedMsg{beadID: firstBeadID, err: fmt.Errorf("no beads found for %v", beadIDs)}
		}

		// All selected beads go into one group (like comma-separated on CLI)
		beadGroups := []beadGroup{{issueIDs: allIssueIDs}}

		// Create work with branch name (silent to avoid console output in TUI)
		// The first bead becomes the root issue ID
		result, err := CreateWorkWithBranch(m.ctx, m.proj, branchName, "main", firstBeadID, WorkCreateOptions{Silent: true})
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

// loadAvailableWorks loads the list of available works with root issue info
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
				item := workItem{
					id:          w.ID,
					status:      w.Status,
					branch:      w.BranchName,
					rootIssueID: w.RootIssueID,
				}
				// Try to get the root issue title from beads cache
				if w.RootIssueID != "" && m.proj.Beads != nil {
					if bead, err := m.proj.Beads.GetBead(m.ctx, w.RootIssueID); err == nil {
						item.rootIssueTitle = bead.Title
					}
				}
				items = append(items, item)
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

