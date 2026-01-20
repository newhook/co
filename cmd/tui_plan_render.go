package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

// renderFocusedWorkSplitView renders the split view when a work is focused
// This shows a horizontal split: Work details on top (40%), Issues/Details below (60%)
func (m *planModel) renderFocusedWorkSplitView() string {
	// Calculate heights for split view (40% work, 60% plan mode)
	totalHeight := m.height - 1 // -1 for status bar
	workPanelHeight := int(float64(totalHeight) * 0.4)
	planPanelHeight := totalHeight - workPanelHeight - 1 // -1 for separator

	// Update work details panel size for the work section
	m.workDetails.SetSize(m.width, workPanelHeight)

	// Render work panel using the workDetails panel
	workPanel := m.workDetails.Render()

	// === Render Plan Mode Panel (Bottom) ===
	// Update issues and details panel sizes for the reduced height
	totalContentWidth := m.width - 4
	separatorWidth := 3
	issuesWidth := int(float64(totalContentWidth-separatorWidth) * m.columnRatio)
	detailsWidth := totalContentWidth - separatorWidth - issuesWidth

	// Temporarily update panel sizes for the reduced height
	m.issuesPanel.SetSize(issuesWidth, planPanelHeight)
	m.detailsPanel.SetSize(detailsWidth, planPanelHeight)

	// Render issues panel
	issuesPanel := m.issuesPanel.RenderWithPanel(planPanelHeight)

	// Select the right panel based on view mode
	var detailsPanel string
	switch m.viewMode {
	case ViewCreateBead, ViewCreateBeadInline, ViewAddChildBead, ViewEditBead:
		m.beadFormPanel.SetSize(detailsWidth, planPanelHeight)
		detailsPanel = m.beadFormPanel.RenderWithPanel(planPanelHeight)
	case ViewLinearImportInline:
		m.linearImportPanel.SetSize(detailsWidth, planPanelHeight)
		detailsPanel = m.linearImportPanel.RenderWithPanel(planPanelHeight)
	case ViewCreateWork:
		m.createWorkPanel.SetSize(detailsWidth, planPanelHeight)
		detailsPanel = m.createWorkPanel.RenderWithPanel(planPanelHeight)
	default:
		detailsPanel = m.detailsPanel.RenderWithPanel(planPanelHeight)
	}

	// Add vertical separator between columns
	vertSeparator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Height(planPanelHeight).
		Render("│")

	// Combine plan mode columns
	planSection := lipgloss.JoinHorizontal(lipgloss.Top, issuesPanel, vertSeparator, detailsPanel)

	// Add horizontal separator between work and plan sections
	horizSeparator := strings.Repeat("─", m.width)
	horizSeparatorStyled := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(horizSeparator)

	// Combine everything vertically
	return lipgloss.JoinVertical(lipgloss.Left, workPanel, horizSeparatorStyled, planSection)
}

// renderTwoColumnLayout renders the issues and details panels side-by-side
func (m *planModel) renderTwoColumnLayout() string {
	// Check if a work is focused - if so, render split view
	if m.focusedWorkID != "" {
		return m.renderFocusedWorkSplitView()
	}

	// Calculate content height (total height - status bar)
	contentHeight := m.height - 1 // -1 for status bar

	// Use panels for rendering (they're already synced with correct sizes and data)
	issuesPanel := m.issuesPanel.RenderWithPanel(contentHeight)

	// Select the right panel based on view mode
	var rightPanel string
	switch m.viewMode {
	case ViewCreateBead, ViewCreateBeadInline, ViewAddChildBead, ViewEditBead:
		rightPanel = m.beadFormPanel.RenderWithPanel(contentHeight)
	case ViewLinearImportInline:
		rightPanel = m.linearImportPanel.RenderWithPanel(contentHeight)
	case ViewCreateWork:
		rightPanel = m.createWorkPanel.RenderWithPanel(contentHeight)
	default:
		rightPanel = m.detailsPanel.RenderWithPanel(contentHeight)
	}

	// Add vertical separator between columns
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Height(contentHeight).
		Render("│")

	// Combine columns horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, issuesPanel, separator, rightPanel)
}

