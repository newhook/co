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
	Source      SourceInfo // Structured source information
	Priority    int        // 0-4 (0=critical, 4=backlog)
	Actionable  bool       // Whether this feedback requires action

	// Typed context fields (only one will be set based on Source.Type)
	CICheck      *CICheckContext      // Set when Source.Type == SourceTypeCI
	Workflow     *WorkflowContext     // Set when Source.Type == SourceTypeWorkflow
	Review       *ReviewContext       // Set when Source.Type == SourceTypeReviewComment
	IssueComment *IssueCommentContext // Set when Source.Type == SourceTypeIssueComment
}

// GetSourceID returns the source ID used for resolution tracking.
// Returns empty string if not applicable.
func (f FeedbackItem) GetSourceID() string {
	return f.Source.ID
}

// GetInReplyToID returns the ID of the parent comment if this is a reply.
// Returns empty string if this is not a reply.
func (f FeedbackItem) GetInReplyToID() string {
	if f.Review != nil && f.Review.InReplyToID != 0 {
		return fmt.Sprintf("%d", f.Review.InReplyToID)
	}
	return ""
}

// GetSourceName returns a human-readable source name (e.g., "CI: test-suite").
func (f FeedbackItem) GetSourceName() string {
	switch f.Source.Type {
	case SourceTypeCI:
		return fmt.Sprintf("CI: %s", f.Source.Name)
	case SourceTypeWorkflow:
		return fmt.Sprintf("Workflow: %s", f.Source.Name)
	case SourceTypeReviewComment:
		return fmt.Sprintf("Review: %s", f.Source.Name)
	case SourceTypeIssueComment:
		return fmt.Sprintf("Comment: %s", f.Source.Name)
	default:
		return f.Source.Name
	}
}

// GetSourceURL returns the URL to the source item.
func (f FeedbackItem) GetSourceURL() string {
	return f.Source.URL
}

// ToFeedbackContext creates a FeedbackContext from the item's typed context fields.
func (f FeedbackItem) ToFeedbackContext() *FeedbackContext {
	ctx := &FeedbackContext{
		CI:       f.CICheck,
		Workflow: f.Workflow,
		Review:   f.Review,
		Comment:  f.IssueComment,
	}
	// Return nil if no context is set
	if ctx.CI == nil && ctx.Workflow == nil && ctx.Review == nil && ctx.Comment == nil {
		return nil
	}
	return ctx
}

