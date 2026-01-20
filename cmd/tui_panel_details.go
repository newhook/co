package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

const detailsPanelPaddingVal = 4

// IssueDetailsPanel renders issue details and various inline forms.
type IssueDetailsPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Data (set by coordinator)
	beadItems      []beadItem
	cursor         int
	activeSessions map[string]bool

	// Form state (shared with coordinator)
	viewMode           ViewMode
	textInput          *textinput.Model
	createDescTextarea *textarea.Model
	createBeadType     int
	createBeadPriority int
	createDialogFocus  int
	editBeadID         string
	parentBeadID       string

	// Linear import state
	linearImportInput      *textarea.Model
	linearImportCreateDeps bool
	linearImportUpdate     bool
	linearImportDryRun     bool
	linearImportMaxDepth   int
	linearImportFocus      int
	linearImporting        bool

	// Create work state
	createWorkBeadIDs   []string
	createWorkBranch    *textinput.Model
	createWorkField     int
	createWorkButtonIdx int

	// Add to work state
	availableWorks []workItem
	worksCursor    int

	// Mouse state
	hoveredDialogButton string

	// Button position tracking
	dialogButtons []ButtonRegion
}

// NewIssueDetailsPanel creates a new IssueDetailsPanel
func NewIssueDetailsPanel() *IssueDetailsPanel {
	return &IssueDetailsPanel{
		width:          60,
		height:         20,
		activeSessions: make(map[string]bool),
	}
}

// SetSize updates the panel dimensions
func (p *IssueDetailsPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates the focus state
func (p *IssueDetailsPanel) SetFocus(focused bool) {
	p.focused = focused
}

// IsFocused returns whether the panel is focused
func (p *IssueDetailsPanel) IsFocused() bool {
	return p.focused
}

// SetData updates the panel's data
func (p *IssueDetailsPanel) SetData(beadItems []beadItem, cursor int, activeSessions map[string]bool) {
	p.beadItems = beadItems
	p.cursor = cursor
	p.activeSessions = activeSessions
}

// SetFormState updates form-related state
func (p *IssueDetailsPanel) SetFormState(
	viewMode ViewMode,
	textInput *textinput.Model,
	createDescTextarea *textarea.Model,
	createBeadType int,
	createBeadPriority int,
	createDialogFocus int,
	editBeadID string,
	parentBeadID string,
) {
	p.viewMode = viewMode
	p.textInput = textInput
	p.createDescTextarea = createDescTextarea
	p.createBeadType = createBeadType
	p.createBeadPriority = createBeadPriority
	p.createDialogFocus = createDialogFocus
	p.editBeadID = editBeadID
	p.parentBeadID = parentBeadID
}

// SetLinearImportState updates Linear import state
func (p *IssueDetailsPanel) SetLinearImportState(
	linearImportInput *textarea.Model,
	linearImportCreateDeps bool,
	linearImportUpdate bool,
	linearImportDryRun bool,
	linearImportMaxDepth int,
	linearImportFocus int,
	linearImporting bool,
) {
	p.linearImportInput = linearImportInput
	p.linearImportCreateDeps = linearImportCreateDeps
	p.linearImportUpdate = linearImportUpdate
	p.linearImportDryRun = linearImportDryRun
	p.linearImportMaxDepth = linearImportMaxDepth
	p.linearImportFocus = linearImportFocus
	p.linearImporting = linearImporting
}

// SetCreateWorkState updates create work dialog state
func (p *IssueDetailsPanel) SetCreateWorkState(
	createWorkBeadIDs []string,
	createWorkBranch *textinput.Model,
	createWorkField int,
	createWorkButtonIdx int,
) {
	p.createWorkBeadIDs = createWorkBeadIDs
	p.createWorkBranch = createWorkBranch
	p.createWorkField = createWorkField
	p.createWorkButtonIdx = createWorkButtonIdx
}

// SetAddToWorkState updates add to work state
func (p *IssueDetailsPanel) SetAddToWorkState(availableWorks []workItem, worksCursor int) {
	p.availableWorks = availableWorks
	p.worksCursor = worksCursor
}

// SetHoveredDialogButton updates which dialog button is hovered
func (p *IssueDetailsPanel) SetHoveredDialogButton(button string) {
	p.hoveredDialogButton = button
}

// GetDialogButtons returns tracked button positions for click detection
func (p *IssueDetailsPanel) GetDialogButtons() []ButtonRegion {
	return p.dialogButtons
}

// Render returns the details panel content (without border/panel styling)
func (p *IssueDetailsPanel) Render(visibleLines int) string {
	// If in any bead form mode, render the unified form
	if p.viewMode == ViewCreateBead || p.viewMode == ViewCreateBeadInline ||
		p.viewMode == ViewAddChildBead || p.viewMode == ViewEditBead {
		return p.renderBeadFormContent(visibleLines)
	}

	// If in inline Linear import mode, render the import form
	if p.viewMode == ViewLinearImportInline {
		return p.renderLinearImportContent(visibleLines)
	}

	// If in create work mode, render the work creation panel
	if p.viewMode == ViewCreateWork {
		return p.renderCreateWorkContent(visibleLines)
	}

	// If in add to work mode, render the works list
	if p.viewMode == ViewAddToWork {
		return p.renderAddToWorkContent(visibleLines)
	}

	// Normal issue details view
	return p.renderIssueDetails(visibleLines)
}

// RenderWithPanel returns the details panel with border styling
func (p *IssueDetailsPanel) RenderWithPanel(contentHeight int) string {
	detailsContentLines := contentHeight - 3
	detailsContent := p.Render(detailsContentLines)

	panelStyle := tuiPanelStyle.Width(p.width).Height(contentHeight - 2)
	if p.focused {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("214"))
	}

	return panelStyle.Render(tuiTitleStyle.Render("Details") + "\n" + detailsContent)
}

