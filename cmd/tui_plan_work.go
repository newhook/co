package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

		// Create work with branch name (silent to avoid console output in TUI)
		// The first bead becomes the root issue ID
		result, err := CreateWorkWithBranch(m.ctx, m.proj, branchName, "main", firstBeadID, WorkCreateOptions{Silent: true})
		if err != nil {
			return planWorkCreatedMsg{beadID: firstBeadID, err: fmt.Errorf("failed to create work: %w", err)}
		}

		// Add beads to the work
		if err := addBeadsToWork(m.ctx, m.proj, result.WorkID, allIssueIDs); err != nil {
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
			// Show all works (users might want to add to completed works)
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
		return worksLoadedMsg{works: items}
	}
}

// addBeadToWork adds a bead to an existing work
func (m *planModel) addBeadToWork(beadID, workID string) tea.Cmd {
	return func() tea.Msg {
		// Use internal function instead of CLI
		_, err := AddBeadsToWork(m.ctx, m.proj, workID, []string{beadID})
		if err != nil {
			return beadAddedToWorkMsg{beadID: beadID, workID: workID, err: fmt.Errorf("failed to add issue to work: %w", err)}
		}

		return beadAddedToWorkMsg{beadID: beadID, workID: workID}
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

// updateWorkOverlay handles input when in work overlay mode
func (m *planModel) updateWorkOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Exit work overlay without selection
		m.viewMode = ViewNormal
		m.selectedWorkTileID = ""
		return m, nil
	case tea.KeyEnter:
		// Select a work tile if one is hovered/selected
		if m.selectedWorkTileID != "" {
			// Set the focused work and return to normal view with split screen
			m.focusedWorkID = m.selectedWorkTileID
			m.viewMode = ViewNormal
			m.statusMessage = fmt.Sprintf("Focused on work %s", m.focusedWorkID)
			m.statusIsError = false
			// Reset focus filter when selecting a new work
			m.focusFilterActive = false
			return m, nil
		}
		return m, nil
	}

	// Navigation through work tiles
	switch msg.String() {
	case "j", "down":
		// Move to next work tile
		if len(m.workTiles) > 0 {
			currentIdx := -1
			for i, work := range m.workTiles {
				if work.work.ID == m.selectedWorkTileID {
					currentIdx = i
					break
				}
			}
			if currentIdx < len(m.workTiles)-1 {
				if currentIdx == -1 {
					m.selectedWorkTileID = m.workTiles[0].work.ID
				} else {
					m.selectedWorkTileID = m.workTiles[currentIdx+1].work.ID
				}
			}
		}
		return m, nil
	case "k", "up":
		// Move to previous work tile
		if len(m.workTiles) > 0 {
			currentIdx := -1
			for i, work := range m.workTiles {
				if work.work.ID == m.selectedWorkTileID {
					currentIdx = i
					break
				}
			}
			if currentIdx > 0 {
				m.selectedWorkTileID = m.workTiles[currentIdx-1].work.ID
			} else if currentIdx == -1 && len(m.workTiles) > 0 {
				m.selectedWorkTileID = m.workTiles[len(m.workTiles)-1].work.ID
			}
		}
		return m, nil
	}

	return m, nil
}

// renderWorkOverlay renders the work tiles overlay
func (m *planModel) renderWorkOverlay() string {
	if m.loading {
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true).
			Align(lipgloss.Center, lipgloss.Center)

		return loadingStyle.
			Width(m.width).
			Height(m.height).
			Render("Loading works...")
	}

	// Create work tiles grid
	var tiles []string
	tileWidth := 30
	tileHeight := 8
	tilesPerRow := m.width / (tileWidth + 2)
	if tilesPerRow < 1 {
		tilesPerRow = 1
	}

	// Style for tiles
	normalTileStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(tileWidth).
		Height(tileHeight).
		Padding(1)

	selectedTileStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Width(tileWidth).
		Height(tileHeight).
		Padding(1).
		Bold(true)

	for _, work := range m.workTiles {
		// Build tile content
		var content strings.Builder
		content.WriteString(fmt.Sprintf("%s %s\n", statusIcon(work.work.Status), work.work.ID))
		content.WriteString(fmt.Sprintf("Branch: %s\n", truncateString(work.work.BranchName, tileWidth-4)))

		// Show task and bead counts
		completedTasks := 0
		totalTasks := len(work.tasks)
		for _, task := range work.tasks {
			if task.task.Status == db.StatusCompleted {
				completedTasks++
			}
		}
		content.WriteString(fmt.Sprintf("Tasks: %d/%d\n", completedTasks, totalTasks))

		// Show unassigned beads count if any
		if work.unassignedBeadCount > 0 {
			content.WriteString(fmt.Sprintf("Unassigned: %d\n", work.unassignedBeadCount))
		}

		// Show feedback count if any
		if work.feedbackCount > 0 {
			content.WriteString(fmt.Sprintf("Feedback: %d\n", work.feedbackCount))
		}

		// Apply appropriate style
		var tileStyle lipgloss.Style
		if work.work.ID == m.selectedWorkTileID {
			tileStyle = selectedTileStyle
		} else {
			tileStyle = normalTileStyle
		}

		tiles = append(tiles, tileStyle.Render(content.String()))
	}

	// Arrange tiles in grid
	var rows []string
	for i := 0; i < len(tiles); i += tilesPerRow {
		end := i + tilesPerRow
		if end > len(tiles) {
			end = len(tiles)
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top, tiles[i:end]...)
		rows = append(rows, row)
	}

	// Add header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("214")).
		MarginBottom(1)
	header := headerStyle.Render("Work Overlay - Press Enter to select, Esc to cancel")

	// Add instructions if no works
	if len(m.workTiles) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Align(lipgloss.Center, lipgloss.Center).
			Width(m.width).
			Height(m.height - 3)
		return lipgloss.JoinVertical(lipgloss.Left, header, emptyStyle.Render("No works found"))
	}

	// Combine everything
	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	fullContent := lipgloss.JoinVertical(lipgloss.Left, header, content)

	// Center the content
	centeredStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Center)

	return centeredStyle.Render(fullContent)
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

