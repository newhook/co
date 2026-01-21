package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
)

// StartSchedulerWatcher starts a goroutine that watches for scheduled tasks.
// It polls the scheduler table and executes tasks when they're due.
func StartSchedulerWatcher(ctx context.Context, proj *project.Project, workID string) {
	logging.Info("Starting scheduler watcher", "work_id", workID)

	go func() {
		// Recover from any panics
		defer func() {
			if r := recover(); r != nil {
				logging.Error("scheduler watcher panicked", "error", r)
			}
		}()

		logging.Debug("Scheduler watcher started", "work_id", workID)

		// Schedule initial PR feedback check based on configured interval
		prFeedbackInterval := proj.Config.Scheduler.GetPRFeedbackInterval()
		commentResolutionInterval := proj.Config.Scheduler.GetCommentResolutionInterval()

		work, err := proj.DB.GetWork(ctx, workID)
		if err == nil && work != nil && work.PRURL != "" {
			initialCheck := time.Now().Add(prFeedbackInterval)
			logging.Info("Scheduling initial PR feedback check", "work_id", workID, "scheduled_for", initialCheck.Format(time.RFC3339), "interval", prFeedbackInterval)

			_, err := proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypePRFeedback, initialCheck, nil)
			if err != nil {
				logging.Warn("failed to schedule initial PR feedback check", "error", err)
			} else {
				logging.Debug("Successfully scheduled PR feedback check", "work_id", workID, "at", initialCheck.Format(time.RFC3339))
			}

			// Also schedule comment resolution check
			initialResolutionCheck := time.Now().Add(commentResolutionInterval)
			_, err = proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypeCommentResolution, initialResolutionCheck, nil)
			if err != nil {
				logging.Warn("failed to schedule initial comment resolution check", "error", err)
			} else {
				logging.Debug("Successfully scheduled comment resolution check", "work_id", workID, "at", initialCheck.Format(time.RFC3339))
			}
		} else {
			logging.Debug("No PR URL found, skipping initial scheduling", "work_id", workID, "has_work", work != nil)
		}

		// Poll the scheduler table at configured interval
		schedulerPollInterval := proj.Config.Scheduler.GetSchedulerPollInterval()
		ticker := time.NewTicker(schedulerPollInterval)
		defer ticker.Stop()

		logging.Debug("Starting scheduler polling loop", "work_id", workID, "poll_interval", schedulerPollInterval)

		lastLogTime := time.Now()
		for {
			select {
			case <-ctx.Done():
				logging.Debug("Scheduler watcher stopping due to context cancellation", "work_id", workID)
				return
			case <-ticker.C:
				// Get pending tasks for this work
				tasks, err := proj.DB.GetScheduledTasksForWork(ctx, workID)
				if err != nil {
					logging.Warn("failed to get scheduled tasks", "error", err)
					continue
				}

				// Log periodically that we're still polling (every minute)
				if time.Since(lastLogTime) > time.Minute {
					logging.Debug("Scheduler still polling", "work_id", workID, "pending_tasks", len(tasks))
					lastLogTime = time.Now()
				}

				// Process any tasks that are due
				for _, task := range tasks {
					if task.ScheduledAt.After(time.Now()) {
						// Not due yet
						continue
					}

					logging.Info("Executing scheduled task", "task_id", task.ID, "task_type", task.TaskType, "work_id", workID, "scheduled_at", task.ScheduledAt.Format(time.RFC3339))

					// Mark as executing
					if err := proj.DB.MarkTaskExecuting(ctx, task.ID); err != nil {
						logging.Warn("failed to mark task as executing", "error", err)
						continue
					}

					// Execute based on task type
					switch task.TaskType {
					case db.TaskTypePRFeedback:
						logging.Debug("Handling PR feedback task", "task_id", task.ID, "work_id", workID)
						handlePRFeedbackTask(ctx, proj, workID, task)
					case db.TaskTypeCommentResolution:
						logging.Debug("Handling comment resolution task", "task_id", task.ID, "work_id", workID)
						handleCommentResolutionTask(ctx, proj, workID, task)
					case db.TaskTypeGitPush:
						logging.Debug("Handling git push task", "task_id", task.ID, "work_id", workID)
						handleGitPushTask(ctx, proj, workID, task)
					case db.TaskTypeGitHubComment:
						logging.Debug("Handling GitHub comment task", "task_id", task.ID, "work_id", workID)
						handleGitHubCommentTask(ctx, proj, workID, task)
					case db.TaskTypeGitHubResolveThread:
						logging.Debug("Handling GitHub resolve thread task", "task_id", task.ID, "work_id", workID)
						handleGitHubResolveThreadTask(ctx, proj, workID, task)
					default:
						logging.Warn("unknown task type", "task_type", task.TaskType)
						proj.DB.MarkTaskFailed(ctx, task.ID, fmt.Sprintf("Unknown task type: %s", task.TaskType))
					}
				}
			}
		}
	}()
}

