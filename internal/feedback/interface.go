package feedback

import (
	"context"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
)

// Processor defines the interface for processing PR feedback.
// This abstraction enables testing without actual GitHub API calls.
type Processor interface {
	// ProcessPRFeedback processes PR feedback for a work and creates beads.
	// Returns the number of beads created and any error.
	ProcessPRFeedback(ctx context.Context, proj *project.Project, database *db.DB, workID string) (int, error)
}

// DefaultProcessor implements Processor using the actual feedback processing logic.
type DefaultProcessor struct{}

// Compile-time check that DefaultProcessor implements Processor.
var _ Processor = (*DefaultProcessor)(nil)

// NewProcessor creates a new default Processor implementation.
func NewProcessor() Processor {
	return &DefaultProcessor{}
}

// ProcessPRFeedback implements Processor.ProcessPRFeedback.
func (p *DefaultProcessor) ProcessPRFeedback(ctx context.Context, proj *project.Project, database *db.DB, workID string) (int, error) {
	return ProcessPRFeedbackQuiet(ctx, proj, database, workID)
}
