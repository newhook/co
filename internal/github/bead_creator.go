package github

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
)

// BeadCreator handles creation of beads from PR feedback.
type BeadCreator struct {
	processor *FeedbackProcessor
}

// NewBeadCreator creates a new bead creator.
func NewBeadCreator(processor *FeedbackProcessor) *BeadCreator {
	return &BeadCreator{
		processor: processor,
	}
}

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

// ProcessPRAndCreateBeadInfo processes PR feedback and returns bead creation info.
func (bc *BeadCreator) ProcessPRAndCreateBeadInfo(ctx context.Context, prURL string, rootIssueID string) ([]BeadInfo, error) {
	// Process feedback
	feedbackItems, err := bc.processor.ProcessPRFeedback(ctx, prURL)
	if err != nil {
		return nil, fmt.Errorf("failed to process PR feedback: %w", err)
	}

	// Convert feedback items to bead info
	var beads []BeadInfo
	for _, item := range feedbackItems {
		beadInfo := bc.feedbackToBeadInfo(item, rootIssueID)
		beads = append(beads, beadInfo)
	}

	// Deduplicate similar beads
	beads = bc.deduplicateBeads(beads)

	return beads, nil
}

// feedbackToBeadInfo converts a feedback item to bead creation info.
func (bc *BeadCreator) feedbackToBeadInfo(item FeedbackItem, rootIssueID string) BeadInfo {
	beadType := bc.getBeadType(item.Type)
	labels := bc.getLabels(item.Type)

	// Add feedback type to labels
	labels = append(labels, "from-pr-feedback")

	// Build metadata from typed context
	metadata := make(map[string]string)
	bc.addContextToMetadata(item, metadata)

	// Add source info to metadata
	metadata["source_type"] = string(item.Source.Type)
	metadata["source_id"] = item.Source.ID
	metadata["source_name"] = item.Source.Name
	metadata["source_url"] = item.Source.URL
	metadata["feedback_type"] = string(item.Type)

	return BeadInfo{
		Title:       item.Title,
		Description: bc.formatDescription(item),
		Type:        beadType,
		Priority:    item.Priority,
		ParentID:    rootIssueID,
		Labels:      labels,
		Metadata:    metadata,
	}
}

// addContextToMetadata extracts typed context fields into metadata map.
// It merges the result of item.ToContextMap() into the provided metadata map.
func (bc *BeadCreator) addContextToMetadata(item FeedbackItem, metadata map[string]string) {
	maps.Copy(metadata, item.ToContextMap())
}

// getBeadType determines the bead type from feedback type.
func (bc *BeadCreator) getBeadType(feedbackType FeedbackType) string {
	switch feedbackType {
	case FeedbackTypeTest, FeedbackTypeBuild, FeedbackTypeCI:
		return "bug"
	case FeedbackTypeLint, FeedbackTypeSecurity:
		return "task"
	case FeedbackTypeReview:
		return "task"
	default:
		return "task"
	}
}

// getLabels returns appropriate labels for the feedback type.
func (bc *BeadCreator) getLabels(feedbackType FeedbackType) []string {
	labels := []string{}

	switch feedbackType {
	case FeedbackTypeCI:
		labels = append(labels, "ci-failure")
	case FeedbackTypeTest:
		labels = append(labels, "test-failure")
	case FeedbackTypeLint:
		labels = append(labels, "lint-issue")
	case FeedbackTypeBuild:
		labels = append(labels, "build-failure")
	case FeedbackTypeReview:
		labels = append(labels, "review-feedback")
	case FeedbackTypeSecurity:
		labels = append(labels, "security")
	}

	return labels
}

// formatDescription creates a detailed description for the bead.
func (bc *BeadCreator) formatDescription(item FeedbackItem) string {
	var sb strings.Builder

	// Add main description
	sb.WriteString(item.Description)
	sb.WriteString("\n\n")

	// Add source information
	sb.WriteString("## Source\n")
	sb.WriteString(fmt.Sprintf("- Type: %s\n", item.Type))
	sb.WriteString(fmt.Sprintf("- From: %s (%s)\n", item.Source.Name, item.Source.Type))
	if item.Source.URL != "" {
		sb.WriteString(fmt.Sprintf("- **GitHub Link**: %s\n", item.Source.URL))
	}
	sb.WriteString("\n_This issue was automatically created from GitHub PR feedback._")

	// Add context if available
	bc.writeContextToDescription(&sb, item)

	// Add resolution guidance based on type
	sb.WriteString("\n## Resolution\n")
	switch item.Type {
	case FeedbackTypeTest:
		sb.WriteString("Fix the failing tests and ensure all test suites pass.\n")
	case FeedbackTypeBuild:
		sb.WriteString("Resolve the build errors and ensure the project compiles successfully.\n")
	case FeedbackTypeLint:
		sb.WriteString("Fix the linting issues to meet code style requirements.\n")
	case FeedbackTypeSecurity:
		sb.WriteString("Address the security vulnerability with appropriate fixes.\n")
	case FeedbackTypeReview:
		sb.WriteString("Address the reviewer's feedback and update the code accordingly.\n")
	case FeedbackTypeCI:
		sb.WriteString("Fix the CI pipeline failure and ensure all checks pass.\n")
	default:
		sb.WriteString("Address the issue as described above.\n")
	}

	return sb.String()
}

