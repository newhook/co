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
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/zellij"
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

	// Per-bead session tracking
	activeBeadSessions map[string]bool // beadID -> has active session
	zj                 *zellij.Client
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
		activeBeadSessions: make(map[string]bool),
		createBeadPriority: 2,
		zj:                 zellij.New(),
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
		return tea.Batch(m.refreshData(), m.startPeriodicRefresh())
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
		if msg.activeSessions != nil {
			m.activeBeadSessions = msg.activeSessions
		}
		m.loading = false
		m.lastUpdate = time.Now()
		if msg.err != nil {
			m.statusMessage = msg.err.Error()
			m.statusIsError = true
		} else {
			m.statusMessage = ""
			m.statusIsError = false
		}
		return m, nil

	case planTickMsg:
		// Refresh data and continue periodic refresh
		return m, tea.Batch(m.refreshData(), m.startPeriodicRefresh())

	case planStatusMsg:
		m.statusMessage = msg.message
		m.statusIsError = msg.isError
		return m, nil

	case planSessionSpawnedMsg:
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Failed: %v", msg.err)
			m.statusIsError = true
		} else if msg.resumed {
			m.statusMessage = fmt.Sprintf("Resumed session for %s", msg.beadID)
			m.statusIsError = false
		} else {
			m.statusMessage = fmt.Sprintf("Started session for %s", msg.beadID)
			m.statusIsError = false
		}
		// Refresh to update session indicators
		return m, m.refreshData()

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	default:
		// Handle Kitty keyboard protocol escape sequences
		// Kitty/Ghostty send escape key as CSI 27 u (bytes: '2' '7' 'u' = 50 55 117)
		typeName := fmt.Sprintf("%T", msg)
		if typeName == "tea.unknownCSISequenceMsg" {
			msgStr := fmt.Sprintf("%s", msg)
			// Check for Kitty protocol escape key: "?CSI[50 55 117]?" = "27u"
			if strings.Contains(msgStr, "50 55 117") {
				return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
			}
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

// planDataMsg is sent when data is refreshed
type planDataMsg struct {
	beads          []beadItem
	activeSessions map[string]bool
	err            error
}

// planStatusMsg is sent to update status text
type planStatusMsg struct {
	message string
	isError bool
}

// planTickMsg triggers periodic refresh
type planTickMsg time.Time

// planSessionSpawnedMsg indicates a planning session was spawned or resumed
type planSessionSpawnedMsg struct {
	beadID  string
	resumed bool
	err     error
}

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
		// Spawn/resume planning session for selected bead
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			beadID := m.beadItems[m.beadsCursor].id
			return m, m.spawnPlanSession(beadID)
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

	// Calculate available lines for list content
	// Total = issuesPanel + detailPanel + statusBar
	// Each panel has: border(2) + title(1) + content
	// Status bar = 1 line
	// So: m.height = (2+1+issuesLines) + (2+1+detailLines) + 1
	//              = issuesLines + detailLines + 7
	issuesContentLines := 10 // Fixed height for issues content
	issuesPanelHeight := issuesContentLines + 3 // +3 for border (2) and title (1)
	detailsPanelHeight := m.height - issuesPanelHeight - 1 // -1 for status bar
	detailsContentLines := max(detailsPanelHeight-3, 2)

	issuesPanel := m.renderFixedPanel("Issues", m.renderIssuesList(issuesContentLines), m.width-4, issuesPanelHeight)
	detailPanel := m.renderFixedPanel("Details", m.renderDetailsContent(detailsContentLines), m.width-4, detailsPanelHeight)
	statusBar := m.renderCommandsBar()

	// Stack panels and status bar
	content := lipgloss.JoinVertical(lipgloss.Left, issuesPanel, detailPanel)

	// Use Place to position content at top and status bar at bottom
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Height(m.height-1).Render(content),
		statusBar,
	)
}

// renderFixedPanel renders a panel with border and fixed height
func (m *planModel) renderFixedPanel(title, content string, width, height int) string {
	titleLine := tuiTitleStyle.Render(title)

	var b strings.Builder
	b.WriteString(titleLine)
	b.WriteString("\n")
	b.WriteString(content)

	// Height-2 for the border lines
	return tuiPanelStyle.Width(width).Height(height - 2).Render(b.String())
}

