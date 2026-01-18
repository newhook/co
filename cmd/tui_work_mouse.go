package cmd

import (
	"context"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/process"
)

// Mouse handling functions

// handleDetailsPanelClick handles clicks in the details panel
func (m *workModel) handleDetailsPanelClick(x, y int) {
	if len(m.works) == 0 || m.worksCursor >= len(m.works) {
		return
	}

	wp := m.works[m.worksCursor]

	// Check if work has a PR URL to show the Poll Feedback button
	if wp.work.PRURL == "" {
		return
	}

	// The PR line is typically around line 10-15 in the details panel
	// Check if click is on the [Poll Feedback] button
	// The button appears after "PR: <url>  " so we check if x position is reasonable
	if y >= 10 && y <= 20 && x >= 10 && x <= 30 {
		// Trigger PR feedback polling
		_ = m.pollPRFeedback()
	}
}

// detectDetailsPanelHover detects hover on buttons in the details panel
func (m *workModel) detectDetailsPanelHover(x, y int) {
	m.hoveredButton = "" // Reset hover state

	if len(m.works) == 0 || m.worksCursor >= len(m.works) {
		return
	}

	wp := m.works[m.worksCursor]

	// Check if work has a PR URL to show the Poll Feedback button
	if wp.work.PRURL == "" {
		return
	}

	// The PR line is typically around line 10-15 in the details panel
	// Check if hovering over the [Poll Feedback] button area
	if y >= 10 && y <= 20 && x >= 10 && x <= 30 {
		m.hoveredButton = "poll-feedback-detail"
	}
}

// detectZoomedHover detects which task or unassigned issue is being hovered in zoomed mode
func (m *workModel) detectZoomedHover(x, y int) {
	m.hoveredTaskIdx = -1

	if len(m.works) == 0 || m.worksCursor >= len(m.works) {
		return
	}

	wp := m.works[m.worksCursor]
	totalItems := len(wp.tasks) + len(wp.unassignedBeads)
	if totalItems == 0 {
		return
	}

	// Calculate panel dimensions to get scroll offset
	panelWidth := m.width / 2
	panelHeight := m.height - 1 - 2

	// Check if hover is in the tasks panel (left side)
	if x >= panelWidth {
		return // Hover is in the details panel
	}

	// Title is at y=1 (after border at y=0), content starts at y=2
	contentLine := y - 2
	if contentLine < 0 {
		return
	}

	// Calculate lines used by tasks section
	tasksVisibleLines := panelHeight - 3
	if len(wp.unassignedBeads) > 0 {
		// Reserve space for unassigned section
		unassignedLines := 3 + min(len(wp.unassignedBeads), 5)
		tasksVisibleLines = panelHeight - 3 - unassignedLines
	}
	if tasksVisibleLines < 3 {
		tasksVisibleLines = 3
	}

	// Check if in tasks section
	if len(wp.tasks) > 0 && contentLine < len(wp.tasks) && contentLine < tasksVisibleLines {
		m.hoveredTaskIdx = contentLine
		return
	}

	// Check if in unassigned section
	if len(wp.unassignedBeads) > 0 {
		// Calculate where unassigned section starts
		// After tasks + blank + divider + header = tasks shown + 3
		tasksShown := min(len(wp.tasks), tasksVisibleLines)
		unassignedStart := tasksShown + 3 // blank line + divider + header

		unassignedLine := contentLine - unassignedStart
		if unassignedLine >= 0 && unassignedLine < len(wp.unassignedBeads) && unassignedLine < 5 {
			m.hoveredTaskIdx = len(wp.tasks) + unassignedLine
		}
	}
}

// handleZoomedClick handles mouse clicks in the zoomed view
func (m *workModel) handleZoomedClick(x, y int) {
	if len(m.works) == 0 || m.worksCursor >= len(m.works) {
		return
	}

	wp := m.works[m.worksCursor]
	totalItems := len(wp.tasks) + len(wp.unassignedBeads)
	if totalItems == 0 {
		return
	}

	// Calculate panel dimensions (matching View layout)
	panelWidth := m.width / 2
	panelHeight := m.height - 1 - 2

	// Check if click is in the tasks panel (left side) or details panel
	if x >= panelWidth {
		// Click was in the details panel - check for Poll Feedback button
		m.handleDetailsPanelClick(x - panelWidth, y)
		return
	}

	// Title is at y=1 (after border at y=0), content starts at y=2
	contentLine := y - 2
	if contentLine < 0 {
		return
	}

	// Calculate lines used by tasks section
	tasksVisibleLines := panelHeight - 3
	if len(wp.unassignedBeads) > 0 {
		unassignedLines := 3 + min(len(wp.unassignedBeads), 5)
		tasksVisibleLines = panelHeight - 3 - unassignedLines
	}
	if tasksVisibleLines < 3 {
		tasksVisibleLines = 3
	}

	// Check if clicking in tasks section
	if len(wp.tasks) > 0 && contentLine < len(wp.tasks) && contentLine < tasksVisibleLines {
		m.tasksCursor = contentLine
		m.activePanel = PanelMiddle
		return
	}

	// Check if clicking in unassigned section
	if len(wp.unassignedBeads) > 0 {
		tasksShown := min(len(wp.tasks), tasksVisibleLines)
		unassignedStart := tasksShown + 3

		unassignedLine := contentLine - unassignedStart
		if unassignedLine >= 0 && unassignedLine < len(wp.unassignedBeads) && unassignedLine < 5 {
			m.tasksCursor = len(wp.tasks) + unassignedLine
			m.activePanel = PanelMiddle
		}
	}
}

