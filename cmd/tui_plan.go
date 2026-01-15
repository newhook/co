package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
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

	// Create bead state
	createBeadType     int // 0=task, 1=bug, 2=feature
	createBeadPriority int // 0-4, default 2
	createDialogFocus  int // 0=title, 1=type, 2=priority

	// Add child bead state
	parentBeadID string // ID of parent when adding child

	// Edit bead state
	editBeadID        string         // ID of bead being edited
	editTitleTextarea textarea.Model // Textarea for title editing
	editDescTextarea  textarea.Model // Textarea for description editing
	editBeadType      int            // Index into beadTypes
	editField         int            // 0=title, 1=type, 2=description, 3=buttons
	editButtonIdx     int            // 0=OK, 1=Cancel

	// Create work dialog state
	createWorkBeadID    string         // Bead ID for work creation
	createWorkBranch    textinput.Model // Editable branch name
	createWorkField     int            // 0=branch, 1=buttons
	createWorkButtonIdx int            // 0=Execute, 1=Auto, 2=Cancel

	// Add to work state
	availableWorks []workItem // List of works to choose from
	worksCursor    int        // Cursor position in works list

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

	titleTa := textarea.New()
	titleTa.Placeholder = "Enter title..."
	titleTa.CharLimit = 200
	titleTa.SetWidth(60)
	titleTa.SetHeight(2)

	descTa := textarea.New()
	descTa.Placeholder = "Enter description..."
	descTa.CharLimit = 2000
	descTa.SetWidth(60)
	descTa.SetHeight(6)

	branchInput := textinput.New()
	branchInput.Placeholder = "Branch name..."
	branchInput.CharLimit = 100
	branchInput.Width = 60

	return &planModel{
		editTitleTextarea:  titleTa,
		editDescTextarea:   descTa,
		createWorkBranch:   branchInput,
		ctx:                ctx,
		proj:               proj,
		width:              80,
		height:             24,
		activePanel:        PanelLeft,
		spinner:            s,
		textInput:          ti,
		activeBeadSessions: make(map[string]bool),
		createBeadPriority: 2,
		zj:                 zellij.New(),
		filters: beadFilters{
			status: "open",
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

// InModal returns true if in a modal/dialog state
func (m *planModel) InModal() bool {
	return m.viewMode != ViewNormal
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

	case planWorkCreatedMsg:
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to create work: %v", msg.err)
			m.statusIsError = true
		} else {
			m.statusMessage = fmt.Sprintf("Created work %s from %s", msg.workID, msg.beadID)
			m.statusIsError = false
		}
		return m, nil

	case worksLoadedMsg:
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to load works: %v", msg.err)
			m.statusIsError = true
			return m, nil
		}
		m.availableWorks = msg.works
		m.worksCursor = 0
		m.viewMode = ViewAddToWork
		return m, nil

	case beadAddedToWorkMsg:
		m.viewMode = ViewNormal
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to add issue: %v", msg.err)
			m.statusIsError = true
		} else {
			m.statusMessage = fmt.Sprintf("Added %s to work %s", msg.beadID, msg.workID)
			m.statusIsError = false
		}
		return m, nil

	case editorFinishedMsg:
		// Refresh data after external editor closes
		m.statusMessage = "Editor closed, refreshing..."
		m.statusIsError = false
		return m, m.refreshData()

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	default:
		// Handle Kitty keyboard protocol escape sequences
		// Kitty/Ghostty send keys as CSI <keycode> ; <modifiers> u
		typeName := fmt.Sprintf("%T", msg)
		if typeName == "tea.unknownCSISequenceMsg" {
			msgStr := fmt.Sprintf("%s", msg)
			// Check for Kitty protocol escape key: "?CSI[50 55 117]?" = "27u"
			if strings.Contains(msgStr, "50 55 117") {
				return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
			}
			// Check for Ctrl+G: 103;5u = bytes "49 48 51 59 53 117"
			if strings.Contains(msgStr, "49 48 51 59 53 117") {
				return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlG})
			}
			// Check for Ctrl+S: 115;5u = bytes "49 49 53 59 53 117"
			if strings.Contains(msgStr, "49 49 53 59 53 117") {
				return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlS})
			}
			// Check for Ctrl+O: 111;5u = bytes "49 49 49 59 53 117"
			if strings.Contains(msgStr, "49 49 49 59 53 117") {
				return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlO})
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

// planWorkCreatedMsg indicates work was created from a bead
type planWorkCreatedMsg struct {
	beadID string
	workID string
	err    error
}

