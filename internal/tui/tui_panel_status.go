package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// StatusBarContext indicates which panel the status bar should show commands for
type StatusBarContext int

const (
	StatusBarContextIssues StatusBarContext = iota
	StatusBarContextWorkDetail
)

// StatusBar is the status bar panel at the bottom of the TUI.
// It renders command buttons, status messages, and handles hover/click detection.
type StatusBar struct {
	// Dimensions
	width int

	// State
	statusMessage string
	statusIsError bool
	loading       bool
	lastUpdate    time.Time
	spinner       spinner.Model

	// Context determines which commands to show
	context StatusBarContext

	// Mouse state
	hoveredButton string

	// Data providers (set by coordinator)
	getBeadItems       func() []beadItem
	getBeadsCursor     func() int
	getActiveSessions  func() map[string]bool
	getViewMode        func() ViewMode
	getTextInput       func() string
}

// NewStatusBar creates a new StatusBar panel
func NewStatusBar() *StatusBar {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	return &StatusBar{
		width:   80,
		spinner: s,
	}
}

// SetSize updates the panel dimensions
func (s *StatusBar) SetSize(width int) {
	s.width = width
}

// SetDataProviders sets the functions to get data from the coordinator
func (s *StatusBar) SetDataProviders(
	getBeadItems func() []beadItem,
	getBeadsCursor func() int,
	getActiveSessions func() map[string]bool,
	getViewMode func() ViewMode,
	getTextInput func() string,
) {
	s.getBeadItems = getBeadItems
	s.getBeadsCursor = getBeadsCursor
	s.getActiveSessions = getActiveSessions
	s.getViewMode = getViewMode
	s.getTextInput = getTextInput
}

// SetStatus updates the status message
func (s *StatusBar) SetStatus(message string, isError bool) {
	// Strip newlines - status bar is single line only
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.ReplaceAll(message, "\r", "")
	s.statusMessage = strings.TrimSpace(message)
	s.statusIsError = isError
}

// SetLoading updates the loading state
func (s *StatusBar) SetLoading(loading bool) {
	s.loading = loading
}

// SetLastUpdate records when data was last refreshed
func (s *StatusBar) SetLastUpdate(t time.Time) {
	s.lastUpdate = t
}

// SetHoveredButton updates which button is hovered
func (s *StatusBar) SetHoveredButton(button string) {
	s.hoveredButton = button
}

// SetContext updates the status bar context (which panel's commands to show)
func (s *StatusBar) SetContext(ctx StatusBarContext) {
	s.context = ctx
}

// GetHoveredButton returns which button is currently hovered
func (s *StatusBar) GetHoveredButton() string {
	return s.hoveredButton
}

// UpdateSpinner updates the spinner animation
func (s *StatusBar) UpdateSpinner(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	s.spinner, cmd = s.spinner.Update(msg)
	return cmd
}

// Render returns the status bar content
func (s *StatusBar) Render() string {
	// If in search mode, show vim-style inline search bar
	if s.getViewMode != nil && s.getViewMode() == ViewBeadSearch {
		searchPrompt := "/"
		searchInput := ""
		if s.getTextInput != nil {
			searchInput = s.getTextInput()
		}
		hint := tuiDimStyle.Render("  [Enter]Search  [Esc]Cancel")
		return tuiStatusBarStyle.Width(s.width).Render(searchPrompt + searchInput + hint)
	}

	var commands string
	var commandsPlain string

	switch s.context {
	case StatusBarContextWorkDetail:
		// Work detail commands
		commands, commandsPlain = s.renderWorkDetailCommands()
	default:
		// Issues commands (default)
		commands, commandsPlain = s.renderIssuesCommands()
	}

	// Status on the right
	var status string
	var statusPlain string
	if s.statusMessage != "" {
		statusPlain = s.statusMessage
		if s.statusIsError {
			status = tuiErrorStyle.Render(s.statusMessage)
		} else {
			status = tuiSuccessStyle.Render(s.statusMessage)
		}
	} else if s.loading {
		statusPlain = "Loading..."
		status = s.spinner.View() + " Loading..."
	} else {
		statusPlain = fmt.Sprintf("Updated: %s", s.lastUpdate.Format("15:04:05"))
		status = tuiDimStyle.Render(statusPlain)
	}

	// Calculate available space for status message and truncate if needed
	// Inner content width = s.width - 2 (status bar has Padding(0,1) = 1 on each side)
	// Content = commands + minPadding + status
	minPadding := 2
	innerWidth := s.width - 2
	commandsWidth := ansi.StringWidth(commandsPlain)
	statusWidth := ansi.StringWidth(statusPlain)

	// Available width for status = inner width minus commands and minimum padding
	availableWidth := max(innerWidth-commandsWidth-minPadding, 0)

	// Truncate status if needed
	if statusWidth > availableWidth {
		if availableWidth <= 3 {
			// Not enough room for status, hide it
			status = ""
			statusPlain = ""
			statusWidth = 0
		} else {
			// Truncate with ellipsis
			truncatedPlain := ansi.Truncate(statusPlain, availableWidth, "...")
			statusPlain = truncatedPlain
			statusWidth = ansi.StringWidth(statusPlain)
			if s.statusIsError {
				status = tuiErrorStyle.Render(truncatedPlain)
			} else if s.loading {
				status = s.spinner.View() + " Loading..."
			} else if s.statusMessage != "" {
				status = tuiSuccessStyle.Render(truncatedPlain)
			} else {
				status = tuiDimStyle.Render(truncatedPlain)
			}
		}
	}

	// Build bar with commands left, status right
	// Padding fills the remaining space
	padding := max(innerWidth-commandsWidth-statusWidth, minPadding)
	return tuiStatusBarStyle.Width(s.width).Render(commands + strings.Repeat(" ", padding) + status)
}

