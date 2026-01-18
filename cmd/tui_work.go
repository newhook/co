package cmd

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/process"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/tracking/watcher"
)

// ZoomLevel represents the zoom state of the work mode view
type ZoomLevel int

const (
	// ZoomOverview shows the grid of all works
	ZoomOverview ZoomLevel = iota
	// ZoomZoomedIn shows the 3-panel task view for a selected work
	ZoomZoomedIn
)

// trackingWatcherEventMsg wraps tracking database watcher events
type trackingWatcherEventMsg watcher.WatcherEvent

// workModel is the Work Mode model focused on work/task management
type workModel struct {
	ctx    context.Context
	proj   *project.Project
	width  int
	height int

	// Panel state
	activePanel Panel
	worksCursor int
	tasksCursor int

	// Data
	works   []*workProgress
	loading bool
	focused bool

	// Watcher for tracking database
	trackingWatcher *watcher.Watcher

	// UI state
	viewMode      ViewMode
	spinner       spinner.Model
	textInput     textinput.Model
	statusMessage string
	statusIsError bool
	lastUpdate    time.Time

	// Zoom state
	zoomLevel      ZoomLevel
	selectedWorkID string

	// Grid state (for overview mode)
	gridConfig          GridConfig
	overviewBeadCursor  int  // cursor for navigating beads within selected worker
	overviewShowDetails bool // whether to show the details panel in overview

	// Mouse state
	mouseX           int
	mouseY           int
	hoveredButton    string
	hoveredWorkerIdx int // -1 if no worker hovered
	hoveredBeadIdx   int // -1 if no bead hovered
	hoveredTaskIdx   int // -1 if no task hovered (zoomed mode)

	// Bead selection (for assign dialogs)
	beadItems     []beadItem
	beadsCursor   int
	selectedBeads map[string]bool

	// Create bead state (for work creation with beads and new bead dialog)
	createBeadType     int
	createBeadPriority int
	createDialogFocus  int            // 0=title, 1=type, 2=priority, 3=description
	createDescTextarea textarea.Model // Textarea for description
}

// workDataMsg is sent when data is refreshed
type workDataMsg struct {
	works []*workProgress
	beads []beadItem
	err   error
}

// workCommandMsg indicates a command completed
type workCommandMsg struct {
	action string
	err    error
}

// workTickMsg triggers periodic refresh
type workTickMsg time.Time

// newWorkModel creates a new Work Mode model
func newWorkModel(ctx context.Context, proj *project.Project) *workModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	ti := textinput.New()
	ti.Placeholder = "feature/my-branch"
	ti.CharLimit = 100
	ti.Width = 40

	createDescTa := textarea.New()
	createDescTa.Placeholder = "Enter description (optional)..."
	createDescTa.CharLimit = 2000
	createDescTa.SetWidth(60)
	createDescTa.SetHeight(4)

	// Initialize tracking database watcher
	trackingDBPath := filepath.Join(proj.Root, ".co", "tracking.db")
	trackingWatcher, err := watcher.New(watcher.DefaultConfig(trackingDBPath))
	if err != nil {
		// Log error but continue without watcher
		logging.Warn("Failed to initialize tracking watcher", "error", err)
		trackingWatcher = nil
	} else {
		if err := trackingWatcher.Start(); err != nil {
			// Log error and disable watcher
			logging.Warn("Failed to start tracking watcher", "error", err)
			trackingWatcher = nil
		}
	}

	return &workModel{
		ctx:                ctx,
		proj:               proj,
		width:              80,
		height:             24,
		activePanel:        PanelLeft,
		spinner:            s,
		textInput:          ti,
		selectedBeads:      make(map[string]bool),
		createBeadPriority: 2,
		hoveredWorkerIdx:   -1,
		hoveredBeadIdx:     -1,
		hoveredTaskIdx:     -1,
		createDescTextarea: createDescTa,
		loading:            true,
		trackingWatcher:    trackingWatcher,
	}
}

// Init implements tea.Model
func (m *workModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.refreshData(),
	}

	// Subscribe to watcher events if watcher is available
	if m.trackingWatcher != nil {
		cmds = append(cmds, m.waitForWatcherEvent())
	} else {
		// Fall back to polling if no watcher
		cmds = append(cmds, m.tick())
	}

	return tea.Batch(cmds...)
}

func (m *workModel) tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return workTickMsg(t)
	})
}

// waitForWatcherEvent waits for a watcher event and returns it as a tea.Msg
func (m *workModel) waitForWatcherEvent() tea.Cmd {
	if m.trackingWatcher == nil {
		return nil
	}

	return func() tea.Msg {
		sub := m.trackingWatcher.Broker().Subscribe(m.ctx)

		evt, ok := <-sub
		if !ok {
			return nil
		}

		return trackingWatcherEventMsg(evt.Payload)
	}
}

// cleanup releases resources when the TUI exits
func (m *workModel) cleanup() {
	// Stop the tracking watcher if it's running
	if m.trackingWatcher != nil {
		_ = m.trackingWatcher.Stop()
	}
}

// SetSize implements SubModel
func (m *workModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.recalculateGrid()
}

// recalculateGrid updates the grid configuration based on current works and dimensions
func (m *workModel) recalculateGrid() {
	// Reserve 1 line for status bar
	gridHeight := m.height - 1
	m.gridConfig = CalculateGridDimensions(len(m.works), m.width, gridHeight)
}

