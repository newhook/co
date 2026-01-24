package cmd

import (
	"context"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/feedback"
)

// ResolveFeedbackForBeads posts resolution comments on GitHub for closed beads.
// Delegates to internal/feedback.ResolveFeedbackForBeads.
func ResolveFeedbackForBeads(ctx context.Context, database *db.DB, beadClient *beads.Client, workID string, closedBeadIDs []string) error {
	return feedback.ResolveFeedbackForBeads(ctx, database, beadClient, workID, closedBeadIDs)
}