// renderIssueDetails renders the normal issue details view
func (p *IssueDetailsPanel) renderIssueDetails(visibleLines int) string {
	var content strings.Builder

	if len(p.beadItems) == 0 || p.cursor >= len(p.beadItems) {
		content.WriteString(tuiDimStyle.Render("No issue selected"))
		return content.String()
	}

	bead := p.beadItems[p.cursor]

	content.WriteString(tuiLabelStyle.Render("ID: "))
	content.WriteString(tuiValueStyle.Render(bead.id))
	content.WriteString("  ")
	content.WriteString(tuiLabelStyle.Render("Type: "))
	content.WriteString(tuiValueStyle.Render(bead.beadType))
	content.WriteString("  ")
	content.WriteString(tuiLabelStyle.Render("P"))
	content.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d", bead.priority)))
	content.WriteString("  ")
	content.WriteString(tuiLabelStyle.Render("Status: "))
	content.WriteString(tuiValueStyle.Render(bead.status))
	if p.activeSessions[bead.id] {
		content.WriteString("  ")
		content.WriteString(tuiSuccessStyle.Render("[Session Active]"))
	}
	if bead.assignedWorkID != "" {
		content.WriteString("  ")
		content.WriteString(tuiDimStyle.Render("Work: " + bead.assignedWorkID))
	}
	content.WriteString("\n")

	// Use width-aware wrapping for title
	titleStyle := tuiValueStyle.Width(p.width - detailsPanelPaddingVal)
	content.WriteString(titleStyle.Render(bead.title))

	// Calculate remaining lines for description and children
	linesUsed := 2 // header + title
	remainingLines := visibleLines - linesUsed

	// Show description if we have room
	if bead.description != "" && remainingLines > 2 {
		content.WriteString("\n")
		descStyle := tuiDimStyle.Width(p.width - detailsPanelPaddingVal)
		desc := bead.description
		descLines := remainingLines - 2
		if len(bead.children) > 0 {
			descLines = min(descLines, 3)
		}
		maxLen := descLines * (p.width - detailsPanelPaddingVal)
		if len(desc) > maxLen && maxLen > 0 {
			desc = desc[:maxLen] + "..."
		}
		content.WriteString(descStyle.Render(desc))
		linesUsed++
		remainingLines--
	}

	// Show children (issues blocked by this one)
	if len(bead.children) > 0 && remainingLines > 1 {
		content.WriteString("\n")
		content.WriteString(tuiLabelStyle.Render("Blocks: "))
		linesUsed++
		remainingLines--

		// Build a map for quick lookup of child status
		childMap := make(map[string]*beadItem)
		for i := range p.beadItems {
			childMap[p.beadItems[i].id] = &p.beadItems[i]
		}

		// Show children with status
		maxChildren := min(len(bead.children), remainingLines)
		for i := 0; i < maxChildren; i++ {
			childID := bead.children[i]
			var childLine string
			if child, ok := childMap[childID]; ok {
				childLine = fmt.Sprintf("\n  %s %s %s",
					statusIcon(child.status),
					issueIDStyle.Render(child.id),
					child.title)
			} else {
				childLine = fmt.Sprintf("\n  ? %s", issueIDStyle.Render(childID))
			}
			if lipgloss.Width(childLine) > p.width {
				childLine = truncate.StringWithTail(childLine, uint(p.width), "...")
			}
			content.WriteString(childLine)
		}
		if len(bead.children) > maxChildren {
			content.WriteString(fmt.Sprintf("\n  ... and %d more", len(bead.children)-maxChildren))
		}
	}

	return content.String()
}

