package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
)

// WorkSummaryPanel renders the right side of the work details view when the root issue is selected.
// It displays work overview, alerts, root issue details, and statistics.
type WorkSummaryPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Viewport for scrolling
	viewport viewport.Model

	// Data
	focusedWork *workProgress
}

// NewWorkSummaryPanel creates a new WorkSummaryPanel
func NewWorkSummaryPanel() *WorkSummaryPanel {
	vp := viewport.New(40, 20) // Initial size, will be updated
	// Mouse wheel events are handled at the top level (planModel.handleMouseWheel)
	// to ensure only the panel under the cursor scrolls
	vp.MouseWheelEnabled = false

	return &WorkSummaryPanel{
		width:    40,
		height:   20,
		viewport: vp,
	}
}

// SetSize updates the panel dimensions
func (p *WorkSummaryPanel) SetSize(width, height int) {
	p.width = width
	p.height = height

	// Update viewport dimensions
	// Calculate available lines for content (minus border and title)
	visibleLines := max(height-3, 1)

	// Set viewport size accounting for padding (2 chars total)
	p.viewport.Width = width - 2
	p.viewport.Height = visibleLines
}

// SetFocus updates the focus state
func (p *WorkSummaryPanel) SetFocus(focused bool) {
	p.focused = focused
}

// SetFocusedWork updates the focused work
func (p *WorkSummaryPanel) SetFocusedWork(focusedWork *workProgress) {
	p.focusedWork = focusedWork
	// Reset viewport scroll when switching focus
	p.viewport.SetYOffset(0)
}

// ScrollUp scrolls the content up (shows earlier content)
func (p *WorkSummaryPanel) ScrollUp() {
	p.viewport.ScrollUp(1)
}

// ScrollDown scrolls the content down (shows later content)
func (p *WorkSummaryPanel) ScrollDown() {
	p.viewport.ScrollDown(1)
}

// ScrollToTop scrolls to the beginning of the content
func (p *WorkSummaryPanel) ScrollToTop() {
	p.viewport.GotoTop()
}

// ScrollToBottom scrolls to the end of the content
func (p *WorkSummaryPanel) ScrollToBottom() {
	p.viewport.GotoBottom()
}

// GetViewport returns the viewport for external updates
func (p *WorkSummaryPanel) GetViewport() *viewport.Model {
	return &p.viewport
}

// Render returns the work summary content using the viewport
func (p *WorkSummaryPanel) Render(panelWidth int) string {
	// Get the full content first
	fullContent := p.renderFullContent(panelWidth)

	// Set the content in the viewport
	p.viewport.SetContent(fullContent)

	// The viewport will handle scrolling internally
	return p.viewport.View()
}

