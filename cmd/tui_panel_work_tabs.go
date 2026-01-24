package cmd

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
)

// WorkState represents the current state of a work for display purposes
type WorkState int

const (
	WorkStateIdle      WorkState = iota // Orchestrator alive, no tasks running
	WorkStateRunning                    // Task is processing
	WorkStateCompleted                  // Work is completed
	WorkStateFailed                     // Work failed
	WorkStateDead                       // Orchestrator is dead
	WorkStateMerged                     // PR was merged
)

// WorkTabsBar renders a horizontal tab bar showing all works.
// Each tab can be clicked to focus that work. Running works show a spinner.
// Styled similar to zellij with seamless color transitions between tabs.
type WorkTabsBar struct {
	// Dimensions
	width int

	// Data
	workTiles          []*workProgress
	focusedWorkID      string
	hoveredTabID       string
	orchestratorHealth map[string]bool // workID -> orchestrator alive

	// Panel state
	activePanel Panel // Which panel is currently focused

	// Spinner for running works
	spinner spinner.Model

	// Tab positions for click detection
	tabRegions []tabRegion
}

// tabRegion represents a clickable tab's position
type tabRegion struct {
	workID string
	startX int
	endX   int
}

// NewWorkTabsBar creates a new WorkTabsBar
func NewWorkTabsBar() *WorkTabsBar {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	return &WorkTabsBar{
		width:              80,
		tabRegions:         []tabRegion{},
		spinner:            s,
		orchestratorHealth: make(map[string]bool),
	}
}

// SetSize updates the bar width
func (b *WorkTabsBar) SetSize(width int) {
	b.width = width
}

// SetWorkTiles updates the work tiles data
func (b *WorkTabsBar) SetWorkTiles(workTiles []*workProgress) {
	b.workTiles = workTiles
}

// SetFocusedWorkID sets which work is currently focused
func (b *WorkTabsBar) SetFocusedWorkID(id string) {
	b.focusedWorkID = id
}

// SetHoveredTabID sets which tab is being hovered
func (b *WorkTabsBar) SetHoveredTabID(id string) {
	b.hoveredTabID = id
}

// SetOrchestratorHealth sets the orchestrator health for a work
func (b *WorkTabsBar) SetOrchestratorHealth(healthMap map[string]bool) {
	b.orchestratorHealth = healthMap
}

// SetActivePanel sets which panel is currently active
func (b *WorkTabsBar) SetActivePanel(panel Panel) {
	b.activePanel = panel
}

// UpdateSpinner updates the spinner animation frame
func (b *WorkTabsBar) UpdateSpinner(s spinner.Model) {
	b.spinner = s
}

// GetSpinner returns the spinner model for update handling
func (b *WorkTabsBar) GetSpinner() spinner.Model {
	return b.spinner
}

// Height returns the height of the tab bar (always 1 line)
func (b *WorkTabsBar) Height() int {
	return 1
}

// getWorkState determines the current state of a work for display
func (b *WorkTabsBar) getWorkState(work *workProgress) WorkState {
	if work == nil {
		return WorkStateDead
	}

	// Check if any task is running FIRST - this takes priority over work status
	// because new tasks can be added to idle/completed works
	for _, task := range work.tasks {
		if task.task.Status == db.StatusProcessing {
			return WorkStateRunning
		}
	}

	// Then check work status
	switch work.work.Status {
	case db.StatusMerged:
		return WorkStateMerged
	case db.StatusCompleted:
		return WorkStateCompleted
	case db.StatusFailed:
		return WorkStateFailed
	case db.StatusIdle:
		return WorkStateIdle
	}

	// Check orchestrator health
	if alive, ok := b.orchestratorHealth[work.work.ID]; ok && !alive {
		return WorkStateDead
	}

	// Default to idle
	return WorkStateIdle
}