func (m *planModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle dialog-specific input
	switch m.viewMode {
	case ViewCreateBead:
		return m.updateCreateBead(msg)
	case ViewAddChildBead:
		return m.updateAddChildBead(msg)
	case ViewEditBead:
		return m.updateEditBead(msg)
	case ViewCreateWork:
		return m.updateCreateWorkDialog(msg)
	case ViewAddToWork:
		return m.updateAddToWork(msg)
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
		m.createDialogFocus = 0 // Start with title focused
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

	case "enter":
		// Spawn/resume planning session for selected bead
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			beadID := m.beadItems[m.beadsCursor].id
			return m, m.spawnPlanSession(beadID)
		}
		return m, nil

	case "w":
		// Create work from selected bead - show dialog
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			bead := m.beadItems[m.beadsCursor]
			m.createWorkBeadID = bead.id
			// Generate proposed branch name
			branchName := generateBranchNameFromBeads([]*beads.Bead{{
				ID:    bead.id,
				Title: bead.title,
			}})
			m.createWorkBranch.SetValue(branchName)
			m.createWorkBranch.Focus()
			m.createWorkField = 0
			m.createWorkButtonIdx = 0
			m.viewMode = ViewCreateWork
		}
		return m, nil

	case "a":
		// Add child issue to selected issue
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			m.parentBeadID = m.beadItems[m.beadsCursor].id
			m.viewMode = ViewAddChildBead
			m.textInput.Reset()
			m.textInput.Focus()
			m.createBeadType = 0
			m.createBeadPriority = 2
			m.createDialogFocus = 0 // Start with title focused
		}
		return m, nil

	case "e":
		// Edit selected issue with textarea
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			bead := m.beadItems[m.beadsCursor]
			m.editBeadID = bead.id
			m.viewMode = ViewEditBead
			m.editTitleTextarea.Reset()
			m.editTitleTextarea.SetValue(bead.title)
			m.editTitleTextarea.Focus()
			m.editDescTextarea.Reset()
			m.editDescTextarea.SetValue(bead.description)
			// Find the type index
			m.editBeadType = 0
			for i, t := range beadTypes {
				if t == bead.beadType {
					m.editBeadType = i
					break
				}
			}
			m.editField = 0     // Start with title focused
			m.editButtonIdx = 0 // OK selected by default
		}
		return m, nil

	case "E":
		// Edit selected issue in external editor
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			bead := m.beadItems[m.beadsCursor]
			return m, m.openInEditor(bead.id)
		}
		return m, nil

	case "W":
		// Add selected issue to existing work
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			return m, m.loadAvailableWorks()
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
	case ViewAddChildBead:
		return m.renderWithDialog(m.renderAddChildBeadDialogContent())
	case ViewEditBead:
		return m.renderWithDialog(m.renderEditBeadDialogContent())
	case ViewCreateWork:
		return m.renderWithDialog(m.renderCreateWorkDialogContent())
	case ViewAddToWork:
		return m.renderWithDialog(m.renderAddToWorkDialogContent())
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

		// Calculate remaining lines for description and children
		linesUsed := 2 // header + title
		remainingLines := visibleLines - linesUsed

		// Show description if we have room
		if bead.description != "" && remainingLines > 2 {
			content.WriteString("\n")
			desc := bead.description
			// Reserve lines for children section
			descLines := remainingLines - 2 // Reserve 2 lines for children header + some items
			if len(bead.children) > 0 {
				descLines = min(descLines, 2) // Limit description to 2 lines if we have children
			}
			maxLen := descLines * 80
			if len(desc) > maxLen && maxLen > 0 {
				desc = desc[:maxLen] + "..."
			}
			content.WriteString(tuiDimStyle.Render(desc))
			linesUsed++
			remainingLines--
		}

		// Show children (issues blocked by this one) if we have them
		if len(bead.children) > 0 && remainingLines > 1 {
			content.WriteString("\n")
			content.WriteString(tuiLabelStyle.Render("Blocks: "))
			linesUsed++
			remainingLines--

			// Build a map for quick lookup of child status
			childMap := make(map[string]*beadItem)
			for i := range m.beadItems {
				childMap[m.beadItems[i].id] = &m.beadItems[i]
			}

			// Show children with status
			maxChildren := min(len(bead.children), remainingLines)
			for i := 0; i < maxChildren; i++ {
				childID := bead.children[i]
				if child, ok := childMap[childID]; ok {
					content.WriteString(fmt.Sprintf("\n  %s %s %s",
						statusIcon(child.status),
						issueIDStyle.Render(child.id),
						child.title))
				} else {
					// Child not in current view (maybe filtered out)
					content.WriteString(fmt.Sprintf("\n  ? %s", issueIDStyle.Render(childID)))
				}
			}
			if len(bead.children) > maxChildren {
				content.WriteString(fmt.Sprintf("\n  ... and %d more", len(bead.children)-maxChildren))
			}
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

	// Commands on the left (plain text for width calculation)
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work %s [?]Help", enterAction)
	commands := styleHotkeys(commandsPlain)

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
	} else if m.loading {
		statusPlain = "Loading..."
		status = m.spinner.View() + " Loading..."
	} else {
		statusPlain = fmt.Sprintf("Updated: %s", m.lastUpdate.Format("15:04:05"))
		status = tuiDimStyle.Render(statusPlain)
	}

	// Build bar with commands left, status right
	padding := max(m.width-len(commandsPlain)-len(statusPlain)-4, 2)
	return tuiStatusBarStyle.Width(m.width).Render(commands + strings.Repeat(" ", padding) + status)
}

