package control

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/procmon"
	"github.com/newhook/co/internal/project"
	trackingwatcher "github.com/newhook/co/internal/tracking/watcher"
)

// RunControlPlaneLoop runs the main control plane event loop
func RunControlPlaneLoop(ctx context.Context, proj *project.Project, procManager *procmon.Manager) error {
	// Initialize tracking database watcher
	trackingDBPath := filepath.Join(proj.Root, ".co", "tracking.db")
	watcher, err := trackingwatcher.New(trackingwatcher.DefaultConfig(trackingDBPath))
	if err != nil {
		return fmt.Errorf("failed to create tracking watcher: %w", err)
	}

	if err := watcher.Start(); err != nil {
		_ = watcher.Stop()
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

	// Set up periodic cleanup timer for stale processes
	cleanupInterval := 60 * time.Second
	cleanupTimer := time.NewTimer(cleanupInterval)
	defer cleanupTimer.Stop()

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

		case <-cleanupTimer.C:
			// Periodic cleanup of stale processes
			logging.Debug("Control plane cleaning up stale processes")
			if err := procManager.KillStaleProcesses(ctx); err != nil {
				logging.Warn("failed to cleanup stale processes", "error", err)
			}
			cleanupTimer.Reset(cleanupInterval)
		}
	}
}

// TaskHandler is the signature for all scheduled task handlers.
type TaskHandler func(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error

// taskHandlers maps task types to their handler functions.
var taskHandlers = map[string]TaskHandler{
	db.TaskTypeCreateWorktree:      HandleCreateWorktreeTask,
	db.TaskTypeSpawnOrchestrator:   HandleSpawnOrchestratorTask,
	db.TaskTypePRFeedback:          HandlePRFeedbackTask,
	db.TaskTypeCommentResolution:   HandleCommentResolutionTask,
	db.TaskTypeGitPush:             HandleGitPushTask,
	db.TaskTypeGitHubComment:       HandleGitHubCommentTask,
	db.TaskTypeGitHubResolveThread: HandleGitHubResolveThreadTask,
	db.TaskTypeDestroyWorktree:     HandleDestroyWorktreeTask,
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
		if handler, ok := taskHandlers[task.TaskType]; ok {
			err = handler(ctx, proj, task)
		} else {
			err = fmt.Errorf("unknown task type: %s", task.TaskType)
		}

		// Handle task result
		if err != nil {
			fmt.Printf("[%s] Task failed: %s\n", time.Now().Format("15:04:05"), err)
			HandleTaskError(ctx, proj, task, err.Error())
		} else {
			fmt.Printf("[%s] Task completed: %s\n", time.Now().Format("15:04:05"), task.TaskType)
			if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
				logging.Warn("failed to mark task as completed", "error", err, "task_id", task.ID)
			}
		}
	}
}
