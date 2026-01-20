package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// AddToWorkPanel renders the add-to-work selection list.
type AddToWorkPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Data
	beadItems      []beadItem
	cursor         int
	selectedBeads  map[string]bool
	availableWorks []workItem
	worksCursor    int
}

// NewAddToWorkPanel creates a new AddToWorkPanel
func NewAddToWorkPanel() *AddToWorkPanel {
	return &AddToWorkPanel{
		width:         60,
		height:        20,
		selectedBeads: make(map[string]bool),
	}
}

// SetSize updates the panel dimensions
func (p *AddToWorkPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates the focus state
func (p *AddToWorkPanel) SetFocus(focused bool) {
	p.focused = focused
}

// IsFocused returns whether the panel is focused
func (p *AddToWorkPanel) IsFocused() bool {
	return p.focused
}

// SetData updates the panel data
func (p *AddToWorkPanel) SetData(
	beadItems []beadItem,
	cursor int,
	selectedBeads map[string]bool,
	availableWorks []workItem,
	worksCursor int,
) {
	p.beadItems = beadItems
	p.cursor = cursor
	p.selectedBeads = selectedBeads
	p.availableWorks = availableWorks
	p.worksCursor = worksCursor
}

// Render returns the add-to-work form content
func (p *AddToWorkPanel) Render(visibleLines int) string {
	var content strings.Builder

	// Collect selected beads
	var selectedBeads []beadItem
	for _, item := range p.beadItems {
		if p.selectedBeads[item.id] {
			selectedBeads = append(selectedBeads, item)
		}
	}

	// If no selected beads, use cursor bead
	if len(selectedBeads) == 0 && len(p.beadItems) > 0 && p.cursor < len(p.beadItems) {
		selectedBeads = append(selectedBeads, p.beadItems[p.cursor])
	}

	// Header
	if len(selectedBeads) == 1 {
		content.WriteString(tuiLabelStyle.Render("Add Issue to Work"))
	} else {
		content.WriteString(tuiLabelStyle.Render(fmt.Sprintf("Add %d Issues to Work", len(selectedBeads))))
	}
	content.WriteString("\n\n")

	// Show which issues we're adding
	if len(selectedBeads) == 1 {
		content.WriteString(tuiDimStyle.Render("Issue: "))
		content.WriteString(issueIDStyle.Render(selectedBeads[0].id))
		content.WriteString("\n")
		titleStyle := tuiValueStyle.Width(p.width - 4)
		content.WriteString(titleStyle.Render(selectedBeads[0].title))
		content.WriteString("\n")
	} else if len(selectedBeads) > 1 {
		content.WriteString(tuiDimStyle.Render("Issues:\n"))
		for i, bead := range selectedBeads {
			if i >= 5 {
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ... and %d more\n", len(selectedBeads)-5)))
				break
			}
			content.WriteString("  ")
			content.WriteString(issueIDStyle.Render(bead.id))
			content.WriteString(": ")
			titleStyle := tuiValueStyle.Width(p.width - 4 - len(bead.id) - 4)
			content.WriteString(titleStyle.Render(bead.title))
			content.WriteString("\n")
		}
	}
	content.WriteString("\n")

	// Works list header
	content.WriteString(tuiLabelStyle.Render("Select a work:"))
	content.WriteString("\n")

	if len(p.availableWorks) == 0 {
		content.WriteString(tuiDimStyle.Render("  No available works found."))
		content.WriteString("\n")
		content.WriteString(tuiDimStyle.Render("  Create a work first with 'w'."))
	} else {
		// Calculate how many works we can show
		linesUsed := 7
		maxWorks := visibleLines - linesUsed
		if maxWorks < 3 {
			maxWorks = 3
		}

		// Show works with scrolling if needed
		start := 0
		if p.worksCursor >= maxWorks {
			start = p.worksCursor - maxWorks + 1
		}
		end := min(start+maxWorks, len(p.availableWorks))

		for i := start; i < end; i++ {
			work := p.availableWorks[i]

			var lineStyle lipgloss.Style
			prefix := "  "
			if i == p.worksCursor {
				prefix = "► "
				lineStyle = tuiSelectedStyle
			} else {
				lineStyle = tuiDimStyle
			}

			var workLine strings.Builder
			workLine.WriteString(prefix)
			workLine.WriteString(work.id)
			workLine.WriteString(" (")
			workLine.WriteString(work.status)
			workLine.WriteString(")")

			if work.rootIssueID != "" {
				workLine.WriteString("\n    ")
				workLine.WriteString("Root: ")
				workLine.WriteString(work.rootIssueID)
				if work.rootIssueTitle != "" {
					title := work.rootIssueTitle
					maxTitleLen := p.width - 4 - 12
					if len(title) > maxTitleLen && maxTitleLen > 10 {
						title = title[:maxTitleLen-3] + "..."
					}
					workLine.WriteString(" - ")
					workLine.WriteString(title)
				}
			}

			workLine.WriteString("\n    ")
			workLine.WriteString("Branch: ")
			branch := work.branch
			maxBranchLen := p.width - 4 - 12
			if len(branch) > maxBranchLen && maxBranchLen > 10 {
				branch = branch[:maxBranchLen-3] + "..."
			}
			workLine.WriteString(branch)

			if i == p.worksCursor {
				content.WriteString(lineStyle.Render(workLine.String()))
			} else {
				content.WriteString(workLine.String())
			}
			content.WriteString("\n")
		}

		if len(p.availableWorks) > maxWorks {
			if start > 0 {
				content.WriteString(tuiDimStyle.Render("  ↑ more above"))
				content.WriteString("\n")
			}
			if end < len(p.availableWorks) {
				content.WriteString(tuiDimStyle.Render("  ↓ more below"))
				content.WriteString("\n")
			}
		}
	}

	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("[↑↓/jk] Navigate  [Enter] Add to work  [Esc] Cancel"))

	return content.String()
}

// RenderWithPanel returns the panel with border styling
func (p *AddToWorkPanel) RenderWithPanel(contentHeight int) string {
	panelContent := p.Render(contentHeight - 3)

	panelStyle := tuiPanelStyle.Width(p.width).Height(contentHeight - 2)
	if p.focused {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("214"))
	}

	return panelStyle.Render(tuiTitleStyle.Render("Add to Work") + "\n" + panelContent)
}
