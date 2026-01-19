package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Dialog rendering functions

// renderWithDialog places a dialog in the center of the screen
func (m *workModel) renderWithDialog(dialog string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

// renderDestroyConfirmDialog renders the destroy work confirmation dialog
func (m *workModel) renderDestroyConfirmDialog() string {
	workID := ""
	workerName := ""
	if len(m.works) > 0 && m.worksCursor < len(m.works) {
		workID = m.works[m.worksCursor].work.ID
		workerName = m.works[m.worksCursor].work.Name
	}

	displayName := workID
	if workerName != "" {
		displayName = fmt.Sprintf("%s (%s)", workerName, workID)
	}

	content := fmt.Sprintf(`
  Destroy Work

  Are you sure you want to destroy %s?
  This will remove the worktree and all task data.

  [y] Yes  [n] No
`, displayName)

	return tuiDialogStyle.Render(content)
}

// renderCreateBeadDialogContent renders the create bead dialog content
func (m *workModel) renderCreateBeadDialogContent() string {
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

	// Show which work the bead will be assigned to
	workInfo := ""
	if len(m.works) > 0 && m.worksCursor < len(m.works) {
		wp := m.works[m.worksCursor]
		displayName := wp.work.ID
		if wp.work.Name != "" {
			displayName = fmt.Sprintf("%s (%s)", wp.work.Name, wp.work.ID)
		}
		workInfo = fmt.Sprintf("\n  Assign to: %s", tuiValueStyle.Render(displayName))
	}

	content := fmt.Sprintf(`  Create New Issue%s

  %s
  %s

  %s %s
  %s %s

  %s
%s

  [Tab] Next field  [Enter] Create  [Esc] Cancel
`, workInfo, titleLabel, m.textInput.View(), typeLabel, typeDisplay, priorityLabel, priorityDisplay, descLabel, indentLines(m.createDescTextarea.View(), "  "))

	return tuiDialogStyle.Render(content)
}


// renderHelp renders the help dialog
func (m *workModel) renderHelp() string {
	help := `
  Work Mode - Help

  View States
  ────────────────────────────
  Overview      Grid of all workers (default)
  Zoomed        3-panel task view for selected work

  Navigation (Overview/Grid)
  ────────────────────────────
  h/l, ←/→      Move between grid cells
  j/k, ↑/↓      Move up/down in grid
  Enter         Zoom into selected work
  g             Go to first work
  G             Go to last work

  Navigation (Zoomed/3-Panel)
  ────────────────────────────
  h/l, ←/→      Move between panels
  j/k, ↑/↓      Navigate list
  Tab           Cycle panels
  Esc           Zoom out to overview

  Work Management (Zoomed Mode)
  ────────────────────────────
  n             Create new issue (assign to work)
  a             Assign existing issues to work
  r             Run work with plan (LLM estimates)
  s             Run work simple (no planning)
  x             Remove selected unassigned issue
  t             Open terminal/console tab
  c             Open Claude Code session
  o             Restart orchestrator
  v             Create review task
  p             Create PR task
  u             Update PR description
  f             Poll PR feedback (if PR exists)
  d             Destroy selected work
  Shift+R       Reset failed task to pending

  General
  ────────────────────────────
  ?             Show this help

  Press any key to close...
`
	return tuiHelpStyle.Width(m.width).Height(m.height).Render(help)
}