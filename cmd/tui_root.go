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
	spinner    spinner.Model
	lastUpdate time.Time
	quitting   bool
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
		activeMode:   ModeWork, // Start in Work mode (existing behavior)
		planModel:    planModel,
		workModel:    workModel,
		monitorModel: monitorModel,
		legacyModel:  legacy,
		spinner:      s,
		lastUpdate:   time.Now(),
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

	case tea.KeyMsg:
		// Global mode switching keys
		switch msg.String() {
		case "P":
			if m.activeMode != ModePlan {
				oldMode := m.activeMode
				m.activeMode = ModePlan
				cmd := m.notifyFocusChange(oldMode, ModePlan)
				return m, cmd
			}
		case "W":
			if m.activeMode != ModeWork {
				oldMode := m.activeMode
				m.activeMode = ModeWork
				cmd := m.notifyFocusChange(oldMode, ModeWork)
				return m, cmd
			}
		case "M":
			if m.activeMode != ModeMonitor {
				oldMode := m.activeMode
				m.activeMode = ModeMonitor
				cmd := m.notifyFocusChange(oldMode, ModeMonitor)
				return m, cmd
			}
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
	// Simple plain text mode indicator
	return fmt.Sprintf("=== %s MODE === [P]lan [W]ork [M]onitor", m.activeMode.Label())
}

// runRootTUI starts the TUI with the new root model
func runRootTUI(ctx context.Context, proj *project.Project) error {
	model := newRootModel(ctx, proj)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}