func (m *planModel) renderBeadLine(i int, bead beadItem) string {
	icon := statusIcon(bead.status)

	// Session indicator
	var sessionIndicator string
	if m.activeBeadSessions[bead.id] {
		sessionIndicator = tuiSuccessStyle.Render("[C]") + " "
	}

	// Tree indentation with connector lines (styled dim)
	var treePrefix string
	if bead.treeDepth > 0 {
		indent := strings.Repeat("  ", bead.treeDepth-1)
		treePrefix = issueTreeStyle.Render(indent + "└─")
	}

	// Styled issue ID
	styledID := issueIDStyle.Render(bead.id)

	// Short type indicator with color
	var styledType string
	switch bead.beadType {
	case "task":
		styledType = typeTaskStyle.Render("T")
	case "bug":
		styledType = typeBugStyle.Render("B")
	case "feature":
		styledType = typeFeatureStyle.Render("F")
	case "epic":
		styledType = typeEpicStyle.Render("E")
	case "chore":
		styledType = typeChoreStyle.Render("C")
	case "merge-request":
		styledType = typeDefaultStyle.Render("M")
	case "molecule":
		styledType = typeDefaultStyle.Render("m")
	case "gate":
		styledType = typeDefaultStyle.Render("G")
	case "agent":
		styledType = typeDefaultStyle.Render("A")
	case "role":
		styledType = typeDefaultStyle.Render("R")
	case "rig":
		styledType = typeDefaultStyle.Render("r")
	case "convoy":
		styledType = typeDefaultStyle.Render("c")
	case "event":
		styledType = typeDefaultStyle.Render("v")
	default:
		styledType = typeDefaultStyle.Render("?")
	}

	var line string
	if m.beadsExpanded {
		line = fmt.Sprintf("%s%s%s %s [P%d %s] %s", treePrefix, sessionIndicator, icon, styledID, bead.priority, bead.beadType, bead.title)
	} else {
		line = fmt.Sprintf("%s%s%s %s %s %s", treePrefix, sessionIndicator, icon, styledID, styledType, bead.title)
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
  n             Create new issue (any type)
  e             Edit issue inline (textarea)
  E             Edit issue in $EDITOR
  a             Add child issue (blocked by selected)
  x             Close selected issue
  w             Create work from issue
  W             Add issue to existing work

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

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> title(0)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		m.createDialogFocus = (m.createDialogFocus + 1) % 3
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else {
			m.textInput.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.createDialogFocus--
		if m.createDialogFocus < 0 {
			m.createDialogFocus = 2
		}
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else {
			m.textInput.Blur()
		}
		return m, nil
	}

	// Enter submits from any field
	if msg.String() == "enter" {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			beadType := beadTypes[m.createBeadType]
			isEpic := beadType == "epic"
			m.viewMode = ViewNormal
			return m, m.createBead(title, beadType, m.createBeadPriority, isEpic)
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
	}

	return m, nil
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

var editDebugLog *log.Logger

func init() {
	f, _ := os.OpenFile("/tmp/edit-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if f != nil {
		editDebugLog = log.New(f, "", log.LstdFlags)
	}
}

func (m *planModel) updateEditBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if editDebugLog != nil {
		editDebugLog.Printf("updateEditBead: key=%q type=%d field=%d", msg.String(), msg.Type, m.editField)
	}

	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.editTitleTextarea.Blur()
		m.editDescTextarea.Blur()
		m.editBeadID = ""
		return m, nil
	}

	// Tab cycles between title(0), type(1), description(2), buttons(3)
	if msg.Type == tea.KeyTab {
		m.editField = (m.editField + 1) % 4
		m.editTitleTextarea.Blur()
		m.editDescTextarea.Blur()
		if m.editField == 0 {
			m.editTitleTextarea.Focus()
		} else if m.editField == 2 {
			m.editDescTextarea.Focus()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.editField--
		if m.editField < 0 {
			m.editField = 3
		}
		m.editTitleTextarea.Blur()
		m.editDescTextarea.Blur()
		if m.editField == 0 {
			m.editTitleTextarea.Focus()
		} else if m.editField == 2 {
			m.editDescTextarea.Focus()
		}
		return m, nil
	}

	// Handle input based on focused field
	var cmd tea.Cmd
	switch m.editField {
	case 0: // Title
		m.editTitleTextarea, cmd = m.editTitleTextarea.Update(msg)
	case 1: // Type selector
		switch msg.String() {
		case "j", "down", "h", "left":
			m.editBeadType--
			if m.editBeadType < 0 {
				m.editBeadType = len(beadTypes) - 1
			}
		case "k", "up", "l", "right":
			m.editBeadType = (m.editBeadType + 1) % len(beadTypes)
		}
	case 2: // Description
		m.editDescTextarea, cmd = m.editDescTextarea.Update(msg)
	case 3: // Buttons
		switch msg.String() {
		case "h", "left", "j", "k", "up", "down", "l", "right":
			// Toggle between OK(0) and Cancel(1)
			m.editButtonIdx = 1 - m.editButtonIdx
		case "enter":
			if m.editButtonIdx == 0 {
				// OK - save
				title := strings.TrimSpace(m.editTitleTextarea.Value())
				desc := strings.TrimSpace(m.editDescTextarea.Value())
				beadType := beadTypes[m.editBeadType]
				if title != "" {
					m.viewMode = ViewNormal
					return m, m.saveBeadEdit(m.editBeadID, title, desc, beadType)
				}
			} else {
				// Cancel
				m.viewMode = ViewNormal
				m.editTitleTextarea.Blur()
				m.editDescTextarea.Blur()
				m.editBeadID = ""
			}
			return m, nil
		}
	}
	return m, cmd
}

// Dialog render helpers
func (m *planModel) renderCreateBeadDialogContent() string {
	typeFocused := m.createDialogFocus == 1
	priorityFocused := m.createDialogFocus == 2

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
	if m.createDialogFocus == 0 {
		titleLabel = tuiValueStyle.Render("Title:") + " (editing)"
	}
	if typeFocused {
		typeLabel = tuiValueStyle.Render("Type:") + " (j/k)"
	}
	if priorityFocused {
		priorityLabel = tuiValueStyle.Render("Priority:") + " (j/k)"
	}

	content := fmt.Sprintf(`  Create New Issue

  %s
  %s

  %s %s
  %s %s

  [Tab] Next field  [Enter] Create  [Esc] Cancel
`, titleLabel, m.textInput.View(), typeLabel, typeDisplay, priorityLabel, priorityDisplay)

	return tuiDialogStyle.Render(content)
}

func (m *planModel) renderEditBeadDialogContent() string {
	// Show focus labels
	titleLabel := "Title:"
	typeLabel := "Type:"
	descLabel := "Description:"

	switch m.editField {
	case 0:
		titleLabel = tuiValueStyle.Render("Title:") + " (editing)"
	case 1:
		typeLabel = tuiValueStyle.Render("Type:") + " (←/→)"
	case 2:
		descLabel = tuiValueStyle.Render("Description:") + " (editing)"
	}

	// Type rotator display
	currentType := beadTypes[m.editBeadType]
	var typeDisplay string
	if m.editField == 1 {
		typeDisplay = fmt.Sprintf("< %s >", tuiValueStyle.Render(currentType))
	} else {
		typeDisplay = typeFeatureStyle.Render(currentType)
	}

	// Render OK/Cancel buttons
	var okBtn, cancelBtn string
	if m.editField == 3 {
		if m.editButtonIdx == 0 {
			okBtn = tuiSelectedStyle.Render(" OK ")
			cancelBtn = tuiDimStyle.Render(" Cancel ")
		} else {
			okBtn = tuiDimStyle.Render(" OK ")
			cancelBtn = tuiSelectedStyle.Render(" Cancel ")
		}
	} else {
		okBtn = tuiDimStyle.Render(" OK ")
		cancelBtn = tuiDimStyle.Render(" Cancel ")
	}

	content := fmt.Sprintf(`  Edit Issue %s

  %s
%s

  %s %s

  %s
%s

  %s  %s

  [Tab] Switch field  [Esc] Cancel
`, issueIDStyle.Render(m.editBeadID), titleLabel, m.editTitleTextarea.View(), typeLabel, typeDisplay, descLabel, m.editDescTextarea.View(), okBtn, cancelBtn)

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

	// Build tree structure from dependencies
	items = buildBeadTree(items, mainRepoPath)

	// If no tree structure, apply regular sorting
	hasTree := false
	for _, item := range items {
		if item.treeDepth > 0 || item.dependentCount > 0 {
			hasTree = true
			break
		}
	}

	if !hasTree {
		// Fall back to regular sorting if no tree structure
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

func (m *planModel) saveBeadEdit(beadID, title, description, beadType string) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()

		// Update the bead using bd update
		args := []string{"update", beadID, "--title=" + title, "--type=" + beadType}
		if description != "" {
			args = append(args, "--description="+description)
		}

		cmd := exec.Command("bd", args...)
		cmd.Dir = mainRepoPath
		if err := cmd.Run(); err != nil {
			return planDataMsg{err: fmt.Errorf("failed to update issue: %w", err)}
		}

		// Refresh after update
		items, err := m.loadBeads()
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}

// openInEditor opens the issue in $EDITOR using bd edit
func (m *planModel) openInEditor(beadID string) tea.Cmd {
	mainRepoPath := m.proj.MainRepoPath()

	// Use bd edit which handles $EDITOR and the issue format
	c := exec.Command("bd", "edit", beadID)
	c.Dir = mainRepoPath
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return planStatusMsg{message: fmt.Sprintf("Editor error: %v", err), isError: true}
		}
		// Refresh data after editing
		return editorFinishedMsg{}
	})
}

