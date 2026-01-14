package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
)

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

	// UI state
	viewMode      ViewMode
	spinner       spinner.Model
	textInput     textinput.Model
	statusMessage string
	statusIsError bool
	lastUpdate    time.Time

	// Bead selection (for assign dialogs)
	beadItems     []beadItem
	beadsCursor   int
	selectedBeads map[string]bool

	// Create bead state (for work creation with beads)
	createBeadType     int
	createBeadPriority int
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

// newWorkModel creates a new Work Mode model
func newWorkModel(ctx context.Context, proj *project.Project) *workModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	ti := textinput.New()
	ti.Placeholder = "feature/my-branch"
	ti.CharLimit = 100
	ti.Width = 40

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
		loading:            true,
	}
}

// Init implements tea.Model
func (m *workModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.refreshData(),
	)
}

// SetSize implements SubModel
func (m *workModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// FocusChanged implements SubModel
func (m *workModel) FocusChanged(focused bool) tea.Cmd {
	m.focused = focused
	if focused {
		// Refresh data when gaining focus
		m.loading = true
		return m.refreshData()
	}
	return nil
}

// Update implements tea.Model
func (m *workModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		}
		cmds = append(cmds, m.refreshData())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	}

	return m, tea.Batch(cmds...)
}

func (m *workModel) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
				if m.tasksCursor < len(wp.tasks)-1 {
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
		if m.activePanel > PanelLeft {
			m.activePanel--
		}

	case "l", "right":
		if m.activePanel < PanelRight {
			m.activePanel++
		}

	case "tab":
		m.activePanel = (m.activePanel + 1) % 3

	case "1":
		m.activePanel = PanelLeft
	case "2":
		m.activePanel = PanelMiddle
	case "3":
		m.activePanel = PanelRight

	case "c":
		// Create work
		if m.activePanel == PanelLeft {
			m.textInput.Reset()
			m.textInput.Placeholder = "feature/my-branch"
			m.textInput.Focus()
			m.viewMode = ViewCreateWork
		}

	case "d":
		// Destroy work
		if m.activePanel == PanelLeft && len(m.works) > 0 {
			m.viewMode = ViewDestroyConfirm
		}

	case "p":
		// Plan work
		if m.activePanel == PanelLeft && len(m.works) > 0 {
			m.viewMode = ViewPlanDialog
		}

	case "r":
		// Run work
		if m.activePanel == PanelLeft && len(m.works) > 0 {
			return m, m.runWork()
		}

	case "a":
		// Assign beads to work
		if m.activePanel == PanelLeft && len(m.works) > 0 {
			m.viewMode = ViewAssignBeads
			m.beadsCursor = 0
			// Load beads for selection
			return m, m.loadBeadsForAssign()
		}

	case "R":
		// Create review task
		if m.activePanel == PanelLeft && len(m.works) > 0 {
			return m, m.createReviewTask()
		}

	case "P":
		// Create PR task
		if m.activePanel == PanelLeft && len(m.works) > 0 {
			return m, m.createPRTask()
		}

	case "?":
		m.viewMode = ViewHelp

	case "g":
		// Go to top
		if m.activePanel == PanelLeft {
			m.worksCursor = 0
			m.tasksCursor = 0
		} else if m.activePanel == PanelMiddle {
			m.tasksCursor = 0
		}

	case "G":
		// Go to bottom
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

	return m, nil
}

func (m *workModel) updateCreateWork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		branchName := m.textInput.Value()
		if branchName == "" {
			m.viewMode = ViewNormal
			return m, nil
		}
		m.viewMode = ViewNormal
		return m, m.createWork(branchName)
	case "esc":
		m.viewMode = ViewNormal
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
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

func (m *workModel) updatePlanDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "a":
		// Auto-group planning
		m.viewMode = ViewNormal
		return m, m.planWork(true)
	case "s":
		// Single-bead planning
		m.viewMode = ViewNormal
		return m, m.planWork(false)
	case "esc":
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
	case "enter":
		m.viewMode = ViewNormal
		return m, m.assignSelectedBeads()
	case "esc":
		m.viewMode = ViewNormal
		m.selectedBeads = make(map[string]bool)
	}
	return m, nil
}

