package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Dialog update handlers

// updateBeadForm handles input for create, add-child, and edit bead dialogs.
// Delegates to beadFormPanel.Update() and handles resulting actions.
func (m *planModel) updateBeadForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, action := m.beadFormPanel.Update(msg)

	switch action {
	case BeadFormActionCancel:
		m.viewMode = ViewNormal
		return m, cmd

	case BeadFormActionSubmit:
		result := m.beadFormPanel.GetResult()
		if result.Title == "" {
			return m, cmd
		}

		m.viewMode = ViewNormal
		m.beadFormPanel.Blur()

		// Determine mode and call appropriate action
		if result.EditBeadID != "" {
			// Edit mode
			return m, m.saveBeadEdit(result.EditBeadID, result.Title, result.Description, result.BeadType)
		}

		// Create or add-child mode
		isEpic := result.BeadType == "epic"
		return m, m.createBead(result.Title, result.BeadType, result.Priority, isEpic, result.Description, result.ParentID)
	}

	return m, cmd
}

// submitBeadForm handles form submission for create, add-child, and edit modes
// Deprecated: Use updateBeadForm which delegates to panel.Update()
func (m *planModel) submitBeadForm() (tea.Model, tea.Cmd) {
	result := m.beadFormPanel.GetResult()
	if result.Title == "" {
		return m, nil
	}

	m.viewMode = ViewNormal
	m.beadFormPanel.Blur()

	// Determine mode and call appropriate action
	if result.EditBeadID != "" {
		// Edit mode
		return m, m.saveBeadEdit(result.EditBeadID, result.Title, result.Description, result.BeadType)
	}

	// Create or add-child mode
	isEpic := result.BeadType == "epic"
	return m, m.createBead(result.Title, result.BeadType, result.Priority, isEpic, result.Description, result.ParentID)
}

func (m *planModel) updateBeadSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Esc or Ctrl+G cancels search and clears filter
	if msg.Type == tea.KeyEsc || msg.String() == "esc" || msg.String() == "escape" || msg.String() == "ctrl+g" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.filters.searchText = ""
		m.searchSeq++ // Increment to invalidate any in-flight searches
		return m, m.refreshData()
	}
	switch msg.String() {
	case "enter":
		// Confirm search and exit search mode, keeping the filter
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.filters.searchText = m.textInput.Value()
		return m, nil // No need to refresh, already filtered incrementally
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		// Apply incremental filtering as user types
		prevSearch := m.filters.searchText
		m.filters.searchText = m.textInput.Value()
		if m.filters.searchText != prevSearch {
			m.beadsCursor = 0 // Reset cursor when search changes
			m.searchSeq++     // Increment to invalidate any in-flight searches
			// Trigger data refresh to apply filter
			return m, tea.Batch(cmd, m.refreshData())
		}
		return m, cmd
	}
}

func (m *planModel) updateLabelFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc || msg.String() == "esc" || msg.String() == "escape" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		return m, nil
	}
	switch msg.String() {
	case "enter":
		m.viewMode = ViewNormal
		m.filters.label = m.textInput.Value()
		return m, m.refreshData()
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m *planModel) updateCloseBeadConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc || msg.String() == "esc" || msg.String() == "escape" {
		m.viewMode = ViewNormal
		return m, nil
	}
	switch msg.String() {
	case "y", "Y":
		// Collect selected beads
		var beadIDs []string
		for _, item := range m.beadItems {
			if m.selectedBeads[item.ID] {
				beadIDs = append(beadIDs, item.ID)
			}
		}

		// If no selected beads, use cursor bead
		if len(beadIDs) == 0 && len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			beadIDs = append(beadIDs, m.beadItems[m.beadsCursor].ID)
		}

		m.viewMode = ViewNormal
		if len(beadIDs) == 1 {
			// Single bead - use the existing closeBead function
			return m, m.closeBead(beadIDs[0])
		} else if len(beadIDs) > 1 {
			// Multiple beads - use the batch close function
			return m, m.closeBeads(beadIDs)
		}
		return m, nil
	case "n", "N":
		m.viewMode = ViewNormal
		return m, nil
	}
	return m, nil
}

// Dialog render helpers

func (m *planModel) renderLabelFilterDialogContent() string {
	currentLabel := m.filters.label
	if currentLabel == "" {
		currentLabel = "(none)"
	}

	content := fmt.Sprintf(`
  Filter by Label

  Current: %s

  Enter label name (empty to clear):
  %s

  [Enter] Apply  [Esc] Cancel
`, currentLabel, m.textInput.View())

	return tuiDialogStyle.Render(content)
}

func (m *planModel) renderCloseBeadConfirmContent() string {
	// Collect selected beads
	var selectedBeads []beadItem
	for _, item := range m.beadItems {
		if m.selectedBeads[item.ID] {
			selectedBeads = append(selectedBeads, item)
		}
	}

	// If no selected beads, use cursor bead
	if len(selectedBeads) == 0 && len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		selectedBeads = append(selectedBeads, m.beadItems[m.beadsCursor])
	}

	// Build the confirmation message
	var beadsList string
	if len(selectedBeads) == 1 {
		beadsList = fmt.Sprintf("  %s\n  %s", selectedBeads[0].ID, selectedBeads[0].Title)
	} else if len(selectedBeads) > 1 {
		beadsList = fmt.Sprintf("  %d issues:\n", len(selectedBeads))
		for i, bead := range selectedBeads {
			if i < 5 { // Show first 5 beads
				beadsList += fmt.Sprintf("  - %s: %s\n", bead.ID, bead.Title)
			}
		}
		if len(selectedBeads) > 5 {
			beadsList += fmt.Sprintf("  ... and %d more", len(selectedBeads)-5)
		}
	}

	var title string
	if len(selectedBeads) == 1 {
		title = "Close Issue"
	} else {
		title = fmt.Sprintf("Close %d Issues", len(selectedBeads))
	}

	content := fmt.Sprintf(`
  %s

  Are you sure you want to close:
%s

  [y] Yes  [n] No
`, title, beadsList)

	return tuiDialogStyle.Render(content)
}