// editorFinishedMsg is sent when the external editor closes
type editorFinishedMsg struct{}

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

// updateCreateWorkDialog handles input for the create work dialog
func (m *planModel) updateCreateWorkDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.viewMode = ViewNormal
		m.createWorkBranch.Blur()
		return m, nil
	}

	// Tab cycles between branch(0), buttons(1)
	if msg.Type == tea.KeyTab {
		m.createWorkField = (m.createWorkField + 1) % 2
		if m.createWorkField == 0 {
			m.createWorkBranch.Focus()
		} else {
			m.createWorkBranch.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.createWorkField = 1 - m.createWorkField
		if m.createWorkField == 0 {
			m.createWorkBranch.Focus()
		} else {
			m.createWorkBranch.Blur()
		}
		return m, nil
	}

	// Handle input based on focused field
	var cmd tea.Cmd
	switch m.createWorkField {
	case 0: // Branch name input
		m.createWorkBranch, cmd = m.createWorkBranch.Update(msg)
	case 1: // Buttons
		switch msg.String() {
		case "h", "left":
			m.createWorkButtonIdx--
			if m.createWorkButtonIdx < 0 {
				m.createWorkButtonIdx = 2
			}
		case "l", "right":
			m.createWorkButtonIdx = (m.createWorkButtonIdx + 1) % 3
		case "enter":
			branchName := strings.TrimSpace(m.createWorkBranch.Value())
			if branchName == "" {
				return m, nil
			}
			switch m.createWorkButtonIdx {
			case 0: // Execute
				m.viewMode = ViewNormal
				return m, m.executeCreateWork(m.createWorkBeadID, branchName, false)
			case 1: // Auto
				m.viewMode = ViewNormal
				return m, m.executeCreateWork(m.createWorkBeadID, branchName, true)
			case 2: // Cancel
				m.viewMode = ViewNormal
				m.createWorkBranch.Blur()
			}
			return m, nil
		}
	}
	return m, cmd
}

