package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var (
	flagTUIProject string
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive TUI for managing works and beads (lazygit-style)",
	Long: `A full-featured interactive TUI for managing the Claude Orchestrator.

Features a 3-panel lazygit-style interface:
  - Works Panel: View and manage work units
  - Beads Panel: View ready/all beads from the issue tracker
  - Details Panel: Context-sensitive details for selected items

Key bindings:
  Navigation:
    Tab, 1-3    Switch between panels
    j/k, ↑/↓    Navigate within panel
    Enter       Drill down / expand

  Work Management:
    c           Create new work (opens branch name dialog)
    d           Destroy selected work
    p           Plan work (create tasks from beads)
    r           Run work (execute pending tasks)

  Bead Management:
    a           Assign beads to selected work
    Space       Toggle selection (for multi-select)
    b           Toggle between ready/all beads

  Advanced:
    A           Automated workflow (create + plan + run + review + PR)
    R           Create review task for work
    P           Create PR task for work

  Other:
    ?           Show help
    q           Quit`,
	Args: cobra.NoArgs,
	RunE: runTUI,
}

func init() {
	rootCmd.AddCommand(tuiCmd)
	tuiCmd.Flags().StringVar(&flagTUIProject, "project", "", "project directory (default: auto-detect)")
}

// Panel represents which panel is currently focused
type Panel int

const (
	PanelWorks Panel = iota
	PanelBeads
	PanelDetails
)

// ViewMode represents the current view mode
type ViewMode int

const (
	ViewNormal ViewMode = iota
	ViewCreateWork
	ViewDestroyConfirm
	ViewPlanDialog
	ViewAssignBeads
	ViewHelp
)

// tuiDataMsg is sent when data is refreshed
type tuiDataMsg struct {
	works []*workProgress
	beads []beadItem
	err   error
}

// tuiTickMsg triggers periodic refresh
type tuiTickMsg time.Time

// tuiCommandMsg indicates a command completed
type tuiCommandMsg struct {
	action string
	err    error
}

// beadItem represents a bead in the beads panel
type beadItem struct {
	id       string
	title    string
	status   string
	priority int
	isReady  bool
	selected bool // for multi-select
}

// tuiModel is the main TUI model
type tuiModel struct {
	ctx           context.Context
	proj          *project.Project
	width         int
	height        int

	// Panel state
	activePanel   Panel
	worksCursor   int
	beadsCursor   int
	detailsScroll int

	// Data
	works         []*workProgress
	beadItems     []beadItem
	showAllBeads  bool

	// UI state
	viewMode      ViewMode
	spinner       spinner.Model
	textInput     textinput.Model
	statusMessage string
	statusIsError bool
	lastUpdate    time.Time

	// Selection state (for multi-select)
	selectedBeads map[string]bool

	// Loading state
	loading       bool
	quitting      bool
}

func newTUIModel(ctx context.Context, proj *project.Project) tuiModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	ti := textinput.New()
	ti.Placeholder = "feature/my-branch"
	ti.CharLimit = 100
	ti.Width = 40

	return tuiModel{
		ctx:           ctx,
		proj:          proj,
		width:         80,
		height:        24,
		activePanel:   PanelWorks,
		spinner:       s,
		textInput:     ti,
		selectedBeads: make(map[string]bool),
		loading:       true,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.fetchData(),
		m.tick(),
	)
}

func (m tuiModel) tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tuiTickMsg(t)
	})
}

func (m tuiModel) fetchData() tea.Cmd {
	return func() tea.Msg {
		works, err := fetchPollData(m.ctx, m.proj, "", "")
		if err != nil {
			return tuiDataMsg{err: err}
		}

		// Fetch beads
		var beadItems []beadItem
		if m.showAllBeads {
			beadItems, err = fetchAllBeads(m.proj.MainRepoPath())
		} else {
			beadItems, err = fetchReadyBeads(m.proj.MainRepoPath())
		}
		if err != nil {
			return tuiDataMsg{works: works, err: err}
		}

		return tuiDataMsg{works: works, beads: beadItems}
	}
}

func fetchReadyBeads(dir string) ([]beadItem, error) {
	readyBeads, err := beads.GetReadyBeadsInDir(dir)
	if err != nil {
		return nil, err
	}

	var items []beadItem
	for _, b := range readyBeads {
		items = append(items, beadItem{
			id:      b.ID,
			title:   b.Title,
			status:  "open",
			isReady: true,
		})
	}
	return items, nil
}