// renderIssuesCommands returns commands for the issues panel
func (s *StatusBar) renderIssuesCommands() (string, string) {
	// Show p action based on session state
	pAction := "[p]Plan"
	if s.getBeadItems != nil && s.getBeadsCursor != nil && s.getActiveSessions != nil {
		beadItems := s.getBeadItems()
		cursor := s.getBeadsCursor()
		activeSessions := s.getActiveSessions()
		if len(beadItems) > 0 && cursor < len(beadItems) {
			beadID := beadItems[cursor].ID
			if activeSessions[beadID] {
				pAction = "[p]Resume"
			}
		}
	}

	// Commands on the left with hover effects
	nButton := styleButtonWithHover("[n]New", s.hoveredButton == "n")
	eButton := styleButtonWithHover("[e]Edit", s.hoveredButton == "e")
	aButton := styleButtonWithHover("[a]Child", s.hoveredButton == "a")
	xButton := styleButtonWithHover("[x]Close", s.hoveredButton == "x")
	wButton := styleButtonWithHover("[w]Work", s.hoveredButton == "w")
	AButton := styleButtonWithHover("[A]dd", s.hoveredButton == "A")
	iButton := styleButtonWithHover("[i]Import", s.hoveredButton == "i")
	pButton := styleButtonWithHover(pAction, s.hoveredButton == "p")
	helpButton := styleButtonWithHover("[?]Help", s.hoveredButton == "?")

	commands := nButton + " " + eButton + " " + aButton + " " + xButton + " " + wButton + " " + AButton + " " + iButton + " " + pButton + " " + helpButton
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work [A]dd [i]Import %s [?]Help", pAction)

	return commands, commandsPlain
}

// renderWorkDetailCommands returns commands for the work detail panel
func (s *StatusBar) renderWorkDetailCommands() (string, string) {
	// Work detail specific commands
	tButton := styleButtonWithHover("[t]erminal", s.hoveredButton == "t")
	cButton := styleButtonWithHover("[c]laude", s.hoveredButton == "c")
	rButton := styleButtonWithHover("[r]un", s.hoveredButton == "r")
	oButton := styleButtonWithHover("[o]rch", s.hoveredButton == "o")
	vButton := styleButtonWithHover("[v]review", s.hoveredButton == "v")
	pButton := styleButtonWithHover("[p]r", s.hoveredButton == "p")
	fButton := styleButtonWithHover("[f]eedback", s.hoveredButton == "f")
	dButton := styleButtonWithHover("[d]estroy", s.hoveredButton == "d")
	escButton := styleButtonWithHover("[Esc]Deselect", s.hoveredButton == "esc")
	helpButton := styleButtonWithHover("[?]Help", s.hoveredButton == "?")

	commands := tButton + " " + cButton + " " + rButton + " " + oButton + " " + vButton + " " + pButton + " " + fButton + " " + dButton + " " + escButton + " " + helpButton
	commandsPlain := "[t]erminal [c]laude [r]un [o]rch [v]review [p]r [f]eedback [d]estroy [Esc]Deselect [?]Help"

	return commands, commandsPlain
}

// DetectButton determines which button is at the given X position
func (s *StatusBar) DetectButton(x int) string {
	// Account for the status bar's left padding (tuiStatusBarStyle has Padding(0, 1))
	if x < 1 {
		return ""
	}
	x = x - 1

	switch s.context {
	case StatusBarContextWorkDetail:
		return s.detectWorkDetailButton(x)
	default:
		return s.detectIssuesButton(x)
	}
}

