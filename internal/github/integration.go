package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Integration handles the integration between GitHub PR feedback and beads.
type Integration struct {
	client    *Client
	processor *FeedbackProcessor
	creator   *BeadCreator
}

// NewIntegration creates a new GitHub integration.
func NewIntegration(rules *FeedbackRules) *Integration {
	client := NewClient()
	processor := NewFeedbackProcessor(client, rules)
	creator := NewBeadCreator(processor)

	return &Integration{
		client:    client,
		processor: processor,
		creator:   creator,
	}
}

// ProcessPRFeedback processes PR feedback and returns bead info.
func (i *Integration) ProcessPRFeedback(ctx context.Context, prURL, rootIssueID string) ([]BeadInfo, error) {
	return i.creator.ProcessPRAndCreateBeadInfo(ctx, prURL, rootIssueID)
}

// CreateBeadFromFeedback creates a bead using the bd CLI.
func (i *Integration) CreateBeadFromFeedback(ctx context.Context, beadInfo BeadInfo) (string, error) {
	// Build the bd create command
	args := []string{"create",
		"--title", beadInfo.Title,
		"--type", beadInfo.Type,
		"--priority", fmt.Sprintf("%d", beadInfo.Priority),
		"--parent", beadInfo.ParentID,
		"--description", beadInfo.Description,
	}

	// Add labels
	for _, label := range beadInfo.Labels {
		args = append(args, "--label", label)
	}

	// Execute bd create
	cmd := exec.CommandContext(ctx, "bd", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create bead: %w\nOutput: %s", err, output)
	}

	// Parse the output to get the bead ID
	// Expected format: "Created issue: beads-xxx"
	outputStr := string(output)
	if strings.Contains(outputStr, "Created issue:") {
		parts := strings.Fields(outputStr)
		for i, part := range parts {
			if part == "issue:" && i+1 < len(parts) {
				return parts[i+1], nil
			}
		}
	}

	// Fallback: try to extract anything that looks like a bead ID
	for _, line := range strings.Split(outputStr, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "beads-") {
			return strings.Fields(line)[0], nil
		}
	}

	return "", fmt.Errorf("could not parse bead ID from output: %s", outputStr)
}

// AddBeadToWork adds a bead to a work using the work_beads table.
func (i *Integration) AddBeadToWork(ctx context.Context, workID, beadID string) error {
	// This would typically be done through the database, but since we're using
	// the bd CLI as the source of truth, we need to ensure the bead is properly
	// tracked in the work_beads table. This is handled by the orchestrator.
	// For now, we'll just verify the bead exists.

	cmd := exec.CommandContext(ctx, "bd", "show", beadID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bead %s not found: %w", beadID, err)
	}

	return nil
}

// FetchAndStoreFeedback fetches PR feedback and stores it in the database.
// This is called by the orchestrator to populate the pr_feedback table.
func (i *Integration) FetchAndStoreFeedback(ctx context.Context, prURL string) ([]FeedbackItem, error) {
	return i.processor.ProcessPRFeedback(ctx, prURL)
}

// PollPRStatus polls a PR for status changes.
func (i *Integration) PollPRStatus(ctx context.Context, prURL string, interval time.Duration, callback func(*PRStatus) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := i.client.GetPRStatus(ctx, prURL)
			if err != nil {
				// Log error but continue polling
				fmt.Printf("Error fetching PR status: %v\n", err)
				continue
			}

			if err := callback(status); err != nil {
				return err
			}

			// Stop polling if PR is closed or merged
			if status.State == "CLOSED" || status.State == "MERGED" {
				return nil
			}
		}
	}
}

// CheckForNewFeedback checks if there's new feedback since the last check.
func (i *Integration) CheckForNewFeedback(ctx context.Context, prURL string, lastCheck time.Time) ([]FeedbackItem, error) {
	allFeedback, err := i.processor.ProcessPRFeedback(ctx, prURL)
	if err != nil {
		return nil, err
	}

	// Filter for feedback created after lastCheck
	var newFeedback []FeedbackItem
	for _, item := range allFeedback {
		// Since FeedbackItem doesn't have a timestamp, we'd need to enhance
		// the structure or track this differently. For now, return all feedback.
		// In a real implementation, we'd check timestamps from the API responses.
		newFeedback = append(newFeedback, item)
	}

	return newFeedback, nil
}

// ResolveFeedback marks feedback as resolved when its associated bead is completed.
func (i *Integration) ResolveFeedback(ctx context.Context, beadID string) error {
	// Check if the bead is closed
	cmd := exec.CommandContext(ctx, "bd", "show", beadID)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check bead status: %w", err)
	}

	// Check if the output indicates the bead is closed
	outputStr := string(output)
	if strings.Contains(outputStr, "CLOSED") || strings.Contains(outputStr, "✓") {
		// Bead is closed, feedback is resolved
		return nil
	}

	return fmt.Errorf("bead %s is not closed", beadID)
}

// CreateBeadsForWork creates beads for all unprocessed feedback for a work.
func (i *Integration) CreateBeadsForWork(ctx context.Context, workID, prURL, rootIssueID string) ([]string, error) {
	// Fetch all feedback
	beadInfos, err := i.ProcessPRFeedback(ctx, prURL, rootIssueID)
	if err != nil {
		return nil, fmt.Errorf("failed to process PR feedback: %w", err)
	}

	var createdBeadIDs []string

	// Create beads for each feedback item
	for _, beadInfo := range beadInfos {
		beadID, err := i.CreateBeadFromFeedback(ctx, beadInfo)
		if err != nil {
			// Log error but continue with other beads
			fmt.Printf("Failed to create bead for '%s': %v\n", beadInfo.Title, err)
			continue
		}

		createdBeadIDs = append(createdBeadIDs, beadID)

		// Add bead to work
		if err := i.AddBeadToWork(ctx, workID, beadID); err != nil {
			fmt.Printf("Failed to add bead %s to work %s: %v\n", beadID, workID, err)
		}
	}

	return createdBeadIDs, nil
}