// renderCreateWorkDialogContent renders the create work dialog
func (m *planModel) renderCreateWorkDialogContent() string {
	branchLabel := "Branch:"
	if m.createWorkField == 0 {
		branchLabel = tuiValueStyle.Render("Branch:") + " (editing)"
	}

	// Render buttons
	var execBtn, autoBtn, cancelBtn string
	if m.createWorkField == 1 {
		switch m.createWorkButtonIdx {
		case 0:
			execBtn = tuiSelectedStyle.Render(" Execute ")
			autoBtn = tuiDimStyle.Render(" Auto ")
			cancelBtn = tuiDimStyle.Render(" Cancel ")
		case 1:
			execBtn = tuiDimStyle.Render(" Execute ")
			autoBtn = tuiSelectedStyle.Render(" Auto ")
			cancelBtn = tuiDimStyle.Render(" Cancel ")
		case 2:
			execBtn = tuiDimStyle.Render(" Execute ")
			autoBtn = tuiDimStyle.Render(" Auto ")
			cancelBtn = tuiSelectedStyle.Render(" Cancel ")
		}
	} else {
		execBtn = tuiDimStyle.Render(" Execute ")
		autoBtn = tuiDimStyle.Render(" Auto ")
		cancelBtn = tuiDimStyle.Render(" Cancel ")
	}

	content := fmt.Sprintf(`  Create Work from %s

  %s
  %s

  %s  %s  %s

  [Tab] Switch field  [Esc] Cancel
`, issueIDStyle.Render(m.createWorkBeadID), branchLabel, m.createWorkBranch.View(), execBtn, autoBtn, cancelBtn)

	return tuiDialogStyle.Render(content)
}

