package cmd

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
)

// TUI-specific styles - shared across all TUI modes
var (
	tuiTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	tuiHotkeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214")) // Orange for hotkeys

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

	// Issue line styles
	issueIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("117")) // Light blue

	issueTreeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")) // Dim gray for tree connectors

	// Type indicator styles
	typeTaskStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75")) // Blue

	typeBugStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")) // Red

	typeFeatureStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")) // Green

	typeEpicStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("213")). // Pink/magenta
			Bold(true)

	typeChoreStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("247")) // Gray

	typeDefaultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("247")) // Gray for others
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
	ViewCreateBeadInline // Create issue inline in description area
	ViewCreateEpic
	ViewAddChildBead // Add child issue to selected issue
	ViewAddToWork    // Add issue to existing work
	ViewEditBead     // Edit selected issue
	ViewDestroyConfirm
	ViewCloseBeadConfirm
	ViewPlanDialog
	ViewAssignBeads
	ViewBeadSearch
	ViewLabelFilter
	ViewHelp
)

// workItem represents a work unit for selection
type workItem struct {
	id     string
	status string
	branch string
}

// beadItem represents a bead in the beads panel
type beadItem struct {
	id              string
	title           string
	status          string
	priority        int
	beadType        string   // task, bug, feature, etc.
	description     string
	isReady         bool
	selected        bool     // for multi-select
	dependencyCount int      // number of issues that block this one
	dependentCount  int      // number of issues this blocks
	dependencies    []string // IDs of issues that block this one
	children        []string // IDs of issues blocked by this one (computed from tree)
	treeDepth         int      // depth in tree view (0 = root)
	assignedWorkID    string   // work ID if already assigned to a work (empty = not assigned)
	isClosedParent    bool     // true if this is a closed bead included for tree context (has visible children)
	isLastChild       bool     // true if this bead is the last child of its parent
	treePrefixPattern string   // precomputed tree prefix pattern (e.g., "│ └─")
}

// beadFilters holds the current filter state for beads
type beadFilters struct {
	status     string // "open", "closed", "ready"
	label      string // filter by label (empty = no filter)
	searchText string // fuzzy search text
	sortBy     string // "default", "priority", "created", "title"
}

// beadTypes is the list of valid bead types
var beadTypes = []string{
	"task",
	"bug",
	"feature",
	"epic",
	"chore",
	"merge-request",
	"molecule",
	"gate",
	"agent",
	"role",
	"rig",
	"convoy",
	"event",
}

// statusIcon returns the icon for a given status
func statusIcon(status string) string {
	switch status {
	// Internal db statuses
	case db.StatusPending:
		return statusPending.Render("○")
	case db.StatusProcessing:
		return statusProcessing.Render("●")
	case db.StatusCompleted:
		return statusCompleted.Render("✓")
	case db.StatusFailed:
		return statusFailed.Render("✗")
	// Bead statuses from bd CLI
	case "open":
		return statusPending.Render("○")
	case "in_progress":
		return statusProcessing.Render("●")
	case "blocked":
		return statusFailed.Render("◐")
	case "deferred":
		return statusPending.Render("❄")
	case "closed":
		return statusCompleted.Render("✓")
	default:
		return "?"
	}
}

// statusIconPlain returns the icon without styling (for use in selected items)
func statusIconPlain(status string) string {
	switch status {
	// Internal db statuses
	case db.StatusPending:
		return "○"
	case db.StatusProcessing:
		return "●"
	case db.StatusCompleted:
		return "✓"
	case db.StatusFailed:
		return "✗"
	// Bead statuses from bd CLI
	case "open":
		return "○"
	case "in_progress":
		return "●"
	case "blocked":
		return "◐"
	case "deferred":
		return "❄"
	case "closed":
		return "✓"
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

// styleHotkeys styles text with hotkeys like "[c]reate [d]elete" by coloring the keys
// The keys inside brackets are rendered with tuiHotkeyStyle
func styleHotkeys(text string) string {
	var result strings.Builder
	i := 0
	for i < len(text) {
		if text[i] == '[' {
			// Find the closing bracket
			end := i + 1
			for end < len(text) && text[end] != ']' {
				end++
			}
			if end < len(text) {
				// Found a complete [key] sequence
				key := text[i+1 : end]
				result.WriteString("[")
				result.WriteString(tuiHotkeyStyle.Render(key))
				result.WriteString("]")
				i = end + 1
				continue
			}
		}
		result.WriteByte(text[i])
		i++
	}
	return result.String()
}

// styleButtonWithHover styles a button with hover effect if hovered is true
// This is used for clickable buttons and mode tabs in the TUI
func styleButtonWithHover(text string, hovered bool) string {
	hoverStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).   // Black text
		Background(lipgloss.Color("214")). // Orange background
		Bold(true)

	if hovered {
		return hoverStyle.Render(text)
	}
	return styleHotkeys(text)
}
