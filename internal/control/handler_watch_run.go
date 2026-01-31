package control

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
)

// HandleWatchWorkflowRunTask watches a GitHub Actions workflow run until completion.
// When the run completes (success or failure), it schedules a PRFeedback task to run immediately.
// This replaces polling for workflow runs that are in-progress.
//
// Required metadata:
// - run_id: The GitHub Actions workflow run ID to watch
// - repo: The repository in owner/repo format
func (cp *ControlPlane) HandleWatchWorkflowRunTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	runIDStr := task.Metadata["run_id"]
	repo := task.Metadata["repo"]

	if runIDStr == "" || repo == "" {
		return fmt.Errorf("watch_workflow_run task missing run_id or repo metadata")
	}

	runID, err := strconv.ParseInt(runIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid run_id: %w", err)
	}

	logging.Info("Starting gh run watch",
		"work_id", workID,
		"run_id", runID,
		"repo", repo)

	// Run gh run watch --exit-status
	// This command blocks until the run completes and returns:
	// - exit code 0 if the run succeeded
	// - non-zero exit code if the run failed
	cmd := exec.CommandContext(ctx, "gh", "run", "watch", runIDStr,
		"--repo", repo,
		"--exit-status")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a context cancellation (work was destroyed)
		if errors.Is(ctx.Err(), context.Canceled) {
			logging.Info("Workflow watch cancelled (context cancelled)",
				"work_id", workID,
				"run_id", runID)
			return nil // Don't reschedule - work is being destroyed
		}

		// gh run watch returns non-zero when the run fails
		// This is expected behavior - we still want to fetch feedback
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			logging.Info("Workflow run completed with failure",
				"work_id", workID,
				"run_id", runID,
				"exit_code", exitErr.ExitCode(),
				"output", string(output))
		} else {
			// Actual error (network, auth, invalid run, etc.)
			return fmt.Errorf("gh run watch failed: %w\nOutput: %s", err, output)
		}
	} else {
		logging.Info("Workflow run completed successfully",
			"work_id", workID,
			"run_id", runID)
	}

	// Schedule PRFeedback task to run immediately
	_, err = proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypePRFeedback, time.Now())
	if err != nil {
		logging.Warn("Failed to schedule PRFeedback task after workflow watch",
			"work_id", workID,
			"error", err)
		// Don't return error - the watch succeeded, just scheduling failed
	} else {
		logging.Info("Scheduled PRFeedback task after workflow completion",
			"work_id", workID,
			"run_id", runID)
	}

	return nil
}

// ScheduleWatchWorkflowRun schedules a task to watch a workflow run.
// Uses the run_id as the idempotency key to prevent duplicate watchers.
func ScheduleWatchWorkflowRun(ctx context.Context, proj *project.Project, workID string, runID int64, repo string) error {
	// Use run_id as idempotency key to prevent duplicate watchers for the same run
	idempotencyKey := fmt.Sprintf("watch_run_%d", runID)

	metadata := map[string]string{
		"run_id": strconv.FormatInt(runID, 10),
		"repo":   repo,
	}

	err := proj.DB.ScheduleTaskWithRetry(ctx, workID, db.TaskTypeWatchWorkflowRun, time.Now(), metadata, idempotencyKey, 1)
	if err != nil {
		return fmt.Errorf("failed to schedule watch_workflow_run task: %w", err)
	}

	logging.Info("Scheduled watch_workflow_run task",
		"work_id", workID,
		"run_id", runID,
		"repo", repo)

	return nil
}
