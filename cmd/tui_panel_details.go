package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

const detailsPanelPaddingVal = 4

// IssueDetailsPanel renders issue details for the focused bead.
type IssueDetailsPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Data (set by coordinator)
	focusedBead      *beadItem
	hasActiveSession bool
	childBeadMap     map[string]*beadItem // For looking up child status
}

// NewIssueDetailsPanel creates a new IssueDetailsPanel
func NewIssueDetailsPanel() *IssueDetailsPanel {
	return &IssueDetailsPanel{
		width:        60,
		height:       20,
		childBeadMap: make(map[string]*beadItem),
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

// SetData updates the panel's data with the focused bead
func (p *IssueDetailsPanel) SetData(focusedBead *beadItem, hasActiveSession bool, childBeadMap map[string]*beadItem) {
	p.focusedBead = focusedBead
	p.hasActiveSession = hasActiveSession
	p.childBeadMap = childBeadMap
}

// Render returns the details panel content (without border/panel styling)
func (p *IssueDetailsPanel) Render(visibleLines int) string {
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

	if p.focusedBead == nil {
		content.WriteString(tuiDimStyle.Render("No issue selected"))
		return content.String()
	}

	bead := p.focusedBead

	content.WriteString(tuiLabelStyle.Render("ID: "))
	content.WriteString(tuiValueStyle.Render(bead.ID))
	content.WriteString("  ")
	content.WriteString(tuiLabelStyle.Render("Type: "))
	content.WriteString(tuiValueStyle.Render(bead.Type))
	content.WriteString("  ")
	content.WriteString(tuiLabelStyle.Render("P"))
	content.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d", bead.Priority)))
	content.WriteString("  ")
	content.WriteString(tuiLabelStyle.Render("Status: "))
	content.WriteString(tuiValueStyle.Render(bead.Status))
	if p.hasActiveSession {
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
	content.WriteString(titleStyle.Render(bead.Title))

	// Calculate remaining lines for description and children
	linesUsed := 2 // header + title
	remainingLines := visibleLines - linesUsed

	// Show description if we have room
	if bead.Description != "" && remainingLines > 2 {
		content.WriteString("\n")
		descStyle := tuiDimStyle.Width(p.width - detailsPanelPaddingVal)
		desc := bead.Description
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

		// Show children with status
		maxChildren := min(len(bead.children), remainingLines)
		for i := range maxChildren {
			childID := bead.children[i]
			var childLine string
			if child, ok := p.childBeadMap[childID]; ok {
				childLine = fmt.Sprintf("\n  %s %s %s",
					statusIcon(child.Status),
					issueIDStyle.Render(child.ID),
					child.Title)
			} else {
				childLine = fmt.Sprintf("\n  ? %s", issueIDStyle.Render(childID))
			}
			if lipgloss.Width(childLine) > p.width {
				childLine = truncate.StringWithTail(childLine, uint(p.width), "...")
			}
			content.WriteString(childLine)
		}
		if len(bead.children) > maxChildren {
			fmt.Fprintf(&content, "\n  ... and %d more", len(bead.children)-maxChildren)
		}
	}

	return content.String()
}
