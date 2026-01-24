package cmd

import (
	"context"

	"github.com/newhook/co/internal/control"
	"github.com/newhook/co/internal/project"
)

// StartSchedulerWatcher starts a goroutine that watches for scheduled tasks for a single work.
// Delegates to internal/control.StartSchedulerWatcher.
//
// Deprecated: This per-work scheduler watcher is no longer used by the orchestrator.
// The control plane (co control) now handles scheduled tasks globally across all works.
// This function is kept for backwards compatibility but should not be called directly.
func StartSchedulerWatcher(ctx context.Context, proj *project.Project, workID string) error {
	return control.StartSchedulerWatcher(ctx, proj, workID)
}
