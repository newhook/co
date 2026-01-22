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

	// Scrolling
	scrollOffset  int // Current scroll position
	totalLines    int // Total content lines (for scroll calculation)

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
	// Reset scroll when switching beads
	p.scrollOffset = 0
}

// ScrollUp scrolls the content up (shows earlier content)
func (p *IssueDetailsPanel) ScrollUp() {
	if p.scrollOffset > 0 {
		p.scrollOffset--
	}
}

// ScrollDown scrolls the content down (shows later content)
func (p *IssueDetailsPanel) ScrollDown() {
	visibleLines := p.height - 3 // Account for border and title
	if p.scrollOffset < p.totalLines-visibleLines {
		p.scrollOffset++
	}
}

// ScrollToTop scrolls to the beginning of the content
func (p *IssueDetailsPanel) ScrollToTop() {
	p.scrollOffset = 0
}

// ScrollToBottom scrolls to the end of the content
func (p *IssueDetailsPanel) ScrollToBottom() {
	visibleLines := p.height - 3
	if p.totalLines > visibleLines {
		p.scrollOffset = p.totalLines - visibleLines
	} else {
		p.scrollOffset = 0
	}
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

// renderIssueDetails renders the normal issue details view with scrolling support
func (p *IssueDetailsPanel) renderIssueDetails(visibleLines int) string {
	if p.focusedBead == nil {
		return tuiDimStyle.Render("No issue selected")
	}

	// First, render all content without line limit to get total lines
	fullContent := p.renderFullIssueContent()

	// Split into lines and update total line count
	lines := strings.Split(fullContent, "\n")
	p.totalLines = len(lines)

	// Apply scrolling
	if p.scrollOffset < 0 {
		p.scrollOffset = 0
	}
	maxScroll := max(0, p.totalLines-visibleLines)
	if p.scrollOffset > maxScroll {
		p.scrollOffset = maxScroll
	}

	// Extract visible portion
	startLine := p.scrollOffset
	endLine := min(startLine+visibleLines, len(lines))

	if startLine >= len(lines) {
		return ""
	}

	visibleContent := lines[startLine:endLine]

	// Add scroll indicators if needed
	if p.totalLines > visibleLines {
		// Modify last visible line to show scroll position
		if len(visibleContent) > 0 {
			lastIdx := len(visibleContent) - 1
			scrollInfo := p.getScrollIndicator(visibleLines)
			// Append scroll indicator to the last line
			innerWidth := p.width - 2
			lastLine := visibleContent[lastIdx]
			// Truncate line if needed to fit scroll indicator
			if lipgloss.Width(lastLine)+lipgloss.Width(scrollInfo) > innerWidth {
				lastLine = truncate.StringWithTail(lastLine, uint(innerWidth-lipgloss.Width(scrollInfo)-1), "...")
			}
			// Right-align the scroll indicator
			padding := innerWidth - lipgloss.Width(lastLine) - lipgloss.Width(scrollInfo)
			if padding > 0 {
				visibleContent[lastIdx] = lastLine + strings.Repeat(" ", padding) + scrollInfo
			} else {
				visibleContent[lastIdx] = lastLine + " " + scrollInfo
			}
		}
	}

	return strings.Join(visibleContent, "\n")
}

// renderFullIssueContent renders all content without line limits
func (p *IssueDetailsPanel) renderFullIssueContent() string {
	var content strings.Builder
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

	// Truncate title to fit on one line
	titleStr := bead.Title
	if lipgloss.Width(titleStr) > innerWidth {
		titleStr = truncate.StringWithTail(titleStr, uint(innerWidth), "...")
	}
	content.WriteString(tuiValueStyle.Render(titleStr))

	// Show full description
	if bead.Description != "" {
		content.WriteString("\n\n")
		// Word wrap description to fit within inner width
		wrapped := wordwrap.String(bead.Description, innerWidth)
		content.WriteString(tuiDimStyle.Render(wrapped))
	}

	// Show all children (issues blocked by this one)
	if len(bead.children) > 0 {
		content.WriteString("\n\n")
		content.WriteString(tuiLabelStyle.Render("Blocks:"))

		// Show all children with status
		for i, childID := range bead.children {
			var childLine string
			if child, ok := p.childBeadMap[childID]; ok {
				childLine = fmt.Sprintf("\n  %s %s %s",
					statusIcon(child.Status),
					issueIDStyle.Render(child.ID),
					child.Title)
			} else {
				childLine = fmt.Sprintf("\n  ? %s", issueIDStyle.Render(childID))
			}
			// Truncate to fit inner width
			if lipgloss.Width(childLine)-1 > innerWidth {
				childLine = truncate.StringWithTail(childLine, uint(innerWidth+1), "...")
			}
			content.WriteString(childLine)
			_ = i // avoid unused variable warning
		}
	}

	return content.String()
}

// getScrollIndicator returns a scroll position indicator
func (p *IssueDetailsPanel) getScrollIndicator(visibleLines int) string {
	if p.totalLines <= visibleLines {
		return ""
	}

	// Calculate position as percentage
	scrollPercentage := 0
	if p.totalLines > visibleLines {
		scrollPercentage = (p.scrollOffset * 100) / (p.totalLines - visibleLines)
	}

	// Create indicator
	indicator := fmt.Sprintf("▲%d%%▼", scrollPercentage)
	if p.scrollOffset == 0 {
		indicator = "▼" // At top, can only scroll down
	} else if p.scrollOffset >= p.totalLines-visibleLines {
		indicator = "▲" // At bottom, can only scroll up
	}

	return tuiDimStyle.Render(indicator)
}
