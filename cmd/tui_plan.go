package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/beads/watcher"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/zellij"
)


// watcherEventMsg wraps watcher events for tea.Msg
type watcherEventMsg watcher.WatcherEvent

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
	createBeadType     int            // 0=task, 1=bug, 2=feature
	createBeadPriority int            // 0-4, default 2
	createDialogFocus  int            // 0=title, 1=type, 2=priority, 3=description
	createDescTextarea textarea.Model // Textarea for description

	// Add child bead state
	parentBeadID string // ID of parent when adding child

	// Edit bead state (editBeadID is set when editing, uses shared form fields)
	editBeadID string // ID of bead being edited

	// Create work dialog state
	createWorkBeadIDs   []string        // Bead IDs for work creation (supports multi-select)
	createWorkBranch    textinput.Model // Editable branch name
	createWorkField     int             // 0=branch, 1=buttons
	createWorkButtonIdx int             // 0=Execute, 1=Auto, 2=Cancel

	// Add to work state
	availableWorks []workItem // List of works to choose from
	worksCursor    int        // Cursor position in works list

	// Multi-select state
	selectedBeads map[string]bool // beadID -> is selected

	// Loading state
	loading bool

	// Search sequence tracking to handle async refresh race conditions
	searchSeq uint64 // Incremented on each search change

	// Per-bead session tracking
	activeBeadSessions map[string]bool // beadID -> has active session
	zj                 *zellij.Client

	// Two-column layout settings
	columnRatio float64 // Ratio of issues column width (0.0-1.0), default 0.4 for 40/60 split

	// Mouse state
	mouseX              int
	mouseY              int
	hoveredButton       string // which button is hovered ("n", "e", "w", "p", etc.)
	hoveredIssue        int    // index of hovered issue, -1 if none
	hoveredDialogButton string // which dialog button is hovered ("ok", "cancel")

	// Linear import state
	linearImportInput      textinput.Model // Input for Linear issue ID/URL
	linearImportCreateDeps bool            // Whether to create dependencies
	linearImportUpdate     bool            // Whether to update existing beads
	linearImportDryRun     bool            // Dry run mode
	linearImportMaxDepth   int             // Max dependency depth
	linearImportFocus      int             // 0=input, 1=createDeps, 2=update, 3=dryRun, 4=maxDepth
	linearImporting        bool            // Whether import is in progress

	// Database watcher for cache invalidation
	beadsWatcher *watcher.Watcher
	beadsClient  *beads.Client
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

	createDescTa := textarea.New()
	createDescTa.Placeholder = "Enter description (optional)..."
	createDescTa.CharLimit = 2000
	createDescTa.SetWidth(60)
	createDescTa.SetHeight(4)

	branchInput := textinput.New()
	branchInput.Placeholder = "Branch name..."
	branchInput.CharLimit = 100
	branchInput.Width = 60

	linearInput := textinput.New()
	linearInput.Placeholder = "Enter Linear issue ID or URL (e.g., ENG-123 or https://linear.app/...)"
	linearInput.CharLimit = 200
	linearInput.Width = 60

	// Initialize beads database client and watcher
	beadsDBPath := filepath.Join(proj.Root, "main", ".beads", "beads.db")
	beadsClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
	if err != nil {
		// Log error but continue without cache - fallback to CLI-based approach
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize beads client: %v\n", err)
		beadsClient = nil
	}

	beadsWatcher, err := watcher.New(watcher.DefaultConfig(beadsDBPath))
	if err != nil {
		// Log error but continue without watcher
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize beads watcher: %v\n", err)
		beadsWatcher = nil
	} else {
		if err := beadsWatcher.Start(); err != nil {
			// Log error and disable watcher
			fmt.Fprintf(os.Stderr, "Warning: Failed to start beads watcher: %v\n", err)
			beadsWatcher = nil
			// Close beadsClient to prevent resource leak
			if beadsClient != nil {
				beadsClient.Close()
				beadsClient = nil
			}
		}
	}

	return &planModel{
		createDescTextarea:   createDescTa,
		createWorkBranch:     branchInput,
		linearImportInput:    linearInput,
		linearImportMaxDepth: 2, // Default max depth
		ctx:                  ctx,
		proj:                 proj,
		width:                80,
		height:               24,
		activePanel:          PanelLeft,
		spinner:              s,
		textInput:            ti,
		activeBeadSessions:   make(map[string]bool),
		selectedBeads:        make(map[string]bool),
		createBeadPriority:   2,
		zj:                   zellij.New(),
		columnRatio:          0.4, // Default 40/60 split (issues/details)
		hoveredIssue:         -1,  // No issue hovered initially
		beadsWatcher:         beadsWatcher,
		beadsClient:          beadsClient,
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
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.refreshData(),
	}

	// Subscribe to watcher events if watcher is available
	if m.beadsWatcher != nil {
		cmds = append(cmds, m.waitForWatcherEvent())
	}

	return tea.Batch(cmds...)
}