// updateLinearImportInline handles input for the Linear import inline form
func (m *planModel) updateLinearImportInline(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, action := m.linearImportPanel.Update(msg)

	switch action {
	case LinearImportActionCancel:
		m.viewMode = ViewNormal
		return m, cmd

	case LinearImportActionSubmit:
		result := m.linearImportPanel.GetResult()
		if result.IssueIDs != "" {
			m.viewMode = ViewNormal
			m.linearImportPanel.SetImporting(true)
			return m, m.importLinearIssue(result.IssueIDs)
		}
		return m, cmd
	}

	return m, cmd
}

// updateCreateWorkDialog handles input for the create work dialog.
// Delegates to createWorkPanel.Update() and handles resulting actions.
func (m *planModel) updateCreateWorkDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, action := m.createWorkPanel.Update(msg)

	switch action {
	case CreateWorkActionCancel:
		m.viewMode = ViewNormal
		return m, cmd

	case CreateWorkActionExecute:
		result := m.createWorkPanel.GetResult()
		if result.BranchName == "" {
			m.statusMessage = "Branch name cannot be empty"
			m.statusIsError = true
			return m, nil
		}
		m.viewMode = ViewNormal
		// Clear selections after work creation
		m.selectedBeads = make(map[string]bool)
		return m, m.executeCreateWork(result.BeadIDs, result.BranchName, false)

	case CreateWorkActionAuto:
		result := m.createWorkPanel.GetResult()
		if result.BranchName == "" {
			m.statusMessage = "Branch name cannot be empty"
			m.statusIsError = true
			return m, nil
		}
		m.viewMode = ViewNormal
		// Clear selections after work creation
		m.selectedBeads = make(map[string]bool)
		return m, m.executeCreateWork(result.BeadIDs, result.BranchName, true)
	}

	return m, cmd
}

// updateWorkOverlay handles input when in work overlay mode.
// Delegates to workOverlay.Update() and handles resulting actions.
func (m *planModel) updateWorkOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle navigation for non-overlay focused state
	if !m.overlayFocused {
		switch msg.String() {
		case "j", "down":
			if m.beadsCursor < len(m.beadItems)-1 {
				m.beadsCursor++
			}
			return m, nil
		case "k", "up":
			if m.beadsCursor > 0 {
				m.beadsCursor--
			}
			return m, nil
		case "tab":
			m.overlayFocused = true
			return m, nil
		case "esc":
			m.viewMode = ViewNormal
			m.selectedWorkTileID = ""
			m.overlayFocused = false
			return m, nil
		}
		return m, nil
	}

	cmd, action := m.workOverlay.Update(msg)

	switch action {
	case WorkOverlayActionCancel:
		m.viewMode = ViewNormal
		m.selectedWorkTileID = ""
		m.overlayFocused = false
		return m, cmd

	case WorkOverlayActionSelect:
		// Set the focused work and return to normal view with split screen
		m.focusedWorkID = m.workOverlay.GetSelectedWorkTileID()
		m.viewMode = ViewNormal
		m.overlayFocused = false
		m.statusMessage = fmt.Sprintf("Focused on work %s", m.focusedWorkID)
		m.statusIsError = false
		// Reset focus filter when selecting a new work
		m.focusFilterActive = false
		return m, cmd

	case WorkOverlayActionToggleFocus:
		m.overlayFocused = !m.overlayFocused
		return m, cmd

	case WorkOverlayActionCreate:
		// Create new work - exit overlay and show create dialog
		if m.beadsCursor < len(m.beadItems) {
			selectedBead := m.beadItems[m.beadsCursor]
			// Generate initial branch name
			beads := []*beadsForBranch{{ID: selectedBead.ID, Title: selectedBead.Title}}
			initialBranch := generateBranchNameFromBeadsForBranch(beads)
			m.createWorkPanel.Reset([]string{selectedBead.ID}, initialBranch)
			m.viewMode = ViewCreateWork
			m.selectedWorkTileID = ""
		}
		return m, cmd

	case WorkOverlayActionDestroy:
		workID := m.workOverlay.GetSelectedWorkTileID()
		if workID != "" {
			m.statusMessage = fmt.Sprintf("Destroying work %s...", workID)
			m.statusIsError = false
			return m, m.destroyWork(workID)
		}
		return m, cmd

	case WorkOverlayActionPlan:
		workID := m.workOverlay.GetSelectedWorkTileID()
		if workID != "" {
			m.statusMessage = fmt.Sprintf("Planning work %s...", workID)
			m.statusIsError = false
			m.viewMode = ViewNormal
			return m, m.planWork(workID)
		}
		return m, cmd

	case WorkOverlayActionRun:
		workID := m.workOverlay.GetSelectedWorkTileID()
		if workID != "" {
			m.statusMessage = fmt.Sprintf("Running work %s...", workID)
			m.statusIsError = false
			m.viewMode = ViewNormal
			return m, m.runWork(workID)
		}
		return m, cmd
	}

	// Sync selected work tile ID from panel to model
	m.selectedWorkTileID = m.workOverlay.GetSelectedWorkTileID()

	return m, cmd
}
