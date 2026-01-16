package cmd

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
)

// refreshData creates a tea.Cmd that refreshes bead data
func (m *planModel) refreshData() tea.Cmd {
	// Capture current filter and sequence at creation time to avoid race conditions
	filters := m.filters
	seq := m.searchSeq
	return m.refreshDataWithFilters(filters, seq)
}

// refreshDataWithFilters creates a refresh command with captured filter values.
// This prevents race conditions when the user types quickly.
func (m *planModel) refreshDataWithFilters(filters beadFilters, seq uint64) tea.Cmd {
	return func() tea.Msg {
		items, err := m.loadBeadsWithFilters(filters)

		// Also fetch active sessions
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)

		return planDataMsg{
			beads:          items,
			activeSessions: activeSessions,
			err:            err,
			searchSeq:      seq,
		}
	}
}

func (m *planModel) loadBeads() ([]beadItem, error) {
	return m.loadBeadsWithFilters(m.filters)
}

// loadBeadsWithFilters loads beads using the provided filters.
// This allows capturing filters at command creation time to avoid race conditions.
func (m *planModel) loadBeadsWithFilters(filters beadFilters) ([]beadItem, error) {
	mainRepoPath := m.proj.MainRepoPath()

	// Use the shared fetchBeadsWithFilters function
	items, err := fetchBeadsWithFilters(mainRepoPath, filters)
	if err != nil {
		return nil, err
	}

	// Fetch assigned beads from database and populate assignedWorkID
	assignedBeads, err := m.proj.DB.GetAllAssignedBeads(m.ctx)
	if err == nil {
		for i := range items {
			if workID, ok := assignedBeads[items[i].id]; ok {
				items[i].assignedWorkID = workID
			}
		}
	}

	// Build tree structure from dependencies
	// When search is active, skip fetching parent beads to avoid adding unfiltered items
	items = buildBeadTree(m.ctx, items, m.beadsClient, mainRepoPath, filters.searchText)

	// If no tree structure, apply regular sorting
	hasTree := false
	for _, item := range items {
		if item.treeDepth > 0 || item.dependentCount > 0 {
			hasTree = true
			break
		}
	}

	if !hasTree {
		// Fall back to regular sorting if no tree structure
		switch filters.sortBy {
		case "priority":
			sort.Slice(items, func(i, j int) bool {
				return items[i].priority < items[j].priority
			})
		case "title":
			sort.Slice(items, func(i, j int) bool {
				return items[i].title < items[j].title
			})
		}
	}

	return items, nil
}

func (m *planModel) createBead(title, beadType string, priority int, isEpic bool, description string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		mainRepoPath := m.proj.MainRepoPath()

		_, err := beads.Create(ctx, mainRepoPath, beads.CreateOptions{
			Title:       title,
			Type:        beadType,
			Priority:    priority,
			IsEpic:      isEpic,
			Description: description,
		})
		if err != nil {
			return planDataMsg{err: fmt.Errorf("failed to create issue: %w", err)}
		}

		// Refresh after creation
		items, err := m.loadBeads()
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}

func (m *planModel) closeBead(beadID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		mainRepoPath := m.proj.MainRepoPath()
		session := m.sessionName()
		tabName := db.TabNameForBead(beadID)

		// If there's an active session for this bead, close it
		if m.activeBeadSessions[beadID] {
			// Terminate and close the tab
			_ = m.zj.TerminateAndCloseTab(m.ctx, session, tabName)
			// Unregister from database
			_ = m.proj.DB.UnregisterPlanSession(m.ctx, beadID)
		}

		// Close the bead
		if err := beads.Close(ctx, beadID, mainRepoPath); err != nil {
			return planDataMsg{err: fmt.Errorf("failed to close issue: %w", err)}
		}

		// Refresh after close
		items, err := m.loadBeads()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}

func (m *planModel) saveBeadEdit(beadID, title, description, beadType string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		mainRepoPath := m.proj.MainRepoPath()

		// Update the bead using beads package
		err := beads.Update(ctx, beadID, mainRepoPath, beads.UpdateOptions{
			Title:       title,
			Type:        beadType,
			Description: description,
		})
		if err != nil {
			return planDataMsg{err: fmt.Errorf("failed to update issue: %w", err)}
		}

		// Refresh after update
		items, err := m.loadBeads()
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}

// openInEditor opens the issue in $EDITOR using bd edit
func (m *planModel) openInEditor(beadID string) tea.Cmd {
	mainRepoPath := m.proj.MainRepoPath()

	// Use bd edit which handles $EDITOR and the issue format
	c := beads.EditCommand(beadID, mainRepoPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return planStatusMsg{message: fmt.Sprintf("Editor error: %v", err), isError: true}
		}
		// Refresh data after editing
		return editorFinishedMsg{}
	})
}

// startPeriodicRefresh starts the periodic refresh timer
func (m *planModel) startPeriodicRefresh() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return planTickMsg(t)
	})
}
