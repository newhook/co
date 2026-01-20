package cmd

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
)

// WorkOverlayAction represents an action result from the panel
type WorkOverlayAction int

const (
	WorkOverlayActionNone WorkOverlayAction = iota
	WorkOverlayActionCancel
	WorkOverlayActionSelect      // Select the currently highlighted work (Enter)
	WorkOverlayActionDestroy     // Destroy selected work (d)
	WorkOverlayActionToggleFocus // Toggle focus between overlay and issues (Tab)
)

// WorkOverlayPanel renders the work overlay dropdown with work tiles.
type WorkOverlayPanel struct {
	// Dimensions
	width  int
	height int

	// Focus state
	focused bool

	// Data
	workTiles          []*workProgress
	selectedWorkTileID string
	loading            bool
}

// NewWorkOverlayPanel creates a new WorkOverlayPanel
func NewWorkOverlayPanel() *WorkOverlayPanel {
	return &WorkOverlayPanel{
		width:  80,
		height: 24,
	}
}

// Init initializes the panel
func (p *WorkOverlayPanel) Init() tea.Cmd {
	// Auto-select first work if none selected
	if p.selectedWorkTileID == "" && len(p.workTiles) > 0 {
		p.selectedWorkTileID = p.workTiles[0].work.ID
	}
	return nil
}

// Reset resets the panel state
func (p *WorkOverlayPanel) Reset() {
	p.selectedWorkTileID = ""
	p.loading = false
}

// Update handles key events and returns an action
func (p *WorkOverlayPanel) Update(msg tea.KeyMsg) (tea.Cmd, WorkOverlayAction) {
	switch msg.Type {
	case tea.KeyEsc:
		return nil, WorkOverlayActionCancel
	case tea.KeyTab:
		return nil, WorkOverlayActionToggleFocus
	case tea.KeyEnter:
		if p.focused && p.selectedWorkTileID != "" {
			return nil, WorkOverlayActionSelect
		}
		return nil, WorkOverlayActionNone
	}

	// Navigation
	switch msg.String() {
	case "tab":
		return nil, WorkOverlayActionToggleFocus
	case "j", "down":
		if p.focused {
			p.NavigateDown()
		}
		return nil, WorkOverlayActionNone
	case "k", "up":
		if p.focused {
			p.NavigateUp()
		}
		return nil, WorkOverlayActionNone
	case "d":
		if p.selectedWorkTileID != "" {
			return nil, WorkOverlayActionDestroy
		}
		return nil, WorkOverlayActionNone
	case "h", "left":
		// For grid layout in future - for now, same as up
		if p.focused {
			p.NavigateUp()
		}
		return nil, WorkOverlayActionNone
	case "l", "right":
		// For grid layout in future - for now, same as down
		if p.focused {
			p.NavigateDown()
		}
		return nil, WorkOverlayActionNone
	}

	return nil, WorkOverlayActionNone
}