// View implements tea.Model
func (m *workModel) View() string {
	if m.viewMode == ViewHelp {
		return m.renderHelp()
	}

	// Calculate panel dimensions
	panelWidth1 := m.width / 3
	panelWidth2 := m.width / 3
	panelWidth3 := m.width - panelWidth1 - panelWidth2
	panelHeight := m.height - 2 // Reserve 2 lines for status bar

	// Render three panels: Works | Tasks | Details
	leftPanel := m.renderWorksPanel(panelWidth1, panelHeight)
	middlePanel := m.renderTasksPanel(panelWidth2, panelHeight)
	rightPanel := m.renderDetailsPanel(panelWidth3, panelHeight)

	content := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, middlePanel, rightPanel)

	// Render status bar
	statusBar := m.renderStatusBar()

	// Handle dialogs
	switch m.viewMode {
	case ViewCreateWork:
		dialog := m.renderCreateWorkDialog()
		return m.renderWithDialog(dialog)
	case ViewDestroyConfirm:
		dialog := m.renderDestroyConfirmDialog()
		return m.renderWithDialog(dialog)
	case ViewPlanDialog:
		dialog := m.renderPlanDialogContent()
		return m.renderWithDialog(dialog)
	case ViewAssignBeads:
		return m.renderAssignBeadsView()
	}

	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

func (m *workModel) renderWithDialog(dialog string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *workModel) renderWorksPanel(width, height int) string {
	title := "[1] Workers"
	if m.activePanel == PanelLeft {
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
		content.WriteString(tuiDimStyle.Render("No workers"))
		content.WriteString("\n")
		content.WriteString(tuiDimStyle.Render("Press 'c' to create"))
	} else {
		visibleLines := height - 3
		if visibleLines < 1 {
			visibleLines = 1
		}

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

		if startIdx > 0 {
			content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↑ %d more", startIdx)))
			content.WriteString("\n")
		}

		for i := startIdx; i < endIdx; i++ {
			wp := m.works[i]
			isSelected := i == m.worksCursor && m.activePanel == PanelLeft

			var icon string
			if isSelected {
				icon = statusIconPlain(wp.work.Status)
			} else {
				icon = statusIcon(wp.work.Status)
			}

			// Show human-readable name if available, otherwise work ID
			displayName := wp.work.Name
			if displayName == "" {
				displayName = wp.work.ID
			}

			line := fmt.Sprintf("%s %s", icon, displayName)

			// Show work ID as subtitle if we have a name
			if wp.work.Name != "" {
				line = fmt.Sprintf("%s %s (%s)", icon, displayName, wp.work.ID)
			}

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

		if endIdx < len(m.works) {
			content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.works)-endIdx)))
			content.WriteString("\n")
		}
	}

	style := tuiPanelStyle
	if m.activePanel == PanelLeft {
		style = tuiActivePanelStyle
	}
	return style.Width(width - 2).Height(height).Render(content.String())
}

