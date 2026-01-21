package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Dialog rendering methods for tuiModel
// These methods render modal dialogs for various user interactions

func (m tuiModel) renderWithDialog(dialog string) string {
	// Center the dialog on screen
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m tuiModel) renderCreateWorkDialog() string {
	// Count selected beads
	selectedCount := 0
	for _, selected := range m.selectedBeads {
		if selected {
			selectedCount++
		}
	}

	var beadOption string
	if selectedCount > 0 {
		beadOption = fmt.Sprintf("\n  [b] Use %d selected bead(s) to auto-generate branch", selectedCount)
	} else {
		beadOption = "\n  (Select beads in Beads panel to use auto-generate)"
	}

	content := fmt.Sprintf(`
  Create New Work

  Enter branch name:
  %s
%s

  [Enter] Create  [Esc] Cancel
`, m.textInput.View(), beadOption)

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderCreateBeadDialog() string {
	// Build type selector
	var typeOptions []string
	for i, t := range beadTypes {
		if i == m.createBeadType {
			typeOptions = append(typeOptions, fmt.Sprintf("[%s]", t))
		} else {
			typeOptions = append(typeOptions, fmt.Sprintf(" %s ", t))
		}
	}
	typeSelector := strings.Join(typeOptions, " ")

	// Build priority display
	priorityLabels := []string{"P0 (critical)", "P1 (high)", "P2 (medium)", "P3 (low)", "P4 (backlog)"}
	priorityDisplay := priorityLabels[m.createBeadPriority]

	content := fmt.Sprintf(`
  Create New Bead

  Title:
  %s

  Type (Tab to cycle):    %s
  Priority (+/- to adjust): %s

  [Enter] Create  [Esc] Cancel
`, m.textInput.View(), typeSelector, priorityDisplay)

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderDestroyConfirmDialog() string {
	workID := ""
	if len(m.works) > 0 {
		workID = m.works[m.worksCursor].work.ID
	}

	content := fmt.Sprintf(`
  Destroy Work

  Are you sure you want to destroy work %s?
  This will remove the worktree and all task data.

  [y] Yes  [n] No
`, workID)

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderPlanDialog() string {
	content := `
  Plan Work

  Choose planning mode:

  [a] Auto-group - LLM estimates complexity
  [s] Single-bead - One task per bead

  [Esc] Cancel
`

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderCreateEpicDialog() string {
	// Build priority display
	priorityLabels := []string{"P0 (critical)", "P1 (high)", "P2 (medium)", "P3 (low)", "P4 (backlog)"}
	priorityDisplay := priorityLabels[m.createBeadPriority]

	content := fmt.Sprintf(`
  Create New Epic

  Title:
  %s

  Type: feature (fixed for epics)
  Priority (+/- to adjust): %s

  [Enter] Create  [Esc] Cancel
`, m.textInput.View(), priorityDisplay)

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderCloseBeadConfirmDialog() string {
	beadID := ""
	beadTitle := ""
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID = m.beadItems[m.beadsCursor].ID
		beadTitle = m.beadItems[m.beadsCursor].Title
	}

	content := fmt.Sprintf(`
  Close Bead

  Are you sure you want to close %s?
  %s

  [y] Yes  [n] No
`, beadID, beadTitle)

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderBeadSearchDialog() string {
	content := fmt.Sprintf(`
  Search Beads

  Enter search text (searches ID, title, description):
  %s

  [Enter] Search  [Esc] Cancel (clears search)
`, m.textInput.View())

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderLabelFilterDialog() string {
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

func (m tuiModel) renderAssignBeadsView() string {
	var b strings.Builder

	b.WriteString(tuiTitleStyle.Render("Assign Beads to Work"))
	b.WriteString("\n\n")

	if len(m.works) > 0 {
		b.WriteString(tuiLabelStyle.Render("Target Work: "))
		b.WriteString(tuiValueStyle.Render(m.works[m.worksCursor].work.ID))
		b.WriteString("\n\n")
	}

	b.WriteString("Select beads (Space to toggle, Enter to confirm, Esc to cancel):\n\n")

	for i, bead := range m.beadItems {
		var checkbox string
		if bead.selected {
			checkbox = "[●]"
		} else {
			checkbox = "[ ]"
		}

		line := fmt.Sprintf("%s %s - %s", checkbox, bead.ID, bead.Title)

		if i == m.beadsCursor {
			line = tuiSelectedStyle.Render("> " + line)
		} else {
			line = "  " + line
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Count selected
	selected := 0
	for _, s := range m.selectedBeads {
		if s {
			selected++
		}
	}
	b.WriteString(fmt.Sprintf("\n%d bead(s) selected", selected))

	return tuiAssignStyle.Width(m.width).Height(m.height).Render(b.String())
}

func (m tuiModel) renderHelp() string {
	help := `
  Claude Orchestrator - Help

  Drill-Down Navigation (lazygit-style)
  ────────────────────────────
  Depth 0: [Beads] | [Works] | [Details]
  Depth 1: [Works] | [Tasks] | [Task Details]
  Depth 2: [Tasks] | [Beads] | [Bead Details]

  Navigation
  ────────────────────────────
  h, ←          Move left / drill out from leftmost
  l, →          Move right / drill in from middle
  j/k, ↑/↓      Navigate list (syncs child panels)
  Tab, 1-3      Jump to panel at current depth

  Bead Management (at Beads panel)
  ────────────────────────────
  n             Create new bead
  e             Create new epic (feature)
  x             Close selected bead
  X             Reopen selected bead
  Space         Toggle bead selection
  A             Automated workflow (full automation)

  Bead Filtering
  ────────────────────────────
  o             Show open issues
  c             Show closed issues
  r             Show ready issues
  /             Fuzzy search (ID, title, description)
  L             Filter by label
  s             Cycle sort (default/priority/title)
  S             Triage sort (priority + type)
  v             Toggle expanded view

  Work Management (at Works panel)
  ────────────────────────────
  c             Create new work
  d             Destroy selected work
  r             Run work (create tasks + start)
  a             Assign beads to work
  v             Create review task
  p             Create PR task

  General
  ────────────────────────────
  ?             Show this help
  q             Quit

  Press any key to close...
`

	return tuiHelpStyle.Width(m.width).Height(m.height).Render(help)
}
