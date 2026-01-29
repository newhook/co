package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/linear"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/names"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/work"
	"github.com/newhook/co/internal/worktree"
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

	// Handle task filter - show beads assigned to a specific task
	if filters.task != "" {
		return m.loadBeadsForTask(filters)
	}

	// Handle children filter - show children (dependents) of a specific bead
	if filters.children != "" {
		return m.loadBeadsForChildren(filters)
	}

	// Use the shared fetchBeadsWithFilters function
	items, err := fetchBeadsWithFilters(m.ctx, m.proj.Beads, mainRepoPath, filters)
	if err != nil {
		return nil, err
	}

	// Fetch assigned beads from database and populate assignedWorkID
	assignedBeads, err := m.proj.DB.GetAllAssignedBeads(m.ctx)
	if err == nil {
		for i := range items {
			if workID, ok := assignedBeads[items[i].ID]; ok {
				items[i].assignedWorkID = workID
			}
		}
	}

	// Build tree structure from dependencies
	items = buildBeadTree(m.ctx, items, m.proj.Beads)

	// If no tree structure, apply regular sorting
	hasTree := false
	for _, item := range items {
		if item.treeDepth > 0 || len(item.Dependents) > 0 {
			hasTree = true
			break
		}
	}

	if !hasTree {
		// Fall back to regular sorting if no tree structure
		switch filters.sortBy {
		case "priority":
			sort.Slice(items, func(i, j int) bool {
				return items[i].Priority < items[j].Priority
			})
		case "title":
			sort.Slice(items, func(i, j int) bool {
				return items[i].Title < items[j].Title
			})
		}
	}

	return items, nil
}

// loadBeadsForTask loads beads assigned to a specific task.
// This fetches all beads for the task regardless of status filter.
func (m *planModel) loadBeadsForTask(filters beadFilters) ([]beadItem, error) {
	// Get bead IDs assigned to this task from the database
	beadIDs, err := m.proj.DB.GetTaskBeads(m.ctx, filters.task)
	if err != nil {
		return nil, fmt.Errorf("failed to get task beads: %w", err)
	}

	if len(beadIDs) == 0 {
		return nil, nil
	}

	// Fetch the beads from the beads client (uses cache)
	var items []beadItem
	for _, beadID := range beadIDs {
		bead, err := m.proj.Beads.GetBead(m.ctx, beadID)
		if err != nil || bead == nil {
			continue
		}
		items = append(items, beadItem{
			BeadWithDeps: bead,
		})
	}

	// Apply search text filter if set
	if filters.searchText != "" {
		searchLower := strings.ToLower(filters.searchText)
		var filtered []beadItem
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.ID), searchLower) ||
				strings.Contains(strings.ToLower(item.Title), searchLower) ||
				strings.Contains(strings.ToLower(item.Description), searchLower) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	// Build tree structure from dependencies
	items = buildBeadTree(m.ctx, items, m.proj.Beads)

	return items, nil
}

// loadBeadsForChildren loads children (dependents) of a specific bead.
// This fetches all dependents regardless of status filter.
func (m *planModel) loadBeadsForChildren(filters beadFilters) ([]beadItem, error) {
	// Get the parent bead to find its dependents
	parentBead, err := m.proj.Beads.GetBead(m.ctx, filters.children)
	if err != nil {
		return nil, fmt.Errorf("failed to get bead: %w", err)
	}
	if parentBead == nil {
		return nil, nil
	}

	// Also include the parent bead itself
	items := []beadItem{{BeadWithDeps: parentBead}}

	// Fetch each dependent bead
	for _, dep := range parentBead.Dependents {
		bead, err := m.proj.Beads.GetBead(m.ctx, dep.IssueID)
		if err != nil || bead == nil {
			continue
		}
		items = append(items, beadItem{
			BeadWithDeps: bead,
		})
	}

	// Apply search text filter if set
	if filters.searchText != "" {
		searchLower := strings.ToLower(filters.searchText)
		var filtered []beadItem
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.ID), searchLower) ||
				strings.Contains(strings.ToLower(item.Title), searchLower) ||
				strings.Contains(strings.ToLower(item.Description), searchLower) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	// Build tree structure from dependencies
	items = buildBeadTree(m.ctx, items, m.proj.Beads)

	return items, nil
}