// detectOverviewHover detects which worker/bead is being hovered in overview mode
func (m *workModel) detectOverviewHover(x, y int) {
	m.hoveredWorkerIdx = -1
	m.hoveredBeadIdx = -1

	if len(m.works) == 0 {
		return
	}

	// Calculate grid dimensions (matching renderOverviewGrid layout)
	detailsWidth := m.width / 3
	if detailsWidth < 30 {
		detailsWidth = 30
	}
	if detailsWidth > 50 {
		detailsWidth = 50
	}
	gridWidth := m.width - detailsWidth
	contentHeight := m.height - 1

	// Check if hover is in the grid area (left side)
	if x >= gridWidth {
		return // Hover is in the details panel
	}

	// Recalculate grid config for the current grid width
	gridConfig := CalculateGridDimensions(len(m.works), gridWidth, contentHeight)

	if gridConfig.CellWidth <= 0 || gridConfig.CellHeight <= 0 {
		return
	}

	cellCol := x / gridConfig.CellWidth
	cellRow := y / gridConfig.CellHeight

	// Clamp to valid range
	if cellCol < 0 || cellCol >= gridConfig.Cols {
		return
	}
	if cellRow < 0 || cellRow >= gridConfig.Rows {
		return
	}

	workerIdx := cellRow*gridConfig.Cols + cellCol
	if workerIdx >= len(m.works) || workerIdx < 0 {
		return
	}

	m.hoveredWorkerIdx = workerIdx

	// Calculate local position within the cell
	cellStartY := cellRow * gridConfig.CellHeight
	localY := y - cellStartY

	// Detect which bead is being hovered
	wp := m.works[workerIdx]
	if len(wp.workBeads) == 0 {
		return
	}

	// Account for panel structure (same as handleOverviewClick)
	beadAreaStart := 8
	beadLine := localY - beadAreaStart

	if beadLine >= 0 && beadLine < len(wp.workBeads) {
		m.hoveredBeadIdx = beadLine
	}
}

// handleOverviewClick handles mouse clicks in the overview grid area
func (m *workModel) handleOverviewClick(x, y int) {
	if len(m.works) == 0 {
		return
	}

	// Calculate grid dimensions (matching renderOverviewGrid layout)
	detailsWidth := m.width / 3
	if detailsWidth < 30 {
		detailsWidth = 30
	}
	if detailsWidth > 50 {
		detailsWidth = 50
	}
	gridWidth := m.width - detailsWidth
	contentHeight := m.height - 1

	// Check if click is in the grid area (left side)
	if x >= gridWidth {
		return // Click was in the details panel, ignore
	}

	// Recalculate grid config for the current grid width
	gridConfig := CalculateGridDimensions(len(m.works), gridWidth, contentHeight)

	// Determine which cell was clicked
	if gridConfig.CellWidth <= 0 || gridConfig.CellHeight <= 0 {
		return
	}

	cellCol := x / gridConfig.CellWidth
	cellRow := y / gridConfig.CellHeight

	// Clamp to valid range
	if cellCol < 0 {
		cellCol = 0
	}
	if cellCol >= gridConfig.Cols {
		cellCol = gridConfig.Cols - 1
	}
	if cellRow < 0 {
		cellRow = 0
	}
	if cellRow >= gridConfig.Rows {
		cellRow = gridConfig.Rows - 1
	}

	workerIdx := cellRow*gridConfig.Cols + cellCol
	if workerIdx >= len(m.works) || workerIdx < 0 {
		return
	}

	// Calculate local position within the cell
	cellStartX := cellCol * gridConfig.CellWidth
	cellStartY := cellRow * gridConfig.CellHeight
	localY := y - cellStartY
	_ = x - cellStartX // localX not needed for now

	// Update worker selection
	previousWorker := m.worksCursor
	m.worksCursor = workerIdx

	// If clicking on a different worker, reset bead cursor
	if workerIdx != previousWorker {
		m.overviewBeadCursor = 0
		return
	}

	// If clicking on the same worker, try to detect which bead was clicked
	wp := m.works[workerIdx]
	if len(wp.workBeads) == 0 {
		return
	}

	// Account for panel structure:
	// - Border top: 1 line
	// - Header content: ~7 lines (name, id, branch, progress, counts, blank line before beads)
	// - Beads start around line 8 from top of cell
	// Each bead takes 1 line

	// Subtract border (1 line) and header lines (~7 lines)
	beadAreaStart := 8 // approximate line where beads start (1 border + 7 header)
	beadLine := localY - beadAreaStart

	if beadLine >= 0 && beadLine < len(wp.workBeads) {
		m.overviewBeadCursor = beadLine
	}
}

