package cmd

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
)

// WorkDetailAction represents an action result from the work details panel
type WorkDetailAction int

const (
	WorkDetailActionNone WorkDetailAction = iota
	WorkDetailActionOpenTerminal       // Open terminal/console (t)
	WorkDetailActionOpenClaude         // Open Claude session (c)
	WorkDetailActionPlan               // Plan work (p)
	WorkDetailActionRun                // Run work (r)
	WorkDetailActionReview             // Create review task (R)
	WorkDetailActionPR                 // Create PR task (P)
	WorkDetailActionNavigateUp         // Navigate up (k/up)
	WorkDetailActionNavigateDown       // Navigate down (j/down)
	WorkDetailActionRestartOrchestrator // Restart orchestrator (o)
)

// WorkDetailsPanel renders the focused work split view showing work and task details.
type WorkDetailsPanel struct {
	// Dimensions
	width       int
	height      int
	columnRatio float64 // Ratio of left column width (0.0-1.0), synced with issues panel

	// Focus state
	leftPanelFocused  bool
	rightPanelFocused bool

	// Data
	focusedWork         *workProgress
	selectedIndex       int  // 0 = root issue, 1+ = tasks (task index + 1)
	orchestratorHealthy bool // Whether the orchestrator process is running
}

// NewWorkDetailsPanel creates a new WorkDetailsPanel
func NewWorkDetailsPanel() *WorkDetailsPanel {
	return &WorkDetailsPanel{
		width:       80,
		height:      20,
		columnRatio: 0.4, // Default 40/60 split to match issues panel
	}
}

