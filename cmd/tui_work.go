package cmd

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/claude"
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
		createDescTextarea: createDescTa,
		loading:            true,
	}
}

// Init implements tea.Model
func (m *workModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.refreshData(),
		m.tick(),
	)
}

func (m *workModel) tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return workTickMsg(t)
	})
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
		// Refresh data when gaining focus and restart periodic tick and spinner
		m.loading = true
		return tea.Batch(m.spinner.Tick, m.refreshData(), m.tick())
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
	case tea.KeyMsg:
		// Handle view-specific keys first
		switch m.viewMode {
		case ViewCreateWork:
			return m.updateCreateWork(msg)
		case ViewCreateBead:
			return m.updateCreateBead(msg)
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
		// Periodic refresh
		cmds = append(cmds, m.refreshData())
		cmds = append(cmds, m.tick())
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

	case "U":
		// Update PR description task
		if m.activePanel == PanelLeft && len(m.works) > 0 {
			return m, m.updatePRDescriptionTask()
		}

	case "n":
		// Create new bead and assign to current work
		if m.activePanel == PanelLeft && len(m.works) > 0 {
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
		if m.activePanel == PanelLeft && len(m.works) > 0 {
			return m, m.openConsole()
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
		case "j", "down":
			m.createBeadType = (m.createBeadType + 1) % len(beadTypes)
		case "k", "up":
			m.createBeadType--
			if m.createBeadType < 0 {
				m.createBeadType = len(beadTypes) - 1
			}
		}
		return m, nil

	case 2: // Priority
		switch msg.String() {
		case "j", "down", "-":
			if m.createBeadPriority < 4 {
				m.createBeadPriority++
			}
		case "k", "up", "+", "=":
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
	case ViewCreateBead:
		dialog := m.renderCreateBeadDialogContent()
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
	title := tuiTitleStyle.Render("Workers")

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

			// Check if any task is actively processing
			hasActiveTask := false
			for _, tp := range wp.tasks {
				if tp.task.Status == db.StatusProcessing {
					hasActiveTask = true
					break
				}
			}

			var icon string
			if wp.work.Status == db.StatusProcessing && hasActiveTask {
				// Use spinner for works with active tasks
				icon = m.spinner.View()
			} else if wp.work.Status == db.StatusProcessing {
				// Idle processing (no active task) - use pause icon
				icon = statusProcessing.Render("◉")
			} else if isSelected {
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
	title := tuiTitleStyle.Render("Tasks")

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

	// Commands on the left (plain text for width calculation)
	keysPlain := "[c]reate [n]ew issue [d]estroy [p]lan [r]un [a]ssign [t]erminal [R]eview [P]R [?]help"
	keys := styleHotkeys(keysPlain)

	// Build bar with commands left, status right
	padding := max(m.width-len(keysPlain)-len(statusPlain)-4, 2)
	return tuiStatusBarStyle.Width(m.width).Render(keys + strings.Repeat(" ", padding) + status)
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

  Navigation
  ────────────────────────────
  h/l, ←/→      Move between panels
  j/k, ↑/↓      Navigate list
  Tab           Cycle panels
  g             Go to top
  G             Go to bottom

  Work Management
  ────────────────────────────
  c             Create new work
  n             Create new issue (assign to work)
  d             Destroy selected work
  p             Plan work (create tasks)
  r             Run work (execute tasks)
  a             Assign issues to work
  t             Open console tab
  R             Create review task
  P             Create PR task
  U             Update PR description

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
		result, err := CreateWorkWithBranch(m.ctx, m.proj, branchName, "main", WorkCreateOptions{Silent: true})
		if err != nil {
			return workCommandMsg{action: "Create work", err: err}
		}

		// Spawn the orchestrator for this work
		if err := claude.SpawnWorkOrchestrator(m.ctx, result.WorkID, m.proj.Config.Project.Name, result.WorktreePath, io.Discard); err != nil {
			// Non-fatal: work was created but orchestrator failed to spawn
			return workCommandMsg{action: fmt.Sprintf("Created work %s (orchestrator failed: %v)", result.WorkID, err)}
		}

		return workCommandMsg{action: fmt.Sprintf("Created work %s", result.WorkID)}
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

func (m *workModel) planWork(autoGroup bool) tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Plan work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		result, err := PlanWorkTasks(m.ctx, m.proj, workID, autoGroup, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Plan work", err: err}
		}

		if result.TasksCreated == 0 {
			return workCommandMsg{action: "Plan work (no unassigned beads)"}
		}
		return workCommandMsg{action: fmt.Sprintf("Planned %d task(s)", result.TasksCreated)}
	}
}

func (m *workModel) runWork() tea.Cmd {
	return func() tea.Msg {
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Run work", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID

		result, err := RunWork(m.ctx, m.proj, workID, false, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Run work", err: err}
		}

		orchestratorStatus := "running"
		if result.OrchestratorSpawned {
			orchestratorStatus = "spawned"
		}

		var msg string
		if result.TasksCreated > 0 {
			msg = fmt.Sprintf("Created %d task(s), orchestrator %s", result.TasksCreated, orchestratorStatus)
		} else {
			msg = fmt.Sprintf("Orchestrator %s", orchestratorStatus)
		}
		return workCommandMsg{action: msg}
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

		return workCommandMsg{action: fmt.Sprintf("Created review task %s", reviewTaskID)}
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
		if m.worksCursor >= len(m.works) {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("no work selected")}
		}
		workID := m.works[m.worksCursor].work.ID
		mainRepoPath := m.proj.MainRepoPath()

		// Create the bead using bd create
		args := []string{"create", "--title=" + title, "--type=" + beadType, fmt.Sprintf("--priority=%d", priority)}
		if isEpic {
			args = append(args, "--epic")
		}
		if description != "" {
			args = append(args, "--description="+description)
		}

		cmd := exec.Command("bd", args...)
		cmd.Dir = mainRepoPath
		output, err := cmd.Output()
		if err != nil {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("failed to create issue: %w", err)}
		}

		// Parse the bead ID from output (bd create outputs the created bead ID)
		beadID := strings.TrimSpace(string(output))
		if beadID == "" {
			return workCommandMsg{action: "Create issue", err: fmt.Errorf("failed to get created issue ID")}
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

		err := claude.OpenConsole(m.ctx, workID, m.proj.Config.Project.Name, wp.work.WorktreePath, m.proj.Config.Hooks.Env, io.Discard)
		if err != nil {
			return workCommandMsg{action: "Open console", err: err}
		}

		return workCommandMsg{action: fmt.Sprintf("Opened console for %s", workID)}
	}
}
