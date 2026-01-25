package control

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/feedback"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/github"
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
		// Work was deleted - mark task as completed (nothing to do)
		logging.Info("Work not found, marking task as completed", "work_id", workID)
		proj.DB.MarkTaskCompleted(ctx, task.ID)
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
		if err := os.Mkdir(workDir, 0755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("failed to create work directory: %w", err)
		}

		// Create git worktree with new branch
		if err := worktree.Create(ctx, mainRepoPath, worktreePath, branchName, baseBranch); err != nil {
			os.RemoveAll(workDir)
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

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark task as completed", "error", err)
	}

	// Schedule orchestrator spawn task
	_, err = proj.DB.ScheduleTask(ctx, workID, db.TaskTypeSpawnOrchestrator, time.Now(), map[string]string{
		"worker_name": workerName,
	})
	if err != nil {
		logging.Warn("failed to schedule orchestrator spawn", "error", err, "work_id", workID)
	}

	return nil
}

// HandleSpawnOrchestratorTask handles a scheduled orchestrator spawn task
func HandleSpawnOrchestratorTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	workerName := task.Metadata["worker_name"]

	logging.Info("Spawning orchestrator for work",
		"work_id", workID,
		"attempt", task.AttemptCount+1)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		// Work was deleted - mark task as completed (nothing to do)
		logging.Info("Work not found, marking task as completed", "work_id", workID)
		proj.DB.MarkTaskCompleted(ctx, task.ID)
		return nil
	}

	if work.WorktreePath == "" {
		return fmt.Errorf("work %s has no worktree path", workID)
	}

	// Spawn the orchestrator
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, work.WorktreePath, workerName, io.Discard); err != nil {
		return fmt.Errorf("failed to spawn orchestrator: %w", err)
	}

	logging.Info("Orchestrator spawned successfully", "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark task as completed", "error", err)
	}

	return nil
}

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
		// Work was already deleted - mark task as completed
		logging.Info("Work not found, marking task as completed", "work_id", workID)
		proj.DB.MarkTaskCompleted(ctx, task.ID)
		return nil
	}

	// Close the root issue if it exists
	if work.RootIssueID != "" {
		logging.Info("Closing root issue", "work_id", workID, "root_issue_id", work.RootIssueID)
		if err := beads.Close(ctx, work.RootIssueID, proj.MainRepoPath()); err != nil {
			// Warn but continue - issue might already be closed or deleted
			logging.Warn("failed to close root issue", "error", err, "root_issue_id", work.RootIssueID)
		}
	}

	// Terminate any running zellij tabs (orchestrator and task tabs) for this work
	if err := claude.TerminateWorkTabs(ctx, workID, proj.Config.Project.Name, io.Discard); err != nil {
		logging.Warn("failed to terminate work tabs", "error", err, "work_id", workID)
		// Continue with destruction even if tab termination fails
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

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark task as completed", "error", err)
	}

	return nil
}

// HandlePRFeedbackTask handles a scheduled PR feedback check.
func HandlePRFeedbackTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) {
	logging.Debug("Starting PR feedback check task", "task_id", task.ID, "work_id", workID)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil || work == nil || work.PRURL == "" {
		logging.Debug("No PR URL for work, not rescheduling", "work_id", workID, "has_pr", work != nil && work.PRURL != "")
		// Mark task as completed but don't reschedule - scheduling happens when PR is created
		proj.DB.MarkTaskCompleted(ctx, task.ID)
		return
	}

	logging.Debug("Checking PR feedback", "pr_url", work.PRURL, "work_id", workID)

	// Process PR feedback - creates beads but doesn't add them to work
	createdCount, err := feedback.ProcessPRFeedbackQuiet(ctx, proj, proj.DB, workID, 2)
	if err != nil {
		logging.Error("Failed to check PR feedback", "error", err, "work_id", workID)
		proj.DB.MarkTaskFailed(ctx, task.ID, err.Error())
	} else {
		if createdCount > 0 {
			logging.Info("Created beads from PR feedback", "count", createdCount, "work_id", workID)
		} else {
			logging.Debug("No new PR feedback found", "work_id", workID)
		}

		// Mark as completed
		if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
			logging.Warn("failed to mark task as completed", "error", err, "task_id", task.ID)
		} else {
			logging.Debug("Task completed successfully", "task_id", task.ID, "work_id", workID)
		}

		// Schedule next check using configured interval
		nextInterval := proj.Config.Scheduler.GetPRFeedbackInterval()
		nextCheck := time.Now().Add(nextInterval)
		_, err = proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypePRFeedback, nextCheck, nil)
		if err != nil {
			logging.Warn("failed to schedule next PR feedback check", "error", err, "work_id", workID)
		} else {
			logging.Info("Scheduled next PR feedback check", "work_id", workID, "next_check", nextCheck.Format(time.RFC3339), "interval", nextInterval)
		}
	}
}

