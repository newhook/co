package cmd

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
)

// monitorModel is the Monitor Mode model with grid layout for active workers
type monitorModel struct {
	ctx    context.Context
	proj   *project.Project
	width  int
	height int

	// Grid state
	selectedIdx int // which worker is selected in the grid
	gridCols    int // number of columns in grid
	gridRows    int // number of rows in grid

	// Data
	works   []*workProgress
	loading bool
	focused bool

	// UI state
	spinner    spinner.Model
	lastUpdate time.Time

	// Mouse state
	mouseX        int
	mouseY        int
	hoveredButton string // which button is hovered ("r" for refresh, etc.)
}

// monitorDataMsg is sent when data is refreshed
type monitorDataMsg struct {
	works []*workProgress
	err   error
}

// monitorTickMsg triggers periodic refresh
type monitorTickMsg time.Time

// newMonitorModel creates a new Monitor Mode model
func newMonitorModel(ctx context.Context, proj *project.Project) *monitorModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	return &monitorModel{
		ctx:     ctx,
		proj:    proj,
		width:   80,
		height:  24,
		spinner: s,
		loading: true,
	}
}

// Init implements tea.Model
func (m *monitorModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.refreshData(),
		m.tick(),
	)
}

// SetSize implements SubModel
func (m *monitorModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.recalculateGrid()
}

// FocusChanged implements SubModel
func (m *monitorModel) FocusChanged(focused bool) tea.Cmd {
	m.focused = focused
	if focused {
		// Refresh data when gaining focus and restart periodic tick
		m.loading = true
		return tea.Batch(m.refreshData(), m.tick())
	}
	return nil
}

// InModal returns true if in a modal/dialog state (monitor mode has no dialogs)
func (m *monitorModel) InModal() bool {
	return false
}

// recalculateGrid calculates optimal grid dimensions based on workers and screen size
func (m *monitorModel) recalculateGrid() {
	numWorkers := len(m.works)
	if numWorkers == 0 {
		m.gridCols = 1
		m.gridRows = 1
		return
	}

	// Calculate optimal grid dimensions
	// Aim for roughly square cells, considering terminal is usually wider than tall
	// Each cell needs minimum 30 chars wide and 10 lines tall
	minCellWidth := 30
	minCellHeight := 10

	maxCols := m.width / minCellWidth
	if maxCols < 1 {
		maxCols = 1
	}
	maxRows := m.height / minCellHeight
	if maxRows < 1 {
		maxRows = 1
	}

	// Find layout that fits all workers
	if numWorkers <= maxCols {
		// Single row
		m.gridCols = numWorkers
		m.gridRows = 1
	} else if numWorkers <= maxCols*2 {
		// Two rows
		m.gridCols = int(math.Ceil(float64(numWorkers) / 2))
		m.gridRows = 2
	} else {
		// Fill grid
		m.gridCols = maxCols
		m.gridRows = int(math.Ceil(float64(numWorkers) / float64(maxCols)))
		if m.gridRows > maxRows {
			m.gridRows = maxRows
		}
	}
}

func (m *monitorModel) tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return monitorTickMsg(t)
	})
}

// Update implements tea.Model
func (m *monitorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseMsg:
		m.mouseX = msg.X
		m.mouseY = msg.Y

		// Calculate status bar Y position (at bottom of view)
		gridHeight := m.height - 2 // -2 for status bar
		statusBarY := gridHeight

		// Handle hover detection for motion events
		if msg.Action == tea.MouseActionMotion {
			if msg.Y == statusBarY {
				m.hoveredButton = m.detectStatusBarButton(msg.X)
			} else {
				m.hoveredButton = ""
			}
			return m, nil
		}

		// Handle clicks on status bar buttons
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.Y == statusBarY {
				clickedButton := m.detectStatusBarButton(msg.X)
				switch clickedButton {
				case "r":
					// Refresh command
					m.loading = true
					cmds = append(cmds, m.refreshData())
				}
				return m, tea.Batch(cmds...)
			}
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			// Move down in grid
			newIdx := m.selectedIdx + m.gridCols
			if newIdx < len(m.works) {
				m.selectedIdx = newIdx
			}
		case "k", "up":
			// Move up in grid
			newIdx := m.selectedIdx - m.gridCols
			if newIdx >= 0 {
				m.selectedIdx = newIdx
			}
		case "h", "left":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case "l", "right":
			if m.selectedIdx < len(m.works)-1 {
				m.selectedIdx++
			}
		case "enter":
			// Could expand selected worker view in future
		case "r":
			// Refresh
			m.loading = true
			cmds = append(cmds, m.refreshData())
		case "g":
			// Go to first
			m.selectedIdx = 0
		case "G":
			// Go to last
			if len(m.works) > 0 {
				m.selectedIdx = len(m.works) - 1
			}
		}

	case monitorDataMsg:
		m.loading = false
		m.lastUpdate = time.Now()
		if msg.err == nil {
			m.works = msg.works
			m.recalculateGrid()
			// Ensure selection is valid
			if m.selectedIdx >= len(m.works) {
				m.selectedIdx = len(m.works) - 1
			}
			if m.selectedIdx < 0 {
				m.selectedIdx = 0
			}
		}

	case monitorTickMsg:
		cmds = append(cmds, m.refreshData())
		cmds = append(cmds, m.tick())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View implements tea.Model
