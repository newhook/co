package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// BeadFormMode indicates which mode the form is in
type BeadFormMode int

const (
	BeadFormModeCreate BeadFormMode = iota
	BeadFormModeAddChild
	BeadFormModeEdit
)

// BeadFormAction represents an action result from the panel
type BeadFormAction int

const (
	BeadFormActionNone BeadFormAction = iota
	BeadFormActionCancel
	BeadFormActionSubmit
)

// BeadFormResult contains form values when submitted
type BeadFormResult struct {
	Title       string
	Description string
	BeadType    string
	Priority    int
	EditBeadID  string // Non-empty when editing
	ParentID    string // Non-empty when adding child
}

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

	// Form state (owned directly)
	titleInput   textinput.Model
	descTextarea textarea.Model
	beadType     int
	priority     int
	focusIdx     int

	// Mouse state
	hoveredButton string
}

// NewBeadFormPanel creates a new BeadFormPanel
func NewBeadFormPanel() *BeadFormPanel {
	titleInput := textinput.New()
	titleInput.Placeholder = "Enter title..."
	titleInput.CharLimit = 100
	titleInput.Width = 40

	descTextarea := textarea.New()
	descTextarea.Placeholder = "Enter description (optional)..."
	descTextarea.CharLimit = 2000
	descTextarea.SetWidth(60)
	descTextarea.SetHeight(4)

	return &BeadFormPanel{
		width:        60,
		height:       20,
		priority:     2,
		titleInput:   titleInput,
		descTextarea: descTextarea,
	}
}

// Init initializes the panel and returns any initial command
func (p *BeadFormPanel) Init() tea.Cmd {
	p.titleInput.Focus()
	return textinput.Blink
}

// Reset resets the form to initial state for creating a new bead
func (p *BeadFormPanel) Reset() {
	p.titleInput.Reset()
	p.titleInput.Focus()
	p.descTextarea.Reset()
	p.beadType = 0
	p.priority = 2
	p.focusIdx = 0
	p.mode = BeadFormModeCreate
	p.editBeadID = ""
	p.parentID = ""
}

// SetEditMode configures the form for editing an existing bead
func (p *BeadFormPanel) SetEditMode(beadID, title, description, beadType string, priority int) {
	p.mode = BeadFormModeEdit
	p.editBeadID = beadID
	p.parentID = ""
	p.titleInput.SetValue(title)
	p.titleInput.Focus()
	p.descTextarea.SetValue(description)
	// Find the type index
	p.beadType = 0
	for i, t := range beadTypes {
		if t == beadType {
			p.beadType = i
			break
		}
	}
	p.priority = priority
	p.focusIdx = 0
}

// SetAddChildMode configures the form for adding a child bead
func (p *BeadFormPanel) SetAddChildMode(parentID string) {
	p.Reset()
	p.mode = BeadFormModeAddChild
	p.parentID = parentID
}