func (m *workModel) renderTasksPanel(width, height int) string {
	title := "[2] Tasks"
	if m.activePanel == PanelMiddle {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

	var content strings.Builder
	content.WriteString(title)
	content.WriteString("\n")

	if len(m.works) == 0 || m.worksCursor >= len(m.works) {
		content.WriteString(tuiDimStyle.Render("No work selected"))
	} else {
		wp := m.works[m.worksCursor]
		if len(wp.tasks) == 0 {
			content.WriteString(tuiDimStyle.Render("No tasks"))
			content.WriteString("\n")
			content.WriteString(tuiDimStyle.Render("Press 'p' to plan"))
		} else {
			visibleLines := height - 3
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

				var icon string
				if isSelected {
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
	}

	style := tuiPanelStyle
	if m.activePanel == PanelMiddle {
		style = tuiActivePanelStyle
	}
	return style.Width(width - 2).Height(height).Render(content.String())
}

func (m *workModel) renderDetailsPanel(width, height int) string {
	title := "[3] Details"
	if m.activePanel == PanelRight {
		title = tuiActiveTabStyle.Render(title)
	} else {
		title = tuiInactiveTabStyle.Render(title)
	}

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
	}

	style := tuiPanelStyle
	if m.activePanel == PanelRight {
		style = tuiActivePanelStyle
	}
	return style.Width(width - 2).Height(height).Render(content.String())
}

func (m *workModel) renderStatusBar() string {
	var status string
	if m.statusMessage != "" {
		if m.statusIsError {
			status = tuiErrorStyle.Render(m.statusMessage)
		} else {
			status = tuiSuccessStyle.Render(m.statusMessage)
		}
	} else {
		status = tuiDimStyle.Render(fmt.Sprintf("Updated: %s", m.lastUpdate.Format("15:04:05")))
	}

	keys := "[c]reate [d]estroy [p]lan [r]un [a]ssign [R]eview [P]R [?]help"

	return tuiStatusBarStyle.Width(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Left,
			status,
			strings.Repeat(" ", max(0, m.width-lipgloss.Width(status)-lipgloss.Width(keys)-4)),
			tuiDimStyle.Render(keys),
		),
	)
}

func (m *workModel) renderCreateWorkDialog() string {
	selectedCount := 0
	for _, selected := range m.selectedBeads {
		if selected {
			selectedCount++
		}
	}

	var beadOption string
	if selectedCount > 0 {
		beadOption = fmt.Sprintf("\n  [b] Use %d selected issue(s) to auto-generate branch", selectedCount)
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

func (m *workModel) renderPlanDialogContent() string {
	content := `
  Plan Work

  Choose planning mode:

  [a] Auto-group - LLM estimates complexity
  [s] Single-bead - One task per bead

  [Esc] Cancel
`
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

  Navigation
  ────────────────────────────
  h, ←          Move to previous panel
  l, →          Move to next panel
  j/k, ↑/↓      Navigate list
  Tab, 1-3      Jump to panel
  g             Go to top
  G             Go to bottom

  Work Management
  ────────────────────────────
  c             Create new work
  d             Destroy selected work
  p             Plan work (create tasks)
  r             Run work (execute tasks)
  a             Assign issues to work
  R             Create review task
  P             Create PR task

  General
  ────────────────────────────
  ?             Show this help
  Esc           Close dialogs

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

func (m *workModel) createWork(branchName string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("co", "work", "create", "--branch", branchName)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Create work", err: err}
		}
		return workCommandMsg{action: "Create work"}
	}
}

func (m *workModel) destroyWork() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Destroy work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		cmd := exec.Command("co", "work", "destroy", workID)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Destroy work", err: err}
		}
		return workCommandMsg{action: "Destroy work"}
	}
}

func (m *workModel) planWork(autoGroup bool) tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Plan work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		args := []string{"run", "--work", workID, "--plan-only"}
		if autoGroup {
			args = append(args, "--plan")
		}

		cmd := exec.Command("co", args...)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Plan work", err: err}
		}
		return workCommandMsg{action: "Plan work"}
	}
}

func (m *workModel) runWork() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Run work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		cmd := exec.Command("co", "run", "--work", workID)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Run work", err: err}
		}
		return workCommandMsg{action: "Run work"}
	}
}

func (m *workModel) createReviewTask() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create review", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		cmd := exec.Command("co", "work", "review", workID)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Create review", err: err}
		}
		return workCommandMsg{action: "Create review"}
	}
}

func (m *workModel) createPRTask() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create PR", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		cmd := exec.Command("co", "work", "pr", workID)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Create PR", err: err}
		}
		return workCommandMsg{action: "Create PR"}
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

		args := []string{"work", "add"}
		args = append(args, beadIDs...)
		args = append(args, "--work", workID)

		cmd := exec.Command("co", args...)
		cmd.Dir = m.proj.Root
		if err := cmd.Run(); err != nil {
			return workCommandMsg{action: "Assign beads", err: err}
		}
		return workCommandMsg{action: "Assign beads"}
	}
}
