package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
)

// ==================== Overview Rendering ====================

// renderOverviewGrid renders the main grid view in overview mode
func (m *workModel) renderOverviewGrid() string {
	if m.loading && len(m.works) == 0 {
		return "Loading workers..."
	}

	if len(m.works) == 0 {
		return m.renderEmptyGridState()
	}

	// Calculate layout: grid on left, details on right
	detailsWidth := m.width / 3
	if detailsWidth < 30 {
		detailsWidth = 30
	}
	if detailsWidth > 50 {
		detailsWidth = 50
	}
	gridWidth := m.width - detailsWidth

	// Reserve 1 line for status bar
	contentHeight := m.height - 1

	// Render grid of worker panels (with reduced width)
	grid := m.renderGridWithWidth(gridWidth, contentHeight)

	// Render details panel for selected bead
	detailsPanel := m.renderOverviewDetailsPanel(detailsWidth, contentHeight)

	// Join grid and details horizontally
	content := lipgloss.JoinHorizontal(lipgloss.Top, grid, detailsPanel)

	// Render status bar
	statusBar := m.renderOverviewStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

// renderEmptyGridState renders the empty state when no works exist
func (m *workModel) renderEmptyGridState() string {
	content := `
  No Active Workers

  Workers will appear here as grid panels.
  Each panel shows:
    - Worker name and status
    - Task list with states
    - Progress indicators

  Press 'c' to create a new work.
  Press 'Enter' on a work to zoom into task view.
`
	style := lipgloss.NewStyle().
		Padding(2, 4).
		Foreground(lipgloss.Color("247"))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		style.Render(content))
}

// renderGridWithWidth renders the grid with a specific width
func (m *workModel) renderGridWithWidth(width, height int) string {
	if len(m.works) == 0 {
		return ""
	}

	// Recalculate grid dimensions for the given width
	gridConfig := CalculateGridDimensions(len(m.works), width, height)

	var rows []string

	for row := 0; row < gridConfig.Rows; row++ {
		var rowPanels []string

		for col := 0; col < gridConfig.Cols; col++ {
			idx := row*gridConfig.Cols + col

			if idx < len(m.works) {
				panel := m.renderGridWorkerPanel(idx, gridConfig.CellWidth, gridConfig.CellHeight)
				rowPanels = append(rowPanels, panel)
			} else {
				// Empty cell
				emptyPanel := m.renderEmptyGridPanel(gridConfig.CellWidth, gridConfig.CellHeight)
				rowPanels = append(rowPanels, emptyPanel)
			}
		}

		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, rowPanels...))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderOverviewDetailsPanel renders the details panel for the selected bead in overview mode
func (m *workModel) renderOverviewDetailsPanel(width, height int) string {
	var content strings.Builder

	title := tuiTitleStyle.Render("Details")
	content.WriteString(title)
	content.WriteString("\n\n")

	// Check if we have a selected worker with beads
	if len(m.works) == 0 || m.worksCursor >= len(m.works) {
		content.WriteString(tuiDimStyle.Render("No worker selected"))
		return tuiPanelStyle.Width(width - 2).Height(height - 2).Render(content.String())
	}

	wp := m.works[m.worksCursor]

	// Show worker info
	content.WriteString(tuiLabelStyle.Render("Worker: "))
	if wp.work.Name != "" {
		content.WriteString(tuiValueStyle.Render(wp.work.Name))
		content.WriteString(tuiDimStyle.Render(fmt.Sprintf(" (%s)", wp.work.ID)))
	} else {
		content.WriteString(tuiValueStyle.Render(wp.work.ID))
	}
	content.WriteString("\n")

	content.WriteString(tuiLabelStyle.Render("Branch: "))
	content.WriteString(tuiValueStyle.Render(wp.work.BranchName))
	content.WriteString("\n")

	if wp.work.RootIssueID != "" {
		content.WriteString(tuiLabelStyle.Render("Root Issue: "))
		content.WriteString(tuiValueStyle.Render(wp.work.RootIssueID))
		content.WriteString("\n")
	}

	content.WriteString(tuiLabelStyle.Render("Status: "))
	content.WriteString(statusStyled(wp.work.Status))
	content.WriteString("\n\n")

	// Show selected bead details
	if len(wp.workBeads) == 0 {
		content.WriteString(tuiDimStyle.Render("No issues assigned"))
	} else {
		// Ensure cursor is valid
		if m.overviewBeadCursor >= len(wp.workBeads) {
			m.overviewBeadCursor = len(wp.workBeads) - 1
		}
		if m.overviewBeadCursor < 0 {
			m.overviewBeadCursor = 0
		}

		bp := wp.workBeads[m.overviewBeadCursor]

		content.WriteString(tuiTitleStyle.Render("Selected Issue"))
		content.WriteString("\n\n")

		content.WriteString(tuiLabelStyle.Render("ID: "))
		content.WriteString(tuiValueStyle.Render(bp.id))
		content.WriteString("\n")

		if bp.title != "" {
			content.WriteString(tuiLabelStyle.Render("Title: "))
			content.WriteString(tuiValueStyle.Render(bp.title))
			content.WriteString("\n")
		}

		if bp.issueType != "" {
			content.WriteString(tuiLabelStyle.Render("Type: "))
			content.WriteString(tuiValueStyle.Render(bp.issueType))
			content.WriteString("\n")
		}

		content.WriteString(tuiLabelStyle.Render("Priority: "))
		content.WriteString(tuiValueStyle.Render(fmt.Sprintf("P%d", bp.priority)))
		content.WriteString("\n")

		content.WriteString(tuiLabelStyle.Render("Status: "))
		if bp.beadStatus == "closed" {
			content.WriteString(statusCompleted.Render("closed"))
		} else {
			content.WriteString(statusPending.Render("open"))
		}
		content.WriteString("\n")

		if bp.description != "" {
			content.WriteString("\n")
			content.WriteString(tuiLabelStyle.Render("Description:"))
			content.WriteString("\n")
			// Word-wrap description to fit panel
			desc := bp.description
			maxDescLen := width - 6
			if len(desc) > maxDescLen*3 {
				desc = desc[:maxDescLen*3] + "..."
			}
			content.WriteString(tuiDimStyle.Render(desc))
			content.WriteString("\n")
		}

		// Show navigation hint
		content.WriteString("\n")
		content.WriteString(tuiDimStyle.Render(fmt.Sprintf("Issue %d of %d", m.overviewBeadCursor+1, len(wp.workBeads))))
	}

	return tuiPanelStyle.Width(width - 2).Height(height - 2).Render(content.String())
}

