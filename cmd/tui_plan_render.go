package cmd

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/logging"
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
	// Calculate heights for split view
	// Note: m.height has already been adjusted for tabs bar in View()
	totalHeight := m.height - 1 // -1 for status bar

	// Calculate work panel height
	// calculateWorkPanelHeight returns content height (10-23)
	// Add 2 for border to get total panel height
	calcBase := m.calculateWorkPanelHeight()
	workPanelHeight := calcBase + 2
	planPanelHeight := totalHeight - workPanelHeight

	// Update work details panel size and render (same pattern as IssuesPanel)
	m.workDetails.SetSize(m.width, workPanelHeight)
	workPanel := m.workDetails.RenderWithPanel(workPanelHeight)

	// === Render Plan Mode Panel (Bottom) ===
	// Update issues and details panel sizes for the reduced height
	totalContentWidth := m.width - 4
	issuesWidth := int(float64(totalContentWidth) * m.columnRatio)
	detailsWidth := totalContentWidth - issuesWidth

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

	// Combine plan mode columns (panels have their own borders)
	planSection := lipgloss.JoinHorizontal(lipgloss.Top, issuesPanel, detailsPanel)

	// Combine everything vertically (panel borders provide visual separation)
	return lipgloss.JoinVertical(lipgloss.Left, workPanel, planSection)
}

