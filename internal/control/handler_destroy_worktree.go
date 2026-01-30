package control

import (
	"context"
	"io"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/work"
)

// HandleDestroyWorktreeTask handles a scheduled worktree destruction task
func HandleDestroyWorktreeTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID

	logging.Info("Destroying worktree for work",
		"work_id", workID,
		"attempt", task.AttemptCount+1)

	// Check if work still exists
	workRecord, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return err
	}
	if workRecord == nil {
		// Work was already deleted - nothing to do
		logging.Info("Work not found, task will be marked completed", "work_id", workID)
		return nil
	}

	// Delegate to the shared DestroyWork function
	if err := work.DestroyWork(ctx, proj, workID, io.Discard); err != nil {
		return err
	}

	logging.Info("Worktree destroyed successfully", "work_id", workID)

	return nil
}
