package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/project"
)

// planModel is the Plan Mode model focused on issue/bead management
type planModel struct {
	ctx    context.Context
	proj   *project.Project
	width  int
	height int

	// Panel state
	activePanel Panel
	beadsCursor int

	// Data
	beadItems     []beadItem
	filters       beadFilters
	beadsExpanded bool

	// UI state
	viewMode      ViewMode
	spinner       spinner.Model
	textInput     textinput.Model
	statusMessage string
	statusIsError bool
	lastUpdate    time.Time

	// Selection state (for multi-select)
	selectedBeads map[string]bool

	// Create bead state
	createBeadType     int // 0=task, 1=bug, 2=feature
	createBeadPriority int // 0-4, default 2

	// Loading state
	loading bool
}

// newPlanModel creates a new Plan Mode model
func newPlanModel(ctx context.Context, proj *project.Project) *planModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	ti := textinput.New()
	ti.Placeholder = "Enter title..."
	ti.CharLimit = 100
	ti.Width = 40

	return &planModel{
		ctx:                ctx,
		proj:               proj,
		width:              80,
		height:             24,
		activePanel:        PanelLeft,
		spinner:            s,
		textInput:          ti,
		selectedBeads:      make(map[string]bool),
		createBeadPriority: 2,
		filters: beadFilters{
			status: "ready",
			sortBy: "default",
		},
	}
}

// SetSize implements SubModel
func (m *planModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// FocusChanged implements SubModel
func (m *planModel) FocusChanged(focused bool) tea.Cmd {
	if focused {
		// Refresh data when gaining focus
		m.loading = true
		return m.refreshData()
	}
	return nil
}

// Init implements tea.Model
func (m *planModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.refreshData(),
	)
}

// Update implements tea.Model
func (m *planModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case planDataMsg:
		m.beadItems = msg.beads
		m.loading = false
		m.lastUpdate = time.Now()
		if msg.err != nil {
			m.statusMessage = msg.err.Error()
			m.statusIsError = true
		}
		return m, nil

	case planTickMsg:
		return m, m.refreshData()

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

// planDataMsg is sent when data is refreshed
type planDataMsg struct {
	beads []beadItem
	err   error
}

// planTickMsg triggers periodic refresh
type planTickMsg time.Time

func (m *planModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle dialog-specific input
	switch m.viewMode {
	case ViewCreateBead:
		return m.updateCreateBead(msg)
	case ViewCreateEpic:
		return m.updateCreateEpic(msg)
	case ViewBeadSearch:
		return m.updateBeadSearch(msg)
	case ViewLabelFilter:
		return m.updateLabelFilter(msg)
	case ViewCloseBeadConfirm:
		return m.updateCloseBeadConfirm(msg)
	case ViewHelp:
		m.viewMode = ViewNormal
		return m, nil
	}

	// Normal mode key handling
	switch msg.String() {
	case "j", "down":
		if m.beadsCursor < len(m.beadItems)-1 {
			m.beadsCursor++
		}
		return m, nil

	case "k", "up":
		if m.beadsCursor > 0 {
			m.beadsCursor--
		}
		return m, nil

	case "n":
		// Create new bead
		m.viewMode = ViewCreateBead
		m.textInput.Reset()
		m.textInput.Focus()
		m.createBeadType = 0
		m.createBeadPriority = 2
		return m, nil

	case "e":
		// Create new epic
		m.viewMode = ViewCreateEpic
		m.textInput.Reset()
		m.textInput.Focus()
		m.createBeadPriority = 2
		return m, nil

	case "x":
		// Close selected bead
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			m.viewMode = ViewCloseBeadConfirm
		}
		return m, nil

	case "/":
		// Search
		m.viewMode = ViewBeadSearch
		m.textInput.Reset()
		m.textInput.SetValue(m.filters.searchText)
		m.textInput.Focus()
		return m, nil

	case "L":
		// Label filter
		m.viewMode = ViewLabelFilter
		m.textInput.Reset()
		m.textInput.SetValue(m.filters.label)
		m.textInput.Focus()
		return m, nil

	case "o":
		m.filters.status = "open"
		return m, m.refreshData()

	case "c":
		m.filters.status = "closed"
		return m, m.refreshData()

	case "r":
		m.filters.status = "ready"
		return m, m.refreshData()

	case "s":
		// Cycle sort mode
		switch m.filters.sortBy {
		case "default":
			m.filters.sortBy = "priority"
		case "priority":
			m.filters.sortBy = "title"
		default:
			m.filters.sortBy = "default"
		}
		return m, m.refreshData()

	case "v":
		m.beadsExpanded = !m.beadsExpanded
		return m, nil

	case " ":
		// Toggle selection
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			id := m.beadItems[m.beadsCursor].id
			m.selectedBeads[id] = !m.selectedBeads[id]
		}
		return m, nil

	case "enter":
		// Launch planning session for selected bead
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			return m, m.launchPlanningSession()
		}
		return m, nil

	case "?":
		m.viewMode = ViewHelp
		return m, nil

	case "q":
		return m, tea.Quit
	}

	return m, nil
}