func fetchAllBeads(dir string) ([]beadItem, error) {
	// Run bd list --status=open --json
	cmd := exec.Command("bd", "list", "--status=open", "--json")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run bd list: %w", err)
	}

	// Parse JSON output
	type beadJSON struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Status   string `json:"status"`
		Priority int    `json:"priority"`
	}
	var beadsJSON []beadJSON
	if err := json.Unmarshal(output, &beadsJSON); err != nil {
		return nil, fmt.Errorf("failed to parse bd list output: %w", err)
	}

	// Check which beads are ready
	readyBeads, _ := beads.GetReadyBeadsInDir(dir)
	readySet := make(map[string]bool)
	for _, b := range readyBeads {
		readySet[b.ID] = true
	}

	var items []beadItem
	for _, b := range beadsJSON {
		items = append(items, beadItem{
			id:       b.ID,
			title:    b.Title,
			status:   b.Status,
			priority: b.Priority,
			isReady:  readySet[b.ID],
		})
	}
	return items, nil
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle view-specific keys first
		switch m.viewMode {
		case ViewCreateWork:
			return m.updateCreateWork(msg)
		case ViewDestroyConfirm:
			return m.updateDestroyConfirm(msg)
		case ViewPlanDialog:
			return m.updatePlanDialog(msg)
		case ViewAssignBeads:
			return m.updateAssignBeads(msg)
		case ViewHelp:
			return m.updateHelp(msg)
		}

		// Normal view key handling
		return m.updateNormal(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tuiTickMsg:
		return m, tea.Batch(m.fetchData(), m.tick())

	case tuiDataMsg:
		m.loading = false
		m.works = msg.works
		if msg.beads != nil {
			// Preserve selection state
			for i := range msg.beads {
				if m.selectedBeads[msg.beads[i].id] {
					msg.beads[i].selected = true
				}
			}
			m.beadItems = msg.beads
		}
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Error: %v", msg.err)
			m.statusIsError = true
		}
		m.lastUpdate = time.Now()
		return m, nil

	case tuiCommandMsg:
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Error: %v", msg.err)
			m.statusIsError = true
		} else {
			m.statusMessage = fmt.Sprintf("%s completed", msg.action)
			m.statusIsError = false
		}
		return m, m.fetchData()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m tuiModel) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "?":
		m.viewMode = ViewHelp
		return m, nil

	// Panel switching
	case "tab":
		m.activePanel = (m.activePanel + 1) % 3
		return m, nil
	case "shift+tab":
		m.activePanel = (m.activePanel + 2) % 3
		return m, nil
	case "1":
		m.activePanel = PanelWorks
		return m, nil
	case "2":
		m.activePanel = PanelBeads
		return m, nil
	case "3":
		m.activePanel = PanelDetails
		return m, nil

	// Navigation
	case "j", "down":
		return m.navigateDown(), nil
	case "k", "up":
		return m.navigateUp(), nil

	// Toggle beads view
	case "b":
		m.showAllBeads = !m.showAllBeads
		return m, m.fetchData()

	// Work actions
	case "c":
		m.viewMode = ViewCreateWork
		m.textInput.Reset()
		m.textInput.Focus()
		return m, textinput.Blink
	case "d":
		if m.activePanel == PanelWorks && len(m.works) > 0 {
			m.viewMode = ViewDestroyConfirm
		}
		return m, nil
	case "p":
		if m.activePanel == PanelWorks && len(m.works) > 0 {
			m.viewMode = ViewPlanDialog
		}
		return m, nil
	case "r":
		return m.runSelectedWork()

	// Bead actions
	case "a":
		if m.activePanel == PanelWorks && len(m.works) > 0 {
			m.viewMode = ViewAssignBeads
			// Clear previous selections
			m.selectedBeads = make(map[string]bool)
			for i := range m.beadItems {
				m.beadItems[i].selected = false
			}
		}
		return m, nil
	case " ":
		if m.activePanel == PanelBeads && len(m.beadItems) > 0 {
			bead := &m.beadItems[m.beadsCursor]
			bead.selected = !bead.selected
			if bead.selected {
				m.selectedBeads[bead.id] = true
			} else {
				delete(m.selectedBeads, bead.id)
			}
		}
		return m, nil

	// Advanced actions
	case "A":
		return m.runAutomatedWorkflow()
	case "R":
		return m.createReviewTask()
	case "P":
		return m.createPRTask()
	}

	return m, nil
}