// waitForWatcherEvent waits for a watcher event and returns it as a tea.Msg
func (m *planModel) waitForWatcherEvent() tea.Cmd {
	if m.beadsWatcher == nil {
		return nil
	}

	return func() tea.Msg {
		sub := m.beadsWatcher.Broker().Subscribe(m.ctx)

		evt, ok := <-sub
		if !ok {
			return nil
		}

		return watcherEventMsg(evt.Payload)
	}
}

// Update implements tea.Model
func (m *planModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case watcherEventMsg:
		// Handle watcher events
		if msg.Type == watcher.DBChanged {
			// Flush cache and trigger data reload
			if m.beadsClient != nil {
				m.beadsClient.FlushCache(m.ctx)
			}
			// Trigger data reload and wait for next watcher event
			return m, tea.Batch(m.refreshData(), m.waitForWatcherEvent())
		} else if msg.Type == watcher.WatcherError {
			// Log error and continue waiting for events
			return m, m.waitForWatcherEvent()
		}
		// Continue waiting for next event
		return m, m.waitForWatcherEvent()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.MouseMsg:
		m.mouseX = msg.X
		m.mouseY = msg.Y

		// Calculate status bar Y position (at bottom of view)
		statusBarY := m.height - 1

		// Handle hover detection for motion events
		if msg.Action == tea.MouseActionMotion {
			if msg.Y == statusBarY {
				m.hoveredButton = m.detectCommandsBarButton(msg.X)
				m.hoveredIssue = -1
				m.hoveredDialogButton = ""
			} else {
				m.hoveredButton = ""
				// Detect hover over dialog buttons if in form mode
				m.hoveredDialogButton = m.detectDialogButton(msg.X, msg.Y)
				if m.hoveredDialogButton != "" {
					m.hoveredIssue = -1
				} else {
					// Detect hover over issue lines
					m.hoveredIssue = m.detectHoveredIssue(msg.Y)
				}
			}
			return m, nil
		}

		// Handle clicks on status bar buttons
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.Y == statusBarY {
				clickedButton := m.detectCommandsBarButton(msg.X)
				// Trigger the corresponding action by simulating a key press
				switch clickedButton {
				case "n":
					return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
				case "e":
					return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
				case "a":
					return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
				case "x":
					return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
				case "A":
					return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
				case "p":
					return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
				case "?":
					return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
				}
			} else {
				// Check if clicking on dialog buttons
				clickedDialogButton := m.detectDialogButton(msg.X, msg.Y)
				if clickedDialogButton == "ok" {
					// Submit the form
					return m.submitBeadForm()
				} else if clickedDialogButton == "cancel" {
					// Cancel the form
					m.viewMode = ViewNormal
					m.textInput.Blur()
					m.createDescTextarea.Blur()
					m.editBeadID = ""
					m.parentBeadID = ""
					return m, nil
				}

				// Check if clicking on an issue
				clickedIssue := m.detectHoveredIssue(msg.Y)
				if clickedIssue >= 0 && clickedIssue < len(m.beadItems) {
					m.beadsCursor = clickedIssue
				}
			}
		}
		return m, nil

	case planDataMsg:
		// Ignore stale search results from older requests
		if msg.searchSeq < m.searchSeq {
			return m, nil
		}
		m.beadItems = msg.beads
		if msg.activeSessions != nil {
			m.activeBeadSessions = msg.activeSessions
		}
		m.loading = false
		m.lastUpdate = time.Now()
		if msg.err != nil {
			m.statusMessage = msg.err.Error()
			m.statusIsError = true
		}
		// Don't clear status message on success - let it persist until next action
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

	case linearImportCompleteMsg:
		m.linearImporting = false
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Import failed: %v", msg.err)
			m.statusIsError = true
		} else if msg.skipReason != "" {
			// Issue was already imported or skipped
			if len(msg.beadIDs) == 1 {
				m.statusMessage = fmt.Sprintf("%s: %s", msg.skipReason, msg.beadIDs[0])
			} else {
				m.statusMessage = msg.skipReason
			}
			m.statusIsError = false
		} else {
			if len(msg.beadIDs) == 1 {
				m.statusMessage = fmt.Sprintf("Successfully imported %s", msg.beadIDs[0])
			} else {
				m.statusMessage = fmt.Sprintf("Successfully imported %d issues", len(msg.beadIDs))
			}
			m.statusIsError = false
		}
		return m, tea.Batch(m.refreshData(), clearStatusAfter(5*time.Second))

	case linearImportProgressMsg:
		if msg.total > 0 {
			m.statusMessage = fmt.Sprintf("Importing... [%d/%d] %s", msg.current, msg.total, msg.message)
		} else {
			m.statusMessage = msg.message
		}
		m.statusIsError = false
		return m, nil

	case statusClearMsg:
		m.statusMessage = ""
		m.statusIsError = false
		return m, nil

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
	searchSeq      uint64 // Sequence number to detect stale results
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

