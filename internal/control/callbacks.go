package control

import (
	"context"

	"github.com/newhook/co/internal/project"
)

// Callbacks defines the interface for control plane to call back into cmd.
// This breaks the circular dependency between control and cmd packages.
type Callbacks interface {
	// ProcessPRFeedback processes PR feedback and returns the count of created beads.
	ProcessPRFeedback(ctx context.Context, proj *project.Project, workID string, minPriority int) (int, error)

	// ProcessPRFeedbackQuiet processes PR feedback quietly.
	ProcessPRFeedbackQuiet(ctx context.Context, proj *project.Project, workID string, minPriority int) (int, error)

	// CheckAndResolveComments checks for feedback items where the bead is closed and posts resolution comments.
	CheckAndResolveComments(ctx context.Context, proj *project.Project, workID, prURL string)

	// CheckAndResolveCommentsQuiet checks for feedback items quietly.
	CheckAndResolveCommentsQuiet(ctx context.Context, proj *project.Project, workID, prURL string)
}

var callbacks Callbacks

// SetCallbacks sets the callbacks implementation.
// This should be called early during initialization.
func SetCallbacks(cb Callbacks) {
	callbacks = cb
}

// GetCallbacks returns the current callbacks implementation.
func GetCallbacks() Callbacks {
	return callbacks
}
