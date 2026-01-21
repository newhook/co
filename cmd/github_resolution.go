package cmd

import (
	"context"
	"fmt"
	"os/exec"
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
// unresolved feedback items. Uses the transactional outbox pattern: marks feedback
// as resolved first, schedules comment tasks, then attempts optimistic execution.
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

		// Mark feedback as resolved in database FIRST (transactional outbox pattern)
		// This ensures idempotency - if we crash after this, we won't try to resolve again
		if err := database.MarkFeedbackResolved(ctx, feedback.ID); err != nil {
			fmt.Printf("Warning: failed to mark feedback %s as resolved: %v\n", feedback.ID, err)
			continue
		}

		// Schedule GitHub comment task with idempotency key
		commentIdempotencyKey := fmt.Sprintf("github-comment-%s-%s", workID, feedback.ID)
		metadata := map[string]string{
			"pr_url": feedback.PRURL,
			"body":   resolutionMessage,
		}
		if isReviewComment {
			metadata["reply_to_id"] = *feedback.SourceID
		}
		_, schedErr := database.ScheduleTaskWithRetry(ctx, workID, db.TaskTypeGitHubComment, time.Now(), metadata, commentIdempotencyKey, db.DefaultMaxAttempts)
		if schedErr != nil {
			logging.Warn("failed to schedule github comment task", "error", schedErr, "work_id", workID, "feedback_id", feedback.ID)
		}

		// Attempt immediate comment post (optimistic execution)
		commentErr := postGitHubComment(ctx, feedback.PRURL, resolutionMessage, isReviewComment, feedback.SourceID)
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

		// For review comments, also schedule thread resolution
		if isReviewComment {
			commentID, convErr := strconv.Atoi(*feedback.SourceID)
			if convErr != nil {
				fmt.Printf("Warning: invalid comment ID %s: %v\n", *feedback.SourceID, convErr)
				continue
			}

			// Schedule GitHub resolve thread task
			resolveIdempotencyKey := fmt.Sprintf("github-resolve-%s-%s", workID, feedback.ID)
			_, schedErr := database.ScheduleTaskWithRetry(ctx, workID, db.TaskTypeGitHubResolveThread, time.Now(), map[string]string{
				"pr_url":     feedback.PRURL,
				"comment_id": *feedback.SourceID,
			}, resolveIdempotencyKey, db.DefaultMaxAttempts)
			if schedErr != nil {
				logging.Warn("failed to schedule github resolve thread task", "error", schedErr, "work_id", workID, "feedback_id", feedback.ID)
			}

			// Attempt immediate thread resolution (optimistic execution)
			ghClient := github.NewClient()
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

// postGitHubComment posts a comment to a GitHub PR.
// If isReviewComment is true and replyToID is provided, it replies to a specific comment thread.
func postGitHubComment(ctx context.Context, prURL string, body string, isReviewComment bool, replyToID *string) error {
	parts := strings.Split(prURL, "/")
	if len(parts) < 7 || parts[5] != "pull" {
		return fmt.Errorf("invalid PR URL format: %s", prURL)
	}

	owner := parts[3]
	repo := parts[4]
	prNumber := parts[6]

	var cmd *exec.Cmd
	if isReviewComment && replyToID != nil && *replyToID != "" {
		// For review comments, reply to the specific comment thread
		cmd = exec.CommandContext(ctx, "gh", "api", "-X", "POST",
			fmt.Sprintf("/repos/%s/%s/pulls/%s/comments/%s/replies", owner, repo, prNumber, *replyToID),
			"--field", fmt.Sprintf("body=%s", body),
			"--header", "Accept: application/vnd.github.v3+json")
	} else {
		// Post a general PR comment
		cmd = exec.CommandContext(ctx, "gh", "api", "-X", "POST",
			fmt.Sprintf("/repos/%s/%s/issues/%s/comments", owner, repo, prNumber),
			"--field", fmt.Sprintf("body=%s", body),
			"--header", "Accept: application/vnd.github.v3+json")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to post comment: %w\n%s", err, string(output))
	}
	return nil
}