func (m *planModel) createBead(title, beadType string, priority int, isEpic bool, description string, parent string) tea.Cmd {
	return func() tea.Msg {
		ctx := m.ctx
		beadsPath := m.proj.BeadsPath()

		beadID, err := beads.Create(ctx, beadsPath, beads.CreateOptions{
			Title:       title,
			Type:        beadType,
			Priority:    priority,
			IsEpic:      isEpic,
			Description: description,
			Parent:      parent,
		})
		if err != nil {
			return planDataMsg{err: fmt.Errorf("failed to create issue: %w", err)}
		}

		// Refresh after creation
		items, err := m.loadBeads()
		session := m.sessionName()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)

		return planDataMsg{beads: items, activeSessions: activeSessions, err: err, createdBeadID: beadID}
	}
}

func (m *planModel) closeBead(beadID string) tea.Cmd {
	return func() tea.Msg {
		beadsPath := m.proj.BeadsPath()
		session := m.sessionName()
		tabName := db.TabNameForBead(beadID)

		// If there's an active session for this bead, close it
		if m.activeBeadSessions[beadID] {
			// Terminate and close the tab
			_ = m.zj.Session(session).TerminateAndCloseTab(m.ctx, tabName)
			// Unregister from database
			_ = m.proj.DB.UnregisterPlanSession(m.ctx, beadID)
		}

		// Close the bead
		if err := beads.Close(m.ctx, beadID, beadsPath); err != nil {
			return planDataMsg{err: fmt.Errorf("failed to close issue: %w", err)}
		}

		// Refresh after close
		items, err := m.loadBeads()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}

func (m *planModel) closeBeads(beadIDs []string) tea.Cmd {
	return func() tea.Msg {
		beadsPath := m.proj.BeadsPath()
		session := m.sessionName()

		// First, close any active sessions for these beads
		zjSession := m.zj.Session(session)
		for _, beadID := range beadIDs {
			if m.activeBeadSessions[beadID] {
				tabName := db.TabNameForBead(beadID)
				// Terminate and close the tab
				_ = zjSession.TerminateAndCloseTab(m.ctx, tabName)
				// Unregister from database
				_ = m.proj.DB.UnregisterPlanSession(m.ctx, beadID)
			}
		}

		// Close all beads using the beads package
		for _, beadID := range beadIDs {
			if err := beads.Close(m.ctx, beadID, beadsPath); err != nil {
				return planDataMsg{err: fmt.Errorf("failed to close issue %s: %w", beadID, err)}
			}
		}

		// Refresh after close
		items, err := m.loadBeads()
		activeSessions, _ := m.proj.DB.GetBeadsWithActiveSessions(m.ctx, session)
		return planDataMsg{beads: items, activeSessions: activeSessions, err: err}
	}
}

func (m *planModel) saveBeadEdit(beadID, title, description, beadType, status string) tea.Cmd {
	return func() tea.Msg {
		beadsPath := m.proj.BeadsPath()

		// Update the bead using beads package
		err := beads.Update(m.ctx, beadID, beadsPath, beads.UpdateOptions{
			Title:       title,
			Type:        beadType,
			Description: description,
			Status:      status,
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
	beadsPath := m.proj.BeadsPath()

	// Use bd edit which handles $EDITOR and the issue format
	c := beads.EditCommand(m.ctx, beadID, beadsPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return planStatusMsg{message: fmt.Sprintf("Editor error: %v", err), isError: true}
		}
		// Refresh data after editing
		return editorFinishedMsg{}
	})
}

// importLinearIssue imports Linear issues (supports multiple IDs/URLs)
func (m *planModel) importLinearIssue(issueIDsInput string) tea.Cmd {
	return func() tea.Msg {
		beadsPath := m.proj.BeadsPath()

		// Get API key from config
		var apiKey string
		if m.proj.Config != nil {
			apiKey = m.proj.Config.Linear.APIKey
		}
		if apiKey == "" {
			return linearImportCompleteMsg{err: fmt.Errorf("linear API key not set (set [linear] api_key in config.toml)")}
		}

		// Create fetcher
		fetcher, err := linear.NewFetcher(apiKey, beadsPath)
		if err != nil {
			return linearImportCompleteMsg{err: fmt.Errorf("failed to create Linear fetcher: %w", err)}
		}

		// Prepare import options from panel
		formResult := m.linearImportPanel.GetResult()
		opts := &linear.ImportOptions{
			DryRun:         formResult.DryRun,
			UpdateExisting: formResult.Update,
			CreateDeps:     formResult.CreateDeps,
			MaxDepDepth:    formResult.MaxDepth,
		}

		// Parse newline-delimited input
		lines := strings.Split(issueIDsInput, "\n")
		var issueIDs []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				issueIDs = append(issueIDs, trimmed)
			}
		}

		// If only one ID, use single import for backward compatibility
		if len(issueIDs) == 1 {
			result, err := fetcher.FetchAndImport(m.ctx, issueIDs[0], opts)
			if err != nil {
				return linearImportCompleteMsg{err: fmt.Errorf("import failed: %w", err)}
			}

			// Check result
			if result.Error != nil {
				return linearImportCompleteMsg{err: result.Error}
			}

			if result.BeadID != "" {
				return linearImportCompleteMsg{
					beadIDs:    []string{result.BeadID},
					skipReason: result.SkipReason,
				}
			}

			return linearImportCompleteMsg{err: fmt.Errorf("import completed but no bead ID returned")}
		}

		// Use batch import for multiple IDs
		results, err := fetcher.FetchBatch(m.ctx, issueIDs, opts)
		if err != nil {
			return linearImportCompleteMsg{err: fmt.Errorf("batch import failed: %w", err)}
		}

		// Collect results
		var beadIDs []string
		var skipReasons []string
		var errors []string
		successCount := 0
		skipCount := 0
		errorCount := 0

		for i, result := range results {
			if result.Error != nil {
				errorCount++
				errors = append(errors, fmt.Sprintf("%s: %v", issueIDs[i], result.Error))
			} else if result.SkipReason != "" {
				skipCount++
				skipReasons = append(skipReasons, fmt.Sprintf("%s: %s", issueIDs[i], result.SkipReason))
			} else if result.BeadID != "" {
				successCount++
				beadIDs = append(beadIDs, result.BeadID)
			}
		}

		// Return aggregated results
		return linearImportCompleteMsg{
			beadIDs:      beadIDs,
			successCount: successCount,
			skipCount:    skipCount,
			errorCount:   errorCount,
			skipReasons:  skipReasons,
			errors:       errors,
		}
	}
}

