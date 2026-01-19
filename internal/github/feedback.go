package github

import (
	"context"
	"fmt"
	"strings"
)

// FeedbackType categorizes the type of feedback.
type FeedbackType string

const (
	FeedbackTypeCI       FeedbackType = "ci_failure"
	FeedbackTypeTest     FeedbackType = "test_failure"
	FeedbackTypeLint     FeedbackType = "lint_error"
	FeedbackTypeBuild    FeedbackType = "build_error"
	FeedbackTypeReview   FeedbackType = "review_comment"
	FeedbackTypeSecurity FeedbackType = "security_issue"
	FeedbackTypeGeneral  FeedbackType = "general"
)

// FeedbackItem represents a piece of feedback that could become a bead.
type FeedbackItem struct {
	Type        FeedbackType
	Title       string
	Description string
	Source      string // e.g., "CI: test-suite", "Review: johndoe"
	SourceURL   string // Link to the specific failure/comment
	Priority    int    // 0-4 (0=critical, 4=backlog)
	Actionable  bool   // Whether this feedback requires action
	Context     map[string]string // Additional context (file, line, etc.)
}

// FeedbackProcessor processes PR feedback and generates actionable items.
type FeedbackProcessor struct {
	client *Client
	rules  *FeedbackRules
}

// FeedbackRules defines configurable rules for feedback processing.
type FeedbackRules struct {
	// CreateBeadForFailedChecks creates beads for failed status checks
	CreateBeadForFailedChecks bool
	// CreateBeadForTestFailures creates beads for test failures
	CreateBeadForTestFailures bool
	// CreateBeadForLintErrors creates beads for lint errors
	CreateBeadForLintErrors bool
	// CreateBeadForReviewComments creates beads for review comments requesting changes
	CreateBeadForReviewComments bool
	// IgnoreDraftPRs skips processing for draft PRs
	IgnoreDraftPRs bool
	// MinimumPriority sets the minimum priority for created beads (0-4)
	MinimumPriority int
}

// DefaultFeedbackRules returns default feedback processing rules.
func DefaultFeedbackRules() *FeedbackRules {
	return &FeedbackRules{
		CreateBeadForFailedChecks:    true,
		CreateBeadForTestFailures:    true,
		CreateBeadForLintErrors:      true,
		CreateBeadForReviewComments:  true,
		IgnoreDraftPRs:               true,
		MinimumPriority:              2, // Default to medium priority
	}
}

// NewFeedbackProcessor creates a new feedback processor.
func NewFeedbackProcessor(client *Client, rules *FeedbackRules) *FeedbackProcessor {
	if rules == nil {
		rules = DefaultFeedbackRules()
	}
	return &FeedbackProcessor{
		client: client,
		rules:  rules,
	}
}

// ProcessPRFeedback fetches and processes feedback for a PR.
func (p *FeedbackProcessor) ProcessPRFeedback(ctx context.Context, prURL string) ([]FeedbackItem, error) {
	// Fetch PR status
	status, err := p.client.GetPRStatus(ctx, prURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR status: %w", err)
	}

	// Skip draft PRs if configured
	if p.rules.IgnoreDraftPRs && strings.EqualFold(status.State, "draft") {
		return nil, nil
	}

	var items []FeedbackItem

	// Process status checks
	if p.rules.CreateBeadForFailedChecks {
		checkItems := p.processStatusChecks(status)
		items = append(items, checkItems...)
	}

	// Process workflow runs
	workflowItems := p.processWorkflowRuns(status)
	items = append(items, workflowItems...)

	// Process reviews
	if p.rules.CreateBeadForReviewComments {
		reviewItems := p.processReviews(status)
		items = append(items, reviewItems...)
	}

	// Process general comments
	commentItems := p.processComments(status)
	items = append(items, commentItems...)

	// Filter by minimum priority
	filtered := make([]FeedbackItem, 0, len(items))
	for _, item := range items {
		if item.Priority <= p.rules.MinimumPriority && item.Actionable {
			filtered = append(filtered, item)
		}
	}

	return filtered, nil
}

// processStatusChecks processes status check failures.
func (p *FeedbackProcessor) processStatusChecks(status *PRStatus) []FeedbackItem {
	var items []FeedbackItem

	for _, check := range status.StatusChecks {
		if check.State == "FAILURE" || check.State == "ERROR" {
			feedbackType := p.categorizeCheckFailure(check.Context)

			item := FeedbackItem{
				Type:        feedbackType,
				Title:       fmt.Sprintf("Fix %s failure", check.Context),
				Description: check.Description,
				Source:      fmt.Sprintf("CI: %s", check.Context),
				SourceURL:   check.TargetURL,
				Priority:    p.getPriorityForType(feedbackType),
				Actionable:  true,
				Context: map[string]string{
					"check_name": check.Context,
					"state":      check.State,
				},
			}

			items = append(items, item)
		}
	}

	return items
}