// detectCommandsBarButton determines which button is at the given X position in the commands bar
func (m *planModel) detectCommandsBarButton(x int) string {
	// Delegate to the status bar panel
	return m.statusBar.DetectButton(x)
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

	// Calculate the Y offset for the issues panel based on focused work mode
	issuesPanelStartY := 0
	var contentHeight int
	if m.focusedWorkID != "" {
		// In focused work mode, the issues panel is below the work panel
		totalHeight := m.height - 1 // -1 for status bar
		workPanelHeight := int(float64(totalHeight) * 0.4)
		issuesPanelStartY = workPanelHeight + 1 // +1 for separator
		contentHeight = totalHeight - workPanelHeight - 1
	} else {
		contentHeight = m.height - 1 // -1 for status bar
	}

	// Layout within panel content:
	// Y=issuesPanelStartY+0: Top border
	// Y=issuesPanelStartY+1: "Issues" title
	// Y=issuesPanelStartY+2: filter info line
	// Y=issuesPanelStartY+3: first visible issue
	// Y=issuesPanelStartY+4: second visible issue, etc.

	// First issue line starts at issuesPanelStartY + 3
	firstIssueY := issuesPanelStartY + 3

	if y < firstIssueY {
		return -1 // Not over an issue
	}

	if len(m.beadItems) == 0 {
		return -1
	}

	// Calculate visible window (same logic as renderIssuesList)
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

// calculateWorkOverlayHeight returns the height of the work overlay dropdown
func (m *planModel) calculateWorkOverlayHeight() int {
	dropdownHeight := int(float64(m.height) * 0.4)
	if dropdownHeight < 12 {
		dropdownHeight = 12
	} else if dropdownHeight > 25 {
		dropdownHeight = 25
	}
	return dropdownHeight
}

// detectHoveredIssueWithOffset detects issue hover when content is offset by overlay
func (m *planModel) detectHoveredIssueWithOffset(y int, overlayHeight int) int {
	// Check if mouse X is within the issues panel
	totalContentWidth := m.width - 4
	separatorWidth := 3
	issuesWidth := int(float64(totalContentWidth-separatorWidth) * m.columnRatio)

	maxIssueX := issuesWidth + separatorWidth + 2
	if m.mouseX > maxIssueX {
		return -1
	}

	// Calculate the adjusted Y position relative to the content below overlay
	// The content starts at overlayHeight
	adjustedY := y - overlayHeight

	// Layout within panel content (same as detectHoveredIssue):
	// Y=0: Top border
	// Y=1: "Issues" title
	// Y=2: filter info line
	// Y=3: first visible issue
	const firstIssueY = 3

	if adjustedY < firstIssueY {
		return -1
	}

	if len(m.beadItems) == 0 {
		return -1
	}

	// Calculate content height (reduced by overlay)
	contentHeight := m.height - overlayHeight - 1 // -1 for status bar
	issuesContentLines := contentHeight - 3
	visibleItems := max(issuesContentLines-1, 1)

	start := 0
	if m.beadsCursor >= visibleItems {
		start = m.beadsCursor - visibleItems + 1
	}
	end := min(start+visibleItems, len(m.beadItems))

	lineIndex := adjustedY - firstIssueY
	absoluteIndex := start + lineIndex

	if absoluteIndex >= 0 && absoluteIndex < end && absoluteIndex < len(m.beadItems) {
		return absoluteIndex
	}

	return -1
}

// detectClickedTask determines if a click is on a task in the focused work panel
// Returns the task ID if clicked on a task, or "" if not over a task
func (m *planModel) detectClickedTask(x, y int) string {
	if m.focusedWorkID == "" {
		return ""
	}

	// Calculate work panel dimensions
	totalHeight := m.height - 1 // -1 for status bar
	workPanelHeight := int(float64(totalHeight) * 0.4)
	halfWidth := (m.width - 4) / 2 - 1 // left panel width

	// Check if click is within work panel bounds (top section, left half)
	if y >= workPanelHeight || x > halfWidth+2 {
		return ""
	}

	// Find the focused work
	var focusedWork *workProgress
	for _, work := range m.workTiles {
		if work != nil && work.work.ID == m.focusedWorkID {
			focusedWork = work
			break
		}
	}
	if focusedWork == nil || len(focusedWork.tasks) == 0 {
		return ""
	}

	// Layout in work panel:
	// Y=0: Top border
	// Y=1: Panel title "Work & Tasks"
	// Y=2: Work ID and status
	// Y=3: Branch
	// Y=4: Progress
	// Y=5: Separator
	// Y=6: "Tasks:" header
	// Y=7: First task
	// Y=8: Second task, etc.

	const firstTaskY = 7
	workPanelContentHeight := workPanelHeight - 3 // -3 for border and title
	headerLines := 5 // lines used for header info above
	availableLines := workPanelContentHeight - headerLines - 1

	if y < firstTaskY || y >= firstTaskY+availableLines {
		return ""
	}

	// Find selected task index for scroll calculation
	selectedIndex := -1
	for i, task := range focusedWork.tasks {
		if task.task.ID == m.selectedTaskID {
			selectedIndex = i
			break
		}
	}

	// Calculate scroll window (same as render logic)
	startIdx := 0
	if selectedIndex >= availableLines && availableLines > 0 {
		startIdx = selectedIndex - availableLines/2
		if startIdx < 0 {
			startIdx = 0
		}
	}

	// Calculate which task line was clicked
	lineIndex := y - firstTaskY
	taskIndex := startIdx + lineIndex

	if taskIndex >= 0 && taskIndex < len(focusedWork.tasks) {
		return focusedWork.tasks[taskIndex].task.ID
	}

	return ""
}

// detectClickedPanel determines which panel was clicked in the focused work view
// Returns "work-left", "work-right", "issues-left", "issues-right", or "" if not in a panel
func (m *planModel) detectClickedPanel(x, y int) string {
	if m.focusedWorkID == "" {
		return ""
	}

	// Calculate panel boundaries
	totalHeight := m.height - 1 // -1 for status bar
	workPanelHeight := int(float64(totalHeight) * 0.4)
	halfWidth := (m.width - 4) / 2 // Half width including separator area

	// Determine Y section (top = work, bottom = issues)
	isWorkSection := y < workPanelHeight
	isIssuesSection := y > workPanelHeight // Skip separator line

	// Determine X section (left or right)
	isLeftSide := x <= halfWidth
	isRightSide := x > halfWidth

	if isWorkSection {
		if isLeftSide {
			return "work-left"
		}
		if isRightSide {
			return "work-right"
		}
	}

	if isIssuesSection {
		if isLeftSide {
			return "issues-left"
		}
		if isRightSide {
			return "issues-right"
		}
	}

	return ""
}

// detectDialogButton determines which dialog button is at the given position.
// This is the mouse click detection component of the button tracking system.
//
// For ViewCreateWork mode, it uses the button positions tracked during rendering:
// 1. Calculates the mouse position relative to the details panel content area
// 2. Iterates through m.dialogButtons to find a matching region
// 3. Checks if the click coordinates fall within any button's boundaries
// 4. Returns the button ID if found, or "" if no button matches
//
// For other dialog modes, it calculates button positions based on the form structure.
// Returns "ok", "cancel", "execute", "auto", or "" if not over a button.
func (m *planModel) detectDialogButton(x, y int) string {
	// Dialog buttons only visible in form modes, Linear import mode, and work creation mode
	if m.viewMode != ViewCreateBead && m.viewMode != ViewCreateBeadInline &&
		m.viewMode != ViewAddChildBead && m.viewMode != ViewEditBead &&
		m.viewMode != ViewLinearImportInline && m.viewMode != ViewCreateWork {
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

	// Handle ViewCreateWork using tracked button positions from the CreateWorkPanel
	if m.viewMode == ViewCreateWork {
		// Use the button positions tracked during rendering.
		// This is the core of the mouse click detection system for dialog buttons.
		// The positions stored are relative to the details panel content area,
		// so we need to translate the absolute mouse coordinates.
		buttonAreaX := x - detailsPanelStart

		// Get button positions from the CreateWorkPanel
		dialogButtons := m.createWorkPanel.GetDialogButtons()

		// Check each tracked button region to see if the click falls within it
		for _, button := range dialogButtons {
			// The Y position stored in button.Y is the line number within the content area.
			// The content starts at row 2 of the details panel (after border and title).
			// The mouse Y has already been adjusted by -1 in tui_root.go.
			// So the absolute Y position for comparison is button.Y + 2 (for border+title)
			absoluteY := button.Y + 2

			// Check if the mouse click coordinates match this button's region.
			// StartX and EndX are inclusive boundaries.
			if y == absoluteY && buttonAreaX >= button.StartX && buttonAreaX <= button.EndX {
				return button.ID
			}
		}
		return ""
	} else if m.viewMode == ViewLinearImportInline {
		// The Linear import form structure:
		// - Header line "Import from Linear (Bulk)"
		// - Blank line
		// - Issue IDs label
		// - Textarea (height 4)
		// - Blank line
		// - Create Dependencies checkbox
		// - Update Existing checkbox
		// - Dry Run checkbox
		// - Blank line
		// - Max Depth line
		// - Blank line
		// - Button row
		formStartY := 2
		linesBeforeButtons := 1  // header
		linesBeforeButtons += 1  // blank line
		linesBeforeButtons += 1  // issue IDs label
		linesBeforeButtons += 4  // textarea (height 4)
		linesBeforeButtons += 1  // blank line
		linesBeforeButtons += 1  // create deps checkbox
		linesBeforeButtons += 1  // update checkbox
		linesBeforeButtons += 1  // dry run checkbox
		linesBeforeButtons += 1  // blank line
		linesBeforeButtons += 1  // max depth
		linesBeforeButtons += 1  // blank line
		buttonRowY := formStartY + linesBeforeButtons

		if y != buttonRowY {
			return ""
		}
	} else {
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
	icon := statusIcon(bead.Status)

	// Selection indicator for multi-select
	var selectionIndicator string
	if m.selectedBeads[bead.ID] {
		selectionIndicator = tuiSelectedCheckStyle.Render("●") + " "
	}

	// Session indicator - compact "P" (processing) shown after status icon
	var sessionIndicator string
	if m.activeBeadSessions[bead.ID] {
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
	styledID := issueIDStyle.Render(bead.ID)

	// Short type indicator with color
	var styledType string
	switch bead.Type {
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
		prefixLen = 3 + len(bead.ID) + 1 + 3 + len(bead.Type) + 3 // icon + ID + space + [P# type] + spaces
	} else {
		prefixLen = 3 + len(bead.ID) + 3 // icon + ID + type letter + spaces
	}
	if bead.assignedWorkID != "" {
		prefixLen += len(bead.assignedWorkID) + 3 // [work-id] + space
	}
	if bead.treeDepth > 0 {
		prefixLen += len(bead.treePrefixPattern)
	}

	// Truncate title to fit on one line
	title := bead.Title
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
		line = fmt.Sprintf("%s%s%s%s %s [P%d %s] %s%s", selectionIndicator, treePrefix, workIndicator, icon, styledID, bead.Priority, bead.Type, sessionIndicator, title)
	} else {
		line = fmt.Sprintf("%s%s%s%s %s %s%s %s", selectionIndicator, treePrefix, workIndicator, icon, styledID, styledType, sessionIndicator, title)
	}

	// For selected/hovered lines, build plain text version to avoid ANSI code conflicts
	if i == m.beadsCursor || i == m.hoveredIssue {
		// Get type letter for compact display
		var typeLetter string
		switch bead.Type {
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
		if m.selectedBeads[bead.ID] {
			plainSelectionIndicator = "● "
		}

		// Build session indicator (plain text) - compact "P" after status icon
		var plainSessionIndicator string
		if m.activeBeadSessions[bead.ID] {
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
			plainLine = fmt.Sprintf("%s%s%s%s %s [P%d %s] %s%s", plainSelectionIndicator, plainTreePrefix, plainWorkIndicator, icon, bead.ID, bead.Priority, bead.Type, plainSessionIndicator, title)
		} else {
			plainLine = fmt.Sprintf("%s%s%s%s %s %s%s %s", plainSelectionIndicator, plainTreePrefix, plainWorkIndicator, icon, bead.ID, typeLetter, plainSessionIndicator, title)
		}

		// Pad to fill width
		visWidth := lipgloss.Width(plainLine)
		if visWidth < availableWidth {
			plainLine += strings.Repeat(" ", availableWidth-visWidth)
		}

		if i == m.beadsCursor {
			// Use yellow background for newly created beads, regular blue for others
			if _, isNew := m.newBeads[bead.ID]; isNew {
				newSelectedStyle := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("0")).   // Black text
					Background(lipgloss.Color("226")) // Yellow background
				return newSelectedStyle.Render(plainLine)
			}
			return tuiSelectedStyle.Render(plainLine)
		}

		// Hover style - also check for new beads
		if _, isNew := m.newBeads[bead.ID]; isNew {
			newHoverStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).   // Black text
				Background(lipgloss.Color("228")). // Lighter yellow
				Bold(true)
			return newHoverStyle.Render(plainLine)
		}
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

	// Style new beads - apply yellow only to the title
	if _, isNew := m.newBeads[bead.ID]; isNew {
		yellowTitle := tuiNewBeadStyle.Render(title)

		var newLine string
		if m.beadsExpanded {
			newLine = fmt.Sprintf("%s%s%s%s %s [P%d %s] %s%s", selectionIndicator, treePrefix, workIndicator, icon, styledID, bead.Priority, bead.Type, sessionIndicator, yellowTitle)
		} else {
			newLine = fmt.Sprintf("%s%s%s%s %s %s%s %s", selectionIndicator, treePrefix, workIndicator, icon, styledID, styledType, sessionIndicator, yellowTitle)
		}

		return newLine
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
  A             Add issue to existing work
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