// prImportCompleteMsg indicates a PR import completed
type prImportCompleteMsg struct {
	workID     string
	prMetadata *github.PRMetadata
	err        error
}

// prImportPreviewMsg indicates a PR preview was fetched
type prImportPreviewMsg struct {
	metadata *github.PRMetadata
	err      error
}

// previewPR fetches PR metadata for preview
func (m *planModel) previewPR(prURL string) tea.Cmd {
	return func() tea.Msg {
		ghClient := github.NewClient()
		importer := github.NewPRImporter(ghClient)

		metadata, err := importer.FetchPRMetadata(m.ctx, prURL, "")
		if err != nil {
			return prImportPreviewMsg{err: fmt.Errorf("failed to fetch PR: %w", err)}
		}

		return prImportPreviewMsg{metadata: metadata}
	}
}

// importPR imports a PR into a work unit
func (m *planModel) importPR(prURL string, createBead, auto bool) tea.Cmd {
	return func() tea.Msg {
		ghClient := github.NewClient()
		importer := github.NewPRImporter(ghClient)

		// Fetch PR metadata first
		metadata, err := importer.FetchPRMetadata(m.ctx, prURL, "")
		if err != nil {
			return prImportCompleteMsg{err: fmt.Errorf("failed to fetch PR: %w", err)}
		}

		// Use the PR's branch name
		branchName := metadata.HeadRefName

		// Generate work ID
		workID, err := m.proj.DB.GenerateWorkID(m.ctx, branchName, m.proj.Config.Project.Name)
		if err != nil {
			return prImportCompleteMsg{err: fmt.Errorf("failed to generate work ID: %w", err)}
		}

		// Create work asynchronously using the same pattern as executeCreateWork
		result := m.importPRSync(workID, prURL, branchName, metadata, createBead, auto)
		return result
	}
}