// renderGridWorkerPanel renders a single worker panel in the grid
func (m *workModel) renderGridWorkerPanel(idx int, width, height int) string {
	wp := m.works[idx]
	isSelected := idx == m.worksCursor

	// Panel styling
	var panelStyle lipgloss.Style
	if isSelected {
		panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(0, 1).
			Width(width - 2).
			Height(height - 2)
	} else {
		panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1).
			Width(width - 2).
			Height(height - 2)
	}

	var content strings.Builder

	// Header: Worker name and status
	workerName := wp.work.Name
	if workerName == "" {
		workerName = wp.work.ID
	}

	// Check if any task is actively processing
	hasActiveTask := false
	var activeTaskID string
	for _, tp := range wp.tasks {
		if tp.task.Status == db.StatusProcessing {
			hasActiveTask = true
			activeTaskID = tp.task.ID
			break
		}
	}

	var icon string
	if wp.work.Status == db.StatusProcessing && hasActiveTask {
		icon = m.spinner.View()
	} else {
		icon = statusIcon(wp.work.Status)
	}

	// Add panel number (0-9) for first 10 panels
	var panelNumber string
	if idx < 10 {
		panelNumber = fmt.Sprintf("[%d] ", idx)
	}

	// Check orchestrator health
	orchestratorHealth := ""
	if wp.work.Status == db.StatusProcessing || hasActiveTask {
		if checkOrchestratorHealth(m.ctx, wp.work.ID) {
			orchestratorHealth = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("●") // Green dot for healthy
		} else {
			orchestratorHealth = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("●") // Red dot for dead
		}
	}

	header := fmt.Sprintf("%s%s %s%s", panelNumber, icon, workerName, orchestratorHealth)
	if isSelected {
		header = tuiActiveTabStyle.Render(header)
	} else {
		header = tuiTitleStyle.Render(header)
	}
	content.WriteString(header)
	content.WriteString("\n")

	// Work ID (if different from name)
	if wp.work.Name != "" {
		content.WriteString(tuiDimStyle.Render(wp.work.ID))
		content.WriteString("\n")
	}

	// Show orchestrator health status
	if wp.work.Status == db.StatusProcessing || hasActiveTask {
		healthStatus := ""
		if checkOrchestratorHealth(m.ctx, wp.work.ID) {
			healthStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓ Orchestrator running")
		} else {
			healthStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗ Orchestrator dead")
		}
		content.WriteString(healthStatus)
		content.WriteString("\n")
	}

	// Show current task if one is processing
	if activeTaskID != "" {
		taskLine := fmt.Sprintf("▶ Task: %s", activeTaskID)
		content.WriteString(statusProcessing.Render(taskLine))
		content.WriteString("\n")
	}

	// Branch name
	if wp.work.BranchName != "" {
		branch := wp.work.BranchName
		maxBranchLen := width - 6
		if len(branch) > maxBranchLen && maxBranchLen > 3 {
			branch = branch[:maxBranchLen-3] + "..."
		}
		content.WriteString(tuiDimStyle.Render("⎇ " + branch))
		content.WriteString("\n")
	}

	// Progress summary
	if len(wp.tasks) > 0 {
		pending := 0
		processing := 0
		completed := 0
		failed := 0
		for _, t := range wp.tasks {
			switch t.task.Status {
			case db.StatusPending:
				pending++
			case db.StatusProcessing:
				processing++
			case db.StatusCompleted:
				completed++
			case db.StatusFailed:
				failed++
			}
		}

		// Progress bar
		total := len(wp.tasks)
		donePercent := float64(completed) / float64(total) * 100
		progressWidth := width - 6
		if progressWidth < 10 {
			progressWidth = 10
		}
		filledWidth := int(float64(progressWidth) * float64(completed) / float64(total))
		emptyWidth := progressWidth - filledWidth

		progressBar := strings.Repeat("█", filledWidth) + strings.Repeat("░", emptyWidth)
		if failed > 0 {
			progressBar = tuiErrorStyle.Render(progressBar)
		} else if processing > 0 {
			progressBar = statusProcessing.Render(progressBar)
		} else if completed == total {
			progressBar = statusCompleted.Render(progressBar)
		}

		content.WriteString(fmt.Sprintf("%s %.0f%%\n", progressBar, donePercent))

		// Status counts
		counts := fmt.Sprintf("✓%d ●%d ○%d", completed, processing, pending)
		if failed > 0 {
			counts += fmt.Sprintf(" ✗%d", failed)
		}
		content.WriteString(tuiDimStyle.Render(counts))
		content.WriteString("\n")
	} else {
		// Show warning if there are unassigned beads
		if wp.unassignedBeadCount > 0 {
			content.WriteString(tuiErrorStyle.Render(fmt.Sprintf("⚠ %d pending", wp.unassignedBeadCount)))
		} else {
			content.WriteString(tuiDimStyle.Render("No tasks"))
		}
		content.WriteString("\n")
	}

	// Assigned beads/issues (show as many as fit)
	linesUsed := 5 // header + id + branch + progress + counts
	if activeTaskID != "" {
		linesUsed++ // account for task line
	}
	availableLines := height - linesUsed - 3 // account for border
	if availableLines < 0 {
		availableLines = 0
	}

	if len(wp.workBeads) > 0 && availableLines > 0 {
		content.WriteString("\n")
		for i, bp := range wp.workBeads {
			if i >= availableLines {
				remaining := len(wp.workBeads) - i
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  +%d more", remaining)))
				break
			}

			// Check if this bead is selected or hovered
			isBeadSelected := isSelected && i == m.overviewBeadCursor
			isBeadHovered := idx == m.hoveredWorkerIdx && i == m.hoveredBeadIdx

			// Show bead with status icon
			beadIcon := "○"
			if bp.beadStatus == "closed" {
				beadIcon = "✓"
			}

			// Show bead ID and title (truncated if needed)
			beadDisplay := bp.id
			if bp.title != "" {
				beadDisplay = fmt.Sprintf("%s %s", bp.id, bp.title)
			}
			maxLen := width - 8
			if len(beadDisplay) > maxLen && maxLen > 3 {
				beadDisplay = beadDisplay[:maxLen-3] + "..."
			}

			line := fmt.Sprintf("%s %s", beadIcon, beadDisplay)
			if isBeadSelected {
				line = tuiSelectedStyle.Render("> " + line)
			} else if isBeadHovered {
				// Hover style - cyan foreground
				hoverStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
				line = hoverStyle.Render("  " + line)
			} else {
				line = "  " + line
			}
			content.WriteString(line + "\n")
		}
	}

	return panelStyle.Render(content.String())
}