// FocusChanged implements SubModel
func (m *workModel) FocusChanged(focused bool) tea.Cmd {
	m.focused = focused
	if focused {
		// Refresh data when gaining focus and restart spinner
		m.loading = true
		cmds := []tea.Cmd{
			m.spinner.Tick,
			m.refreshData(),
		}

		// Use watcher if available, otherwise fall back to polling
		if m.trackingWatcher != nil {
			cmds = append(cmds, m.waitForWatcherEvent())
		} else {
			cmds = append(cmds, m.tick())
		}

		return tea.Batch(cmds...)
	}
	return nil
}

// InModal returns true if in a modal/dialog state
func (m *workModel) InModal() bool {
	return m.viewMode != ViewNormal
}

// Update implements tea.Model
func (m *workModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseMsg:
		m.mouseX = msg.X
		m.mouseY = msg.Y

		// Calculate status bar Y position (at bottom of view)
		statusBarY := m.height - 1

		// Handle hover detection for motion events
		if msg.Action == tea.MouseActionMotion {
			if msg.Y == statusBarY {
				m.hoveredButton = m.detectStatusBarButton(msg.X)
				m.hoveredWorkerIdx = -1
				m.hoveredBeadIdx = -1
				m.hoveredTaskIdx = -1
			} else {
				m.hoveredButton = ""
				// Detect hover over beads in overview mode or tasks in zoomed mode
				if m.zoomLevel == ZoomOverview {
					m.detectOverviewHover(msg.X, msg.Y)
					m.hoveredTaskIdx = -1
				} else {
					m.hoveredWorkerIdx = -1
					m.hoveredBeadIdx = -1
					m.detectZoomedHover(msg.X, msg.Y)
				}
			}
			return m, nil
		}

		// Handle clicks on status bar buttons
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.Y == statusBarY {
				clickedButton := m.detectStatusBarButton(msg.X)
				// Check if we can act on the selected work (overview mode or left panel in zoomed)
				canActOnWork := m.zoomLevel == ZoomOverview || m.activePanel == PanelLeft

				// Trigger the corresponding action by simulating a key press
				switch clickedButton {
				case "n":
					// Create new bead and assign to current work
					if canActOnWork && len(m.works) > 0 {
						m.viewMode = ViewCreateBead
						m.textInput.Reset()
						m.textInput.Placeholder = "Enter issue title..."
						m.textInput.Focus()
						m.createBeadType = 0
						m.createBeadPriority = 2
						m.createDialogFocus = 0
						m.createDescTextarea.Reset()
					}
					return m, nil
				case "d":
					// Destroy work
					if canActOnWork && len(m.works) > 0 {
						m.viewMode = ViewDestroyConfirm
					}
					return m, nil
				case "r":
					// Run work with LLM estimation
					if canActOnWork && len(m.works) > 0 {
						return m, m.runWork(true)
					}
					return m, nil
				case "s":
					// Run work simple (one task per issue)
					if canActOnWork && len(m.works) > 0 {
						return m, m.runWork(false)
					}
					return m, nil
				case "a":
					// Assign beads to work
					if canActOnWork && len(m.works) > 0 {
						m.viewMode = ViewAssignBeads
						m.beadsCursor = 0
						return m, m.loadBeadsForAssign()
					}
					return m, nil
				case "t":
					// Open console tab for selected work
					if canActOnWork && len(m.works) > 0 {
						return m, m.openConsole()
					}
					return m, nil
				case "c":
					// Open Claude Code session tab for selected work
					if canActOnWork && len(m.works) > 0 {
						return m, m.openClaude()
					}
					return m, nil
				case "o":
					// Restart orchestrator for selected work
					if canActOnWork && len(m.works) > 0 {
						return m, m.restartOrchestrator()
					}
					return m, nil
				case "v":
					// Create review task
					if canActOnWork && len(m.works) > 0 {
						return m, m.createReviewTask()
					}
					return m, nil
				case "p":
					// Create PR task
					if canActOnWork && len(m.works) > 0 {
						return m, m.createPRTask()
					}
					return m, nil
				case "u":
					// Update PR description
					if canActOnWork && len(m.works) > 0 {
						return m, m.updatePRDescriptionTask()
					}
					return m, nil
				case "?":
					m.viewMode = ViewHelp
					return m, nil
				}
			} else if m.zoomLevel == ZoomOverview {
				// Handle clicks in the grid area (overview mode)
				m.handleOverviewClick(msg.X, msg.Y)
			} else if m.zoomLevel == ZoomZoomedIn {
				// Handle clicks in the zoomed view (tasks panel)
				m.handleZoomedClick(msg.X, msg.Y)
			}
		}

		return m, nil

	case tea.KeyMsg:
		// Handle view-specific keys first
		switch m.viewMode {
		case ViewCreateBead:
			return m.updateCreateBead(msg)
		case ViewDestroyConfirm:
			return m.updateDestroyConfirm(msg)
		case ViewAssignBeads:
			return m.updateAssignBeads(msg)
		case ViewHelp:
			m.viewMode = ViewNormal
			return m, nil
		case ViewNormal:
			return m.updateNormal(msg)
		}

	case workDataMsg:
		m.loading = false
		m.lastUpdate = time.Now()
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Error: %v", msg.err)
			m.statusIsError = true
		} else {
			m.works = msg.works
			m.beadItems = msg.beads
			m.statusMessage = ""
			m.statusIsError = false
			m.recalculateGrid()
		}

		// Ensure cursor is valid
		if m.worksCursor >= len(m.works) {
			m.worksCursor = len(m.works) - 1
		}
		if m.worksCursor < 0 {
			m.worksCursor = 0
		}

	case workCommandMsg:
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("%s failed: %v", msg.action, msg.err)
			m.statusIsError = true
		} else {
			m.statusMessage = fmt.Sprintf("%s completed", msg.action)
			m.statusIsError = false
			// Clear bead selections after successful assignment
			if strings.HasPrefix(msg.action, "Assigned") {
				m.selectedBeads = make(map[string]bool)
			}
		}
		cmds = append(cmds, m.refreshData())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case workTickMsg:
		// Periodic refresh (fallback when no watcher)
		cmds = append(cmds, m.refreshData())
		cmds = append(cmds, m.tick())

	case trackingWatcherEventMsg:
		// Handle watcher events
		if msg.Type == watcher.DBChanged {
			// Trigger data reload and wait for next watcher event
			cmds = append(cmds, m.refreshData())
			cmds = append(cmds, m.waitForWatcherEvent())
		} else if msg.Type == watcher.WatcherError {
			// Log error and continue waiting for events
			logging.Error("Tracking watcher error", "error", msg.Error)
			cmds = append(cmds, m.waitForWatcherEvent())
		} else {
			// Continue waiting for next event
			cmds = append(cmds, m.waitForWatcherEvent())
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *workModel) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle grid navigation when in overview mode
	if m.zoomLevel == ZoomOverview {
		switch msg.String() {
		case "j", "down":
			// Move down through beads in current worker
			if len(m.works) > 0 && m.worksCursor < len(m.works) {
				wp := m.works[m.worksCursor]
				if m.overviewBeadCursor < len(wp.workBeads)-1 {
					m.overviewBeadCursor++
				}
			}
			return m, nil
		case "k", "up":
			// Move up through beads in current worker
			if m.overviewBeadCursor > 0 {
				m.overviewBeadCursor--
			}
			return m, nil
		case "h", "left":
			// Move to previous worker
			if m.worksCursor > 0 {
				m.worksCursor--
				m.overviewBeadCursor = 0 // Reset bead cursor when changing worker
			}
			return m, nil
		case "l", "right":
			// Move to next worker
			if m.worksCursor < len(m.works)-1 {
				m.worksCursor++
				m.overviewBeadCursor = 0 // Reset bead cursor when changing worker
			}
			return m, nil
		case "tab":
			// Cycle through workers
			if len(m.works) > 0 {
				m.worksCursor = (m.worksCursor + 1) % len(m.works)
				m.overviewBeadCursor = 0 // Reset bead cursor when changing worker
			}
			return m, nil
		case "shift+tab":
			// Cycle backwards through workers
			if len(m.works) > 0 {
				m.worksCursor--
				if m.worksCursor < 0 {
					m.worksCursor = len(m.works) - 1
				}
				m.overviewBeadCursor = 0 // Reset bead cursor when changing worker
			}
			return m, nil
		case "g":
			// Go to first bead
			m.overviewBeadCursor = 0
			return m, nil
		case "G":
			// Go to last bead
			if len(m.works) > 0 && m.worksCursor < len(m.works) {
				wp := m.works[m.worksCursor]
				if len(wp.workBeads) > 0 {
					m.overviewBeadCursor = len(wp.workBeads) - 1
				}
			}
			return m, nil
		case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// Direct zoom to panel by number (0-9)
			panelIdx := int(msg.String()[0] - '0')
			if panelIdx < len(m.works) && panelIdx < 10 {
				m.worksCursor = panelIdx
				m.overviewBeadCursor = 0
				m.zoomLevel = ZoomZoomedIn
				m.selectedWorkID = m.works[panelIdx].work.ID
				m.activePanel = PanelMiddle // Start on tasks panel (no left panel in zoom mode)
				m.tasksCursor = 0
			}
			return m, nil
		case "enter":
			// Zoom in on selected work
			if len(m.works) > 0 && m.worksCursor < len(m.works) {
				m.zoomLevel = ZoomZoomedIn
				m.selectedWorkID = m.works[m.worksCursor].work.ID
				m.activePanel = PanelMiddle // Start on tasks panel (no left panel in zoom mode)
				m.tasksCursor = 0
			}
			return m, nil
		case "d":
			// Destroy selected work
			if len(m.works) > 0 {
				m.viewMode = ViewDestroyConfirm
			}
			return m, nil
		case "r":
			// Run selected work with LLM estimation
			if len(m.works) > 0 {
				return m, m.runWork(true)
			}
			return m, nil
		case "s":
			// Run selected work simple (one task per issue)
			if len(m.works) > 0 {
				return m, m.runWork(false)
			}
			return m, nil
		case "a":
			// Assign beads to selected work
			if len(m.works) > 0 {
				m.viewMode = ViewAssignBeads
				m.beadsCursor = 0
				return m, m.loadBeadsForAssign()
			}
			return m, nil
		case "v":
			// Create review task
			if len(m.works) > 0 {
				return m, m.createReviewTask()
			}
			return m, nil
		case "p":
			// Create PR task
			if len(m.works) > 0 {
				return m, m.createPRTask()
			}
			return m, nil
		case "u":
			// Update PR description
			if len(m.works) > 0 {
				return m, m.updatePRDescriptionTask()
			}
			return m, nil
		case "t":
			// Open console tab for selected work
			if len(m.works) > 0 {
				return m, m.openConsole()
			}
			return m, nil
		case "c":
			// Open Claude Code session tab for selected work
			if len(m.works) > 0 {
				return m, m.openClaude()
			}
			return m, nil
		case "?":
			m.viewMode = ViewHelp
			return m, nil
		}
	}

	switch msg.String() {
	case "j", "down":
		if m.activePanel == PanelLeft {
			if m.worksCursor < len(m.works)-1 {
				m.worksCursor++
				m.tasksCursor = 0
			}
		} else if m.activePanel == PanelMiddle {
			if len(m.works) > 0 && m.worksCursor < len(m.works) {
				wp := m.works[m.worksCursor]
				// Total items = tasks + unassigned issues
				totalItems := len(wp.tasks) + len(wp.unassignedBeads)
				if m.tasksCursor < totalItems-1 {
					m.tasksCursor++
				}
			}
		}

	case "k", "up":
		if m.activePanel == PanelLeft {
			if m.worksCursor > 0 {
				m.worksCursor--
				m.tasksCursor = 0
			}
		} else if m.activePanel == PanelMiddle {
			if m.tasksCursor > 0 {
				m.tasksCursor--
			}
		}

	case "h", "left":
		// In zoomed mode, only toggle between Middle and Right (no Left panel)
		if m.zoomLevel == ZoomZoomedIn {
			if m.activePanel == PanelRight {
				m.activePanel = PanelMiddle
			}
		} else if m.activePanel > PanelLeft {
			m.activePanel--
		}

	case "l", "right":
		// In zoomed mode, only toggle between Middle and Right (no Left panel)
		if m.zoomLevel == ZoomZoomedIn {
			if m.activePanel == PanelMiddle {
				m.activePanel = PanelRight
			}
		} else if m.activePanel < PanelRight {
			m.activePanel++
		}

	case "tab":
		if m.zoomLevel == ZoomZoomedIn {
			// Toggle between Middle and Right panels only (no Left panel in zoom mode)
			if m.activePanel == PanelMiddle {
				m.activePanel = PanelRight
			} else {
				m.activePanel = PanelMiddle
			}
		}

	case "1":
		if m.zoomLevel == ZoomZoomedIn {
			m.activePanel = PanelMiddle // 1 = Tasks panel
		}
	case "2":
		if m.zoomLevel == ZoomZoomedIn {
			m.activePanel = PanelRight // 2 = Details panel
		}

	case "d":
		// Destroy work (PanelLeft in overview, or PanelMiddle in zoomed mode)
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			m.viewMode = ViewDestroyConfirm
		}

	case "r":
		// Run work with LLM estimation (PanelLeft in overview, or PanelMiddle in zoomed mode)
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.runWork(true)
		}

	case "o":
		// Restart orchestrator for selected work (PanelLeft in overview, or PanelMiddle in zoomed mode)
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.restartOrchestrator()
		}

	case "s":
		// Run work simple - one task per issue (PanelLeft in overview, or PanelMiddle in zoomed mode)
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.runWork(false)
		}

	case "a":
		// Assign beads to work (PanelLeft in overview, or PanelMiddle in zoomed mode)
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			m.viewMode = ViewAssignBeads
			m.beadsCursor = 0
			// Load beads for selection
			return m, m.loadBeadsForAssign()
		}

	case "v":
		// Create review task
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.createReviewTask()
		}

	case "p":
		// Create PR task
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.createPRTask()
		}

	case "u":
		// Update PR description task
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.updatePRDescriptionTask()
		}

	case "c":
		// Open Claude Code session tab for selected work
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.openClaude()
		}

	case "x":
		// Remove unassigned issue from work
		if m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle && len(m.works) > 0 {
			wp := m.works[m.worksCursor]
			unassignedIdx := m.tasksCursor - len(wp.tasks)
			if unassignedIdx >= 0 && unassignedIdx < len(wp.unassignedBeads) {
				beadID := wp.unassignedBeads[unassignedIdx].id
				return m, m.removeBeadFromWork(beadID)
			}
		}

	case "n":
		// Create new bead and assign to current work
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			m.viewMode = ViewCreateBead
			m.textInput.Reset()
			m.textInput.Placeholder = "Enter issue title..."
			m.textInput.Focus()
			m.createBeadType = 0
			m.createBeadPriority = 2
			m.createDialogFocus = 0 // Start with title focused
			m.createDescTextarea.Reset()
		}

	case "t":
		// Open console tab for selected work
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.openConsole()
		}

	case "C":
		// Open Claude Code session tab for selected work
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.openClaude()
		}

	case "?":
		m.viewMode = ViewHelp

	case "esc":
		// Zoom out to overview
		if m.zoomLevel == ZoomZoomedIn {
			m.zoomLevel = ZoomOverview
			m.selectedWorkID = ""
		}

	case "g":
		// Go to top (only in zoomed view - grid handles overview)
		if m.zoomLevel == ZoomZoomedIn {
			if m.activePanel == PanelLeft {
				m.worksCursor = 0
				m.tasksCursor = 0
			} else if m.activePanel == PanelMiddle {
				m.tasksCursor = 0
			}
		}

	case "G":
		// Go to bottom (only in zoomed view - grid handles overview)
		if m.zoomLevel == ZoomZoomedIn {
			if m.activePanel == PanelLeft && len(m.works) > 0 {
				m.worksCursor = len(m.works) - 1
				m.tasksCursor = 0
			} else if m.activePanel == PanelMiddle && len(m.works) > 0 {
				wp := m.works[m.worksCursor]
				if len(wp.tasks) > 0 {
					m.tasksCursor = len(wp.tasks) - 1
				}
			}
		}
	}

	return m, nil
}

