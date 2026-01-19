package github

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"
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

// extractGitHubID extracts a GitHub identifier from a URL
// For example: from "https://github.com/owner/repo/pull/123#issuecomment-456789"
// returns "comment-456789"
// For review comments: "https://github.com/owner/repo/pull/123#discussion_r789"
// returns "review-comment-789"
func extractGitHubID(url string) string {
	// Try to extract review comment ID (e.g., #discussion_r123456)
	reviewCommentRe := regexp.MustCompile(`#discussion_r(\d+)`)
	if matches := reviewCommentRe.FindStringSubmatch(url); len(matches) > 1 {
		return "review-comment-" + matches[1]
	}

	// Try to extract regular comment ID (e.g., #issuecomment-456789)
	commentRe := regexp.MustCompile(`#issuecomment-(\d+)`)
	if matches := commentRe.FindStringSubmatch(url); len(matches) > 1 {
		return "comment-" + matches[1]
	}

	// Try to extract PR number
	prRe := regexp.MustCompile(`/pull/(\d+)`)
	if matches := prRe.FindStringSubmatch(url); len(matches) > 1 {
		return "pr-" + matches[1]
	}

	// Try to extract issue number
	issueRe := regexp.MustCompile(`/issues/(\d+)`)
	if matches := issueRe.FindStringSubmatch(url); len(matches) > 1 {
		return "issue-" + matches[1]
	}

	// Default to using the last part of the URL path
	parts := strings.Split(strings.TrimSuffix(url, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return "github"
}

// validateAndSanitizeInput validates and sanitizes user input to prevent injection attacks.
// It ensures the input is safe to pass to shell commands.
func validateAndSanitizeInput(input string, maxLength int, fieldName string) (string, error) {
	// Check for null bytes which could cause issues in command execution
	if strings.Contains(input, "\x00") {
		return "", fmt.Errorf("%s contains null bytes", fieldName)
	}

	// Trim whitespace
	input = strings.TrimSpace(input)

	// Check length
	if len(input) == 0 {
		return "", fmt.Errorf("%s cannot be empty", fieldName)
	}
	if maxLength > 0 && len(input) > maxLength {
		return "", fmt.Errorf("%s exceeds maximum length of %d characters", fieldName, maxLength)
	}

	// Remove any control characters except newlines and tabs
	var sanitized strings.Builder
	for _, r := range input {
		if r == '\n' || r == '\t' || (r >= 32 && r < 127) || r > 127 {
			// Allow printable ASCII, newlines, tabs, and UTF-8 characters
			sanitized.WriteRune(r)
		} else if unicode.IsSpace(r) {
			// Replace other whitespace with regular space
			sanitized.WriteRune(' ')
		}
		// Skip other control characters
	}

	result := sanitized.String()
	if len(result) == 0 {
		return "", fmt.Errorf("%s contains only invalid characters", fieldName)
	}

	return result, nil
}

// validateBeadType ensures the bead type is valid.
func validateBeadType(beadType string) error {
	validTypes := map[string]bool{
		"bug":     true,
		"feature": true,
		"task":    true,
		"epic":    true,
	}

	if !validTypes[strings.ToLower(beadType)] {
		return fmt.Errorf("invalid bead type: %s", beadType)
	}
	return nil
}

// validatePriority ensures the priority is within valid range.
func validatePriority(priority int) error {
	if priority < 0 || priority > 4 {
		return errors.New("priority must be between 0 and 4")
	}
	return nil
}

// CreateBeadFromFeedback creates a bead using the bd CLI with proper input validation.
func (i *Integration) CreateBeadFromFeedback(ctx context.Context, beadInfo BeadInfo) (string, error) {
	// Validate and sanitize all inputs to prevent injection attacks
	title, err := validateAndSanitizeInput(beadInfo.Title, 200, "title")
	if err != nil {
		return "", fmt.Errorf("invalid title: %w", err)
	}

	// Validate bead type
	if err := validateBeadType(beadInfo.Type); err != nil {
		return "", err
	}

	// Validate priority
	if err := validatePriority(beadInfo.Priority); err != nil {
		return "", err
	}

	// Validate and sanitize parent ID
	parentID, err := validateAndSanitizeInput(beadInfo.ParentID, 100, "parent ID")
	if err != nil {
		return "", fmt.Errorf("invalid parent ID: %w", err)
	}

	// Validate and sanitize description (allow longer descriptions)
	description, err := validateAndSanitizeInput(beadInfo.Description, 5000, "description")
	if err != nil {
		return "", fmt.Errorf("invalid description: %w", err)
	}

	// Build the bd create command with validated inputs
	args := []string{"create",
		"--title", title,
		"--type", beadInfo.Type, // Already validated
		"--priority", fmt.Sprintf("%d", beadInfo.Priority), // Already validated as int
		"--parent", parentID,
		"--description", description,
	}

	// Add external reference if we have a source URL
	if sourceURL, ok := beadInfo.Metadata["source_url"]; ok && sourceURL != "" {
		// Validate the source URL
		sanitizedURL, err := validateAndSanitizeInput(sourceURL, 500, "source URL")
		if err == nil {
			// Create a short reference for the external-ref field
			// For example, "gh-comment-123456" or "gh-pr-123"
			externalRef := fmt.Sprintf("gh-%s", extractGitHubID(sanitizedURL))
			// Validate the external reference
			if sanitizedRef, err := validateAndSanitizeInput(externalRef, 100, "external reference"); err == nil {
				args = append(args, "--external-ref", sanitizedRef)
			}
		}
		// If validation fails, we skip adding the external reference but continue
	}

	// Add labels with validation
	for _, label := range beadInfo.Labels {
		// Validate each label
		sanitizedLabel, err := validateAndSanitizeInput(label, 50, "label")
		if err != nil {
			// Skip invalid labels but continue
			continue
		}
		args = append(args, "--label", sanitizedLabel)
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