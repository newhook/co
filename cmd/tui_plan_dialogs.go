package cmd

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Dialog update handlers

func (m *planModel) updateCreateBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check escape/cancel keys
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.createDescTextarea.Blur()
		return m, nil
	}

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> description(3) -> title(0)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		m.createDialogFocus = (m.createDialogFocus + 1) % 4
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
			m.createDescTextarea.Blur()
		} else if m.createDialogFocus == 3 {
			m.textInput.Blur()
			m.createDescTextarea.Focus()
		} else {
			m.textInput.Blur()
			m.createDescTextarea.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.createDialogFocus--
		if m.createDialogFocus < 0 {
			m.createDialogFocus = 3
		}
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
			m.createDescTextarea.Blur()
		} else if m.createDialogFocus == 3 {
			m.textInput.Blur()
			m.createDescTextarea.Focus()
		} else {
			m.textInput.Blur()
			m.createDescTextarea.Blur()
		}
		return m, nil
	}

	// Enter submits from any field (but not from description textarea - use Ctrl+Enter there)
	if msg.String() == "enter" && m.createDialogFocus != 3 {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			beadType := beadTypes[m.createBeadType]
			isEpic := beadType == "epic"
			description := strings.TrimSpace(m.createDescTextarea.Value())
			m.viewMode = ViewNormal
			m.createDescTextarea.Reset()
			return m, m.createBead(title, beadType, m.createBeadPriority, isEpic, description)
		}
		return m, nil
	}

	// Ctrl+Enter submits from description textarea
	if msg.String() == "ctrl+enter" && m.createDialogFocus == 3 {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			beadType := beadTypes[m.createBeadType]
			isEpic := beadType == "epic"
			description := strings.TrimSpace(m.createDescTextarea.Value())
			m.viewMode = ViewNormal
			m.createDescTextarea.Reset()
			return m, m.createBead(title, beadType, m.createBeadPriority, isEpic, description)
		}
		return m, nil
	}

	// Handle input based on focused element
	switch m.createDialogFocus {
	case 0: // Title input
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd

	case 1: // Type selector
		switch msg.String() {
		case "j", "down":
			m.createBeadType = (m.createBeadType + 1) % len(beadTypes)
		case "k", "up":
			m.createBeadType--
			if m.createBeadType < 0 {
				m.createBeadType = len(beadTypes) - 1
			}
		}
		return m, nil

	case 2: // Priority
		switch msg.String() {
		case "j", "down", "-":
			if m.createBeadPriority < 4 {
				m.createBeadPriority++
			}
		case "k", "up", "+", "=":
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

func (m *planModel) updateCreateBeadInline(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check escape/cancel keys
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.createDescTextarea.Blur()
		return m, nil
	}

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> description(3) -> title(0)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		m.createDialogFocus = (m.createDialogFocus + 1) % 4
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
			m.createDescTextarea.Blur()
		} else if m.createDialogFocus == 3 {
			m.textInput.Blur()
			m.createDescTextarea.Focus()
		} else {
			m.textInput.Blur()
			m.createDescTextarea.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.createDialogFocus--
		if m.createDialogFocus < 0 {
			m.createDialogFocus = 3
		}
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
			m.createDescTextarea.Blur()
		} else if m.createDialogFocus == 3 {
			m.textInput.Blur()
			m.createDescTextarea.Focus()
		} else {
			m.textInput.Blur()
			m.createDescTextarea.Blur()
		}
		return m, nil
	}

	// Enter submits from any field (but not from description textarea - use Ctrl+Enter there)
	if msg.String() == "enter" && m.createDialogFocus != 3 {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			beadType := beadTypes[m.createBeadType]
			isEpic := beadType == "epic"
			description := strings.TrimSpace(m.createDescTextarea.Value())
			m.viewMode = ViewNormal
			m.createDescTextarea.Reset()
			return m, m.createBead(title, beadType, m.createBeadPriority, isEpic, description)
		}
		return m, nil
	}

	// Ctrl+Enter submits from description textarea
	if msg.String() == "ctrl+enter" && m.createDialogFocus == 3 {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			beadType := beadTypes[m.createBeadType]
			isEpic := beadType == "epic"
			description := strings.TrimSpace(m.createDescTextarea.Value())
			m.viewMode = ViewNormal
			m.createDescTextarea.Reset()
			return m, m.createBead(title, beadType, m.createBeadPriority, isEpic, description)
		}
		return m, nil
	}

	// Handle input based on focused element
	switch m.createDialogFocus {
	case 0: // Title input
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd

	case 1: // Type selector
		switch msg.String() {
		case "j", "down":
			m.createBeadType = (m.createBeadType + 1) % len(beadTypes)
		case "k", "up":
			m.createBeadType--
			if m.createBeadType < 0 {
				m.createBeadType = len(beadTypes) - 1
			}
		}
		return m, nil

	case 2: // Priority
		switch msg.String() {
		case "j", "down", "-":
			if m.createBeadPriority < 4 {
				m.createBeadPriority++
			}
		case "k", "up", "+", "=":
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

func (m *planModel) updateEditBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.editTitleTextarea.Blur()
		m.editDescTextarea.Blur()
		m.editBeadID = ""
		return m, nil
	}

	// Tab cycles between title(0), type(1), description(2), buttons(3)
	if msg.Type == tea.KeyTab {
		m.editField = (m.editField + 1) % 4
		m.editTitleTextarea.Blur()
		m.editDescTextarea.Blur()
		if m.editField == 0 {
			m.editTitleTextarea.Focus()
		} else if m.editField == 2 {
			m.editDescTextarea.Focus()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.editField--
		if m.editField < 0 {
			m.editField = 3
		}
		m.editTitleTextarea.Blur()
		m.editDescTextarea.Blur()
		if m.editField == 0 {
			m.editTitleTextarea.Focus()
		} else if m.editField == 2 {
			m.editDescTextarea.Focus()
		}
		return m, nil
	}

	// Handle input based on focused field
	var cmd tea.Cmd
	switch m.editField {
	case 0: // Title
		m.editTitleTextarea, cmd = m.editTitleTextarea.Update(msg)
	case 1: // Type selector
		switch msg.String() {
		case "j", "down", "h", "left":
			m.editBeadType--
			if m.editBeadType < 0 {
				m.editBeadType = len(beadTypes) - 1
			}
		case "k", "up", "l", "right":
			m.editBeadType = (m.editBeadType + 1) % len(beadTypes)
		}
	case 2: // Description
		m.editDescTextarea, cmd = m.editDescTextarea.Update(msg)
	case 3: // Buttons
		switch msg.String() {
		case "h", "left", "j", "k", "up", "down", "l", "right":
			// Toggle between OK(0) and Cancel(1)
			m.editButtonIdx = 1 - m.editButtonIdx
		case "enter":
			if m.editButtonIdx == 0 {
				// OK - save
				title := strings.TrimSpace(m.editTitleTextarea.Value())
				desc := strings.TrimSpace(m.editDescTextarea.Value())
				beadType := beadTypes[m.editBeadType]
				if title != "" {
					m.viewMode = ViewNormal
					return m, m.saveBeadEdit(m.editBeadID, title, desc, beadType)
				}
			} else {
				// Cancel
				m.viewMode = ViewNormal
				m.editTitleTextarea.Blur()
				m.editDescTextarea.Blur()
				m.editBeadID = ""
			}
			return m, nil
		}
	}
	return m, cmd
}

// updateAddChildBead handles input for the add child bead dialog
func (m *planModel) updateAddChildBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.parentBeadID = ""
		return m, nil
	}

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> title(0)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		m.createDialogFocus = (m.createDialogFocus + 1) % 3
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else {
			m.textInput.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.createDialogFocus--
		if m.createDialogFocus < 0 {
			m.createDialogFocus = 2
		}
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else {
			m.textInput.Blur()
		}
		return m, nil
	}

	// Enter submits from any field
	if msg.String() == "enter" {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			m.viewMode = ViewNormal
			return m, m.createChildBead(title, beadTypes[m.createBeadType], m.createBeadPriority)
		}
		return m, nil
	}

	// Handle input based on focused element
	switch m.createDialogFocus {
	case 0: // Title input
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd

	case 1: // Type selector
		switch msg.String() {
		case "j", "down":
			m.createBeadType = (m.createBeadType + 1) % len(beadTypes)
		case "k", "up":
			m.createBeadType--
			if m.createBeadType < 0 {
				m.createBeadType = len(beadTypes) - 1
			}
		}
		return m, nil

	case 2: // Priority
		switch msg.String() {
		case "j", "down", "-":
			if m.createBeadPriority < 4 {
				m.createBeadPriority++
			}
		case "k", "up", "+", "=":
			if m.createBeadPriority > 0 {
				m.createBeadPriority--
			}
		}
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

func (m *planModel) renderCreateBeadDialogContent() string {
	typeFocused := m.createDialogFocus == 1
	priorityFocused := m.createDialogFocus == 2
	descFocused := m.createDialogFocus == 3

	// Type rotator display
	currentType := beadTypes[m.createBeadType]
	var typeDisplay string
	if typeFocused {
		typeDisplay = fmt.Sprintf("< %s >", tuiValueStyle.Render(currentType))
	} else {
		typeDisplay = typeFeatureStyle.Render(currentType)
	}

	// Priority display
	priorityLabels := []string{"P0 (critical)", "P1 (high)", "P2 (medium)", "P3 (low)", "P4 (backlog)"}
	var priorityDisplay string
	if priorityFocused {
		priorityDisplay = fmt.Sprintf("< %s >", tuiValueStyle.Render(priorityLabels[m.createBeadPriority]))
	} else {
		priorityDisplay = priorityLabels[m.createBeadPriority]
	}

	// Show focus labels
	titleLabel := "Title:"
	typeLabel := "Type:"
	priorityLabel := "Priority:"
	descLabel := "Description:"
	if m.createDialogFocus == 0 {
		titleLabel = tuiValueStyle.Render("Title:") + " (editing)"
	}
	if typeFocused {
		typeLabel = tuiValueStyle.Render("Type:") + " (j/k)"
	}
	if priorityFocused {
		priorityLabel = tuiValueStyle.Render("Priority:") + " (j/k)"
	}
	if descFocused {
		descLabel = tuiValueStyle.Render("Description:") + " (optional)"
	}

	content := fmt.Sprintf(`  Create New Issue

  %s
  %s

  %s %s
  %s %s

  %s
%s

  [Tab] Next field  [Enter] Create  [Esc] Cancel
`, titleLabel, m.textInput.View(), typeLabel, typeDisplay, priorityLabel, priorityDisplay, descLabel, indentLines(m.createDescTextarea.View(), "  "))

	return tuiDialogStyle.Render(content)
}

func (m *planModel) renderEditBeadDialogContent() string {
	// Show focus labels
	titleLabel := "Title:"
	typeLabel := "Type:"
	descLabel := "Description:"

	switch m.editField {
	case 0:
		titleLabel = tuiValueStyle.Render("Title:") + " (editing)"
	case 1:
		typeLabel = tuiValueStyle.Render("Type:") + " (←/→)"
	case 2:
		descLabel = tuiValueStyle.Render("Description:") + " (editing)"
	}

	// Type rotator display
	currentType := beadTypes[m.editBeadType]
	var typeDisplay string
	if m.editField == 1 {
		typeDisplay = fmt.Sprintf("< %s >", tuiValueStyle.Render(currentType))
	} else {
		typeDisplay = typeFeatureStyle.Render(currentType)
	}

	// Render OK/Cancel buttons
	var okBtn, cancelBtn string
	if m.editField == 3 {
		if m.editButtonIdx == 0 {
			okBtn = tuiSelectedStyle.Render(" OK ")
			cancelBtn = tuiDimStyle.Render(" Cancel ")
		} else {
			okBtn = tuiDimStyle.Render(" OK ")
			cancelBtn = tuiSelectedStyle.Render(" Cancel ")
		}
	} else {
		okBtn = tuiDimStyle.Render(" OK ")
		cancelBtn = tuiDimStyle.Render(" Cancel ")
	}

	content := fmt.Sprintf(`  Edit Issue %s

  %s
%s

  %s %s

  %s
%s

  %s  %s

  [Tab] Switch field  [Esc] Cancel
`, issueIDStyle.Render(m.editBeadID), titleLabel, indentLines(m.editTitleTextarea.View(), "  "), typeLabel, typeDisplay, descLabel, indentLines(m.editDescTextarea.View(), "  "), okBtn, cancelBtn)

	return tuiDialogStyle.Render(content)
}

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

// renderAddChildBeadDialogContent renders the add child bead dialog
func (m *planModel) renderAddChildBeadDialogContent() string {
	typeFocused := m.createDialogFocus == 1
	priorityFocused := m.createDialogFocus == 2

	// Type rotator display
	currentType := beadTypes[m.createBeadType]
	var typeDisplay string
	if typeFocused {
		typeDisplay = fmt.Sprintf("< %s >", tuiValueStyle.Render(currentType))
	} else {
		typeDisplay = typeFeatureStyle.Render(currentType)
	}

	// Priority display
	priorityLabels := []string{"P0 (critical)", "P1 (high)", "P2 (medium)", "P3 (low)", "P4 (backlog)"}
	var priorityDisplay string
	if priorityFocused {
		priorityDisplay = fmt.Sprintf("< %s >", tuiValueStyle.Render(priorityLabels[m.createBeadPriority]))
	} else {
		priorityDisplay = priorityLabels[m.createBeadPriority]
	}

	// Show focus labels
	titleLabel := "Title:"
	typeLabel := "Type:"
	priorityLabel := "Priority:"
	if m.createDialogFocus == 0 {
		titleLabel = tuiValueStyle.Render("Title:") + " (editing)"
	}
	if typeFocused {
		typeLabel = tuiValueStyle.Render("Type:") + " (j/k)"
	}
	if priorityFocused {
		priorityLabel = tuiValueStyle.Render("Priority:") + " (j/k)"
	}

	content := fmt.Sprintf(`  Add Child Issue to %s

  %s
  %s

  %s %s
  %s %s

  The new issue will be blocked by %s.

  [Tab] Next field  [Enter] Create  [Esc] Cancel
`, issueIDStyle.Render(m.parentBeadID), titleLabel, m.textInput.View(), typeLabel, typeDisplay, priorityLabel, priorityDisplay, m.parentBeadID)

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
		m.linearImportFocus = (m.linearImportFocus + 1) % 5
		if m.linearImportFocus == 0 {
			m.linearImportInput.Focus()
		} else {
			m.linearImportInput.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.linearImportFocus--
		if m.linearImportFocus < 0 {
			m.linearImportFocus = 4
		}
		if m.linearImportFocus == 0 {
			m.linearImportInput.Focus()
		} else {
			m.linearImportInput.Blur()
		}
		return m, nil
	}

	// Enter submits from input field
	if msg.String() == "enter" && m.linearImportFocus == 0 {
		issueID := strings.TrimSpace(m.linearImportInput.Value())
		if issueID != "" {
			m.viewMode = ViewNormal
			m.linearImporting = true
			return m, m.importLinearIssue(issueID)
		}
		return m, nil
	}

	// Handle input based on focused element
	var cmd tea.Cmd
	switch m.linearImportFocus {
	case 0: // Input field
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
