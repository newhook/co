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
	issuesContent := m.renderIssuesList(issuesContentLines, issuesWidth)
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
func (m *planModel) renderIssuesList(visibleLines int, panelWidth int) string {
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
			content.WriteString(m.renderBeadLine(i, m.beadItems[i], panelWidth))
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

	// If in any bead form mode, render the unified form
	if m.viewMode == ViewCreateBead || m.viewMode == ViewCreateBeadInline ||
		m.viewMode == ViewAddChildBead || m.viewMode == ViewEditBead {
		return m.renderBeadFormContent(width)
	}

	// If in inline Linear import mode, render the import form instead of issue details
	if m.viewMode == ViewLinearImportInline {
		return m.renderLinearImportInlineContent(visibleLines, width)
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

// renderBeadFormContent renders the unified bead form (create, add child, or edit)
// The mode is determined by:
//   - editBeadID set → edit mode
//   - parentBeadID set → add child mode
//   - neither set → create mode
func (m *planModel) renderBeadFormContent(width int) string {
	var content strings.Builder

	// Adapt input widths to available space (account for panel padding)
	inputWidth := width - detailsPanelPadding
	if inputWidth < 20 {
		inputWidth = 20
	}
	m.textInput.Width = inputWidth
	m.createDescTextarea.SetWidth(inputWidth)

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

	// Determine mode and render appropriate header
	var header string
	if m.editBeadID != "" {
		// Edit mode
		header = "Edit Issue " + issueIDStyle.Render(m.editBeadID)
	} else if m.parentBeadID != "" {
		// Add child mode
		header = "Add Child Issue"
	} else {
		// Create mode
		header = "Create New Issue"
	}

	// Render header
	content.WriteString(tuiLabelStyle.Render(header))
	content.WriteString("\n")

	// Show parent info for add child mode
	if m.parentBeadID != "" {
		content.WriteString(tuiDimStyle.Render("Parent: ") + tuiValueStyle.Render(m.parentBeadID))
		content.WriteString("\n")
	}

	// Render form fields
	content.WriteString("\n")
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

	// Render Ok and Cancel buttons with hover styling
	okButton := styleButtonWithHover("  Ok  ", m.hoveredDialogButton == "ok")
	cancelButton := styleButtonWithHover("Cancel", m.hoveredDialogButton == "cancel")
	content.WriteString(okButton + "  " + cancelButton)
	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("[Tab] Next field"))

	return content.String()
}

// renderLinearImportInlineContent renders the Linear import form inline in the details panel
func (m *planModel) renderLinearImportInlineContent(visibleLines int, width int) string {
	var content strings.Builder

	// Show focus labels
	issueIDLabel := "Issue ID/URL:"
	createDepsLabel := "Create Dependencies:"
	updateLabel := "Update Existing:"
	dryRunLabel := "Dry Run:"
	maxDepthLabel := "Max Dependency Depth:"

	if m.linearImportFocus == 0 {
		issueIDLabel = tuiValueStyle.Render("Issue ID/URL:") + " (editing)"
	}
	if m.linearImportFocus == 1 {
		createDepsLabel = tuiValueStyle.Render("Create Dependencies:") + " (space to toggle)"
	}
	if m.linearImportFocus == 2 {
		updateLabel = tuiValueStyle.Render("Update Existing:") + " (space to toggle)"
	}
	if m.linearImportFocus == 3 {
		dryRunLabel = tuiValueStyle.Render("Dry Run:") + " (space to toggle)"
	}
	if m.linearImportFocus == 4 {
		maxDepthLabel = tuiValueStyle.Render("Max Dependency Depth:") + " (+/- adjust)"
	}

	// Checkbox display
	createDepsCheck := " "
	updateCheck := " "
	dryRunCheck := " "
	if m.linearImportCreateDeps {
		createDepsCheck = "x"
	}
	if m.linearImportUpdate {
		updateCheck = "x"
	}
	if m.linearImportDryRun {
		dryRunCheck = "x"
	}

	// Render the form
	content.WriteString(tuiLabelStyle.Render("Import from Linear"))
	content.WriteString("\n\n")
	content.WriteString(issueIDLabel)
	content.WriteString("\n")
	content.WriteString(m.linearImportInput.View())
	content.WriteString("\n\n")
	content.WriteString(createDepsLabel + " [" + createDepsCheck + "]")
	content.WriteString("\n")
	content.WriteString(updateLabel + " [" + updateCheck + "]")
	content.WriteString("\n")
	content.WriteString(dryRunLabel + " [" + dryRunCheck + "]")
	content.WriteString("\n\n")
	content.WriteString(maxDepthLabel + " " + tuiValueStyle.Render(fmt.Sprintf("%d", m.linearImportMaxDepth)))
	content.WriteString("\n\n")

	if m.linearImporting {
		content.WriteString(tuiDimStyle.Render("Importing..."))
	} else {
		content.WriteString(tuiDimStyle.Render("[Tab] Next field  [Enter] Import  [Esc] Cancel"))
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
	iButton := styleButtonWithHover("[i]Import", m.hoveredButton == "i")
	pButton := styleButtonWithHover(pAction, m.hoveredButton == "p")
	helpButton := styleButtonWithHover("[?]Help", m.hoveredButton == "?")

	commands := nButton + " " + eButton + " " + aButton + " " + xButton + " " + wButton + " " + iButton + " " + pButton + " " + helpButton

	// Commands plain text for width calculation
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work [i]Import %s [?]Help", pAction)

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
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work [i]Import %s [?]Help", pAction)

	// Find positions of each button
	nIdx := strings.Index(commandsPlain, "[n]New")
	eIdx := strings.Index(commandsPlain, "[e]Edit")
	aIdx := strings.Index(commandsPlain, "[a]Child")
	xIdx := strings.Index(commandsPlain, "[x]Close")
	wIdx := strings.Index(commandsPlain, "[w]Work")
	iIdx := strings.Index(commandsPlain, "[i]Import")
	pIdx := strings.Index(commandsPlain, pAction)
	helpIdx := strings.Index(commandsPlain, "[?]Help")

	// Check if mouse is over any button (give reasonable width for clickability)
	if nIdx >= 0 && x >= nIdx && x < nIdx+len("[n]New") {
		return "n"
	}
	if eIdx >= 0 && x >= eIdx && x < eIdx+len("[e]Edit") {
		return "e"
	}
	if aIdx >= 0 && x >= aIdx && x < aIdx+len("[a]Child") {
		return "a"
	}
	if xIdx >= 0 && x >= xIdx && x < xIdx+len("[x]Close") {
		return "x"
	}
	if wIdx >= 0 && x >= wIdx && x < wIdx+len("[w]Work") {
		return "w"
	}
	if iIdx >= 0 && x >= iIdx && x < iIdx+len("[i]Import") {
		return "i"
	}
	if pIdx >= 0 && x >= pIdx && x < pIdx+len(pAction) {
		return "p"
	}
	if helpIdx >= 0 && x >= helpIdx && x < helpIdx+len("[?]Help") {
		return "?"
	}

	return ""
}

// detectHoveredIssue determines which issue is at the given Y position
// Returns the absolute index in m.beadItems, or -1 if not over an issue
func (m *planModel) detectHoveredIssue(y int) int {
	// Check if mouse X is within the issues panel
	// Calculate column widths (same as renderTwoColumnLayout)
	totalContentWidth := m.width - 4 // -4 for outer margins
	separatorWidth := 3
	issuesWidth := int(float64(totalContentWidth-separatorWidth) * m.columnRatio)

	// Check if mouse is in the issues panel (left side)
	// Be generous with the boundary - include the entire panel width plus some margin
	maxIssueX := issuesWidth + separatorWidth + 2 // Include panel width, separator, and padding
	if m.mouseX > maxIssueX {
		return -1
	}

	// Layout within panel content:
	// Y=0: Top border
	// Y=1: "Issues" title
	// Y=2: filter info line
	// Y=3: first visible issue
	// Y=4: second visible issue, etc.

	// First issue line starts at Y=3
	const firstIssueY = 3

	if y < firstIssueY {
		return -1 // Not over an issue
	}

	if len(m.beadItems) == 0 {
		return -1
	}

	// Calculate visible window (same logic as renderIssuesList)
	contentHeight := m.height - 1 // -1 for status bar
	issuesContentLines := contentHeight - 3 // -3 for border (2) + title (1)
	visibleItems := max(issuesContentLines-1, 1) // -1 for filter line

	start := 0
	if m.beadsCursor >= visibleItems {
		start = m.beadsCursor - visibleItems + 1
	}
	end := min(start+visibleItems, len(m.beadItems))

	// Calculate which issue line was clicked
	lineIndex := y - firstIssueY
	absoluteIndex := start + lineIndex

	if absoluteIndex >= 0 && absoluteIndex < end && absoluteIndex < len(m.beadItems) {
		return absoluteIndex
	}

	return -1
}

// detectDialogButton determines which dialog button is at the given position
// Returns "ok", "cancel", or "" if not over a button
func (m *planModel) detectDialogButton(x, y int) string {
	// Dialog buttons only visible in form modes
	if m.viewMode != ViewCreateBead && m.viewMode != ViewCreateBeadInline &&
		m.viewMode != ViewAddChildBead && m.viewMode != ViewEditBead {
		return ""
	}

	// Calculate the details panel boundaries
	totalContentWidth := m.width - 4
	separatorWidth := 3
	issuesWidth := int(float64(totalContentWidth-separatorWidth) * m.columnRatio)

	// Details panel starts after issues panel + separator
	detailsPanelStart := issuesWidth + separatorWidth + 2 // +2 for left margin

	// Check if mouse is in the details panel
	if x < detailsPanelStart {
		return ""
	}

	// The buttons are rendered in the form content
	// We need to calculate the Y position of the button row
	// The form structure is:
	// - Header line
	// - Parent info line (if add child mode)
	// - Blank line
	// - Title label
	// - Title input
	// - Blank line + type + blank line
	// - Priority line
	// - Blank line
	// - Description label
	// - Textarea (4 lines)
	// - Blank line
	// - Button row

	// Calculate expected Y position of button row
	// Start from top of details panel (Y=1 for title, Y=2 for content start)
	formStartY := 2
	linesBeforeButtons := 1 // header
	if m.parentBeadID != "" {
		linesBeforeButtons++ // parent info line
	}
	linesBeforeButtons += 1  // blank line
	linesBeforeButtons += 1  // title label
	linesBeforeButtons += 1  // title input
	linesBeforeButtons += 2  // type + priority lines with preceding blank
	linesBeforeButtons += 1  // priority
	linesBeforeButtons += 2  // blank + desc label
	linesBeforeButtons += 4  // textarea (default height)
	linesBeforeButtons += 1  // blank line before buttons
	buttonRowY := formStartY + linesBeforeButtons

	if y != buttonRowY {
		return ""
	}

	// Calculate X position of buttons within the details panel
	// Buttons are at the start of the panel content
	// "  Ok  " (6 chars) + "  " (2 chars) + "Cancel" (6 chars)
	buttonAreaX := x - detailsPanelStart

	// Account for panel border and padding (approximately 2 chars)
	buttonAreaX -= 2

	if buttonAreaX >= 0 && buttonAreaX < 6 {
		return "ok"
	}
	if buttonAreaX >= 8 && buttonAreaX < 14 {
		return "cancel"
	}

	return ""
}

func (m *planModel) renderBeadLine(i int, bead beadItem, panelWidth int) string {
	icon := statusIcon(bead.status)

	// Selection indicator for multi-select
	var selectionIndicator string
	if m.selectedBeads[bead.id] {
		selectionIndicator = tuiSelectedCheckStyle.Render("●") + " "
	}

	// Session indicator - compact "P" (processing) shown after status icon
	var sessionIndicator string
	if m.activeBeadSessions[bead.id] {
		sessionIndicator = tuiSuccessStyle.Render("P")
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

	// Calculate available width and truncate title if needed to prevent wrapping
	availableWidth := panelWidth - 4 // Account for panel padding/borders

	// Calculate prefix length for normal display
	var prefixLen int
	if m.beadsExpanded {
		prefixLen = 3 + len(bead.id) + 1 + 3 + len(bead.beadType) + 3 // icon + ID + space + [P# type] + spaces
	} else {
		prefixLen = 3 + len(bead.id) + 3 // icon + ID + type letter + spaces
	}
	if bead.assignedWorkID != "" {
		prefixLen += len(bead.assignedWorkID) + 3 // [work-id] + space
	}
	if bead.treeDepth > 0 {
		prefixLen += len(bead.treePrefixPattern)
	}

	// Truncate title to fit on one line
	title := bead.title
	maxTitleLen := availableWidth - prefixLen
	if maxTitleLen < 10 {
		maxTitleLen = 10 // Minimum space for title
	}
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-3] + "..."
	}

	// Build styled line for normal display
	var line string
	if m.beadsExpanded {
		line = fmt.Sprintf("%s%s%s%s %s [P%d %s] %s%s", selectionIndicator, treePrefix, workIndicator, icon, styledID, bead.priority, bead.beadType, sessionIndicator, title)
	} else {
		line = fmt.Sprintf("%s%s%s%s %s %s%s %s", selectionIndicator, treePrefix, workIndicator, icon, styledID, styledType, sessionIndicator, title)
	}

	// For selected/hovered lines, build plain text version to avoid ANSI code conflicts
	if i == m.beadsCursor || i == m.hoveredIssue {
		// Get type letter for compact display
		var typeLetter string
		switch bead.beadType {
		case "task":
			typeLetter = "T"
		case "bug":
			typeLetter = "B"
		case "feature":
			typeLetter = "F"
		case "epic":
			typeLetter = "E"
		case "chore":
			typeLetter = "C"
		default:
			typeLetter = "?"
		}

		// Build selection indicator (plain text)
		var plainSelectionIndicator string
		if m.selectedBeads[bead.id] {
			plainSelectionIndicator = "● "
		}

		// Build session indicator (plain text) - compact "P" after status icon
		var plainSessionIndicator string
		if m.activeBeadSessions[bead.id] {
			plainSessionIndicator = "P"
		}

		// Build work indicator (plain text)
		var plainWorkIndicator string
		if bead.assignedWorkID != "" {
			plainWorkIndicator = "[" + bead.assignedWorkID + "] "
		}

		// Build tree prefix (plain text, no styling)
		var plainTreePrefix string
		if bead.treeDepth > 0 && bead.treePrefixPattern != "" {
			plainTreePrefix = bead.treePrefixPattern
		}

		// Build plain text line without any styling (using already truncated title)
		var plainLine string
		if m.beadsExpanded {
			plainLine = fmt.Sprintf("%s%s%s%s %s [P%d %s] %s%s", plainSelectionIndicator, plainTreePrefix, plainWorkIndicator, icon, bead.id, bead.priority, bead.beadType, plainSessionIndicator, title)
		} else {
			plainLine = fmt.Sprintf("%s%s%s%s %s %s%s %s", plainSelectionIndicator, plainTreePrefix, plainWorkIndicator, icon, bead.id, typeLetter, plainSessionIndicator, title)
		}

		// Pad to fill width
		visWidth := lipgloss.Width(plainLine)
		if visWidth < availableWidth {
			plainLine += strings.Repeat(" ", availableWidth-visWidth)
		}

		if i == m.beadsCursor {
			return tuiSelectedStyle.Render(plainLine)
		}

		// Hover style
		hoverStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("240")).
			Bold(true)
		return hoverStyle.Render(plainLine)
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
  i             Import issue from Linear

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
  P             Issue is processing (active Claude session)
  [w-xxx]       Issue is assigned to work w-xxx

  Press any key to close...
`
	return tuiHelpStyle.Width(m.width).Height(m.height).Render(help)
}