// processWorkflowRuns processes workflow run failures.
func (p *FeedbackProcessor) processWorkflowRuns(status *PRStatus) []FeedbackItem {
	var items []FeedbackItem

	for _, workflow := range status.Workflows {
		if workflow.Conclusion == "failure" {
			// Find the specific failed jobs/steps
			failureDetails := p.extractWorkflowFailures(workflow)

			for _, detail := range failureDetails {
				feedbackType := p.categorizeWorkflowFailure(workflow.Name, detail)

				item := FeedbackItem{
					Type:        feedbackType,
					Title:       fmt.Sprintf("Fix %s in %s", detail, workflow.Name),
					Description: fmt.Sprintf("Workflow '%s' failed at: %s", workflow.Name, detail),
					Source:      fmt.Sprintf("Workflow: %s", workflow.Name),
					SourceURL:   workflow.URL,
					Priority:    p.getPriorityForType(feedbackType),
					Actionable:  true,
					Context: map[string]string{
						"workflow":   workflow.Name,
						"failure":    detail,
						"run_id":     fmt.Sprintf("%d", workflow.ID),
					},
				}

				// Only create beads for relevant failure types
				if p.shouldCreateBeadForWorkflow(feedbackType) {
					items = append(items, item)
				}
			}
		}
	}

	return items
}

// processReviews processes review comments.
func (p *FeedbackProcessor) processReviews(status *PRStatus) []FeedbackItem {
	var items []FeedbackItem

	for _, review := range status.Reviews {
		// Process reviews requesting changes
		if review.State == "CHANGES_REQUESTED" {
			item := FeedbackItem{
				Type:        FeedbackTypeReview,
				Title:       fmt.Sprintf("Address review feedback from %s", review.Author),
				Description: p.truncateText(review.Body, 500),
				Source:      fmt.Sprintf("Review: %s", review.Author),
				SourceURL:   status.URL, // Link to PR
				Priority:    1, // High priority for requested changes
				Actionable:  true,
				Context: map[string]string{
					"reviewer":   review.Author,
					"review_id":  fmt.Sprintf("%d", review.ID),
					"source_id":  fmt.Sprintf("%d", review.ID), // Store review ID as source_id for resolution tracking
				},
			}

			items = append(items, item)
		}

		// Process specific review comments
		for _, comment := range review.Comments {
			if p.isActionableComment(comment.Body) {
				item := FeedbackItem{
					Type:        FeedbackTypeReview,
					Title:       fmt.Sprintf("Fix issue in %s (line %d)", p.getFileName(comment.Path), comment.Line),
					Description: p.truncateText(comment.Body, 300),
					Source:      fmt.Sprintf("Review: %s", comment.Author),
					SourceURL:   status.URL,
					Priority:    2, // Medium priority for line comments
					Actionable:  true,
					Context: map[string]string{
						"file":       comment.Path,
						"line":       fmt.Sprintf("%d", comment.Line),
						"reviewer":   comment.Author,
						"comment_id": fmt.Sprintf("%d", comment.ID),
						"source_id":  fmt.Sprintf("%d", comment.ID), // Store comment ID as source_id for resolution tracking
					},
				}

				items = append(items, item)
			}
		}
	}

	return items
}

// processComments processes general PR comments.
func (p *FeedbackProcessor) processComments(status *PRStatus) []FeedbackItem {
	var items []FeedbackItem

	for _, comment := range status.Comments {
		if p.isActionableComment(comment.Body) {
			// Check if this is a bot comment with specific patterns
			feedbackType := p.categorizeComment(comment)

			if feedbackType != FeedbackTypeGeneral {
				item := FeedbackItem{
					Type:        feedbackType,
					Title:       p.extractTitleFromComment(comment.Body),
					Description: p.truncateText(comment.Body, 500),
					Source:      fmt.Sprintf("Comment: %s", comment.Author),
					SourceURL:   status.URL,
					Priority:    p.getPriorityForType(feedbackType),
					Actionable:  true,
					Context: map[string]string{
						"author":     comment.Author,
						"comment_id": fmt.Sprintf("%d", comment.ID),
						"source_id":  fmt.Sprintf("%d", comment.ID), // Store comment ID as source_id for resolution tracking
					},
				}

				items = append(items, item)
			}
		}
	}

	return items
}

