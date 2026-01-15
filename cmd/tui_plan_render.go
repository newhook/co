package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderFixedPanel renders a panel with border and fixed height
func (m *planModel) renderFixedPanel(title, content string, width, height int) string {
	titleLine := tuiTitleStyle.Render(title)

	var b strings.Builder
	b.WriteString(titleLine)
	b.WriteString("\n")
	b.WriteString(content)

	// Height-2 for the border lines
	return tuiPanelStyle.Width(width).Height(height - 2).Render(b.String())
}

// renderIssuesList renders just the list content for the given number of visible lines
func (m *planModel) renderIssuesList(visibleLines int) string {
	filterInfo := fmt.Sprintf("Filter: %s | Sort: %s", m.filters.status, m.filters.sortBy)
	if m.filters.searchText != "" {
		filterInfo += fmt.Sprintf(" | Search: %s", m.filters.searchText)
	}
	if m.filters.label != "" {
		filterInfo += fmt.Sprintf(" | Label: %s", m.filters.label)
	}

	var content strings.Builder
	content.WriteString(tuiDimStyle.Render(filterInfo))
	content.WriteString("\n")

	if len(m.beadItems) == 0 {
		content.WriteString(tuiDimStyle.Render("No issues found"))
	} else {
		visibleItems := max(visibleLines-1, 1) // -1 for filter line

		start := 0
		if m.beadsCursor >= visibleItems {
			start = m.beadsCursor - visibleItems + 1
		}
		end := min(start+visibleItems, len(m.beadItems))

		for i := start; i < end; i++ {
			content.WriteString(m.renderBeadLine(i, m.beadItems[i]))
			if i < end-1 {
				content.WriteString("\n")
			}
		}
	}

	return content.String()
}

// renderDetailsContent renders the detail panel content
func (m *planModel) renderDetailsContent(visibleLines int) string {
	var content strings.Builder

	if len(m.beadItems) == 0 || m.beadsCursor >= len(m.beadItems) {
		content.WriteString(tuiDimStyle.Render("No issue selected"))
	} else {
		bead := m.beadItems[m.beadsCursor]

		content.WriteString(tuiLabelStyle.Render("ID: "))
		content.WriteString(tuiValueStyle.Render(bead.id))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("Type: "))
		content.WriteString(tuiValueStyle.Render(bead.beadType))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("P"))
		content.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d", bead.priority)))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("Status: "))
		content.WriteString(tuiValueStyle.Render(bead.status))
		if m.activeBeadSessions[bead.id] {
			content.WriteString("  ")
			content.WriteString(tuiSuccessStyle.Render("[Session Active]"))
		}
		if bead.assignedWorkID != "" {
			content.WriteString("  ")
			content.WriteString(tuiDimStyle.Render("Work: " + bead.assignedWorkID))
		}
		content.WriteString("\n")
		content.WriteString(tuiValueStyle.Render(bead.title))

		// Calculate remaining lines for description and children
		linesUsed := 2 // header + title
		remainingLines := visibleLines - linesUsed

		// Show description if we have room
		if bead.description != "" && remainingLines > 2 {
			content.WriteString("\n")
			desc := bead.description
			// Reserve lines for children section
			descLines := remainingLines - 2 // Reserve 2 lines for children header + some items
			if len(bead.children) > 0 {
				descLines = min(descLines, 2) // Limit description to 2 lines if we have children
			}
			maxLen := descLines * 80
			if len(desc) > maxLen && maxLen > 0 {
				desc = desc[:maxLen] + "..."
			}
			content.WriteString(tuiDimStyle.Render(desc))
			linesUsed++
			remainingLines--
		}

		// Show children (issues blocked by this one) if we have them
		if len(bead.children) > 0 && remainingLines > 1 {
			content.WriteString("\n")
			content.WriteString(tuiLabelStyle.Render("Blocks: "))
			linesUsed++
			remainingLines--

			// Build a map for quick lookup of child status
			childMap := make(map[string]*beadItem)
			for i := range m.beadItems {
				childMap[m.beadItems[i].id] = &m.beadItems[i]
			}

			// Show children with status
			maxChildren := min(len(bead.children), remainingLines)
			for i := 0; i < maxChildren; i++ {
				childID := bead.children[i]
				if child, ok := childMap[childID]; ok {
					content.WriteString(fmt.Sprintf("\n  %s %s %s",
						statusIcon(child.status),
						issueIDStyle.Render(child.id),
						child.title))
				} else {
					// Child not in current view (maybe filtered out)
					content.WriteString(fmt.Sprintf("\n  ? %s", issueIDStyle.Render(childID)))
				}
			}
			if len(bead.children) > maxChildren {
				content.WriteString(fmt.Sprintf("\n  ... and %d more", len(bead.children)-maxChildren))
			}
		}
	}

	return content.String()
}

