package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

// LinearImportPanel renders the Linear import form.
type LinearImportPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Form state
	input      *textarea.Model
	createDeps bool
	update     bool
	dryRun     bool
	maxDepth   int
	focusIdx   int
	importing  bool

	// Mouse state
	hoveredButton string
}

// NewLinearImportPanel creates a new LinearImportPanel
func NewLinearImportPanel() *LinearImportPanel {
	return &LinearImportPanel{
		width:    60,
		height:   20,
		maxDepth: 2,
	}
}

// SetSize updates the panel dimensions
func (p *LinearImportPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates the focus state
func (p *LinearImportPanel) SetFocus(focused bool) {
	p.focused = focused
}

// IsFocused returns whether the panel is focused
func (p *LinearImportPanel) IsFocused() bool {
	return p.focused
}

// SetFormState updates the form state
func (p *LinearImportPanel) SetFormState(
	input *textarea.Model,
	createDeps bool,
	update bool,
	dryRun bool,
	maxDepth int,
	focusIdx int,
	importing bool,
) {
	p.input = input
	p.createDeps = createDeps
	p.update = update
	p.dryRun = dryRun
	p.maxDepth = maxDepth
	p.focusIdx = focusIdx
	p.importing = importing
}

// SetHoveredButton updates which button is hovered
func (p *LinearImportPanel) SetHoveredButton(button string) {
	p.hoveredButton = button
}

// Render returns the Linear import form content
func (p *LinearImportPanel) Render() string {
	var content strings.Builder

	// Adapt textarea width
	inputWidth := p.width - 4
	if inputWidth < 20 {
		inputWidth = 20
	}
	if p.input != nil {
		p.input.SetWidth(inputWidth)
	}

	// Show focus labels
	issueIDsLabel := "Issue IDs/URLs:"
	createDepsLabel := "Create Dependencies:"
	updateLabel := "Update Existing:"
	dryRunLabel := "Dry Run:"
	maxDepthLabel := "Max Dependency Depth:"

	if p.focusIdx == 0 {
		issueIDsLabel = tuiValueStyle.Render("Issue IDs/URLs:") + " (one per line, Ctrl+Enter to submit)"
	}
	if p.focusIdx == 1 {
		createDepsLabel = tuiValueStyle.Render("Create Dependencies:") + " (space to toggle)"
	}
	if p.focusIdx == 2 {
		updateLabel = tuiValueStyle.Render("Update Existing:") + " (space to toggle)"
	}
	if p.focusIdx == 3 {
		dryRunLabel = tuiValueStyle.Render("Dry Run:") + " (space to toggle)"
	}
	if p.focusIdx == 4 {
		maxDepthLabel = tuiValueStyle.Render("Max Dependency Depth:") + " (+/- adjust)"
	}

	// Checkbox display
	createDepsCheck := " "
	updateCheck := " "
	dryRunCheck := " "
	if p.createDeps {
		createDepsCheck = "x"
	}
	if p.update {
		updateCheck = "x"
	}
	if p.dryRun {
		dryRunCheck = "x"
	}

	content.WriteString(tuiLabelStyle.Render("Import from Linear (Bulk)"))
	content.WriteString("\n\n")
	content.WriteString(issueIDsLabel)
	content.WriteString("\n")
	if p.input != nil {
		content.WriteString(p.input.View())
	}
	content.WriteString("\n\n")
	content.WriteString(createDepsLabel + " [" + createDepsCheck + "]")
	content.WriteString("\n")
	content.WriteString(updateLabel + " [" + updateCheck + "]")
	content.WriteString("\n")
	content.WriteString(dryRunLabel + " [" + dryRunCheck + "]")
	content.WriteString("\n\n")
	content.WriteString(maxDepthLabel + " " + tuiValueStyle.Render(fmt.Sprintf("%d", p.maxDepth)))
	content.WriteString("\n\n")

	// Render Ok and Cancel buttons
	okLabel := "  Ok  "
	cancelLabel := "Cancel"
	focusHint := ""

	if p.focusIdx == 5 {
		okLabel = tuiValueStyle.Render("[ Ok ]")
		focusHint = tuiDimStyle.Render(" (press Enter to import)")
	} else {
		okLabel = styleButtonWithHover("  Ok  ", p.hoveredButton == "ok")
	}

	if p.focusIdx == 6 {
		cancelLabel = tuiValueStyle.Render("[Cancel]")
		focusHint = tuiDimStyle.Render(" (press Enter to cancel)")
	} else {
		cancelLabel = styleButtonWithHover("Cancel", p.hoveredButton == "cancel")
	}

	content.WriteString(okLabel + "  " + cancelLabel + focusHint)
	content.WriteString("\n")

	if p.importing {
		content.WriteString(tuiDimStyle.Render("Importing..."))
	} else {
		content.WriteString(tuiDimStyle.Render("[Tab] Next field  [Enter] Activate"))
	}

	return content.String()
}

// RenderWithPanel returns the panel with border styling
func (p *LinearImportPanel) RenderWithPanel(contentHeight int) string {
	panelContent := p.Render()

	panelStyle := tuiPanelStyle.Width(p.width).Height(contentHeight - 2)
	if p.focused {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("214"))
	}

	return panelStyle.Render(tuiTitleStyle.Render("Linear Import") + "\n" + panelContent)
}
