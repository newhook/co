package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
)

// WorkDetailsPanel renders the focused work split view showing work and task details.
type WorkDetailsPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	leftPanelFocused  bool
	rightPanelFocused bool

	// Data
	focusedWork    *workProgress
	selectedTaskID string
}

// NewWorkDetailsPanel creates a new WorkDetailsPanel
func NewWorkDetailsPanel() *WorkDetailsPanel {
	return &WorkDetailsPanel{
		width:  80,
		height: 20,
	}
}

// SetSize updates the panel dimensions
func (p *WorkDetailsPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates which side is focused
func (p *WorkDetailsPanel) SetFocus(leftFocused, rightFocused bool) {
	p.leftPanelFocused = leftFocused
	p.rightPanelFocused = rightFocused
}

// SetFocusedWork updates the focused work, preserving task selection if valid
func (p *WorkDetailsPanel) SetFocusedWork(focusedWork *workProgress) {
	p.focusedWork = focusedWork
	// Validate current selection still exists
	if p.selectedTaskID != "" && focusedWork != nil {
		found := false
		for _, task := range focusedWork.tasks {
			if task.task.ID == p.selectedTaskID {
				found = true
				break
			}
		}
		if !found {
			p.selectedTaskID = ""
		}
	}
	// Auto-select first task if none selected
	if p.selectedTaskID == "" && focusedWork != nil && len(focusedWork.tasks) > 0 {
		p.selectedTaskID = focusedWork.tasks[0].task.ID
	}
}

// GetSelectedTaskID returns the currently selected task ID
func (p *WorkDetailsPanel) GetSelectedTaskID() string {
	return p.selectedTaskID
}

// SetSelectedTaskID sets the selected task ID
func (p *WorkDetailsPanel) SetSelectedTaskID(id string) {
	p.selectedTaskID = id
}

// Render returns the work details split view
func (p *WorkDetailsPanel) Render() string {
	workPanelHeight := p.height
	halfWidth := (p.width - 4) / 2 - 1

	// === Left side: Work info and tasks list ===
	leftContent := p.renderLeftPanel(workPanelHeight, halfWidth)

	// === Right side: Task details ===
	rightContent := p.renderRightPanel(workPanelHeight, halfWidth)

	// Create the two panels with proper highlighting
	leftPanelStyle := tuiPanelStyle.Width(halfWidth).Height(workPanelHeight - 2)
	if p.leftPanelFocused {
		leftPanelStyle = leftPanelStyle.BorderForeground(lipgloss.Color("214"))
	}
	leftPanel := leftPanelStyle.Render(tuiTitleStyle.Render("Work & Tasks") + "\n" + leftContent)

	rightPanelStyle := tuiPanelStyle.Width(halfWidth).Height(workPanelHeight - 2)
	if p.rightPanelFocused {
		rightPanelStyle = rightPanelStyle.BorderForeground(lipgloss.Color("214"))
	}
	rightPanel := rightPanelStyle.Render(tuiTitleStyle.Render("Task Details") + "\n" + rightContent)

	// Combine with separator
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Height(workPanelHeight - 2).
		Render("│")

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, separator, rightPanel)
}