// ToContextMap converts typed context fields to a legacy map[string]string.
// This is provided for backwards compatibility with code that expects Context.
func (f FeedbackItem) ToContextMap() map[string]string {
	ctx := make(map[string]string)
	ctx["source_id"] = f.Source.ID

	switch {
	case f.CICheck != nil:
		ctx["check_name"] = f.CICheck.CheckName
		ctx["state"] = f.CICheck.State
	case f.Workflow != nil:
		ctx["workflow"] = f.Workflow.WorkflowName
		ctx["failure"] = f.Workflow.FailureDetail
		ctx["run_id"] = fmt.Sprintf("%d", f.Workflow.RunID)
		if f.Workflow.JobName != "" {
			ctx["job_name"] = f.Workflow.JobName
		}
		if f.Workflow.StepName != "" {
			ctx["step_name"] = f.Workflow.StepName
		}
	case f.Review != nil:
		if f.Review.File != "" {
			ctx["file"] = f.Review.File
		}
		if f.Review.Line != 0 {
			ctx["line"] = fmt.Sprintf("%d", f.Review.Line)
		}
		ctx["reviewer"] = f.Review.Reviewer
		ctx["comment_id"] = fmt.Sprintf("%d", f.Review.CommentID)
		if f.Review.InReplyToID != 0 {
			ctx["in_reply_to_id"] = fmt.Sprintf("%d", f.Review.InReplyToID)
		}
	case f.IssueComment != nil:
		ctx["author"] = f.IssueComment.Author
		ctx["comment_id"] = fmt.Sprintf("%d", f.IssueComment.CommentID)
	}

	return ctx
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
				Source: SourceInfo{
					Type: SourceTypeCI,
					ID:   check.Context, // Use check name as ID for status checks
					Name: check.Context,
					URL:  check.TargetURL,
				},
				Priority:   p.getPriorityForType(feedbackType),
				Actionable: true,
				CICheck: &CICheckContext{
					CheckName: check.Context,
					State:     check.State,
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

				// Parse job and step from detail (format: "jobName: stepName" or just "jobName")
				jobName, stepName := parseWorkflowDetail(detail)

				item := FeedbackItem{
					Type:        feedbackType,
					Title:       fmt.Sprintf("Fix %s in %s", detail, workflow.Name),
					Description: fmt.Sprintf("Workflow '%s' failed at: %s", workflow.Name, detail),
					Source: SourceInfo{
						Type: SourceTypeWorkflow,
						ID:   fmt.Sprintf("%d", workflow.ID),
						Name: workflow.Name,
						URL:  workflow.URL,
					},
					Priority:   p.getPriorityForType(feedbackType),
					Actionable: true,
					Workflow: &WorkflowContext{
						WorkflowName:  workflow.Name,
						FailureDetail: detail,
						RunID:         workflow.ID,
						JobName:       jobName,
						StepName:      stepName,
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

// parseWorkflowDetail extracts job and step names from failure detail.
// Format is either "jobName: stepName" or just "jobName".
func parseWorkflowDetail(detail string) (jobName, stepName string) {
	if idx := strings.Index(detail, ": "); idx != -1 {
		return detail[:idx], detail[idx+2:]
	}
	return detail, ""
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
				Source: SourceInfo{
					Type: SourceTypeReviewComment,
					ID:   fmt.Sprintf("%d", review.ID),
					Name: review.Author,
					URL:  status.URL, // Link to PR
				},
				Priority:   1, // High priority for requested changes
				Actionable: true,
				Review: &ReviewContext{
					Reviewer:  review.Author,
					CommentID: int64(review.ID),
				},
			}

			items = append(items, item)
		}

		// Process specific review comments - ALL review comments are considered actionable
		for _, comment := range review.Comments {
			// Skip only trivial comments like "LGTM", "looks good", etc.
			if p.isTrivialComment(comment.Body) {
				continue
			}

			// Create a unique URL for this review comment
			// GitHub review comments have a different URL structure than issue comments
			commentURL := fmt.Sprintf("%s#discussion_r%d", status.URL, comment.ID)

			// Use Line if available, otherwise fall back to OriginalLine
			lineNum := comment.Line
			if lineNum == 0 {
				lineNum = comment.OriginalLine
			}

			item := FeedbackItem{
				Type:        FeedbackTypeReview,
				Title:       fmt.Sprintf("Fix issue in %s (line %d)", comment.Path, lineNum),
				Description: p.truncateText(comment.Body, 300),
				Source: SourceInfo{
					Type: SourceTypeReviewComment,
					ID:   fmt.Sprintf("%d", comment.ID),
					Name: comment.Author,
					URL:  commentURL,
				},
				Priority:   2, // Medium priority for line comments
				Actionable: true,
				Review: &ReviewContext{
					File:        comment.Path,
					Line:        lineNum,
					Reviewer:    comment.Author,
					CommentID:   int64(comment.ID),
					InReplyToID: int64(comment.InReplyToID),
				},
			}

			items = append(items, item)
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
				// Create a unique URL for this issue comment
				commentURL := fmt.Sprintf("%s#issuecomment-%d", status.URL, comment.ID)

				item := FeedbackItem{
					Type:        feedbackType,
					Title:       p.extractTitleFromComment(comment.Body),
					Description: p.truncateText(comment.Body, 500),
					Source: SourceInfo{
						Type: SourceTypeIssueComment,
						ID:   fmt.Sprintf("%d", comment.ID),
						Name: comment.Author,
						URL:  commentURL,
					},
					Priority:   p.getPriorityForType(feedbackType),
					Actionable: true,
					IssueComment: &IssueCommentContext{
						Author:    comment.Author,
						CommentID: int64(comment.ID),
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

func (p *FeedbackProcessor) isTrivialComment(body string) bool {
	// Filter out only truly trivial comments
	trivialPatterns := []string{
		"lgtm",
		"looks good to me",
		"looks good",
		"nice",
		"great",
		"thanks",
		"thank you",
		"+1",
		"👍",
		"approved",
		"ship it",
	}

	trimmed := strings.TrimSpace(strings.ToLower(body))

	// Check if the entire comment is just a trivial phrase
	for _, pattern := range trivialPatterns {
		if trimmed == pattern || trimmed == pattern + "!" || trimmed == pattern + "." {
			return true
		}
	}

	// Very short comments (less than 10 chars) that don't contain actionable content
	if len(trimmed) < 10 && !strings.Contains(trimmed, "fix") && !strings.Contains(trimmed, "bug") {
		return true
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