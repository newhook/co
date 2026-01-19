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
	go func() {
		// Recover from any panics
		defer func() {
			if r := recover(); r != nil {
				logging.Error("scheduler watcher panicked", "error", r)
			}
		}()

		// Schedule initial PR feedback check in 5 minutes
		work, err := proj.DB.GetWork(ctx, workID)
		if err == nil && work != nil && work.PRURL != "" {
			initialCheck := time.Now().Add(5 * time.Minute)
			_, err := proj.DB.ScheduleTask(ctx, workID, db.TaskTypePRFeedback, initialCheck, nil)
			if err != nil {
				logging.Warn("failed to schedule initial PR feedback check", "error", err)
			}

			// Also schedule comment resolution check
			_, err = proj.DB.ScheduleTask(ctx, workID, db.TaskTypeCommentResolution, initialCheck, nil)
			if err != nil {
				logging.Warn("failed to schedule initial comment resolution check", "error", err)
			}
		}

		// Poll the scheduler table every second
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get pending tasks for this work
				tasks, err := proj.DB.GetScheduledTasksForWork(ctx, workID)
				if err != nil {
					logging.Warn("failed to get scheduled tasks", "error", err)
					continue
				}

				// Process any tasks that are due
				for _, task := range tasks {
					if task.ScheduledAt.After(time.Now()) {
						// Not due yet
						continue
					}

					// Mark as executing
					if err := proj.DB.MarkTaskExecuting(ctx, task.ID); err != nil {
						logging.Warn("failed to mark task as executing", "error", err)
						continue
					}

					// Execute based on task type
					switch task.TaskType {
					case db.TaskTypePRFeedback:
						handlePRFeedbackTask(ctx, proj, workID, task)
					case db.TaskTypeCommentResolution:
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
	logging.Debug("scheduled PR feedback check", "work_id", workID)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil || work == nil || work.PRURL == "" {
		// Mark task as completed but don't reschedule
		proj.DB.MarkTaskCompleted(ctx, task.ID)
		return
	}

	logging.Debug("checking PR feedback", "pr_url", work.PRURL)

	// Process PR feedback - suppress output when called from scheduler
	createdCount, err := ProcessPRFeedbackQuiet(ctx, proj, proj.DB, workID, true, 2)
	if err != nil {
		logging.Error("error checking PR feedback", "error", err)
		proj.DB.MarkTaskFailed(ctx, task.ID, err.Error())
	} else {
		if createdCount > 0 {
			logging.Info("created beads from PR feedback", "count", createdCount)
		} else {
			logging.Debug("no new PR feedback found")
		}

		// Mark as completed
		if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
			logging.Warn("failed to mark task as completed", "error", err)
		}

		// Schedule next check in 5 minutes
		nextCheck := time.Now().Add(5 * time.Minute)
		_, err = proj.DB.ScheduleTask(ctx, workID, db.TaskTypePRFeedback, nextCheck, nil)
		if err != nil {
			logging.Warn("failed to schedule next PR feedback check", "error", err)
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
	_, err = proj.DB.ScheduleTask(ctx, workID, db.TaskTypeCommentResolution, nextCheck, nil)
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