func (m *monitorModel) View() string {
	if m.loading && len(m.works) == 0 {
		return "Loading workers..."
	}

	if len(m.works) == 0 {
		return m.renderEmptyState()
	}

	// Render grid of worker panels
	grid := m.renderGrid()

	// Render status bar
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, grid, statusBar)
}

func (m *monitorModel) renderEmptyState() string {
	content := `
  No Active Workers

  Workers will appear here as grid panels.
  Each panel shows:
    - Worker name and status
    - Task list with states
    - Progress indicators

  Press c-w to switch to Work mode and create a worker.
`
	style := lipgloss.NewStyle().
		Padding(2, 4).
		Foreground(lipgloss.Color("247"))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		style.Render(content))
}

func (m *monitorModel) renderGrid() string {
	if len(m.works) == 0 {
		return ""
	}

	// Calculate cell dimensions
	cellWidth := m.width / m.gridCols
	cellHeight := (m.height - 2) / m.gridRows // -2 for status bar
	if cellHeight < 5 {
		cellHeight = 5
	}

	var rows []string

	for row := 0; row < m.gridRows; row++ {
		var rowPanels []string

		for col := 0; col < m.gridCols; col++ {
			idx := row*m.gridCols + col

			if idx < len(m.works) {
				panel := m.renderWorkerPanel(idx, cellWidth, cellHeight)
				rowPanels = append(rowPanels, panel)
			} else {
				// Empty cell
				emptyPanel := m.renderEmptyPanel(cellWidth, cellHeight)
				rowPanels = append(rowPanels, emptyPanel)
			}
		}

		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, rowPanels...))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *monitorModel) renderWorkerPanel(idx int, width, height int) string {
	wp := m.works[idx]
	isSelected := idx == m.selectedIdx

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

	header := fmt.Sprintf("%s %s", statusIcon(wp.work.Status), workerName)
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
		content.WriteString(tuiDimStyle.Render("No tasks"))
		content.WriteString("\n")
	}

	// Task list (show as many as fit)
	linesUsed := 4 // header + id + progress + counts
	availableLines := height - linesUsed - 3 // account for border
	if availableLines < 0 {
		availableLines = 0
	}

	if len(wp.tasks) > 0 && availableLines > 0 {
		content.WriteString("\n")
		for i, tp := range wp.tasks {
			if i >= availableLines {
				remaining := len(wp.tasks) - i
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  +%d more", remaining)))
				break
			}

			icon := statusIcon(tp.task.Status)
			taskID := tp.task.ID
			// Truncate long task IDs
			maxLen := width - 8
			if len(taskID) > maxLen && maxLen > 3 {
				taskID = taskID[:maxLen-3] + "..."
			}
			content.WriteString(fmt.Sprintf("%s %s\n", icon, taskID))
		}
	}

	return panelStyle.Render(content.String())
}

func (m *monitorModel) renderEmptyPanel(width, height int) string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("237")).
		Padding(0, 1).
		Width(width - 2).
		Height(height - 2)

	return panelStyle.Render("")
}

// detectStatusBarButton determines which button is at the given X position in the status bar
func (m *monitorModel) detectStatusBarButton(x int) string {
	// Status bar format: "[←↑↓→]navigate [r]efresh"
	// We need to find the position of each command in the rendered bar

	// Account for the status bar's left padding (tuiStatusBarStyle has Padding(0, 1))
	// This adds 1 character of padding to the left, shifting all content by 1 column
	if x < 1 {
		return ""
	}
	x = x - 1

	statusBar := m.renderStatusBarPlain()

	// Find positions of each button
	refreshIdx := strings.Index(statusBar, "[r]efresh")

	// Check if mouse is over any button (give reasonable width for clickability)
	if refreshIdx >= 0 && x >= refreshIdx && x < refreshIdx+9 {
		return "r"
	}

	return ""
}

// renderStatusBarPlain returns the plain text version of the status bar for position detection
func (m *monitorModel) renderStatusBarPlain() string {
	keysPlain := "[←↑↓→]navigate [r]efresh"

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

	padding := max(m.width-len(keysPlain)-len(statusPlain)-4, 2)
	return keysPlain + strings.Repeat(" ", padding) + statusPlain
}

// styleButtonWithHover styles a button with hover effect if mouse is over it
func (m *monitorModel) styleButtonWithHover(text, buttonKey string) string {
	hoverStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).   // Black text
		Background(lipgloss.Color("214")). // Orange background
		Bold(true)

	if m.hoveredButton == buttonKey {
		return hoverStyle.Render(text)
	}
	return styleHotkeys(text)
}

func (m *monitorModel) renderStatusBar() string {
	// Commands on the left with hover effects
	refreshButton := m.styleButtonWithHover("[r]efresh", "r")
	keysPlain := "[←↑↓→]navigate " + "[r]efresh"
	keys := styleHotkeys("[←↑↓→]navigate") + " " + refreshButton

	// Status on the right - worker count and update time
	var statusParts []string
	if len(m.works) > 0 {
		// Count by status
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

// Command generators
func (m *monitorModel) refreshData() tea.Cmd {
	return func() tea.Msg {
		works, err := fetchPollData(m.ctx, m.proj, "", "")
		return monitorDataMsg{works: works, err: err}
	}
}
