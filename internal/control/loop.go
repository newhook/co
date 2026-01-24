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

// RunControlPlaneLoop runs the main control plane event loop
func RunControlPlaneLoop(ctx context.Context, proj *project.Project) error {
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
	defer watcher.Stop()

	logging.Info("Control plane started with database events")

	// Subscribe to watcher events
	sub := watcher.Broker().Subscribe(ctx)

	// Set up periodic check timer (safety net)
	checkInterval := 30 * time.Second
	checkTimer := time.NewTimer(checkInterval)
	defer checkTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			logging.Debug("Control plane stopping due to context cancellation")
			fmt.Println("\nControl plane stopped.")
			return nil

		case event, ok := <-sub:
			if !ok {
				logging.Debug("Watcher subscription closed")
				return nil
			}

			// Handle database change event
			if event.Payload.Type == trackingwatcher.DBChanged {
				logging.Debug("Database changed, checking scheduled tasks")
				ProcessAllDueTasks(ctx, proj)
			}

		case <-checkTimer.C:
			// Periodic check as a safety net
			logging.Debug("Control plane periodic check")
			ProcessAllDueTasks(ctx, proj)
			checkTimer.Reset(checkInterval)
		}
	}
}

// ProcessAllDueTasks checks for and executes any scheduled tasks that are due across all works
func ProcessAllDueTasks(ctx context.Context, proj *project.Project) {
	// Get the next due task globally (not work-specific)
	for {
		task, err := proj.DB.GetNextScheduledTask(ctx)
		if err != nil {
			logging.Warn("failed to get next scheduled task", "error", err)
			return
		}

		if task == nil {
			// No more due tasks
			return
		}

		logging.Info("Executing scheduled task",
			"task_id", task.ID,
			"task_type", task.TaskType,
			"work_id", task.WorkID,
			"scheduled_at", task.ScheduledAt.Format(time.RFC3339))

		// Print to stdout
		fmt.Printf("[%s] Executing %s for %s\n", time.Now().Format("15:04:05"), task.TaskType, task.WorkID)

		// Mark as executing
		if err := proj.DB.MarkTaskExecuting(ctx, task.ID); err != nil {
			logging.Warn("failed to mark task as executing", "error", err)
			continue
		}

		// Execute based on task type
		var taskErr error
		switch task.TaskType {
		case db.TaskTypeCreateWorktree:
			taskErr = HandleCreateWorktreeTask(ctx, proj, task)
		case db.TaskTypeSpawnOrchestrator:
			taskErr = HandleSpawnOrchestratorTask(ctx, proj, task)
		case db.TaskTypePRFeedback:
			HandlePRFeedbackTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeCommentResolution:
			HandleCommentResolutionTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeGitPush:
			taskErr = HandleGitPushTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeGitHubComment:
			taskErr = HandleGitHubCommentTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeGitHubResolveThread:
			taskErr = HandleGitHubResolveThreadTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeDestroyWorktree:
			taskErr = HandleDestroyWorktreeTask(ctx, proj, task)
		default:
			taskErr = fmt.Errorf("unknown task type: %s", task.TaskType)
		}

		// Handle task result
		if taskErr != nil {
			fmt.Printf("[%s] Task failed: %s\n", time.Now().Format("15:04:05"), taskErr)
			HandleTaskError(ctx, proj, task, taskErr.Error())
		} else {
			fmt.Printf("[%s] Task completed: %s\n", time.Now().Format("15:04:05"), task.TaskType)
		}
	}
}
