package cmd

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Dialog update handlers

// updateBeadForm handles input for create, add-child, and edit bead dialogs.
// The mode is determined by:
//   - editBeadID set → edit mode
//   - parentBeadID set → add child mode
//   - neither set → create mode
func (m *planModel) updateBeadForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check escape/cancel keys
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.createDescTextarea.Blur()
		m.editBeadID = ""
		m.parentBeadID = ""
		return m, nil
	}

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> description(3) -> ok(4) -> cancel(5) -> title(0)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		// Leave current focus
		if m.createDialogFocus == 0 {
			m.textInput.Blur()
		} else if m.createDialogFocus == 3 {
			m.createDescTextarea.Blur()
		}

		m.createDialogFocus = (m.createDialogFocus + 1) % 6

		// Enter new focus
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else if m.createDialogFocus == 3 {
			m.createDescTextarea.Focus()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		// Leave current focus
		if m.createDialogFocus == 0 {
			m.textInput.Blur()
		} else if m.createDialogFocus == 3 {
			m.createDescTextarea.Blur()
		}

		m.createDialogFocus--
		if m.createDialogFocus < 0 {
			m.createDialogFocus = 5
		}

		// Enter new focus
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else if m.createDialogFocus == 3 {
			m.createDescTextarea.Focus()
		}
		return m, nil
	}

	// Enter key handling depends on focused element
	if msg.String() == "enter" {
		switch m.createDialogFocus {
		case 0, 1, 2: // Title, type, or priority - submit form
			return m.submitBeadForm()
		case 4: // Ok button - submit form
			return m.submitBeadForm()
		case 5: // Cancel button - cancel form
			m.viewMode = ViewNormal
			m.textInput.Blur()
			m.createDescTextarea.Blur()
			m.editBeadID = ""
			m.parentBeadID = ""
			return m, nil
		}
		// For description textarea (3), Enter adds a newline (handled below)
	}

	// Ctrl+Enter submits from description textarea
	if msg.String() == "ctrl+enter" && m.createDialogFocus == 3 {
		return m.submitBeadForm()
	}

	// Handle input based on focused element
	switch m.createDialogFocus {
	case 0: // Title input
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd

	case 1: // Type selector
		switch msg.String() {
		case "j", "down", "right":
			m.createBeadType = (m.createBeadType + 1) % len(beadTypes)
		case "k", "up", "left":
			m.createBeadType--
			if m.createBeadType < 0 {
				m.createBeadType = len(beadTypes) - 1
			}
		}
		return m, nil

	case 2: // Priority
		switch msg.String() {
		case "j", "down", "right", "-":
			if m.createBeadPriority < 4 {
				m.createBeadPriority++
			}
		case "k", "up", "left", "+", "=":
			if m.createBeadPriority > 0 {
				m.createBeadPriority--
			}
		}
		return m, nil

	case 3: // Description textarea
		var cmd tea.Cmd
		m.createDescTextarea, cmd = m.createDescTextarea.Update(msg)
		return m, cmd

	case 4: // Ok button - Space can also activate it
		if msg.String() == " " {
			return m.submitBeadForm()
		}
		return m, nil

	case 5: // Cancel button - Space can also activate it
		if msg.String() == " " {
			m.viewMode = ViewNormal
			m.textInput.Blur()
			m.createDescTextarea.Blur()
			m.editBeadID = ""
			m.parentBeadID = ""
			return m, nil
		}
		return m, nil
	}

	return m, nil
}

// submitBeadForm handles form submission for create, add-child, and edit modes
func (m *planModel) submitBeadForm() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.textInput.Value())
	if title == "" {
		return m, nil
	}

	beadType := beadTypes[m.createBeadType]
	description := strings.TrimSpace(m.createDescTextarea.Value())

	m.viewMode = ViewNormal
	m.textInput.Blur()
	m.createDescTextarea.Reset()

	// Determine mode and call appropriate action
	if m.editBeadID != "" {
		// Edit mode
		beadID := m.editBeadID
		m.editBeadID = ""
		return m, m.saveBeadEdit(beadID, title, description, beadType)
	}

	// Create or add-child mode
	isEpic := beadType == "epic"
	parentID := m.parentBeadID
	m.parentBeadID = ""
	return m, m.createBead(title, beadType, m.createBeadPriority, isEpic, description, parentID)
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