// renderEmptyGridPanel renders an empty grid cell
func (m *workModel) renderEmptyGridPanel(width, height int) string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("237")).
		Padding(0, 1).
		Width(width - 2).
		Height(height - 2)

	return panelStyle.Render("")
}

// renderOverviewStatusBar renders the status bar for overview mode
func (m *workModel) renderOverviewStatusBar() string {
	// Commands on the left - work-level actions for overview mode
	cButton := styleButtonWithHover("[c]reate", m.hoveredButton == "c")
	dButton := styleButtonWithHover("[d]estroy", m.hoveredButton == "d")
	helpButton := styleButtonWithHover("[?]help", m.hoveredButton == "?")

	keys := "[Tab]workers [←→]workers [↑↓]issues [Enter]zoom " + cButton + " " + dButton + " " + helpButton
	keysPlain := "[Tab]workers [←→]workers [↑↓]issues [Enter]zoom [c]reate [d]estroy [?]help"

	// Status on the right
	var statusParts []string
	if len(m.works) > 0 {
		pending := 0
		processing := 0
		completed := 0
		for _, wp := range m.works {
			switch wp.work.Status {
			case db.StatusPending:
				pending++
			case db.StatusProcessing:
				processing++
			case db.StatusCompleted:
				completed++
			}
		}
		statusParts = append(statusParts, fmt.Sprintf("Workers: %d (●%d ✓%d ○%d)", len(m.works), processing, completed, pending))
	}
	statusParts = append(statusParts, fmt.Sprintf("Updated: %s", m.lastUpdate.Format("15:04:05")))
	statusPlain := strings.Join(statusParts, " | ")
	status := tuiDimStyle.Render(statusPlain)

	// Build bar with commands left, status right
	padding := max(m.width-len(keysPlain)-len(statusPlain)-4, 2)
	return tuiStatusBarStyle.Width(m.width).Render(keys + strings.Repeat(" ", padding) + status)
}