// renderFullContent returns the full content without scrolling applied
func (p *WorkSummaryPanel) renderFullContent(panelWidth int) string {
	var content strings.Builder

	if p.focusedWork == nil {
		content.WriteString(tuiDimStyle.Render("Loading..."))
		return content.String()
	}

	// Account for padding (tuiPanelStyle has Padding(0, 1) = 2 chars total)
	contentWidth := panelWidth - 2

	// == Work Overview Section ==
	overviewStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	content.WriteString(overviewStyle.Render("Work Overview"))
	content.WriteString("\n")
	content.WriteString(strings.Repeat("─", contentWidth))
	content.WriteString("\n")

	// Work metadata
	statusStyle := lipgloss.NewStyle()
	switch p.focusedWork.work.Status {
	case db.StatusCompleted:
		statusStyle = statusStyle.Foreground(lipgloss.Color("82"))
	case db.StatusProcessing:
		statusStyle = statusStyle.Foreground(lipgloss.Color("214"))
	case db.StatusFailed:
		statusStyle = statusStyle.Foreground(lipgloss.Color("196"))
	default:
		statusStyle = statusStyle.Foreground(lipgloss.Color("247"))
	}
	fmt.Fprintf(&content, "Status: %s\n", statusStyle.Render(p.focusedWork.work.Status))

	// PR URL (if available)
	if p.focusedWork.work.PRURL != "" {
		prStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
		fmt.Fprintf(&content, "PR: %s\n", prStyle.Render(p.focusedWork.work.PRURL))

		// PR Status section (only show if we have a PR)
		content.WriteString("\n")
		prStatusHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("141"))
		content.WriteString(prStatusHeaderStyle.Render("PR Status"))
		content.WriteString("\n")

		// CI Status
		ciStatus := p.focusedWork.ciStatus
		if ciStatus == "" {
			ciStatus = "pending"
		}
		ciIcon := "⏳"
		ciText := "Pending"
		ciColor := lipgloss.Color("226") // yellow
		switch ciStatus {
		case "success":
			ciIcon = "✓"
			ciText = "Passing"
			ciColor = lipgloss.Color("82") // green
		case "failure":
			ciIcon = "✗"
			ciText = "Failing"
			ciColor = lipgloss.Color("196") // red
		}
		ciStyle := lipgloss.NewStyle().Foreground(ciColor)
		fmt.Fprintf(&content, "  CI: %s\n", ciStyle.Render(ciIcon+" "+ciText))

		// Approval Status
		approvalStatus := p.focusedWork.approvalStatus
		if approvalStatus == "" {
			approvalStatus = "pending"
		}
		approvalIcon := "⏳"
		approvalText := "Awaiting review"
		approvalColor := lipgloss.Color("247") // dim
		switch approvalStatus {
		case "approved":
			approvalIcon = "✓"
			if len(p.focusedWork.approvers) > 0 {
				approvalText = "Approved by " + strings.Join(p.focusedWork.approvers, ", ")
			} else {
				approvalText = "Approved"
			}
			approvalColor = lipgloss.Color("82") // green
		case "changes_requested":
			approvalIcon = "⚠"
			approvalText = "Changes requested"
			approvalColor = lipgloss.Color("214") // orange
		}
		approvalStyle := lipgloss.NewStyle().Foreground(approvalColor)
		fmt.Fprintf(&content, "  Review: %s\n", approvalStyle.Render(approvalIcon+" "+approvalText))

		// Feedback (show bead IDs)
		if p.focusedWork.feedbackCount > 0 {
			feedbackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
			beadIDsStr := strings.Join(p.focusedWork.feedbackBeadIDs, ", ")
			fmt.Fprintf(&content, "  Feedback: %s\n", feedbackStyle.Render(beadIDsStr))
		}
	}

	// Creation time
	if p.focusedWork.work.CreatedAt.Unix() > 0 {
		timeAgo := time.Since(p.focusedWork.work.CreatedAt)
		var timeStr string
		if timeAgo.Hours() < 1 {
			timeStr = fmt.Sprintf("%d minutes ago", int(timeAgo.Minutes()))
		} else if timeAgo.Hours() < 24 {
			timeStr = fmt.Sprintf("%d hours ago", int(timeAgo.Hours()))
		} else {
			days := int(timeAgo.Hours() / 24)
			timeStr = fmt.Sprintf("%d days ago", days)
		}
		fmt.Fprintf(&content, "Created: %s\n", timeStr)
	}

	// Progress
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
	content.WriteString("Progress: ")
	content.WriteString(progressStyle.Render(fmt.Sprintf("%d%%", percentage)))
	fmt.Fprintf(&content, " (%d/%d tasks completed)\n", completedTasks, len(p.focusedWork.tasks))

	// Alerts/Warnings
	if p.focusedWork.unassignedBeadCount > 0 || p.focusedWork.feedbackCount > 0 {
		content.WriteString("\n")
		alertHeaderStyle := lipgloss.NewStyle().Bold(true)
		content.WriteString(alertHeaderStyle.Render("Alerts:"))
		content.WriteString("\n")

		if p.focusedWork.unassignedBeadCount > 0 {
			warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
			content.WriteString(warningStyle.Render(fmt.Sprintf("  ⚠ %d unassigned bead(s) need attention\n", p.focusedWork.unassignedBeadCount)))
		}
		if p.focusedWork.feedbackCount > 0 {
			alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
			beadIDsStr := strings.Join(p.focusedWork.feedbackBeadIDs, ", ")
			content.WriteString(alertStyle.Render(fmt.Sprintf("  ● %d pending PR feedback: %s\n", p.focusedWork.feedbackCount, beadIDsStr)))
		}
	}

	content.WriteString("\n")

	// == Root Issue Section ==
	rootID := p.focusedWork.work.RootIssueID
	issueHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	content.WriteString(issueHeaderStyle.Render("Root Issue"))
	content.WriteString("\n")
	content.WriteString(strings.Repeat("─", contentWidth))
	content.WriteString("\n")

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

		// Description (truncate to avoid layout issues)
		if rootBead.description != "" {
			content.WriteString("\n")
			content.WriteString("Description:\n")
			// Keep multiline but truncate to reasonable length
			desc := rootBead.description
			if len(desc) > 300 {
				desc = desc[:297] + "..."
			}
			content.WriteString(tuiDimStyle.Render(desc))
			content.WriteString("\n")
		}
	} else {
		// Fallback if bead not found
		fmt.Fprintf(&content, "Issue: %s\n", rootID)
		content.WriteString(tuiDimStyle.Render("(Issue details not loaded)"))
		content.WriteString("\n")
	}

	// Summary statistics
	content.WriteString("\n")
	summaryHeaderStyle := lipgloss.NewStyle().Bold(true)
	content.WriteString(summaryHeaderStyle.Render("Statistics:"))
	content.WriteString("\n")
	fmt.Fprintf(&content, "  Total Beads: %d\n", len(p.focusedWork.workBeads))
	fmt.Fprintf(&content, "  Total Tasks: %d\n", len(p.focusedWork.tasks))

	// Count task types
	var estimateTasks, implementTasks, reviewTasks int
	for _, task := range p.focusedWork.tasks {
		switch task.task.TaskType {
		case "estimate":
			estimateTasks++
		case "implement":
			implementTasks++
		case "review":
			reviewTasks++
		}
	}
	if estimateTasks > 0 || reviewTasks > 0 {
		content.WriteString("  Task Breakdown: ")
		parts := []string{}
		if estimateTasks > 0 {
			parts = append(parts, fmt.Sprintf("%d estimate", estimateTasks))
		}
		if implementTasks > 0 {
			parts = append(parts, fmt.Sprintf("%d implement", implementTasks))
		}
		if reviewTasks > 0 {
			parts = append(parts, fmt.Sprintf("%d review", reviewTasks))
		}
		content.WriteString(strings.Join(parts, ", "))
		content.WriteString("\n")
	}

	return content.String()
}
