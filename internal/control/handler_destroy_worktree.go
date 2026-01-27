package control

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/worktree"
)

// HandleDestroyWorktreeTask handles a scheduled worktree destruction task
func HandleDestroyWorktreeTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID

	logging.Info("Destroying worktree for work",
		"work_id", workID,
		"attempt", task.AttemptCount+1)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		// Work was already deleted - nothing to do
		logging.Info("Work not found, task will be marked completed", "work_id", workID)
		return nil
	}

	// Close the root issue if it exists
	if work.RootIssueID != "" {
		logging.Info("Closing root issue", "work_id", workID, "root_issue_id", work.RootIssueID)
		if err := beads.Close(ctx, work.RootIssueID, proj.BeadsPath()); err != nil {
			// Warn but continue - issue might already be closed or deleted
			logging.Warn("failed to close root issue", "error", err, "root_issue_id", work.RootIssueID)
		}
	}

	// Terminate any running zellij tabs (orchestrator, task, console, and claude tabs) for this work
	// Only if configured to do so (defaults to true)
	if proj.Config.Zellij.ShouldKillTabsOnDestroy() {
		if err := claude.TerminateWorkTabs(ctx, workID, proj.Config.Project.Name, io.Discard); err != nil {
			logging.Warn("failed to terminate work tabs", "error", err, "work_id", workID)
			// Continue with destruction even if tab termination fails
		}
	}

	// Remove git worktree if it exists
	// Note: We continue even if this fails, because the worktree might not exist in git's records
	// but the directory might still exist. The os.RemoveAll below will clean up the directory.
	if work.WorktreePath != "" {
		logging.Info("Removing git worktree", "work_id", workID, "path", work.WorktreePath)
		if err := worktree.RemoveForce(ctx, proj.MainRepoPath(), work.WorktreePath); err != nil {
			logging.Warn("failed to remove git worktree (continuing with directory removal)", "error", err, "work_id", workID)
		}
	}

	// Remove work directory
	workDir := filepath.Join(proj.Root, workID)
	logging.Info("Removing work directory", "work_id", workID, "path", workDir)
	if err := os.RemoveAll(workDir); err != nil {
		// This is a retriable error
		return fmt.Errorf("failed to remove work directory: %w", err)
	}

	// Delete work from database (also deletes associated tasks and relationships)
	if err := proj.DB.DeleteWork(ctx, workID); err != nil {
		return fmt.Errorf("failed to delete work from database: %w", err)
	}

	logging.Info("Worktree destroyed successfully", "work_id", workID)

	return nil
}
