package control

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/worktree"
)

// HandleCreateWorktreeTask handles a scheduled worktree creation task
func HandleCreateWorktreeTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	branchName := task.Metadata["branch"]
	baseBranch := task.Metadata["base_branch"]
	workerName := task.Metadata["worker_name"]

	if baseBranch == "" {
		baseBranch = "main"
	}

	logging.Info("Creating worktree for work",
		"work_id", workID,
		"branch", branchName,
		"base_branch", baseBranch,
		"attempt", task.AttemptCount+1)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		// Work was deleted - nothing to do
		logging.Info("Work not found, task will be marked completed", "work_id", workID)
		return nil
	}

	// If worktree path is already set and exists, skip creation
	if work.WorktreePath != "" {
		// Worktree already created - just need to ensure git push
		logging.Info("Worktree already exists, skipping creation", "work_id", workID, "path", work.WorktreePath)
	} else {
		// Create the worktree
		workDir := filepath.Join(proj.Root, workID)
		worktreePath := filepath.Join(workDir, "tree")
		mainRepoPath := proj.MainRepoPath()

		// Create work directory
		if err := os.Mkdir(workDir, 0750); err != nil && !os.IsExist(err) {
			return fmt.Errorf("failed to create work directory: %w", err)
		}

		// Fetch latest from origin for the base branch
		if err := git.FetchBranch(ctx, mainRepoPath, baseBranch); err != nil {
			_ = os.RemoveAll(workDir)
			return fmt.Errorf("failed to fetch base branch: %w", err)
		}

		// Create git worktree with new branch based on origin/<baseBranch>
		if err := worktree.Create(ctx, mainRepoPath, worktreePath, branchName, "origin/"+baseBranch); err != nil {
			_ = os.RemoveAll(workDir)
			return fmt.Errorf("failed to create worktree: %w", err)
		}

		// Initialize mise if configured
		if err := mise.InitializeWithOutput(worktreePath, io.Discard); err != nil {
			logging.Warn("mise initialization failed", "error", err)
			// Non-fatal, continue
		}

		// Update work with worktree path
		if err := proj.DB.UpdateWorkWorktreePath(ctx, workID, worktreePath); err != nil {
			return fmt.Errorf("failed to update work worktree path: %w", err)
		}
	}

	// Attempt git push
	work, _ = proj.DB.GetWork(ctx, workID) // Refresh work
	if work != nil && work.WorktreePath != "" {
		if err := git.PushSetUpstreamInDir(ctx, branchName, work.WorktreePath); err != nil {
			return fmt.Errorf("git push failed: %w", err)
		}
	}

	logging.Info("Worktree created and pushed successfully", "work_id", workID)

	// Schedule orchestrator spawn task
	_, err = proj.DB.ScheduleTask(ctx, workID, db.TaskTypeSpawnOrchestrator, time.Now(), map[string]string{
		"worker_name": workerName,
	})
	if err != nil {
		logging.Warn("failed to schedule orchestrator spawn", "error", err, "work_id", workID)
	}

	return nil
}
