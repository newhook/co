package cmd

import (
	"context"

	"github.com/newhook/co/internal/control"
	"github.com/newhook/co/internal/project"
)

// controlCallbacks implements control.Callbacks
type controlCallbacks struct{}

func (c *controlCallbacks) ProcessPRFeedback(ctx context.Context, proj *project.Project, workID string, minPriority int) (int, error) {
	return ProcessPRFeedback(ctx, proj, proj.DB, workID, minPriority)
}

func (c *controlCallbacks) ProcessPRFeedbackQuiet(ctx context.Context, proj *project.Project, workID string, minPriority int) (int, error) {
	return ProcessPRFeedbackQuiet(ctx, proj, proj.DB, workID, minPriority)
}

func (c *controlCallbacks) CheckAndResolveComments(ctx context.Context, proj *project.Project, workID, prURL string) {
	checkAndResolveComments(ctx, proj, workID, prURL)
}

func (c *controlCallbacks) CheckAndResolveCommentsQuiet(ctx context.Context, proj *project.Project, workID, prURL string) {
	checkAndResolveCommentsQuiet(ctx, proj, workID, prURL)
}

func init() {
	control.SetCallbacks(&controlCallbacks{})
}