// SetSize updates the panel dimensions
func (p *WorkOverlayPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// SetFocus updates the focus state
func (p *WorkOverlayPanel) SetFocus(focused bool) {
	p.focused = focused
}

// IsFocused returns whether the panel is focused
func (p *WorkOverlayPanel) IsFocused() bool {
	return p.focused
}

// SetWorkTiles updates the work tiles, preserving selection if possible
func (p *WorkOverlayPanel) SetWorkTiles(workTiles []*workProgress) {
	p.workTiles = workTiles
	// Auto-select first work if current selection is invalid
	if p.selectedWorkTileID != "" {
		found := false
		for _, work := range p.workTiles {
			if work != nil && work.work.ID == p.selectedWorkTileID {
				found = true
				break
			}
		}
		if !found {
			p.selectedWorkTileID = ""
		}
	}
	if p.selectedWorkTileID == "" && len(p.workTiles) > 0 {
		p.selectedWorkTileID = p.workTiles[0].work.ID
	}
}

// ClearSelection clears the selected work tile
func (p *WorkOverlayPanel) ClearSelection() {
	p.selectedWorkTileID = ""
}

// SetLoading updates the loading state
func (p *WorkOverlayPanel) SetLoading(loading bool) {
	p.loading = loading
}

// GetSelectedWorkTileID returns the currently selected work tile ID
func (p *WorkOverlayPanel) GetSelectedWorkTileID() string {
	return p.selectedWorkTileID
}

// SetSelectedWorkTileID sets the selected work tile ID
func (p *WorkOverlayPanel) SetSelectedWorkTileID(id string) {
	p.selectedWorkTileID = id
}

// GetWorkTiles returns the work tiles
func (p *WorkOverlayPanel) GetWorkTiles() []*workProgress {
	return p.workTiles
}

// FindWorkByID finds a work by its ID, returns nil if not found
func (p *WorkOverlayPanel) FindWorkByID(id string) *workProgress {
	for _, work := range p.workTiles {
		if work != nil && work.work.ID == id {
			return work
		}
	}
	return nil
}

// CalculateHeight returns the height of the overlay dropdown
func (p *WorkOverlayPanel) CalculateHeight() int {
	dropdownHeight := int(float64(p.height) * 0.4)
	if dropdownHeight < 12 {
		dropdownHeight = 12
	} else if dropdownHeight > 25 {
		dropdownHeight = 25
	}
	return dropdownHeight
}

// Render returns the work overlay dropdown content
func (p *WorkOverlayPanel) Render() string {
	dropdownHeight := p.CalculateHeight()

	// Create dropdown panel style with highlight when focused
	borderColor := "240"
	if p.focused {
		borderColor = "214"
	}
	dropdownStyle := lipgloss.NewStyle().
		Width(p.width).
		Height(dropdownHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Background(lipgloss.Color("235"))

	if p.loading {
		loadingContent := lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true).
			Align(lipgloss.Center, lipgloss.Center).
			Width(p.width - 2).
			Height(dropdownHeight - 2).
			Render("Loading works...")
		return dropdownStyle.Render(loadingContent)
	}

	var content strings.Builder

	// Header bar
	headerBar := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("255")).
		Bold(true).
		Width(p.width - 2).
		Padding(0, 1).
		Render("Work Overview                                                  [Esc] Close")
	content.WriteString(headerBar)
	content.WriteString("\n")

	// Calculate available space for work items
	availableLines := dropdownHeight - 3
	worksPerPage := availableLines / 3 // Each work takes 3 lines

	if len(p.workTiles) == 0 {
		emptyMsg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true).
			Align(lipgloss.Center).
			Width(p.width - 2).
			Height(availableLines).
			Render("No works found. Press 'c' to create a new work.")
		content.WriteString(emptyMsg)
	} else {
		// Find selected index
		selectedIndex := -1
		for i, work := range p.workTiles {
			if work.work.ID == p.selectedWorkTileID {
				selectedIndex = i
				break
			}
		}

		// Calculate visible window
		startIdx := 0
		if selectedIndex >= worksPerPage {
			startIdx = selectedIndex - worksPerPage/2
			if startIdx < 0 {
				startIdx = 0
			}
		}
		endIdx := startIdx + worksPerPage
		if endIdx > len(p.workTiles) {
			endIdx = len(p.workTiles)
			if endIdx-startIdx < worksPerPage && len(p.workTiles) >= worksPerPage {
				startIdx = endIdx - worksPerPage
				if startIdx < 0 {
					startIdx = 0
				}
			}
		}

		// Render visible works
		for i := startIdx; i < endIdx; i++ {
			work := p.workTiles[i]
			if work == nil {
				continue
			}

			isSelected := work.work.ID == p.selectedWorkTileID
			content.WriteString(p.renderWorkTile(work, isSelected))
		}

		// Scroll indicator
		if len(p.workTiles) > worksPerPage {
			scrollInfo := fmt.Sprintf("\n  (Showing %d-%d of %d works)", startIdx+1, endIdx, len(p.workTiles))
			content.WriteString(tuiDimStyle.Render(scrollInfo))
		}
	}

	return dropdownStyle.Render(content.String())
}