// ==================== Zoomed Mode Rendering ====================

func (m *workModel) renderTasksPanel(width, height int) string {
	title := tuiTitleStyle.Render("Tasks")

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n")

	if len(m.works) == 0 || m.worksCursor >= len(m.works) {
		content.WriteString(tuiDimStyle.Render("No work selected"))
	} else {
		wp := m.works[m.worksCursor]

		// Calculate space: reserve lines for unassigned issues section if any
		unassignedLines := 0
		if len(wp.unassignedBeads) > 0 {
			// Reserve: 1 for divider, 1 for header, up to 5 for issues, 1 for "more" indicator
			unassignedLines = 3 + min(len(wp.unassignedBeads), 5)
		}

		tasksHeight := height - 3 - unassignedLines // -3 for panel border and title
		if tasksHeight < 3 {
			tasksHeight = 3
		}

		if len(wp.tasks) == 0 {
			content.WriteString(tuiDimStyle.Render("No tasks yet"))
			content.WriteString("\n")
			if len(wp.unassignedBeads) == 0 {
				content.WriteString(tuiDimStyle.Render("Press 'a' to assign issues"))
			}
		} else {
			visibleLines := tasksHeight
			if visibleLines < 1 {
				visibleLines = 1
			}

			startIdx := 0
			if m.tasksCursor >= visibleLines {
				startIdx = m.tasksCursor - visibleLines + 1
			}
			endIdx := startIdx + visibleLines
			if endIdx > len(wp.tasks) {
				endIdx = len(wp.tasks)
				startIdx = endIdx - visibleLines
				if startIdx < 0 {
					startIdx = 0
				}
			}

			if startIdx > 0 {
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↑ %d more", startIdx)))
				content.WriteString("\n")
			}

			for i := startIdx; i < endIdx; i++ {
				tp := wp.tasks[i]
				isSelected := i == m.tasksCursor && m.activePanel == PanelMiddle
				isHovered := i == m.hoveredTaskIdx

				var icon string
				if tp.task.Status == db.StatusProcessing {
					// Use spinner for processing tasks
					icon = m.spinner.View()
				} else if isSelected {
					icon = statusIconPlain(tp.task.Status)
				} else {
					icon = statusIcon(tp.task.Status)
				}

				line := fmt.Sprintf("%s %s", icon, tp.task.ID)

				if isSelected {
					fullLine := "> " + line
					visWidth := lipgloss.Width(fullLine)
					if visWidth < width-4 {
						fullLine += strings.Repeat(" ", width-4-visWidth)
					}
					line = tuiSelectedStyle.Render(fullLine)
				} else if isHovered {
					// Hover style - bold bright cyan with arrow indicator
					line = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51")).Render("→ " + line)
				} else {
					line = "  " + line
				}
				content.WriteString(line)
				content.WriteString("\n")
			}

			if endIdx < len(wp.tasks) {
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↓ %d more", len(wp.tasks)-endIdx)))
				content.WriteString("\n")
			}
		}

		// Show unassigned issues section if any
		if len(wp.unassignedBeads) > 0 {
			content.WriteString("\n")
			content.WriteString(tuiDimStyle.Render("─────────────────────"))
			content.WriteString("\n")
			content.WriteString(tuiLabelStyle.Render(fmt.Sprintf("Unassigned (%d)", len(wp.unassignedBeads))))
			content.WriteString("\n")

			// Calculate which unassigned issues to show based on cursor position
			unassignedCursor := m.tasksCursor - len(wp.tasks) // -1 if in tasks, 0+ if in unassigned
			maxShow := 5
			startIdx := 0
			if unassignedCursor >= maxShow {
				startIdx = unassignedCursor - maxShow + 1
			}
			endIdx := min(startIdx+maxShow, len(wp.unassignedBeads))

			if startIdx > 0 {
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↑ %d more", startIdx)))
				content.WriteString("\n")
			}

			for i := startIdx; i < endIdx; i++ {
				bp := wp.unassignedBeads[i]
				itemIdx := len(wp.tasks) + i
				isSelected := m.tasksCursor == itemIdx && m.activePanel == PanelMiddle
				isHovered := m.hoveredTaskIdx == itemIdx

				// Type indicator
				var typeIndicator string
				switch bp.issueType {
				case "task":
					typeIndicator = typeTaskStyle.Render("T")
				case "bug":
					typeIndicator = typeBugStyle.Render("B")
				case "feature":
					typeIndicator = typeFeatureStyle.Render("F")
				case "epic":
					typeIndicator = typeEpicStyle.Render("E")
				default:
					typeIndicator = tuiDimStyle.Render("?")
				}

				// Truncate title to fit
				title := bp.title
				maxTitleLen := width - 18 // account for prefix and padding
				if maxTitleLen < 10 {
					maxTitleLen = 10
				}
				if len(title) > maxTitleLen {
					title = title[:maxTitleLen-3] + "..."
				}

				if isSelected {
					line := fmt.Sprintf("> %s %s %s", typeIndicator, bp.id, title)
					visWidth := lipgloss.Width(line)
					if visWidth < width-4 {
						line += strings.Repeat(" ", width-4-visWidth)
					}
					content.WriteString(tuiSelectedStyle.Render(line))
				} else if isHovered {
					line := fmt.Sprintf("→ %s %s %s", typeIndicator, bp.id, title)
					content.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51")).Render(line))
				} else {
					line := fmt.Sprintf("  %s %s %s", typeIndicator, issueIDStyle.Render(bp.id), title)
					content.WriteString(line)
				}
				content.WriteString("\n")
			}

			if endIdx < len(wp.unassignedBeads) {
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↓ %d more", len(wp.unassignedBeads)-endIdx)))
				content.WriteString("\n")
			}

			content.WriteString(tuiDimStyle.Render("[r]un [s]imple [x]remove"))
		}
	}

	style := tuiPanelStyle
	if m.activePanel == PanelMiddle {
		style = tuiActivePanelStyle
	}
	return style.Width(width - 2).Height(height).Render(content.String())
}