// handlePRFeedbackTask handles a scheduled PR feedback check.
func handlePRFeedbackTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) {
	logging.Debug("Starting PR feedback check task", "task_id", task.ID, "work_id", workID)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil || work == nil || work.PRURL == "" {
		logging.Debug("No PR URL for work, not rescheduling", "work_id", workID, "has_pr", work != nil && work.PRURL != "")
		// Mark task as completed but don't reschedule
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
		// Mark task as completed but don't reschedule
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
// This is called from the TUI when the user presses F5.
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
func handleGitPushTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) {
	// Get branch and directory from metadata
	branch := task.Metadata["branch"]
	dir := task.Metadata["dir"]

	if branch == "" {
		// Try to get from work
		work, err := proj.DB.GetWork(ctx, workID)
		if err != nil || work == nil {
			handleTaskError(ctx, proj, task, "failed to get work for git push: work not found")
			return
		}
		branch = work.BranchName
		dir = work.WorktreePath
	}

	if branch == "" || dir == "" {
		handleTaskError(ctx, proj, task, "git push task missing branch or dir metadata")
		return
	}

	logging.Info("Executing git push", "branch", branch, "dir", dir, "attempt", task.AttemptCount+1)

	// Execute git push -u origin <branch>
	cmd := exec.CommandContext(ctx, "git", "push", "--set-upstream", "origin", branch)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()

	if err != nil {
		errMsg := fmt.Sprintf("git push failed: %v\n%s", err, string(output))
		handleTaskError(ctx, proj, task, errMsg)
		return
	}

	logging.Info("Git push succeeded", "branch", branch, "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark git push task as completed", "error", err)
	}
}

// handleGitHubCommentTask handles a scheduled GitHub comment posting task.
func handleGitHubCommentTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) {
	// Get comment details from metadata
	prURL := task.Metadata["pr_url"]
	body := task.Metadata["body"]
	replyToID := task.Metadata["reply_to_id"]

	if prURL == "" || body == "" {
		handleTaskError(ctx, proj, task, "GitHub comment task missing pr_url or body metadata")
		return
	}

	logging.Info("Posting GitHub comment", "pr_url", prURL, "attempt", task.AttemptCount+1)

	// Parse PR URL to get owner/repo/pr_number
	// Expected format: https://github.com/owner/repo/pull/123
	parts := strings.Split(prURL, "/")
	if len(parts) < 7 || parts[5] != "pull" {
		handleTaskError(ctx, proj, task, fmt.Sprintf("invalid PR URL format: %s", prURL))
		return
	}

	owner := parts[3]
	repo := parts[4]
	prNumber := parts[6]

	var cmd *exec.Cmd
	if replyToID != "" {
		// Reply to a specific review comment thread
		cmd = exec.CommandContext(ctx, "gh", "api", "-X", "POST",
			fmt.Sprintf("/repos/%s/%s/pulls/%s/comments/%s/replies", owner, repo, prNumber, replyToID),
			"--field", fmt.Sprintf("body=%s", body),
			"--header", "Accept: application/vnd.github.v3+json")
	} else {
		// Post a general PR comment
		cmd = exec.CommandContext(ctx, "gh", "api", "-X", "POST",
			fmt.Sprintf("/repos/%s/%s/issues/%s/comments", owner, repo, prNumber),
			"--field", fmt.Sprintf("body=%s", body),
			"--header", "Accept: application/vnd.github.v3+json")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := fmt.Sprintf("GitHub comment failed: %v\n%s", err, string(output))
		handleTaskError(ctx, proj, task, errMsg)
		return
	}

	logging.Info("GitHub comment posted successfully", "pr_url", prURL, "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark GitHub comment task as completed", "error", err)
	}
}

// handleGitHubResolveThreadTask handles a scheduled GitHub thread resolution task.
func handleGitHubResolveThreadTask(ctx context.Context, proj *project.Project, workID string, task *db.ScheduledTask) {
	// Get thread details from metadata
	prURL := task.Metadata["pr_url"]
	commentIDStr := task.Metadata["comment_id"]

	if prURL == "" || commentIDStr == "" {
		handleTaskError(ctx, proj, task, "GitHub resolve thread task missing pr_url or comment_id metadata")
		return
	}

	commentID, err := strconv.Atoi(commentIDStr)
	if err != nil {
		handleTaskError(ctx, proj, task, fmt.Sprintf("invalid comment_id: %s", commentIDStr))
		return
	}

	logging.Info("Resolving GitHub thread", "pr_url", prURL, "comment_id", commentID, "attempt", task.AttemptCount+1)

	ghClient := github.NewClient()
	if err := ghClient.ResolveReviewThread(ctx, prURL, commentID); err != nil {
		errMsg := fmt.Sprintf("GitHub resolve thread failed: %v", err)
		handleTaskError(ctx, proj, task, errMsg)
		return
	}

	logging.Info("GitHub thread resolved successfully", "pr_url", prURL, "comment_id", commentID, "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark GitHub resolve thread task as completed", "error", err)
	}
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