// Helper functions

func (p *FeedbackProcessor) categorizeCheckFailure(checkName string) FeedbackType {
	lower := strings.ToLower(checkName)

	if strings.Contains(lower, "test") {
		return FeedbackTypeTest
	} else if strings.Contains(lower, "lint") || strings.Contains(lower, "style") {
		return FeedbackTypeLint
	} else if strings.Contains(lower, "build") || strings.Contains(lower, "compile") {
		return FeedbackTypeBuild
	} else if strings.Contains(lower, "security") || strings.Contains(lower, "vulnerability") {
		return FeedbackTypeSecurity
	}

	return FeedbackTypeCI
}

func (p *FeedbackProcessor) categorizeWorkflowFailure(workflowName, failureDetail string) FeedbackType {
	lower := strings.ToLower(workflowName + " " + failureDetail)

	if strings.Contains(lower, "test") {
		return FeedbackTypeTest
	} else if strings.Contains(lower, "lint") || strings.Contains(lower, "format") {
		return FeedbackTypeLint
	} else if strings.Contains(lower, "build") || strings.Contains(lower, "compile") {
		return FeedbackTypeBuild
	} else if strings.Contains(lower, "security") || strings.Contains(lower, "scan") {
		return FeedbackTypeSecurity
	}

	return FeedbackTypeCI
}

func (p *FeedbackProcessor) extractWorkflowFailures(workflow WorkflowRun) []string {
	var failures []string

	for _, job := range workflow.Jobs {
		if job.Conclusion == "failure" {
			// Try to find the specific failed step
			failedStep := ""
			for _, step := range job.Steps {
				if step.Conclusion == "failure" {
					failedStep = step.Name
					break
				}
			}

			if failedStep != "" {
				failures = append(failures, fmt.Sprintf("%s: %s", job.Name, failedStep))
			} else {
				failures = append(failures, job.Name)
			}
		}
	}

	return failures
}

func (p *FeedbackProcessor) shouldCreateBeadForWorkflow(feedbackType FeedbackType) bool {
	switch feedbackType {
	case FeedbackTypeTest:
		return p.rules.CreateBeadForTestFailures
	case FeedbackTypeLint:
		return p.rules.CreateBeadForLintErrors
	case FeedbackTypeBuild, FeedbackTypeCI:
		return p.rules.CreateBeadForFailedChecks
	default:
		return true
	}
}

func (p *FeedbackProcessor) isActionableComment(body string) bool {
	// Check for patterns that indicate actionable feedback
	actionablePatterns := []string{
		"please",
		"should",
		"must",
		"need to",
		"needs to",
		"fix",
		"change",
		"update",
		"add",
		"remove",
		"todo",
		"fixme",
		"bug",
		"error",
		"warning",
		"failed",
		"failure",
		"detected",
		"vulnerability",
		"risk",
	}

	lower := strings.ToLower(body)
	for _, pattern := range actionablePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

func (p *FeedbackProcessor) categorizeComment(comment Comment) FeedbackType {
	lower := strings.ToLower(comment.Body)

	// Check for bot comments with specific patterns
	if strings.Contains(comment.Author, "bot") || strings.Contains(comment.Author, "[bot]") {
		if strings.Contains(lower, "security") || strings.Contains(lower, "vulnerability") {
			return FeedbackTypeSecurity
		} else if strings.Contains(lower, "test") && strings.Contains(lower, "fail") {
			return FeedbackTypeTest
		} else if strings.Contains(lower, "lint") || strings.Contains(lower, "style") {
			return FeedbackTypeLint
		}
	}

	return FeedbackTypeGeneral
}

func (p *FeedbackProcessor) extractTitleFromComment(body string) string {
	// Try to extract a meaningful title from the comment
	lines := strings.Split(body, "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		if firstLine != "" {
			if len(firstLine) > 100 {
				return firstLine[:100] + "..."
			}
			return firstLine
		}
	}
	return "Address comment feedback"
}

func (p *FeedbackProcessor) getPriorityForType(feedbackType FeedbackType) int {
	switch feedbackType {
	case FeedbackTypeSecurity:
		return 0 // Critical
	case FeedbackTypeBuild, FeedbackTypeCI:
		return 1 // High
	case FeedbackTypeTest:
		return 2 // Medium
	case FeedbackTypeLint, FeedbackTypeReview:
		return 2 // Medium
	default:
		return 3 // Low
	}
}

func (p *FeedbackProcessor) truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

func (p *FeedbackProcessor) getFileName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}