// importPRSync performs the synchronous PR import operations
func (m *planModel) importPRSync(workID, prURL, branchName string, metadata *github.PRMetadata, createBead, auto bool) prImportCompleteMsg {
	ctx := m.ctx
	proj := m.proj

	// Import the PR using the internal work package
	// This is a simplified version - the full implementation uses work.CreateWorkAsyncWithOptions
	// with the PR import fields
	opts := importPROpts{
		WorkID:     workID,
		PRURL:      prURL,
		BranchName: branchName,
		Metadata:   metadata,
		CreateBead: createBead,
		Auto:       auto,
	}

	err := doImportPR(ctx, proj, &opts)
	if err != nil {
		return prImportCompleteMsg{err: err}
	}

	return prImportCompleteMsg{
		workID:     workID,
		prMetadata: metadata,
	}
}

// importPROpts contains options for PR import
type importPROpts struct {
	WorkID     string
	PRURL      string
	BranchName string
	Metadata   *github.PRMetadata
	CreateBead bool
	Auto       bool
}

// doImportPR performs the actual PR import
func doImportPR(ctx context.Context, proj *project.Project, opts *importPROpts) error {
	// This delegates to the work package for the actual import
	// The implementation is in cmd/work_import_pr.go runWorkImportPR
	// For TUI, we need a programmatic version

	importer := github.NewPRImporter(github.NewClient())
	mainRepoPath := proj.MainRepoPath()

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, opts.WorkID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Set up worktree from PR
	_, worktreePath, err := importer.SetupWorktreeFromPR(ctx, mainRepoPath, opts.PRURL, "", workDir, opts.BranchName)
	if err != nil {
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to set up worktree: %w", err)
	}

	// Set up upstream tracking
	gitOps := git.NewOperations()
	wtOps := worktree.NewOperations()
	if err := gitOps.PushSetUpstream(ctx, opts.BranchName, worktreePath); err != nil {
		_ = wtOps.RemoveForce(ctx, mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to set upstream: %w", err)
	}

	// Initialize mise if needed
	_ = mise.Initialize(worktreePath)

	// Get worker name
	workerName, _ := names.GetNextAvailableName(ctx, proj.DB.DB)

	// Optionally create a bead from PR metadata
	var rootIssueID string
	if opts.CreateBead {
		result, err := importer.CreateBeadFromPR(ctx, opts.Metadata, &github.CreateBeadOptions{
			BeadsDir:     proj.BeadsPath(),
			SkipIfExists: true,
		})
		if err == nil && result.Created {
			rootIssueID = result.BeadID
		} else if err == nil && result.BeadID != "" {
			rootIssueID = result.BeadID
		}
	}

	// Get base branch from project config
	baseBranch := proj.Config.Repo.GetBaseBranch()

	// Create work record in database
	if err := proj.DB.CreateWork(ctx, opts.WorkID, workerName, worktreePath, opts.BranchName, baseBranch, rootIssueID, opts.Auto); err != nil {
		_ = wtOps.RemoveForce(ctx, mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	// Set PR URL on the work
	prFeedbackInterval := proj.Config.Scheduler.GetPRFeedbackInterval()
	commentResolutionInterval := proj.Config.Scheduler.GetCommentResolutionInterval()
	_ = proj.DB.SetWorkPRURLAndScheduleFeedback(ctx, opts.WorkID, opts.PRURL, prFeedbackInterval, commentResolutionInterval)

	// Add bead to work_beads if created
	if rootIssueID != "" {
		_ = work.AddBeadsToWorkInternal(ctx, proj, opts.WorkID, []string{rootIssueID})
	}

	return nil
}
