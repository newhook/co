package github

// SourceType categorizes the origin of feedback.
type SourceType string

const (
	SourceTypeCI           SourceType = "ci"
	SourceTypeWorkflow     SourceType = "workflow"
	SourceTypeReviewComment SourceType = "review_comment"
	SourceTypeIssueComment SourceType = "issue_comment"
)

// SourceInfo provides structured metadata about the source of feedback.
// It replaces the fragile Source string and parts of the Context map.
type SourceInfo struct {
	Type SourceType // The type of source (CI, workflow, review, comment)
	ID   string     // Unique identifier (comment ID, check run ID, workflow run ID)
	Name string     // Human-readable name (check name, workflow name, reviewer username)
	URL  string     // Link to the specific item
}

// CICheckContext holds context specific to CI status check failures.
type CICheckContext struct {
	CheckName string // Name of the status check
	State     string // State of the check (e.g., FAILURE, ERROR)
}

// WorkflowContext holds context specific to workflow run failures.
type WorkflowContext struct {
	WorkflowName  string // Name of the workflow
	FailureDetail string // Description of what failed
	RunID         int64  // Workflow run ID
	JobName       string // Name of the failed job (if available)
	StepName      string // Name of the failed step (if available)
}

// ReviewContext holds context specific to review comments.
type ReviewContext struct {
	File        string // Path to the file being reviewed
	Line        int    // Line number in the file
	Reviewer    string // Username of the reviewer
	CommentID   int64  // ID of the review comment
	InReplyToID int64  // ID of parent comment if this is a reply (0 if not a reply)
}

// IssueCommentContext holds context specific to issue/PR comments.
type IssueCommentContext struct {
	Author    string // Username of the comment author
	CommentID int64  // ID of the comment
}