// renderBeadFormContent renders the unified bead form (create, add child, or edit)
func (p *IssueDetailsPanel) renderBeadFormContent(visibleLines int) string {
	var content strings.Builder

	// Adapt input widths to available space
	inputWidth := p.width - detailsPanelPaddingVal
	if inputWidth < 20 {
		inputWidth = 20
	}
	p.textInput.Width = inputWidth
	p.createDescTextarea.SetWidth(inputWidth)

	// Calculate dynamic height for description textarea
	descHeight := max(visibleLines-12, 4)
	p.createDescTextarea.SetHeight(descHeight)

	typeFocused := p.createDialogFocus == 1
	priorityFocused := p.createDialogFocus == 2
	descFocused := p.createDialogFocus == 3

	// Type rotator display
	currentType := beadTypes[p.createBeadType]
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
		priorityDisplay = fmt.Sprintf("< %s >", tuiValueStyle.Render(priorityLabels[p.createBeadPriority]))
	} else {
		priorityDisplay = priorityLabels[p.createBeadPriority]
	}

	// Show focus labels
	titleLabel := "Title:"
	typeLabel := "Type:"
	priorityLabel := "Priority:"
	descLabel := "Description:"
	if p.createDialogFocus == 0 {
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
	if p.editBeadID != "" {
		header = "Edit Issue " + issueIDStyle.Render(p.editBeadID)
	} else if p.parentBeadID != "" {
		header = "Add Child Issue"
	} else {
		header = "Create New Issue"
	}

	content.WriteString(tuiLabelStyle.Render(header))
	content.WriteString("\n")

	// Show parent info for add child mode
	if p.parentBeadID != "" {
		content.WriteString(tuiDimStyle.Render("Parent: ") + tuiValueStyle.Render(p.parentBeadID))
		content.WriteString("\n")
	}

	// Render form fields
	content.WriteString("\n")
	content.WriteString(titleLabel)
	content.WriteString("\n")
	content.WriteString(p.textInput.View())
	content.WriteString("\n\n")
	content.WriteString(typeLabel + " " + typeDisplay)
	content.WriteString("\n")
	content.WriteString(priorityLabel + " " + priorityDisplay)
	content.WriteString("\n\n")
	content.WriteString(descLabel)
	content.WriteString("\n")
	content.WriteString(p.createDescTextarea.View())
	content.WriteString("\n\n")

	// Render Ok and Cancel buttons
	okFocused := p.createDialogFocus == 4
	cancelFocused := p.createDialogFocus == 5
	okButton := styleButtonWithHover("  Ok  ", p.hoveredDialogButton == "ok" || okFocused)
	cancelButton := styleButtonWithHover("Cancel", p.hoveredDialogButton == "cancel" || cancelFocused)
	content.WriteString(okButton + "  " + cancelButton)
	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("[Tab] Next  [Enter/Space] Select"))

	return content.String()
}

