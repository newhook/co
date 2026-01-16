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

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> description(3) -> title(0)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		// Leave current focus
		if m.createDialogFocus == 0 {
			m.textInput.Blur()
		} else if m.createDialogFocus == 3 {
			m.createDescTextarea.Blur()
		}

		m.createDialogFocus = (m.createDialogFocus + 1) % 4

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
			m.createDialogFocus = 3
		}

		// Enter new focus
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else if m.createDialogFocus == 3 {
			m.createDescTextarea.Focus()
		}
		return m, nil
	}

	// Enter submits from any field (but not from description textarea - use Ctrl+Enter there)
	if msg.String() == "enter" && m.createDialogFocus != 3 {
		return m.submitBeadForm()
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
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			beadID := m.beadItems[m.beadsCursor].id
			m.viewMode = ViewNormal
			return m, m.closeBead(beadID)
		}
		m.viewMode = ViewNormal
		return m, nil
	case "n", "N":
		m.viewMode = ViewNormal
		return m, nil
	}
	return m, nil
}

// updateAddToWork handles input for the add to work dialog
func (m *planModel) updateAddToWork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		return m, nil
	}
	switch msg.String() {
	case "j", "down":
		if m.worksCursor < len(m.availableWorks)-1 {
			m.worksCursor++
		}
		return m, nil
	case "k", "up":
		if m.worksCursor > 0 {
			m.worksCursor--
		}
		return m, nil
	case "enter":
		if len(m.availableWorks) > 0 && m.worksCursor < len(m.availableWorks) {
			workID := m.availableWorks[m.worksCursor].id
			beadID := m.beadItems[m.beadsCursor].id
			return m, m.addBeadToWork(beadID, workID)
		}
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
	beadID := ""
	beadTitle := ""
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID = m.beadItems[m.beadsCursor].id
		beadTitle = m.beadItems[m.beadsCursor].title
	}

	content := fmt.Sprintf(`
  Close Issue

  Are you sure you want to close %s?
  %s

  [y] Yes  [n] No
`, beadID, beadTitle)

	return tuiDialogStyle.Render(content)
}

// renderAddToWorkDialogContent renders the add to work dialog
func (m *planModel) renderAddToWorkDialogContent() string {
	beadID := ""
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID = m.beadItems[m.beadsCursor].id
	}

	var worksList strings.Builder
	if len(m.availableWorks) == 0 {
		worksList.WriteString("  No available works.\n")
	} else {
		for i, work := range m.availableWorks {
			prefix := "  "
			if i == m.worksCursor {
				prefix = "> "
			}
			worksList.WriteString(fmt.Sprintf("%s%s (%s) - %s\n", prefix, work.id, work.status, work.branch))
		}
	}

	content := fmt.Sprintf(`
  Add Issue to Work

  Issue: %s

  Select a work:
%s
  [Enter] Add  [j/k] Navigate  [Esc] Cancel
`, issueIDStyle.Render(beadID), worksList.String())

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

	// Tab cycles between elements: input(0) -> createDeps(1) -> update(2) -> dryRun(3) -> maxDepth(4)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		// Leave textarea focus before switching
		if m.linearImportFocus == 0 {
			m.linearImportInput.Blur()
		}

		m.linearImportFocus = (m.linearImportFocus + 1) % 5

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
			m.linearImportFocus = 4
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

	// Enter submits from other fields (but not from textarea - use Ctrl+Enter there)
	if msg.String() == "enter" && m.linearImportFocus != 0 {
		// Submit the form from non-textarea fields
		issueIDs := strings.TrimSpace(m.linearImportInput.Value())
		if issueIDs != "" {
			m.viewMode = ViewNormal
			m.linearImporting = true
			return m, m.importLinearIssue(issueIDs)
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