// editorFinishedMsg is sent when the external editor closes
type editorFinishedMsg struct{}

// linearImportCompleteMsg is sent when a Linear import completes
type linearImportCompleteMsg struct {
	beadIDs    []string // IDs of imported beads
	err        error
	skipReason string // e.g., "already imported"
}

// linearImportProgressMsg is sent to update Linear import progress
type linearImportProgressMsg struct {
	current int
	total   int
	message string
}

// statusClearMsg is sent to clear the status message after a delay
type statusClearMsg struct{}

// clearStatusAfter returns a command that clears the status after the given duration
func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return statusClearMsg{}
	})
}

func (m *planModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle dialog-specific input
	switch m.viewMode {
	case ViewCreateBead, ViewCreateBeadInline, ViewAddChildBead, ViewEditBead:
		// All bead form dialogs use the unified handler
		return m.updateBeadForm(msg)
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
	case ViewLinearImportInline:
		return m.updateLinearImportInline(msg)
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
		// Create new bead inline
		m.viewMode = ViewCreateBeadInline
		m.textInput.Reset()
		m.textInput.Focus()
		m.createBeadType = 0
		m.createBeadPriority = 2
		m.createDialogFocus = 0 // Start with title focused
		m.createDescTextarea.Reset()
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

	case "[":
		// Decrease column ratio (make issues column narrower)
		if m.columnRatio > 0.3 {
			m.columnRatio -= 0.1
			if m.columnRatio < 0.3 {
				m.columnRatio = 0.3
			}
		}
		return m, nil

	case "]":
		// Increase column ratio (make issues column wider)
		if m.columnRatio < 0.5 {
			m.columnRatio += 0.1
			if m.columnRatio > 0.5 {
				m.columnRatio = 0.5
			}
		}
		return m, nil

	case " ":
		// Toggle bead selection for multi-select
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			bead := m.beadItems[m.beadsCursor]
			// Prevent selecting already-assigned beads
			if bead.assignedWorkID != "" {
				m.statusMessage = fmt.Sprintf("Cannot select: already assigned to %s", bead.assignedWorkID)
				m.statusIsError = true
				return m, nil
			}
			m.selectedBeads[bead.id] = !m.selectedBeads[bead.id]
		}
		return m, nil

	case "p":
		// Spawn/resume planning session for selected bead
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			beadID := m.beadItems[m.beadsCursor].id
			return m, m.spawnPlanSession(beadID)
		}
		return m, nil

	case "A":
		// Create work from selected bead(s) - show dialog
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			// Collect selected beads, or use cursor bead if none selected
			var selectedIDs []string
			var branchBeads []*beadsForBranch
			var alreadyAssigned []string
			for _, item := range m.beadItems {
				if m.selectedBeads[item.id] {
					if item.assignedWorkID != "" {
						alreadyAssigned = append(alreadyAssigned, item.id+" ("+item.assignedWorkID+")")
					} else {
						selectedIDs = append(selectedIDs, item.id)
						branchBeads = append(branchBeads, &beadsForBranch{
							ID:    item.id,
							Title: item.title,
						})
					}
				}
			}
			// If no beads selected, use the cursor bead
			if len(selectedIDs) == 0 && len(alreadyAssigned) == 0 {
				bead := m.beadItems[m.beadsCursor]
				if bead.assignedWorkID != "" {
					m.statusMessage = fmt.Sprintf("Cannot create work: %s already assigned to %s", bead.id, bead.assignedWorkID)
					m.statusIsError = true
					return m, nil
				}
				selectedIDs = []string{bead.id}
				branchBeads = []*beadsForBranch{{
					ID:    bead.id,
					Title: bead.title,
				}}
			}
			// Show error if some selected beads are already assigned
			if len(alreadyAssigned) > 0 {
				m.statusMessage = fmt.Sprintf("Skipped already-assigned: %s", strings.Join(alreadyAssigned, ", "))
				m.statusIsError = true
				// If all beads were assigned, abort
				if len(selectedIDs) == 0 {
					m.statusMessage = "All selected beads are already assigned to works"
					return m, nil
				}
			}
			m.createWorkBeadIDs = selectedIDs
			// Generate proposed branch name from all selected beads
			branchName := generateBranchNameFromBeadsForBranch(branchBeads)
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
		// Edit selected issue using the unified bead form
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			bead := m.beadItems[m.beadsCursor]
			m.editBeadID = bead.id
			m.viewMode = ViewEditBead
			m.textInput.Reset()
			m.textInput.SetValue(bead.title)
			m.textInput.Focus()
			m.createDescTextarea.Reset()
			m.createDescTextarea.SetValue(bead.description)
			// Find the type index
			m.createBeadType = 0
			for i, t := range beadTypes {
				if t == bead.beadType {
					m.createBeadType = i
					break
				}
			}
			m.createBeadPriority = bead.priority
			m.createDialogFocus = 0 // Start with title focused
		}
		return m, nil

	case "E":
		// Edit selected issue in external editor
		if len(m.beadItems) > 0 && m.beadsCursor < len(m.beadItems) {
			bead := m.beadItems[m.beadsCursor]
			return m, m.openInEditor(bead.id)
		}
		return m, nil

	case "i":
		// Import Linear issue inline - check for API key first
		apiKey := os.Getenv("LINEAR_API_KEY")
		if apiKey == "" && m.proj.Config != nil {
			apiKey = m.proj.Config.Linear.APIKey
		}
		if apiKey == "" {
			m.statusMessage = "Linear API key not configured (set LINEAR_API_KEY env var or [linear] api_key in config.toml)"
			m.statusIsError = true
			return m, nil
		}
		m.viewMode = ViewLinearImportInline
		m.linearImportInput.Reset()
		m.linearImportInput.Focus()
		m.linearImportFocus = 0
		m.linearImportCreateDeps = false
		m.linearImportUpdate = false
		m.linearImportDryRun = false
		m.linearImportMaxDepth = 2
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
		// Clean up resources before quitting
		m.cleanup()
		return m, tea.Quit
	}

	return m, nil
}