// renderLinearImportContent renders the Linear import form
func (p *IssueDetailsPanel) renderLinearImportContent(visibleLines int) string {
	var content strings.Builder

	// Adapt textarea width
	inputWidth := p.width - detailsPanelPaddingVal
	if inputWidth < 20 {
		inputWidth = 20
	}
	p.linearImportInput.SetWidth(inputWidth)

	// Show focus labels
	issueIDsLabel := "Issue IDs/URLs:"
	createDepsLabel := "Create Dependencies:"
	updateLabel := "Update Existing:"
	dryRunLabel := "Dry Run:"
	maxDepthLabel := "Max Dependency Depth:"

	if p.linearImportFocus == 0 {
		issueIDsLabel = tuiValueStyle.Render("Issue IDs/URLs:") + " (one per line, Ctrl+Enter to submit)"
	}
	if p.linearImportFocus == 1 {
		createDepsLabel = tuiValueStyle.Render("Create Dependencies:") + " (space to toggle)"
	}
	if p.linearImportFocus == 2 {
		updateLabel = tuiValueStyle.Render("Update Existing:") + " (space to toggle)"
	}
	if p.linearImportFocus == 3 {
		dryRunLabel = tuiValueStyle.Render("Dry Run:") + " (space to toggle)"
	}
	if p.linearImportFocus == 4 {
		maxDepthLabel = tuiValueStyle.Render("Max Dependency Depth:") + " (+/- adjust)"
	}

	// Checkbox display
	createDepsCheck := " "
	updateCheck := " "
	dryRunCheck := " "
	if p.linearImportCreateDeps {
		createDepsCheck = "x"
	}
	if p.linearImportUpdate {
		updateCheck = "x"
	}
	if p.linearImportDryRun {
		dryRunCheck = "x"
	}

	content.WriteString(tuiLabelStyle.Render("Import from Linear (Bulk)"))
	content.WriteString("\n\n")
	content.WriteString(issueIDsLabel)
	content.WriteString("\n")
	content.WriteString(p.linearImportInput.View())
	content.WriteString("\n\n")
	content.WriteString(createDepsLabel + " [" + createDepsCheck + "]")
	content.WriteString("\n")
	content.WriteString(updateLabel + " [" + updateCheck + "]")
	content.WriteString("\n")
	content.WriteString(dryRunLabel + " [" + dryRunCheck + "]")
	content.WriteString("\n\n")
	content.WriteString(maxDepthLabel + " " + tuiValueStyle.Render(fmt.Sprintf("%d", p.linearImportMaxDepth)))
	content.WriteString("\n\n")

	// Render Ok and Cancel buttons
	okLabel := "  Ok  "
	cancelLabel := "Cancel"
	focusHint := ""

	if p.linearImportFocus == 5 {
		okLabel = tuiValueStyle.Render("[ Ok ]")
		focusHint = tuiDimStyle.Render(" (press Enter to import)")
	} else {
		okLabel = styleButtonWithHover("  Ok  ", p.hoveredDialogButton == "ok")
	}

	if p.linearImportFocus == 6 {
		cancelLabel = tuiValueStyle.Render("[Cancel]")
		focusHint = tuiDimStyle.Render(" (press Enter to cancel)")
	} else {
		cancelLabel = styleButtonWithHover("Cancel", p.hoveredDialogButton == "cancel")
	}

	content.WriteString(okLabel + "  " + cancelLabel + focusHint)
	content.WriteString("\n")

	if p.linearImporting {
		content.WriteString(tuiDimStyle.Render("Importing..."))
	} else {
		content.WriteString(tuiDimStyle.Render("[Tab] Next field  [Enter] Activate"))
	}

	return content.String()
}