// renderIssuesList renders just the list content for the given number of visible lines
func (m *planModel) renderIssuesList(visibleLines int) string {
	filterInfo := fmt.Sprintf("Filter: %s | Sort: %s", m.filters.status, m.filters.sortBy)
	if m.filters.searchText != "" {
		filterInfo += fmt.Sprintf(" | Search: %s", m.filters.searchText)
	}
	if m.filters.label != "" {
		filterInfo += fmt.Sprintf(" | Label: %s", m.filters.label)
	}

	var content strings.Builder
	content.WriteString(tuiDimStyle.Render(filterInfo))
	content.WriteString("\n")

	if len(m.beadItems) == 0 {
		content.WriteString(tuiDimStyle.Render("No issues found"))
	} else {
		visibleItems := max(visibleLines-1, 1) // -1 for filter line

		start := 0
		if m.beadsCursor >= visibleItems {
			start = m.beadsCursor - visibleItems + 1
		}
		end := min(start+visibleItems, len(m.beadItems))

		for i := start; i < end; i++ {
			content.WriteString(m.renderBeadLine(i, m.beadItems[i]))
			if i < end-1 {
				content.WriteString("\n")
			}
		}
	}

	return content.String()
}

// renderDetailsContent renders the detail panel content
func (m *planModel) renderDetailsContent(visibleLines int) string {
	var content strings.Builder

	if len(m.beadItems) == 0 || m.beadsCursor >= len(m.beadItems) {
		content.WriteString(tuiDimStyle.Render("No issue selected"))
	} else {
		bead := m.beadItems[m.beadsCursor]

		content.WriteString(tuiLabelStyle.Render("ID: "))
		content.WriteString(tuiValueStyle.Render(bead.id))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("Type: "))
		content.WriteString(tuiValueStyle.Render(bead.beadType))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("P"))
		content.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d", bead.priority)))
		content.WriteString("  ")
		content.WriteString(tuiLabelStyle.Render("Status: "))
		content.WriteString(tuiValueStyle.Render(bead.status))
		if m.activeBeadSessions[bead.id] {
			content.WriteString("  ")
			content.WriteString(tuiSuccessStyle.Render("[Session Active]"))
		}
		content.WriteString("\n")
		content.WriteString(tuiValueStyle.Render(bead.title))

		if bead.description != "" && visibleLines > 3 {
			content.WriteString("\n")
			desc := bead.description
			maxLen := (visibleLines - 3) * 80
			if len(desc) > maxLen && maxLen > 0 {
				desc = desc[:maxLen] + "..."
			}
			content.WriteString(tuiDimStyle.Render(desc))
		}
	}

	return content.String()
}

func (m *planModel) renderCommandsBar() string {
	// Show Enter action based on session state
	enterAction := "[Enter]Plan"
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID := m.beadItems[m.beadsCursor].id
		if m.activeBeadSessions[beadID] {
			enterAction = "[Enter]Resume"
		}
	}

	// Commands on the left
	commands := fmt.Sprintf("[n]New [e]Epic [x]Close %s [o/c/r]Filter [/]Search [s]Sort [?]Help", enterAction)

	// Status on the right
	var status string
	if m.statusMessage != "" {
		if m.statusIsError {
			status = tuiErrorStyle.Render(m.statusMessage)
		} else {
			status = tuiSuccessStyle.Render(m.statusMessage)
		}
	} else if m.loading {
		status = m.spinner.View() + " Loading..."
	}

	// Build bar with commands left, status right
	if status != "" {
		// Pad between commands and status
		padding := max(m.width-len(commands)-len(m.statusMessage)-4, 2)
		return tuiStatusBarStyle.Width(m.width).Render(commands + strings.Repeat(" ", padding) + status)
	}

	return tuiStatusBarStyle.Width(m.width).Render(commands)
}

func (m *planModel) renderBeadLine(i int, bead beadItem) string {
	icon := statusIcon(bead.status)

	// Session indicator
	var sessionIndicator string
	if m.activeBeadSessions[bead.id] {
		sessionIndicator = tuiSuccessStyle.Render("[C]") + " "
	}

	// Selection indicator
	var prefix string
	if m.selectedBeads[bead.id] {
		prefix = tuiSelectedCheckStyle.Render("[*]")
	} else {
		prefix = "[ ]"
	}

	var line string
	if m.beadsExpanded {
		line = fmt.Sprintf("%s %s%s %s [P%d %s] %s", prefix, sessionIndicator, icon, bead.id, bead.priority, bead.beadType, bead.title)
	} else {
		line = fmt.Sprintf("%s %s%s %s %s", prefix, sessionIndicator, icon, bead.id, bead.title)
	}

	if i == m.beadsCursor {
		return tuiSelectedStyle.Render(line)
	}
	return line
}