// HandleCommentResolutionTask handles a scheduled comment resolution check.
func HandleCommentResolutionTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) {
	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil || work == nil || work.PRURL == "" {
		// Mark task as completed but don't reschedule - scheduling happens when PR is created
		proj.DB.MarkTaskCompleted(ctx, task.ID)
		return
	}

	// Check and resolve comments
	feedback.CheckAndResolveComments(ctx, proj, workID)

	// Mark as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark task as completed", "error", err)
	}

	// Schedule next check using configured interval
	nextInterval := proj.Config.Scheduler.GetCommentResolutionInterval()
	nextCheck := time.Now().Add(nextInterval)
	_, err = proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypeCommentResolution, nextCheck, nil)
	if err != nil {
		logging.Warn("failed to schedule next comment resolution check", "error", err)
	}
}

// HandleGitPushTask handles a scheduled git push task with retry support.
// Returns nil on success, error on failure (caller handles retry/completion).
func HandleGitPushTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) error {
	// Get branch and directory from metadata
	branch := task.Metadata["branch"]
	dir := task.Metadata["dir"]

	if branch == "" {
		// Try to get from work
		work, err := proj.DB.GetWork(ctx, workID)
		if err != nil || work == nil {
			return fmt.Errorf("failed to get work for git push: work not found")
		}
		branch = work.BranchName
		dir = work.WorktreePath
	}

	if branch == "" || dir == "" {
		return fmt.Errorf("git push task missing branch or dir metadata")
	}

	logging.Info("Executing git push", "branch", branch, "dir", dir, "attempt", task.AttemptCount+1)

	if err := git.PushSetUpstreamInDir(ctx, branch, dir); err != nil {
		return err
	}

	logging.Info("Git push succeeded", "branch", branch, "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark git push task as completed", "error", err)
	}
	return nil
}

// HandleGitHubCommentTask handles a scheduled GitHub comment posting task.
// Returns nil on success, error on failure (caller handles retry/completion).
func HandleGitHubCommentTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) error {
	// Get comment details from metadata
	prURL := task.Metadata["pr_url"]
	body := task.Metadata["body"]
	replyToID := task.Metadata["reply_to_id"]

	if prURL == "" || body == "" {
		return fmt.Errorf("GitHub comment task missing pr_url or body metadata")
	}

	logging.Info("Posting GitHub comment", "pr_url", prURL, "attempt", task.AttemptCount+1)

	ghClient := github.NewClient()
	var err error
	if replyToID != "" {
		// Reply to a specific review comment thread
		commentID, convErr := strconv.Atoi(replyToID)
		if convErr != nil {
			return fmt.Errorf("invalid reply_to_id: %s", replyToID)
		}
		err = ghClient.PostReviewReply(ctx, prURL, commentID, body)
	} else {
		// Post a general PR comment
		err = ghClient.PostPRComment(ctx, prURL, body)
	}

	if err != nil {
		return fmt.Errorf("GitHub comment failed: %w", err)
	}

	logging.Info("GitHub comment posted successfully", "pr_url", prURL, "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark GitHub comment task as completed", "error", err)
	}
	return nil
}

// HandleGitHubResolveThreadTask handles a scheduled GitHub thread resolution task.
// Returns nil on success, error on failure (caller handles retry/completion).
func HandleGitHubResolveThreadTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) error {
	// Get thread details from metadata
	prURL := task.Metadata["pr_url"]
	commentIDStr := task.Metadata["comment_id"]

	if prURL == "" || commentIDStr == "" {
		return fmt.Errorf("GitHub resolve thread task missing pr_url or comment_id metadata")
	}

	commentID, err := strconv.Atoi(commentIDStr)
	if err != nil {
		return fmt.Errorf("invalid comment_id: %s", commentIDStr)
	}

	logging.Info("Resolving GitHub thread", "pr_url", prURL, "comment_id", commentID, "attempt", task.AttemptCount+1)

	ghClient := github.NewClient()
	if err := ghClient.ResolveReviewThread(ctx, prURL, commentID); err != nil {
		return fmt.Errorf("GitHub resolve thread failed: %w", err)
	}

	logging.Info("GitHub thread resolved successfully", "pr_url", prURL, "comment_id", commentID, "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark GitHub resolve thread task as completed", "error", err)
	}
	return nil
}

// HandleTaskError handles an error for a task, rescheduling with backoff if appropriate.
func HandleTaskError(ctx context.Context, proj *project.Project, task *db.ScheduledTask, errMsg string) {
	logging.Error("Task failed", "task_id", task.ID, "task_type", task.TaskType, "error", errMsg, "attempt", task.AttemptCount+1)

	// Check if we should retry
	if task.ShouldRetry() {
		logging.Info("Rescheduling task with backoff", "task_id", task.ID, "attempt", task.AttemptCount+1, "max_attempts", task.MaxAttempts)
		if err := proj.DB.RescheduleWithBackoff(ctx, task.ID, errMsg); err != nil {
			logging.Error("Failed to reschedule task", "task_id", task.ID, "error", err)
			// Fall back to marking as failed
			proj.DB.MarkTaskFailed(ctx, task.ID, errMsg)
		}
	} else {
		logging.Warn("Task exhausted retries", "task_id", task.ID, "attempts", task.AttemptCount+1, "max_attempts", task.MaxAttempts)
		proj.DB.MarkTaskFailed(ctx, task.ID, errMsg)
	}
}