func (m *workModel) renderDetailsPanel(width, height int) string {
	// If in create bead mode, render the bead form inline
	if m.viewMode == ViewCreateBead {
		return m.renderBeadFormInline(width, height)
	}

	// If in assign beads mode, render the assign beads form inline
	if m.viewMode == ViewAssignBeads {
		return m.renderAssignBeadsInline(width, height)
	}

	title := tuiTitleStyle.Render("Details")

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n\n")

	if len(m.works) == 0 || m.worksCursor >= len(m.works) {
		content.WriteString(tuiDimStyle.Render("No work selected"))
	} else {
		wp := m.works[m.worksCursor]

		// Show work details
		content.WriteString(tuiLabelStyle.Render("Worker: "))
		if wp.work.Name != "" {
			content.WriteString(tuiValueStyle.Render(wp.work.Name))
			content.WriteString(tuiDimStyle.Render(fmt.Sprintf(" (%s)", wp.work.ID)))
		} else {
			content.WriteString(tuiValueStyle.Render(wp.work.ID))
		}
		content.WriteString("\n")

		content.WriteString(tuiLabelStyle.Render("Status: "))
		content.WriteString(statusStyled(wp.work.Status))
		content.WriteString("\n")

		content.WriteString(tuiLabelStyle.Render("Branch: "))
		content.WriteString(tuiValueStyle.Render(wp.work.BranchName))
		content.WriteString("\n")

		if wp.work.RootIssueID != "" {
			content.WriteString(tuiLabelStyle.Render("Root Issue: "))
			content.WriteString(tuiValueStyle.Render(wp.work.RootIssueID))
			content.WriteString("\n")
		}

		if wp.work.PRURL != "" {
			content.WriteString(tuiLabelStyle.Render("PR: "))
			content.WriteString(tuiValueStyle.Render(wp.work.PRURL))
			content.WriteString("\n")
		}

		// Task summary
		content.WriteString("\n")
		content.WriteString(tuiLabelStyle.Render("Tasks: "))
		if len(wp.tasks) == 0 {
			content.WriteString(tuiDimStyle.Render("none"))
		} else {
			pending := 0
			processing := 0
			completed := 0
			failed := 0
			for _, t := range wp.tasks {
				switch t.task.Status {
				case db.StatusPending:
					pending++
				case db.StatusProcessing:
					processing++
				case db.StatusCompleted:
					completed++
				case db.StatusFailed:
					failed++
				}
			}
			summary := fmt.Sprintf("%d total", len(wp.tasks))
			if completed > 0 {
				summary += fmt.Sprintf(", %d done", completed)
			}
			if processing > 0 {
				summary += fmt.Sprintf(", %d running", processing)
			}
			if pending > 0 {
				summary += fmt.Sprintf(", %d pending", pending)
			}
			if failed > 0 {
				summary += fmt.Sprintf(", %d failed", failed)
			}
			content.WriteString(tuiValueStyle.Render(summary))
		}
		content.WriteString("\n")

		// Review iterations count
		reviewCount := 0
		for _, t := range wp.tasks {
			if strings.HasPrefix(t.task.ID, wp.work.ID+".review") {
				reviewCount++
			}
		}
		if reviewCount > 0 {
			maxIterations := m.proj.Config.Workflow.GetMaxReviewIterations()
			content.WriteString(tuiLabelStyle.Render("Review Iterations: "))
			iterStatus := fmt.Sprintf("%d/%d", reviewCount, maxIterations)
			if reviewCount >= maxIterations {
				// Show in warning color if at or over limit
				content.WriteString(tuiErrorStyle.Render(iterStatus))
				content.WriteString(tuiDimStyle.Render(" (limit reached)"))
			} else {
				content.WriteString(tuiValueStyle.Render(iterStatus))
			}
			content.WriteString("\n")
		}

		// Pending issues count
		if wp.unassignedBeadCount > 0 {
			content.WriteString(tuiLabelStyle.Render("Pending Issues: "))
			content.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d", wp.unassignedBeadCount)))
			content.WriteString("\n")
		}

		// If task is selected, show task details
		if m.activePanel == PanelMiddle && len(wp.tasks) > 0 && m.tasksCursor < len(wp.tasks) {
			tp := wp.tasks[m.tasksCursor]
			content.WriteString("\n")
			content.WriteString(tuiTitleStyle.Render("Selected Task"))
			content.WriteString("\n")

			content.WriteString(tuiLabelStyle.Render("ID: "))
			content.WriteString(tuiValueStyle.Render(tp.task.ID))
			content.WriteString("\n")

			content.WriteString(tuiLabelStyle.Render("Type: "))
			content.WriteString(tuiValueStyle.Render(tp.task.TaskType))
			content.WriteString("\n")

			content.WriteString(tuiLabelStyle.Render("Status: "))
			content.WriteString(statusStyled(tp.task.Status))
			content.WriteString("\n")

			if len(tp.beads) > 0 {
				content.WriteString(tuiLabelStyle.Render("Issues: "))
				content.WriteString("\n")
				for _, bp := range tp.beads {
					content.WriteString(fmt.Sprintf("  %s %s\n", statusIcon(bp.status), bp.id))
				}
			}
		}

		// If unassigned issue is selected, show issue details
		unassignedIdx := m.tasksCursor - len(wp.tasks)
		if m.activePanel == PanelMiddle && unassignedIdx >= 0 && unassignedIdx < len(wp.unassignedBeads) {
			bp := wp.unassignedBeads[unassignedIdx]
			content.WriteString("\n")
			content.WriteString(tuiTitleStyle.Render("Selected Issue"))
			content.WriteString("\n")

			content.WriteString(tuiLabelStyle.Render("ID: "))
			content.WriteString(issueIDStyle.Render(bp.id))
			content.WriteString("\n")

			content.WriteString(tuiLabelStyle.Render("Title: "))
			content.WriteString(tuiValueStyle.Render(bp.title))
			content.WriteString("\n")

			content.WriteString(tuiLabelStyle.Render("Type: "))
			content.WriteString(tuiValueStyle.Render(bp.issueType))
			content.WriteString("\n")

			content.WriteString(tuiLabelStyle.Render("Priority: "))
			content.WriteString(tuiValueStyle.Render(fmt.Sprintf("P%d", bp.priority)))
			content.WriteString("\n")

			content.WriteString(tuiLabelStyle.Render("Status: "))
			content.WriteString(tuiValueStyle.Render(bp.beadStatus))
			content.WriteString("\n")

			if bp.description != "" {
				content.WriteString("\n")
				content.WriteString(tuiLabelStyle.Render("Description:"))
				content.WriteString("\n")
				desc := bp.description
				// Truncate if too long
				maxDescLen := (width - 6) * 3 // ~3 lines worth
				if len(desc) > maxDescLen {
					desc = desc[:maxDescLen-3] + "..."
				}
				content.WriteString("  " + tuiDimStyle.Render(desc) + "\n")
			}

			content.WriteString("\n")
			content.WriteString(tuiDimStyle.Render("Press [x] to remove from work"))
		}
	}

	style := tuiPanelStyle
	if m.activePanel == PanelRight {
		style = tuiActivePanelStyle
	}
	return style.Width(width - 2).Height(height).Render(content.String())
}

