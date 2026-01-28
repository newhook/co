package control

import (
	"context"
	"fmt"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/feedback"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
)

// HandlePRFeedbackTask handles a scheduled PR feedback check.
// Returns nil on success, error on failure (caller handles retry/completion).
func HandlePRFeedbackTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	logging.Debug("Starting PR feedback check task", "task_id", task.ID, "work_id", workID)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil || work == nil || work.PRURL == "" {
		logging.Debug("No PR URL for work, not rescheduling", "work_id", workID, "has_pr", work != nil && work.PRURL != "")
		// Don't reschedule - scheduling happens when PR is created
		return nil
	}

	logging.Debug("Checking PR feedback", "pr_url", work.PRURL, "work_id", workID)

	// Process PR feedback - creates beads but doesn't add them to work
	createdCount, err := feedback.ProcessPRFeedbackQuiet(ctx, proj, proj.DB, workID)
	if err != nil {
		return fmt.Errorf("failed to check PR feedback: %w", err)
	}

	if createdCount > 0 {
		logging.Info("Created beads from PR feedback", "count", createdCount, "work_id", workID)
	} else {
		logging.Debug("No new PR feedback found", "work_id", workID)
	}

	// Schedule next check using configured interval
	nextInterval := proj.Config.Scheduler.GetPRFeedbackInterval()
	nextCheck := time.Now().Add(nextInterval)
	_, err = proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypePRFeedback, nextCheck)
	if err != nil {
		logging.Warn("failed to schedule next PR feedback check", "error", err, "work_id", workID)
	} else {
		logging.Info("Scheduled next PR feedback check", "work_id", workID, "next_check", nextCheck.Format(time.RFC3339), "interval", nextInterval)
	}

	return nil
}
