package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CreateWorkAction represents an action result from the panel
type CreateWorkAction int

const (
	CreateWorkActionNone CreateWorkAction = iota
	CreateWorkActionCancel
	CreateWorkActionExecute
	CreateWorkActionAuto
)

// CreateWorkResult contains form values when submitted
type CreateWorkResult struct {
	BranchName string
	BeadID     string
}

// CreateWorkPanel renders the work creation form.
type CreateWorkPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Form state (owned directly)
	beadID      string
	branchInput textinput.Model
	fieldIdx    int // 0=branch, 1=buttons
	buttonIdx   int // 0=Execute, 1=Auto, 2=Cancel

	// Mouse state
	hoveredButton string

	// Button position tracking
	dialogButtons []ButtonRegion
}

// NewCreateWorkPanel creates a new CreateWorkPanel
func NewCreateWorkPanel() *CreateWorkPanel {
	branchInput := textinput.New()
	branchInput.Placeholder = "Branch name..."
	branchInput.CharLimit = 100
	branchInput.Width = 60

	return &CreateWorkPanel{
		width:       60,
		height:      20,
		branchInput: branchInput,
	}
}

// Init initializes the panel and returns any initial command
func (p *CreateWorkPanel) Init() tea.Cmd {
	p.branchInput.Focus()
	return textinput.Blink
}

// Reset resets the form to initial state
func (p *CreateWorkPanel) Reset(beadID string, branchName string) {
	p.beadID = beadID
	p.branchInput.SetValue(branchName)
	p.branchInput.Focus()
	p.fieldIdx = 0
	p.buttonIdx = 0
}

// Update handles key events and returns an action
func (p *CreateWorkPanel) Update(msg tea.KeyMsg) (tea.Cmd, CreateWorkAction) {
	if msg.Type == tea.KeyEsc {
		p.branchInput.Blur()
		return nil, CreateWorkActionCancel
	}

	// Tab cycles between branch(0), buttons(1)
	if msg.Type == tea.KeyTab {
		p.fieldIdx = (p.fieldIdx + 1) % 2
		if p.fieldIdx == 0 {
			p.branchInput.Focus()
		} else {
			p.branchInput.Blur()
		}
		return nil, CreateWorkActionNone
	}

	// Shift+Tab goes backwards
	if msg.Type == tea.KeyShiftTab {
		p.fieldIdx = 1 - p.fieldIdx
		if p.fieldIdx == 0 {
			p.branchInput.Focus()
		} else {
			p.branchInput.Blur()
		}
		return nil, CreateWorkActionNone
	}

	// Handle input based on focused field
	var cmd tea.Cmd
	switch p.fieldIdx {
	case 0: // Branch name input
		p.branchInput, cmd = p.branchInput.Update(msg)
	case 1: // Buttons
		switch msg.String() {
		case "k", "up":
			p.buttonIdx--
			if p.buttonIdx < 0 {
				p.buttonIdx = 2
			}
		case "j", "down":
			p.buttonIdx = (p.buttonIdx + 1) % 3
		case "enter":
			branchName := strings.TrimSpace(p.branchInput.Value())
			if branchName == "" {
				// Empty branch name - don't submit
				return nil, CreateWorkActionNone
			}
			switch p.buttonIdx {
			case 0: // Execute
				return nil, CreateWorkActionExecute
			case 1: // Auto
				return nil, CreateWorkActionAuto
			case 2: // Cancel
				p.branchInput.Blur()
				return nil, CreateWorkActionCancel
			}
		}
	}
	return cmd, CreateWorkActionNone
}

// GetResult returns the current form values
func (p *CreateWorkPanel) GetResult() CreateWorkResult {
	return CreateWorkResult{
		BranchName: strings.TrimSpace(p.branchInput.Value()),
		BeadID:     p.beadID,
	}
}

// GetBeadID returns the bead ID for this work
func (p *CreateWorkPanel) GetBeadID() string {
	return p.beadID
}

// Blur removes focus from the input
func (p *CreateWorkPanel) Blur() {
	p.branchInput.Blur()
}

