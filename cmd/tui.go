package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
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

Features a lazygit-style drill-down interface with 3 depth levels:
  Depth 0: [Beads] | [Works] | [Work Details]
  Depth 1: [Works] | [Tasks] | [Task Details]
  Depth 2: [Tasks] | [Beads] | [Bead Details]

Key bindings:
  Navigation:
    h, ←        Move left / drill out from leftmost panel
    l, →        Move right / drill in from middle panel
    j/k, ↑/↓    Navigate list (syncs child panels when in left panel)
    Tab, 1-3    Jump to panel at current depth

  Bead Management (at Beads panel, depth 0):
    n           Create new bead
    Space       Toggle selection (for multi-select)
    b           Toggle between ready/all beads
    A           Automated workflow (create + plan + run + review + PR)

  Work Management (at Works panel):
    c           Create new work (opens branch name dialog)
    d           Destroy selected work
    p           Plan work (create tasks from beads)
    r           Run work (execute pending tasks)
    a           Assign beads to selected work
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

// Panel represents which panel position is currently focused (relative to current depth)
type Panel int

const (
	PanelLeft   Panel = iota // Left panel at current depth
	PanelMiddle              // Middle panel at current depth
	PanelRight               // Right panel (details) at current depth
)

// ViewMode represents the current view mode
type ViewMode int

const (
	ViewNormal ViewMode = iota
	ViewCreateWork
	ViewCreateBead
	ViewCreateEpic
	ViewDestroyConfirm
	ViewCloseBeadConfirm
	ViewPlanDialog
	ViewAssignBeads
	ViewBeadSearch
	ViewLabelFilter
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
	id          string
	title       string
	status      string
	priority    int
	beadType    string // task, bug, feature
	description string
	isReady     bool
	selected    bool // for multi-select
}

// beadFilters holds the current filter state for beads
type beadFilters struct {
	status     string // "open", "closed", "ready"
	label      string // filter by label (empty = no filter)
	searchText string // fuzzy search text
	sortBy     string // "default", "priority", "created", "title"
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

	// Drill-down state
	depth           int // 0, 1, or 2
	tasksCursor     int // cursor for tasks panel at depth 1
	taskBeadsCursor int // cursor for beads panel at depth 2
	selectedWorkIdx int // which work we drilled into
	selectedTaskIdx int // which task we drilled into

	// Data
	works         []*workProgress
	beadItems     []beadItem
	filters       beadFilters
	beadsExpanded bool // expanded view shows type/priority/title

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
	loading  bool
	quitting bool
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
		ctx:                ctx,
		proj:               proj,
		width:              80,
		height:             24,
		activePanel:        PanelLeft,
		depth:              0,
		spinner:            s,
		textInput:          ti,
		selectedBeads:      make(map[string]bool),
		createBeadPriority: 2, // default priority
		filters: beadFilters{
			status: "ready",
			sortBy: "default",
		},
		loading: true,
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

		// Fetch beads with filters
		beadItems, err := fetchBeadsWithFilters(m.proj.MainRepoPath(), m.filters)
		if err != nil {
			return tuiDataMsg{works: works, err: err}
		}

		return tuiDataMsg{works: works, beads: beadItems}
	}
}

func fetchBeadsWithFilters(dir string, filters beadFilters) ([]beadItem, error) {
	// For "ready" status, use bd ready command
	if filters.status == "ready" {
		return fetchReadyBeads(dir, filters)
	}

	// Build bd list command based on filters
	args := []string{"list", "--json"}
	if filters.status == "open" || filters.status == "closed" {
		args = append(args, "--status="+filters.status)
	}
	if filters.label != "" {
		args = append(args, "--label="+filters.label)
	}

	cmd := exec.Command("bd", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run bd list: %w", err)
	}

	// Parse JSON output
	type beadJSON struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		Priority    int    `json:"priority"`
		Type        string `json:"type"`
		Description string `json:"description"`
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
		// Apply search filter
		if filters.searchText != "" {
			searchLower := strings.ToLower(filters.searchText)
			if !strings.Contains(strings.ToLower(b.ID), searchLower) &&
				!strings.Contains(strings.ToLower(b.Title), searchLower) &&
				!strings.Contains(strings.ToLower(b.Description), searchLower) {
				continue
			}
		}

		items = append(items, beadItem{
			id:          b.ID,
			title:       b.Title,
			status:      b.Status,
			priority:    b.Priority,
			beadType:    b.Type,
			description: b.Description,
			isReady:     readySet[b.ID],
		})
	}

	// Apply sorting
	items = sortBeadItems(items, filters.sortBy)

	return items, nil
}

func fetchReadyBeads(dir string, filters beadFilters) ([]beadItem, error) {
	readyBeads, err := beads.GetReadyBeadsInDir(dir)
	if err != nil {
		return nil, err
	}

	var items []beadItem
	for _, b := range readyBeads {
		// Apply search filter
		if filters.searchText != "" {
			searchLower := strings.ToLower(filters.searchText)
			if !strings.Contains(strings.ToLower(b.ID), searchLower) &&
				!strings.Contains(strings.ToLower(b.Title), searchLower) &&
				!strings.Contains(strings.ToLower(b.Description), searchLower) {
				continue
			}
		}

		items = append(items, beadItem{
			id:          b.ID,
			title:       b.Title,
			description: b.Description,
			status:      "open",
			isReady:     true,
		})
	}

	// Apply sorting
	items = sortBeadItems(items, filters.sortBy)

	return items, nil
}

