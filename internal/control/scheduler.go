package control

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	trackingwatcher "github.com/newhook/co/internal/tracking/watcher"
)

// ScheduleDestroyWorktree schedules a worktree destruction task for the control plane.
// This is the preferred way to destroy a worktree as it runs asynchronously with retry support.
func ScheduleDestroyWorktree(ctx context.Context, proj *project.Project, workID string) error {
	idempotencyKey := fmt.Sprintf("destroy-worktree-%s", workID)
	_, err := proj.DB.ScheduleTaskWithRetry(ctx, workID, db.TaskTypeDestroyWorktree, time.Now(), nil, idempotencyKey, db.DefaultMaxAttempts)
	if err != nil {
		return fmt.Errorf("failed to schedule destroy worktree task: %w", err)
	}
	logging.Info("Scheduled destroy worktree task", "work_id", workID)
	return nil
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
			HandlePRFeedbackTask(ctx, proj, workID, task)
		case db.TaskTypeCommentResolution:
			logging.Debug("Handling comment resolution task", "task_id", task.ID, "work_id", workID)
			HandleCommentResolutionTask(ctx, proj, workID, task)
		case db.TaskTypeGitPush:
			logging.Debug("Handling git push task", "task_id", task.ID, "work_id", workID)
			taskErr = HandleGitPushTask(ctx, proj, workID, task)
		case db.TaskTypeGitHubComment:
			logging.Debug("Handling GitHub comment task", "task_id", task.ID, "work_id", workID)
			taskErr = HandleGitHubCommentTask(ctx, proj, workID, task)
		case db.TaskTypeGitHubResolveThread:
			logging.Debug("Handling GitHub resolve thread task", "task_id", task.ID, "work_id", workID)
			taskErr = HandleGitHubResolveThreadTask(ctx, proj, workID, task)
		default:
			taskErr = fmt.Errorf("unknown task type: %s", task.TaskType)
		}

		// Handle task result for one-shot tasks
		if taskErr != nil {
			HandleTaskError(ctx, proj, task, taskErr.Error())
		}
	}
}
