package cmd

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
)

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

	for _, feedback := range feedbacks {
		if feedback.BeadID == nil || feedback.SourceID == nil {
			continue
		}

		// Get bead details for close reason
		bead, err := beadClient.GetBead(ctx, *feedback.BeadID)
		if err != nil {
			fmt.Printf("Warning: failed to get bead %s: %v\n", *feedback.BeadID, err)
			continue
		}

		if bead == nil {
			continue
		}

		// Construct resolution message
		resolutionMessage := fmt.Sprintf("✅ Resolved in work %s (issue %s)", workID, *feedback.BeadID)
		if bead.CloseReason != "" {
			resolutionMessage = fmt.Sprintf("✅ Resolved in work %s (issue %s): %s", workID, *feedback.BeadID, bead.CloseReason)
		}

		// Parse PR URL to get owner/repo/pr_number
		// Expected format: https://github.com/owner/repo/pull/123
		parts := strings.Split(feedback.PRURL, "/")
		if len(parts) < 7 || parts[5] != "pull" {
			fmt.Printf("Warning: invalid PR URL format: %s\n", feedback.PRURL)
			continue
		}

		isReviewComment := strings.Contains(feedback.Source, "Review:") && *feedback.SourceID != ""

		// Build the list of tasks to schedule
		var tasksToSchedule []db.ScheduledTaskParams

		// GitHub comment task
		commentIdempotencyKey := fmt.Sprintf("github-comment-%s-%s", workID, feedback.ID)
		commentMetadata := map[string]string{
			"pr_url": feedback.PRURL,
			"body":   resolutionMessage,
		}
		if isReviewComment {
			commentMetadata["reply_to_id"] = *feedback.SourceID
		}
		tasksToSchedule = append(tasksToSchedule, db.ScheduledTaskParams{
			WorkID:         workID,
			TaskType:       db.TaskTypeGitHubComment,
			ScheduledAt:    time.Now(),
			Metadata:       commentMetadata,
			IdempotencyKey: commentIdempotencyKey,
			MaxAttempts:    db.DefaultMaxAttempts,
		})

		// For review comments, also add thread resolution task
		var resolveIdempotencyKey string
		var commentID int
		if isReviewComment {
			var convErr error
			commentID, convErr = strconv.Atoi(*feedback.SourceID)
			if convErr != nil {
				fmt.Printf("Warning: invalid comment ID %s: %v\n", *feedback.SourceID, convErr)
				continue
			}

			resolveIdempotencyKey = fmt.Sprintf("github-resolve-%s-%s", workID, feedback.ID)
			tasksToSchedule = append(tasksToSchedule, db.ScheduledTaskParams{
				WorkID:      workID,
				TaskType:    db.TaskTypeGitHubResolveThread,
				ScheduledAt: time.Now(),
				Metadata: map[string]string{
					"pr_url":     feedback.PRURL,
					"comment_id": *feedback.SourceID,
				},
				IdempotencyKey: resolveIdempotencyKey,
				MaxAttempts:    db.DefaultMaxAttempts,
			})
		}

		// Atomically mark feedback resolved and schedule all tasks in a single transaction
		if err := database.MarkFeedbackResolvedAndScheduleTasks(ctx, feedback.ID, tasksToSchedule); err != nil {
			fmt.Printf("Warning: failed to mark feedback %s as resolved and schedule tasks: %v\n", feedback.ID, err)
			continue
		}

		// Attempt immediate comment post (optimistic execution)
		ghClient := github.NewClient()
		var commentErr error
		if isReviewComment {
			sourceID, _ := strconv.Atoi(*feedback.SourceID)
			commentErr = ghClient.PostReviewReply(ctx, feedback.PRURL, sourceID, resolutionMessage)
		} else {
			commentErr = ghClient.PostPRComment(ctx, feedback.PRURL, resolutionMessage)
		}
		if commentErr != nil {
			logging.Warn("Initial GitHub comment failed, scheduler will retry", "error", commentErr, "work_id", workID, "bead_id", *feedback.BeadID)
			fmt.Printf("Warning: initial comment post failed, will retry in background: %v\n", commentErr)
		} else {
			// Success - mark scheduled task as completed
			if markErr := database.MarkTaskCompletedByIdempotencyKey(ctx, commentIdempotencyKey); markErr != nil {
				logging.Warn("failed to mark github comment task as completed", "error", markErr, "work_id", workID)
			}
			fmt.Printf("Successfully posted resolution comment for bead %s on GitHub\n", *feedback.BeadID)
		}

		// For review comments, also attempt immediate thread resolution
		if isReviewComment {
			if resolveErr := ghClient.ResolveReviewThread(ctx, feedback.PRURL, commentID); resolveErr != nil {
				logging.Warn("Initial GitHub thread resolution failed, scheduler will retry", "error", resolveErr, "work_id", workID, "comment_id", commentID)
				fmt.Printf("Warning: initial thread resolution failed, will retry in background: %v\n", resolveErr)
			} else {
				// Success - mark scheduled task as completed
				if markErr := database.MarkTaskCompletedByIdempotencyKey(ctx, resolveIdempotencyKey); markErr != nil {
					logging.Warn("failed to mark github resolve thread task as completed", "error", markErr, "work_id", workID)
				}
				fmt.Printf("Successfully resolved review thread for bead %s\n", *feedback.BeadID)
			}
		}
	}

	return nil
}