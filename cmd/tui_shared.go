package cmd

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
)

// TUI-specific styles - shared across all TUI modes
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

	tuiActivePanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("99")).
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

// beadTypes is the list of valid bead types
var beadTypes = []string{"task", "bug", "feature"}

// statusIcon returns the icon for a given status
func statusIcon(status string) string {
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
func statusIconPlain(status string) string {
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

// statusStyled returns a styled status string
func statusStyled(status string) string {
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