// renderTwoColumnLayout renders the issues and details panels side-by-side
func (m *planModel) renderTwoColumnLayout() string {
	// Check if a work is focused - if so, render split view
	if m.focusedWorkID != "" {
		return m.renderFocusedWorkSplitView()
	}

	// Calculate content height
	// Note: m.height has already been adjusted for tabs bar in View()
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

	// Combine columns horizontally (panels have their own borders)
	return lipgloss.JoinHorizontal(lipgloss.Top, issuesPanel, rightPanel)
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
	issuesWidth := int(float64(totalContentWidth) * m.columnRatio)

	// Check if mouse is in the issues panel (left side)
	// Be generous with the boundary - include the entire panel width plus some margin
	maxIssueX := issuesWidth + 2 // Include panel width and padding
	if m.mouseX > maxIssueX {
		return -1
	}

	// Account for tabs bar at the top
	tabsBarHeight := m.workTabsBar.Height()

	// Calculate the Y offset for the issues panel based on focused work mode
	issuesPanelStartY := tabsBarHeight
	var contentHeight int
	if m.focusedWorkID != "" {
		// In focused work mode, the issues panel is below the work panel
		// Use calculateWorkPanelHeightForEvents since this is event handling (original m.height)
		totalHeight := m.height - 1 - tabsBarHeight              // -1 for status bar, - tabs bar
		workPanelHeight := m.calculateWorkPanelHeightForEvents() + 2 // +2 for border
		issuesPanelStartY = tabsBarHeight + workPanelHeight
		contentHeight = totalHeight - workPanelHeight
	} else {
		contentHeight = m.height - 1 - tabsBarHeight // -1 for status bar
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
	issuesContentLines := contentHeight - 3      // -3 for border (2) + title (1)
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

// calculateWorkPanelHeight returns the height of the work details panel content.
// This returns the content height, not including the panel border (+2).
// NOTE: This function assumes m.height has been adjusted for tabs bar (as done in View()).
// For event handling where m.height is the original value, use calculateWorkPanelHeightForEvents().
func (m *planModel) calculateWorkPanelHeight() int {
	// Calculate based on available height
	// Note: m.height has already been adjusted for tabs bar in View()
	availableHeight := m.height - 1 // -1 for status bar
	dropdownHeight := int(float64(availableHeight) * 0.4)
	if dropdownHeight < 10 {
		dropdownHeight = 10
	} else if dropdownHeight > 23 {
		dropdownHeight = 23
	}
	return dropdownHeight
}

// calculateWorkPanelHeightForEvents returns the work panel height for event handling.
// Unlike calculateWorkPanelHeight(), this function works with the original m.height
// (not temporarily reduced for tabs bar) which is the case during event handling.
func (m *planModel) calculateWorkPanelHeightForEvents() int {
	tabsBarHeight := m.workTabsBar.Height()
	// Subtract tabs bar and status bar from original height
	availableHeight := m.height - tabsBarHeight - 1
	dropdownHeight := int(float64(availableHeight) * 0.4)
	if dropdownHeight < 10 {
		dropdownHeight = 10
	} else if dropdownHeight > 23 {
		dropdownHeight = 23
	}
	return dropdownHeight
}

// detectHoveredIssueWithOffset detects issue hover when content is offset by work panel
func (m *planModel) detectHoveredIssueWithOffset(y int, offsetHeight int) int {
	// Check if mouse X is within the issues panel
	totalContentWidth := m.width - 4
	issuesWidth := int(float64(totalContentWidth) * m.columnRatio)

	maxIssueX := issuesWidth + 2
	if m.mouseX > maxIssueX {
		return -1
	}

	// Calculate the adjusted Y position relative to the content below work panel
	// The content starts at offsetHeight
	adjustedY := y - offsetHeight

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

	// Calculate content height (reduced by work panel)
	contentHeight := m.height - offsetHeight - 1 // -1 for status bar
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
	// Delegate to the work details panel which owns the layout logic
	return m.workDetails.DetectClickedTask(x, y)
}

// detectClickedPanel determines which panel was clicked in the focused work view
// Returns "work-left", "work-right", "issues-left", "issues-right", or "" if not in a panel
func (m *planModel) detectClickedPanel(x, y int) string {
	if m.focusedWorkID == "" {
		return ""
	}

	// Calculate panel boundaries using calculateWorkPanelHeightForEvents (event handling context)
	tabsBarHeight := m.workTabsBar.Height()
	workPanelHeight := m.calculateWorkPanelHeightForEvents() + 2 // +2 for border
	halfWidth := (m.width - 4) / 2                               // Half width

	// Determine Y section (top = work, bottom = issues)
	// Account for tabs bar at the top
	workPanelEndY := tabsBarHeight + workPanelHeight
	isWorkSection := y >= tabsBarHeight && y < workPanelEndY
	isIssuesSection := y >= workPanelEndY

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
	issuesWidth := int(float64(totalContentWidth) * m.columnRatio)

	// Details panel starts after issues panel
	detailsPanelStart := issuesWidth + 2 // +2 for left margin

	// Check if mouse is in the details panel
	if x < detailsPanelStart {
		return ""
	}

	// Calculate the Y offset for the details panel based on tabs bar and focused work mode
	tabsBarHeight := m.workTabsBar.Height()
	detailsPanelStartY := tabsBarHeight
	if m.focusedWorkID != "" {
		// In focused work mode, the details panel is below the work panel
		// Use calculateWorkPanelHeightForEvents since this is event handling (original m.height)
		workPanelHeight := m.calculateWorkPanelHeightForEvents() + 2 // +2 for border
		detailsPanelStartY = tabsBarHeight + workPanelHeight
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
			// Add detailsPanelStartY to account for tabs bar and work panel (if focused).
			absoluteY := detailsPanelStartY + button.Y + 2

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
		linesBeforeButtons := 1 // header
		linesBeforeButtons += 1 // blank line
		linesBeforeButtons += 1 // issue IDs label
		linesBeforeButtons += 4 // textarea (height 4)
		linesBeforeButtons += 1 // blank line
		linesBeforeButtons += 1 // create deps checkbox
		linesBeforeButtons += 1 // update checkbox
		linesBeforeButtons += 1 // dry run checkbox
		linesBeforeButtons += 1 // blank line
		linesBeforeButtons += 1 // max depth
		linesBeforeButtons += 1 // blank line
		// Add detailsPanelStartY to account for tabs bar and work panel (if focused)
		buttonRowY := detailsPanelStartY + formStartY + linesBeforeButtons

		if y != buttonRowY {
			return ""
		}

		// Check X position for buttons
		// "  Ok  " (6 chars) + "  " (2 chars) + "Cancel" (6 chars)
		buttonAreaX := x - detailsPanelStart - 2 // -2 for panel border and padding

		if buttonAreaX >= 0 && buttonAreaX < 6 {
			return "ok"
		}
		if buttonAreaX >= 8 && buttonAreaX < 14 {
			return "cancel"
		}
		return ""
	} else {
		// Use tracked button positions from BeadFormPanel
		// This handles dynamic textarea height correctly
		dialogButtons := m.beadFormPanel.GetDialogButtons()

		// Calculate X position relative to panel content area
		buttonAreaX := x - detailsPanelStart - 2 // -2 for panel border and padding

		// Debug logging
		logging.Debug("detectDialogButton bead form",
			"x", x, "y", y,
			"detailsPanelStart", detailsPanelStart,
			"detailsPanelStartY", detailsPanelStartY,
			"buttonAreaX", buttonAreaX,
			"tabsBarHeight", tabsBarHeight,
			"focusedWorkID", m.focusedWorkID,
			"m.height", m.height,
			"numButtons", len(dialogButtons))

		// Check each tracked button region
		for _, button := range dialogButtons {
			// The Y position stored in button.Y is the line number within the content area.
			// The content starts at row 2 of the details panel (after border and title).
			// Add detailsPanelStartY to account for tabs bar and work panel (if focused).
			absoluteY := detailsPanelStartY + button.Y + 2

			logging.Debug("detectDialogButton checking button",
				"buttonID", button.ID,
				"button.Y", button.Y,
				"absoluteY", absoluteY,
				"y", y,
				"button.StartX", button.StartX,
				"button.EndX", button.EndX,
				"buttonAreaX", buttonAreaX,
				"yMatch", y == absoluteY,
				"xMatch", buttonAreaX >= button.StartX && buttonAreaX <= button.EndX)

			if y == absoluteY && buttonAreaX >= button.StartX && buttonAreaX <= button.EndX {
				return button.ID
			}
		}
		return ""
	}
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
					Foreground(lipgloss.Color("0")).  // Black text
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
  1-9           Select work by position
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

// handleMouseWheel handles mouse wheel events by routing them to the appropriate panel
// based on the mouse position. Only the panel under the mouse cursor will scroll.
func (m *planModel) handleMouseWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Debounce rapid scroll events (terminals often send 3+ events per wheel click)
	// 50ms debounce allows continuous scrolling while filtering burst events
	now := time.Now()
	if now.Sub(m.lastWheelScroll) < 50*time.Millisecond {
		return m, nil
	}
	m.lastWheelScroll = now

	// Calculate panel boundaries
	tabsBarHeight := m.workTabsBar.Height()

	// Determine scroll direction
	scrollUp := msg.Button == tea.MouseButtonWheelUp

	// Calculate panel widths
	totalContentWidth := m.width - 4
	leftPanelWidth := int(float64(totalContentWidth) * m.columnRatio)
	rightPanelStartX := leftPanelWidth + 2 // +2 for left panel border

	// If focused work mode, determine if mouse is over work details or issues panel
	if m.focusedWorkID != "" {
		workPanelHeight := m.calculateWorkPanelHeightForEvents() + 2 // +2 for border
		workPanelEndY := tabsBarHeight + workPanelHeight

		// Check if mouse is in work details area (top panel)
		if msg.Y >= tabsBarHeight && msg.Y < workPanelEndY {
			// Check if over the right panel (details)
			if msg.X >= rightPanelStartX {
				// Scroll the work details right panel (summary or task)
				return m, m.workDetails.UpdateViewport(msg)
			}
			// Over left panel (work overview) - navigate task selection
			if scrollUp {
				m.workDetails.NavigateUp()
			} else {
				m.workDetails.NavigateDown()
			}
			return m, nil
		}

		// Mouse is in issues area below work panel
		if scrollUp {
			if m.beadsCursor > 0 {
				m.beadsCursor--
			}
		} else {
			if m.beadsCursor < len(m.beadItems)-1 {
				m.beadsCursor++
			}
		}
		return m, nil
	}

	// Normal mode (no focused work) - check which panel mouse is over

	// Check if mouse is over the issues panel (left side)
	if msg.X <= leftPanelWidth+2 {
		// Issues panel - move cursor
		if scrollUp {
			if m.beadsCursor > 0 {
				m.beadsCursor--
			}
		} else {
			if m.beadsCursor < len(m.beadItems)-1 {
				m.beadsCursor++
			}
		}
		return m, nil
	}

	// Mouse is over the details panel (right side)
	// Details panel uses viewport for scrolling
	if scrollUp {
		m.detailsPanel.ScrollUp()
	} else {
		m.detailsPanel.ScrollDown()
	}
	return m, nil
}
