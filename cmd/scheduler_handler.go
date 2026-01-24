package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	trackingwatcher "github.com/newhook/co/internal/tracking/watcher"
)

// StartSchedulerWatcher starts a goroutine that watches for scheduled tasks for a single work.
// It uses the tracking database watcher to detect changes and executes tasks when they're due.
//
// Deprecated: This per-work scheduler watcher is no longer used by the orchestrator.
// The control plane (co control) now handles scheduled tasks globally across all works.
// This function is kept for backwards compatibility but should not be called directly.
func StartSchedulerWatcher(ctx context.Context, proj *project.Project, workID string) error {
	logging.Info("Starting scheduler watcher with database events", "work_id", workID)

	// Initialize tracking database watcher
	trackingDBPath := filepath.Join(proj.Root, ".co", "tracking.db")
	watcher, err := trackingwatcher.New(trackingwatcher.DefaultConfig(trackingDBPath))
	if err != nil {
		return fmt.Errorf("failed to create tracking watcher: %w", err)
	}

	if err := watcher.Start(); err != nil {
		watcher.Stop()
		return fmt.Errorf("failed to start tracking watcher: %w", err)
	}

	go func() {
		// Recover from any panics
		defer func() {
			if r := recover(); r != nil {
				logging.Error("scheduler watcher panicked", "error", r)
			}
			watcher.Stop()
		}()

		logging.Debug("Scheduler watcher started", "work_id", workID)

		// Subscribe to watcher events
		sub := watcher.Broker().Subscribe(ctx)

		logging.Debug("Starting scheduler event-driven loop", "work_id", workID)

		// Also set up a timer to check for due tasks periodically (as a safety net)
		// This ensures tasks don't get stuck if they become due between DB changes
		checkInterval := 30 * time.Second
		checkTimer := time.NewTimer(checkInterval)
		defer checkTimer.Stop()

		lastLogTime := time.Now()
		for {
			select {
			case <-ctx.Done():
				logging.Debug("Scheduler watcher stopping due to context cancellation", "work_id", workID)
				return

			case event, ok := <-sub:
				if !ok {
					logging.Debug("Watcher subscription closed", "work_id", workID)
					return
				}

				// Handle database change event
				if event.Payload.Type == trackingwatcher.DBChanged {
					logging.Debug("Database changed, checking scheduled tasks", "work_id", workID)
					processDueTasks(ctx, proj, workID)
				}

			case <-checkTimer.C:
				// Periodic check as a safety net
				if time.Since(lastLogTime) > time.Minute {
					logging.Debug("Scheduler periodic check", "work_id", workID)
					lastLogTime = time.Now()
				}
				processDueTasks(ctx, proj, workID)
				checkTimer.Reset(checkInterval)
			}
		}
	}()

	return nil
}


// processDueTasks checks for and executes any scheduled tasks that are due
func processDueTasks(ctx context.Context, proj *project.Project, workID string) {
	// Get due tasks for this work (query now filters by scheduled_at <= CURRENT_TIMESTAMP)
	tasks, err := proj.DB.GetScheduledTasksForWork(ctx, workID)
	if err != nil {
		logging.Warn("failed to get scheduled tasks", "error", err)
		return
	}

	// Process all tasks (they're already filtered to be due)
	for _, task := range tasks {

		logging.Info("Executing scheduled task", "task_id", task.ID, "task_type", task.TaskType, "work_id", workID, "scheduled_at", task.ScheduledAt.Format(time.RFC3339))

		// Mark as executing
		if err := proj.DB.MarkTaskExecuting(ctx, task.ID); err != nil {
			logging.Warn("failed to mark task as executing", "error", err)
			continue
		}

		// Execute based on task type
		var taskErr error
		switch task.TaskType {
		case db.TaskTypePRFeedback:
			logging.Debug("Handling PR feedback task", "task_id", task.ID, "work_id", workID)
			handlePRFeedbackTask(ctx, proj, workID, task)
		case db.TaskTypeCommentResolution:
			logging.Debug("Handling comment resolution task", "task_id", task.ID, "work_id", workID)
			handleCommentResolutionTask(ctx, proj, workID, task)
		case db.TaskTypeGitPush:
			logging.Debug("Handling git push task", "task_id", task.ID, "work_id", workID)
			taskErr = handleGitPushTask(ctx, proj, workID, task)
		case db.TaskTypeGitHubComment:
			logging.Debug("Handling GitHub comment task", "task_id", task.ID, "work_id", workID)
			taskErr = handleGitHubCommentTask(ctx, proj, workID, task)
		case db.TaskTypeGitHubResolveThread:
			logging.Debug("Handling GitHub resolve thread task", "task_id", task.ID, "work_id", workID)
			taskErr = handleGitHubResolveThreadTask(ctx, proj, workID, task)
		default:
			taskErr = fmt.Errorf("unknown task type: %s", task.TaskType)
		}

		// Handle task result for one-shot tasks
		if taskErr != nil {
			handleTaskError(ctx, proj, task, taskErr.Error())
		}
	}
}

// handlePRFeedbackTask handles a scheduled PR feedback check.
func handlePRFeedbackTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) {
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
	createdCount, err := ProcessPRFeedbackQuiet(ctx, proj, proj.DB, workID, 2)
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

// handleCommentResolutionTask handles a scheduled comment resolution check.
func handleCommentResolutionTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) {
	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil || work == nil || work.PRURL == "" {
		// Mark task as completed but don't reschedule - scheduling happens when PR is created
		proj.DB.MarkTaskCompleted(ctx, task.ID)
		return
	}

	// Check and resolve comments
	checkAndResolveCommentsQuiet(ctx, proj, workID, work.PRURL)

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

// TriggerPRFeedbackCheck schedules an immediate PR feedback check.
// This is called from the TUI when the user presses 'f' in the work details panel.
func TriggerPRFeedbackCheck(ctx context.Context, proj *project.Project, workID string) error {
	logging.Debug("triggering immediate PR feedback check", "work_id", workID)

	// Schedule the task to run now
	_, err := proj.DB.TriggerTaskNow(ctx, workID, db.TaskTypePRFeedback, nil)
	if err != nil {
		return fmt.Errorf("failed to trigger PR feedback check: %w", err)
	}

	logging.Debug("PR feedback check scheduled", "work_id", workID)
	return nil
}

// handleGitPushTask handles a scheduled git push task with retry support.
// Returns nil on success, error on failure (caller handles retry/completion).
func handleGitPushTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) error {
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

// handleGitHubCommentTask handles a scheduled GitHub comment posting task.
// Returns nil on success, error on failure (caller handles retry/completion).
func handleGitHubCommentTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) error {
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

// handleGitHubResolveThreadTask handles a scheduled GitHub thread resolution task.
// Returns nil on success, error on failure (caller handles retry/completion).
func handleGitHubResolveThreadTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) error {
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

// handleTaskError handles an error for a task, rescheduling with backoff if appropriate.
func handleTaskError(ctx context.Context, proj *project.Project, task *db.ScheduledTask, errMsg string) {
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