func (m *workModel) updateDestroyConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.viewMode = ViewNormal
		if len(m.works) > 0 {
			return m, m.destroyWork()
		}
	case "n", "N", "esc":
		m.viewMode = ViewNormal
	}
	return m, nil
}

func (m *workModel) updateAssignBeads(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.beadsCursor < len(m.beadItems)-1 {
			m.beadsCursor++
		}
	case "k", "up":
		if m.beadsCursor > 0 {
			m.beadsCursor--
		}
	case " ":
		// Toggle selection
		if len(m.beadItems) > 0 {
			id := m.beadItems[m.beadsCursor].id
			m.selectedBeads[id] = !m.selectedBeads[id]
		}
	case "a":
		// Select all / deselect all toggle
		allSelected := true
		for _, bead := range m.beadItems {
			if !m.selectedBeads[bead.id] {
				allSelected = false
				break
			}
		}
		if allSelected {
			// Deselect all
			m.selectedBeads = make(map[string]bool)
		} else {
			// Select all
			for _, bead := range m.beadItems {
				m.selectedBeads[bead.id] = true
			}
		}
	case "enter":
		m.viewMode = ViewNormal
		return m, m.assignSelectedBeads()
	case "esc":
		m.viewMode = ViewNormal
		m.selectedBeads = make(map[string]bool)
	}
	return m, nil
}

