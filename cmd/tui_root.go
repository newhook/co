package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/project"
)


// Mode represents the active TUI mode
type Mode int

const (
	ModePlan Mode = iota // Planning beads and work
	ModeWork             // Managing works and tasks
)

// modeLabel returns the display label for a mode
func (m Mode) Label() string {
	switch m {
	case ModePlan:
		return "Plan"
	case ModeWork:
		return "Work"
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

	// Sub-models for each mode
	planModel SubModel
	workModel SubModel

	// For backwards compatibility, keep the existing model
	legacyModel tuiModel

	// Global state
	spinner    spinner.Model
	lastUpdate time.Time
	quitting   bool

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

	return rootModel{
		ctx:         ctx,
		proj:        proj,
		width:       80,
		height:      24,
		activeMode:  ModePlan, // Start in Plan mode
		planModel:   planModel,
		workModel:   workModel,
		legacyModel: legacy,
		spinner:     s,
		lastUpdate:  time.Now(),
		hoveredMode: -1, // No mode hovered initially
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

	return tea.Batch(cmds...)
}

// Update implements tea.Model
func (m rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate available height after tab bar (1 line)
		availableHeight := m.height - 1

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
				return m, nil
			} else {
				m.hoveredMode = -1
			}
			// Fall through to route adjusted motion events to sub-model
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

		// Adjust mouse Y coordinate for sub-model (tab bar is at row 0, sub-model starts at row 1)
		adjustedMsg := msg
		adjustedMsg.Y = msg.Y - 1

		// Route adjusted mouse events to active model
		return m.routeToActiveModel(adjustedMsg)

	case tea.KeyMsg:
		// Check if active model is in modal state - if so, route directly to it
		activeModel := m.getActiveModel()
		if activeModel != nil && activeModel.InModal() {
			return m.routeToActiveModel(msg)
		}

		// Global keys (only when not in modal)
		switch msg.String() {
		case "P":
			if m.activeMode != ModePlan {
				oldMode := m.activeMode
				m.activeMode = ModePlan
				cmd := m.notifyFocusChange(oldMode, ModePlan)
				return m, cmd
			}
			return m, nil
		case "W":
			if m.activeMode != ModeWork {
				oldMode := m.activeMode
				m.activeMode = ModeWork
				cmd := m.notifyFocusChange(oldMode, ModeWork)
				return m, cmd
			}
			return m, nil
		case "q":
			m.quitting = true
			// Clean up resources in both models
			if planModel, ok := m.planModel.(*planModel); ok {
				planModel.cleanup()
			}
			if workModel, ok := m.workModel.(*workModel); ok {
				workModel.cleanup()
			}
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
	default:
		return nil
	}
}

// detectHoveredMode determines which mode tab is hovered based on mouse X position
func (m *rootModel) detectHoveredMode(x int) Mode {
	// Tab bar format: "=== Claude Örchestratör: PLAN MODE === [P]lan [W]ork"
	// The hotkeys portion shows which keys to press
	// We need to detect hover over the hotkey portions

	// Build PLAIN text version (no styling) for position detection
	// Must match exactly what renderTabBar produces (minus ANSI codes)
	modeName := m.activeMode.Label()
	tabBarPlain := fmt.Sprintf("=== Claude Örchestratör: %s MODE === [P]lan [W]ork", modeName)

	// Find positions of [P], [W] in the plain tab bar
	planIdx := strings.Index(tabBarPlain, "[P]lan")
	workIdx := strings.Index(tabBarPlain, "[W]ork")

	// Check if mouse is over any of these hotkeys
	if planIdx >= 0 && x >= planIdx && x < planIdx+len("[P]lan") {
		return ModePlan
	}
	if workIdx >= 0 && x >= workIdx && x < workIdx+len("[W]ork") {
		return ModeWork
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

	// Style "Claude Örchestratör" in orange
	orangeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	appName := orangeStyle.Render("Claude Örchestratör")

	// Style the mode switching keys with hover effects
	planKey := styleButtonWithHover("[P]lan", m.hoveredMode == ModePlan)
	workKey := styleButtonWithHover("[W]ork", m.hoveredMode == ModeWork)
	modeKeys := planKey + " " + workKey

	return fmt.Sprintf("=== %s: %s MODE === %s", appName, modeName, modeKeys)
}

// runRootTUI starts the TUI with the new root model
func runRootTUI(ctx context.Context, proj *project.Project, enableMouse bool) error {
	model := newRootModel(ctx, proj)

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if enableMouse {
		opts = append(opts, tea.WithMouseAllMotion())
	}
	p := tea.NewProgram(model, opts...)

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}