// Update handles key events and returns an action
func (p *BeadFormPanel) Update(msg tea.KeyMsg) (tea.Cmd, BeadFormAction) {
	// Check escape/cancel keys
	if msg.Type == tea.KeyEsc || msg.String() == "esc" {
		p.titleInput.Blur()
		p.descTextarea.Blur()
		return nil, BeadFormActionCancel
	}

	// Tab cycles between elements: title(0) -> type(1) -> priority(2) -> description(3) -> ok(4) -> cancel(5)
	if msg.Type == tea.KeyTab || msg.String() == "tab" {
		// Leave current focus
		if p.focusIdx == 0 {
			p.titleInput.Blur()
		} else if p.focusIdx == 3 {
			p.descTextarea.Blur()
		}

		p.focusIdx = (p.focusIdx + 1) % 6

		// Enter new focus
		if p.focusIdx == 0 {
			p.titleInput.Focus()
		} else if p.focusIdx == 3 {
			p.descTextarea.Focus()
		}
		return nil, BeadFormActionNone
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		// Leave current focus
		if p.focusIdx == 0 {
			p.titleInput.Blur()
		} else if p.focusIdx == 3 {
			p.descTextarea.Blur()
		}

		p.focusIdx--
		if p.focusIdx < 0 {
			p.focusIdx = 5
		}

		// Enter new focus
		if p.focusIdx == 0 {
			p.titleInput.Focus()
		} else if p.focusIdx == 3 {
			p.descTextarea.Focus()
		}
		return nil, BeadFormActionNone
	}

	// Enter key handling depends on focused element
	if msg.String() == "enter" {
		switch p.focusIdx {
		case 0, 1, 2: // Title, type, or priority - submit form
			title := strings.TrimSpace(p.titleInput.Value())
			if title != "" {
				return nil, BeadFormActionSubmit
			}
			return nil, BeadFormActionNone
		case 4: // Ok button - submit form
			title := strings.TrimSpace(p.titleInput.Value())
			if title != "" {
				return nil, BeadFormActionSubmit
			}
			return nil, BeadFormActionNone
		case 5: // Cancel button - cancel form
			p.titleInput.Blur()
			p.descTextarea.Blur()
			return nil, BeadFormActionCancel
		}
		// For description textarea (3), Enter adds a newline (handled below)
	}

	// Ctrl+Enter submits from description textarea
	if msg.String() == "ctrl+enter" && p.focusIdx == 3 {
		title := strings.TrimSpace(p.titleInput.Value())
		if title != "" {
			return nil, BeadFormActionSubmit
		}
		return nil, BeadFormActionNone
	}

	// Handle input based on focused element
	switch p.focusIdx {
	case 0: // Title input
		var cmd tea.Cmd
		p.titleInput, cmd = p.titleInput.Update(msg)
		return cmd, BeadFormActionNone

	case 1: // Type selector
		switch msg.String() {
		case "j", "down", "right":
			p.beadType = (p.beadType + 1) % len(beadTypes)
		case "k", "up", "left":
			p.beadType--
			if p.beadType < 0 {
				p.beadType = len(beadTypes) - 1
			}
		}
		return nil, BeadFormActionNone

	case 2: // Priority
		switch msg.String() {
		case "j", "down", "right", "-":
			if p.priority < 4 {
				p.priority++
			}
		case "k", "up", "left", "+", "=":
			if p.priority > 0 {
				p.priority--
			}
		}
		return nil, BeadFormActionNone

	case 3: // Description textarea
		var cmd tea.Cmd
		p.descTextarea, cmd = p.descTextarea.Update(msg)
		return cmd, BeadFormActionNone

	case 4: // Ok button - Space can also activate it
		if msg.String() == " " {
			title := strings.TrimSpace(p.titleInput.Value())
			if title != "" {
				return nil, BeadFormActionSubmit
			}
		}
		return nil, BeadFormActionNone

	case 5: // Cancel button - Space can also activate it
		if msg.String() == " " {
			p.titleInput.Blur()
			p.descTextarea.Blur()
			return nil, BeadFormActionCancel
		}
		return nil, BeadFormActionNone
	}

	return nil, BeadFormActionNone
}

// GetResult returns the current form values
func (p *BeadFormPanel) GetResult() BeadFormResult {
	return BeadFormResult{
		Title:       strings.TrimSpace(p.titleInput.Value()),
		Description: strings.TrimSpace(p.descTextarea.Value()),
		BeadType:    beadTypes[p.beadType],
		Priority:    p.priority,
		EditBeadID:  p.editBeadID,
		ParentID:    p.parentID,
	}
}

// GetMode returns the current form mode
func (p *BeadFormPanel) GetMode() BeadFormMode {
	return p.mode
}

// Blur removes focus from all inputs
func (p *BeadFormPanel) Blur() {
	p.titleInput.Blur()
	p.descTextarea.Blur()
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

// SetMode updates the form mode (deprecated - use Reset/SetEditMode/SetAddChildMode)
func (p *BeadFormPanel) SetMode(mode BeadFormMode, editBeadID, parentID string) {
	p.mode = mode
	p.editBeadID = editBeadID
	p.parentID = parentID
}

// SetFormState updates the form state (deprecated - panel owns its state now)
func (p *BeadFormPanel) SetFormState(
	titleInput *textinput.Model,
	descTextarea *textarea.Model,
	beadType int,
	priority int,
	focusIdx int,
) {
	// No-op: panel owns its own state now
	// This method is kept for backwards compatibility during migration
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
	p.titleInput.Width = inputWidth
	p.descTextarea.SetWidth(inputWidth)
	// Calculate dynamic height for description textarea
	descHeight := max(visibleLines-12, 4)
	p.descTextarea.SetHeight(descHeight)

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
	content.WriteString(p.titleInput.View())
	content.WriteString("\n\n")
	content.WriteString(typeLabel + " " + typeDisplay)
	content.WriteString("\n")
	content.WriteString(priorityLabel + " " + priorityDisplay)
	content.WriteString("\n\n")
	content.WriteString(descLabel)
	content.WriteString("\n")
	content.WriteString(p.descTextarea.View())
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