// detectIssuesButton detects button clicks for the issues panel
func (s *StatusBar) detectIssuesButton(x int) string {
	// Get the plain text version of the commands
	pAction := "[p]Plan"
	if s.getBeadItems != nil && s.getBeadsCursor != nil && s.getActiveSessions != nil {
		beadItems := s.getBeadItems()
		cursor := s.getBeadsCursor()
		activeSessions := s.getActiveSessions()
		if len(beadItems) > 0 && cursor < len(beadItems) {
			beadID := beadItems[cursor].ID
			if activeSessions[beadID] {
				pAction = "[p]Resume"
			}
		}
	}
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work [A]dd [i]Import %s [?]Help", pAction)

	// Find positions of each button
	nIdx := strings.Index(commandsPlain, "[n]New")
	eIdx := strings.Index(commandsPlain, "[e]Edit")
	aIdx := strings.Index(commandsPlain, "[a]Child")
	xIdx := strings.Index(commandsPlain, "[x]Close")
	wIdx := strings.Index(commandsPlain, "[w]Work")
	AIdx := strings.Index(commandsPlain, "[A]dd")
	iIdx := strings.Index(commandsPlain, "[i]Import")
	pIdx := strings.Index(commandsPlain, pAction)
	helpIdx := strings.Index(commandsPlain, "[?]Help")

	// Check if mouse is over any button
	if nIdx >= 0 && x >= nIdx && x < nIdx+len("[n]New") {
		return "n"
	}
	if eIdx >= 0 && x >= eIdx && x < eIdx+len("[e]Edit") {
		return "e"
	}
	if aIdx >= 0 && x >= aIdx && x < aIdx+len("[a]Child") {
		return "a"
	}
	if xIdx >= 0 && x >= xIdx && x < xIdx+len("[x]Close") {
		return "x"
	}
	if wIdx >= 0 && x >= wIdx && x < wIdx+len("[w]Work") {
		return "w"
	}
	if AIdx >= 0 && x >= AIdx && x < AIdx+len("[A]dd") {
		return "A"
	}
	if iIdx >= 0 && x >= iIdx && x < iIdx+len("[i]Import") {
		return "i"
	}
	if pIdx >= 0 && x >= pIdx && x < pIdx+len(pAction) {
		return "p"
	}
	if helpIdx >= 0 && x >= helpIdx && x < helpIdx+len("[?]Help") {
		return "?"
	}

	return ""
}

// detectWorkDetailButton detects button clicks for the work detail panel
func (s *StatusBar) detectWorkDetailButton(x int) string {
	commandsPlain := "[t]erminal [c]laude [r]un [o]rch [v]review [p]r [f]eedback [d]estroy [Esc]Deselect [?]Help"

	tIdx := strings.Index(commandsPlain, "[t]erminal")
	cIdx := strings.Index(commandsPlain, "[c]laude")
	rIdx := strings.Index(commandsPlain, "[r]un")
	oIdx := strings.Index(commandsPlain, "[o]rch")
	vIdx := strings.Index(commandsPlain, "[v]review")
	pIdx := strings.Index(commandsPlain, "[p]r")
	fIdx := strings.Index(commandsPlain, "[f]eedback")
	dIdx := strings.Index(commandsPlain, "[d]estroy")
	escIdx := strings.Index(commandsPlain, "[Esc]Deselect")
	helpIdx := strings.Index(commandsPlain, "[?]Help")

	if tIdx >= 0 && x >= tIdx && x < tIdx+len("[t]erminal") {
		return "t"
	}
	if cIdx >= 0 && x >= cIdx && x < cIdx+len("[c]laude") {
		return "c"
	}
	if rIdx >= 0 && x >= rIdx && x < rIdx+len("[r]un") {
		return "r"
	}
	if oIdx >= 0 && x >= oIdx && x < oIdx+len("[o]rch") {
		return "o"
	}
	if vIdx >= 0 && x >= vIdx && x < vIdx+len("[v]review") {
		return "v"
	}
	if pIdx >= 0 && x >= pIdx && x < pIdx+len("[p]r") {
		return "p"
	}
	if fIdx >= 0 && x >= fIdx && x < fIdx+len("[f]eedback") {
		return "f"
	}
	if dIdx >= 0 && x >= dIdx && x < dIdx+len("[d]estroy") {
		return "d"
	}
	if escIdx >= 0 && x >= escIdx && x < escIdx+len("[Esc]Deselect") {
		return "esc"
	}
	if helpIdx >= 0 && x >= helpIdx && x < helpIdx+len("[?]Help") {
		return "?"
	}

	return ""
}

// ClearStatus clears the status message
func (s *StatusBar) ClearStatus() {
	s.statusMessage = ""
	s.statusIsError = false
}