// renderBeadFormInline renders the create bead form inline in the details panel
func (m *workModel) renderBeadFormInline(width, height int) string {
	var content strings.Builder

	// Adapt input widths to available space (account for panel padding)
	inputWidth := width - 6
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
		typeLabel = tuiValueStyle.Render("Type:") + " (←/→)"
	}
	if priorityFocused {
		priorityLabel = tuiValueStyle.Render("Priority:") + " (←/→)"
	}
	if descFocused {
		descLabel = tuiValueStyle.Render("Description:") + " (optional)"
	}

	// Render header
	content.WriteString(tuiTitleStyle.Render("Create New Issue"))
	content.WriteString("\n\n")

	// Render form fields
	content.WriteString(titleLabel)
	content.WriteString("\n")
	content.WriteString(m.textInput.View())
	content.WriteString("\n\n")
	content.WriteString(typeLabel + " " + typeDisplay)
	content.WriteString("\n\n")
	content.WriteString(priorityLabel + " " + priorityDisplay)
	content.WriteString("\n\n")
	content.WriteString(descLabel)
	content.WriteString("\n")
	content.WriteString(m.createDescTextarea.View())
	content.WriteString("\n\n")

	// Render buttons
	content.WriteString("[Tab] next  [Enter] create  [Esc] cancel")

	style := tuiPanelStyle
	if m.activePanel == PanelRight {
		style = tuiActivePanelStyle
	}
	return style.Width(width - 2).Height(height).Render(content.String())
}