// renderLeftPanel renders the left panel with work info and task list
func (p *WorkDetailsPanel) renderLeftPanel(panelHeight, panelWidth int) string {
	var content strings.Builder
	workPanelContentHeight := panelHeight - 3

	if p.focusedWork == nil {
		content.WriteString("Loading work details...")
		return content.String()
	}

	// Work header
	workHeader := fmt.Sprintf("%s %s", statusIcon(p.focusedWork.work.Status), p.focusedWork.work.ID)
	if p.focusedWork.work.Name != "" {
		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
		workHeader += " " + nameStyle.Render(p.focusedWork.work.Name)
	}
	content.WriteString(workHeader + "\n")
	content.WriteString(fmt.Sprintf("Branch: %s\n",
		truncateString(p.focusedWork.work.BranchName, panelWidth-8)))

	// Progress summary
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
	} else if percentage >= 50 {
		progressStyle = progressStyle.Foreground(lipgloss.Color("214"))
	} else {
		progressStyle = progressStyle.Foreground(lipgloss.Color("247"))
	}
	content.WriteString(fmt.Sprintf("Progress: %s (%d/%d)\n",
		progressStyle.Render(fmt.Sprintf("%d%%", percentage)),
		completedTasks, len(p.focusedWork.tasks)))

	// Separator
	content.WriteString(strings.Repeat("─", panelWidth-2))
	content.WriteString("\n")

	// Tasks list header
	content.WriteString(tuiSuccessStyle.Render("Tasks:"))
	content.WriteString("\n")

	// Calculate scrollable area
	headerLines := 5
	availableLines := workPanelContentHeight - headerLines - 1

	// Auto-select first task if none selected
	if p.selectedTaskID == "" && len(p.focusedWork.tasks) > 0 {
		p.selectedTaskID = p.focusedWork.tasks[0].task.ID
	}

	// Find selected task index
	selectedIndex := -1
	for i, task := range p.focusedWork.tasks {
		if task.task.ID == p.selectedTaskID {
			selectedIndex = i
			break
		}
	}

	// Calculate scroll window
	startIdx := 0
	if selectedIndex >= availableLines && availableLines > 0 {
		startIdx = selectedIndex - availableLines/2
		if startIdx < 0 {
			startIdx = 0
		}
	}
	endIdx := startIdx + availableLines
	if endIdx > len(p.focusedWork.tasks) {
		endIdx = len(p.focusedWork.tasks)
	}

	// Render visible tasks
	for i := startIdx; i < endIdx; i++ {
		task := p.focusedWork.tasks[i]

		prefix := "  "
		style := tuiDimStyle
		if task.task.ID == p.selectedTaskID {
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
		if task.task.TaskType == "estimate" {
			taskType = "est"
		} else if task.task.TaskType == "review" {
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

	// Scroll indicator
	if len(p.focusedWork.tasks) > availableLines && availableLines > 0 {
		scrollInfo := fmt.Sprintf("(%d-%d of %d)", startIdx+1, endIdx, len(p.focusedWork.tasks))
		content.WriteString(tuiDimStyle.Render(scrollInfo))
	}

	return content.String()
}

// renderRightPanel renders the right panel with task details
func (p *WorkDetailsPanel) renderRightPanel(panelHeight, panelWidth int) string {
	var content strings.Builder

	// Find selected task
	var selectedTask *taskProgress
	if p.focusedWork != nil {
		for _, task := range p.focusedWork.tasks {
			if task.task.ID == p.selectedTaskID {
				selectedTask = task
				break
			}
		}
	}

	if selectedTask != nil {
		content.WriteString(fmt.Sprintf("ID: %s\n", selectedTask.task.ID))
		content.WriteString(fmt.Sprintf("Type: %s\n", selectedTask.task.TaskType))
		content.WriteString(fmt.Sprintf("Status: %s\n", selectedTask.task.Status))

		if selectedTask.task.ComplexityBudget > 0 {
			content.WriteString(fmt.Sprintf("Budget: %d\n", selectedTask.task.ComplexityBudget))
		}

		// Show task beads
		content.WriteString(fmt.Sprintf("\nBeads (%d):\n", len(selectedTask.beads)))
		for i, bead := range selectedTask.beads {
			if i >= 10 {
				content.WriteString(fmt.Sprintf("  ... and %d more\n", len(selectedTask.beads)-10))
				break
			}
			statusStr := "○"
			if bead.status == db.StatusCompleted {
				statusStr = "✓"
			} else if bead.status == db.StatusProcessing {
				statusStr = "●"
			}
			beadLine := fmt.Sprintf("  %s %s\n", statusStr, bead.id)
			if bead.title != "" {
				beadLine = fmt.Sprintf("  %s %s: %s\n",
					statusStr,
					bead.id,
					truncateString(bead.title, panelWidth-10))
			}
			content.WriteString(beadLine)
		}

		// Show error if failed
		if selectedTask.task.Status == db.StatusFailed && selectedTask.task.ErrorMessage != "" {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
			content.WriteString("\n")
			content.WriteString(errorStyle.Render("Error:"))
			content.WriteString("\n")
			content.WriteString(truncateString(selectedTask.task.ErrorMessage, panelWidth-2))
		}
	} else if p.focusedWork != nil && len(p.focusedWork.tasks) == 0 {
		content.WriteString(tuiDimStyle.Render("No tasks available"))
	} else {
		content.WriteString(tuiDimStyle.Render("Select a task to view details"))
	}

	return content.String()
}

// NavigateTaskUp moves selection to the previous task
func (p *WorkDetailsPanel) NavigateTaskUp() {
	if p.focusedWork == nil || len(p.focusedWork.tasks) == 0 {
		return
	}

	currentIdx := -1
	for i, task := range p.focusedWork.tasks {
		if task.task.ID == p.selectedTaskID {
			currentIdx = i
			break
		}
	}

	if currentIdx > 0 {
		p.selectedTaskID = p.focusedWork.tasks[currentIdx-1].task.ID
	}
}

// NavigateTaskDown moves selection to the next task
func (p *WorkDetailsPanel) NavigateTaskDown() {
	if p.focusedWork == nil || len(p.focusedWork.tasks) == 0 {
		return
	}

	currentIdx := -1
	for i, task := range p.focusedWork.tasks {
		if task.task.ID == p.selectedTaskID {
			currentIdx = i
			break
		}
	}

	if currentIdx < len(p.focusedWork.tasks)-1 {
		p.selectedTaskID = p.focusedWork.tasks[currentIdx+1].task.ID
	}
}

// DetectClickedTask determines if a click is on a task
func (p *WorkDetailsPanel) DetectClickedTask(x, y, screenHeight int) string {
	if p.focusedWork == nil || len(p.focusedWork.tasks) == 0 {
		return ""
	}

	// Calculate work panel dimensions
	totalHeight := screenHeight - 1
	workPanelHeight := int(float64(totalHeight) * 0.4)
	halfWidth := (p.width - 4) / 2 - 1

	// Check if click is within work panel bounds
	if y >= workPanelHeight || x > halfWidth+2 {
		return ""
	}

	// Layout in work panel:
	// Y=0: Top border
	// Y=1: Panel title
	// Y=2-6: Work info
	// Y=7: First task
	const firstTaskY = 7
	workPanelContentHeight := workPanelHeight - 3
	headerLines := 5
	availableLines := workPanelContentHeight - headerLines - 1

	if y < firstTaskY || y >= firstTaskY+availableLines {
		return ""
	}

	// Find selected task index for scroll calculation
	selectedIndex := -1
	for i, task := range p.focusedWork.tasks {
		if task.task.ID == p.selectedTaskID {
			selectedIndex = i
			break
		}
	}

	startIdx := 0
	if selectedIndex >= availableLines && availableLines > 0 {
		startIdx = selectedIndex - availableLines/2
		if startIdx < 0 {
			startIdx = 0
		}
	}

	lineIndex := y - firstTaskY
	taskIndex := startIdx + lineIndex

	if taskIndex >= 0 && taskIndex < len(p.focusedWork.tasks) {
		return p.focusedWork.tasks[taskIndex].task.ID
	}

	return ""
}