func (m *workModel) updateCreateBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check escape/cancel keys
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.createDescTextarea.Blur()
		return m, nil
	}

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> description(3) -> title(0)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		m.createDialogFocus = (m.createDialogFocus + 1) % 4
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
			m.createDescTextarea.Blur()
		} else if m.createDialogFocus == 3 {
			m.textInput.Blur()
			m.createDescTextarea.Focus()
		} else {
			m.textInput.Blur()
			m.createDescTextarea.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.createDialogFocus--
		if m.createDialogFocus < 0 {
			m.createDialogFocus = 3
		}
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
			m.createDescTextarea.Blur()
		} else if m.createDialogFocus == 3 {
			m.textInput.Blur()
			m.createDescTextarea.Focus()
		} else {
			m.textInput.Blur()
			m.createDescTextarea.Blur()
		}
		return m, nil
	}

	// Enter submits from any field (but not from description textarea - use Ctrl+Enter there)
	if msg.String() == "enter" && m.createDialogFocus != 3 {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			beadType := beadTypes[m.createBeadType]
			isEpic := beadType == "epic"
			description := strings.TrimSpace(m.createDescTextarea.Value())
			m.viewMode = ViewNormal
			m.createDescTextarea.Reset()
			return m, m.createBeadAndAssign(title, beadType, m.createBeadPriority, isEpic, description)
		}
		return m, nil
	}

	// Ctrl+Enter submits from description textarea
	if msg.String() == "ctrl+enter" && m.createDialogFocus == 3 {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			beadType := beadTypes[m.createBeadType]
			isEpic := beadType == "epic"
			description := strings.TrimSpace(m.createDescTextarea.Value())
			m.viewMode = ViewNormal
			m.createDescTextarea.Reset()
			return m, m.createBeadAndAssign(title, beadType, m.createBeadPriority, isEpic, description)
		}
		return m, nil
	}

	// Handle input based on focused element
	switch m.createDialogFocus {
	case 0: // Title input
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd

	case 1: // Type selector
		switch msg.String() {
		case "j", "down", "right":
			m.createBeadType = (m.createBeadType + 1) % len(beadTypes)
		case "k", "up", "left":
			m.createBeadType--
			if m.createBeadType < 0 {
				m.createBeadType = len(beadTypes) - 1
			}
		}
		return m, nil

	case 2: // Priority
		switch msg.String() {
		case "j", "down", "right", "-":
			if m.createBeadPriority < 4 {
				m.createBeadPriority++
			}
		case "k", "up", "left", "+", "=":
			if m.createBeadPriority > 0 {
				m.createBeadPriority--
			}
		}
		return m, nil

	case 3: // Description textarea
		var cmd tea.Cmd
		m.createDescTextarea, cmd = m.createDescTextarea.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View implements tea.Model
func (m *workModel) View() string {
	if m.viewMode == ViewHelp {
		return m.renderHelp()
	}

	// Handle dialogs first
	switch m.viewMode {
	case ViewCreateBead:
		// In zoomed mode, render inline in details panel (fall through)
		// In overview mode, use dialog
		if m.zoomLevel == ZoomOverview {
			dialog := m.renderCreateBeadDialogContent()
			return m.renderWithDialog(dialog)
		}
		// Fall through to normal rendering - details panel will show the form
	case ViewDestroyConfirm:
		dialog := m.renderDestroyConfirmDialog()
		return m.renderWithDialog(dialog)
	case ViewAssignBeads:
		// In zoomed mode, render inline in details panel (fall through)
		// In overview mode, use full-screen view
		if m.zoomLevel == ZoomOverview {
			return m.renderAssignBeadsView()
		}
		// Fall through to normal rendering - details panel will show the assign beads form
	}

	// Render based on zoom level
	if m.zoomLevel == ZoomOverview {
		return m.renderOverviewGrid()
	}

	// Zoomed in view: 2-panel layout (Tasks | Details)
	// Calculate panel dimensions (40/60 ratio)
	panelWidth1 := m.width * 40 / 100
	panelWidth2 := m.width - panelWidth1
	// Reserve 1 line for status bar, and account for panel borders (2 lines per panel)
	panelHeight := m.height - 1 - 2 // -1 for status bar, -2 for border

	// Render two panels: Tasks | Details
	tasksPanel := m.renderTasksPanel(panelWidth1, panelHeight)
	detailsPanel := m.renderDetailsPanel(panelWidth2, panelHeight)

	content := lipgloss.JoinHorizontal(lipgloss.Top, tasksPanel, detailsPanel)

	// Render status bar
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

// renderOverviewGrid renders the grid view of all works
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

func (m *workModel) renderWithDialog(dialog string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

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

func (m *workModel) renderDestroyConfirmDialog() string {
	workID := ""
	workerName := ""
	if len(m.works) > 0 && m.worksCursor < len(m.works) {
		workID = m.works[m.worksCursor].work.ID
		workerName = m.works[m.worksCursor].work.Name
	}

	displayName := workID
	if workerName != "" {
		displayName = fmt.Sprintf("%s (%s)", workerName, workID)
	}

	content := fmt.Sprintf(`
  Destroy Work

  Are you sure you want to destroy %s?
  This will remove the worktree and all task data.

  [y] Yes  [n] No
`, displayName)

	return tuiDialogStyle.Render(content)
}

func (m *workModel) renderCreateBeadDialogContent() string {
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

	// Show which work the bead will be assigned to
	workInfo := ""
	if len(m.works) > 0 && m.worksCursor < len(m.works) {
		wp := m.works[m.worksCursor]
		displayName := wp.work.ID
		if wp.work.Name != "" {
			displayName = fmt.Sprintf("%s (%s)", wp.work.Name, wp.work.ID)
		}
		workInfo = fmt.Sprintf("\n  Assign to: %s", tuiValueStyle.Render(displayName))
	}

	content := fmt.Sprintf(`  Create New Issue%s

  %s
  %s

  %s %s
  %s %s

  %s
%s

  [Tab] Next field  [Enter] Create  [Esc] Cancel
`, workInfo, titleLabel, m.textInput.View(), typeLabel, typeDisplay, priorityLabel, priorityDisplay, descLabel, indentLines(m.createDescTextarea.View(), "  "))

	return tuiDialogStyle.Render(content)
}

func (m *workModel) renderAssignBeadsView() string {
	var b strings.Builder

	b.WriteString(tuiTitleStyle.Render("Assign Issues to Work"))
	b.WriteString("\n\n")

	if len(m.works) > 0 && m.worksCursor < len(m.works) {
		wp := m.works[m.worksCursor]
		b.WriteString(tuiLabelStyle.Render("Target: "))
		if wp.work.Name != "" {
			b.WriteString(tuiValueStyle.Render(fmt.Sprintf("%s (%s)", wp.work.Name, wp.work.ID)))
		} else {
			b.WriteString(tuiValueStyle.Render(wp.work.ID))
		}
		b.WriteString("\n\n")
	}

	b.WriteString("Select issues (Space to toggle, Enter to confirm, Esc to cancel):\n\n")

	for i, bead := range m.beadItems {
		var checkbox string
		if m.selectedBeads[bead.id] {
			checkbox = "[●]"
		} else {
			checkbox = "[ ]"
		}

		line := fmt.Sprintf("%s %s - %s", checkbox, bead.id, bead.title)

		if i == m.beadsCursor {
			line = tuiSelectedStyle.Render("> " + line)
		} else {
			line = "  " + line
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	selected := 0
	for _, s := range m.selectedBeads {
		if s {
			selected++
		}
	}
	b.WriteString(fmt.Sprintf("\n%d issue(s) selected", selected))

	return tuiAssignStyle.Width(m.width).Height(m.height).Render(b.String())
}

func (m *workModel) renderHelp() string {
	help := `
  Work Mode - Help

  View States
  ────────────────────────────
  Overview      Grid of all workers (default)
  Zoomed        3-panel task view for selected work

  Navigation (Overview/Grid)
  ────────────────────────────
  h/l, ←/→      Move between grid cells
  j/k, ↑/↓      Move up/down in grid
  Enter         Zoom into selected work
  g             Go to first work
  G             Go to last work

  Navigation (Zoomed/3-Panel)
  ────────────────────────────
  h/l, ←/→      Move between panels
  j/k, ↑/↓      Navigate list
  Tab           Cycle panels
  Esc           Zoom out to overview

  Work Management (Zoomed Mode)
  ────────────────────────────
  n             Create new issue (assign to work)
  a             Assign existing issues to work
  r             Run work with plan (LLM estimates)
  s             Run work simple (no planning)
  x             Remove selected unassigned issue
  t             Open terminal/console tab
  c             Open Claude Code session
  o             Restart orchestrator
  v             Create review task
  p             Create PR task
  u             Update PR description
  d             Destroy selected work

  General
  ────────────────────────────
  ?             Show this help

  Press any key to close...
`
	return tuiHelpStyle.Width(m.width).Height(m.height).Render(help)
}

// Command generators
func (m *workModel) refreshData() tea.Cmd {
	return func() tea.Msg {
		works, err := fetchPollData(m.ctx, m.proj, "", "")
		if err != nil {
			return workDataMsg{err: err}
		}

		// Also fetch beads for potential assignment
		beads, _ := fetchBeadsWithFilters(m.proj.MainRepoPath(), beadFilters{status: "ready"})

		return workDataMsg{works: works, beads: beads}
	}
}

func (m *workModel) loadBeadsForAssign() tea.Cmd {
	return func() tea.Msg {
		beads, err := fetchBeadsWithFilters(m.proj.MainRepoPath(), beadFilters{status: "ready"})
		if err != nil {
			return workDataMsg{err: err}
		}
		return workDataMsg{beads: beads}
	}
}

func (m *workModel) destroyWork() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Destroy work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		if err := DestroyWork(m.ctx, m.proj, workID, io.Discard); err != nil {
			return workCommandMsg{action: "Destroy work", err: err}
		}
		return workCommandMsg{action: "Destroy work"}
	}
}

func (m *workModel) removeBeadFromWork(beadID string) tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Remove issue", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		// Remove the bead from the work using the database
		if err := m.proj.DB.RemoveWorkBead(m.ctx, workID, beadID); err != nil {
			return workCommandMsg{action: "Remove issue", err: err}
		}
		return workCommandMsg{action: fmt.Sprintf("Removed %s from work", beadID)}
	}
}

func (m *workModel) runWork(usePlan bool) tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Run work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		result, err := RunWork(m.ctx, m.proj, workID, usePlan, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Run work", err: err}
		}

		orchestratorStatus := "running"
		if result.OrchestratorSpawned {
			orchestratorStatus = "spawned"
		}

		var msg string
		modeStr := ""
		if usePlan {
			modeStr = " (with estimation)"
		}
		if result.TasksCreated > 0 {
			msg = fmt.Sprintf("Created %d task(s)%s, orchestrator %s", result.TasksCreated, modeStr, orchestratorStatus)
		} else {
			msg = fmt.Sprintf("Orchestrator %s", orchestratorStatus)
		}
		return workCommandMsg{action: msg}
	}
}