func (m tuiModel) navigateDown() tuiModel {
	switch m.activePanel {
	case PanelWorks:
		if m.worksCursor < len(m.works)-1 {
			m.worksCursor++
		}
	case PanelBeads:
		if m.beadsCursor < len(m.beadItems)-1 {
			m.beadsCursor++
		}
	case PanelDetails:
		m.detailsScroll++
	}
	return m
}

func (m tuiModel) navigateUp() tuiModel {
	switch m.activePanel {
	case PanelWorks:
		if m.worksCursor > 0 {
			m.worksCursor--
		}
	case PanelBeads:
		if m.beadsCursor > 0 {
			m.beadsCursor--
		}
	case PanelDetails:
		if m.detailsScroll > 0 {
			m.detailsScroll--
		}
	}
	return m
}

func (m tuiModel) updateCreateWork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		return m, nil
	case "enter":
		branchName := strings.TrimSpace(m.textInput.Value())
		if branchName != "" {
			m.viewMode = ViewNormal
			return m, m.createWork(branchName)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m tuiModel) updateDestroyConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if len(m.works) > 0 {
			workID := m.works[m.worksCursor].work.ID
			m.viewMode = ViewNormal
			return m, m.destroyWork(workID)
		}
	case "n", "N", "esc":
		m.viewMode = ViewNormal
	}
	return m, nil
}

func (m tuiModel) updatePlanDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "a", "A":
		// Auto-group
		if len(m.works) > 0 {
			workID := m.works[m.worksCursor].work.ID
			m.viewMode = ViewNormal
			return m, m.planWork(workID, true)
		}
	case "s", "S":
		// Single-bead tasks
		if len(m.works) > 0 {
			workID := m.works[m.worksCursor].work.ID
			m.viewMode = ViewNormal
			return m, m.planWork(workID, false)
		}
	case "esc":
		m.viewMode = ViewNormal
	}
	return m, nil
}

func (m tuiModel) updateAssignBeads(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		return m, nil
	case "enter":
		m.viewMode = ViewNormal
		// Collect selected beads
		var selectedIDs []string
		for id, selected := range m.selectedBeads {
			if selected {
				selectedIDs = append(selectedIDs, id)
			}
		}
		if len(selectedIDs) > 0 && len(m.works) > 0 {
			workID := m.works[m.worksCursor].work.ID
			return m, m.assignBeadsToWork(workID, selectedIDs)
		}
		return m, nil
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
	case " ":
		if len(m.beadItems) > 0 {
			bead := &m.beadItems[m.beadsCursor]
			bead.selected = !bead.selected
			if bead.selected {
				m.selectedBeads[bead.id] = true
			} else {
				delete(m.selectedBeads, bead.id)
			}
		}
		return m, nil
	}
	return m, nil
}

func (m tuiModel) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key dismisses help
	m.viewMode = ViewNormal
	return m, nil
}

// Command functions
func (m tuiModel) createWork(branchName string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("co", "work", "create", branchName)
		cmd.Dir = m.proj.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Create work", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "Create work"}
	}
}

func (m tuiModel) destroyWork(workID string) tea.Cmd {
	return func() tea.Msg {
		// Use exec to avoid stdin issues
		cmd := exec.Command("co", "work", "destroy", workID)
		cmd.Dir = m.proj.Root
		// Auto-confirm by providing "y" to stdin
		cmd.Stdin = strings.NewReader("y\n")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Destroy work", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "Destroy work"}
	}
}

func (m tuiModel) planWork(workID string, autoGroup bool) tea.Cmd {
	return func() tea.Msg {
		args := []string{"plan", "--work", workID}
		if autoGroup {
			args = append(args, "--auto-group")
		}
		cmd := exec.Command("co", args...)
		cmd.Dir = m.proj.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Plan work", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "Plan work"}
	}
}

func (m tuiModel) assignBeadsToWork(workID string, beadIDs []string) tea.Cmd {
	return func() tea.Msg {
		// Plan with specific beads
		args := []string{"plan", "--work", workID}
		args = append(args, strings.Join(beadIDs, ","))
		cmd := exec.Command("co", args...)
		cmd.Dir = m.proj.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Assign beads", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "Assign beads"}
	}
}

func (m tuiModel) runSelectedWork() (tea.Model, tea.Cmd) {
	if m.activePanel != PanelWorks || len(m.works) == 0 {
		return m, nil
	}
	workID := m.works[m.worksCursor].work.ID

	return m, func() tea.Msg {
		cmd := exec.Command("co", "run", workID)
		cmd.Dir = m.proj.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Run work", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "Run work"}
	}
}

