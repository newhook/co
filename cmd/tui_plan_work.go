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
		m.overlayFocused = false
		return m, nil
	case tea.KeyTab:
		// Toggle focus between overlay and issues below
		m.overlayFocused = !m.overlayFocused
		return m, nil
	case tea.KeyEnter:
		// Select a work tile if one is hovered/selected (only when overlay focused)
		if m.overlayFocused && m.selectedWorkTileID != "" {
			// Set the focused work and return to normal view with split screen
			m.focusedWorkID = m.selectedWorkTileID
			m.viewMode = ViewNormal
			m.overlayFocused = false
			m.statusMessage = fmt.Sprintf("Focused on work %s", m.focusedWorkID)
			m.statusIsError = false
			// Reset focus filter when selecting a new work
			m.focusFilterActive = false
			return m, nil
		}
		return m, nil
	}

	// Navigation - depends on which section has focus
	switch msg.String() {
	case "tab":
		// Also handle tab as string (fallback)
		m.overlayFocused = !m.overlayFocused
		return m, nil
	case "j", "down":
		if m.overlayFocused {
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
		} else {
			// Navigate issues below the overlay
			if m.beadsCursor < len(m.beadItems)-1 {
				m.beadsCursor++
			}
		}
		return m, nil
	case "k", "up":
		if m.overlayFocused {
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
		} else {
			// Navigate issues below the overlay
			if m.beadsCursor > 0 {
				m.beadsCursor--
			}
		}
		return m, nil

	case "c":
		// Create new work - exit overlay and show create dialog
		if m.beadsCursor < len(m.beadItems) {
			selectedBead := m.beadItems[m.beadsCursor]
			m.createWorkBeadIDs = []string{selectedBead.id}
			m.viewMode = ViewCreateWork
			m.selectedWorkTileID = ""

			// Generate initial branch name
			beads := []*beadsForBranch{{ID: selectedBead.id, Title: selectedBead.title}}
			initialBranch := generateBranchNameFromBeadsForBranch(beads)
			m.createWorkBranch.SetValue(initialBranch)
			m.createWorkBranch.Focus()
			m.createWorkField = 0
			m.createWorkButtonIdx = 0
		}
		return m, nil

	case "d":
		// Destroy selected work
		if m.selectedWorkTileID != "" {
			// TODO: Add confirmation dialog before destroying
			m.statusMessage = fmt.Sprintf("Destroying work %s...", m.selectedWorkTileID)
			m.statusIsError = false
			return m, m.destroyWork(m.selectedWorkTileID)
		}
		return m, nil

	case "p":
		// Plan selected work
		if m.selectedWorkTileID != "" {
			m.statusMessage = fmt.Sprintf("Planning work %s...", m.selectedWorkTileID)
			m.statusIsError = false
			// Exit overlay and run plan
			m.viewMode = ViewNormal
			return m, m.planWork(m.selectedWorkTileID)
		}
		return m, nil

	case "r":
		// Run selected work
		if m.selectedWorkTileID != "" {
			m.statusMessage = fmt.Sprintf("Running work %s...", m.selectedWorkTileID)
			m.statusIsError = false
			// Exit overlay and run work
			m.viewMode = ViewNormal
			return m, m.runWork(m.selectedWorkTileID)
		}
		return m, nil

	case "h", "left":
		// For grid layout in future - move left in grid
		// For now, just move up
		return m.updateWorkOverlay(tea.KeyMsg{Type: tea.KeyUp})

	case "l", "right":
		// For grid layout in future - move right in grid
		// For now, just move down
		return m.updateWorkOverlay(tea.KeyMsg{Type: tea.KeyDown})
	}

	return m, nil
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