// View implements tea.Model
func (m *planModel) View() string {
	// Handle dialogs
	switch m.viewMode {
	case ViewCreateBead:
		return m.renderWithDialog(m.renderCreateBeadDialogContent())
	case ViewCreateEpic:
		return m.renderWithDialog(m.renderCreateEpicDialogContent())
	case ViewBeadSearch:
		return m.renderWithDialog(m.renderBeadSearchDialogContent())
	case ViewLabelFilter:
		return m.renderWithDialog(m.renderLabelFilterDialogContent())
	case ViewCloseBeadConfirm:
		return m.renderWithDialog(m.renderCloseBeadConfirmContent())
	case ViewHelp:
		return m.renderHelp()
	}

	// Single pane view
	var b strings.Builder

	// Title
	b.WriteString(tuiTitleStyle.Render("Plan Mode - Issue Management"))
	b.WriteString("\n\n")

	// Filter info
	filterInfo := fmt.Sprintf("Filter: %s | Sort: %s | %d issue(s)",
		m.filters.status, m.filters.sortBy, len(m.beadItems))
	if m.filters.searchText != "" {
		filterInfo += fmt.Sprintf(" | Search: %s", m.filters.searchText)
	}
	if m.filters.label != "" {
		filterInfo += fmt.Sprintf(" | Label: %s", m.filters.label)
	}
	b.WriteString(tuiDimStyle.Render(filterInfo))
	b.WriteString("\n\n")

	// Bead list
	if len(m.beadItems) == 0 {
		b.WriteString(tuiDimStyle.Render("No issues found"))
	} else {
		for i, bead := range m.beadItems {
			line := m.renderBeadLine(i, bead)
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Status bar
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())

	return b.String()
}

func (m *planModel) renderBeadLine(i int, bead beadItem) string {
	icon := statusIcon(bead.status)

	// Selection indicator
	var prefix string
	if m.selectedBeads[bead.id] {
		prefix = tuiSelectedCheckStyle.Render("[●]")
	} else {
		prefix = "[ ]"
	}

	var line string
	if m.beadsExpanded {
		line = fmt.Sprintf("%s %s %s [P%d %s] %s", prefix, icon, bead.id, bead.priority, bead.beadType, bead.title)
	} else {
		line = fmt.Sprintf("%s %s %s %s", prefix, icon, bead.id, bead.title)
	}

	if i == m.beadsCursor {
		return tuiSelectedStyle.Render(line)
	}
	return line
}

func (m *planModel) renderStatusBar() string {
	actions := "[n] New [e] Epic [x] Close [/] Search [L] Label [o/c/r] Filter [s] Sort [v] Expand [Enter] Plan [?] Help"

	var statusStr string
	if m.statusMessage != "" {
		if m.statusIsError {
			statusStr = tuiErrorStyle.Render(m.statusMessage)
		} else {
			statusStr = tuiSuccessStyle.Render(m.statusMessage)
		}
	} else if m.loading {
		statusStr = m.spinner.View() + " Loading..."
	} else {
		statusStr = tuiDimStyle.Render(fmt.Sprintf("Updated: %s", m.lastUpdate.Format("15:04:05")))
	}

	return tuiStatusBarStyle.Width(m.width).Render(actions + "  " + statusStr)
}

func (m *planModel) renderWithDialog(dialog string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *planModel) renderHelp() string {
	help := `
  Plan Mode - Help

  Navigation
  ────────────────────────────
  j/k, ↑/↓      Navigate list
  Enter         Launch planning session for selected issue

  Issue Management
  ────────────────────────────
  n             Create new issue (task)
  e             Create new epic (feature)
  x             Close selected issue
  Space         Toggle selection

  Filtering & Sorting
  ────────────────────────────
  o             Show open issues
  c             Show closed issues
  r             Show ready issues
  /             Fuzzy search
  L             Filter by label
  s             Cycle sort mode
  v             Toggle expanded view

  Press any key to close...
`
	return tuiHelpStyle.Width(m.width).Height(m.height).Render(help)
}

// Dialog update handlers
func (m *planModel) updateCreateBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			return m, m.createBead(title, beadTypes[m.createBeadType], m.createBeadPriority, false)
		}
		return m, nil
	case "tab":
		m.createBeadType = (m.createBeadType + 1) % len(beadTypes)
		return m, nil
	case "+", "=":
		if m.createBeadPriority > 0 {
			m.createBeadPriority--
		}
		return m, nil
	case "-":
		if m.createBeadPriority < 4 {
			m.createBeadPriority++
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m *planModel) updateCreateEpic(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			return m, m.createBead(title, "feature", m.createBeadPriority, true)
		}
		return m, nil
	case "+", "=":
		if m.createBeadPriority > 0 {
			m.createBeadPriority--
		}
		return m, nil
	case "-":
		if m.createBeadPriority < 4 {
			m.createBeadPriority++
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m *planModel) updateBeadSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		m.filters.searchText = ""
		return m, m.refreshData()
	case "enter":
		m.viewMode = ViewNormal
		m.filters.searchText = m.textInput.Value()
		return m, m.refreshData()
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m *planModel) updateLabelFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		return m, nil
	case "enter":
		m.viewMode = ViewNormal
		m.filters.label = m.textInput.Value()
		return m, m.refreshData()
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m *planModel) updateCloseBeadConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			beadID := m.beadItems[m.beadsCursor].id
			m.viewMode = ViewNormal
			return m, m.closeBead(beadID)
		}
		m.viewMode = ViewNormal
		return m, nil
	case "n", "N", "esc":
		m.viewMode = ViewNormal
		return m, nil
	}
	return m, nil
}

