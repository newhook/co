package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
	"github.com/newhook/co/internal/db"
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

	// Find the focused work
	var focusedWork *workProgress
	for _, work := range m.workTiles {
		if work != nil && work.work.ID == m.focusedWorkID {
			focusedWork = work
			break
		}
	}

	// === Split the Work Panel into two columns ===
	workPanelContentHeight := workPanelHeight - 3 // -3 for border and title
	halfWidth := (m.width - 4) / 2 - 1 // -4 for margins, divide by 2, -1 for separator

	// === Left side: Work info and tasks list ===
	var leftContent strings.Builder
	var selectedTask *taskProgress

	if focusedWork != nil {
		// Work header
		workHeader := fmt.Sprintf("%s %s", statusIcon(focusedWork.work.Status), focusedWork.work.ID)
		if focusedWork.work.Name != "" {
			nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
			workHeader += " " + nameStyle.Render(focusedWork.work.Name)
		}
		leftContent.WriteString(workHeader + "\n")
		leftContent.WriteString(fmt.Sprintf("Branch: %s\n",
			truncateString(focusedWork.work.BranchName, halfWidth-8)))

		// Progress summary
		completedTasks := 0
		for _, task := range focusedWork.tasks {
			if task.task.Status == db.StatusCompleted {
				completedTasks++
			}
		}
		percentage := 0
		if len(focusedWork.tasks) > 0 {
			percentage = (completedTasks * 100) / len(focusedWork.tasks)
		}

		progressStyle := lipgloss.NewStyle().Bold(true)
		if percentage == 100 {
			progressStyle = progressStyle.Foreground(lipgloss.Color("82"))
		} else if percentage >= 50 {
			progressStyle = progressStyle.Foreground(lipgloss.Color("214"))
		} else {
			progressStyle = progressStyle.Foreground(lipgloss.Color("247"))
		}
		leftContent.WriteString(fmt.Sprintf("Progress: %s (%d/%d)\n",
			progressStyle.Render(fmt.Sprintf("%d%%", percentage)),
			completedTasks, len(focusedWork.tasks)))

		// Separator
		leftContent.WriteString(strings.Repeat("─", halfWidth-2))
		leftContent.WriteString("\n")

		// Tasks list header
		leftContent.WriteString(tuiSuccessStyle.Render("Tasks:"))
		leftContent.WriteString("\n")

		// Calculate scrollable area for tasks
		headerLines := 5 // lines used for header info above
		availableLines := workPanelContentHeight - headerLines - 1

		// Auto-select first task if none selected
		if m.selectedTaskID == "" && len(focusedWork.tasks) > 0 {
			m.selectedTaskID = focusedWork.tasks[0].task.ID
		}

		// Find selected task index
		selectedIndex := -1
		for i, task := range focusedWork.tasks {
			if task.task.ID == m.selectedTaskID {
				selectedIndex = i
				selectedTask = task
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
		if endIdx > len(focusedWork.tasks) {
			endIdx = len(focusedWork.tasks)
		}

		// Render visible tasks
		for i := startIdx; i < endIdx; i++ {
			task := focusedWork.tasks[i]

			// Selection indicator
			prefix := "  "
			style := tuiDimStyle
			if task.task.ID == m.selectedTaskID {
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
			default: // pending/queued
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

			// Build task line
			taskLine := fmt.Sprintf("%s%s %s [%s]",
				prefix,
				statusStyle.Render(statusStr),
				task.task.ID,
				taskType)

			leftContent.WriteString(style.Render(taskLine))
			leftContent.WriteString("\n")
		}

		// Scroll indicator
		if len(focusedWork.tasks) > availableLines && availableLines > 0 {
			scrollInfo := fmt.Sprintf("(%d-%d of %d)", startIdx+1, endIdx, len(focusedWork.tasks))
			leftContent.WriteString(tuiDimStyle.Render(scrollInfo))
		}
	} else {
		leftContent.WriteString("Loading work details...")
	}

	// === Right side: Task details ===
	var rightContent strings.Builder

	if selectedTask != nil {
		rightContent.WriteString(fmt.Sprintf("ID: %s\n", selectedTask.task.ID))
		rightContent.WriteString(fmt.Sprintf("Type: %s\n", selectedTask.task.TaskType))
		rightContent.WriteString(fmt.Sprintf("Status: %s\n", selectedTask.task.Status))

		if selectedTask.task.ComplexityBudget > 0 {
			rightContent.WriteString(fmt.Sprintf("Budget: %d\n", selectedTask.task.ComplexityBudget))
		}

		// Show task beads
		rightContent.WriteString(fmt.Sprintf("\nBeads (%d):\n", len(selectedTask.beads)))
		for i, bead := range selectedTask.beads {
			if i >= 10 { // Show first 10 beads
				rightContent.WriteString(fmt.Sprintf("  ... and %d more\n", len(selectedTask.beads)-10))
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
					truncateString(bead.title, halfWidth-10))
			}
			rightContent.WriteString(beadLine)
		}

		// Show error if failed
		if selectedTask.task.Status == db.StatusFailed && selectedTask.task.ErrorMessage != "" {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
			rightContent.WriteString("\n")
			rightContent.WriteString(errorStyle.Render("Error:"))
			rightContent.WriteString("\n")
			rightContent.WriteString(truncateString(selectedTask.task.ErrorMessage, halfWidth-2))
		}
	} else if focusedWork != nil && len(focusedWork.tasks) == 0 {
		rightContent.WriteString(tuiDimStyle.Render("No tasks available"))
	} else {
		rightContent.WriteString(tuiDimStyle.Render("Select a task to view details"))
	}

	// Create the two panels with proper highlighting based on focus
	leftPanelStyle := tuiPanelStyle.Width(halfWidth).Height(workPanelHeight - 2)
	if m.workPanelFocused && m.activePanel == PanelLeft {
		leftPanelStyle = leftPanelStyle.BorderForeground(lipgloss.Color("214"))
	}
	leftPanel := leftPanelStyle.Render(tuiTitleStyle.Render("Work & Tasks") + "\n" + leftContent.String())

	rightPanelStyle := tuiPanelStyle.Width(halfWidth).Height(workPanelHeight - 2)
	if m.workPanelFocused && m.activePanel == PanelRight {
		rightPanelStyle = rightPanelStyle.BorderForeground(lipgloss.Color("214"))
	}
	rightPanel := rightPanelStyle.Render(tuiTitleStyle.Render("Task Details") + "\n" + rightContent.String())

	// Combine with separator
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Height(workPanelHeight - 2).
		Render("│")

	workPanel := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, separator, rightPanel)

	// === Render Plan Mode Panel (Bottom) ===
	// Calculate column widths for plan mode section
	totalContentWidth := m.width - 4 // -4 for outer margins
	separatorWidth := 3
	issuesWidth := int(float64(totalContentWidth-separatorWidth) * m.columnRatio)
	detailsWidth := totalContentWidth - separatorWidth - issuesWidth

	// Render reduced issues panel
	issuesContentLines := planPanelHeight - 3 // -3 for border (2) + title (1)
	issuesContent := m.renderIssuesList(issuesContentLines, issuesWidth)
	issuesPanelStyle := tuiPanelStyle.Width(issuesWidth).Height(planPanelHeight - 2)
	if !m.workPanelFocused && m.activePanel == PanelLeft {
		issuesPanelStyle = issuesPanelStyle.BorderForeground(lipgloss.Color("214"))
	}
	issuesPanel := issuesPanelStyle.Render(tuiTitleStyle.Render("Issues") + "\n" + issuesContent)

	// Render reduced details panel
	detailsContentLines := planPanelHeight - 3
	detailsContent := m.renderDetailsPanel(detailsContentLines, detailsWidth)
	detailsPanelStyle := tuiPanelStyle.Width(detailsWidth).Height(planPanelHeight - 2)
	if !m.workPanelFocused && m.activePanel == PanelRight {
		detailsPanelStyle = detailsPanelStyle.BorderForeground(lipgloss.Color("214"))
	}
	detailsPanel := detailsPanelStyle.Render(tuiTitleStyle.Render("Details") + "\n" + detailsContent)

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
	if m.focusFilterActive && m.focusedWorkID != "" {
		filterInfo = fmt.Sprintf("[FOCUS: %s] %s", m.focusedWorkID, filterInfo)
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
		return m.renderBeadFormContent(visibleLines, width)
	}

	// If in inline Linear import mode, render the import form instead of issue details
	if m.viewMode == ViewLinearImportInline {
		return m.renderLinearImportInlineContent(visibleLines, width)
	}

	// If in create work mode, render the work creation panel overlay
	if m.viewMode == ViewCreateWork {
		return m.renderCreateWorkInlineContent(visibleLines, width)
	}

	// If in add to work mode, render the works list inline
	if m.viewMode == ViewAddToWork {
		return m.renderAddToWorkInlineContent(visibleLines, width)
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
func (m *planModel) renderBeadFormContent(visibleLines int, width int) string {
	var content strings.Builder

	// Adapt input widths to available space (account for panel padding)
	inputWidth := width - detailsPanelPadding
	if inputWidth < 20 {
		inputWidth = 20
	}
	m.textInput.Width = inputWidth
	m.createDescTextarea.SetWidth(inputWidth)

	// Calculate dynamic height for description textarea
	// 12 accounts for: header (1), parent info (0-1), blank lines (4), title label+input (2),
	// type+priority lines (2), desc label (1), buttons+hints (2)
	descHeight := max(visibleLines-12, 4)
	m.createDescTextarea.SetHeight(descHeight)

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

	// Render Ok and Cancel buttons with hover/focus styling
	okFocused := m.createDialogFocus == 4
	cancelFocused := m.createDialogFocus == 5
	okButton := styleButtonWithHover("  Ok  ", m.hoveredDialogButton == "ok" || okFocused)
	cancelButton := styleButtonWithHover("Cancel", m.hoveredDialogButton == "cancel" || cancelFocused)
	content.WriteString(okButton + "  " + cancelButton)
	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("[Tab] Next  [Enter/Space] Select"))

	return content.String()
}

// renderLinearImportInlineContent renders the Linear import form inline in the details panel
func (m *planModel) renderLinearImportInlineContent(visibleLines int, width int) string {
	var content strings.Builder

	// Adapt textarea width to available space (account for panel padding)
	inputWidth := width - detailsPanelPadding
	if inputWidth < 20 {
		inputWidth = 20
	}
	m.linearImportInput.SetWidth(inputWidth)

	// Show focus labels
	issueIDsLabel := "Issue IDs/URLs:"
	createDepsLabel := "Create Dependencies:"
	updateLabel := "Update Existing:"
	dryRunLabel := "Dry Run:"
	maxDepthLabel := "Max Dependency Depth:"

	if m.linearImportFocus == 0 {
		issueIDsLabel = tuiValueStyle.Render("Issue IDs/URLs:") + " (one per line, Ctrl+Enter to submit)"
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
	content.WriteString(tuiLabelStyle.Render("Import from Linear (Bulk)"))
	content.WriteString("\n\n")
	content.WriteString(issueIDsLabel)
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

	// Render Ok and Cancel buttons
	okLabel := "  Ok  "
	cancelLabel := "Cancel"
	focusHint := ""

	if m.linearImportFocus == 5 {
		okLabel = tuiValueStyle.Render("[ Ok ]")
		focusHint = tuiDimStyle.Render(" (press Enter to import)")
	} else {
		okLabel = styleButtonWithHover("  Ok  ", m.hoveredDialogButton == "ok")
	}

	if m.linearImportFocus == 6 {
		cancelLabel = tuiValueStyle.Render("[Cancel]")
		focusHint = tuiDimStyle.Render(" (press Enter to cancel)")
	} else {
		cancelLabel = styleButtonWithHover("Cancel", m.hoveredDialogButton == "cancel")
	}

	content.WriteString(okLabel + "  " + cancelLabel + focusHint)
	content.WriteString("\n")

	if m.linearImporting {
		content.WriteString(tuiDimStyle.Render("Importing..."))
	} else {
		content.WriteString(tuiDimStyle.Render("[Tab] Next field  [Enter] Activate"))
	}

	return content.String()
}

// renderAddToWorkInlineContent renders the add-to-work selection inline in the details panel
func (m *planModel) renderAddToWorkInlineContent(visibleLines int, width int) string {
	var content strings.Builder

	// Collect selected beads
	var selectedBeads []beadItem
	for _, item := range m.beadItems {
		if m.selectedBeads[item.id] {
			selectedBeads = append(selectedBeads, item)
		}
	}

	// If no selected beads, use cursor bead
	if len(selectedBeads) == 0 && len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		selectedBeads = append(selectedBeads, m.beadItems[m.beadsCursor])
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
		titleStyle := tuiValueStyle.Width(width - detailsPanelPadding)
		content.WriteString(titleStyle.Render(selectedBeads[0].title))
		content.WriteString("\n")
	} else if len(selectedBeads) > 1 {
		content.WriteString(tuiDimStyle.Render("Issues:\n"))
		for i, bead := range selectedBeads {
			if i >= 5 { // Show max 5 beads
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ... and %d more\n", len(selectedBeads)-5)))
				break
			}
			content.WriteString("  ")
			content.WriteString(issueIDStyle.Render(bead.id))
			content.WriteString(": ")
			titleStyle := tuiValueStyle.Width(width - detailsPanelPadding - len(bead.id) - 4)
			content.WriteString(titleStyle.Render(bead.title))
			content.WriteString("\n")
		}
	}
	content.WriteString("\n")

	// Works list header
	content.WriteString(tuiLabelStyle.Render("Select a work:"))
	content.WriteString("\n")

	if len(m.availableWorks) == 0 {
		content.WriteString(tuiDimStyle.Render("  No available works found."))
		content.WriteString("\n")
		content.WriteString(tuiDimStyle.Render("  Create a work first with 'w'."))
	} else {
		// Calculate how many works we can show
		linesUsed := 7 // header + issue info + select header + nav hint
		maxWorks := visibleLines - linesUsed
		if maxWorks < 3 {
			maxWorks = 3
		}

		// Show works with scrolling if needed
		start := 0
		if m.worksCursor >= maxWorks {
			start = m.worksCursor - maxWorks + 1
		}
		end := min(start+maxWorks, len(m.availableWorks))

		for i := start; i < end; i++ {
			work := m.availableWorks[i]

			// Selection indicator
			var lineStyle lipgloss.Style
			prefix := "  "
			if i == m.worksCursor {
				prefix = "► "
				lineStyle = tuiSelectedStyle
			} else {
				lineStyle = tuiDimStyle
			}

			// Build work line with root issue info
			var workLine strings.Builder
			workLine.WriteString(prefix)
			workLine.WriteString(work.id)
			workLine.WriteString(" (")
			workLine.WriteString(work.status)
			workLine.WriteString(")")

			// Show root issue if available
			if work.rootIssueID != "" {
				workLine.WriteString("\n    ")
				workLine.WriteString("Root: ")
				workLine.WriteString(work.rootIssueID)
				if work.rootIssueTitle != "" {
					// Truncate title if too long
					title := work.rootIssueTitle
					maxTitleLen := width - detailsPanelPadding - 12 // account for "Root: " + ID
					if len(title) > maxTitleLen && maxTitleLen > 10 {
						title = title[:maxTitleLen-3] + "..."
					}
					workLine.WriteString(" - ")
					workLine.WriteString(title)
				}
			}

			// Show branch
			workLine.WriteString("\n    ")
			workLine.WriteString("Branch: ")
			branch := work.branch
			maxBranchLen := width - detailsPanelPadding - 12
			if len(branch) > maxBranchLen && maxBranchLen > 10 {
				branch = branch[:maxBranchLen-3] + "..."
			}
			workLine.WriteString(branch)

			if i == m.worksCursor {
				content.WriteString(lineStyle.Render(workLine.String()))
			} else {
				content.WriteString(workLine.String())
			}
			content.WriteString("\n")
		}

		// Show scroll indicator if needed
		if len(m.availableWorks) > maxWorks {
			if start > 0 {
				content.WriteString(tuiDimStyle.Render("  ↑ more above"))
				content.WriteString("\n")
			}
			if end < len(m.availableWorks) {
				content.WriteString(tuiDimStyle.Render("  ↓ more below"))
				content.WriteString("\n")
			}
		}
	}

	// Navigation help
	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("[↑↓/jk] Navigate  [Enter] Add to work  [Esc] Cancel"))

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
	AButton := styleButtonWithHover("[A]dd", m.hoveredButton == "A")
	iButton := styleButtonWithHover("[i]Import", m.hoveredButton == "i")
	pButton := styleButtonWithHover(pAction, m.hoveredButton == "p")
	helpButton := styleButtonWithHover("[?]Help", m.hoveredButton == "?")

	commands := nButton + " " + eButton + " " + aButton + " " + xButton + " " + wButton + " " + AButton + " " + iButton + " " + pButton + " " + helpButton

	// Commands plain text for width calculation
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work [A]dd [i]Import %s [?]Help", pAction)

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
	// Commands bar format: "[n]New [e]Edit [a]Child [x]Close [A]ssign [p]Plan [?]Help"
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
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work [A]dd [i]Import %s [?]Help", pAction)

	// Find positions of each button
	nIdx := strings.Index(commandsPlain, "[n]New")
	eIdx := strings.Index(commandsPlain, "[e]Edit")
	aIdx := strings.Index(commandsPlain, "[a]Child")
	xIdx := strings.Index(commandsPlain, "[x]Close")
	wIdx := strings.Index(commandsPlain, "[w]Work")
	AIdx := strings.Index(commandsPlain, "[A]dd")
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
	if AIdx >= 0 && x >= AIdx && x < AIdx+len("[A]dd") {
		return "A"
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

	// Handle ViewCreateWork using tracked button positions
	if m.viewMode == ViewCreateWork {
		// Use the button positions tracked during rendering.
		// This is the core of the mouse click detection system for dialog buttons.
		// The positions stored in m.dialogButtons are relative to the details panel
		// content area, so we need to translate the absolute mouse coordinates.
		buttonAreaX := x - detailsPanelStart

		// Check each tracked button region to see if the click falls within it
		for _, button := range m.dialogButtons {
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
			// Use yellow background for newly created beads, regular blue for others
			if _, isNew := m.newBeads[bead.id]; isNew {
				newSelectedStyle := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("0")).   // Black text
					Background(lipgloss.Color("226")) // Yellow background
				return newSelectedStyle.Render(plainLine)
			}
			return tuiSelectedStyle.Render(plainLine)
		}

		// Hover style - also check for new beads
		if _, isNew := m.newBeads[bead.id]; isNew {
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
	if _, isNew := m.newBeads[bead.id]; isNew {
		yellowTitle := tuiNewBeadStyle.Render(title)

		var newLine string
		if m.beadsExpanded {
			newLine = fmt.Sprintf("%s%s%s%s %s [P%d %s] %s%s", selectionIndicator, treePrefix, workIndicator, icon, styledID, bead.priority, bead.beadType, sessionIndicator, yellowTitle)
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