// executeCreateWork creates a work unit with the given branch name
// This calls internal logic directly instead of shelling out to the CLI.
func (m *planModel) executeCreateWork(beadID, branchName string, auto bool) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()

		// Expand the bead (handles epics and transitive deps)
		expandedBeads, err := collectBeadsForAutomatedWorkflow(beadID, mainRepoPath)
		if err != nil {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("failed to expand bead %s: %w", beadID, err)}
		}

		if len(expandedBeads) == 0 {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("no beads found for %s", beadID)}
		}

		// Convert to beadGroup for compatibility with existing code
		var groupBeads []*beads.Bead
		for _, b := range expandedBeads {
			groupBeads = append(groupBeads, &beads.Bead{
				ID:          b.ID,
				Title:       b.Title,
				Description: b.Description,
			})
		}
		beadGroups := []beadGroup{{beads: groupBeads}}

		// Create work with branch name
		result, err := CreateWorkWithBranch(m.ctx, m.proj, branchName, "main")
		if err != nil {
			return planWorkCreatedMsg{beadID: beadID, err: fmt.Errorf("failed to create work: %w", err)}
		}

		// Add beads to the work
		if err := addBeadGroupsToWork(m.ctx, m.proj, result.WorkID, beadGroups); err != nil {
			// Work was created but beads couldn't be added - don't fail completely
			return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID, err: fmt.Errorf("work created but failed to add beads: %w", err)}
		}

		// Spawn the orchestrator for this work (or run automated workflow if auto)
		if auto {
			// Run automated workflow in a separate goroutine since it's long-running
			go func() {
				_ = runAutomatedWorkflowForWork(m.proj, result.WorkID, result.WorktreePath)
			}()
		} else {
			// Spawn the orchestrator
			if err := claude.SpawnWorkOrchestrator(m.ctx, result.WorkID, m.proj.Config.Project.Name, result.WorktreePath); err != nil {
				// Non-fatal: work was created but orchestrator failed to spawn
				return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID, err: fmt.Errorf("work created but orchestrator failed: %w", err)}
			}
		}

		return planWorkCreatedMsg{beadID: beadID, workID: result.WorkID}
	}
}

// startPeriodicRefresh starts the periodic refresh timer
func (m *planModel) startPeriodicRefresh() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return planTickMsg(t)
	})
}