// Dialog render helpers
func (m *planModel) renderCreateBeadDialogContent() string {
	var typeOptions []string
	for i, t := range beadTypes {
		if i == m.createBeadType {
			typeOptions = append(typeOptions, fmt.Sprintf("[%s]", t))
		} else {
			typeOptions = append(typeOptions, fmt.Sprintf(" %s ", t))
		}
	}
	typeSelector := strings.Join(typeOptions, " ")

	priorityLabels := []string{"P0 (critical)", "P1 (high)", "P2 (medium)", "P3 (low)", "P4 (backlog)"}
	priorityDisplay := priorityLabels[m.createBeadPriority]

	content := fmt.Sprintf(`
  Create New Issue

  Title:
  %s

  Type (Tab to cycle):    %s
  Priority (+/- to adjust): %s

  [Enter] Create  [Esc] Cancel
`, m.textInput.View(), typeSelector, priorityDisplay)

	return tuiDialogStyle.Render(content)
}

func (m *planModel) renderCreateEpicDialogContent() string {
	priorityLabels := []string{"P0 (critical)", "P1 (high)", "P2 (medium)", "P3 (low)", "P4 (backlog)"}
	priorityDisplay := priorityLabels[m.createBeadPriority]

	content := fmt.Sprintf(`
  Create New Epic

  Title:
  %s

  Type: feature (fixed for epics)
  Priority (+/- to adjust): %s

  [Enter] Create  [Esc] Cancel
`, m.textInput.View(), priorityDisplay)

	return tuiDialogStyle.Render(content)
}