// renderWorkTile renders a single work tile
func (p *WorkOverlayPanel) renderWorkTile(work *workProgress, isSelected bool) string {
	var content strings.Builder

	// === Line 1: Main info ===
	var line1 strings.Builder

	if isSelected {
		line1.WriteString(tuiSuccessStyle.Render(">"))
	} else {
		line1.WriteString(" ")
	}
	line1.WriteString(" ")

	// Status icon
	line1.WriteString(statusIcon(work.work.Status))
	line1.WriteString(" ")

	// Work ID
	idStyle := lipgloss.NewStyle().Bold(true)
	if isSelected {
		idStyle = idStyle.Foreground(lipgloss.Color("214"))
	}
	line1.WriteString(idStyle.Render(work.work.ID))
	line1.WriteString(" ")

	// Friendly name
	if work.work.Name != "" {
		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
		line1.WriteString(nameStyle.Render(work.work.Name))
		line1.WriteString(" ")
	}

	// Status text
	statusTextStyle := lipgloss.NewStyle()
	switch work.work.Status {
	case db.StatusCompleted:
		statusTextStyle = statusTextStyle.Foreground(lipgloss.Color("82"))
	case db.StatusProcessing:
		statusTextStyle = statusTextStyle.Foreground(lipgloss.Color("214"))
	case db.StatusFailed:
		statusTextStyle = statusTextStyle.Foreground(lipgloss.Color("196"))
	default:
		statusTextStyle = statusTextStyle.Foreground(lipgloss.Color("247"))
	}
	line1.WriteString(statusTextStyle.Render(fmt.Sprintf("[%s]", work.work.Status)))
	line1.WriteString(" ")

	// Created time
	if work.work.CreatedAt.Unix() > 0 {
		timeAgo := time.Since(work.work.CreatedAt)
		var timeStr string
		if timeAgo.Hours() < 1 {
			timeStr = fmt.Sprintf("%dm ago", int(timeAgo.Minutes()))
		} else if timeAgo.Hours() < 24 {
			timeStr = fmt.Sprintf("%dh ago", int(timeAgo.Hours()))
		} else {
			days := int(timeAgo.Hours() / 24)
			timeStr = fmt.Sprintf("%dd ago", days)
		}
		timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		line1.WriteString(timeStyle.Render(fmt.Sprintf("Created %s", timeStr)))
	}

	content.WriteString(line1.String())
	content.WriteString("\n")

	// === Line 2: Branch and progress ===
	var line2 strings.Builder
	line2.WriteString("   ")

	// Branch name
	branchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
	branch := truncateString(work.work.BranchName, 50)
	line2.WriteString(branchStyle.Render(fmt.Sprintf("@ %s", branch)))
	line2.WriteString("  ")

	// Progress percentage
	completedTasks := 0
	for _, task := range work.tasks {
		if task.task.Status == db.StatusCompleted {
			completedTasks++
		}
	}

	percentage := 0
	if len(work.tasks) > 0 {
		percentage = (completedTasks * 100) / len(work.tasks)
	}

	progressStyle := lipgloss.NewStyle().Bold(true)
	if percentage == 100 {
		progressStyle = progressStyle.Foreground(lipgloss.Color("82"))
	} else if percentage >= 75 {
		progressStyle = progressStyle.Foreground(lipgloss.Color("226"))
	} else if percentage >= 50 {
		progressStyle = progressStyle.Foreground(lipgloss.Color("214"))
	} else {
		progressStyle = progressStyle.Foreground(lipgloss.Color("247"))
	}
	line2.WriteString(progressStyle.Render(fmt.Sprintf("%d%%", percentage)))
	line2.WriteString(" ")
	line2.WriteString(fmt.Sprintf("(%d/%d tasks)", completedTasks, len(work.tasks)))

	// Warnings/alerts
	if work.unassignedBeadCount > 0 {
		warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		line2.WriteString(" ")
		line2.WriteString(warningStyle.Render(fmt.Sprintf("! %d unassigned", work.unassignedBeadCount)))
	}
	if work.feedbackCount > 0 {
		alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		line2.WriteString(" ")
		line2.WriteString(alertStyle.Render(fmt.Sprintf("* %d feedback", work.feedbackCount)))
	}

	content.WriteString(line2.String())
	content.WriteString("\n")

	// === Line 3: Root issue details ===
	if work.work.RootIssueID != "" {
		var line3 strings.Builder
		line3.WriteString("   ")

		var rootTitle string
		for _, bead := range work.workBeads {
			if bead.id == work.work.RootIssueID {
				rootTitle = bead.title
				break
			}
		}

		if rootTitle == "" && len(work.workBeads) > 0 {
			rootTitle = work.workBeads[0].title
		}

		if rootTitle != "" {
			issueStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("247")).
				Italic(true)
			rootTitle = truncateString(rootTitle, 70)
			line3.WriteString(issueStyle.Render(fmt.Sprintf("# %s", rootTitle)))
		} else {
			issueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			line3.WriteString(issueStyle.Render(fmt.Sprintf("Root: %s", work.work.RootIssueID)))
		}
		content.WriteString(line3.String())
		content.WriteString("\n")
	} else {
		content.WriteString("\n")
	}

	return content.String()
}

// NavigateUp moves selection to the previous work tile
func (p *WorkOverlayPanel) NavigateUp() {
	if len(p.workTiles) == 0 {
		return
	}

	currentIdx := -1
	for i, work := range p.workTiles {
		if work.work.ID == p.selectedWorkTileID {
			currentIdx = i
			break
		}
	}

	if currentIdx > 0 {
		p.selectedWorkTileID = p.workTiles[currentIdx-1].work.ID
	} else if currentIdx == -1 && len(p.workTiles) > 0 {
		p.selectedWorkTileID = p.workTiles[len(p.workTiles)-1].work.ID
	}
}

// NavigateDown moves selection to the next work tile
func (p *WorkOverlayPanel) NavigateDown() {
	if len(p.workTiles) == 0 {
		return
	}

	currentIdx := -1
	for i, work := range p.workTiles {
		if work.work.ID == p.selectedWorkTileID {
			currentIdx = i
			break
		}
	}

	if currentIdx < len(p.workTiles)-1 {
		if currentIdx == -1 {
			p.selectedWorkTileID = p.workTiles[0].work.ID
		} else {
			p.selectedWorkTileID = p.workTiles[currentIdx+1].work.ID
		}
	}
}