func sortBeadItems(items []beadItem, sortBy string) []beadItem {
	switch sortBy {
	case "priority":
		sort.Slice(items, func(i, j int) bool {
			return items[i].priority < items[j].priority
		})
	case "title":
		sort.Slice(items, func(i, j int) bool {
			return items[i].title < items[j].title
		})
	case "triage":
		// Triage sort: priority first, then by type (bug > task > feature)
		sort.Slice(items, func(i, j int) bool {
			if items[i].priority != items[j].priority {
				return items[i].priority < items[j].priority
			}
			typeOrder := map[string]int{"bug": 0, "task": 1, "feature": 2}
			return typeOrder[items[i].beadType] < typeOrder[items[j].beadType]
		})
	}
	return items
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle view-specific keys first
		switch m.viewMode {
		case ViewCreateWork:
			return m.updateCreateWork(msg)
		case ViewCreateBead:
			return m.updateCreateBead(msg)
		case ViewCreateEpic:
			return m.updateCreateEpic(msg)
		case ViewDestroyConfirm:
			return m.updateDestroyConfirm(msg)
		case ViewCloseBeadConfirm:
			return m.updateCloseBeadConfirm(msg)
		case ViewPlanDialog:
			return m.updatePlanDialog(msg)
		case ViewAssignBeads:
			return m.updateAssignBeads(msg)
		case ViewBeadSearch:
			return m.updateBeadSearch(msg)
		case ViewLabelFilter:
			return m.updateLabelFilter(msg)
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
		m.activePanel = PanelLeft
		return m, nil
	case "2":
		m.activePanel = PanelMiddle
		return m, nil
	case "3":
		m.activePanel = PanelRight
		return m, nil

	// Horizontal navigation (panel switching + drill in/out)
	case "l", "right":
		return m.navigateRight(), nil
	case "h", "left":
		return m.navigateLeft(), nil

	// Vertical navigation
	case "j", "down":
		return m.navigateDown(), nil
	case "k", "up":
		return m.navigateUp(), nil

	// Beads filter keys (only at depth 0 when beads panel active)
	case "o": // Show open issues
		if m.isBeadsPanelActive() {
			m.filters.status = "open"
			m.beadsCursor = 0
			return m, m.fetchData()
		}
		return m, nil
	case "/": // Fuzzy search
		if m.isBeadsPanelActive() {
			m.viewMode = ViewBeadSearch
			m.textInput.Reset()
			m.textInput.Placeholder = "Search beads..."
			m.textInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	case "L": // Filter by label (shift-L)
		if m.isBeadsPanelActive() {
			m.viewMode = ViewLabelFilter
			m.textInput.Reset()
			m.textInput.Placeholder = "Enter label (empty to clear)..."
			m.textInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	case "s": // Cycle sort
		if m.isBeadsPanelActive() {
			// Cycle: default → priority → title → default
			switch m.filters.sortBy {
			case "default":
				m.filters.sortBy = "priority"
			case "priority":
				m.filters.sortBy = "title"
			default:
				m.filters.sortBy = "default"
			}
			return m, m.fetchData()
		}
		return m, nil
	case "S": // Triage sort
		if m.isBeadsPanelActive() {
			m.filters.sortBy = "triage"
			return m, m.fetchData()
		}
		return m, nil
	case "v": // Toggle expanded view
		if m.isBeadsPanelActive() {
			m.beadsExpanded = !m.beadsExpanded
		}
		return m, nil

	// Work actions (available when Works panel is active)
	case "c":
		// "c" on beads panel shows closed issues
		if m.isBeadsPanelActive() {
			m.filters.status = "closed"
			m.beadsCursor = 0
			return m, m.fetchData()
		}
		// "c" on works panel creates work
		if m.isWorksPanelActive() {
			m.viewMode = ViewCreateWork
			m.textInput.Reset()
			m.textInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	case "d":
		if m.isWorksPanelActive() && len(m.works) > 0 {
			m.viewMode = ViewDestroyConfirm
		}
		return m, nil
	case "p":
		if m.isWorksPanelActive() && len(m.works) > 0 {
			m.viewMode = ViewPlanDialog
		}
		return m, nil
	case "r":
		// "r" on beads panel shows ready issues
		if m.isBeadsPanelActive() {
			m.filters.status = "ready"
			m.beadsCursor = 0
			return m, m.fetchData()
		}
		// "r" on works panel runs work
		if m.isWorksPanelActive() {
			return m.runSelectedWork()
		}
		return m, nil

	// Bead actions (available when Beads panel is active at depth 0)
	case "n":
		if m.isBeadsPanelActive() {
			m.viewMode = ViewCreateBead
			m.textInput.Reset()
			m.textInput.Placeholder = "Issue title..."
			m.textInput.Focus()
			m.createBeadType = 0     // default to task
			m.createBeadPriority = 2 // default priority
			return m, textinput.Blink
		}
		return m, nil
	case "a":
		if m.isWorksPanelActive() && len(m.works) > 0 {
			m.viewMode = ViewAssignBeads
			// Clear previous selections
			m.selectedBeads = make(map[string]bool)
			for i := range m.beadItems {
				m.beadItems[i].selected = false
			}
		}
		return m, nil
	case " ":
		if m.isBeadsPanelActive() && len(m.beadItems) > 0 {
			bead := &m.beadItems[m.beadsCursor]
			bead.selected = !bead.selected
			if bead.selected {
				m.selectedBeads[bead.id] = true
			} else {
				delete(m.selectedBeads, bead.id)
			}
		}
		return m, nil

	// Close bead
	case "x":
		if m.isBeadsPanelActive() && len(m.beadItems) > 0 {
			bead := m.beadItems[m.beadsCursor]
			if bead.status == "open" {
				m.viewMode = ViewCloseBeadConfirm
			}
		}
		return m, nil

	// Reopen bead
	case "X":
		if m.isBeadsPanelActive() && len(m.beadItems) > 0 {
			bead := m.beadItems[m.beadsCursor]
			if bead.status == "closed" {
				return m, m.reopenBead(bead.id)
			}
		}
		return m, nil

	// Create epic
	case "e":
		if m.isBeadsPanelActive() {
			m.viewMode = ViewCreateEpic
			m.textInput.Reset()
			m.textInput.Placeholder = "Epic title..."
			m.textInput.Focus()
			m.createBeadPriority = 2 // default priority
			return m, textinput.Blink
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

// isBeadsPanelActive returns true if the beads panel is currently focused
// Beads panel is at PanelLeft when depth is 0
func (m tuiModel) isBeadsPanelActive() bool {
	return m.depth == 0 && m.activePanel == PanelLeft
}

// isWorksPanelActive returns true if the works panel is currently focused
// Works panel is at PanelMiddle when depth is 0, or PanelLeft when depth is 1
func (m tuiModel) isWorksPanelActive() bool {
	return (m.depth == 0 && m.activePanel == PanelMiddle) ||
		(m.depth == 1 && m.activePanel == PanelLeft)
}

// isTasksPanelActive returns true if the tasks panel is currently focused
// Tasks panel is at PanelMiddle when depth is 1, or PanelLeft when depth is 2
func (m tuiModel) isTasksPanelActive() bool {
	return (m.depth == 1 && m.activePanel == PanelMiddle) ||
		(m.depth == 2 && m.activePanel == PanelLeft)
}

// isTaskBeadsPanelActive returns true if the task beads panel is currently focused
// Task beads panel is at PanelMiddle when depth is 2
func (m tuiModel) isTaskBeadsPanelActive() bool {
	return m.depth == 2 && m.activePanel == PanelMiddle
}

// navigateRight moves right: panel switching or drill-in
func (m tuiModel) navigateRight() tuiModel {
	switch m.activePanel {
	case PanelLeft:
		// Move from left to middle panel
		m.activePanel = PanelMiddle
	case PanelMiddle:
		// Try to drill in, otherwise move to right panel
		switch m.depth {
		case 0:
			if len(m.works) > 0 && m.worksCursor < len(m.works) {
				m.depth = 1
				m.selectedWorkIdx = m.worksCursor
				m.tasksCursor = 0
			} else {
				m.activePanel = PanelRight
			}
		case 1:
			if m.worksCursor < len(m.works) {
				wp := m.works[m.worksCursor]
				if len(wp.tasks) > 0 && m.tasksCursor < len(wp.tasks) {
					m.depth = 2
					m.selectedTaskIdx = m.tasksCursor
					m.taskBeadsCursor = 0
				} else {
					m.activePanel = PanelRight
				}
			} else {
				m.activePanel = PanelRight
			}
		default:
			m.activePanel = PanelRight
		}
	case PanelRight:
		// Already at rightmost, do nothing
	}
	return m
}

// navigateLeft moves left: panel switching or drill-out
func (m tuiModel) navigateLeft() tuiModel {
	switch m.activePanel {
	case PanelRight:
		// Move from right to middle panel
		m.activePanel = PanelMiddle
	case PanelMiddle:
		// Move from middle to left panel
		m.activePanel = PanelLeft
	case PanelLeft:
		// At leftmost, drill out if possible
		if m.depth > 0 {
			m.depth--
			m.activePanel = PanelMiddle
		}
	}
	return m
}

func (m tuiModel) navigateDown() tuiModel {
	switch m.activePanel {
	case PanelLeft:
		// Left panel content depends on depth
		switch m.depth {
		case 0: // Beads panel
			if m.beadsCursor < len(m.beadItems)-1 {
				m.beadsCursor++
			}
		case 1: // Works panel (sync to update tasks panel)
			if m.worksCursor < len(m.works)-1 {
				m.worksCursor++
				m.selectedWorkIdx = m.worksCursor
				m.tasksCursor = 0 // Reset tasks cursor when work changes
			}
		case 2: // Tasks panel (sync to update beads panel)
			if m.selectedWorkIdx < len(m.works) {
				wp := m.works[m.selectedWorkIdx]
				if m.tasksCursor < len(wp.tasks)-1 {
					m.tasksCursor++
					m.selectedTaskIdx = m.tasksCursor
					m.taskBeadsCursor = 0 // Reset beads cursor when task changes
				}
			}
		}
	case PanelMiddle:
		// Middle panel content depends on depth
		switch m.depth {
		case 0: // Works panel
			if m.worksCursor < len(m.works)-1 {
				m.worksCursor++
			}
		case 1: // Tasks panel
			if m.selectedWorkIdx < len(m.works) {
				wp := m.works[m.selectedWorkIdx]
				if m.tasksCursor < len(wp.tasks)-1 {
					m.tasksCursor++
				}
			}
		case 2: // Task beads panel
			if m.selectedWorkIdx < len(m.works) && m.selectedTaskIdx < len(m.works[m.selectedWorkIdx].tasks) {
				tp := m.works[m.selectedWorkIdx].tasks[m.selectedTaskIdx]
				if m.taskBeadsCursor < len(tp.beads)-1 {
					m.taskBeadsCursor++
				}
			}
		}
	case PanelRight:
		m.detailsScroll++
	}
	return m
}

func (m tuiModel) navigateUp() tuiModel {
	switch m.activePanel {
	case PanelLeft:
		// Left panel content depends on depth
		switch m.depth {
		case 0: // Beads panel
			if m.beadsCursor > 0 {
				m.beadsCursor--
			}
		case 1: // Works panel (sync to update tasks panel)
			if m.worksCursor > 0 {
				m.worksCursor--
				m.selectedWorkIdx = m.worksCursor
				m.tasksCursor = 0 // Reset tasks cursor when work changes
			}
		case 2: // Tasks panel (sync to update beads panel)
			if m.tasksCursor > 0 {
				m.tasksCursor--
				m.selectedTaskIdx = m.tasksCursor
				m.taskBeadsCursor = 0 // Reset beads cursor when task changes
			}
		}
	case PanelMiddle:
		// Middle panel content depends on depth
		switch m.depth {
		case 0: // Works panel
			if m.worksCursor > 0 {
				m.worksCursor--
			}
		case 1: // Tasks panel
			if m.tasksCursor > 0 {
				m.tasksCursor--
			}
		case 2: // Task beads panel
			if m.taskBeadsCursor > 0 {
				m.taskBeadsCursor--
			}
		}
	case PanelRight:
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
	case "b":
		// Use selected beads to auto-generate branch name
		var selectedIDs []string
		for id, selected := range m.selectedBeads {
			if selected {
				selectedIDs = append(selectedIDs, id)
			}
		}
		if len(selectedIDs) > 0 {
			m.viewMode = ViewNormal
			return m, m.createWorkWithBeads(selectedIDs)
		}
		m.statusMessage = "No beads selected"
		m.statusIsError = true
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

func (m tuiModel) updateHelp(_ tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key dismisses help
	m.viewMode = ViewNormal
	return m, nil
}

var beadTypes = []string{"task", "bug", "feature"}

func (m tuiModel) updateCreateBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			m.viewMode = ViewNormal
			return m, m.createBead(title, beadTypes[m.createBeadType], m.createBeadPriority)
		}
		return m, nil
	case "tab":
		// Cycle through bead types
		m.createBeadType = (m.createBeadType + 1) % len(beadTypes)
		return m, nil
	case "shift+tab":
		// Cycle backward through bead types
		m.createBeadType = (m.createBeadType + len(beadTypes) - 1) % len(beadTypes)
		return m, nil
	case "+", "=":
		// Increase priority (lower number = higher priority)
		if m.createBeadPriority > 0 {
			m.createBeadPriority--
		}
		return m, nil
	case "-", "_":
		// Decrease priority (higher number = lower priority)
		if m.createBeadPriority < 4 {
			m.createBeadPriority++
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m tuiModel) updateCreateEpic(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			m.viewMode = ViewNormal
			// Epics are always features
			return m, m.createBead(title, "feature", m.createBeadPriority)
		}
		return m, nil
	case "+", "=":
		if m.createBeadPriority > 0 {
			m.createBeadPriority--
		}
		return m, nil
	case "-", "_":
		if m.createBeadPriority < 4 {
			m.createBeadPriority++
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m tuiModel) updateCloseBeadConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			beadID := m.beadItems[m.beadsCursor].id
			m.viewMode = ViewNormal
			return m, m.closeBead(beadID)
		}
	case "n", "N", "esc":
		m.viewMode = ViewNormal
	}
	return m, nil
}

func (m tuiModel) updateBeadSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		m.filters.searchText = "" // Clear search on cancel
		return m, m.fetchData()
	case "enter":
		m.viewMode = ViewNormal
		m.filters.searchText = strings.TrimSpace(m.textInput.Value())
		m.beadsCursor = 0
		return m, m.fetchData()
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m tuiModel) updateLabelFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.viewMode = ViewNormal
		return m, nil // Keep existing label on cancel
	case "enter":
		m.viewMode = ViewNormal
		m.filters.label = strings.TrimSpace(m.textInput.Value())
		m.beadsCursor = 0
		return m, m.fetchData()
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
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

func (m tuiModel) createWorkWithBeads(beadIDs []string) tea.Cmd {
	return func() tea.Msg {
		// Use --bead flag to auto-generate branch name (without --auto for full automation)
		cmd := exec.Command("co", "work", "create", "--bead="+strings.Join(beadIDs, ","))
		cmd.Dir = m.proj.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Create work", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: "Create work (from beads)"}
	}
}

func (m tuiModel) createBead(title, beadType string, priority int) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("bd", "create",
			"--title", title,
			"--type", beadType,
			"--priority", fmt.Sprintf("%d", priority),
		)
		cmd.Dir = m.proj.MainRepoPath()
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Create bead", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: fmt.Sprintf("Created %s", beadType)}
	}
}

func (m tuiModel) closeBead(beadID string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("bd", "close", beadID)
		cmd.Dir = m.proj.MainRepoPath()
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Close bead", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: fmt.Sprintf("Closed %s", beadID)}
	}
}