func (m *workModel) restartOrchestrator() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Restart orchestrator", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Get the work details
		work, err := m.proj.DB.GetWork(m.ctx, workID)
		if err != nil || work == nil {
			return workCommandMsg{action: "Restart orchestrator", err: fmt.Errorf("work not found: %w", err)}
		}

		// Kill any existing orchestrator
		projectName := m.proj.Config.Project.Name

		// Check if process is running and kill it
		pattern := fmt.Sprintf("co orchestrate --work %s", workID)
		if running, _ := process.IsProcessRunning(m.ctx, pattern); running {
			// Process is running, kill it
			process.KillProcess(m.ctx, pattern) // Ignore error as process might have already exited
			time.Sleep(500 * time.Millisecond)
		}

		// Ensure the orchestrator is running (will restart if dead)
		spawned, err := claude.EnsureWorkOrchestrator(
			m.ctx,
			workID,
			projectName,
			work.WorktreePath,
			work.Name,
			io.Discard,
		)
		if err != nil {
			return workCommandMsg{action: "Restart orchestrator", err: err}
		}

		status := "already running"
		if spawned {
			status = "restarted"
		}
		return workCommandMsg{action: fmt.Sprintf("Orchestrator %s", status)}
	}
}

func (m *workModel) createReviewTask() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create review", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Get existing tasks to generate unique review task ID
		ctx := m.ctx
		tasks, err := m.proj.DB.GetWorkTasks(ctx, workID)
		if err != nil {
			return workCommandMsg{action: "Create review", err: fmt.Errorf("failed to get work tasks: %w", err)}
		}

		// Count existing review tasks
		reviewCount := 0
		reviewPrefix := fmt.Sprintf("%s.review", workID)
		for _, task := range tasks {
			if strings.HasPrefix(task.ID, reviewPrefix) {
				reviewCount++
			}
		}

		// Generate unique review task ID
		reviewTaskID := fmt.Sprintf("%s.review-%d", workID, reviewCount+1)

		// Create the review task
		err = m.proj.DB.CreateTask(ctx, reviewTaskID, "review", []string{}, 0, workID)
		if err != nil {
			return workCommandMsg{action: "Create review", err: fmt.Errorf("failed to create task: %w", err)}
		}

		// Include iteration count in success message
		maxIterations := m.proj.Config.Workflow.GetMaxReviewIterations()
		var actionMsg string
		if reviewCount >= maxIterations {
			// Already exceeded the limit, just note it
			actionMsg = fmt.Sprintf("Created review task %s (iteration %d, exceeds limit of %d)", reviewTaskID, reviewCount+1, maxIterations)
		} else if reviewCount+1 == maxIterations {
			// At the limit now
			actionMsg = fmt.Sprintf("Created review task %s (%d/%d iterations - at limit)", reviewTaskID, reviewCount+1, maxIterations)
		} else {
			// Still under the limit
			actionMsg = fmt.Sprintf("Created review task %s (%d/%d iterations)", reviewTaskID, reviewCount+1, maxIterations)
		}

		return workCommandMsg{action: actionMsg}
	}
}