// indentLines adds a prefix to each line of a multi-line string.
// This is used to properly align textarea components within dialogs.
func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
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
	// Check escape/cancel keys
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.linearImportInput.Blur()
		return m, nil
	}

	// Tab cycles between elements: input(0) -> createDeps(1) -> update(2) -> dryRun(3) -> maxDepth(4) -> Ok(5) -> Cancel(6)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		// Leave textarea focus before switching
		if m.linearImportFocus == 0 {
			m.linearImportInput.Blur()
		}

		m.linearImportFocus = (m.linearImportFocus + 1) % 7

		// Enter new focus
		if m.linearImportFocus == 0 {
			m.linearImportInput.Focus()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		// Leave textarea focus before switching
		if m.linearImportFocus == 0 {
			m.linearImportInput.Blur()
		}

		m.linearImportFocus--
		if m.linearImportFocus < 0 {
			m.linearImportFocus = 6
		}

		// Enter new focus
		if m.linearImportFocus == 0 {
			m.linearImportInput.Focus()
		}
		return m, nil
	}

	// Ctrl+Enter submits from textarea
	if msg.String() == "ctrl+enter" && m.linearImportFocus == 0 {
		issueIDs := strings.TrimSpace(m.linearImportInput.Value())
		if issueIDs != "" {
			m.viewMode = ViewNormal
			m.linearImporting = true
			return m, m.importLinearIssue(issueIDs)
		}
		return m, nil
	}

	// Enter or Space activates buttons and submits from other fields (but not from textarea - use Ctrl+Enter there)
	if (msg.String() == "enter" || msg.String() == " ") && m.linearImportFocus != 0 {
		// Handle Ok button (focus = 5)
		if m.linearImportFocus == 5 {
			issueIDs := strings.TrimSpace(m.linearImportInput.Value())
			if issueIDs != "" {
				m.viewMode = ViewNormal
				m.linearImporting = true
				return m, m.importLinearIssue(issueIDs)
			}
			return m, nil
		}
		// Handle Cancel button (focus = 6)
		if m.linearImportFocus == 6 {
			m.viewMode = ViewNormal
			m.linearImportInput.Blur()
			return m, nil
		}
		// Only Enter (not space) submits the form from other non-textarea fields
		if msg.String() == "enter" {
			issueIDs := strings.TrimSpace(m.linearImportInput.Value())
			if issueIDs != "" {
				m.viewMode = ViewNormal
				m.linearImporting = true
				return m, m.importLinearIssue(issueIDs)
			}
		}
		return m, nil
	}

	// Handle input based on focused element
	var cmd tea.Cmd
	switch m.linearImportFocus {
	case 0: // Textarea field
		m.linearImportInput, cmd = m.linearImportInput.Update(msg)
		return m, cmd

	case 1: // Create dependencies checkbox
		if msg.String() == " " || msg.String() == "x" {
			m.linearImportCreateDeps = !m.linearImportCreateDeps
		}
		return m, nil

	case 2: // Update existing checkbox
		if msg.String() == " " || msg.String() == "x" {
			m.linearImportUpdate = !m.linearImportUpdate
		}
		return m, nil

	case 3: // Dry run checkbox
		if msg.String() == " " || msg.String() == "x" {
			m.linearImportDryRun = !m.linearImportDryRun
		}
		return m, nil

	case 4: // Max depth
		switch msg.String() {
		case "j", "down", "-":
			if m.linearImportMaxDepth > 1 {
				m.linearImportMaxDepth--
			}
		case "k", "up", "+", "=":
			if m.linearImportMaxDepth < 5 {
				m.linearImportMaxDepth++
			}
		}
		return m, nil
	}

	return m, nil
}
