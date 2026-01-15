package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

const detailsPanelPadding = 4

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

// renderTwoColumnLayout renders the issues and details panels side-by-side
func (m *planModel) renderTwoColumnLayout() string {
	// Calculate column widths based on ratio
	// Account for separator (3 chars: " │ ") and panel borders
	totalContentWidth := m.width - 4 // -4 for outer margins
	separatorWidth := 3

	issuesWidth := int(float64(totalContentWidth-separatorWidth) * m.columnRatio)
	detailsWidth := totalContentWidth - separatorWidth - issuesWidth

	// Calculate content height (total height - status bar)
	contentHeight := m.height - 1 // -1 for status bar

	// Calculate visible lines for each panel (subtract border and title)
	issuesContentLines := contentHeight - 3 // -3 for border (2) + title (1)
	detailsContentLines := contentHeight - 3

	// Render issues panel
	issuesContent := m.renderIssuesList(issuesContentLines)
	issuesPanelStyle := tuiPanelStyle.Width(issuesWidth).Height(contentHeight - 2)
	if m.activePanel == PanelLeft {
		issuesPanelStyle = issuesPanelStyle.BorderForeground(lipgloss.Color("214")) // Highlight active panel
	}
	issuesPanel := issuesPanelStyle.Render(tuiTitleStyle.Render("Issues") + "\n" + issuesContent)

	// Render details panel
	detailsContent := m.renderDetailsPanel(detailsContentLines, detailsWidth)
	detailsPanelStyle := tuiPanelStyle.Width(detailsWidth).Height(contentHeight - 2)
	if m.activePanel == PanelRight {
		detailsPanelStyle = detailsPanelStyle.BorderForeground(lipgloss.Color("214")) // Highlight active panel
	}
	detailsPanel := detailsPanelStyle.Render(tuiTitleStyle.Render("Details") + "\n" + detailsContent)

	// Add vertical separator between columns
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Height(contentHeight).
		Render("│")

	// Combine columns horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, issuesPanel, separator, detailsPanel)
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

// renderDetailsPanel renders the detail panel content with width-aware text wrapping
func (m *planModel) renderDetailsPanel(visibleLines int, width int) string {
	var content strings.Builder

	// If in inline create mode, render the create form instead of issue details
	if m.viewMode == ViewCreateBeadInline {
		return m.renderCreateBeadInlineContent(visibleLines, width)
	}

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
		// Use width-aware wrapping for title
		titleStyle := tuiValueStyle.Width(width - detailsPanelPadding)
		content.WriteString(titleStyle.Render(bead.title))

		// Calculate remaining lines for description and children
		linesUsed := 2 // header + title
		remainingLines := visibleLines - linesUsed

		// Show description if we have room
		if bead.description != "" && remainingLines > 2 {
			content.WriteString("\n")
			// Use lipgloss word-wrapping for description
			descStyle := tuiDimStyle.Width(width - detailsPanelPadding)
			desc := bead.description
			// Reserve lines for children section
			descLines := remainingLines - 2 // Reserve 2 lines for children header + some items
			if len(bead.children) > 0 {
				descLines = min(descLines, 3) // Limit description to 3 lines if we have children
			}
			// Estimate max characters based on width and lines
			maxLen := descLines * (width - detailsPanelPadding)
			if len(desc) > maxLen && maxLen > 0 {
				desc = desc[:maxLen] + "..."
			}
			content.WriteString(descStyle.Render(desc))
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

			// Show children with status, truncate if needed to fit width
			maxChildren := min(len(bead.children), remainingLines)
			for i := 0; i < maxChildren; i++ {
				childID := bead.children[i]
				var childLine string
				if child, ok := childMap[childID]; ok {
					childLine = fmt.Sprintf("\n  %s %s %s",
						statusIcon(child.status),
						issueIDStyle.Render(child.id),
						child.title)
				} else {
					// Child not in current view (maybe filtered out)
					childLine = fmt.Sprintf("\n  ? %s", issueIDStyle.Render(childID))
				}
				// Truncate child line if it exceeds width (ANSI-aware)
				if lipgloss.Width(childLine) > width {
					childLine = truncate.StringWithTail(childLine, uint(width), "...")
				}
				content.WriteString(childLine)
			}
			if len(bead.children) > maxChildren {
				content.WriteString(fmt.Sprintf("\n  ... and %d more", len(bead.children)-maxChildren))
			}
		}
	}

	return content.String()
}