// Render renders the tab bar with zellij-like styling
func (b *WorkTabsBar) Render() string {
	// Clear tab regions for fresh tracking
	b.tabRegions = nil

	// Colors
	barBg := lipgloss.Color("235")      // Dark background
	ribbonBg := lipgloss.Color("29")    // Teal for ribbon
	ribbonFg := lipgloss.Color("15")    // White text
	inactiveBg := lipgloss.Color("240") // Gray for inactive
	inactiveFg := lipgloss.Color("255") // Light text
	activeBg := lipgloss.Color("214")   // Orange for active
	activeFg := lipgloss.Color("232")   // Dark text

	// Zellij-style: uses right-pointing triangle on both sides
	triangle := "\ue0b0" // U+E0B0 - right-pointing solid triangle

	var content string
	currentX := 0

	// Ribbon as simple box (no triangles)
	// Show focus indicator when work tabs panel is active
	ribbonText := " Ørchestratör "
	if b.activePanel == PanelWorkTabs {
		ribbonText = "► Ørchestratör ◄"
	}
	ribbonStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ribbonFg).
		Background(ribbonBg)

	content += ribbonStyle.Render(ribbonText)
	currentX += lipgloss.Width(ribbonText)

	// Space before tabs
	spaceStyle := lipgloss.NewStyle().Background(barBg)
	content += spaceStyle.Render(" ")
	currentX++

	for i, work := range b.workTiles {
		if work == nil {
			continue
		}

		isActive := work.work.ID == b.focusedWorkID
		isHovered := work.work.ID == b.hoveredTabID
		workState := b.getWorkState(work)

		// Determine tab colors
		var tabBg, tabFg lipgloss.Color
		if isActive || isHovered {
			tabBg = activeBg
			tabFg = activeFg
		} else {
			tabBg = inactiveBg
			tabFg = inactiveFg
		}

		regionStart := currentX

		// Left triangle for tab: dark arrow on tab background
		tabLeftStyle := lipgloss.NewStyle().
			Foreground(barBg).
			Background(tabBg)
		content += tabLeftStyle.Render(triangle)
		currentX++

		// Status icon
		var icon string
		switch workState {
		case WorkStateMerged:
			icon = "✓" // Checkmark for merged PRs
		case WorkStateCompleted:
			icon = "✓"
		case WorkStateRunning:
			// Get raw spinner frame by removing style - View() with styling adds
			// ANSI reset codes that break the background color of the containing tab
			unstyled := b.spinner
			unstyled.Style = lipgloss.NewStyle()
			icon = unstyled.View()
		case WorkStateFailed:
			icon = "✗"
		case WorkStateDead:
			icon = "☠"
		default:
			icon = "○"
		}

		// Work name
		name := work.work.ID
		if work.work.Name != "" {
			name = work.work.Name
		}
		if len(name) > 20 {
			name = name[:19] + "…"
		}

		// Tab content with optional unseen badge
		tabContent := fmt.Sprintf(" %s %s", icon, name)
		tabStyle := lipgloss.NewStyle().
			Foreground(tabFg).
			Background(tabBg)
		content += tabStyle.Render(tabContent)

		// Add pending work indicator (orange warning for feedback or unassigned beads)
		if work.feedbackCount > 0 || work.unassignedBeadCount > 0 {
			badgeStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")). // Orange for pending work
				Background(tabBg)
			content += badgeStyle.Render(" \uf071") // nf-fa-exclamation_triangle
			currentX += 2
		}

		// Add unseen PR changes indicator (colored dot)
		if work.hasUnseenPRChanges {
			badgeStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("81")). // Cyan dot for new changes
				Background(tabBg)
			content += badgeStyle.Render(" ●")
			currentX += 2
		}

		// Trailing space
		content += tabStyle.Render(" ")
		currentX += lipgloss.Width(tabContent)

		// Right chevron for tab
		tabRightStyle := lipgloss.NewStyle().
			Foreground(tabBg).
			Background(barBg)
		content += tabRightStyle.Render(triangle)
		currentX++

		// Track region for click detection (endX is exclusive)
		b.tabRegions = append(b.tabRegions, tabRegion{
			workID: work.work.ID,
			startX: regionStart,
			endX:   currentX - 1,
		})

		// Space between tabs (except last)
		if i < len(b.workTiles)-1 {
			content += spaceStyle.Render(" ")
			currentX++
		}
	}

	// Wrap in bar background
	barStyle := lipgloss.NewStyle().
		Background(barBg).
		Width(b.width)

	return barStyle.Render(content)
}

// DetectHoveredTab returns the work ID of the tab at the given X position
func (b *WorkTabsBar) DetectHoveredTab(x int) string {
	for _, region := range b.tabRegions {
		if x >= region.startX && x <= region.endX {
			return region.workID
		}
	}
	return ""
}

// HandleClick handles a mouse click and returns the clicked work ID (if any)
func (b *WorkTabsBar) HandleClick(x int) string {
	return b.DetectHoveredTab(x)
}