// renderAssignBeadsInline renders the assign beads form inline in the details panel
func (m *workModel) renderAssignBeadsInline(width, height int) string {
	var content strings.Builder

	content.WriteString(tuiTitleStyle.Render("Assign Issues"))
	content.WriteString("\n")

	// Show target work
	if len(m.works) > 0 && m.worksCursor < len(m.works) {
		wp := m.works[m.worksCursor]
		content.WriteString(tuiLabelStyle.Render("To: "))
		if wp.work.Name != "" {
			content.WriteString(tuiValueStyle.Render(wp.work.Name))
		} else {
			content.WriteString(tuiValueStyle.Render(wp.work.ID))
		}
		content.WriteString("\n\n")
	}

	// Reserve space for details section (about 40% of height) and controls (2 lines)
	detailsLines := height * 40 / 100
	if detailsLines < 5 {
		detailsLines = 5
	}
	controlLines := 2
	headerLines := 3 // title + target + blank
	listLines := height - headerLines - detailsLines - controlLines
	if listLines < 3 {
		listLines = 3
	}

	// Show the beads list
	if len(m.beadItems) == 0 {
		content.WriteString(tuiDimStyle.Render("No ready issues found"))
	} else {
		// Calculate scroll window
		start := 0
		if m.beadsCursor >= listLines {
			start = m.beadsCursor - listLines + 1
		}
		end := start + listLines
		if end > len(m.beadItems) {
			end = len(m.beadItems)
		}

		for i := start; i < end; i++ {
			bead := m.beadItems[i]

			// Checkbox
			var checkbox string
			if m.selectedBeads[bead.id] {
				checkbox = tuiSelectedCheckStyle.Render("[●]")
			} else {
				checkbox = tuiDimStyle.Render("[ ]")
			}

			// Status and type icons
			statusStr := statusIcon(bead.status)
			var typeStr string
			switch bead.beadType {
			case "task":
				typeStr = typeTaskStyle.Render("T")
			case "bug":
				typeStr = typeBugStyle.Render("B")
			case "feature":
				typeStr = typeFeatureStyle.Render("F")
			case "epic":
				typeStr = typeEpicStyle.Render("E")
			case "chore":
				typeStr = typeChoreStyle.Render("C")
			default:
				typeStr = typeDefaultStyle.Render("?")
			}

			// Truncate title to fit width
			maxTitleLen := width - 20 // Account for checkbox, status, type, ID, spacing
			if maxTitleLen < 10 {
				maxTitleLen = 10
			}
			title := bead.title
			if len(title) > maxTitleLen {
				title = title[:maxTitleLen-3] + "..."
			}

			line := fmt.Sprintf("%s %s %s %s %s",
				checkbox,
				statusStr,
				typeStr,
				issueIDStyle.Render(bead.id),
				title)

			// Highlight selected row
			if i == m.beadsCursor {
				line = tuiSelectedStyle.Render("> " + line)
			} else {
				line = "  " + line
			}

			content.WriteString(line)
			if i < end-1 {
				content.WriteString("\n")
			}
		}
	}

	// Show details of currently selected issue
	content.WriteString("\n\n")
	content.WriteString(tuiDimStyle.Render(strings.Repeat("─", width-6)))
	content.WriteString("\n")

	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		bead := m.beadItems[m.beadsCursor]

		// ID, Type, Priority, Status line
		content.WriteString(tuiLabelStyle.Render("ID: "))
		content.WriteString(issueIDStyle.Render(bead.id))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("Type: "))
		content.WriteString(tuiValueStyle.Render(bead.beadType))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("P"))
		content.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d", bead.priority)))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("Status: "))
		content.WriteString(tuiValueStyle.Render(bead.status))
		content.WriteString("\n")

		// Title (full, with wrapping)
		titleStyle := tuiValueStyle.Width(width - 6)
		content.WriteString(titleStyle.Render(bead.title))
		content.WriteString("\n")

		// Description (if available)
		if bead.description != "" {
			descStyle := tuiDimStyle.Width(width - 6)
			// Limit description length to fit in remaining space
			desc := bead.description
			maxDescLen := (detailsLines - 3) * (width - 6)
			if maxDescLen > 0 && len(desc) > maxDescLen {
				desc = desc[:maxDescLen-3] + "..."
			}
			content.WriteString(descStyle.Render(desc))
		}
	}

	// Show selection count and controls
	selected := 0
	for _, s := range m.selectedBeads {
		if s {
			selected++
		}
	}
	content.WriteString(fmt.Sprintf("\n\n%d selected  [Space] toggle  [Enter] assign  [Esc] cancel", selected))

	style := tuiPanelStyle
	if m.activePanel == PanelRight {
		style = tuiActivePanelStyle
	}
	return style.Width(width - 2).Height(height).Render(content.String())
}