// renderCreateBeadInlineContent renders the create issue form inline in the details panel
func (m *planModel) renderCreateBeadInlineContent(visibleLines int, width int) string {
	var content strings.Builder

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

	// Render the form
	content.WriteString(tuiLabelStyle.Render("Create New Issue"))
	content.WriteString("\n\n")
	content.WriteString(titleLabel)
	content.WriteString("\n")
	content.WriteString(m.textInput.View())
	content.WriteString("\n\n")
	content.WriteString(typeLabel + " " + typeDisplay)
	content.WriteString("\n")
	content.WriteString(priorityLabel + " " + priorityDisplay)
	content.WriteString("\n\n")
	content.WriteString(descLabel)
	content.WriteString("\n")
	content.WriteString(m.createDescTextarea.View())
	content.WriteString("\n\n")
	content.WriteString(tuiDimStyle.Render("[Tab] Next field  [Enter] Create  [Esc] Cancel"))

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

	// Show p action based on session state
	pAction := "[p]Plan"
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID := m.beadItems[m.beadsCursor].id
		if m.activeBeadSessions[beadID] {
			pAction = "[p]Resume"
		}
	}

	// Commands on the left with hover effects
	nButton := styleButtonWithHover("[n]New", m.hoveredButton == "n")
	eButton := styleButtonWithHover("[e]Edit", m.hoveredButton == "e")
	aButton := styleButtonWithHover("[a]Child", m.hoveredButton == "a")
	xButton := styleButtonWithHover("[x]Close", m.hoveredButton == "x")
	wButton := styleButtonWithHover("[w]Work", m.hoveredButton == "w")
	pButton := styleButtonWithHover(pAction, m.hoveredButton == "p")
	helpButton := styleButtonWithHover("[?]Help", m.hoveredButton == "?")

	commands := nButton + " " + eButton + " " + aButton + " " + xButton + " " + wButton + " " + pButton + " " + helpButton

	// Commands plain text for width calculation
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work %s [?]Help", pAction)

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


// detectCommandsBarButton determines which button is at the given X position in the commands bar
func (m *planModel) detectCommandsBarButton(x int) string {
	// Commands bar format: "[n]New [e]Edit [a]Child [x]Close [w]Work [p]Plan [?]Help"
	// We need to find the position of each command in the rendered bar

	// Account for the status bar's left padding (tuiStatusBarStyle has Padding(0, 1))
	// This adds 1 character of padding to the left, shifting all content by 1 column
	if x < 1 {
		return ""
	}
	x = x - 1

	// Get the plain text version of the commands
	pAction := "[p]Plan"
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID := m.beadItems[m.beadsCursor].id
		if m.activeBeadSessions[beadID] {
			pAction = "[p]Resume"
		}
	}
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work %s [?]Help", pAction)

	// Find positions of each button
	nIdx := strings.Index(commandsPlain, "[n]New")
	eIdx := strings.Index(commandsPlain, "[e]Edit")
	aIdx := strings.Index(commandsPlain, "[a]Child")
	xIdx := strings.Index(commandsPlain, "[x]Close")
	wIdx := strings.Index(commandsPlain, "[w]Work")
	pIdx := strings.Index(commandsPlain, pAction)
	helpIdx := strings.Index(commandsPlain, "[?]Help")

	// Check if mouse is over any button (give reasonable width for clickability)
	if nIdx >= 0 && x >= nIdx && x < nIdx+6 { // "[n]New" is 6 chars
		return "n"
	}
	if eIdx >= 0 && x >= eIdx && x < eIdx+7 { // "[e]Edit" is 7 chars
		return "e"
	}
	if aIdx >= 0 && x >= aIdx && x < aIdx+8 { // "[a]Child" is 8 chars
		return "a"
	}
	if xIdx >= 0 && x >= xIdx && x < xIdx+8 { // "[x]Close" is 8 chars
		return "x"
	}
	if wIdx >= 0 && x >= wIdx && x < wIdx+7 { // "[w]Work" is 7 chars
		return "w"
	}
	if pIdx >= 0 && x >= pIdx && x < pIdx+len(pAction) {
		return "p"
	}
	if helpIdx >= 0 && x >= helpIdx && x < helpIdx+7 { // "[?]Help" is 7 chars
		return "?"
	}

	return ""
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
  Use 'p' to start or resume a planning session for an issue.

  Layout
  ────────────────────────────
  Two-column layout:
    - Left: Issues list (default 40% width)
    - Right: Issue details (default 60% width)
  [ / ]         Adjust column ratio (30/70, 40/60, 50/50)

  Navigation
  ────────────────────────────
  j/k, ↑/↓      Navigate list
  p             Start/Resume planning session

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
