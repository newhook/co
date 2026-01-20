package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// BeadFormMode indicates which mode the form is in
type BeadFormMode int

const (
	BeadFormModeCreate BeadFormMode = iota
	BeadFormModeAddChild
	BeadFormModeEdit
)

// BeadFormPanel renders the bead create/edit form.
type BeadFormPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Form mode
	mode       BeadFormMode
	editBeadID string
	parentID   string

	// Form state
	titleInput   *textinput.Model
	descTextarea *textarea.Model
	beadType     int
	priority     int
	focusIdx     int

	// Mouse state
	hoveredButton string
}

// NewBeadFormPanel creates a new BeadFormPanel
func NewBeadFormPanel() *BeadFormPanel {
	return &BeadFormPanel{
		width:    60,
		height:   20,
		priority: 2,
	}
}

// SetSize updates the panel dimensions
func (p *BeadFormPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates the focus state
func (p *BeadFormPanel) SetFocus(focused bool) {
	p.focused = focused
}

// IsFocused returns whether the panel is focused
func (p *BeadFormPanel) IsFocused() bool {
	return p.focused
}

// SetMode updates the form mode
func (p *BeadFormPanel) SetMode(mode BeadFormMode, editBeadID, parentID string) {
	p.mode = mode
	p.editBeadID = editBeadID
	p.parentID = parentID
}

// SetFormState updates the form state
func (p *BeadFormPanel) SetFormState(
	titleInput *textinput.Model,
	descTextarea *textarea.Model,
	beadType int,
	priority int,
	focusIdx int,
) {
	p.titleInput = titleInput
	p.descTextarea = descTextarea
	p.beadType = beadType
	p.priority = priority
	p.focusIdx = focusIdx
}

// SetHoveredButton updates which button is hovered
func (p *BeadFormPanel) SetHoveredButton(button string) {
	p.hoveredButton = button
}

// Render returns the bead form content
func (p *BeadFormPanel) Render(visibleLines int) string {
	var content strings.Builder

	// Adapt input widths to available space
	inputWidth := p.width - 4
	if inputWidth < 20 {
		inputWidth = 20
	}
	if p.titleInput != nil {
		p.titleInput.Width = inputWidth
	}
	if p.descTextarea != nil {
		p.descTextarea.SetWidth(inputWidth)
		// Calculate dynamic height for description textarea
		descHeight := max(visibleLines-12, 4)
		p.descTextarea.SetHeight(descHeight)
	}

	typeFocused := p.focusIdx == 1
	priorityFocused := p.focusIdx == 2
	descFocused := p.focusIdx == 3

	// Type rotator display
	currentType := beadTypes[p.beadType]
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
		priorityDisplay = fmt.Sprintf("< %s >", tuiValueStyle.Render(priorityLabels[p.priority]))
	} else {
		priorityDisplay = priorityLabels[p.priority]
	}

	// Show focus labels
	titleLabel := "Title:"
	typeLabel := "Type:"
	priorityLabel := "Priority:"
	descLabel := "Description:"
	if p.focusIdx == 0 {
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

	// Determine mode and render appropriate header
	var header string
	switch p.mode {
	case BeadFormModeEdit:
		header = "Edit Issue " + issueIDStyle.Render(p.editBeadID)
	case BeadFormModeAddChild:
		header = "Add Child Issue"
	default:
		header = "Create New Issue"
	}

	content.WriteString(tuiLabelStyle.Render(header))
	content.WriteString("\n")

	// Show parent info for add child mode
	if p.mode == BeadFormModeAddChild && p.parentID != "" {
		content.WriteString(tuiDimStyle.Render("Parent: ") + tuiValueStyle.Render(p.parentID))
		content.WriteString("\n")
	}

	// Render form fields
	content.WriteString("\n")
	content.WriteString(titleLabel)
	content.WriteString("\n")
	if p.titleInput != nil {
		content.WriteString(p.titleInput.View())
	}
	content.WriteString("\n\n")
	content.WriteString(typeLabel + " " + typeDisplay)
	content.WriteString("\n")
	content.WriteString(priorityLabel + " " + priorityDisplay)
	content.WriteString("\n\n")
	content.WriteString(descLabel)
	content.WriteString("\n")
	if p.descTextarea != nil {
		content.WriteString(p.descTextarea.View())
	}
	content.WriteString("\n\n")

	// Render Ok and Cancel buttons
	okFocused := p.focusIdx == 4
	cancelFocused := p.focusIdx == 5
	okButton := styleButtonWithHover("  Ok  ", p.hoveredButton == "ok" || okFocused)
	cancelButton := styleButtonWithHover("Cancel", p.hoveredButton == "cancel" || cancelFocused)
	content.WriteString(okButton + "  " + cancelButton)
	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("[Tab] Next  [Enter/Space] Select"))

	return content.String()
}

// RenderWithPanel returns the panel with border styling
func (p *BeadFormPanel) RenderWithPanel(contentHeight int) string {
	panelContent := p.Render(contentHeight - 3)

	panelStyle := tuiPanelStyle.Width(p.width).Height(contentHeight - 2)
	if p.focused {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("214"))
	}

	// Determine title based on mode
	var title string
	switch p.mode {
	case BeadFormModeEdit:
		title = "Edit Issue"
	case BeadFormModeAddChild:
		title = "Add Child"
	default:
		title = "Create Issue"
	}

	return panelStyle.Render(tuiTitleStyle.Render(title) + "\n" + panelContent)
}
