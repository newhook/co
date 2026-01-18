package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/logging"
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
						m.selectedBeads = make(map[string]bool) // Initialize selection map
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
			// Only update works if provided (not nil)
			if msg.works != nil {
				m.works = msg.works
			}
			// Only update beads if provided (not nil)
			if msg.beads != nil {
				m.beadItems = msg.beads
			}
			m.statusMessage = ""
			m.statusIsError = false
			// Only recalculate grid if works were updated
			if msg.works != nil {
				m.recalculateGrid()
			}
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
	// Delegate to appropriate handler based on zoom level
	if m.zoomLevel == ZoomOverview {
		return m.updateOverviewKeys(msg)
	}
	return m.updateZoomedKeys(msg)
}

// updateOverviewKeys handles keyboard input in overview mode
func (m *workModel) updateOverviewKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		// Default - no matching key in overview mode
		return m, nil
}

// updateZoomedKeys handles keyboard input in zoomed mode
func (m *workModel) updateZoomedKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m, nil

	case "o":
		// Restart orchestrator for selected work (PanelLeft in overview, or PanelMiddle in zoomed mode)
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.restartOrchestrator()
		}
		return m, nil

	case "s":
		// Run work simple - one task per issue (PanelLeft in overview, or PanelMiddle in zoomed mode)
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.runWork(false)
		}
		return m, nil

	case "a":
		// Assign beads to work (PanelLeft in overview, or PanelMiddle in zoomed mode)
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			m.viewMode = ViewAssignBeads
			m.beadsCursor = 0
			m.selectedBeads = make(map[string]bool) // Initialize selection map
			// Load beads for selection
			return m, m.loadBeadsForAssign()
		}
		return m, nil

	case "v":
		// Create review task
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.createReviewTask()
		}
		return m, nil

	case "p":
		// Create PR task
		canActOnWork := m.activePanel == PanelLeft || (m.zoomLevel == ZoomZoomedIn && m.activePanel == PanelMiddle)
		if canActOnWork && len(m.works) > 0 {
			return m, m.createPRTask()
		}
		return m, nil

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
		// Count selected beads
		selectedCount := 0
		for _, selected := range m.selectedBeads {
			if selected {
				selectedCount++
			}
		}

		// Only proceed if beads are selected
		if selectedCount > 0 {
			m.viewMode = ViewNormal
			return m, m.assignSelectedBeads()
		}
		// Stay in assign mode if no beads selected
		return m, nil
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
		// Assignment only works in zoomed mode
		if m.zoomLevel == ZoomOverview {
			// Should not happen - reset to normal view
			m.viewMode = ViewNormal
			// Fall through to normal rendering
		}
		// In zoomed mode, details panel will show the assign beads form inline
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

// All functions have been extracted to separate files for better organization:
// - tui_work_render.go for rendering functions
// - tui_work_dialogs.go for dialog rendering
// - tui_work_commands.go for command generators
// - tui_work_mouse.go for mouse handling
