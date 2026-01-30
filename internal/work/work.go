package work

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/names"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/worktree"
)

// AddBeadsToWorkResult contains the result of adding beads to a work.
type AddBeadsToWorkResult struct {
	BeadsAdded int
}

// AddBeadsToWorkInternal adds beads to work_beads table.
// This is an internal helper without validation.
func AddBeadsToWorkInternal(ctx context.Context, proj *project.Project, workID string, beadIDs []string) error {
	if len(beadIDs) == 0 {
		return nil
	}
	if err := proj.DB.AddWorkBeads(ctx, workID, beadIDs); err != nil {
		return fmt.Errorf("failed to add beads: %w", err)
	}
	return nil
}

// AddBeadsToWork adds beads to an existing work.
// This is the core logic for adding beads that can be called from both the CLI and TUI.
// Each bead is added as its own group (no grouping).
func AddBeadsToWork(ctx context.Context, proj *project.Project, workID string, beadIDs []string) (*AddBeadsToWorkResult, error) {
	if len(beadIDs) == 0 {
		return nil, fmt.Errorf("no beads specified")
	}

	// Verify work exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	// Check if any bead is already in a task
	for _, beadID := range beadIDs {
		inTask, err := proj.DB.IsBeadInTask(ctx, workID, beadID)
		if err != nil {
			return nil, fmt.Errorf("failed to check bead %s: %w", beadID, err)
		}
		if inTask {
			return nil, fmt.Errorf("bead %s is already assigned to a task", beadID)
		}
	}

	// Add beads to work
	if err := proj.DB.AddWorkBeads(ctx, workID, beadIDs); err != nil {
		return nil, fmt.Errorf("failed to add beads: %w", err)
	}

	return &AddBeadsToWorkResult{
		BeadsAdded: len(beadIDs),
	}, nil
}

// CreateWorkAsyncResult contains the result of creating a work unit asynchronously.
type CreateWorkAsyncResult struct {
	WorkID      string
	WorkerName  string
	BranchName  string
	BaseBranch  string
	RootIssueID string
}

// CreateWorkAsync creates a work unit asynchronously by scheduling tasks.
// This is the async work creation for the control plane architecture:
// 1. Creates work record in DB (without worktree path)
// 2. Schedules TaskTypeCreateWorktree task for the control plane
// The control plane will handle worktree creation, git push, and orchestrator spawning.
//
// Deprecated: Use WorkService.CreateWorkAsync instead. This wrapper exists for backward compatibility.
func CreateWorkAsync(ctx context.Context, proj *project.Project, branchName, baseBranch, rootIssueID string, auto bool) (*CreateWorkAsyncResult, error) {
	svc := NewWorkService(proj)
	return svc.CreateWorkAsync(ctx, branchName, baseBranch, rootIssueID, auto)
}

// CreateWorkAsyncOptions contains options for creating a work unit asynchronously.
type CreateWorkAsyncOptions struct {
	BranchName        string
	BaseBranch        string
	RootIssueID       string
	Auto              bool
	UseExistingBranch bool
	BeadIDs           []string // Beads to add to the work (added immediately, not by control plane)
}

// CreateWorkAsyncWithOptions creates a work unit asynchronously with the given options.
// This is similar to CreateWorkAsync but supports additional options like using an existing branch.
//
// Deprecated: Use WorkService.CreateWorkAsyncWithOptions instead. This wrapper exists for backward compatibility.
func CreateWorkAsyncWithOptions(ctx context.Context, proj *project.Project, opts CreateWorkAsyncOptions) (*CreateWorkAsyncResult, error) {
	svc := NewWorkService(proj)
	return svc.CreateWorkAsyncWithOptions(ctx, opts)
}

// ImportPRAsyncOptions contains options for importing a PR asynchronously.
type ImportPRAsyncOptions struct {
	PRURL       string
	BranchName  string
	RootIssueID string
}

// ImportPRAsyncResult contains the result of scheduling a PR import.
type ImportPRAsyncResult struct {
	WorkID      string
	WorkerName  string
	BranchName  string
	RootIssueID string
}