// renderCreateWorkContent renders the work creation panel
func (p *IssueDetailsPanel) renderCreateWorkContent(visibleLines int) string {
	var content strings.Builder

	// Clear previous button positions
	p.dialogButtons = nil
	currentLine := 0

	// Panel header
	content.WriteString(tuiSuccessStyle.Render("Create Work"))
	content.WriteString("\n\n")
	currentLine += 2

	// Show bead info
	var beadInfo string
	if len(p.createWorkBeadIDs) == 1 {
		beadInfo = fmt.Sprintf("Creating work from issue: %s", issueIDStyle.Render(p.createWorkBeadIDs[0]))
	} else {
		beadInfo = fmt.Sprintf("Creating work from %d issues", len(p.createWorkBeadIDs))
		content.WriteString(beadInfo)
		content.WriteString("\n")
		currentLine++
		maxShow := 5
		if len(p.createWorkBeadIDs) < maxShow {
			maxShow = len(p.createWorkBeadIDs)
		}
		for i := 0; i < maxShow; i++ {
			content.WriteString("  • " + issueIDStyle.Render(p.createWorkBeadIDs[i]))
			content.WriteString("\n")
			currentLine++
		}
		if len(p.createWorkBeadIDs) > maxShow {
			content.WriteString(fmt.Sprintf("  ... and %d more", len(p.createWorkBeadIDs)-maxShow))
			content.WriteString("\n")
			currentLine++
		}
		content.WriteString("\n")
		currentLine++
	}

	if len(p.createWorkBeadIDs) == 1 {
		content.WriteString(beadInfo)
		content.WriteString("\n\n")
		currentLine += 2
	}

	// Branch name input
	branchLabel := "Branch name:"
	if p.createWorkField == 0 {
		branchLabel = tuiSuccessStyle.Render("Branch name:") + " " + tuiDimStyle.Render("(editing)")
	} else {
		branchLabel = tuiLabelStyle.Render("Branch name:")
	}
	content.WriteString(branchLabel)
	content.WriteString("\n")
	currentLine++
	content.WriteString(p.createWorkBranch.View())
	content.WriteString("\n\n")
	currentLine += 2

	// Action buttons
	content.WriteString("Actions:\n")
	currentLine++

	// Execute button
	executeStyle := tuiDimStyle
	executePrefix := "  "
	if p.createWorkField == 1 && p.createWorkButtonIdx == 0 {
		executeStyle = tuiSelectedStyle
		executePrefix = "► "
	} else if p.hoveredDialogButton == "execute" {
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
	if p.createWorkField == 1 && p.createWorkButtonIdx == 1 {
		autoStyle = tuiSelectedStyle
		autoPrefix = "► "
	} else if p.hoveredDialogButton == "auto" {
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
	if p.createWorkField == 1 && p.createWorkButtonIdx == 2 {
		cancelStyle = tuiSelectedStyle
		cancelPrefix = "► "
	} else if p.hoveredDialogButton == "cancel" {
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
	content.WriteString(tuiDimStyle.Render("Navigation: [Tab/Shift+Tab] Switch field  [↑↓/jk] Select button  [Enter] Confirm  [Esc] Cancel"))

	return content.String()
}

// renderAddToWorkContent renders the add-to-work selection
func (p *IssueDetailsPanel) renderAddToWorkContent(visibleLines int) string {
	var content strings.Builder

	// Collect selected beads
	var selectedBeads []beadItem
	for _, item := range p.beadItems {
		if item.selected {
			selectedBeads = append(selectedBeads, item)
		}
	}

	// If no selected beads, use cursor bead
	if len(selectedBeads) == 0 && len(p.beadItems) > 0 && p.cursor < len(p.beadItems) {
		selectedBeads = append(selectedBeads, p.beadItems[p.cursor])
	}

	// Header
	if len(selectedBeads) == 1 {
		content.WriteString(tuiLabelStyle.Render("Add Issue to Work"))
	} else {
		content.WriteString(tuiLabelStyle.Render(fmt.Sprintf("Add %d Issues to Work", len(selectedBeads))))
	}
	content.WriteString("\n\n")

	// Show which issues we're adding
	if len(selectedBeads) == 1 {
		content.WriteString(tuiDimStyle.Render("Issue: "))
		content.WriteString(issueIDStyle.Render(selectedBeads[0].id))
		content.WriteString("\n")
		titleStyle := tuiValueStyle.Width(p.width - detailsPanelPaddingVal)
		content.WriteString(titleStyle.Render(selectedBeads[0].title))
		content.WriteString("\n")
	} else if len(selectedBeads) > 1 {
		content.WriteString(tuiDimStyle.Render("Issues:\n"))
		for i, bead := range selectedBeads {
			if i >= 5 {
				content.WriteString(tuiDimStyle.Render(fmt.Sprintf("  ... and %d more\n", len(selectedBeads)-5)))
				break
			}
			content.WriteString("  ")
			content.WriteString(issueIDStyle.Render(bead.id))
			content.WriteString(": ")
			titleStyle := tuiValueStyle.Width(p.width - detailsPanelPaddingVal - len(bead.id) - 4)
			content.WriteString(titleStyle.Render(bead.title))
			content.WriteString("\n")
		}
	}
	content.WriteString("\n")

	// Works list header
	content.WriteString(tuiLabelStyle.Render("Select a work:"))
	content.WriteString("\n")

	if len(p.availableWorks) == 0 {
		content.WriteString(tuiDimStyle.Render("  No available works found."))
		content.WriteString("\n")
		content.WriteString(tuiDimStyle.Render("  Create a work first with 'w'."))
	} else {
		// Calculate how many works we can show
		linesUsed := 7
		maxWorks := visibleLines - linesUsed
		if maxWorks < 3 {
			maxWorks = 3
		}

		// Show works with scrolling if needed
		start := 0
		if p.worksCursor >= maxWorks {
			start = p.worksCursor - maxWorks + 1
		}
		end := min(start+maxWorks, len(p.availableWorks))

		for i := start; i < end; i++ {
			work := p.availableWorks[i]

			var lineStyle lipgloss.Style
			prefix := "  "
			if i == p.worksCursor {
				prefix = "► "
				lineStyle = tuiSelectedStyle
			} else {
				lineStyle = tuiDimStyle
			}

			var workLine strings.Builder
			workLine.WriteString(prefix)
			workLine.WriteString(work.id)
			workLine.WriteString(" (")
			workLine.WriteString(work.status)
			workLine.WriteString(")")

			if work.rootIssueID != "" {
				workLine.WriteString("\n    ")
				workLine.WriteString("Root: ")
				workLine.WriteString(work.rootIssueID)
				if work.rootIssueTitle != "" {
					title := work.rootIssueTitle
					maxTitleLen := p.width - detailsPanelPaddingVal - 12
					if len(title) > maxTitleLen && maxTitleLen > 10 {
						title = title[:maxTitleLen-3] + "..."
					}
					workLine.WriteString(" - ")
					workLine.WriteString(title)
				}
			}

			workLine.WriteString("\n    ")
			workLine.WriteString("Branch: ")
			branch := work.branch
			maxBranchLen := p.width - detailsPanelPaddingVal - 12
			if len(branch) > maxBranchLen && maxBranchLen > 10 {
				branch = branch[:maxBranchLen-3] + "..."
			}
			workLine.WriteString(branch)

			if i == p.worksCursor {
				content.WriteString(lineStyle.Render(workLine.String()))
			} else {
				content.WriteString(workLine.String())
			}
			content.WriteString("\n")
		}

		if len(p.availableWorks) > maxWorks {
			if start > 0 {
				content.WriteString(tuiDimStyle.Render("  ↑ more above"))
				content.WriteString("\n")
			}
			if end < len(p.availableWorks) {
				content.WriteString(tuiDimStyle.Render("  ↓ more below"))
				content.WriteString("\n")
			}
		}
	}

	content.WriteString("\n")
	content.WriteString(tuiDimStyle.Render("[↑↓/jk] Navigate  [Enter] Add to work  [Esc] Cancel"))

	return content.String()
}

// DetectDialogButton determines which dialog button is at the given position
func (p *IssueDetailsPanel) DetectDialogButton(x, y, detailsPanelStartX int) string {
	if p.viewMode != ViewCreateBead && p.viewMode != ViewCreateBeadInline &&
		p.viewMode != ViewAddChildBead && p.viewMode != ViewEditBead &&
		p.viewMode != ViewLinearImportInline && p.viewMode != ViewCreateWork {
		return ""
	}

	// Check if mouse is in the details panel
	if x < detailsPanelStartX {
		return ""
	}

	// Handle ViewCreateWork using tracked button positions
	if p.viewMode == ViewCreateWork {
		buttonAreaX := x - detailsPanelStartX

		for _, button := range p.dialogButtons {
			absoluteY := button.Y + 2 // +2 for border+title
			if y == absoluteY && buttonAreaX >= button.StartX && buttonAreaX <= button.EndX {
				return button.ID
			}
		}
		return ""
	}

	// For other forms, calculate button positions based on form structure
	formStartY := 2
	var linesBeforeButtons int

	if p.viewMode == ViewLinearImportInline {
		linesBeforeButtons = 1 + 1 + 1 + 4 + 1 + 1 + 1 + 1 + 1 + 1 + 1 // header, blank, label, textarea, etc.
	} else {
		linesBeforeButtons = 1
		if p.parentBeadID != "" {
			linesBeforeButtons++
		}
		linesBeforeButtons += 1 + 1 + 1 + 2 + 1 + 2 + 4 + 1 // blank, title label/input, type/priority, desc
	}

	buttonRowY := formStartY + linesBeforeButtons
	if y != buttonRowY {
		return ""
	}

	// Calculate X position of buttons
	buttonAreaX := x - detailsPanelStartX - 2

	if buttonAreaX >= 0 && buttonAreaX < 6 {
		return "ok"
	}
	if buttonAreaX >= 8 && buttonAreaX < 14 {
		return "cancel"
	}

	return ""
}