func (m *workModel) renderStatusBar() string {
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
	} else {
		statusPlain = fmt.Sprintf("Updated: %s", m.lastUpdate.Format("15:04:05"))
		status = tuiDimStyle.Render(statusPlain)
	}

	var keys, keysPlain string

	// Show different status bar based on view mode
	if m.viewMode == ViewAssignBeads {
		// Assign beads mode - show selection controls
		selected := 0
		for _, s := range m.selectedBeads {
			if s {
				selected++
			}
		}
		selectionInfo := fmt.Sprintf("%d selected", selected)
		if selected > 0 {
			selectionInfo = tuiSuccessStyle.Render(selectionInfo)
		} else {
			selectionInfo = tuiDimStyle.Render(selectionInfo)
		}

		escButton := styleHotkeys("[Esc]cancel")
		spaceButton := styleHotkeys("[Space]toggle")
		enterButton := styleHotkeys("[Enter]assign")
		aButton := styleHotkeys("[a]select all")

		keys = escButton + "  " + spaceButton + "  " + aButton + "  " + enterButton + "  " + selectionInfo
		keysPlain = fmt.Sprintf("[Esc]cancel  [Space]toggle  [a]select all  [Enter]assign  %d selected", selected)
	} else {
		// Normal zoomed view - task-specific actions
		// Check what's available based on current state
		hasUnassigned := false
		isUnassignedSelected := false
		if len(m.works) > 0 && m.worksCursor < len(m.works) {
			wp := m.works[m.worksCursor]
			hasUnassigned = len(wp.unassignedBeads) > 0
			isUnassignedSelected = m.tasksCursor >= len(wp.tasks) && m.tasksCursor < len(wp.tasks)+len(wp.unassignedBeads)
		}

		escButton := "[Esc]overview"

		// Run buttons - only enabled if there are unassigned issues
		var rButton, sButton string
		if hasUnassigned {
			rButton = styleButtonWithHover("[r]un", m.hoveredButton == "r")
			sButton = styleButtonWithHover("[s]imple", m.hoveredButton == "s")
		} else {
			rButton = tuiDimStyle.Render("[r]un")
			sButton = tuiDimStyle.Render("[s]imple")
		}

		aButton := styleButtonWithHover("[a]ssign", m.hoveredButton == "a")
		nButton := styleButtonWithHover("[n]ew", m.hoveredButton == "n")

		// Remove button - only enabled if an unassigned issue is selected
		var xButton string
		if isUnassignedSelected {
			xButton = styleButtonWithHover("[x]remove", m.hoveredButton == "x")
		} else {
			xButton = tuiDimStyle.Render("[x]remove")
		}

		tButton := styleButtonWithHover("[t]erminal", m.hoveredButton == "t")
		cButton := styleButtonWithHover("[c]laude", m.hoveredButton == "c")
		oButton := styleButtonWithHover("[o]rchestrator", m.hoveredButton == "o")
		vButton := styleButtonWithHover("[v]review", m.hoveredButton == "v")
		pButton := styleButtonWithHover("[p]r", m.hoveredButton == "p")
		uButton := styleButtonWithHover("[u]pdate", m.hoveredButton == "u")
		helpButton := styleButtonWithHover("[?]help", m.hoveredButton == "?")

		keys = escButton + " " + rButton + " " + sButton + " " + aButton + " " + nButton + " " + xButton + " " + tButton + " " + cButton + " " + oButton + " " + vButton + " " + pButton + " " + uButton + " " + helpButton
		keysPlain = "[Esc]overview [r]un [s]imple [a]ssign [n]ew [x]remove [t]erminal [c]laude [o]rchestrator [v]review [p]r [u]pdate [?]help"
	}

	// Build bar with commands left, status right
	padding := max(m.width-len(keysPlain)-len(statusPlain)-4, 2)
	return tuiStatusBarStyle.Width(m.width).Render(keys + strings.Repeat(" ", padding) + status)
}