// ImportPRAsync imports a PR asynchronously by scheduling a control plane task.
// This creates the work record and schedules the import task - the actual
// worktree setup happens in the control plane.
func ImportPRAsync(ctx context.Context, proj *project.Project, opts ImportPRAsyncOptions) (*ImportPRAsyncResult, error) {
	baseBranch := proj.Config.Repo.GetBaseBranch()

	// Generate work ID
	workID, err := proj.DB.GenerateWorkID(ctx, opts.BranchName, proj.Config.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate work ID: %w", err)
	}

	// Get a human-readable name for this worker
	workerName, err := names.GetNextAvailableName(ctx, proj.DB.DB)
	if err != nil {
		workerName = "" // Non-fatal
	}

	// Create work record in DB (without worktree path - control plane will set it)
	if err := proj.DB.CreateWork(ctx, workID, workerName, "", opts.BranchName, baseBranch, opts.RootIssueID, false); err != nil {
		return nil, fmt.Errorf("failed to create work record: %w", err)
	}

	// Add root issue to work_beads immediately (before control plane runs)
	if opts.RootIssueID != "" {
		if err := AddBeadsToWorkInternal(ctx, proj, workID, []string{opts.RootIssueID}); err != nil {
			_ = proj.DB.DeleteWork(ctx, workID)
			return nil, fmt.Errorf("failed to add bead to work: %w", err)
		}
	}

	// Schedule the PR import task for the control plane
	err = proj.DB.ScheduleTaskWithRetry(ctx, workID, db.TaskTypeImportPR, time.Now(), map[string]string{
		"pr_url":      opts.PRURL,
		"branch":      opts.BranchName,
		"worker_name": workerName,
	}, fmt.Sprintf("import-pr-%s", workID), db.DefaultMaxAttempts)
	if err != nil {
		// Work record created but task scheduling failed - cleanup
		_ = proj.DB.DeleteWork(ctx, workID)
		return nil, fmt.Errorf("failed to schedule PR import: %w", err)
	}

	return &ImportPRAsyncResult{
		WorkID:      workID,
		WorkerName:  workerName,
		BranchName:  opts.BranchName,
		RootIssueID: opts.RootIssueID,
	}, nil
}

// DestroyWork destroys a work unit and all its resources.
// This is the core work destruction logic that can be called from both the CLI and TUI.
// It does not perform interactive confirmation - that should be handled by the caller.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func DestroyWork(ctx context.Context, proj *project.Project, workID string, w io.Writer) error {
	// Get work to verify it exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Close the root issue if it exists
	if work.RootIssueID != "" {
		fmt.Fprintf(w, "Closing root issue %s...\n", work.RootIssueID)
		if err := beads.Close(ctx, work.RootIssueID, proj.BeadsPath()); err != nil {
			// Warn but continue - issue might already be closed or deleted
			fmt.Fprintf(w, "Warning: failed to close root issue %s: %v\n", work.RootIssueID, err)
		}
	}

	// Terminate any running zellij tabs (orchestrator, task, console, and claude tabs) for this work
	// Only if configured to do so (defaults to true)
	if proj.Config.Zellij.ShouldKillTabsOnDestroy() {
		if err := claude.TerminateWorkTabs(ctx, workID, proj.Config.Project.Name, w); err != nil {
			// Warn but continue - tab termination is non-fatal
			fmt.Fprintf(w, "Warning: failed to terminate work tabs: %v\n", err)
		}
	}

	// Remove git worktree if it exists
	if work.WorktreePath != "" {
		if err := worktree.NewOperations().RemoveForce(ctx, proj.MainRepoPath(), work.WorktreePath); err != nil {
			fmt.Fprintf(w, "Warning: failed to remove worktree: %v\n", err)
		}
	}

	// Remove work directory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.RemoveAll(workDir); err != nil {
		fmt.Fprintf(w, "Warning: failed to remove work directory %s: %v\n", workDir, err)
	}

	// Delete work from database (also deletes associated tasks and relationships)
	if err := proj.DB.DeleteWork(ctx, workID); err != nil {
		return fmt.Errorf("failed to delete work from database: %w", err)
	}

	return nil
}