// writeContextToDescription writes typed context fields to the description.
func (bc *BeadCreator) writeContextToDescription(sb *strings.Builder, item FeedbackItem) {
	hasContext := item.CICheck != nil || item.Workflow != nil || item.Review != nil || item.IssueComment != nil
	if !hasContext {
		return
	}

	sb.WriteString("\n## Context\n")

	switch {
	case item.CICheck != nil:
		sb.WriteString(fmt.Sprintf("- Check Name: %s\n", item.CICheck.CheckName))
		sb.WriteString(fmt.Sprintf("- State: %s\n", item.CICheck.State))
	case item.Workflow != nil:
		sb.WriteString(fmt.Sprintf("- Workflow: %s\n", item.Workflow.WorkflowName))
		sb.WriteString(fmt.Sprintf("- Failure: %s\n", item.Workflow.FailureDetail))
		sb.WriteString(fmt.Sprintf("- Run Id: %d\n", item.Workflow.RunID))
		if item.Workflow.JobName != "" {
			sb.WriteString(fmt.Sprintf("- Job Name: %s\n", item.Workflow.JobName))
		}
		if item.Workflow.StepName != "" {
			sb.WriteString(fmt.Sprintf("- Step Name: %s\n", item.Workflow.StepName))
		}
	case item.Review != nil:
		if item.Review.File != "" {
			sb.WriteString(fmt.Sprintf("- File: %s\n", item.Review.File))
		}
		if item.Review.Line != 0 {
			sb.WriteString(fmt.Sprintf("- Line: %d\n", item.Review.Line))
		}
		sb.WriteString(fmt.Sprintf("- Reviewer: %s\n", item.Review.Reviewer))
		sb.WriteString(fmt.Sprintf("- Comment Id: %d\n", item.Review.CommentID))
		if item.Review.InReplyToID != 0 {
			sb.WriteString(fmt.Sprintf("- In Reply To Id: %d\n", item.Review.InReplyToID))
		}
	case item.IssueComment != nil:
		sb.WriteString(fmt.Sprintf("- Author: %s\n", item.IssueComment.Author))
		sb.WriteString(fmt.Sprintf("- Comment Id: %d\n", item.IssueComment.CommentID))
	}
}

// deduplicateBeads removes duplicate or similar beads.
func (bc *BeadCreator) deduplicateBeads(beads []BeadInfo) []BeadInfo {
	seen := make(map[string]bool)
	var unique []BeadInfo

	for _, bead := range beads {
		// Create a key based on type and significant parts of title
		key := bc.createDeduplicationKey(bead)

		if !seen[key] {
			seen[key] = true
			unique = append(unique, bead)
		} else {
			// If we've seen a similar bead, check if this one has higher priority
			for i, existing := range unique {
				if bc.createDeduplicationKey(existing) == key && bead.Priority < existing.Priority {
					// Replace with higher priority version
					unique[i] = bead
					break
				}
			}
		}
	}

	return unique
}

// createDeduplicationKey creates a key for deduplication.
func (bc *BeadCreator) createDeduplicationKey(bead BeadInfo) string {
	// Normalize title for comparison
	normalizedTitle := strings.ToLower(bead.Title)

	// Remove common prefixes
	prefixes := []string{"fix ", "address ", "resolve ", "handle "}
	for _, prefix := range prefixes {
		normalizedTitle = strings.TrimPrefix(normalizedTitle, prefix)
	}

	// For file-specific issues, include the file in the key
	if file, ok := bead.Metadata["file"]; ok {
		return fmt.Sprintf("%s:%s:%s", bead.Type, file, normalizedTitle)
	}

	// For workflow/CI issues, include the workflow/check name
	if workflow, ok := bead.Metadata["workflow"]; ok {
		return fmt.Sprintf("%s:workflow:%s", bead.Type, workflow)
	}
	if checkName, ok := bead.Metadata["check_name"]; ok {
		return fmt.Sprintf("%s:check:%s", bead.Type, checkName)
	}

	// Default key
	return fmt.Sprintf("%s:%s", bead.Type, normalizedTitle)
}

// GroupBeadsByType groups beads by their type for batch processing.
func (bc *BeadCreator) GroupBeadsByType(beads []BeadInfo) map[string][]BeadInfo {
	grouped := make(map[string][]BeadInfo)

	for _, bead := range beads {
		grouped[bead.Type] = append(grouped[bead.Type], bead)
	}

	return grouped
}

// PrioritizeBeads sorts beads by priority (0 = highest).
func (bc *BeadCreator) PrioritizeBeads(beads []BeadInfo) []BeadInfo {
	sorted := make([]BeadInfo, len(beads))
	copy(sorted, beads)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	return sorted
}