// cleanup releases resources when the TUI exits
func (m *planModel) cleanup() {
	// Stop the beads watcher if it's running
	if m.beadsWatcher != nil {
		_ = m.beadsWatcher.Stop()
	}
	// Close the beads client to release database connections
	if m.beadsClient != nil {
		_ = m.beadsClient.Close()
	}
}

// View implements tea.Model
func (m *planModel) View() string {
	// Handle dialogs
	switch m.viewMode {
	case ViewCreateBead, ViewCreateBeadInline, ViewAddChildBead, ViewEditBead:
		// All bead form modes render inline in the details panel
		// Fall through to normal rendering
	case ViewCreateWork:
		return m.renderWithDialog(m.renderCreateWorkDialogContent())
	case ViewAddToWork:
		return m.renderWithDialog(m.renderAddToWorkDialogContent())
	case ViewBeadSearch:
		// Inline search mode - render normal view with search bar in status area
		// Fall through to normal rendering
	case ViewLabelFilter:
		return m.renderWithDialog(m.renderLabelFilterDialogContent())
	case ViewCloseBeadConfirm:
		return m.renderWithDialog(m.renderCloseBeadConfirmContent())
	case ViewLinearImportInline:
		// Inline import mode - render normal view with import form in details area
		// Fall through to normal rendering
	case ViewHelp:
		return m.renderHelp()
	}

	// Use two-column layout
	content := m.renderTwoColumnLayout()
	statusBar := m.renderCommandsBar()

	// Combine content and status bar
	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

// beadsForBranch is a minimal struct for branch name generation
type beadsForBranch struct {
	ID    string
	Title string
}

// generateBranchNameFromBeadsForBranch generates a branch name from beads
func generateBranchNameFromBeadsForBranch(beads []*beadsForBranch) string {
	if len(beads) == 0 {
		return ""
	}
	// Use the same logic as generateBranchNameFromBeads but with local struct
	var titles []string
	for _, b := range beads {
		titles = append(titles, b.Title)
	}
	combined := strings.Join(titles, " ")
	// Sanitize for branch name
	combined = strings.ToLower(combined)
	combined = strings.ReplaceAll(combined, " ", "-")
	// Remove special characters
	var result strings.Builder
	for _, c := range combined {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result.WriteRune(c)
		}
	}
	branchName := result.String()
	// Limit length
	if len(branchName) > 50 {
		branchName = branchName[:50]
	}
	// Remove trailing dashes
	branchName = strings.TrimRight(branchName, "-")
	return "feat/" + branchName
}