func (m tuiModel) runAutomatedWorkflow() (tea.Model, tea.Cmd) {
	// Get selected beads
	var selectedIDs []string
	for id, selected := range m.selectedBeads {
		if selected {
			selectedIDs = append(selectedIDs, id)
		}
	}
	if len(selectedIDs) == 0 {
		m.statusMessage = "No beads selected for automated workflow"
		m.statusIsError = true
		return m, nil
	}

	return m, func() tea.Msg {
		cmd := exec.Command("co", "work", "create", "--bead="+strings.Join(selectedIDs, ","))
		cmd.Dir = m.proj.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Automated workflow", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "Automated workflow started"}
	}
}

func (m tuiModel) createReviewTask() (tea.Model, tea.Cmd) {
	if m.activePanel != PanelWorks || len(m.works) == 0 {
		return m, nil
	}
	workID := m.works[m.worksCursor].work.ID

	return m, func() tea.Msg {
		cmd := exec.Command("co", "work", "review", workID)
		cmd.Dir = m.proj.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Create review", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "Review task created"}
	}
}

func (m tuiModel) createPRTask() (tea.Model, tea.Cmd) {
	if m.activePanel != PanelWorks || len(m.works) == 0 {
		return m, nil
	}
	workID := m.works[m.worksCursor].work.ID

	return m, func() tea.Msg {
		cmd := exec.Command("co", "work", "pr", workID)
		cmd.Dir = m.proj.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Create PR", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "PR task created"}
	}
}

// View rendering
func (m tuiModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Render based on view mode
	switch m.viewMode {
	case ViewHelp:
		return m.renderHelp()
	case ViewCreateWork:
		return m.renderWithDialog(m.renderCreateWorkDialog())
	case ViewDestroyConfirm:
		return m.renderWithDialog(m.renderDestroyConfirmDialog())
	case ViewPlanDialog:
		return m.renderWithDialog(m.renderPlanDialog())
	case ViewAssignBeads:
		return m.renderAssignBeadsView()
	}

	// Normal view
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderPanels())
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())

	return b.String()
}

func (m tuiModel) renderHeader() string {
	title := tuiTitleStyle.Render("co - Claude Orchestrator")
	help := tuiDimStyle.Render("[?] Help")

	padding := m.width - lipgloss.Width(title) - lipgloss.Width(help)
	if padding < 0 {
		padding = 0
	}

	return title + strings.Repeat(" ", padding) + help
}

func (m tuiModel) renderPanels() string {
	// Calculate panel widths
	totalWidth := m.width - 4 // Account for borders
	panelWidth1 := totalWidth / 4
	panelWidth2 := totalWidth / 4
	panelWidth3 := totalWidth - panelWidth1 - panelWidth2

	// Calculate panel height
	panelHeight := m.height - 6 // Header, footer, borders

	// Render each panel
	worksPanel := m.renderWorksPanel(panelWidth1, panelHeight)
	beadsPanel := m.renderBeadsPanel(panelWidth2, panelHeight)
	detailsPanel := m.renderDetailsPanel(panelWidth3, panelHeight)

	// Join panels horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, worksPanel, beadsPanel, detailsPanel)
}