// fetchDependencies gets the list of issue IDs that block the given issue
func fetchDependencies(dir, beadID string) ([]string, error) {
	cmd := exec.Command("bd", "dep", "list", beadID, "--json")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	type depJSON struct {
		ID   string `json:"id"`
		Type string `json:"dependency_type"`
	}
	var deps []depJSON
	if err := json.Unmarshal(output, &deps); err != nil {
		return nil, err
	}

	var ids []string
	for _, d := range deps {
		if d.Type == "blocks" {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}

// buildBeadTree takes a flat list of beads and organizes them into a tree
// based on dependency relationships. Returns the items in tree order with
// treeDepth set for each item.
func buildBeadTree(items []beadItem, dir string) []beadItem {
	if len(items) == 0 {
		return items
	}

	// Build a map of ID -> beadItem for quick lookup
	itemMap := make(map[string]*beadItem)
	for i := range items {
		itemMap[items[i].id] = &items[i]
	}

	// Fetch dependencies for items that have them
	for i := range items {
		if items[i].dependencyCount > 0 {
			deps, err := fetchDependencies(dir, items[i].id)
			if err == nil {
				items[i].dependencies = deps
			}
		}
	}

	// Build parent -> children map (issues that block -> issues they block)
	// If A blocks B, then B depends on A, so A is parent, B is child
	childrenMap := make(map[string][]string)
	for i := range items {
		for _, depID := range items[i].dependencies {
			// This item depends on depID, so depID is the parent
			childrenMap[depID] = append(childrenMap[depID], items[i].id)
		}
	}

	// Store children in each item
	for i := range items {
		items[i].children = childrenMap[items[i].id]
	}

	// Find root nodes (items with no dependencies within our set)
	roots := []string{}
	for i := range items {
		if items[i].dependencyCount == 0 {
			roots = append(roots, items[i].id)
		}
	}

	// Sort roots by priority then ID for consistent ordering
	sort.Slice(roots, func(i, j int) bool {
		a, b := itemMap[roots[i]], itemMap[roots[j]]
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		return a.id < b.id
	})

	// DFS to build tree order
	var result []beadItem
	visited := make(map[string]bool)

	var visit func(id string, depth int)
	visit = func(id string, depth int) {
		if visited[id] {
			return
		}
		visited[id] = true

		item, ok := itemMap[id]
		if !ok {
			return
		}

		item.treeDepth = depth
		result = append(result, *item)

		// Sort children by priority
		childIDs := childrenMap[id]
		sort.Slice(childIDs, func(i, j int) bool {
			a, b := itemMap[childIDs[i]], itemMap[childIDs[j]]
			if a == nil || b == nil {
				return childIDs[i] < childIDs[j]
			}
			if a.priority != b.priority {
				return a.priority < b.priority
			}
			return a.id < b.id
		})

		for _, childID := range childIDs {
			visit(childID, depth+1)
		}
	}

	// Visit all roots
	for _, rootID := range roots {
		visit(rootID, 0)
	}

	// Add any orphaned items (not reachable from roots)
	for i := range items {
		if !visited[items[i].id] {
			items[i].treeDepth = 0
			result = append(result, items[i])
		}
	}

	return result
}

// updateAddChildBead handles input for the add child bead dialog
func (m *planModel) updateAddChildBead(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		m.textInput.Blur()
		m.parentBeadID = ""
		return m, nil
	}

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> title(0)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		m.createDialogFocus = (m.createDialogFocus + 1) % 3
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else {
			m.textInput.Blur()
		}
		return m, nil
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		m.createDialogFocus--
		if m.createDialogFocus < 0 {
			m.createDialogFocus = 2
		}
		if m.createDialogFocus == 0 {
			m.textInput.Focus()
		} else {
			m.textInput.Blur()
		}
		return m, nil
	}

	// Enter submits from any field
	if msg.String() == "enter" {
		title := strings.TrimSpace(m.textInput.Value())
		if title != "" {
			m.viewMode = ViewNormal
			return m, m.createChildBead(title, beadTypes[m.createBeadType], m.createBeadPriority)
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
	}

	return m, nil
}

// updateAddToWork handles input for the add to work dialog
func (m *planModel) updateAddToWork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		m.viewMode = ViewNormal
		return m, nil
	}
	switch msg.String() {
	case "j", "down":
		if m.worksCursor < len(m.availableWorks)-1 {
			m.worksCursor++
		}
		return m, nil
	case "k", "up":
		if m.worksCursor > 0 {
			m.worksCursor--
		}
		return m, nil
	case "enter":
		if len(m.availableWorks) > 0 && m.worksCursor < len(m.availableWorks) {
			workID := m.availableWorks[m.worksCursor].id
			beadID := m.beadItems[m.beadsCursor].id
			return m, m.addBeadToWork(beadID, workID)
		}
		return m, nil
	}
	return m, nil
}

