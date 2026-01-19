package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/newhook/co/internal/db"
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

		// Schedule initial PR feedback check in 5 minutes
		work, err := proj.DB.GetWork(ctx, workID)
		if err == nil && work != nil && work.PRURL != "" {
			initialCheck := time.Now().Add(5 * time.Minute)
			logging.Info("Scheduling initial PR feedback check", "work_id", workID, "scheduled_for", initialCheck.Format(time.RFC3339))

			_, err := proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypePRFeedback, initialCheck, nil)
			if err != nil {
				logging.Warn("failed to schedule initial PR feedback check", "error", err)
			} else {
				logging.Debug("Successfully scheduled PR feedback check", "work_id", workID, "at", initialCheck.Format(time.RFC3339))
			}

			// Also schedule comment resolution check
			_, err = proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypeCommentResolution, initialCheck, nil)
			if err != nil {
				logging.Warn("failed to schedule initial comment resolution check", "error", err)
			} else {
				logging.Debug("Successfully scheduled comment resolution check", "work_id", workID, "at", initialCheck.Format(time.RFC3339))
			}
		} else {
			logging.Debug("No PR URL found, skipping initial scheduling", "work_id", workID, "has_work", work != nil)
		}

		// Poll the scheduler table every second
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		logging.Debug("Starting scheduler polling loop", "work_id", workID, "poll_interval", "1s")

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

	// Process PR feedback - suppress output when called from scheduler
	createdCount, err := ProcessPRFeedbackQuiet(ctx, proj, proj.DB, workID, true, 2)
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

		// Schedule next check in 5 minutes
		nextCheck := time.Now().Add(5 * time.Minute)
		_, err = proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypePRFeedback, nextCheck, nil)
		if err != nil {
			logging.Warn("failed to schedule next PR feedback check", "error", err, "work_id", workID)
		} else {
			logging.Info("Scheduled next PR feedback check", "work_id", workID, "next_check", nextCheck.Format(time.RFC3339))
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

	// Schedule next check in 5 minutes
	nextCheck := time.Now().Add(5 * time.Minute)
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