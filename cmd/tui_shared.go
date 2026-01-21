package cmd

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/beads"
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
			Foreground(lipgloss.Color("214")) // Orange

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

	// New bead animation style
	tuiNewBeadStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFF00")). // Bright yellow for newly created beads
			Bold(true)
)

// Panel represents which panel is currently focused
type Panel int

const (
	PanelLeft        Panel = iota // Left panel (issues)
	PanelMiddle                   // Middle panel at current depth (used by tui.go)
	PanelRight                    // Right panel (details/forms)
	PanelWorkDetails              // Work details in split view
	PanelWorkOverlay              // Work overlay tiles
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
	ViewEditBead     // Edit selected issue
	ViewDestroyConfirm
	ViewCloseBeadConfirm
	ViewPlanDialog
	ViewAssignBeads
	ViewBeadSearch
	ViewLabelFilter
	ViewLinearImportInline // Import from Linear (inline in details panel)
	ViewHelp
	ViewWorkOverlay // Work overlay system showing work tiles
)

// workItem represents a work unit for selection
type workItem struct {
	id             string
	status         string
	branch         string
	rootIssueID    string
	rootIssueTitle string
}

// beadItem represents a bead in the beads panel with TUI-specific display state.
// It embeds beads.BeadWithDeps to access domain data directly.
type beadItem struct {
	*beads.BeadWithDeps

	// TUI-specific display state
	selected          bool     // for multi-select
	isReady           bool     // computed ready state
	treeDepth         int      // depth in tree view (0 = root)
	assignedWorkID    string   // work ID if already assigned to a work (empty = not assigned)
	isClosedParent    bool     // true if this is a closed bead included for tree context (has visible children)
	isLastChild       bool     // true if this bead is the last child of its parent
	treePrefixPattern string   // precomputed tree prefix pattern (e.g., "│ └─")
	children          []string // IDs of issues blocked by this one (computed from tree)
}

// beadFilters holds the current filter state for beads
type beadFilters struct {
	status     string // "open", "closed", "ready"
	label      string // filter by label (empty = no filter)
	searchText string // fuzzy search text
	sortBy     string // "default", "priority", "created", "title"

	// Entity-based filters (override status filter when set)
	task     string // task ID - show beads assigned to this task
	children string // bead ID - show children (dependents) of this bead
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

// GridConfig holds the computed dimensions for a grid layout
type GridConfig struct {
	Cols       int
	Rows       int
	CellWidth  int
	CellHeight int
}

// CalculateGridDimensions computes optimal grid layout based on item count and screen size
func CalculateGridDimensions(numItems, screenWidth, screenHeight int) GridConfig {
	if numItems == 0 {
		return GridConfig{Cols: 1, Rows: 1, CellWidth: screenWidth, CellHeight: screenHeight}
	}

	// Aim for roughly square cells, considering terminal is usually wider than tall
	// Each cell needs minimum 30 chars wide and 10 lines tall
	minCellWidth := 30
	minCellHeight := 10

	maxCols := screenWidth / minCellWidth
	if maxCols < 1 {
		maxCols = 1
	}
	maxRows := screenHeight / minCellHeight
	if maxRows < 1 {
		maxRows = 1
	}

	var gridCols, gridRows int

	// Find layout that fits all items
	if numItems <= maxCols {
		// Single row
		gridCols = numItems
		gridRows = 1
	} else if numItems <= maxCols*2 {
		// Two rows
		gridCols = (numItems + 1) / 2
		gridRows = 2
	} else {
		// Fill grid
		gridCols = maxCols
		gridRows = (numItems + maxCols - 1) / maxCols
		if gridRows > maxRows {
			gridRows = maxRows
		}
	}

	// Calculate actual cell dimensions
	cellWidth := screenWidth / gridCols
	cellHeight := screenHeight / gridRows

	return GridConfig{
		Cols:       gridCols,
		Rows:       gridRows,
		CellWidth:  cellWidth,
		CellHeight: cellHeight,
	}
}