// renderAddChildBeadDialogContent renders the add child bead dialog
func (m *planModel) renderAddChildBeadDialogContent() string {
	typeFocused := m.createDialogFocus == 1
	priorityFocused := m.createDialogFocus == 2

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
	if m.createDialogFocus == 0 {
		titleLabel = tuiValueStyle.Render("Title:") + " (editing)"
	}
	if typeFocused {
		typeLabel = tuiValueStyle.Render("Type:") + " (j/k)"
	}
	if priorityFocused {
		priorityLabel = tuiValueStyle.Render("Priority:") + " (j/k)"
	}

	content := fmt.Sprintf(`  Add Child Issue to %s

  %s
  %s

  %s %s
  %s %s

  The new issue will be blocked by %s.

  [Tab] Next field  [Enter] Create  [Esc] Cancel
`, issueIDStyle.Render(m.parentBeadID), titleLabel, m.textInput.View(), typeLabel, typeDisplay, priorityLabel, priorityDisplay, m.parentBeadID)

	return tuiDialogStyle.Render(content)
}

// renderAddToWorkDialogContent renders the add to work dialog
func (m *planModel) renderAddToWorkDialogContent() string {
	beadID := ""
	if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
		beadID = m.beadItems[m.beadsCursor].id
	}

	var worksList strings.Builder
	if len(m.availableWorks) == 0 {
		worksList.WriteString("  No available works.\n")
	} else {
		for i, work := range m.availableWorks {
			prefix := "  "
			if i == m.worksCursor {
				prefix = "> "
			}
			worksList.WriteString(fmt.Sprintf("%s%s (%s) - %s\n", prefix, work.id, work.status, work.branch))
		}
	}

	content := fmt.Sprintf(`
  Add Issue to Work

  Issue: %s

  Select a work:
%s
  [Enter] Add  [j/k] Navigate  [Esc] Cancel
`, issueIDStyle.Render(beadID), worksList.String())

	return tuiDialogStyle.Render(content)
}

// worksLoadedMsg indicates available works have been loaded
type worksLoadedMsg struct {
	works []workItem
	err   error
}

// beadAddedToWorkMsg indicates a bead was added to a work
type beadAddedToWorkMsg struct {
	beadID string
	workID string
	err    error
}

// loadAvailableWorks loads the list of available works
func (m *planModel) loadAvailableWorks() tea.Cmd {
	return func() tea.Msg {
		// Empty string means no filter (all statuses)
		works, err := m.proj.DB.ListWorks(m.ctx, "")
		if err != nil {
			return worksLoadedMsg{err: err}
		}

		var items []workItem
		for _, w := range works {
			// Only show pending/processing works (not completed/failed)
			if w.Status == "pending" || w.Status == "processing" {
				items = append(items, workItem{
					id:     w.ID,
					status: w.Status,
					branch: w.BranchName,
				})
			}
		}
		return worksLoadedMsg{works: items}
	}
}

// addBeadToWork adds a bead to an existing work
func (m *planModel) addBeadToWork(beadID, workID string) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()

		// Use co work add command
		cmd := exec.Command("co", "work", "add", beadID, "--work="+workID)
		cmd.Dir = mainRepoPath
		if err := cmd.Run(); err != nil {
			return beadAddedToWorkMsg{beadID: beadID, workID: workID, err: fmt.Errorf("failed to add issue to work: %w", err)}
		}

		return beadAddedToWorkMsg{beadID: beadID, workID: workID}
	}
}

// createChildBead creates a new bead that depends on the parent bead
func (m *planModel) createChildBead(title, beadType string, priority int) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath := m.proj.MainRepoPath()
		parentID := m.parentBeadID

		// Create the new bead
		args := []string{"create", "--title=" + title, "--type=" + beadType, fmt.Sprintf("--priority=%d", priority)}
		createCmd := exec.Command("bd", args...)
		createCmd.Dir = mainRepoPath
		output, err := createCmd.Output()
		if err != nil {
			return planDataMsg{err: fmt.Errorf("failed to create issue: %w", err)}
		}

		// Parse the new bead ID from output (bd create outputs the new ID)
		newBeadID := strings.TrimSpace(string(output))
		// Handle case where output might have extra text
		if strings.Contains(newBeadID, " ") {
			parts := strings.Fields(newBeadID)
			for _, p := range parts {
				if strings.HasPrefix(p, "ac-") || strings.HasPrefix(p, "bd-") {
					newBeadID = p
					break
				}
			}
		}

		// Add dependency: new bead depends on parent (parent blocks new bead)
		if newBeadID != "" && parentID != "" {
			depCmd := exec.Command("bd", "dep", "add", newBeadID, parentID)
			depCmd.Dir = mainRepoPath
			_ = depCmd.Run() // Best effort, don't fail if dependency add fails
		}

		// Refresh after creation
		items, err := m.loadBeads()
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}