// SetSize updates the panel dimensions
func (p *CreateWorkPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates the focus state
func (p *CreateWorkPanel) SetFocus(focused bool) {
	p.focused = focused
}

// IsFocused returns whether the panel is focused
func (p *CreateWorkPanel) IsFocused() bool {
	return p.focused
}

// SetFormState updates the form state (deprecated - panel owns its state now)
func (p *CreateWorkPanel) SetFormState(
	beadID string,
	branchInput *textinput.Model,
	fieldIdx int,
	buttonIdx int,
) {
	// No-op: panel owns its own state now
	// This method is kept for backwards compatibility during migration
}

// SetHoveredButton updates which button is hovered
func (p *CreateWorkPanel) SetHoveredButton(button string) {
	p.hoveredButton = button
}

// GetDialogButtons returns tracked button positions for click detection
func (p *CreateWorkPanel) GetDialogButtons() []ButtonRegion {
	return p.dialogButtons
}

// Render returns the work creation form content
func (p *CreateWorkPanel) Render() string {
	var content strings.Builder

	// Clear previous button positions
	p.dialogButtons = nil
	currentLine := 0

	// Panel header
	content.WriteString(tuiSuccessStyle.Render("Create Work"))
	content.WriteString("\n\n")
	currentLine += 2

	// Show bead info
	beadInfo := fmt.Sprintf("Creating work from issue: %s", issueIDStyle.Render(p.beadID))
	content.WriteString(beadInfo)
	content.WriteString("\n\n")
	currentLine += 2

	// Branch name input
	var branchLabel string
	if p.fieldIdx == 0 {
		branchLabel = tuiSuccessStyle.Render("Branch name:") + " " + tuiDimStyle.Render("(editing)")
	} else {
		branchLabel = tuiLabelStyle.Render("Branch name:")
	}
	content.WriteString(branchLabel)
	content.WriteString("\n")
	currentLine++
	content.WriteString(p.branchInput.View())
	content.WriteString("\n\n")
	currentLine += 2

	// Action buttons
	content.WriteString("Actions:\n")
	currentLine++

	// Execute button
	executeStyle := tuiDimStyle
	executePrefix := "  "
	if p.fieldIdx == 1 && p.buttonIdx == 0 {
		executeStyle = tuiSelectedStyle
		executePrefix = "> "
	} else if p.hoveredButton == "execute" {
		executeStyle = tuiSuccessStyle
	}
	executeButtonText := executePrefix + "Execute"
	p.dialogButtons = append(p.dialogButtons, ButtonRegion{
		ID:     "execute",
		Y:      currentLine,
		StartX: 2,
		EndX:   2 + len(executeButtonText),
	})
	content.WriteString("  " + executeStyle.Render(executeButtonText))
	content.WriteString(" - Create work and spawn orchestrator\n")
	currentLine++

	// Auto button
	autoStyle := tuiDimStyle
	autoPrefix := "  "
	if p.fieldIdx == 1 && p.buttonIdx == 1 {
		autoStyle = tuiSelectedStyle
		autoPrefix = "> "
	} else if p.hoveredButton == "auto" {
		autoStyle = tuiSuccessStyle
	}
	autoButtonText := autoPrefix + "Auto"
	p.dialogButtons = append(p.dialogButtons, ButtonRegion{
		ID:     "auto",
		Y:      currentLine,
		StartX: 2,
		EndX:   2 + len(autoButtonText),
	})
	content.WriteString("  " + autoStyle.Render(autoButtonText))
	content.WriteString(" - Create work with automated workflow\n")
	currentLine++

	// Cancel button
	cancelStyle := tuiDimStyle
	cancelPrefix := "  "
	if p.fieldIdx == 1 && p.buttonIdx == 2 {
		cancelStyle = tuiSelectedStyle
		cancelPrefix = "> "
	} else if p.hoveredButton == "cancel" {
		cancelStyle = tuiSuccessStyle
	}
	cancelButtonText := cancelPrefix + "Cancel"
	p.dialogButtons = append(p.dialogButtons, ButtonRegion{
		ID:     "cancel",
		Y:      currentLine,
		StartX: 2,
		EndX:   2 + len(cancelButtonText),
	})
	content.WriteString("  " + cancelStyle.Render(cancelButtonText))
	content.WriteString(" - Cancel work creation\n")

	// Navigation help
	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("Navigation: [Tab/Shift+Tab] Switch field  [j/k] Select button  [Enter] Confirm  [Esc] Cancel"))

	return content.String()
}

// RenderWithPanel returns the panel with border styling
func (p *CreateWorkPanel) RenderWithPanel(contentHeight int) string {
	panelContent := p.Render()

	panelStyle := tuiPanelStyle.Width(p.width).Height(contentHeight - 2)
	if p.focused {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("214"))
	}

	result := panelStyle.Render(tuiTitleStyle.Render("Create Work") + "\n" + panelContent)

	// If the result is taller than expected (due to lipgloss wrapping), fix it
	// by removing extra lines from the INNER content while preserving borders and title
	if lipgloss.Height(result) > contentHeight {
		lines := strings.Split(result, "\n")
		extraLines := len(lines) - contentHeight
		// Need at least 4 lines: top border, title, 1+ content, bottom border
		if extraLines > 0 && len(lines) > 3 {
			// Keep first line (top border), second line (title), and last line (bottom border)
			// Remove extra lines from content area only
			topBorder := lines[0]
			titleLine := lines[1]
			bottomBorder := lines[len(lines)-1]
			// Content is from lines[2] to lines[len-2]
			contentLines := lines[2 : len(lines)-1]
			// Calculate how many content lines we can keep
			keepContentLines := len(contentLines) - extraLines
			if keepContentLines < 1 {
				keepContentLines = 1 // Always show at least 1 content line
			}
			// Truncate content from the end
			if keepContentLines < len(contentLines) {
				contentLines = contentLines[:keepContentLines]
			}
			lines = []string{topBorder, titleLine}
			lines = append(lines, contentLines...)
			lines = append(lines, bottomBorder)
			result = strings.Join(lines, "\n")
		}
	}

	return result
}
