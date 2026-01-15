package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/project"
)

var rootDebugLog *log.Logger

func init() {
	f, _ := os.OpenFile("/tmp/root-key-debug.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if f != nil {
		rootDebugLog = log.New(f, "", log.LstdFlags)
	}
}

// Mode represents the active TUI mode
type Mode int

const (
	ModePlan    Mode = iota // Planning beads and work
	ModeWork                // Managing works and tasks
	ModeMonitor             // Monitoring active works (grid view)
)

// modeLabel returns the display label for a mode
func (m Mode) Label() string {
	switch m {
	case ModePlan:
		return "Plan"
	case ModeWork:
		return "Work"
	case ModeMonitor:
		return "Monitor"
	default:
		return "Unknown"
	}
}

// ModeKey returns the key to switch to this mode
func (m Mode) Key() string {
	switch m {
	case ModePlan:
		return "P"
	case ModeWork:
		return "W"
	case ModeMonitor:
		return "M"
	default:
		return "?"
	}
}

// SubModel interface for mode-specific models
type SubModel interface {
	tea.Model
	// SetSize updates the available dimensions for the sub-model
	SetSize(width, height int)
	// FocusChanged is called when this mode gains/loses focus, returns cmd to run
	FocusChanged(focused bool) tea.Cmd
	// InModal returns true if the model is in a modal/dialog state where global keys shouldn't be intercepted
	InModal() bool
}

// rootModel is the top-level TUI model that manages mode switching
type rootModel struct {
	ctx    context.Context
	proj   *project.Project
	width  int
	height int

	// Current mode
	activeMode Mode

	// Sub-models for each mode (will be populated as we implement them)
	// For now, we use the existing tuiModel as a placeholder
	planModel    SubModel
	workModel    SubModel
	monitorModel SubModel

	// For backwards compatibility, keep the existing model
	legacyModel tuiModel

	// Global state
	spinner           spinner.Model
	lastUpdate        time.Time
	quitting          bool
	pendingModeSwitch bool // true when 'c' was pressed, waiting for p/m/w

	// Mouse hover state
	hoveredMode Mode // which mode tab is being hovered over (0-2), -1 if none
	mouseX      int
	mouseY      int
}

// Tab bar styles
var (
	modeHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("214")).
			Padding(0, 1)

	tabBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("240"))

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("62")).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("247")).
				Background(lipgloss.Color("235")).
				Padding(0, 2)
)

// newRootModel creates a new root TUI model
func newRootModel(ctx context.Context, proj *project.Project) rootModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	// Create the legacy model for backwards compatibility
	legacy := newTUIModel(ctx, proj)

	// Create dedicated mode models
	planModel := newPlanModel(ctx, proj)
	workModel := newWorkModel(ctx, proj)
	monitorModel := newMonitorModel(ctx, proj)

	return rootModel{
		ctx:          ctx,
		proj:         proj,
		width:        80,
		height:       24,
		activeMode:   ModePlan, // Start in Plan mode
		planModel:    planModel,
		workModel:    workModel,
		monitorModel: monitorModel,
		legacyModel:  legacy,
		spinner:      s,
		lastUpdate:   time.Now(),
		hoveredMode:  -1, // No mode hovered initially
	}
}

// Init implements tea.Model
func (m rootModel) Init() tea.Cmd {
	// Initialize all mode models
	cmds := []tea.Cmd{
		m.legacyModel.Init(),
	}

	// Initialize mode models
	if m.planModel != nil {
		cmds = append(cmds, m.planModel.Init())
	}
	if m.workModel != nil {
		cmds = append(cmds, m.workModel.Init())
	}
	if m.monitorModel != nil {
		cmds = append(cmds, m.monitorModel.Init())
	}

	return tea.Batch(cmds...)
}