func (m tuiModel) reopenBead(beadID string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("bd", "reopen", beadID)
		cmd.Dir = m.proj.MainRepoPath()
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiCommandMsg{action: "Reopen bead", err: fmt.Errorf("%w: %s", err, output)}
		}
		return tuiCommandMsg{action: fmt.Sprintf("Reopened %s", beadID)}
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
	if !m.isWorksPanelActive() || len(m.works) == 0 {
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
	if !m.isWorksPanelActive() || len(m.works) == 0 {
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
	if !m.isWorksPanelActive() || len(m.works) == 0 {
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
	case ViewCreateBead:
		return m.renderWithDialog(m.renderCreateBeadDialog())
	case ViewCreateEpic:
		return m.renderWithDialog(m.renderCreateEpicDialog())
	case ViewDestroyConfirm:
		return m.renderWithDialog(m.renderDestroyConfirmDialog())
	case ViewCloseBeadConfirm:
		return m.renderWithDialog(m.renderCloseBeadConfirmDialog())
	case ViewPlanDialog:
		return m.renderWithDialog(m.renderPlanDialog())
	case ViewAssignBeads:
		return m.renderAssignBeadsView()
	case ViewBeadSearch:
		return m.renderWithDialog(m.renderBeadSearchDialog())
	case ViewLabelFilter:
		return m.renderWithDialog(m.renderLabelFilterDialog())
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

	var leftPanel, middlePanel, rightPanel string

	// Render panels based on depth
	switch m.depth {
	case 0:
		// Depth 0: Beads | Works | Details (context-aware)
		leftPanel = m.renderBeadsPanelAt(panelWidth1, panelHeight, PanelLeft)
		middlePanel = m.renderWorksPanelAt(panelWidth2, panelHeight, PanelMiddle)
		// Show bead details when beads panel is focused, otherwise work details
		if m.activePanel == PanelLeft {
			rightPanel = m.renderBeadItemDetailsPanel(panelWidth3, panelHeight)
		} else {
			rightPanel = m.renderWorkDetailsPanel(panelWidth3, panelHeight)
		}
	case 1:
		// Depth 1: Works | Tasks | Task Details
		leftPanel = m.renderWorksPanelAt(panelWidth1, panelHeight, PanelLeft)
		middlePanel = m.renderTasksPanel(panelWidth2, panelHeight)
		rightPanel = m.renderTaskDetailsPanel(panelWidth3, panelHeight)
	case 2:
		// Depth 2: Tasks | Task Beads | Bead Details
		leftPanel = m.renderTasksPanelAt(panelWidth1, panelHeight, PanelLeft)
		middlePanel = m.renderTaskBeadsPanel(panelWidth2, panelHeight)
		rightPanel = m.renderTaskBeadDetailsPanel(panelWidth3, panelHeight)
	}

	// Join panels horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, middlePanel, rightPanel)
}

// renderWorksPanelAt renders the works panel at a given position
func (m tuiModel) renderWorksPanelAt(width, height int, position Panel) string {
	panelNum := int(position) + 1
	title := fmt.Sprintf("[%d] Work", panelNum)
	if m.activePanel == position {
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
		// Calculate visible window
		visibleLines := height - 3
		if visibleLines < 1 {
			visibleLines = 1
		}

		// Determine scroll offset to keep cursor visible
		startIdx := 0
		if m.worksCursor >= visibleLines {
			startIdx = m.worksCursor - visibleLines + 1
		}
		endIdx := startIdx + visibleLines
		if endIdx > len(m.works) {
			endIdx = len(m.works)
			startIdx = endIdx - visibleLines
			if startIdx < 0 {
				startIdx = 0
			}
		}

		// Show scroll indicator if needed
		if startIdx > 0 {
			content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↑ %d more", startIdx)))
			content.WriteString("\n")
		}

		for i := startIdx; i < endIdx; i++ {
			wp := m.works[i]
			isSelected := i == m.worksCursor && m.activePanel == position

			// Use plain icon when selected (so it inherits selected style colors)
			var icon string
			if isSelected {
				icon = m.statusIconPlain(wp.work.Status)
			} else {
				icon = m.statusIcon(wp.work.Status)
			}
			line := fmt.Sprintf("%s %s", icon, wp.work.ID)

			if isSelected {
				// Pad line to full width so background extends across panel
				fullLine := "> " + line
				visWidth := lipgloss.Width(fullLine)
				if visWidth < width-4 {
					fullLine += strings.Repeat(" ", width-4-visWidth)
				}
				line = tuiSelectedStyle.Render(fullLine)
			} else {
				line = "  " + line
			}
			content.WriteString(line)
			content.WriteString("\n")
		}

		// Show scroll indicator if more items below
		if endIdx < len(m.works) {
			content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.works)-endIdx)))
			content.WriteString("\n")
		}
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == position {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderBeadsPanelAt renders the beads panel at a given position
func (m tuiModel) renderBeadsPanelAt(width, height int, position Panel) string {
	panelNum := int(position) + 1
	title := fmt.Sprintf("[%d] Beads", panelNum)

	// Build filter indicator
	var filterParts []string
	filterParts = append(filterParts, m.filters.status) // ready/open/closed
	if m.filters.label != "" {
		filterParts = append(filterParts, "label:"+m.filters.label)
	}
	if m.filters.searchText != "" {
		filterParts = append(filterParts, "/"+m.filters.searchText)
	}
	if m.filters.sortBy != "default" {
		filterParts = append(filterParts, "sort:"+m.filters.sortBy)
	}
	title += " (" + strings.Join(filterParts, " ") + ")"

	if m.activePanel == position {
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
		// Calculate visible window (accounting for title line and padding)
		visibleLines := height - 3
		if visibleLines < 1 {
			visibleLines = 1
		}

		// Determine scroll offset to keep cursor visible
		startIdx := 0
		if m.beadsCursor >= visibleLines {
			startIdx = m.beadsCursor - visibleLines + 1
		}
		endIdx := startIdx + visibleLines
		if endIdx > len(m.beadItems) {
			endIdx = len(m.beadItems)
			startIdx = endIdx - visibleLines
			if startIdx < 0 {
				startIdx = 0
			}
		}

		// Show scroll indicator if needed
		if startIdx > 0 {
			content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↑ %d more", startIdx)))
			content.WriteString("\n")
			visibleLines-- // Account for indicator
			endIdx = startIdx + visibleLines
			if endIdx > len(m.beadItems) {
				endIdx = len(m.beadItems)
			}
		}

		for i := startIdx; i < endIdx; i++ {
			bead := m.beadItems[i]
			isSelected := i == m.beadsCursor && m.activePanel == position

			// Use plain icons when selected (so they inherit selected style colors)
			var icon string
			if isSelected {
				// Plain icons for selected items
				if bead.selected {
					icon = "●"
				} else if bead.isReady {
					icon = "○"
				} else {
					icon = "◌"
				}
			} else {
				// Styled icons for non-selected items
				if bead.selected {
					icon = tuiSelectedCheckStyle.Render("●")
				} else if bead.isReady {
					icon = statusCompleted.Render("○")
				} else {
					icon = statusPending.Render("◌")
				}
			}

			var line string
			if m.beadsExpanded {
				// Expanded view: icon id [type] Pn - title
				typeStr := bead.beadType
				if typeStr == "" {
					typeStr = "task"
				}
				// Abbreviate type
				if typeStr == "feature" {
					typeStr = "feat"
				}
				line = fmt.Sprintf("%s %s [%s] P%d", icon, bead.id, typeStr, bead.priority)
				// Add title if space allows
				maxTitleLen := width - len(line) - 5
				if maxTitleLen > 10 && bead.title != "" {
					titlePart := bead.title
					if len(titlePart) > maxTitleLen {
						titlePart = titlePart[:maxTitleLen-3] + "..."
					}
					line += " " + titlePart
				}
			} else {
				// Compact view: icon id
				line = fmt.Sprintf("%s %s", icon, bead.id)
			}

			if isSelected {
				// Pad line to full width so background extends across panel
				fullLine := "> " + line
				visWidth := lipgloss.Width(fullLine)
				if visWidth < width-4 {
					fullLine += strings.Repeat(" ", width-4-visWidth)
				}
				line = tuiSelectedStyle.Render(fullLine)
			} else {
				line = "  " + line
			}
			content.WriteString(line)
			content.WriteString("\n")
		}

		// Show scroll indicator if more items below
		if endIdx < len(m.beadItems) {
			content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.beadItems)-endIdx)))
			content.WriteString("\n")
		}
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == position {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderWorkDetailsPanel renders the work details panel (depth 0 right panel)
func (m tuiModel) renderWorkDetailsPanel(width, height int) string {
	title := "[3] Work Details"
	if m.activePanel == PanelRight {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n\n")

	if len(m.works) > 0 && m.worksCursor < len(m.works) {
		wp := m.works[m.worksCursor]
		content.WriteString(m.renderWorkDetails(wp, width-4))
	} else {
		content.WriteString(tuiDimStyle.Render("Select a work to view details"))
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == PanelRight {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderBeadItemDetailsPanel renders the bead details panel (depth 0 right panel when beads focused)
func (m tuiModel) renderBeadItemDetailsPanel(width, height int) string {
	title := "[3] Bead Details"
	if m.activePanel == PanelRight || m.activePanel == PanelLeft {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n\n")

	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		bead := m.beadItems[m.beadsCursor]
		content.WriteString(m.renderBeadItemDetails(bead, width-4))
	} else {
		content.WriteString(tuiDimStyle.Render("Select a bead to view details"))
	}

	style := tuiPanelStyle.Width(width).Height(height)
	// Highlight when beads panel is active (since this shows bead details)
	if m.activePanel == PanelLeft {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderBeadItemDetails renders the details for a beadItem
func (m tuiModel) renderBeadItemDetails(bead beadItem, width int) string {
	var b strings.Builder

	b.WriteString(tuiLabelStyle.Render("Bead: "))
	b.WriteString(tuiValueStyle.Render(bead.id))
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Title: "))
	b.WriteString(tuiValueStyle.Render(bead.title))
	b.WriteString("\n")

	if bead.beadType != "" {
		b.WriteString(tuiLabelStyle.Render("Type: "))
		b.WriteString(tuiValueStyle.Render(bead.beadType))
		b.WriteString("\n")
	}

	b.WriteString(tuiLabelStyle.Render("Status: "))
	if bead.status == "open" {
		b.WriteString(statusProcessing.Render(bead.status))
	} else {
		b.WriteString(statusCompleted.Render(bead.status))
	}
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Priority: "))
	b.WriteString(tuiValueStyle.Render(fmt.Sprintf("P%d", bead.priority)))
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Ready: "))
	if bead.isReady {
		b.WriteString(statusCompleted.Render("Yes"))
	} else {
		b.WriteString(statusPending.Render("No (blocked)"))
	}
	b.WriteString("\n")

	if bead.description != "" {
		b.WriteString("\n")
		b.WriteString(tuiLabelStyle.Render("Description:"))
		b.WriteString("\n")
		// Word-wrap description
		desc := bead.description
		if len(desc) > width*5 {
			desc = desc[:width*5-3] + "..."
		}
		b.WriteString(tuiDimStyle.Render(desc))
		b.WriteString("\n")
	}

	return b.String()
}

// renderTasksPanel renders the tasks panel (depth 1 middle panel)
func (m tuiModel) renderTasksPanel(width, height int) string {
	title := "[2] Tasks"
	if m.activePanel == PanelMiddle {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n")

	// Get tasks for selected work
	if m.selectedWorkIdx >= len(m.works) {
		content.WriteString(tuiDimStyle.Render("No work selected"))
	} else {
		wp := m.works[m.selectedWorkIdx]
		if len(wp.tasks) == 0 {
			content.WriteString(tuiDimStyle.Render("No tasks"))
		} else {
			for i, tp := range wp.tasks {
				isSelected := i == m.tasksCursor && m.activePanel == PanelMiddle
				var icon string
				if isSelected {
					icon = m.statusIconPlain(tp.task.Status)
				} else {
					icon = m.statusIcon(tp.task.Status)
				}
				taskType := tp.task.TaskType
				if taskType == "" {
					taskType = "implement"
				}
				line := fmt.Sprintf("%s %s [%s]", icon, tp.task.ID, taskType)

				if isSelected {
					fullLine := "> " + line
					visWidth := lipgloss.Width(fullLine)
					if visWidth < width-4 {
						fullLine += strings.Repeat(" ", width-4-visWidth)
					}
					line = tuiSelectedStyle.Render(fullLine)
				} else {
					line = "  " + line
				}
				content.WriteString(line)
				content.WriteString("\n")
			}
		}
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == PanelMiddle {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderTasksPanelAt renders the tasks panel at a given position (for depth 2 left panel)
func (m tuiModel) renderTasksPanelAt(width, height int, position Panel) string {
	panelNum := int(position) + 1
	title := fmt.Sprintf("[%d] Tasks", panelNum)
	if m.activePanel == position {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n")

	// Get tasks for selected work
	if m.selectedWorkIdx >= len(m.works) {
		content.WriteString(tuiDimStyle.Render("No work selected"))
	} else {
		wp := m.works[m.selectedWorkIdx]
		if len(wp.tasks) == 0 {
			content.WriteString(tuiDimStyle.Render("No tasks"))
		} else {
			for i, tp := range wp.tasks {
				isSelected := i == m.tasksCursor && m.activePanel == position
				var icon string
				if isSelected {
					icon = m.statusIconPlain(tp.task.Status)
				} else {
					icon = m.statusIcon(tp.task.Status)
				}
				line := fmt.Sprintf("%s %s", icon, tp.task.ID)

				if isSelected {
					fullLine := "> " + line
					visWidth := lipgloss.Width(fullLine)
					if visWidth < width-4 {
						fullLine += strings.Repeat(" ", width-4-visWidth)
					}
					line = tuiSelectedStyle.Render(fullLine)
				} else {
					line = "  " + line
				}
				content.WriteString(line)
				content.WriteString("\n")
			}
		}
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == position {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderTaskDetailsPanel renders task details (depth 1 right panel)
func (m tuiModel) renderTaskDetailsPanel(width, height int) string {
	title := "[3] Task Details"
	if m.activePanel == PanelRight {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n\n")

	// Get selected task
	if m.selectedWorkIdx < len(m.works) {
		wp := m.works[m.selectedWorkIdx]
		if m.tasksCursor < len(wp.tasks) {
			tp := wp.tasks[m.tasksCursor]
			content.WriteString(m.renderTaskDetails(tp, width-4))
		} else {
			content.WriteString(tuiDimStyle.Render("Select a task to view details"))
		}
	} else {
		content.WriteString(tuiDimStyle.Render("No work selected"))
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == PanelRight {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderTaskBeadsPanel renders beads for the selected task (depth 2 middle panel)
func (m tuiModel) renderTaskBeadsPanel(width, height int) string {
	title := "[2] Beads"
	if m.activePanel == PanelMiddle {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n")

	// Get beads for selected task
	if m.selectedWorkIdx < len(m.works) && m.selectedTaskIdx < len(m.works[m.selectedWorkIdx].tasks) {
		tp := m.works[m.selectedWorkIdx].tasks[m.selectedTaskIdx]
		if len(tp.beads) == 0 {
			content.WriteString(tuiDimStyle.Render("No beads assigned"))
		} else {
			for i, bp := range tp.beads {
				isSelected := i == m.taskBeadsCursor && m.activePanel == PanelMiddle
				var icon string
				if isSelected {
					icon = m.statusIconPlain(bp.status)
				} else {
					icon = m.statusIcon(bp.status)
				}
				line := fmt.Sprintf("%s %s", icon, bp.id)

				if isSelected {
					fullLine := "> " + line
					visWidth := lipgloss.Width(fullLine)
					if visWidth < width-4 {
						fullLine += strings.Repeat(" ", width-4-visWidth)
					}
					line = tuiSelectedStyle.Render(fullLine)
				} else {
					line = "  " + line
				}
				content.WriteString(line)
				content.WriteString("\n")
			}
		}
	} else {
		content.WriteString(tuiDimStyle.Render("No task selected"))
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == PanelMiddle {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderTaskBeadDetailsPanel renders details for a bead in a task (depth 2 right panel)
func (m tuiModel) renderTaskBeadDetailsPanel(width, height int) string {
	title := "[3] Bead Details"
	if m.activePanel == PanelRight {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n\n")

	// Get selected bead from task
	if m.selectedWorkIdx < len(m.works) && m.selectedTaskIdx < len(m.works[m.selectedWorkIdx].tasks) {
		tp := m.works[m.selectedWorkIdx].tasks[m.selectedTaskIdx]
		if m.taskBeadsCursor < len(tp.beads) {
			bp := tp.beads[m.taskBeadsCursor]
			content.WriteString(m.renderTaskBeadProgressDetails(&bp, width-4))
		} else {
			content.WriteString(tuiDimStyle.Render("Select a bead to view details"))
		}
	} else {
		content.WriteString(tuiDimStyle.Render("No task selected"))
	}

	style := tuiPanelStyle.Width(width).Height(height)
	if m.activePanel == PanelRight {
		style = style.BorderForeground(lipgloss.Color("99"))
	}
	return style.Render(content.String())
}

// renderTaskDetails renders details for a task
func (m tuiModel) renderTaskDetails(tp *taskProgress, width int) string {
	var b strings.Builder

	b.WriteString(tuiLabelStyle.Render("Task: "))
	b.WriteString(tuiValueStyle.Render(tp.task.ID))
	b.WriteString("\n")

	taskType := tp.task.TaskType
	if taskType == "" {
		taskType = "implement"
	}
	b.WriteString(tuiLabelStyle.Render("Type: "))
	b.WriteString(tuiValueStyle.Render(taskType))
	b.WriteString("\n")

	b.WriteString(tuiLabelStyle.Render("Status: "))
	b.WriteString(m.statusStyled(tp.task.Status))
	b.WriteString("\n")

	// Spawn status
	if tp.task.SpawnStatus != "" && tp.task.SpawnStatus != "idle" {
		b.WriteString(tuiLabelStyle.Render("Spawn: "))
		b.WriteString(tuiValueStyle.Render(tp.task.SpawnStatus))
		b.WriteString("\n")
	}

	// Timestamps
	b.WriteString("\n")
	b.WriteString(tuiLabelStyle.Render("Created: "))
	b.WriteString(tuiDimStyle.Render(tp.task.CreatedAt.Format("2006-01-02 15:04")))
	b.WriteString("\n")

	if tp.task.StartedAt != nil {
		b.WriteString(tuiLabelStyle.Render("Started: "))
		b.WriteString(tuiDimStyle.Render(tp.task.StartedAt.Format("2006-01-02 15:04")))
		b.WriteString("\n")
	}

	if tp.task.CompletedAt != nil {
		b.WriteString(tuiLabelStyle.Render("Completed: "))
		b.WriteString(tuiDimStyle.Render(tp.task.CompletedAt.Format("2006-01-02 15:04")))
		b.WriteString("\n")
		// Show duration
		if tp.task.StartedAt != nil {
			duration := tp.task.CompletedAt.Sub(*tp.task.StartedAt)
			b.WriteString(tuiLabelStyle.Render("Duration: "))
			b.WriteString(tuiDimStyle.Render(formatDuration(duration)))
			b.WriteString("\n")
		}
	}

	// Complexity info
	if tp.task.ComplexityBudget > 0 || tp.task.ActualComplexity > 0 {
		b.WriteString("\n")
		if tp.task.ComplexityBudget > 0 {
			b.WriteString(tuiLabelStyle.Render("Budget: "))
			b.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d tokens", tp.task.ComplexityBudget)))
			b.WriteString("\n")
		}
		if tp.task.ActualComplexity > 0 {
			b.WriteString(tuiLabelStyle.Render("Actual: "))
			b.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d tokens", tp.task.ActualComplexity)))
			b.WriteString("\n")
		}
	}

	// Error message
	if tp.task.ErrorMessage != "" {
		b.WriteString("\n")
		b.WriteString(tuiErrorStyle.Render("Error: "))
		b.WriteString(tuiErrorStyle.Render(tp.task.ErrorMessage))
		b.WriteString("\n")
	}

	// PR URL
	if tp.task.PRURL != "" {
		b.WriteString("\n")
		b.WriteString(tuiLabelStyle.Render("PR: "))
		b.WriteString(tuiValueStyle.Render(tp.task.PRURL))
		b.WriteString("\n")
	}

	// Beads in this task
	b.WriteString("\n")
	b.WriteString(tuiLabelStyle.Render(fmt.Sprintf("Beads (%d)", len(tp.beads))))
	b.WriteString("\n")

	for _, bp := range tp.beads {
		icon := m.statusIcon(bp.status)
		b.WriteString(fmt.Sprintf("  %s %s\n", icon, bp.id))
	}

	return b.String()
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// renderTaskBeadProgressDetails renders details for a beadProgress item
func (m tuiModel) renderTaskBeadProgressDetails(bp *beadProgress, width int) string {
	var b strings.Builder

	b.WriteString(tuiLabelStyle.Render("Bead: "))
	b.WriteString(tuiValueStyle.Render(bp.id))
	b.WriteString("\n")

	if bp.title != "" {
		b.WriteString(tuiLabelStyle.Render("Title: "))
		b.WriteString(tuiValueStyle.Render(bp.title))
		b.WriteString("\n")
	}

	b.WriteString(tuiLabelStyle.Render("Task Status: "))
	b.WriteString(m.statusStyled(bp.status))
	b.WriteString("\n")

	if bp.beadStatus != "" {
		b.WriteString(tuiLabelStyle.Render("Bead Status: "))
		b.WriteString(tuiValueStyle.Render(bp.beadStatus))
		b.WriteString("\n")
	}

	if bp.description != "" {
		b.WriteString("\n")
		b.WriteString(tuiLabelStyle.Render("Description:"))
		b.WriteString("\n")
		// Word-wrap description to panel width
		desc := bp.description
		if len(desc) > width*3 {
			desc = desc[:width*3-3] + "..."
		}
		b.WriteString(tuiDimStyle.Render(desc))
		b.WriteString("\n")
	}

	return b.String()
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

		// Show beads for this task
		for _, bp := range tp.beads {
			beadIcon := m.statusIcon(bp.status)
			b.WriteString(fmt.Sprintf("    %s %s\n", beadIcon, bp.id))
		}
	}

	return b.String()
}

func (m tuiModel) renderStatusBar() string {
	// Action hints based on depth and panel
	var actions []string

	// Navigation hints
	navHints := "[h/l] move  [j/k] select"

	switch m.depth {
	case 0:
		// Depth 0: Beads | Works | Work Details
		if m.isBeadsPanelActive() {
			actions = []string{"[n]ew", "[e]pic", "[x]close", "[v]iew", "[o]pen [c]losed [r]eady", "[/]search", "[L]abel", "[s]ort [S]triage"}
		} else if m.isWorksPanelActive() {
			actions = []string{"[l] drill", "[c]reate", "[d]estroy", "[p]lan", "[r]un", "[a]ssign"}
		} else {
			actions = []string{"[h] back"}
		}
	case 1:
		// Depth 1: Works | Tasks | Task Details
		if m.isWorksPanelActive() {
			actions = []string{"[c]reate", "[d]estroy", "[r]un"}
		} else if m.isTasksPanelActive() {
			actions = []string{"[l] drill to beads", "[h] back"}
		} else {
			actions = []string{"[h] back"}
		}
	case 2:
		// Depth 2: Tasks | Beads | Bead Details
		if m.isTasksPanelActive() {
			actions = []string{"[h] drill out"}
		} else {
			actions = []string{"[h] back"}
		}
	}

	actionStr := navHints + "  " + strings.Join(actions, " ")

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

  Drill-Down Navigation (lazygit-style)
  ────────────────────────────
  Depth 0: [Beads] | [Works] | [Details]
  Depth 1: [Works] | [Tasks] | [Task Details]
  Depth 2: [Tasks] | [Beads] | [Bead Details]

  Navigation
  ────────────────────────────
  h, ←          Move left / drill out from leftmost
  l, →          Move right / drill in from middle
  j/k, ↑/↓      Navigate list (syncs child panels)
  Tab, 1-3      Jump to panel at current depth

  Bead Management (at Beads panel)
  ────────────────────────────
  n             Create new bead
  e             Create new epic (feature)
  x             Close selected bead
  X             Reopen selected bead
  Space         Toggle bead selection
  A             Automated workflow (full automation)

  Bead Filtering
  ────────────────────────────
  o             Show open issues
  c             Show closed issues
  r             Show ready issues
  /             Fuzzy search (ID, title, description)
  L             Filter by label
  s             Cycle sort (default/priority/title)
  S             Triage sort (priority + type)
  v             Toggle expanded view

  Work Management (at Works panel)
  ────────────────────────────
  c             Create new work
  d             Destroy selected work
  p             Plan work (create tasks)
  r             Run work
  a             Assign beads to work
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
	// Count selected beads
	selectedCount := 0
	for _, selected := range m.selectedBeads {
		if selected {
			selectedCount++
		}
	}

	var beadOption string
	if selectedCount > 0 {
		beadOption = fmt.Sprintf("\n  [b] Use %d selected bead(s) to auto-generate branch", selectedCount)
	} else {
		beadOption = "\n  (Select beads in Beads panel to use auto-generate)"
	}

	content := fmt.Sprintf(`
  Create New Work

  Enter branch name:
  %s
%s

  [Enter] Create  [Esc] Cancel
`, m.textInput.View(), beadOption)

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderCreateBeadDialog() string {
	// Build type selector
	var typeOptions []string
	for i, t := range beadTypes {
		if i == m.createBeadType {
			typeOptions = append(typeOptions, fmt.Sprintf("[%s]", t))
		} else {
			typeOptions = append(typeOptions, fmt.Sprintf(" %s ", t))
		}
	}
	typeSelector := strings.Join(typeOptions, " ")

	// Build priority display
	priorityLabels := []string{"P0 (critical)", "P1 (high)", "P2 (medium)", "P3 (low)", "P4 (backlog)"}
	priorityDisplay := priorityLabels[m.createBeadPriority]

	content := fmt.Sprintf(`
  Create New Bead

  Title:
  %s

  Type (Tab to cycle):    %s
  Priority (+/- to adjust): %s

  [Enter] Create  [Esc] Cancel
`, m.textInput.View(), typeSelector, priorityDisplay)

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

func (m tuiModel) renderCreateEpicDialog() string {
	// Build priority display
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

func (m tuiModel) renderCloseBeadConfirmDialog() string {
	beadID := ""
	beadTitle := ""
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID = m.beadItems[m.beadsCursor].id
		beadTitle = m.beadItems[m.beadsCursor].title
	}

	content := fmt.Sprintf(`
  Close Bead

  Are you sure you want to close %s?
  %s

  [y] Yes  [n] No
`, beadID, beadTitle)

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderBeadSearchDialog() string {
	content := fmt.Sprintf(`
  Search Beads

  Enter search text (searches ID, title, description):
  %s

  [Enter] Search  [Esc] Cancel (clears search)
`, m.textInput.View())

	return tuiDialogStyle.Render(content)
}

func (m tuiModel) renderLabelFilterDialog() string {
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

// statusIconPlain returns the icon without styling (for use in selected items)
func (m tuiModel) statusIconPlain(status string) string {
	switch status {
	case db.StatusPending:
		return "○"
	case db.StatusProcessing:
		return "●"
	case db.StatusCompleted:
		return "✓"
	case db.StatusFailed:
		return "✗"
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
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("62"))

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

	// Status indicator styles
	statusPending = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	statusProcessing = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Bold(true)

	statusCompleted = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)

	statusFailed = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)
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
