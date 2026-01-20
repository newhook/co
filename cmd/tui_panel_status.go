package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	s.statusMessage = message
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

	// Commands plain text for width calculation
	commandsPlain := fmt.Sprintf("[n]New [e]Edit [a]Child [x]Close [w]Work [A]dd [i]Import %s [?]Help", pAction)

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

	// Build bar with commands left, status right
	padding := max(s.width-len(commandsPlain)-len(statusPlain)-4, 2)
	return tuiStatusBarStyle.Width(s.width).Render(commands + strings.Repeat(" ", padding) + status)
}

// DetectButton determines which button is at the given X position
func (s *StatusBar) DetectButton(x int) string {
	// Account for the status bar's left padding (tuiStatusBarStyle has Padding(0, 1))
	if x < 1 {
		return ""
	}
	x = x - 1

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

// ClearStatus clears the status message
func (s *StatusBar) ClearStatus() {
	s.statusMessage = ""
	s.statusIsError = false
}