// renderWorkOverlayDropdown renders the work dropdown panel at the top
func (m *planModel) renderWorkOverlayDropdown() string {
	// Calculate dropdown height (about 40% of screen for more details)
	dropdownHeight := int(float64(m.height) * 0.4)
	if dropdownHeight < 12 {
		dropdownHeight = 12
	} else if dropdownHeight > 25 {
		dropdownHeight = 25
	}

	// Create dropdown panel style with shadow effect
	// Highlight border when overlay is focused
	borderColor := "240"
	if m.overlayFocused {
		borderColor = "214" // Orange when focused
	}
	dropdownStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(dropdownHeight).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		BorderBottom(true).
		BorderLeft(false).
		BorderRight(false).
		BorderTop(false).
		Background(lipgloss.Color("235"))

	if m.loading {
		loadingContent := lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true).
			Align(lipgloss.Center, lipgloss.Center).
			Width(m.width - 2).
			Height(dropdownHeight - 2).
			Render("Loading works...")
		return dropdownStyle.Render(loadingContent)
	}

	var content strings.Builder

	// Header bar with close hint
	headerBar := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("255")).
		Bold(true).
		Width(m.width - 2).
		Padding(0, 1).
		Render("Work Overview                                                  [Esc] Close")
	content.WriteString(headerBar)
	content.WriteString("\n")

	// Instructions line
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("247")).
		Padding(0, 1)
	content.WriteString(instructionStyle.Render(
		"[↑↓] Navigate  [Tab] Switch Focus  [Enter] Select  [c] Create  [d] Destroy  [p] Plan  [r] Run"))
	content.WriteString("\n")

	// Calculate available space for work items (2 lines per work now)
	availableLines := dropdownHeight - 4 // -4 for header, instructions, borders
	worksPerPage := availableLines / 3   // Each work takes 3 lines now

	if len(m.workTiles) == 0 {
		// No works message
		emptyMsg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true).
			Align(lipgloss.Center).
			Width(m.width - 2).
			Height(availableLines).
			Render("No works found. Press 'c' to create a new work.")
		content.WriteString(emptyMsg)
	} else {
		// Find selected index
		selectedIndex := -1
		for i, work := range m.workTiles {
			if work.work.ID == m.selectedWorkTileID {
				selectedIndex = i
				break
			}
		}

		// Calculate visible window (for works that take multiple lines)
		startIdx := 0
		if selectedIndex >= worksPerPage {
			startIdx = selectedIndex - worksPerPage/2
			if startIdx < 0 {
				startIdx = 0
			}
		}
		endIdx := startIdx + worksPerPage
		if endIdx > len(m.workTiles) {
			endIdx = len(m.workTiles)
			if endIdx-startIdx < worksPerPage && len(m.workTiles) >= worksPerPage {
				startIdx = endIdx - worksPerPage
				if startIdx < 0 {
					startIdx = 0
				}
			}
		}

		// Render visible works with full details
		for i := startIdx; i < endIdx; i++ {
			work := m.workTiles[i]
			if work == nil {
				continue
			}

			isSelected := work.work.ID == m.selectedWorkTileID

			// === Line 1: Main info ===
			var line1 strings.Builder

			// Selection indicator
			if isSelected {
				line1.WriteString(tuiSuccessStyle.Render("►"))
			} else {
				line1.WriteString(" ")
			}
			line1.WriteString(" ")

			// Status icon
			line1.WriteString(statusIcon(work.work.Status))
			line1.WriteString(" ")

			// Work ID
			idStyle := lipgloss.NewStyle().Bold(true)
			if isSelected {
				idStyle = idStyle.Foreground(lipgloss.Color("214"))
			}
			line1.WriteString(idStyle.Render(work.work.ID))
			line1.WriteString(" ")

			// Friendly name (if set)
			if work.work.Name != "" {
				nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
				line1.WriteString(nameStyle.Render(work.work.Name))
				line1.WriteString(" ")
			}

			// Status text
			statusTextStyle := lipgloss.NewStyle()
			switch work.work.Status {
			case db.StatusCompleted:
				statusTextStyle = statusTextStyle.Foreground(lipgloss.Color("82")) // Green
			case db.StatusProcessing:
				statusTextStyle = statusTextStyle.Foreground(lipgloss.Color("214")) // Orange
			case db.StatusFailed:
				statusTextStyle = statusTextStyle.Foreground(lipgloss.Color("196")) // Red
			default:
				statusTextStyle = statusTextStyle.Foreground(lipgloss.Color("247")) // Gray
			}
			line1.WriteString(statusTextStyle.Render(fmt.Sprintf("[%s]", work.work.Status)))
			line1.WriteString(" ")

			// Created time (if available)
			if work.work.CreatedAt.Unix() > 0 {
				timeAgo := time.Since(work.work.CreatedAt)
				var timeStr string
				if timeAgo.Hours() < 1 {
					timeStr = fmt.Sprintf("%dm ago", int(timeAgo.Minutes()))
				} else if timeAgo.Hours() < 24 {
					timeStr = fmt.Sprintf("%dh ago", int(timeAgo.Hours()))
				} else {
					days := int(timeAgo.Hours() / 24)
					timeStr = fmt.Sprintf("%dd ago", days)
				}
				timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
				line1.WriteString(timeStyle.Render(fmt.Sprintf("Created %s", timeStr)))
			}

			content.WriteString(line1.String())
			content.WriteString("\n")

			// === Line 2: Branch and progress ===
			var line2 strings.Builder
			line2.WriteString("   ")  // Indent

			// Branch name
			branchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
			branch := truncateString(work.work.BranchName, 50)
			line2.WriteString(branchStyle.Render(fmt.Sprintf("📌 %s", branch)))
			line2.WriteString("  ")

			// Progress percentage
			completedTasks := 0
			for _, task := range work.tasks {
				if task.task.Status == db.StatusCompleted {
					completedTasks++
				}
			}

			percentage := 0
			if len(work.tasks) > 0 {
				percentage = (completedTasks * 100) / len(work.tasks)
			}

			progressStyle := lipgloss.NewStyle().Bold(true)
			if percentage == 100 {
				progressStyle = progressStyle.Foreground(lipgloss.Color("82")) // Green
			} else if percentage >= 75 {
				progressStyle = progressStyle.Foreground(lipgloss.Color("226")) // Yellow
			} else if percentage >= 50 {
				progressStyle = progressStyle.Foreground(lipgloss.Color("214")) // Orange
			} else {
				progressStyle = progressStyle.Foreground(lipgloss.Color("247")) // Gray
			}
			line2.WriteString(progressStyle.Render(fmt.Sprintf("%d%%", percentage)))
			line2.WriteString(" ")
			line2.WriteString(fmt.Sprintf("(%d/%d tasks)", completedTasks, len(work.tasks)))

			// Warnings/alerts
			if work.unassignedBeadCount > 0 {
				warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
				line2.WriteString(" ")
				line2.WriteString(warningStyle.Render(fmt.Sprintf("⚠ %d unassigned", work.unassignedBeadCount)))
			}
			if work.feedbackCount > 0 {
				alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
				line2.WriteString(" ")
				line2.WriteString(alertStyle.Render(fmt.Sprintf("🔔 %d feedback", work.feedbackCount)))
			}

			content.WriteString(line2.String())
			content.WriteString("\n")

			// === Line 3: Root issue details ===
			if work.work.RootIssueID != "" {
				var line3 strings.Builder
				line3.WriteString("   ")  // Indent

				// Try to find the root issue in beads
				var rootTitle string
				for _, bead := range work.workBeads {
					if bead.id == work.work.RootIssueID {
						rootTitle = bead.title
						break
					}
				}

				if rootTitle == "" && len(work.workBeads) > 0 {
					// Fallback to first bead if root not found
					rootTitle = work.workBeads[0].title
				}

				if rootTitle != "" {
					issueStyle := lipgloss.NewStyle().
						Foreground(lipgloss.Color("247")).
						Italic(true)
					rootTitle = truncateString(rootTitle, 70)
					line3.WriteString(issueStyle.Render(fmt.Sprintf("📋 %s", rootTitle)))
				} else {
					// Just show the root issue ID
					issueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
					line3.WriteString(issueStyle.Render(fmt.Sprintf("Root: %s", work.work.RootIssueID)))
				}
				content.WriteString(line3.String())
				content.WriteString("\n")
			} else {
				// Add blank line for spacing
				content.WriteString("\n")
			}
		}

		// Show scroll indicator if needed
		if len(m.workTiles) > worksPerPage {
			scrollInfo := fmt.Sprintf("\n  (Showing %d-%d of %d works)", startIdx+1, endIdx, len(m.workTiles))
			content.WriteString(tuiDimStyle.Render(scrollInfo))
		}
	}

	return dropdownStyle.Render(content.String())
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