func (m *planModel) renderWithDialog(dialog string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *planModel) renderHelp() string {
	help := `
  Plan Mode - Help

  Each issue gets its own dedicated Claude session in a separate tab.
  Use Enter to start or resume a planning session for an issue.

  Navigation
  ────────────────────────────
  j/k, ↑/↓      Navigate list
  Enter         Start/Resume planning session

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

  Session Indicators
  ────────────────────────────
  [C]           Issue has an active Claude session

  Press any key to close...
`
	return tuiHelpStyle.Width(m.width).Height(m.height).Render(help)
}

// Dialog update handlers
func (m *planModel) updateCreateBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check escape/cancel keys
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		return m, nil
	}
	switch msg.String() {
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
	if msg.Type == tea.KeyEsc || msg.String() == "esc" || msg.String() == "escape" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		return m, nil
	}
	switch msg.String() {
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
	if msg.Type == tea.KeyEsc || msg.String() == "esc" || msg.String() == "escape" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.filters.searchText = ""
		return m, m.refreshData()
	}
	switch msg.String() {
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
	if msg.Type == tea.KeyEsc || msg.String() == "esc" || msg.String() == "escape" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		return m, nil
	}
	switch msg.String() {
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
	if msg.Type == tea.KeyEsc || msg.String() == "esc" || msg.String() == "escape" {
		m.viewMode = ViewNormal
		return m, nil
	}
	switch msg.String() {
	case "y", "Y":
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			beadID := m.beadItems[m.beadsCursor].id
			m.viewMode = ViewNormal
			return m, m.closeBead(beadID)
		}
		m.viewMode = ViewNormal
		return m, nil
	case "n", "N":
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

		// Also fetch active sessions
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)

		return planDataMsg{
			beads:          items,
			activeSessions: activeSessions,
			err:            err,
		}
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
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}

func (m *planModel) closeBead(beadID string) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()
		session := m.sessionName()
		tabName := db.TabNameForBead(beadID)

		// If there's an active session for this bead, close it
		if m.activeBeadSessions[beadID] {
			// Terminate and close the tab
			_ = m.zj.TerminateAndCloseTab(m.ctx, session, tabName)
			// Unregister from database
			_ = m.proj.DB.UnregisterPlanSession(m.ctx, beadID)
		}

		// Close the bead
		cmd := exec.Command("bd", "close", beadID)
		cmd.Dir = mainRepoPath
		if err := cmd.Run(); err != nil {
			return planDataMsg{err: fmt.Errorf("failed to close issue: %w", err)}
		}

		// Refresh after close
		items, err := m.loadBeads()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}

// sessionName returns the zellij session name for this project
func (m *planModel) sessionName() string {
	return fmt.Sprintf("co-%s", m.proj.Config.Project.Name)
}

// spawnPlanSession spawns or resumes a planning session for a specific bead
func (m *planModel) spawnPlanSession(beadID string) tea.Cmd {
	return func() tea.Msg {
		session := m.sessionName()
		tabName := db.TabNameForBead(beadID)
		mainRepoPath := m.proj.MainRepoPath()

		// Ensure zellij session exists
		if err := m.zj.EnsureSession(m.ctx, session); err != nil {
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		// Check if session already running for this bead
		running, _ := m.proj.DB.IsPlanSessionRunning(m.ctx, beadID)
		if running {
			// Session exists - just switch to it
			if err := m.zj.SwitchToTab(m.ctx, session, tabName); err != nil {
				return planSessionSpawnedMsg{beadID: beadID, err: err}
			}
			return planSessionSpawnedMsg{beadID: beadID, resumed: true}
		}

		// Check if tab exists (might be orphaned)
		exists, _ := m.zj.TabExists(m.ctx, session, tabName)
		if exists {
			// Tab exists but session not registered - terminate and recreate
			_ = m.zj.TerminateAndCloseTab(m.ctx, session, tabName)
			time.Sleep(200 * time.Millisecond)
		}

		// Create new tab for this bead
		if err := m.zj.CreateTab(m.ctx, session, tabName, mainRepoPath); err != nil {
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		// Switch to the tab
		time.Sleep(200 * time.Millisecond)
		if err := m.zj.SwitchToTab(m.ctx, session, tabName); err != nil {
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		// Run co plan with the bead ID
		planCmd := fmt.Sprintf("co plan %s", beadID)
		time.Sleep(200 * time.Millisecond)
		if err := m.zj.ExecuteCommand(m.ctx, session, planCmd); err != nil {
			return planSessionSpawnedMsg{beadID: beadID, err: err}
		}

		return planSessionSpawnedMsg{beadID: beadID, resumed: false}
	}
}

// startPeriodicRefresh starts the periodic refresh timer
func (m *planModel) startPeriodicRefresh() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return planTickMsg(t)
	})
}