func (m *planModel) renderBeadSearchDialogContent() string {
	content := fmt.Sprintf(`
  Search Issues

  Enter search text (searches ID, title, description):
  %s

  [Enter] Search  [Esc] Cancel (clears search)
`, m.textInput.View())

	return tuiDialogStyle.Render(content)
}

func (m *planModel) renderLabelFilterDialogContent() string {
	currentLabel := m.filters.label
	if currentLabel == "" {
		currentLabel = "(none)"
	}

	content := fmt.Sprintf(`
  Filter by Label

  Current: %s

  Enter label name (empty to clear):
  %s

  [Enter] Apply  [Esc] Cancel
`, currentLabel, m.textInput.View())

	return tuiDialogStyle.Render(content)
}

func (m *planModel) renderCloseBeadConfirmContent() string {
	beadID := ""
	beadTitle := ""
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID = m.beadItems[m.beadsCursor].id
		beadTitle = m.beadItems[m.beadsCursor].title
	}

	content := fmt.Sprintf(`
  Close Issue

  Are you sure you want to close %s?
  %s

  [y] Yes  [n] No
`, beadID, beadTitle)

	return tuiDialogStyle.Render(content)
}

// Command generators
func (m *planModel) refreshData() tea.Cmd {
	return func() tea.Msg {
		items, err := m.loadBeads()
		return planDataMsg{beads: items, err: err}
	}
}

func (m *planModel) loadBeads() ([]beadItem, error) {
	mainRepoPath := m.proj.MainRepoPath()

	// Use the shared fetchBeadsWithFilters function
	items, err := fetchBeadsWithFilters(mainRepoPath, m.filters)
	if err != nil {
		return nil, err
	}

	// Sort based on sortBy
	switch m.filters.sortBy {
	case "priority":
		sort.Slice(items, func(i, j int) bool {
			return items[i].priority < items[j].priority
		})
	case "title":
		sort.Slice(items, func(i, j int) bool {
			return items[i].title < items[j].title
		})
	}

	return items, nil
}

func (m *planModel) createBead(title, beadType string, priority int, isEpic bool) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()

		args := []string{"create", "--title=" + title, "--type=" + beadType, fmt.Sprintf("--priority=%d", priority)}
		if isEpic {
			args = append(args, "--epic")
		}

		cmd := exec.Command("bd", args...)
		cmd.Dir = mainRepoPath
		if err := cmd.Run(); err != nil {
			return planDataMsg{err: fmt.Errorf("failed to create issue: %w", err)}
		}

		// Refresh after creation
		items, err := m.loadBeads()
		return planDataMsg{beads: items, err: err}
	}
}

func (m *planModel) closeBead(beadID string) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()

		cmd := exec.Command("bd", "close", beadID)
		cmd.Dir = mainRepoPath
		if err := cmd.Run(); err != nil {
			return planDataMsg{err: fmt.Errorf("failed to close issue: %w", err)}
		}

		// Refresh after close
		items, err := m.loadBeads()
		return planDataMsg{beads: items, err: err}
	}
}

func (m *planModel) launchPlanningSession() tea.Cmd {
	return func() tea.Msg {
		if len(m.beadItems) == 0 || m.beadsCursor >= len(m.beadItems) {
			return nil
		}

		beadID := m.beadItems[m.beadsCursor].id
		mainRepoPath := m.proj.MainRepoPath()

		// Launch a planning session using zellij run
		args := []string{
			"run", "--name", "plan-" + beadID, "--",
			"claude", "--", "bd show " + beadID,
		}

		cmd := exec.Command("zellij", args...)
		cmd.Dir = mainRepoPath
		_ = cmd.Start()

		return nil
	}
}
