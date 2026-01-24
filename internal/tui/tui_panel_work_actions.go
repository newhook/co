package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/progress"
)

// WorkActionsPanel renders the right side of the work details view when showing work actions.
// It displays available actions for the focused work.
type WorkActionsPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Data
	focusedWork *progress.WorkProgress

	// Selected action index for navigation
	selectedIndex int

	// Mouse state
	hoveredIndex int // -1 = none hovered

	// Button region tracking for mouse clicks
	actionRegions []actionRegion
}

// actionRegion tracks a clickable action's position
type actionRegion struct {
	index int
	y     int // Y position relative to panel content
}

// WorkAction represents an action that can be performed on a work
type WorkAction struct {
	Key         string
	Label       string
	Description string
}

// GetWorkActions returns the available actions for the work actions panel
func GetWorkActions() []WorkAction {
	return []WorkAction{
		{Key: "a", Label: "Auto-group", Description: "Group related beads into tasks"},
		{Key: "s", Label: "Single-bead", Description: "One task per bead"},
	}
}

// NewWorkActionsPanel creates a new WorkActionsPanel
func NewWorkActionsPanel() *WorkActionsPanel {
	return &WorkActionsPanel{
		width:         40,
		height:        20,
		selectedIndex: 0,
		hoveredIndex:  -1,
	}
}

// SetHoveredIndex sets which action is being hovered
func (p *WorkActionsPanel) SetHoveredIndex(index int) {
	p.hoveredIndex = index
}

// GetHoveredIndex returns the currently hovered action index
func (p *WorkActionsPanel) GetHoveredIndex() int {
	return p.hoveredIndex
}

// DetectHoveredAction returns the action index at the given Y position (relative to panel content)
func (p *WorkActionsPanel) DetectHoveredAction(y int) int {
	for _, region := range p.actionRegions {
		if y == region.y {
			return region.index
		}
	}
	return -1
}

// DetectClickedAction returns the action index that was clicked, or -1 if none
func (p *WorkActionsPanel) DetectClickedAction(y int) int {
	return p.DetectHoveredAction(y)
}

// SetSize updates the panel dimensions
func (p *WorkActionsPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates the focus state
func (p *WorkActionsPanel) SetFocus(focused bool) {
	p.focused = focused
}

// SetFocusedWork updates the focused work
func (p *WorkActionsPanel) SetFocusedWork(focusedWork *progress.WorkProgress) {
	p.focusedWork = focusedWork
}

// GetSelectedIndex returns the currently selected action index
func (p *WorkActionsPanel) GetSelectedIndex() int {
	return p.selectedIndex
}

// SetSelectedIndex sets the selected action index
func (p *WorkActionsPanel) SetSelectedIndex(index int) {
	actions := GetWorkActions()
	if index >= 0 && index < len(actions) {
		p.selectedIndex = index
	}
}

// NavigateUp moves selection to the previous action
func (p *WorkActionsPanel) NavigateUp() {
	if p.selectedIndex > 0 {
		p.selectedIndex--
	}
}

// NavigateDown moves selection to the next action
func (p *WorkActionsPanel) NavigateDown() {
	actions := GetWorkActions()
	if p.selectedIndex < len(actions)-1 {
		p.selectedIndex++
	}
}

// Render returns the work actions content
func (p *WorkActionsPanel) Render(panelWidth int) string {
	var content strings.Builder

	// Clear action regions for fresh tracking
	p.actionRegions = nil

	if p.focusedWork == nil {
		content.WriteString(tuiDimStyle.Render("No work selected"))
		return content.String()
	}

	// Account for padding (tuiPanelStyle has Padding(0, 1) = 2 chars total)
	contentWidth := panelWidth - 2

	// Track current line for action region detection
	currentLine := 0

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	content.WriteString(headerStyle.Render("Work Actions"))
	content.WriteString("\n")
	currentLine++
	content.WriteString(strings.Repeat("─", contentWidth))
	content.WriteString("\n\n")
	currentLine += 2

	// Work info
	workID := p.focusedWork.Work.ID
	workName := p.focusedWork.Work.Name
	if workName == "" {
		workName = workID
	}

	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
	content.WriteString(infoStyle.Render(fmt.Sprintf("Work: %s", workName)))
	content.WriteString("\n\n")
	currentLine += 2

	// Actions list
	actions := GetWorkActions()
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
	hoverStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	for i, action := range actions {
		line := fmt.Sprintf("[%s] %-12s %s", action.Key, action.Label, action.Description)

		// Track action region for mouse detection
		p.actionRegions = append(p.actionRegions, actionRegion{
			index: i,
			y:     currentLine,
		})

		isSelected := i == p.selectedIndex
		isHovered := i == p.hoveredIndex

		if isSelected {
			// Calculate visible width for proper padding
			visWidth := lipgloss.Width(line)
			if visWidth < contentWidth {
				line += strings.Repeat(" ", contentWidth-visWidth)
			}
			content.WriteString(tuiSelectedStyle.Render(line))
		} else if isHovered {
			// Hover style (orange text)
			content.WriteString(hoverStyle.Render(line))
		} else {
			// Build styled line
			styledKey := keyStyle.Render(fmt.Sprintf("[%s]", action.Key))
			styledLabel := fmt.Sprintf(" %-12s ", action.Label)
			styledDesc := descStyle.Render(action.Description)
			content.WriteString(styledKey + styledLabel + styledDesc)
		}
		content.WriteString("\n")
		currentLine++
	}

	// Footer with instructions
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	content.WriteString(footerStyle.Render("Press key or click to execute, j/k to navigate, Esc to cancel"))

	return content.String()
}