// Update implements tea.Model
func (m rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Log ALL messages to debug Ctrl key issues
	if rootDebugLog != nil {
		rootDebugLog.Printf("Update msg type=%T value=%v", msg, msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate available height after tab bar (1 line + 1 border)
		availableHeight := m.height - 2

		// Update legacy model dimensions
		m.legacyModel.width = m.width
		m.legacyModel.height = availableHeight

		// Update sub-models
		if m.planModel != nil {
			m.planModel.SetSize(m.width, availableHeight)
		}
		if m.workModel != nil {
			m.workModel.SetSize(m.width, availableHeight)
		}
		if m.monitorModel != nil {
			m.monitorModel.SetSize(m.width, availableHeight)
		}

		return m, nil

	case tea.MouseMsg:
		m.mouseX = msg.X
		m.mouseY = msg.Y

		// Only handle hover detection for motion events
		if msg.Action == tea.MouseActionMotion {
			// Detect hover over mode tabs in tab bar (row 0)
			if msg.Y == 0 {
				// Parse tab bar to find which mode is hovered
				// Tab bar format: "=== MODE MODE === hotkeys"
				// We need to check the position against the rendered tab positions
				m.hoveredMode = m.detectHoveredMode(msg.X)
			} else {
				m.hoveredMode = -1
			}
			return m, nil
		}

		// Handle clicks on mode tabs
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.Y == 0 {
				clickedMode := m.detectHoveredMode(msg.X)
				if clickedMode >= 0 && clickedMode != m.activeMode {
					oldMode := m.activeMode
					m.activeMode = clickedMode
					cmd := m.notifyFocusChange(oldMode, m.activeMode)
					return m, cmd
				}
				return m, nil
			}
		}

		// Route mouse events to active model
		return m.routeToActiveModel(msg)

	case tea.KeyMsg:
		if rootDebugLog != nil {
			activeModel := m.getActiveModel()
			inModal := activeModel != nil && activeModel.InModal()
			rootDebugLog.Printf("KeyMsg: key=%q type=%d inModal=%v", msg.String(), msg.Type, inModal)
		}

		// Check if active model is in modal state - if so, route directly to it
		activeModel := m.getActiveModel()
		if activeModel != nil && activeModel.InModal() {
			return m.routeToActiveModel(msg)
		}

		// Handle pending mode switch (after 'c' was pressed)
		if m.pendingModeSwitch {
			m.pendingModeSwitch = false
			switch msg.String() {
			case "p", "P":
				if m.activeMode != ModePlan {
					oldMode := m.activeMode
					m.activeMode = ModePlan
					cmd := m.notifyFocusChange(oldMode, ModePlan)
					return m, cmd
				}
				return m, nil
			case "w", "W":
				if m.activeMode != ModeWork {
					oldMode := m.activeMode
					m.activeMode = ModeWork
					cmd := m.notifyFocusChange(oldMode, ModeWork)
					return m, cmd
				}
				return m, nil
			case "m", "M":
				if m.activeMode != ModeMonitor {
					oldMode := m.activeMode
					m.activeMode = ModeMonitor
					cmd := m.notifyFocusChange(oldMode, ModeMonitor)
					return m, cmd
				}
				return m, nil
			default:
				// Any other key cancels mode switch, route to submodel
				return m.routeToActiveModel(msg)
			}
		}

		// Global keys (only when not in modal)
		switch msg.String() {
		case "c":
			// Start mode switch sequence
			m.pendingModeSwitch = true
			return m, nil
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

		// Route to active mode model
		return m.routeToActiveModel(msg)

	default:
		// Route other messages to active model and all models (for timers etc)
		return m.routeToActiveModel(msg)
	}
}

// notifyFocusChange notifies models of focus changes and returns commands to run
func (m *rootModel) notifyFocusChange(oldMode, newMode Mode) tea.Cmd {
	var cmds []tea.Cmd

	// Notify old model it lost focus
	switch oldMode {
	case ModePlan:
		if m.planModel != nil {
			if cmd := m.planModel.FocusChanged(false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case ModeWork:
		if m.workModel != nil {
			if cmd := m.workModel.FocusChanged(false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case ModeMonitor:
		if m.monitorModel != nil {
			if cmd := m.monitorModel.FocusChanged(false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	// Notify new model it gained focus
	switch newMode {
	case ModePlan:
		if m.planModel != nil {
			if cmd := m.planModel.FocusChanged(true); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case ModeWork:
		if m.workModel != nil {
			if cmd := m.workModel.FocusChanged(true); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case ModeMonitor:
		if m.monitorModel != nil {
			if cmd := m.monitorModel.FocusChanged(true); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return tea.Batch(cmds...)
}

// getActiveModel returns the currently active sub-model
func (m *rootModel) getActiveModel() SubModel {
	switch m.activeMode {
	case ModePlan:
		return m.planModel
	case ModeWork:
		return m.workModel
	case ModeMonitor:
		return m.monitorModel
	default:
		return nil
	}
}

// detectHoveredMode determines which mode tab is hovered based on mouse X position
func (m *rootModel) detectHoveredMode(x int) Mode {
	// Tab bar format: "=== PLAN MODE === c-[P]lan c-[W]ork c-[M]onitor"
	// The hotkeys portion shows which keys to press
	// We need to detect hover over the hotkey portions

	// Find the hotkeys in the rendered tab bar
	// Format is "c-[P]lan c-[W]ork c-[M]onitor"
	tabBar := m.renderTabBar()

	// Find positions of c-[P], c-[W], c-[M] in the tab bar
	planIdx := strings.Index(tabBar, "c-[P]lan")
	workIdx := strings.Index(tabBar, "c-[W]ork")
	monitorIdx := strings.Index(tabBar, "c-[M]onitor")

	// Check if mouse is over any of these hotkeys (give 8 char width for each)
	if planIdx >= 0 && x >= planIdx && x < planIdx+8 {
		return ModePlan
	}
	if workIdx >= 0 && x >= workIdx && x < workIdx+8 {
		return ModeWork
	}
	if monitorIdx >= 0 && x >= monitorIdx && x < monitorIdx+11 {
		return ModeMonitor
	}

	return -1 // No mode hovered
}

// routeToActiveModel routes a message to the currently active mode model
func (m rootModel) routeToActiveModel(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var newModel tea.Model

	switch m.activeMode {
	case ModePlan:
		if m.planModel != nil {
			newModel, cmd = m.planModel.Update(msg)
			m.planModel = newModel.(SubModel)
		}
	case ModeWork:
		if m.workModel != nil {
			newModel, cmd = m.workModel.Update(msg)
			m.workModel = newModel.(SubModel)
		}
	case ModeMonitor:
		if m.monitorModel != nil {
			newModel, cmd = m.monitorModel.Update(msg)
			m.monitorModel = newModel.(SubModel)
		}
	}

	return m, cmd
}

// View implements tea.Model
func (m rootModel) View() string {
	if m.quitting {
		return ""
	}

	// Render tab bar
	tabBar := m.renderTabBar()
	tabBarHeight := lipgloss.Height(tabBar)

	// Render active mode content
	var content string
	switch m.activeMode {
	case ModePlan:
		if m.planModel != nil {
			content = m.planModel.View()
		}
	case ModeWork:
		if m.workModel != nil {
			content = m.workModel.View()
		}
	case ModeMonitor:
		if m.monitorModel != nil {
			content = m.monitorModel.View()
		}
	}

	contentHeight := lipgloss.Height(content)
	availableHeight := m.height - tabBarHeight

	// Truncate content if too tall
	if contentHeight > availableHeight {
		lines := strings.Split(content, "\n")
		if len(lines) > availableHeight {
			lines = lines[:availableHeight]
		}
		content = strings.Join(lines, "\n")
	}

	// Combine tab bar and content
	return lipgloss.JoinVertical(lipgloss.Top, tabBar, content)
}

// renderTabBar renders the mode switching tab bar
func (m rootModel) renderTabBar() string {
	// Style the current mode name
	modeName := tuiTitleStyle.Render(m.activeMode.Label())

	// Style the mode switching keys with hover effects
	planKey := m.styleHotkeyWithHover("c-[P]lan", ModePlan)
	workKey := m.styleHotkeyWithHover("c-[W]ork", ModeWork)
	monitorKey := m.styleHotkeyWithHover("c-[M]onitor", ModeMonitor)
	modeKeys := planKey + " " + workKey + " " + monitorKey

	if m.pendingModeSwitch {
		return fmt.Sprintf("=== %s MODE === %s  (waiting for p/w/m...)", modeName, modeKeys)
	}
	return fmt.Sprintf("=== %s MODE === %s", modeName, modeKeys)
}

// styleHotkeyWithHover styles a hotkey with hover effect if mouse is over it
func (m rootModel) styleHotkeyWithHover(text string, mode Mode) string {
	// Create a hover style for clickable elements
	hoverStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("214")).
		Bold(true)

	if m.hoveredMode == mode {
		// Apply hover style to entire text
		return hoverStyle.Render(text)
	}
	// Apply normal hotkey styling
	return styleHotkeys(text)
}

// runRootTUI starts the TUI with the new root model
func runRootTUI(ctx context.Context, proj *project.Project) error {
	model := newRootModel(ctx, proj)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion())

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}
