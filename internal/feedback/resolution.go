package feedback

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
)

// CheckAndResolveComments checks for feedback items where the bead is closed and posts resolution comments to GitHub
func CheckAndResolveComments(ctx context.Context, proj *project.Project, workID, prURL string) {
	checkAndResolveCommentsInternal(ctx, proj, workID, prURL, false)
}

// CheckAndResolveCommentsQuiet checks for feedback items quietly
func CheckAndResolveCommentsQuiet(ctx context.Context, proj *project.Project, workID, prURL string) {
	checkAndResolveCommentsInternal(ctx, proj, workID, prURL, true)
}

func checkAndResolveCommentsInternal(ctx context.Context, proj *project.Project, workID, _ string, quiet bool) {
	// Get unresolved feedback items for this work
	feedbacks, err := proj.DB.GetUnresolvedFeedbackForClosedBeads(ctx, workID)
	if err != nil {
		if !quiet {
			fmt.Printf("Error getting unresolved feedback: %v\n", err)
		}
		logging.Error("failed to get unresolved feedback", "error", err)
		return
	}

	if len(feedbacks) == 0 {
		return
	}

	if !quiet {
		fmt.Printf("\nChecking %d feedback items for resolution...\n", len(feedbacks))
	}
	logging.Debug("checking feedback items for resolution", "count", len(feedbacks))

	// Collect closed bead IDs
	var closedBeadIDs []string
	for _, fb := range feedbacks {
		if fb.BeadID == nil || fb.SourceID == nil {
			continue
		}

		// Check if the bead is actually closed
		bead, err := proj.Beads.GetBead(ctx, *fb.BeadID)
		if err != nil {
			if !quiet {
				fmt.Printf("Error getting bead %s: %v\n", *fb.BeadID, err)
			}
			logging.Error("failed to get bead", "bead_id", *fb.BeadID, "error", err)
			continue
		}

		if bead != nil && bead.Status == beads.StatusClosed {
			closedBeadIDs = append(closedBeadIDs, *fb.BeadID)
		}
	}

	// Resolve feedback for all closed beads
	if len(closedBeadIDs) > 0 {
		if err := ResolveFeedbackForBeads(ctx, proj.DB, proj.Beads, workID, closedBeadIDs); err != nil {
			if !quiet {
				fmt.Printf("Error resolving feedback comments: %v\n", err)
			}
			logging.Error("failed to resolve feedback comments", "error", err)
		}
	}
}