// SetSize updates the panel dimensions
func (p *WorkDetailsPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetColumnRatio sets the column width ratio to match the issues panel
func (p *WorkDetailsPanel) SetColumnRatio(ratio float64) {
	p.columnRatio = ratio
}

// SetFocus updates which side is focused
func (p *WorkDetailsPanel) SetFocus(leftFocused, rightFocused bool) {
	p.leftPanelFocused = leftFocused
	p.rightPanelFocused = rightFocused
}

// SetFocusedWork updates the focused work, preserving selection if valid
func (p *WorkDetailsPanel) SetFocusedWork(focusedWork *workProgress) {
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
func (p *WorkDetailsPanel) SetOrchestratorHealth(healthy bool) {
	p.orchestratorHealthy = healthy
}

// IsOrchestratorHealthy returns whether the orchestrator is running
func (p *WorkDetailsPanel) IsOrchestratorHealthy() bool {
	return p.orchestratorHealthy
}

// GetSelectedIndex returns the currently selected index (0 = root issue, 1+ = tasks)
func (p *WorkDetailsPanel) GetSelectedIndex() int {
	return p.selectedIndex
}

// GetFocusedWork returns the currently focused work, or nil if none
func (p *WorkDetailsPanel) GetFocusedWork() *workProgress {
	return p.focusedWork
}

// SetSelectedIndex sets the selected index
func (p *WorkDetailsPanel) SetSelectedIndex(idx int) {
	p.selectedIndex = idx
}

// GetSelectedTaskID returns the currently selected task ID, or empty if root issue is selected
func (p *WorkDetailsPanel) GetSelectedTaskID() string {
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
func (p *WorkDetailsPanel) GetSelectedBeadIDs() []string {
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
func (p *WorkDetailsPanel) IsTaskSelected() bool {
	return p.selectedIndex > 0
}

// SetSelectedTaskID sets selection to the task with given ID
func (p *WorkDetailsPanel) SetSelectedTaskID(id string) {
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

// Render returns the work details split view (uses p.height from SetSize)
func (p *WorkDetailsPanel) Render() string {
	return p.RenderWithPanel(p.height)
}

// RenderWithPanel returns the work details split view with the given total height
// This matches the IssuesPanel.RenderWithPanel pattern exactly
func (p *WorkDetailsPanel) RenderWithPanel(contentHeight int) string {
	// Ensure minimum content height to prevent layout issues
	if contentHeight < 6 {
		contentHeight = 6
	}

	// Calculate column widths using the same formula as issues panel
	totalContentWidth := p.width - 4
	leftWidth := int(float64(totalContentWidth) * p.columnRatio)
	rightWidth := totalContentWidth - leftWidth

	// Content lines available inside each sub-panel (excluding border and title)
	// Same formula as IssuesPanel: contentHeight - 3 for border (2) + title (1)
	availableContentLines := contentHeight - 3
	if availableContentLines < 1 {
		availableContentLines = 1
	}

	// === Left side: Work info and items list ===
	leftContent := p.renderLeftPanel(availableContentLines, leftWidth)

	// === Right side: Selected item details ===
	rightContent := p.renderRightPanel(availableContentLines, rightWidth)

	// Pad or truncate content to exactly fit availableContentLines
	leftContent = padOrTruncateLines(leftContent, availableContentLines)
	rightContent = padOrTruncateLines(rightContent, availableContentLines)

	// Create the two panels with fixed height (matching IssuesPanel pattern exactly)
	// IssuesPanel uses: Height(contentHeight - 2)
	leftPanelStyle := tuiPanelStyle.Width(leftWidth).Height(contentHeight - 2)
	if p.leftPanelFocused {
		leftPanelStyle = leftPanelStyle.BorderForeground(lipgloss.Color("214"))
	}
	leftPanel := leftPanelStyle.Render(tuiTitleStyle.Render("Work") + "\n" + leftContent)

	rightPanelStyle := tuiPanelStyle.Width(rightWidth).Height(contentHeight - 2)
	if p.rightPanelFocused {
		rightPanelStyle = rightPanelStyle.BorderForeground(lipgloss.Color("214"))
	}
	rightPanel := rightPanelStyle.Render(tuiTitleStyle.Render("Details") + "\n" + rightContent)

	// Combine panels horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
}

// padOrTruncateLines ensures the content has exactly targetLines lines
func padOrTruncateLines(content string, targetLines int) string {
	// Ensure targetLines is at least 1 to prevent issues
	if targetLines < 1 {
		targetLines = 1
	}

	lines := strings.Split(content, "\n")
	// Remove trailing empty line if present (from trailing \n)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if len(lines) > targetLines {
		// Truncate
		lines = lines[:targetLines]
	} else if len(lines) < targetLines {
		// Pad with empty lines
		for len(lines) < targetLines {
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}

// renderLeftPanel renders the left panel with root issue and task list
func (p *WorkDetailsPanel) renderLeftPanel(panelHeight, panelWidth int) string {
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
		// Truncate name to fit: contentWidth - status icon (2) - space (1) - workID - space (1)
		maxNameLen := contentWidth - 4 - len(p.focusedWork.work.ID)
		if maxNameLen > 0 {
			workHeader += " " + nameStyle.Render(truncateString(p.focusedWork.work.Name, maxNameLen))
		}
	}
	content.WriteString(workHeader + "\n")
	// Branch info (1 line) - "Branch: " is 8 chars
	fmt.Fprintf(&content, "Branch: %s\n", truncateString(p.focusedWork.work.BranchName, contentWidth-8))

	// Orchestrator health (1 line) - only show if work is processing or has active tasks
	hasActiveTask := false
	for _, task := range p.focusedWork.tasks {
		if task.task.Status == db.StatusProcessing {
			hasActiveTask = true
			break
		}
	}
	headerLines := 3
	if p.focusedWork.work.Status == db.StatusProcessing || hasActiveTask {
		if p.orchestratorHealthy {
			healthStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
			content.WriteString(healthStyle.Render("✓ Orchestrator running"))
		} else {
			healthStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
			content.WriteString(healthStyle.Render("✗ Orchestrator dead [o] restart"))
		}
		content.WriteString("\n")
		headerLines = 4
	}

	// Separator (1 line)
	content.WriteString(strings.Repeat("─", contentWidth))
	content.WriteString("\n")
	availableLines := panelHeight - headerLines - 1
	if availableLines < 1 {
		availableLines = 1
	}

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
func (p *WorkDetailsPanel) renderRootIssueLine(content *strings.Builder, panelWidth int) {
	prefix := "  "
	style := tuiDimStyle
	if p.selectedIndex == 0 {
		prefix = "► "
		style = tuiSelectedStyle
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

	issueIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Render("◆")
	line := fmt.Sprintf("%s%s %s", prefix, issueIcon, rootID)
	if rootTitle != "" {
		maxTitleLen := panelWidth - len(line) - 4
		if maxTitleLen > 0 {
			line += " " + truncateString(rootTitle, maxTitleLen)
		}
	}

	content.WriteString(style.Render(line))
	content.WriteString("\n")
}

// renderTaskLine renders a task line
func (p *WorkDetailsPanel) renderTaskLine(content *strings.Builder, taskIdx int, panelWidth int) {
	task := p.focusedWork.tasks[taskIdx]
	itemIndex := taskIdx + 1 // +1 because 0 is root issue

	prefix := "  "
	style := tuiDimStyle
	if p.selectedIndex == itemIndex {
		prefix = "► "
		style = tuiSelectedStyle
	}

	// Status icon and color
	statusStr := ""
	statusStyle := lipgloss.NewStyle()
	switch task.task.Status {
	case db.StatusCompleted:
		statusStr = "✓"
		statusStyle = statusStyle.Foreground(lipgloss.Color("82"))
	case db.StatusProcessing:
		statusStr = "●"
		statusStyle = statusStyle.Foreground(lipgloss.Color("214"))
	case db.StatusFailed:
		statusStr = "✗"
		statusStyle = statusStyle.Foreground(lipgloss.Color("196"))
	default:
		statusStr = "○"
		statusStyle = statusStyle.Foreground(lipgloss.Color("247"))
	}

	// Task type
	taskType := "impl"
	switch task.task.TaskType {
	case "estimate":
		taskType = "est"
	case "review":
		taskType = "rev"
	}

	taskLine := fmt.Sprintf("%s%s %s [%s]",
		prefix,
		statusStyle.Render(statusStr),
		task.task.ID,
		taskType)

	content.WriteString(style.Render(taskLine))
	content.WriteString("\n")
}

// renderUnassignedBeadLine renders an unassigned bead line
func (p *WorkDetailsPanel) renderUnassignedBeadLine(content *strings.Builder, beadIdx int, panelWidth int) {
	if beadIdx >= len(p.focusedWork.unassignedBeads) {
		return
	}

	bead := p.focusedWork.unassignedBeads[beadIdx]
	tasksEndIdx := 1 + len(p.focusedWork.tasks)
	itemIdx := tasksEndIdx + beadIdx

	prefix := "  "
	style := tuiDimStyle
	if p.selectedIndex == itemIdx {
		prefix = "► "
		style = tuiSelectedStyle
	}

	// Use warning color (orange) for unassigned beads
	beadIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("○")
	beadLine := fmt.Sprintf("%s%s %s", prefix, beadIcon, bead.id)
	if bead.title != "" {
		maxTitleLen := panelWidth - len(beadLine) - 4
		if maxTitleLen > 0 {
			beadLine += " " + truncateString(bead.title, maxTitleLen)
		}
	}

	content.WriteString(style.Render(beadLine))
	content.WriteString("\n")
}

// renderRightPanel renders the right panel with selected item details
func (p *WorkDetailsPanel) renderRightPanel(panelHeight, panelWidth int) string {
	var content strings.Builder

	if p.focusedWork == nil {
		content.WriteString(tuiDimStyle.Render("Loading..."))
		return content.String()
	}

	if p.selectedIndex == 0 {
		// Show root issue details
		return p.renderRootIssueDetails(panelWidth)
	}

	// Check if task or unassigned bead is selected
	tasksEndIdx := 1 + len(p.focusedWork.tasks)

	// Show task details
	taskIdx := p.selectedIndex - 1
	if taskIdx >= 0 && taskIdx < len(p.focusedWork.tasks) {
		return p.renderTaskDetails(p.focusedWork.tasks[taskIdx], panelWidth)
	}

	// Show unassigned bead details
	unassignedIdx := p.selectedIndex - tasksEndIdx
	if unassignedIdx >= 0 && unassignedIdx < len(p.focusedWork.unassignedBeads) {
		return p.renderUnassignedBeadDetails(p.focusedWork.unassignedBeads[unassignedIdx], panelWidth)
	}

	content.WriteString(tuiDimStyle.Render("Select an item to view details"))
	return content.String()
}

// renderRootIssueDetails renders details for the root issue
func (p *WorkDetailsPanel) renderRootIssueDetails(panelWidth int) string {
	var content strings.Builder

	// Account for padding (tuiPanelStyle has Padding(0, 1) = 2 chars total)
	contentWidth := panelWidth - 2

	rootID := p.focusedWork.work.RootIssueID

	// Find root bead in workBeads
	var rootBead *beadProgress
	for i := range p.focusedWork.workBeads {
		if p.focusedWork.workBeads[i].id == rootID {
			rootBead = &p.focusedWork.workBeads[i]
			break
		}
	}

	// If not found in workBeads, try unassignedBeads
	if rootBead == nil {
		for i := range p.focusedWork.unassignedBeads {
			if p.focusedWork.unassignedBeads[i].id == rootID {
				rootBead = &p.focusedWork.unassignedBeads[i]
				break
			}
		}
	}

	// Display root issue details
	if rootBead != nil {
		// Title first (truncated to fit content width with some margin)
		if rootBead.title != "" {
			titleStyle := lipgloss.NewStyle().Bold(true)
			title := truncateString(rootBead.title, contentWidth-2)
			content.WriteString(titleStyle.Render(title))
			content.WriteString("\n")
		}

		// Metadata line
		fmt.Fprintf(&content, "%s  Type: %s  P%d  %s\n",
			rootID,
			rootBead.issueType,
			rootBead.priority,
			rootBead.beadStatus)

		// Description (truncate to single line to avoid layout issues)
		// Must remove newlines before styling to prevent multi-line styled blocks
		if rootBead.description != "" {
			descOneLine := strings.ReplaceAll(rootBead.description, "\n", " ")
			desc := truncateString(descOneLine, contentWidth-2)
			content.WriteString(tuiDimStyle.Render(desc))
			content.WriteString("\n")
		}
	} else {
		// Fallback if bead not found
		fmt.Fprintf(&content, "Issue: %s\n", rootID)
		content.WriteString(tuiDimStyle.Render("(Issue details not loaded)"))
		content.WriteString("\n")
	}

	// Summary counts
	fmt.Fprintf(&content, "Beads: %d  Tasks: %d\n",
		len(p.focusedWork.workBeads),
		len(p.focusedWork.tasks))

	return content.String()
}

// renderTaskDetails renders details for a task
func (p *WorkDetailsPanel) renderTaskDetails(task *taskProgress, panelWidth int) string {
	var content strings.Builder

	// Account for padding (tuiPanelStyle has Padding(0, 1) = 2 chars total)
	contentWidth := panelWidth - 2

	content.WriteString(fmt.Sprintf("ID: %s\n", task.task.ID))
	content.WriteString(fmt.Sprintf("Type: %s\n", task.task.TaskType))
	content.WriteString(fmt.Sprintf("Status: %s\n", task.task.Status))

	if task.task.ComplexityBudget > 0 {
		content.WriteString(fmt.Sprintf("Budget: %d\n", task.task.ComplexityBudget))
	}

	// Show task beads
	content.WriteString(fmt.Sprintf("\nBeads (%d):\n", len(task.beads)))
	for i, bead := range task.beads {
		if i >= 10 {
			content.WriteString(fmt.Sprintf("  ... and %d more\n", len(task.beads)-10))
			break
		}
		statusStr := "○"
		if bead.status == db.StatusCompleted {
			statusStr = "✓"
		} else if bead.status == db.StatusProcessing {
			statusStr = "●"
		}
		beadLine := fmt.Sprintf("  %s %s", statusStr, bead.id)
		if bead.title != "" {
			// "  ○ ID: " is about 8 chars prefix
			beadLine += ": " + truncateString(bead.title, contentWidth-8-len(bead.id))
		}
		content.WriteString(beadLine + "\n")
	}

	// Show error if failed
	if task.task.Status == db.StatusFailed && task.task.ErrorMessage != "" {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		content.WriteString("\n")
		content.WriteString(errorStyle.Render("Error:"))
		content.WriteString("\n")
		content.WriteString(truncateString(task.task.ErrorMessage, contentWidth))
	}

	return content.String()
}

// renderUnassignedBeadDetails renders details for an unassigned bead
func (p *WorkDetailsPanel) renderUnassignedBeadDetails(bead beadProgress, panelWidth int) string {
	var content strings.Builder

	// Account for padding (tuiPanelStyle has Padding(0, 1) = 2 chars total)
	contentWidth := panelWidth - 2

	// Header with warning style and action hint
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	content.WriteString(warningStyle.Render("Unassigned Issue"))
	content.WriteString(" ")
	content.WriteString(tuiDimStyle.Render("[p] plan [r] run"))
	content.WriteString("\n\n")

	content.WriteString(fmt.Sprintf("ID: %s\n", bead.id))
	if bead.title != "" {
		content.WriteString(fmt.Sprintf("Title: %s\n", truncateString(bead.title, contentWidth-7)))
	}
	if bead.issueType != "" {
		content.WriteString(fmt.Sprintf("Type: %s\n", bead.issueType))
	}
	content.WriteString(fmt.Sprintf("Priority: %d\n", bead.priority))
	content.WriteString(fmt.Sprintf("Status: %s\n", bead.beadStatus))

	if bead.description != "" {
		content.WriteString("\nDescription:\n")
		content.WriteString(truncateString(bead.description, contentWidth))
	}

	return content.String()
}

// NavigateUp moves selection to the previous item
func (p *WorkDetailsPanel) NavigateUp() {
	if p.focusedWork == nil {
		return
	}
	if p.selectedIndex > 0 {
		p.selectedIndex--
	}
}

// NavigateDown moves selection to the next item
func (p *WorkDetailsPanel) NavigateDown() {
	if p.focusedWork == nil {
		return
	}
	// 0 = root, 1..n = tasks, n+1..m = unassigned beads
	maxIndex := len(p.focusedWork.tasks) + len(p.focusedWork.unassignedBeads)
	logging.Debug("NavigateDown",
		"selectedIndex", p.selectedIndex,
		"maxIndex", maxIndex,
		"tasks", len(p.focusedWork.tasks),
		"unassignedBeads", len(p.focusedWork.unassignedBeads))
	if p.selectedIndex < maxIndex {
		p.selectedIndex++
	}
}

// NavigateTaskUp is an alias for NavigateUp (for compatibility)
func (p *WorkDetailsPanel) NavigateTaskUp() {
	p.NavigateUp()
}

// NavigateTaskDown is an alias for NavigateDown (for compatibility)
func (p *WorkDetailsPanel) NavigateTaskDown() {
	p.NavigateDown()
}

// Update handles key events and returns an action.
// This follows the same pattern as WorkOverlayPanel for consistency.
func (p *WorkDetailsPanel) Update(msg tea.KeyMsg) (tea.Cmd, WorkDetailAction) {
	logging.Debug("WorkDetailsPanel.Update",
		"key", msg.String())

	// Handle navigation keys
	switch msg.String() {
	case "j", "down":
		p.NavigateDown()
		return nil, WorkDetailActionNavigateDown
	case "k", "up":
		p.NavigateUp()
		return nil, WorkDetailActionNavigateUp
	case "t":
		return nil, WorkDetailActionOpenTerminal
	case "c":
		logging.Debug("WorkDetailsPanel intercepted 'c' key")
		return nil, WorkDetailActionOpenClaude
	case "p":
		return nil, WorkDetailActionPlan
	case "r":
		logging.Debug("WorkDetailsPanel intercepted 'r' key")
		return nil, WorkDetailActionRun
	case "R":
		return nil, WorkDetailActionReview
	case "P":
		return nil, WorkDetailActionPR
	case "o":
		return nil, WorkDetailActionRestartOrchestrator
	}

	return nil, WorkDetailActionNone
}

// DetectClickedItem determines which item was clicked and returns its index
func (p *WorkDetailsPanel) DetectClickedItem(x, y int) int {
	if p.focusedWork == nil {
		return -1
	}

	// Use panel's actual dimensions (set via SetSize)
	workPanelHeight := p.height
	halfWidth := (p.width - 4) / 2 - 1

	// Check if click is within work panel bounds
	if y >= workPanelHeight || x > halfWidth+2 {
		return -1
	}

	// Layout in work panel (matching renderLeftPanel):
	// Y=0: Top border
	// Y=1: Panel title "Work"
	// Y=2: Work header (ID, status)
	// Y=3: Branch info
	// Y=4: Separator
	// Y=5: First item (root issue or task depending on scroll)
	const firstItemY = 5

	// Calculate available lines using same logic as renderLeftPanel:
	// contentHeight = p.height - 2 (lipgloss Height, excluding border)
	// availableContentHeight = contentHeight - 1 (for title)
	// headerLines = 3 (work header, branch, separator)
	// availableLines = availableContentHeight - headerLines - 1 (reserve for scroll)
	contentHeight := p.height - 2
	availableContentHeight := contentHeight - 1
	headerLines := 3
	availableLines := availableContentHeight - headerLines - 1

	if y < firstItemY || y >= firstItemY+availableLines {
		return -1
	}

	// Calculate scroll window (same as renderLeftPanel)
	totalItems := 1 + len(p.focusedWork.tasks)
	startIdx := 0
	if p.selectedIndex >= availableLines && availableLines > 0 {
		startIdx = p.selectedIndex - availableLines/2
		if startIdx < 0 {
			startIdx = 0
		}
	}

	lineIndex := y - firstItemY
	itemIndex := startIdx + lineIndex

	if itemIndex >= 0 && itemIndex < totalItems {
		return itemIndex
	}

	return -1
}

// DetectClickedTask returns the task ID if a task was clicked, empty string otherwise
func (p *WorkDetailsPanel) DetectClickedTask(x, y int) string {
	itemIndex := p.DetectClickedItem(x, y)
	if itemIndex <= 0 {
		return "" // -1 = no click, 0 = root issue
	}
	taskIdx := itemIndex - 1
	if taskIdx >= 0 && taskIdx < len(p.focusedWork.tasks) {
		return p.focusedWork.tasks[taskIdx].task.ID
	}
	return ""
}
