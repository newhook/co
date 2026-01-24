package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/newhook/co/internal/db"
)

// WorkOverviewPanel renders the left side of the work details view.
// It displays the work header, branch info, progress, orchestrator health,
// and a selectable list of tasks and unassigned beads.
type WorkOverviewPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Data
	focusedWork         *workProgress
	selectedIndex       int  // 0 = root issue, 1+ = tasks, N+ = unassigned beads
	hoveredIndex        int  // -1 = none, 0 = root issue, 1+ = tasks/unassigned beads
	orchestratorHealthy bool // Whether the orchestrator process is running
}

// NewWorkOverviewPanel creates a new WorkOverviewPanel
func NewWorkOverviewPanel() *WorkOverviewPanel {
	return &WorkOverviewPanel{
		width:        40,
		height:       20,
		hoveredIndex: -1, // No item hovered initially
	}
}

// SetSize updates the panel dimensions
func (p *WorkOverviewPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates the focus state
func (p *WorkOverviewPanel) SetFocus(focused bool) {
	p.focused = focused
}

// SetFocusedWork updates the focused work, preserving selection if valid
func (p *WorkOverviewPanel) SetFocusedWork(focusedWork *workProgress) {
	p.focusedWork = focusedWork
	// Validate current selection still exists
	if focusedWork != nil {
		// 0 = root, 1..n = tasks, n+1..m = unassigned beads
		maxIndex := len(focusedWork.tasks) + len(focusedWork.unassignedBeads)
		if p.selectedIndex > maxIndex {
			p.selectedIndex = 0 // Reset to root issue
		}
	} else {
		p.selectedIndex = 0
	}
}

// SetOrchestratorHealth updates the orchestrator health status
func (p *WorkOverviewPanel) SetOrchestratorHealth(healthy bool) {
	p.orchestratorHealthy = healthy
}

// IsOrchestratorHealthy returns whether the orchestrator is running
func (p *WorkOverviewPanel) IsOrchestratorHealthy() bool {
	return p.orchestratorHealthy
}

// GetSelectedIndex returns the currently selected index (0 = root issue, 1+ = tasks)
func (p *WorkOverviewPanel) GetSelectedIndex() int {
	return p.selectedIndex
}

// SetSelectedIndex sets the selected index
func (p *WorkOverviewPanel) SetSelectedIndex(idx int) {
	p.selectedIndex = idx
}

// SetHoveredItem updates which item is hovered
func (p *WorkOverviewPanel) SetHoveredItem(index int) {
	p.hoveredIndex = index
}

// GetHoveredItem returns the currently hovered item index
func (p *WorkOverviewPanel) GetHoveredItem() int {
	return p.hoveredIndex
}

// GetFocusedWork returns the currently focused work, or nil if none
func (p *WorkOverviewPanel) GetFocusedWork() *workProgress {
	return p.focusedWork
}

// GetSelectedTaskID returns the currently selected task ID, or empty if root issue is selected
func (p *WorkOverviewPanel) GetSelectedTaskID() string {
	if p.selectedIndex == 0 || p.focusedWork == nil {
		return ""
	}
	taskIdx := p.selectedIndex - 1
	if taskIdx >= 0 && taskIdx < len(p.focusedWork.tasks) {
		return p.focusedWork.tasks[taskIdx].task.ID
	}
	return ""
}

// GetSelectedBeadIDs returns the bead IDs that should be shown based on current selection.
// - If root issue is selected (index 0): returns all work beads (root + dependents)
// - If a task is selected: returns only the beads assigned to that task
// - If an unassigned bead is selected: returns just that bead's ID
// Returns nil if no work is focused.
func (p *WorkOverviewPanel) GetSelectedBeadIDs() []string {
	if p.focusedWork == nil {
		return nil
	}

	if p.selectedIndex == 0 {
		// Root issue selected - return all work beads
		var beadIDs []string
		for _, bp := range p.focusedWork.workBeads {
			beadIDs = append(beadIDs, bp.id)
		}
		return beadIDs
	}

	tasksEndIdx := 1 + len(p.focusedWork.tasks)

	// Task selected - return only task's beads
	taskIdx := p.selectedIndex - 1
	if taskIdx >= 0 && taskIdx < len(p.focusedWork.tasks) {
		var beadIDs []string
		for _, bp := range p.focusedWork.tasks[taskIdx].beads {
			beadIDs = append(beadIDs, bp.id)
		}
		return beadIDs
	}

	// Unassigned bead selected - return just that bead
	unassignedIdx := p.selectedIndex - tasksEndIdx
	if unassignedIdx >= 0 && unassignedIdx < len(p.focusedWork.unassignedBeads) {
		return []string{p.focusedWork.unassignedBeads[unassignedIdx].id}
	}

	return nil
}

// IsTaskSelected returns true if a task is currently selected (vs root issue)
func (p *WorkOverviewPanel) IsTaskSelected() bool {
	return p.selectedIndex > 0
}

// SetSelectedTaskID sets selection to the task with given ID
func (p *WorkOverviewPanel) SetSelectedTaskID(id string) {
	if p.focusedWork == nil {
		return
	}
	for i, task := range p.focusedWork.tasks {
		if task.task.ID == id {
			p.selectedIndex = i + 1 // +1 because 0 is root issue
			return
		}
	}
}

// NavigateUp moves selection to the previous item
func (p *WorkOverviewPanel) NavigateUp() {
	if p.focusedWork == nil {
		return
	}
	if p.selectedIndex > 0 {
		p.selectedIndex--
	}
}

// NavigateDown moves selection to the next item
func (p *WorkOverviewPanel) NavigateDown() {
	if p.focusedWork == nil {
		return
	}
	// 0 = root, 1..n = tasks, n+1..m = unassigned beads
	maxIndex := len(p.focusedWork.tasks) + len(p.focusedWork.unassignedBeads)
	if p.selectedIndex < maxIndex {
		p.selectedIndex++
	}
}

// Render returns the left panel content
func (p *WorkOverviewPanel) Render(panelHeight, panelWidth int) string {
	var content strings.Builder

	if p.focusedWork == nil {
		content.WriteString("Loading work details...")
		return content.String()
	}

	// Account for padding (tuiPanelStyle has Padding(0, 1) = 2 chars total)
	contentWidth := panelWidth - 2

	// Work header (1 line)
	workHeader := fmt.Sprintf("%s %s", statusIcon(p.focusedWork.work.Status), p.focusedWork.work.ID)
	if p.focusedWork.work.Name != "" {
		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
		// Calculate available space for name
		maxNameLen := contentWidth - 4 - len(p.focusedWork.work.ID)

		// Add creation time (if it will fit)
		var timeStr string
		if p.focusedWork.work.CreatedAt.Unix() > 0 {
			timeAgo := time.Since(p.focusedWork.work.CreatedAt)
			if timeAgo.Hours() < 1 {
				timeStr = fmt.Sprintf(" (%dm ago)", int(timeAgo.Minutes()))
			} else if timeAgo.Hours() < 24 {
				timeStr = fmt.Sprintf(" (%dh ago)", int(timeAgo.Hours()))
			} else {
				days := int(timeAgo.Hours() / 24)
				timeStr = fmt.Sprintf(" (%dd ago)", days)
			}
			maxNameLen -= len(timeStr)
		}

		if maxNameLen > 0 {
			workHeader += " " + nameStyle.Render(ansi.Truncate(p.focusedWork.work.Name, maxNameLen, "..."))
		}

		// Add time string at the end
		if timeStr != "" {
			timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			workHeader += timeStyle.Render(timeStr)
		}
	} else {
		// If no name, just show creation time
		if p.focusedWork.work.CreatedAt.Unix() > 0 {
			timeAgo := time.Since(p.focusedWork.work.CreatedAt)
			var timeStr string
			if timeAgo.Hours() < 1 {
				timeStr = fmt.Sprintf(" (%dm ago)", int(timeAgo.Minutes()))
			} else if timeAgo.Hours() < 24 {
				timeStr = fmt.Sprintf(" (%dh ago)", int(timeAgo.Hours()))
			} else {
				days := int(timeAgo.Hours() / 24)
				timeStr = fmt.Sprintf(" (%dd ago)", days)
			}
			timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			workHeader += timeStyle.Render(timeStr)
		}
	}
	content.WriteString(workHeader + "\n")
	// Branch info (1 line) - "Branch: " is 8 chars
	fmt.Fprintf(&content, "Branch: %s\n", ansi.Truncate(p.focusedWork.work.BranchName, contentWidth-8, "..."))

	// Progress percentage and warnings (1 line)
	var progressLine strings.Builder

	// Calculate progress
	completedTasks := 0
	for _, task := range p.focusedWork.tasks {
		if task.task.Status == db.StatusCompleted {
			completedTasks++
		}
	}
	percentage := 0
	if len(p.focusedWork.tasks) > 0 {
		percentage = (completedTasks * 100) / len(p.focusedWork.tasks)
	}

	// Progress percentage
	progressStyle := lipgloss.NewStyle().Bold(true)
	if percentage == 100 {
		progressStyle = progressStyle.Foreground(lipgloss.Color("82"))
	} else if percentage >= 75 {
		progressStyle = progressStyle.Foreground(lipgloss.Color("226"))
	} else if percentage >= 50 {
		progressStyle = progressStyle.Foreground(lipgloss.Color("214"))
	} else {
		progressStyle = progressStyle.Foreground(lipgloss.Color("247"))
	}
	progressLine.WriteString("Progress: ")
	progressLine.WriteString(progressStyle.Render(fmt.Sprintf("%d%%", percentage)))
	progressLine.WriteString(fmt.Sprintf(" (%d/%d tasks)", completedTasks, len(p.focusedWork.tasks)))

	// Warning badges
	if p.focusedWork.unassignedBeadCount > 0 {
		warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		progressLine.WriteString("  ")
		progressLine.WriteString(warningStyle.Render(fmt.Sprintf("⚠ %d unassigned", p.focusedWork.unassignedBeadCount)))
	}
	if p.focusedWork.feedbackCount > 0 {
		alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		progressLine.WriteString("  ")
		progressLine.WriteString(alertStyle.Render("feedback"))
	}

	content.WriteString(progressLine.String() + "\n")

	// Orchestrator health (1 line) - only show if work is processing or has active tasks
	hasActiveTask := false
	for _, task := range p.focusedWork.tasks {
		if task.task.Status == db.StatusProcessing {
			hasActiveTask = true
			break
		}
	}
	// Base header lines: work header (1), branch (1), progress (1), separator (1) = 4
	headerLines := 4
	if p.focusedWork.work.Status == db.StatusProcessing || hasActiveTask {
		if p.orchestratorHealthy {
			healthStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
			content.WriteString(healthStyle.Render("✓ Orchestrator running"))
		} else {
			healthStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
			content.WriteString(healthStyle.Render("✗ Orchestrator dead [o] restart"))
		}
		content.WriteString("\n")
		headerLines = 5 // Add one for orchestrator health line
	}

	// Separator (1 line)
	content.WriteString(strings.Repeat("─", contentWidth))
	content.WriteString("\n")
	availableLines := max(panelHeight-headerLines-1, 1)

	// Total items: 1 root issue + n tasks + unassigned beads (if any)
	totalItems := 1 + len(p.focusedWork.tasks) + len(p.focusedWork.unassignedBeads)

	// Calculate scroll window
	startIdx := 0
	if p.selectedIndex >= availableLines && availableLines > 0 {
		startIdx = max(0, p.selectedIndex-availableLines/2)
	}
	endIdx := min(startIdx+availableLines, totalItems)

	// Render visible items (use contentWidth which accounts for padding)
	// Layout: index 0 = root issue, 1..n = tasks, n+1..m = unassigned beads
	tasksEndIdx := 1 + len(p.focusedWork.tasks)
	for i := startIdx; i < endIdx; i++ {
		if i == 0 {
			// Root issue
			p.renderRootIssueLine(&content, contentWidth)
		} else if i < tasksEndIdx {
			// Task (index i-1 in tasks array)
			taskIdx := i - 1
			if taskIdx < len(p.focusedWork.tasks) {
				p.renderTaskLine(&content, taskIdx, contentWidth)
			}
		} else {
			// Unassigned bead (index i - tasksEndIdx in unassignedBeads array)
			unassignedIdx := i - tasksEndIdx
			if unassignedIdx < len(p.focusedWork.unassignedBeads) {
				p.renderUnassignedBeadLine(&content, unassignedIdx, contentWidth)
			}
		}
	}

	// Scroll indicator
	if totalItems > availableLines && availableLines > 0 {
		scrollInfo := fmt.Sprintf("(%d-%d of %d)", startIdx+1, endIdx, totalItems)
		content.WriteString(tuiDimStyle.Render(scrollInfo))
	}

	return content.String()
}

// renderRootIssueLine renders the root issue line
func (p *WorkOverviewPanel) renderRootIssueLine(content *strings.Builder, panelWidth int) {
	isSelected := p.selectedIndex == 0
	isHovered := p.hoveredIndex == 0

	prefix := "  "
	if isSelected {
		prefix = "► "
	}

	// Find root issue info from workBeads
	rootID := p.focusedWork.work.RootIssueID
	rootTitle := ""
	for _, bead := range p.focusedWork.workBeads {
		if bead.id == rootID {
			rootTitle = bead.title
			break
		}
	}

	// Build the icon - style depends on hover/selected state
	var issueIcon string
	if isSelected || isHovered {
		// Use plain icon when applying full line style
		issueIcon = "◆"
	} else {
		// Styled icon for normal display
		issueIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Render("◆")
	}

	// Build text portion (ID and title)
	textPortion := rootID
	if rootTitle != "" {
		// Calculate max title length: panelWidth - prefix(2) - icon(1) - spaces(2) - ID - buffer
		maxTitleLen := panelWidth - 2 - 1 - 2 - len(rootID) - 4
		if maxTitleLen > 0 {
			textPortion += " " + ansi.Truncate(rootTitle, maxTitleLen, "...")
		}
	}

	content.WriteString(prefix)
	if isSelected {
		// Full selected style on icon + text
		content.WriteString(tuiSelectedStyle.Render(issueIcon + " " + textPortion))
	} else if isHovered {
		// Orange text for hover on icon + text
		hoverStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		content.WriteString(hoverStyle.Render(issueIcon + " " + textPortion))
	} else {
		// Normal: styled icon + dim text
		content.WriteString(issueIcon + " ")
		content.WriteString(tuiDimStyle.Render(textPortion))
	}
	content.WriteString("\n")
}

// renderTaskLine renders a task line
func (p *WorkOverviewPanel) renderTaskLine(content *strings.Builder, taskIdx int, panelWidth int) {
	task := p.focusedWork.tasks[taskIdx]
	itemIndex := taskIdx + 1 // +1 because 0 is root issue

	isSelected := p.selectedIndex == itemIndex
	isHovered := p.hoveredIndex == itemIndex

	prefix := "  "
	if isSelected {
		prefix = "► "
	}

	// Status icon (plain or styled depending on hover/selected state)
	statusStr := ""
	switch task.task.Status {
	case db.StatusCompleted:
		statusStr = "✓"
	case db.StatusProcessing:
		statusStr = "●"
	case db.StatusFailed:
		statusStr = "✗"
	default:
		statusStr = "○"
	}

	// Task type
	taskType := "impl"
	switch task.task.TaskType {
	case "estimate":
		taskType = "est"
	case "review":
		taskType = "rev"
	case "pr":
		taskType = "pr"
	}

	content.WriteString(prefix)
	if isSelected {
		// Full selected style on entire line
		textContent := fmt.Sprintf("%s %s [%s]", statusStr, task.task.ID, taskType)
		content.WriteString(tuiSelectedStyle.Render(textContent))
	} else if isHovered {
		// Orange text for hover on entire line
		hoverStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		textContent := fmt.Sprintf("%s %s [%s]", statusStr, task.task.ID, taskType)
		content.WriteString(hoverStyle.Render(textContent))
	} else {
		// Normal: styled status icon + dim text
		var statusStyle lipgloss.Style
		switch task.task.Status {
		case db.StatusCompleted:
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
		case db.StatusProcessing:
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		case db.StatusFailed:
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		default:
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
		}
		content.WriteString(statusStyle.Render(statusStr))
		content.WriteString(" ")
		content.WriteString(tuiDimStyle.Render(fmt.Sprintf("%s [%s]", task.task.ID, taskType)))
	}
	content.WriteString("\n")
}

// renderUnassignedBeadLine renders an unassigned bead line
func (p *WorkOverviewPanel) renderUnassignedBeadLine(content *strings.Builder, beadIdx, panelWidth int) {
	if beadIdx >= len(p.focusedWork.unassignedBeads) {
		return
	}

	bead := p.focusedWork.unassignedBeads[beadIdx]
	tasksEndIdx := 1 + len(p.focusedWork.tasks)
	itemIdx := tasksEndIdx + beadIdx

	isSelected := p.selectedIndex == itemIdx
	isHovered := p.hoveredIndex == itemIdx

	prefix := "  "
	if isSelected {
		prefix = "► "
	}

	// Build text portion (ID and title)
	textPortion := bead.id
	if bead.title != "" {
		// Calculate max title length: panelWidth - prefix(2) - icon(1) - spaces(2) - ID - buffer
		maxTitleLen := panelWidth - 2 - 1 - 2 - len(bead.id) - 4
		if maxTitleLen > 0 {
			textPortion += " " + ansi.Truncate(bead.title, maxTitleLen, "...")
		}
	}

	content.WriteString(prefix)
	if isSelected {
		// Full selected style on icon + text
		content.WriteString(tuiSelectedStyle.Render("○ " + textPortion))
	} else if isHovered {
		// Orange text for hover on icon + text
		hoverStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		content.WriteString(hoverStyle.Render("○ " + textPortion))
	} else {
		// Normal: orange icon for unassigned + dim text
		beadIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("○")
		content.WriteString(beadIcon + " ")
		content.WriteString(tuiDimStyle.Render(textPortion))
	}
	content.WriteString("\n")
}

// DetectClickedItem determines which item was clicked and returns its index
func (p *WorkOverviewPanel) DetectClickedItem(x, y, totalPanelHeight int) int {
	if p.focusedWork == nil {
		return -1
	}

	// Check if click is within left panel bounds (where items are displayed)
	if x > p.width+2 {
		return -1
	}

	// Check if y is within panel height
	if y >= totalPanelHeight {
		return -1
	}

	// Calculate header lines - this matches Render logic
	// Header: work header (1) + branch (1) + progress (1) + separator (1) = 4
	// Plus orchestrator line (1) if work is processing or has active tasks
	headerLines := 4
	hasActiveTask := false
	for _, task := range p.focusedWork.tasks {
		if task.task.Status == db.StatusProcessing {
			hasActiveTask = true
			break
		}
	}
	if p.focusedWork.work.Status == db.StatusProcessing || hasActiveTask {
		headerLines = 5
	}

	// Layout in work panel:
	// Y=0: Top border
	// Y=1: Panel title "Work"
	// Y=2+: Header lines (work header, branch, progress, [orchestrator], separator)
	// Y=2+headerLines: First item
	firstItemY := 2 + headerLines

	// Calculate available lines (same logic as Render)
	contentHeight := totalPanelHeight - 2 // excludes border
	availableContentLines := max(contentHeight-3, 1)
	availableLines := max(availableContentLines-headerLines, 1)

	if y < firstItemY || y >= firstItemY+availableLines {
		return -1
	}

	// Total items: root issue + tasks + unassigned beads
	totalItems := 1 + len(p.focusedWork.tasks) + len(p.focusedWork.unassignedBeads)

	// Calculate scroll window (same as Render)
	startIdx := 0
	if p.selectedIndex >= availableLines && availableLines > 0 {
		startIdx = max(0, p.selectedIndex-availableLines/2)
	}

	lineIndex := y - firstItemY
	itemIndex := startIdx + lineIndex

	if itemIndex >= 0 && itemIndex < totalItems {
		return itemIndex
	}

	return -1
}

// DetectHoveredItem determines which item is at the given Y position for hover detection.
// Returns the absolute index (0 = root, 1+ = tasks, N+ = unassigned beads), or -1 if not over an item.
func (p *WorkOverviewPanel) DetectHoveredItem(x, y, totalPanelHeight int) int {
	// Reuse click detection logic since hover uses the same boundaries
	return p.DetectClickedItem(x, y, totalPanelHeight)
}