// detectStatusBarButton determines which button is at the given X position in the status bar
func (m *workModel) detectStatusBarButton(x int) string {
	// Account for the status bar's left padding (tuiStatusBarStyle has Padding(0, 1))
	if x < 1 {
		return ""
	}
	x = x - 1

	// Use different button layouts based on zoom level
	if m.zoomLevel == ZoomOverview {
		// Overview mode - calculate visual positions (not byte positions)
		// because arrows like ←→↑↓ are multi-byte but single-width
		prefix := "[Tab]workers [←→]workers [↑↓]issues [Enter]zoom "
		prefixWidth := lipgloss.Width(prefix)

		cStart := prefixWidth
		cEnd := cStart + len("[c]reate")
		dStart := cEnd + 1 // +1 for space
		dEnd := dStart + len("[d]estroy")
		helpStart := dEnd + 1
		helpEnd := helpStart + len("[?]help")

		if x >= cStart && x < cEnd {
			return "c"
		}
		if x >= dStart && x < dEnd {
			return "d"
		}
		if x >= helpStart && x < helpEnd {
			return "?"
		}
	} else {
		// Zoomed mode: "[Esc]overview [r]un [s]imple [a]ssign [n]ew [x]remove [t]erminal [c]laude [o]rchestrator [v]review [p]r [u]pdate [?]help"

		keysPlain := "[Esc]overview [r]un [s]imple [a]ssign [n]ew [x]remove [t]erminal [c]laude [o]rchestrator [v]review [p]r [u]pdate [?]help"

		rIdx := strings.Index(keysPlain, "[r]un")
		sIdx := strings.Index(keysPlain, "[s]imple")
		aIdx := strings.Index(keysPlain, "[a]ssign")
		nIdx := strings.Index(keysPlain, "[n]ew")
		xIdx := strings.Index(keysPlain, "[x]remove")
		tIdx := strings.Index(keysPlain, "[t]erminal")
		cIdx := strings.Index(keysPlain, "[c]laude")
		oIdx := strings.Index(keysPlain, "[o]rchestrator")
		vIdx := strings.Index(keysPlain, "[v]review")
		pIdx := strings.Index(keysPlain, "[p]r")
		uIdx := strings.Index(keysPlain, "[u]pdate")
		helpIdx := strings.Index(keysPlain, "[?]help")

		if rIdx >= 0 && x >= rIdx && x < rIdx+len("[r]un") {
			return "r"
		}
		if sIdx >= 0 && x >= sIdx && x < sIdx+len("[s]imple") {
			return "s"
		}
		if aIdx >= 0 && x >= aIdx && x < aIdx+len("[a]ssign") {
			return "a"
		}
		if nIdx >= 0 && x >= nIdx && x < nIdx+len("[n]ew") {
			return "n"
		}
		if xIdx >= 0 && x >= xIdx && x < xIdx+len("[x]remove") {
			return "x"
		}
		if tIdx >= 0 && x >= tIdx && x < tIdx+len("[t]erminal") {
			return "t"
		}
		if cIdx >= 0 && x >= cIdx && x < cIdx+len("[c]laude") {
			return "c"
		}
		if oIdx >= 0 && x >= oIdx && x < oIdx+len("[o]rchestrator") {
			return "o"
		}
		if vIdx >= 0 && x >= vIdx && x < vIdx+len("[v]review") {
			return "v"
		}
		if pIdx >= 0 && x >= pIdx && x < pIdx+len("[p]r") {
			return "p"
		}
		if uIdx >= 0 && x >= uIdx && x < uIdx+len("[u]pdate") {
			return "u"
		}
		if helpIdx >= 0 && x >= helpIdx && x < helpIdx+len("[?]help") {
			return "?"
		}
	}

	return ""
}

// checkOrchestratorHealth checks if the orchestrator process is running for a work
func checkOrchestratorHealth(ctx context.Context, workID string) bool {
	// Check if an orchestrator process is running for this specific work
	pattern := "co orchestrate --work " + workID
	running, _ := process.IsProcessRunning(ctx, pattern)
	return running
}