// ResolveFeedbackForBeads posts resolution comments on GitHub for closed beads.
// It checks the provided beads and posts resolution comments for any associated
// unresolved feedback items. Uses the transactional outbox pattern: atomically marks
// feedback as resolved and schedules comment tasks, then attempts optimistic execution.
func ResolveFeedbackForBeads(ctx context.Context, database *db.DB, beadClient *beads.Client, workID string, closedBeadIDs []string) error {
	if len(closedBeadIDs) == 0 {
		return nil
	}

	// Get unresolved feedback for the closed beads
	feedbacks, err := database.GetUnresolvedFeedbackForBeads(ctx, closedBeadIDs)
	if err != nil {
		return fmt.Errorf("failed to get unresolved feedback: %w", err)
	}

	if len(feedbacks) == 0 {
		return nil
	}

	fmt.Printf("\nResolving %d GitHub feedback items for closed beads...\n", len(feedbacks))

	for _, fb := range feedbacks {
		if fb.BeadID == nil || fb.SourceID == nil {
			continue
		}

		// Get bead details for close reason
		bead, err := beadClient.GetBead(ctx, *fb.BeadID)
		if err != nil {
			fmt.Printf("Warning: failed to get bead %s: %v\n", *fb.BeadID, err)
			continue
		}

		if bead == nil {
			continue
		}

		// Construct resolution message
		resolutionMessage := fmt.Sprintf("✅ Resolved in work %s (issue %s)", workID, *fb.BeadID)
		if bead.CloseReason != "" {
			resolutionMessage = fmt.Sprintf("✅ Resolved in work %s (issue %s): %s", workID, *fb.BeadID, bead.CloseReason)
		}

		// Parse PR URL to get owner/repo/pr_number
		// Expected format: https://github.com/owner/repo/pull/123
		parts := strings.Split(fb.PRURL, "/")
		if len(parts) < 7 || parts[5] != "pull" {
			fmt.Printf("Warning: invalid PR URL format: %s\n", fb.PRURL)
			continue
		}

		isReviewComment := fb.IsReviewComment()

		// Build the list of tasks to schedule
		var tasksToSchedule []db.ScheduledTaskParams

		// GitHub comment task
		commentIdempotencyKey := fmt.Sprintf("github-comment-%s-%s", workID, fb.ID)
		commentMetadata := map[string]string{
			"pr_url": fb.PRURL,
			"body":   resolutionMessage,
		}
		if isReviewComment {
			commentMetadata["reply_to_id"] = *fb.SourceID
		}
		tasksToSchedule = append(tasksToSchedule, db.ScheduledTaskParams{
			WorkID:         workID,
			TaskType:       db.TaskTypeGitHubComment,
			ScheduledAt:    time.Now().Add(db.OptimisticExecutionDelay),
			Metadata:       commentMetadata,
			IdempotencyKey: commentIdempotencyKey,
			MaxAttempts:    db.DefaultMaxAttempts,
		})

		// For review comments, also add thread resolution task
		var resolveIdempotencyKey string
		var commentID int
		if isReviewComment {
			var convErr error
			commentID, convErr = strconv.Atoi(*fb.SourceID)
			if convErr != nil {
				fmt.Printf("Warning: invalid comment ID %s: %v\n", *fb.SourceID, convErr)
				continue
			}

			resolveIdempotencyKey = fmt.Sprintf("github-resolve-%s-%s", workID, fb.ID)
			tasksToSchedule = append(tasksToSchedule, db.ScheduledTaskParams{
				WorkID:      workID,
				TaskType:    db.TaskTypeGitHubResolveThread,
				ScheduledAt: time.Now().Add(db.OptimisticExecutionDelay),
				Metadata: map[string]string{
					"pr_url":     fb.PRURL,
					"comment_id": *fb.SourceID,
				},
				IdempotencyKey: resolveIdempotencyKey,
				MaxAttempts:    db.DefaultMaxAttempts,
			})
		}

		// Atomically mark feedback resolved and schedule all tasks in a single transaction
		if err := database.MarkFeedbackResolvedAndScheduleTasks(ctx, fb.ID, tasksToSchedule); err != nil {
			fmt.Printf("Warning: failed to mark feedback %s as resolved and schedule tasks: %v\n", fb.ID, err)
			continue
		}

		// Attempt immediate comment post (optimistic execution)
		ghClient := github.NewClient()
		var commentErr error
		if isReviewComment {
			sourceID, _ := strconv.Atoi(*fb.SourceID)
			commentErr = ghClient.PostReviewReply(ctx, fb.PRURL, sourceID, resolutionMessage)
		} else {
			commentErr = ghClient.PostPRComment(ctx, fb.PRURL, resolutionMessage)
		}
		if commentErr != nil {
			logging.Warn("Initial GitHub comment failed, scheduler will retry", "error", commentErr, "work_id", workID, "bead_id", *fb.BeadID)
			fmt.Printf("Warning: initial comment post failed, will retry in background: %v\n", commentErr)
		} else {
			// Success - mark scheduled task as completed
			if markErr := database.MarkTaskCompletedByIdempotencyKey(ctx, commentIdempotencyKey); markErr != nil {
				logging.Warn("failed to mark github comment task as completed", "error", markErr, "work_id", workID)
			}
			fmt.Printf("Successfully posted resolution comment for bead %s on GitHub\n", *fb.BeadID)
		}

		// For review comments, also attempt immediate thread resolution
		if isReviewComment {
			if resolveErr := ghClient.ResolveReviewThread(ctx, fb.PRURL, commentID); resolveErr != nil {
				logging.Warn("Initial GitHub thread resolution failed, scheduler will retry", "error", resolveErr, "work_id", workID, "comment_id", commentID)
				fmt.Printf("Warning: initial thread resolution failed, will retry in background: %v\n", resolveErr)
			} else {
				// Success - mark scheduled task as completed
				if markErr := database.MarkTaskCompletedByIdempotencyKey(ctx, resolveIdempotencyKey); markErr != nil {
					logging.Warn("failed to mark github resolve thread task as completed", "error", markErr, "work_id", workID)
				}
				fmt.Printf("Successfully resolved review thread for bead %s\n", *fb.BeadID)
			}
		}
	}

	return nil
}