func (m *workModel) createPRTask() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create PR", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Check if work is completed
		if wp.work.Status != db.StatusCompleted {
			return workCommandMsg{action: "Create PR", err: fmt.Errorf("work %s is not completed (status: %s)", workID, wp.work.Status)}
		}

		// Check if PR already exists
		if wp.work.PRURL != "" {
			return workCommandMsg{action: fmt.Sprintf("PR already exists: %s", wp.work.PRURL)}
		}

		// Generate task ID for PR creation
		prTaskID := fmt.Sprintf("%s.pr", workID)

		// Create the PR task
		ctx := m.ctx
		err := m.proj.DB.CreateTask(ctx, prTaskID, "pr", []string{}, 0, workID)
		if err != nil {
			return workCommandMsg{action: "Create PR", err: fmt.Errorf("failed to create task: %w", err)}
		}

		return workCommandMsg{action: fmt.Sprintf("Created PR task %s", prTaskID)}
	}
}

func (m *workModel) updatePRDescriptionTask() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Update PR", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		// Check if work has a PR
		if wp.work.PRURL == "" {
			return workCommandMsg{action: "Update PR", err: fmt.Errorf("work %s has no PR", workID)}
		}

		// Create update-pr-description task
		taskID := fmt.Sprintf("%s.update-pr-%d", workID, time.Now().Unix())
		ctx := context.Background()
		err := m.proj.DB.CreateTask(ctx, taskID, "update-pr-description", []string{}, 0, workID)
		if err != nil {
			return workCommandMsg{action: "Update PR", err: err}
		}

		// Process the task
		cmd := exec.Command("co", "orchestrate", "--task", taskID)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Update PR", err: err}
		}
		return workCommandMsg{action: "Update PR"}
	}
}