func (m *planModel) renderCommandsBar() string {
	// If in search mode, show vim-style inline search bar
	if m.viewMode == ViewBeadSearch {
		searchPrompt := "/"
		searchInput := m.textInput.View()
		hint := tuiDimStyle.Render("  [Enter]Search  [Esc]Cancel")
		return tuiStatusBarStyle.Width(m.width).Render(searchPrompt + searchInput + hint)
	}

	// Show Enter action based on session state
	enterAction := "[Enter]Plan"
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID := m.beadItems[m.beadsCursor].id
		if m.activeBeadSessions[beadID] {
			enterAction = "[Enter]Resume"
		}
	}

	// Commands on the left (plain text for width calculation)
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work %s [?]Help", enterAction)
	commands := styleHotkeys(commandsPlain)

	// Status on the right
	var status string
	var statusPlain string
	if m.statusMessage != "" {
		statusPlain = m.statusMessage
		if m.statusIsError {
			status = tuiErrorStyle.Render(m.statusMessage)
		} else {
			status = tuiSuccessStyle.Render(m.statusMessage)
		}
	} else if m.loading {
		statusPlain = "Loading..."
		status = m.spinner.View() + " Loading..."
	} else {
		statusPlain = fmt.Sprintf("Updated: %s", m.lastUpdate.Format("15:04:05"))
		status = tuiDimStyle.Render(statusPlain)
	}

	// Build bar with commands left, status right
	padding := max(m.width-len(commandsPlain)-len(statusPlain)-4, 2)
	return tuiStatusBarStyle.Width(m.width).Render(commands + strings.Repeat(" ", padding) + status)
}

func (m *planModel) renderBeadLine(i int, bead beadItem) string {
	icon := statusIcon(bead.status)

	// Selection indicator for multi-select
	var selectionIndicator string
	if m.selectedBeads[bead.id] {
		selectionIndicator = tuiSelectedCheckStyle.Render("●") + " "
	}

	// Session indicator
	var sessionIndicator string
	if m.activeBeadSessions[bead.id] {
		sessionIndicator = tuiSuccessStyle.Render("[C]") + " "
	}

	// Work assignment indicator
	var workIndicator string
	if bead.assignedWorkID != "" {
		workIndicator = tuiDimStyle.Render("["+bead.assignedWorkID+"]") + " "
	}

	// Tree indentation with connector lines (styled dim)
	var treePrefix string
	if bead.treeDepth > 0 && bead.treePrefixPattern != "" {
		treePrefix = issueTreeStyle.Render(bead.treePrefixPattern)
	}

	// Styled issue ID
	styledID := issueIDStyle.Render(bead.id)

	// Short type indicator with color
	var styledType string
	switch bead.beadType {
	case "task":
		styledType = typeTaskStyle.Render("T")
	case "bug":
		styledType = typeBugStyle.Render("B")
	case "feature":
		styledType = typeFeatureStyle.Render("F")
	case "epic":
		styledType = typeEpicStyle.Render("E")
	case "chore":
		styledType = typeChoreStyle.Render("C")
	case "merge-request":
		styledType = typeDefaultStyle.Render("M")
	case "molecule":
		styledType = typeDefaultStyle.Render("m")
	case "gate":
		styledType = typeDefaultStyle.Render("G")
	case "agent":
		styledType = typeDefaultStyle.Render("A")
	case "role":
		styledType = typeDefaultStyle.Render("R")
	case "rig":
		styledType = typeDefaultStyle.Render("r")
	case "convoy":
		styledType = typeDefaultStyle.Render("c")
	case "event":
		styledType = typeDefaultStyle.Render("v")
	default:
		styledType = typeDefaultStyle.Render("?")
	}

	var line string
	if m.beadsExpanded {
		line = fmt.Sprintf("%s%s%s%s%s %s [P%d %s] %s", selectionIndicator, treePrefix, workIndicator, sessionIndicator, icon, styledID, bead.priority, bead.beadType, bead.title)
	} else {
		line = fmt.Sprintf("%s%s%s%s%s %s %s %s", selectionIndicator, treePrefix, workIndicator, sessionIndicator, icon, styledID, styledType, bead.title)
	}

	if i == m.beadsCursor {
		return tuiSelectedStyle.Render(line)
	}

	// Style closed parent beads with dim style (grayed out)
	if bead.isClosedParent {
		return tuiDimStyle.Render(line)
	}

	return line
}

func (m *planModel) renderWithDialog(dialog string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *planModel) renderHelp() string {
	help := `
  Plan Mode - Help

  Each issue gets its own dedicated Claude session in a separate tab.
  Use Enter to start or resume a planning session for an issue.

  Navigation
  ────────────────────────────
  j/k, ↑/↓      Navigate list
  Enter         Start/Resume planning session

  Issue Management
  ────────────────────────────
  n             Create new issue (any type)
  e             Edit issue inline (textarea)
  E             Edit issue in $EDITOR
  a             Add child issue (blocked by selected)
  x             Close selected issue
  Space         Toggle issue selection (for multi-select)
  w             Create work from issue(s)
  W             Add issue to existing work

  Filtering & Sorting
  ────────────────────────────
  o             Show open issues
  c             Show closed issues
  r             Show ready issues
  /             Fuzzy search
  L             Filter by label
  s             Cycle sort mode
  v             Toggle expanded view

  Indicators
  ────────────────────────────
  ●             Issue is selected for multi-select
  [C]           Issue has an active Claude session
  [w-xxx]       Issue is assigned to work w-xxx

  Press any key to close...
`
	return tuiHelpStyle.Width(m.width).Height(m.height).Render(help)
}
