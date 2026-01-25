package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/newhook/co/internal/beads"
)

// BeadInfo represents information for creating a bead.
type BeadInfo struct {
	Title       string
	Description string
	Type        string // task, bug, feature
	Priority    int    // 0-4
	ParentID    string // Parent issue ID (root_issue_id)
	Labels      []string
	Metadata    map[string]string
}

// Integration handles the integration between GitHub PR feedback and beads.
type Integration struct {
	client    *Client
	processor *FeedbackProcessor
}

// NewIntegration creates a new GitHub integration.
func NewIntegration(rules *FeedbackRules) *Integration {
	client := NewClient()
	processor := NewFeedbackProcessor(client, rules)

	return &Integration{
		client:    client,
		processor: processor,
	}
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

// CreateBeadFromFeedback creates a bead using the beads package with proper input validation.
func (i *Integration) CreateBeadFromFeedback(ctx context.Context, beadDir string, beadInfo BeadInfo) (string, error) {
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

	// Prepare external reference if we have a source URL
	var externalRef string
	if sourceURL, ok := beadInfo.Metadata["source_url"]; ok && sourceURL != "" {
		// Validate the source URL
		sanitizedURL, err := validateAndSanitizeInput(sourceURL, 500, "source URL")
		if err == nil {
			// Create a short reference for the external-ref field
			// For example, "gh-comment-123456" or "gh-pr-123"
			ref := fmt.Sprintf("gh-%s", extractGitHubID(sanitizedURL))
			// Validate the external reference
			if sanitizedRef, err := validateAndSanitizeInput(ref, 100, "external reference"); err == nil {
				externalRef = sanitizedRef
			}
		}
		// If validation fails, we skip adding the external reference but continue
	}

	// Validate labels
	var validLabels []string
	for _, label := range beadInfo.Labels {
		// Validate each label
		sanitizedLabel, err := validateAndSanitizeInput(label, 50, "label")
		if err != nil {
			// Skip invalid labels but continue
			continue
		}
		validLabels = append(validLabels, sanitizedLabel)
	}

	// Create bead using the beads package
	createOpts := beads.CreateOptions{
		Title:       title,
		Type:        beadInfo.Type,    // Already validated
		Priority:    beadInfo.Priority, // Already validated as int
		Parent:      parentID,
		Description: description,
		Labels:      validLabels,
		ExternalRef: externalRef,
	}

	beadID, err := beads.Create(ctx, beadDir, createOpts)
	if err != nil {
		return "", fmt.Errorf("failed to create bead: %w", err)
	}

	return beadID, nil
}

// FetchAndStoreFeedback fetches PR feedback and stores it in the database.
// This is called by the orchestrator to populate the pr_feedback table.
func (i *Integration) FetchAndStoreFeedback(ctx context.Context, prURL string) ([]FeedbackItem, error) {
	return i.processor.ProcessPRFeedback(ctx, prURL)
}

// PRStatusInfo represents the extracted PR status information.
type PRStatusInfo struct {
	CIStatus       string   // pending, success, failure
	ApprovalStatus string   // pending, approved, changes_requested
	Approvers      []string // List of usernames who approved
	PRState        string   // open, closed, merged
}

// ExtractPRStatus fetches a PR and extracts CI and approval status.
func (i *Integration) ExtractPRStatus(ctx context.Context, prURL string) (*PRStatusInfo, error) {
	status, err := i.client.GetPRStatus(ctx, prURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR status: %w", err)
	}

	return ExtractStatusFromPRStatus(status), nil
}

// ExtractStatusFromPRStatus extracts CI and approval status from a PRStatus object.
func ExtractStatusFromPRStatus(status *PRStatus) *PRStatusInfo {
	info := &PRStatusInfo{
		CIStatus:       "pending",
		ApprovalStatus: "pending",
		Approvers:      []string{},
		PRState:        normalizePRState(status.State),
	}

	// Extract CI status from status checks and workflow runs
	info.CIStatus = extractCIStatus(status)

	// Extract approval status and approvers from reviews
	info.ApprovalStatus, info.Approvers = extractApprovalStatus(status)

	return info
}

// normalizePRState converts GitHub PR state to our normalized state.
// GitHub uses: OPEN, CLOSED, MERGED (uppercase)
// We use: open, closed, merged (lowercase)
func normalizePRState(state string) string {
	switch strings.ToUpper(state) {
	case "OPEN":
		return "open"
	case "CLOSED":
		return "closed"
	case "MERGED":
		return "merged"
	default:
		return "open" // Default to open if unknown
	}
}

// extractCIStatus determines the overall CI status from status checks and workflows.
// Returns: "pending", "success", or "failure"
func extractCIStatus(status *PRStatus) string {
	// Check workflow runs first (GitHub Actions)
	hasWorkflows := len(status.Workflows) > 0
	hasStatusChecks := len(status.StatusChecks) > 0

	if !hasWorkflows && !hasStatusChecks {
		// No CI configured
		return "pending"
	}

	// Check for any failures
	for _, workflow := range status.Workflows {
		if workflow.Conclusion == "failure" {
			return "failure"
		}
	}
	for _, check := range status.StatusChecks {
		if check.State == "FAILURE" || check.State == "ERROR" {
			return "failure"
		}
	}

	// Check for any pending
	for _, workflow := range status.Workflows {
		if workflow.Status == "in_progress" || workflow.Status == "queued" ||
			(workflow.Status == "completed" && workflow.Conclusion == "") {
			return "pending"
		}
	}
	for _, check := range status.StatusChecks {
		if check.State == "PENDING" || check.State == "" {
			return "pending"
		}
	}

	// If we have at least some completed checks/workflows and no failures or pending
	return "success"
}

// extractApprovalStatus determines the approval status from reviews.
// Returns: (status, approvers) where status is "pending", "approved", or "changes_requested"
func extractApprovalStatus(status *PRStatus) (string, []string) {
	if len(status.Reviews) == 0 {
		return "pending", []string{}
	}

	// Track the latest review state per user
	// Later reviews override earlier ones for the same user
	latestStateByUser := make(map[string]string)
	latestTimeByUser := make(map[string]time.Time)

	for _, review := range status.Reviews {
		// Skip COMMENTED reviews - they don't affect approval status
		if review.State == "COMMENTED" {
			continue
		}

		// Only update if this review is newer than the previous one from this user
		if prevTime, exists := latestTimeByUser[review.Author]; !exists || review.CreatedAt.After(prevTime) {
			latestStateByUser[review.Author] = review.State
			latestTimeByUser[review.Author] = review.CreatedAt
		}
	}

	// Collect approvers and check for changes requested
	var approvers []string
	hasChangesRequested := false

	for user, state := range latestStateByUser {
		switch state {
		case "APPROVED":
			approvers = append(approvers, user)
		case "CHANGES_REQUESTED":
			hasChangesRequested = true
		}
	}

	// Determine overall status
	// If any reviewer has requested changes, the status is "changes_requested"
	// If at least one reviewer has approved (and no changes requested), status is "approved"
	// Otherwise, status is "pending"
	if hasChangesRequested {
		return "changes_requested", approvers
	}
	if len(approvers) > 0 {
		return "approved", approvers
	}
	return "pending", []string{}
}

// ApproversToJSON converts a list of approvers to a JSON string.
func ApproversToJSON(approvers []string) string {
	if len(approvers) == 0 {
		return "[]"
	}
	data, err := json.Marshal(approvers)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// ApproversFromJSON parses a JSON string into a list of approvers.
func ApproversFromJSON(jsonStr string) []string {
	if jsonStr == "" || jsonStr == "[]" {
		return []string{}
	}
	var approvers []string
	if err := json.Unmarshal([]byte(jsonStr), &approvers); err != nil {
		return []string{}
	}
	return approvers
}