func (m *workModel) assignSelectedBeads() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Assign beads", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		// Collect selected bead IDs
		var beadIDs []string
		for id, selected := range m.selectedBeads {
			if selected {
				beadIDs = append(beadIDs, id)
			}
		}

		if len(beadIDs) == 0 {
			return workCommandMsg{action: "Assign beads", err: fmt.Errorf("no beads selected")}
		}

		result, err := AddBeadsToWork(m.ctx, m.proj, workID, beadIDs)
		if err != nil {
			return workCommandMsg{action: "Assign beads", err: err}
		}

		return workCommandMsg{action: fmt.Sprintf("Assigned %d bead(s)", result.BeadsAdded)}
	}
}

func (m *workModel) createBeadAndAssign(title, beadType string, priority int, isEpic bool, description string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID
		mainRepoPath := m.proj.MainRepoPath()

		// Create the bead using beads package
		beadID, err := beads.Create(ctx, mainRepoPath, beads.CreateOptions{
			Title:       title,
			Type:        beadType,
			Priority:    priority,
			IsEpic:      isEpic,
			Description: description,
		})
		if err != nil {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("failed to create issue: %w", err)}
		}

		// Assign the bead to the current work
		_, err = AddBeadsToWork(m.ctx, m.proj, workID, []string{beadID})
		if err != nil {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("created issue %s but failed to assign to work: %w", beadID, err)}
		}

		return workCommandMsg{action: fmt.Sprintf("Created and assigned %s", beadID)}
	}
}

