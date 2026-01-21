package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/reflow/wordwrap"
)

// Panel padding: tuiPanelStyle has Padding(0, 1) = 2 chars horizontal padding total

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

	// Ensure content is exactly the right number of lines to prevent layout overflow
	detailsContent = padOrTruncateLinesDetails(detailsContent, detailsContentLines)

	panelStyle := tuiPanelStyle.Width(p.width).Height(contentHeight - 2)
	if p.focused {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("214"))
	}

	result := panelStyle.Render(tuiTitleStyle.Render("Details") + "\n" + detailsContent)

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

// padOrTruncateLinesDetails ensures the content has exactly targetLines lines
func padOrTruncateLinesDetails(content string, targetLines int) string {
	if targetLines < 1 {
		targetLines = 1
	}

	lines := strings.Split(content, "\n")
	// Remove trailing empty line if present (from trailing \n)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if len(lines) > targetLines {
		// Truncate
		lines = lines[:targetLines]
	} else if len(lines) < targetLines {
		// Pad with empty lines
		for len(lines) < targetLines {
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}

// renderIssueDetails renders the normal issue details view
func (p *IssueDetailsPanel) renderIssueDetails(visibleLines int) string {
	var content strings.Builder

	if p.focusedBead == nil {
		content.WriteString(tuiDimStyle.Render("No issue selected"))
		return content.String()
	}

	bead := p.focusedBead

	// Calculate inner width (panel has Padding(0, 1) = 2 chars total horizontal padding)
	innerWidth := p.width - 2

	// Build header line - may need truncation to fit
	var header strings.Builder
	header.WriteString(tuiLabelStyle.Render("ID: "))
	header.WriteString(tuiValueStyle.Render(bead.ID))
	header.WriteString("  ")
	header.WriteString(tuiLabelStyle.Render("Type: "))
	header.WriteString(tuiValueStyle.Render(bead.Type))
	header.WriteString("  ")
	header.WriteString(tuiLabelStyle.Render("P"))
	header.WriteString(tuiValueStyle.Render(fmt.Sprintf("%d", bead.Priority)))
	header.WriteString("  ")
	header.WriteString(tuiLabelStyle.Render("Status: "))
	header.WriteString(tuiValueStyle.Render(bead.Status))
	if p.hasActiveSession {
		header.WriteString("  ")
		header.WriteString(tuiSuccessStyle.Render("[Session Active]"))
	}
	if bead.assignedWorkID != "" {
		header.WriteString("  ")
		header.WriteString(tuiDimStyle.Render("Work: " + bead.assignedWorkID))
	}

	// Truncate header to fit inner width
	headerStr := header.String()
	if lipgloss.Width(headerStr) > innerWidth {
		headerStr = truncate.StringWithTail(headerStr, uint(innerWidth), "...")
	}
	content.WriteString(headerStr)
	content.WriteString("\n")

	// Truncate title to fit on one line (use innerWidth which accounts for panel padding)
	titleStr := bead.Title
	if lipgloss.Width(titleStr) > innerWidth {
		titleStr = truncate.StringWithTail(titleStr, uint(innerWidth), "...")
	}
	content.WriteString(tuiValueStyle.Render(titleStr))

	// Calculate remaining lines for description and children
	linesUsed := 2 // header + title
	remainingLines := visibleLines - linesUsed

	// Show description if we have room
	if bead.Description != "" && remainingLines > 2 {
		content.WriteString("\n")
		// Word wrap description to fit within inner width
		desc := bead.Description
		descLines := remainingLines - 2
		if len(bead.children) > 0 {
			descLines = min(descLines, 3)
		}
		// Word wrap the description to innerWidth
		wrapped := wordwrap.String(desc, innerWidth)
		wrappedLines := strings.Split(wrapped, "\n")
		// Limit to descLines
		if len(wrappedLines) > descLines {
			wrappedLines = wrappedLines[:descLines]
			// Add ellipsis to last line if truncated
			lastLine := wrappedLines[len(wrappedLines)-1]
			if lipgloss.Width(lastLine)+3 <= innerWidth {
				wrappedLines[len(wrappedLines)-1] = lastLine + "..."
			} else {
				wrappedLines[len(wrappedLines)-1] = truncate.StringWithTail(lastLine, uint(innerWidth), "...")
			}
		}
		descStr := tuiDimStyle.Render(strings.Join(wrappedLines, "\n"))
		content.WriteString(descStr)
		linesUsed += len(wrappedLines)
		remainingLines -= len(wrappedLines)
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
			// Truncate to fit inner width (minus 1 for the leading \n that doesn't count toward width)
			if lipgloss.Width(childLine)-1 > innerWidth {
				childLine = truncate.StringWithTail(childLine, uint(innerWidth+1), "...")
			}
			content.WriteString(childLine)
		}
		if len(bead.children) > maxChildren {
			fmt.Fprintf(&content, "\n  ... and %d more", len(bead.children)-maxChildren)
		}
	}

	return content.String()
}