func (m tuiModel) renderWorksPanel(width, height int) string {
	title := "[1] Works"
	if m.activePanel == PanelWorks {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n")

	if m.loading {
		content.WriteString(m.spinner.View())
		content.WriteString(" Loading...")
	} else if len(m.works) == 0 {
		content.WriteString(tuiDimStyle.Render("No works"))
	} else {
		for i, wp := range m.works {
			icon := m.statusIcon(wp.work.Status)
			line := fmt.Sprintf("%s %s", icon, wp.work.ID)

			if i == m.worksCursor && m.activePanel == PanelWorks {
				line = tuiSelectedStyle.Render("> " + line)
			} else {
				line = "  " + line
			}

			// Truncate if needed
			if len(line) > width-2 {
				line = line[:width-5] + "..."
			}
			content.WriteString(line)
			content.WriteString("\n")
		}
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == PanelWorks {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

func (m tuiModel) renderBeadsPanel(width, height int) string {
	title := "[2] Beads"
	if m.showAllBeads {
		title += " (all)"
	} else {
		title += " (ready)"
	}
	if m.activePanel == PanelBeads {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n")

	if m.loading {
		content.WriteString(m.spinner.View())
		content.WriteString(" Loading...")
	} else if len(m.beadItems) == 0 {
		content.WriteString(tuiDimStyle.Render("No beads"))
	} else {
		for i, bead := range m.beadItems {
			var icon string
			if bead.selected {
				icon = tuiSelectedCheckStyle.Render("●")
			} else if bead.isReady {
				icon = statusCompleted.Render("○")
			} else {
				icon = statusPending.Render("◌")
			}

			line := fmt.Sprintf("%s %s", icon, bead.id)

			if i == m.beadsCursor && m.activePanel == PanelBeads {
				line = tuiSelectedStyle.Render("> " + line)
			} else {
				line = "  " + line
			}

			// Truncate if needed
			if len(line) > width-2 {
				line = line[:width-5] + "..."
			}
			content.WriteString(line)
			content.WriteString("\n")
		}
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == PanelBeads {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

func (m tuiModel) renderDetailsPanel(width, height int) string {
	title := "[3] Details"
	if m.activePanel == PanelDetails {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n\n")

	// Show details based on active panel selection
	switch m.activePanel {
	case PanelWorks:
		if len(m.works) > 0 && m.worksCursor < len(m.works) {
			wp := m.works[m.worksCursor]
			content.WriteString(m.renderWorkDetails(wp, width-4))
		} else {
			content.WriteString(tuiDimStyle.Render("Select a work to view details"))
		}
	case PanelBeads:
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			bead := m.beadItems[m.beadsCursor]
			content.WriteString(m.renderBeadDetails(bead, width-4))
		} else {
			content.WriteString(tuiDimStyle.Render("Select a bead to view details"))
		}
	default:
		content.WriteString(tuiDimStyle.Render("Navigate to Works or Beads panel"))
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == PanelDetails {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

func (m tuiModel) renderWorkDetails(wp *workProgress, width int) string {
	var b strings.Builder

	b.WriteString(tuiLabelStyle.Render("Work: "))
	b.WriteString(tuiValueStyle.Render(wp.work.ID))
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Branch: "))
	b.WriteString(tuiValueStyle.Render(wp.work.BranchName))
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Status: "))
	b.WriteString(m.statusStyled(wp.work.Status))
	b.WriteString("\n")

	if wp.work.PRURL != "" {
		b.WriteString(tuiLabelStyle.Render("PR: "))
		b.WriteString(tuiValueStyle.Render(wp.work.PRURL))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Tasks
	completed := 0
	for _, tp := range wp.tasks {
		if tp.task.Status == db.StatusCompleted {
			completed++
		}
	}

	b.WriteString(tuiLabelStyle.Render(fmt.Sprintf("Tasks (%d/%d)", completed, len(wp.tasks))))
	b.WriteString("\n")

	for _, tp := range wp.tasks {
		icon := m.statusIcon(tp.task.Status)
		taskType := tp.task.TaskType
		if taskType == "" {
			taskType = "implement"
		}
		b.WriteString(fmt.Sprintf("  %s %s [%s]\n", icon, tp.task.ID, tuiDimStyle.Render(taskType)))
	}

	return b.String()
}

func (m tuiModel) renderBeadDetails(bead beadItem, width int) string {
	var b strings.Builder

	b.WriteString(tuiLabelStyle.Render("Bead: "))
	b.WriteString(tuiValueStyle.Render(bead.id))
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Title: "))
	b.WriteString(tuiValueStyle.Render(bead.title))
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Status: "))
	b.WriteString(tuiValueStyle.Render(bead.status))
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Ready: "))
	if bead.isReady {
		b.WriteString(statusCompleted.Render("Yes"))
	} else {
		b.WriteString(statusPending.Render("No (blocked)"))
	}
	b.WriteString("\n")

	return b.String()
}

func (m tuiModel) renderStatusBar() string {
	// Action hints
	var actions []string
	switch m.activePanel {
	case PanelWorks:
		actions = []string{"[c]reate", "[d]estroy", "[p]lan", "[r]un", "[a]ssign", "[R]eview", "[P]R"}
	case PanelBeads:
		actions = []string{"[Space] select", "[b] toggle view", "[A]uto workflow"}
	}

	actionStr := strings.Join(actions, " ")

	// Status message
	var statusStr string
	if m.statusMessage != "" {
		if m.statusIsError {
			statusStr = tuiErrorStyle.Render(m.statusMessage)
		} else {
			statusStr = tuiSuccessStyle.Render(m.statusMessage)
		}
	} else {
		statusStr = tuiDimStyle.Render(fmt.Sprintf("Updated: %s", m.lastUpdate.Format("15:04:05")))
	}

	// Combine
	availableWidth := m.width - lipgloss.Width(statusStr) - 2
	if len(actionStr) > availableWidth {
		actionStr = actionStr[:availableWidth-3] + "..."
	}

	return tuiStatusBarStyle.Width(m.width).Render(actionStr + strings.Repeat(" ", max(0, availableWidth-len(actionStr))) + statusStr)
}

func (m tuiModel) renderHelp() string {
	help := `
  Claude Orchestrator - Help

  Navigation
  ────────────────────────────
  Tab, 1-3      Switch panels
  j/k, ↑/↓      Navigate list
  b             Toggle bead view (ready/all)

  Work Management
  ────────────────────────────
  c             Create new work
  d             Destroy selected work
  p             Plan work (create tasks)
  r             Run work

  Bead Management
  ────────────────────────────
  a             Assign beads to work
  Space         Toggle bead selection
  A             Automated workflow

  Advanced
  ────────────────────────────
  R             Create review task
  P             Create PR task

  General
  ────────────────────────────
  ?             Show this help
  q             Quit

  Press any key to close...
`

	return tuiHelpStyle.Width(m.width).Height(m.height).Render(help)
}

func (m tuiModel) renderWithDialog(dialog string) string {
	// Center the dialog on screen
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m tuiModel) renderCreateWorkDialog() string {
	content := fmt.Sprintf(`
  Create New Work

  Enter branch name:
  %s

  [Enter] Create  [Esc] Cancel
`, m.textInput.View())

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderDestroyConfirmDialog() string {
	workID := ""
	if len(m.works) > 0 {
		workID = m.works[m.worksCursor].work.ID
	}

	content := fmt.Sprintf(`
  Destroy Work

  Are you sure you want to destroy work %s?
  This will remove the worktree and all task data.

  [y] Yes  [n] No
`, workID)

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderPlanDialog() string {
	content := `
  Plan Work

  Choose planning mode:

  [a] Auto-group - LLM estimates complexity
  [s] Single-bead - One task per bead

  [Esc] Cancel
`

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderAssignBeadsView() string {
	var b strings.Builder

	b.WriteString(tuiTitleStyle.Render("Assign Beads to Work"))
	b.WriteString("\n\n")

	if len(m.works) > 0 {
		b.WriteString(tuiLabelStyle.Render("Target Work: "))
		b.WriteString(tuiValueStyle.Render(m.works[m.worksCursor].work.ID))
		b.WriteString("\n\n")
	}

	b.WriteString("Select beads (Space to toggle, Enter to confirm, Esc to cancel):\n\n")

	for i, bead := range m.beadItems {
		var checkbox string
		if bead.selected {
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

	// Count selected
	selected := 0
	for _, s := range m.selectedBeads {
		if s {
			selected++
		}
	}
	b.WriteString(fmt.Sprintf("\n%d bead(s) selected", selected))

	return tuiAssignStyle.Width(m.width).Height(m.height).Render(b.String())
}

// Helper functions
func (m tuiModel) statusIcon(status string) string {
	switch status {
	case db.StatusPending:
		return statusPending.Render("○")
	case db.StatusProcessing:
		return statusProcessing.Render("●")
	case db.StatusCompleted:
		return statusCompleted.Render("✓")
	case db.StatusFailed:
		return statusFailed.Render("✗")
	default:
		return "?"
	}
}

func (m tuiModel) statusStyled(status string) string {
	switch status {
	case db.StatusPending:
		return statusPending.Render(status)
	case db.StatusProcessing:
		return statusProcessing.Render(status)
	case db.StatusCompleted:
		return statusCompleted.Render(status)
	case db.StatusFailed:
		return statusFailed.Render(status)
	default:
		return status
	}
}

// TUI-specific styles
var (
	tuiTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	tuiActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99"))

	tuiInactiveTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("247"))

	tuiPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	tuiSelectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	tuiSelectedCheckStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	tuiLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("247"))

	tuiValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	tuiDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	tuiErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	tuiSuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	tuiStatusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	tuiDialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(1, 2).
			Background(lipgloss.Color("235"))

	tuiHelpStyle = lipgloss.NewStyle().
			Padding(2, 4).
			Background(lipgloss.Color("235"))

	tuiAssignStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Background(lipgloss.Color("235"))
)

func runTUI(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	proj, err := project.Find(ctx, flagTUIProject)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	model := newTUIModel(ctx, proj)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	return nil
}