func (m *workModel) openConsole() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Open console", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		err := claude.OpenConsole(m.ctx, workID, m.proj.Config.Project.Name, wp.work.WorktreePath, wp.work.Name, m.proj.Config.Hooks.Env, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Open console", err: err}
		}

		return workCommandMsg{action: fmt.Sprintf("Opened console for %s", workID)}
	}
}

func (m *workModel) openClaude() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Open Claude session", err: fmt.Errorf("no work selected")}
		}
		wp := m.works[m.worksCursor]
		workID := wp.work.ID

		err := claude.OpenClaudeSession(m.ctx, workID, m.proj.Config.Project.Name, wp.work.WorktreePath, wp.work.Name, m.proj.Config.Hooks.Env, m.proj.Config, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Open Claude session", err: err}
		}

		return workCommandMsg{action: fmt.Sprintf("Opened Claude session for %s", workID)}
	}
}

// handleOverviewClick handles mouse clicks in the overview grid area
// detectOverviewHover detects which worker/bead is being hovered in overview mode
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

	// Check if click is in the tasks panel (left side)
	if x >= panelWidth {
		return // Click was in the details panel
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
	// Check if orchestrator process is running
	pattern := fmt.Sprintf("co orchestrate --work %s", workID)
	running, err := process.IsProcessRunning(ctx, pattern)
	if err != nil {
		// Log error but treat as not running
		logging.Debug("Failed to check orchestrator health", "error", err, "workID", workID)
		return false
	}
	return running
}
