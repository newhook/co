package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/newhook/co/internal/logging"
)

// Client wraps the gh CLI for GitHub API operations.
type Client struct{}

// NewClient creates a new GitHub client.
func NewClient() *Client {
	return &Client{}
}

// PRStatus represents the status of a PR.
type PRStatus struct {
	URL           string         `json:"url"`
	State         string         `json:"state"`
	Mergeable     bool           `json:"mergeable"`
	MergeableState string        `json:"mergeableState"`
	StatusChecks  []StatusCheck  `json:"statusCheckRollup"`
	Comments      []Comment      `json:"comments"`
	Reviews       []Review       `json:"reviews"`
	Workflows     []WorkflowRun  `json:"workflows"`
}

// StatusCheck represents a PR status check.
type StatusCheck struct {
	Context     string    `json:"context"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	TargetURL   string    `json:"targetUrl"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Comment represents a PR comment.
type Comment struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Review represents a PR review.
type Review struct {
	ID        int       `json:"id"`
	State     string    `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	Comments  []ReviewComment `json:"comments"`
}

// ReviewComment represents a comment on a specific line in a PR.
type ReviewComment struct {
	ID        int       `json:"id"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
}

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`     // completed, in_progress, queued
	Conclusion string    `json:"conclusion"` // success, failure, cancelled, skipped
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Jobs       []Job     `json:"jobs"`
}

// Job represents a job within a workflow run.
type Job struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	Steps      []Step    `json:"steps"`
	URL        string    `json:"url"`
}

// Step represents a step within a job.
type Step struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Number     int    `json:"number"`
}

// GetPRStatus fetches comprehensive PR status information.
func (c *Client) GetPRStatus(ctx context.Context, prURL string) (*PRStatus, error) {
	logging.Info("fetching PR status", "prURL", prURL)

	// Extract PR number from URL
	prNumber, repo, err := parsePRURL(prURL)
	if err != nil {
		logging.Error("invalid PR URL", "error", err, "prURL", prURL)
		return nil, fmt.Errorf("invalid PR URL: %w", err)
	}

	logging.Debug("parsed PR URL", "prNumber", prNumber, "repo", repo)

	status := &PRStatus{
		URL: prURL,
	}

	// Fetch basic PR info
	if err := c.fetchPRInfo(ctx, repo, prNumber, status); err != nil {
		logging.Error("failed to fetch PR info", "error", err)
		return nil, fmt.Errorf("failed to fetch PR info: %w", err)
	}

	// Fetch status checks
	if err := c.fetchStatusChecks(ctx, repo, prNumber, status); err != nil {
		logging.Error("failed to fetch status checks", "error", err)
		return nil, fmt.Errorf("failed to fetch status checks: %w", err)
	}

	// Fetch comments
	if err := c.fetchComments(ctx, repo, prNumber, status); err != nil {
		logging.Error("failed to fetch comments", "error", err)
		return nil, fmt.Errorf("failed to fetch comments: %w", err)
	}

	// Fetch reviews
	if err := c.fetchReviews(ctx, repo, prNumber, status); err != nil {
		logging.Error("failed to fetch reviews", "error", err)
		return nil, fmt.Errorf("failed to fetch reviews: %w", err)
	}

	// Fetch workflow runs
	if err := c.fetchWorkflowRuns(ctx, repo, prNumber, status); err != nil {
		logging.Error("failed to fetch workflow runs", "error", err)
		return nil, fmt.Errorf("failed to fetch workflow runs: %w", err)
	}

	logging.Info("successfully fetched PR status",
		"prURL", prURL,
		"state", status.State,
		"mergeable", status.Mergeable,
		"numChecks", len(status.StatusChecks),
		"numComments", len(status.Comments),
		"numReviews", len(status.Reviews),
		"numWorkflows", len(status.Workflows))

	return status, nil
}

// fetchPRInfo fetches basic PR information.
func (c *Client) fetchPRInfo(ctx context.Context, repo, prNumber string, status *PRStatus) error {
	logging.Debug("fetching PR info", "repo", repo, "prNumber", prNumber)

	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber,
		"--repo", repo,
		"--json", "state,mergeable,mergeStateStatus")

	output, err := cmd.Output()
	if err != nil {
		logging.Error("gh pr view failed", "error", err, "repo", repo, "prNumber", prNumber)
		return fmt.Errorf("gh pr view failed: %w", err)
	}

	// Log the raw output for debugging
	logging.Debug("gh pr view response", "output", string(output))

	var prInfo struct {
		State          string `json:"state"`
		Mergeable      string `json:"mergeable"`     // Changed from bool to string
		MergeStateStatus string `json:"mergeStateStatus"`
	}

	if err := json.Unmarshal(output, &prInfo); err != nil {
		logging.Error("failed to parse PR info", "error", err, "output", string(output))
		return fmt.Errorf("failed to parse PR info: %w", err)
	}

	status.State = prInfo.State
	// Convert string mergeable to bool
	status.Mergeable = prInfo.Mergeable == "MERGEABLE"
	status.MergeableState = prInfo.MergeStateStatus

	logging.Debug("parsed PR info",
		"state", status.State,
		"mergeable", status.Mergeable,
		"mergeableState", status.MergeableState)

	return nil
}

// fetchStatusChecks fetches PR status checks.
func (c *Client) fetchStatusChecks(ctx context.Context, repo, prNumber string, status *PRStatus) error {
	logging.Debug("fetching status checks", "repo", repo, "prNumber", prNumber)

	cmd := exec.CommandContext(ctx, "gh", "pr", "checks", prNumber,
		"--repo", repo,
		"--json", "name,state,description,link,startedAt")

	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		// Status checks might not exist
		if strings.Contains(outputStr, "no checks reported") || strings.Contains(outputStr, "no checks") {
			logging.Debug("no status checks found", "output", outputStr)
			return nil
		}
		logging.Error("gh pr checks failed", "error", err, "output", outputStr, "repo", repo, "prNumber", prNumber)
		return fmt.Errorf("gh pr checks failed: %w", err)
	}

	logging.Debug("gh pr checks response", "output", string(output))

	var checks []struct {
		Name        string    `json:"name"`
		State       string    `json:"state"`
		Description string    `json:"description"`
		Link        string    `json:"link"`
		StartedAt   time.Time `json:"startedAt"`
	}

	if err := json.Unmarshal(output, &checks); err != nil {
		logging.Error("failed to parse status checks", "error", err, "output", string(output))
		return fmt.Errorf("failed to parse status checks: %w", err)
	}

	for _, check := range checks {
		status.StatusChecks = append(status.StatusChecks, StatusCheck{
			Context:     check.Name,
			State:       check.State,
			Description: check.Description,
			TargetURL:   check.Link,
			CreatedAt:   check.StartedAt, // Use startedAt for CreatedAt
		})
	}

	logging.Debug("fetched status checks", "count", len(status.StatusChecks))

	return nil
}

// fetchComments fetches PR comments.
func (c *Client) fetchComments(ctx context.Context, repo, prNumber string, status *PRStatus) error {
	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/issues/%s/comments", repo, prNumber))

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh api comments failed: %w", err)
	}

	var comments []struct {
		ID        int       `json:"id"`
		Body      string    `json:"body"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	if err := json.Unmarshal(output, &comments); err != nil {
		return fmt.Errorf("failed to parse comments: %w", err)
	}

	for _, comment := range comments {
		status.Comments = append(status.Comments, Comment{
			ID:        comment.ID,
			Body:      comment.Body,
			Author:    comment.User.Login,
			CreatedAt: comment.CreatedAt,
			UpdatedAt: comment.UpdatedAt,
		})
	}

	return nil
}

// fetchReviews fetches PR reviews.
func (c *Client) fetchReviews(ctx context.Context, repo, prNumber string, status *PRStatus) error {
	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/pulls/%s/reviews", repo, prNumber))

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh api reviews failed: %w", err)
	}

	var reviews []struct {
		ID        int       `json:"id"`
		State     string    `json:"state"`
		Body      string    `json:"body"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		SubmittedAt time.Time `json:"submitted_at"`
	}

	if err := json.Unmarshal(output, &reviews); err != nil {
		return fmt.Errorf("failed to parse reviews: %w", err)
	}

	for _, review := range reviews {
		r := Review{
			ID:        review.ID,
			State:     review.State,
			Body:      review.Body,
			Author:    review.User.Login,
			CreatedAt: review.SubmittedAt,
		}

		// Fetch review comments
		if err := c.fetchReviewComments(ctx, repo, prNumber, review.ID, &r); err != nil {
			// Log but don't fail if we can't fetch comments
			continue
		}

		status.Reviews = append(status.Reviews, r)
	}

	return nil
}

// fetchReviewComments fetches comments for a specific review.
func (c *Client) fetchReviewComments(ctx context.Context, repo, prNumber string, reviewID int, review *Review) error {
	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/pulls/%s/reviews/%d/comments", repo, prNumber, reviewID))

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh api review comments failed: %w", err)
	}

	var comments []struct {
		ID        int       `json:"id"`
		Path      string    `json:"path"`
		Line      int       `json:"line"`
		Body      string    `json:"body"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		CreatedAt time.Time `json:"created_at"`
	}

	if err := json.Unmarshal(output, &comments); err != nil {
		return fmt.Errorf("failed to parse review comments: %w", err)
	}

	for _, comment := range comments {
		review.Comments = append(review.Comments, ReviewComment{
			ID:        comment.ID,
			Path:      comment.Path,
			Line:      comment.Line,
			Body:      comment.Body,
			Author:    comment.User.Login,
			CreatedAt: comment.CreatedAt,
		})
	}

	return nil
}

// fetchWorkflowRuns fetches workflow runs associated with a PR.
func (c *Client) fetchWorkflowRuns(ctx context.Context, repo, prNumber string, status *PRStatus) error {
	// Get the branch name for the PR
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber,
		"--repo", repo,
		"--json", "headRefName")

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get PR branch: %w", err)
	}

	var prInfo struct {
		HeadRefName string `json:"headRefName"`
	}

	if err := json.Unmarshal(output, &prInfo); err != nil {
		return fmt.Errorf("failed to parse PR branch: %w", err)
	}

	// Fetch workflow runs for the branch
	cmd = exec.CommandContext(ctx, "gh", "run", "list",
		"--repo", repo,
		"--branch", prInfo.HeadRefName,
		"--json", "databaseId,name,status,conclusion,url,createdAt,updatedAt",
		"--limit", "10")

	output, err = cmd.Output()
	if err != nil {
		// Workflow runs might not exist
		return nil
	}

	var runs []struct {
		DatabaseID int64     `json:"databaseId"`
		Name       string    `json:"name"`
		Status     string    `json:"status"`
		Conclusion string    `json:"conclusion"`
		URL        string    `json:"url"`
		CreatedAt  time.Time `json:"createdAt"`
		UpdatedAt  time.Time `json:"updatedAt"`
	}

	if err := json.Unmarshal(output, &runs); err != nil {
		return fmt.Errorf("failed to parse workflow runs: %w", err)
	}

	for _, run := range runs {
		workflowRun := WorkflowRun{
			ID:         run.DatabaseID,
			Name:       run.Name,
			Status:     run.Status,
			Conclusion: run.Conclusion,
			URL:        run.URL,
			CreatedAt:  run.CreatedAt,
			UpdatedAt:  run.UpdatedAt,
		}

		// Fetch jobs for this workflow run
		if err := c.fetchWorkflowJobs(ctx, repo, run.DatabaseID, &workflowRun); err != nil {
			// Log but don't fail if we can't fetch jobs
			continue
		}

		status.Workflows = append(status.Workflows, workflowRun)
	}

	return nil
}

// fetchWorkflowJobs fetches jobs for a workflow run.
func (c *Client) fetchWorkflowJobs(ctx context.Context, repo string, runID int64, workflow *WorkflowRun) error {
	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/actions/runs/%d/jobs", repo, runID))

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh api workflow jobs failed: %w", err)
	}

	var response struct {
		Jobs []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
			Steps      []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				Number     int    `json:"number"`
			} `json:"steps"`
		} `json:"jobs"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("failed to parse workflow jobs: %w", err)
	}

	for _, job := range response.Jobs {
		j := Job{
			ID:         job.ID,
			Name:       job.Name,
			Status:     job.Status,
			Conclusion: job.Conclusion,
			URL:        job.HTMLURL,
		}

		for _, step := range job.Steps {
			j.Steps = append(j.Steps, Step{
				Name:       step.Name,
				Status:     step.Status,
				Conclusion: step.Conclusion,
				Number:     step.Number,
			})
		}

		workflow.Jobs = append(workflow.Jobs, j)
	}

	return nil
}

// PostPRComment posts a comment on a PR issue.
func (c *Client) PostPRComment(ctx context.Context, prURL string, body string) error {
	logging.Debug("posting PR comment", "prURL", prURL, "bodyLen", len(body))

	// Extract PR number and repo from URL
	prNumber, repo, err := parsePRURL(prURL)
	if err != nil {
		logging.Error("invalid PR URL for posting comment", "error", err, "prURL", prURL)
		return fmt.Errorf("invalid PR URL: %w", err)
	}

	// Use gh CLI to post the comment
	cmd := exec.CommandContext(ctx, "gh", "pr", "comment", prNumber,
		"--repo", repo,
		"--body", body)

	output, err := cmd.CombinedOutput()
	if err != nil {
		logging.Error("failed to post PR comment", "error", err, "output", string(output))
		return fmt.Errorf("failed to post PR comment: %w\nOutput: %s", err, output)
	}

	logging.Info("successfully posted PR comment", "prURL", prURL)
	return nil
}

// PostReplyToComment posts a reply to a specific comment on a PR.
// This is used to acknowledge when we've created a bead from feedback.
func (c *Client) PostReplyToComment(ctx context.Context, prURL string, commentID int, body string) error {
	logging.Debug("posting reply to comment", "prURL", prURL, "commentID", commentID, "bodyLen", len(body))

	// Extract PR number and repo from URL
	prNumber, repo, err := parsePRURL(prURL)
	if err != nil {
		logging.Error("invalid PR URL for reply", "error", err, "prURL", prURL)
		return fmt.Errorf("invalid PR URL: %w", err)
	}

	// GitHub doesn't support direct replies to specific comments via gh CLI,
	// but we can post a new comment that references the original
	// Format: "@username, regarding your comment: [message]"
	// For now, we'll just post a regular comment mentioning the issue was created
	formattedBody := fmt.Sprintf("Re: Comment #%d\n\n%s", commentID, body)

	// Use gh CLI to post the comment
	cmd := exec.CommandContext(ctx, "gh", "pr", "comment", prNumber,
		"--repo", repo,
		"--body", formattedBody)

	output, err := cmd.CombinedOutput()
	if err != nil {
		logging.Error("failed to post reply", "error", err, "output", string(output))
		return fmt.Errorf("failed to post reply: %w\nOutput: %s", err, output)
	}

	logging.Info("successfully posted reply to comment", "prURL", prURL, "commentID", commentID)
	return nil
}

// PostReviewReply posts a reply to a review comment.
// Since review comments are different from issue comments, this uses the review API.
func (c *Client) PostReviewReply(ctx context.Context, prURL string, reviewCommentID int, body string) error {
	logging.Debug("posting reply to review comment", "prURL", prURL, "reviewCommentID", reviewCommentID, "bodyLen", len(body))

	// Extract PR number and repo from URL
	prNumber, repo, err := parsePRURL(prURL)
	if err != nil {
		logging.Error("invalid PR URL for review reply", "error", err, "prURL", prURL)
		return fmt.Errorf("invalid PR URL: %w", err)
	}

	// Use the GitHub API to post a reply to the review comment
	// We need to create the reply using the API directly
	replyBody := fmt.Sprintf(`{"body": %q, "in_reply_to": %d}`, body, reviewCommentID)

	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/pulls/%s/comments", repo, prNumber),
		"--method", "POST",
		"--input", "-")

	cmd.Stdin = strings.NewReader(replyBody)

	output, err := cmd.CombinedOutput()
	if err != nil {
		logging.Error("failed to post review reply", "error", err, "output", string(output))
		return fmt.Errorf("failed to post review reply: %w\nOutput: %s", err, output)
	}

	logging.Info("successfully posted reply to review comment", "prURL", prURL, "reviewCommentID", reviewCommentID)
	return nil
}

// ResolveReviewThread resolves a review thread containing the specified comment.
// This uses the GraphQL API since the REST API doesn't support resolving threads.
func (c *Client) ResolveReviewThread(ctx context.Context, prURL string, commentID int) error {
	logging.Debug("resolving review thread", "prURL", prURL, "commentID", commentID)

	// Extract PR number and repo from URL
	prNumber, repo, err := parsePRURL(prURL)
	if err != nil {
		logging.Error("invalid PR URL for resolving thread", "error", err, "prURL", prURL)
		return fmt.Errorf("invalid PR URL: %w", err)
	}

	// Parse repo into owner and name
	repoParts := strings.Split(repo, "/")
	if len(repoParts) != 2 {
		return fmt.Errorf("invalid repo format: %s", repo)
	}
	owner := repoParts[0]
	repoName := repoParts[1]

	prNum, err := strconv.Atoi(prNumber)
	if err != nil {
		return fmt.Errorf("invalid PR number: %w", err)
	}

	// First, find the thread ID containing this comment using GraphQL
	threadID, err := c.findReviewThreadID(ctx, owner, repoName, prNum, commentID)
	if err != nil {
		return fmt.Errorf("failed to find review thread: %w", err)
	}

	if threadID == "" {
		logging.Warn("review thread not found for comment", "commentID", commentID)
		return nil // Thread not found, might already be resolved or deleted
	}

	// Resolve the thread using GraphQL mutation
	mutation := fmt.Sprintf(`mutation {
		resolveReviewThread(input: {threadId: "%s"}) {
			thread {
				isResolved
			}
		}
	}`, threadID)

	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-f", fmt.Sprintf("query=%s", mutation))

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if thread is already resolved
		if strings.Contains(string(output), "already resolved") {
			logging.Debug("review thread already resolved", "threadID", threadID)
			return nil
		}
		logging.Error("failed to resolve review thread", "error", err, "output", string(output))
		return fmt.Errorf("failed to resolve review thread: %w\nOutput: %s", err, output)
	}

	logging.Info("successfully resolved review thread", "prURL", prURL, "commentID", commentID, "threadID", threadID)
	return nil
}

// findReviewThreadID finds the GraphQL thread ID for a review comment.
func (c *Client) findReviewThreadID(ctx context.Context, owner, repo string, prNumber, commentID int) (string, error) {
	// Query for review threads and find the one containing our comment
	query := fmt.Sprintf(`query {
		repository(owner: "%s", name: "%s") {
			pullRequest(number: %d) {
				reviewThreads(first: 100) {
					nodes {
						id
						isResolved
						comments(first: 10) {
							nodes {
								databaseId
							}
						}
					}
				}
			}
		}
	}`, owner, repo, prNumber)

	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-f", fmt.Sprintf("query=%s", query))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("graphql query failed: %w\nOutput: %s", err, output)
	}

	// Parse the response to find the thread containing our comment
	var response struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							ID         string `json:"id"`
							IsResolved bool   `json:"isResolved"`
							Comments   struct {
								Nodes []struct {
									DatabaseID int `json:"databaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return "", fmt.Errorf("failed to parse graphql response: %w", err)
	}

	// Find the thread containing the comment
	for _, thread := range response.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if thread.IsResolved {
			continue // Skip already resolved threads
		}
		for _, comment := range thread.Comments.Nodes {
			if comment.DatabaseID == commentID {
				return thread.ID, nil
			}
		}
	}

	return "", nil // Thread not found
}

// parsePRURL extracts the repo and PR number from a GitHub PR URL.
func parsePRURL(prURL string) (prNumber, repo string, err error) {
	// Expected format: https://github.com/owner/repo/pull/123

	// Parse and validate URL
	u, err := url.Parse(prURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}

	// Validate scheme and host
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("URL must use HTTPS scheme, got: %s", u.Scheme)
	}

	if u.Host != "github.com" {
		return "", "", fmt.Errorf("URL must be from github.com, got: %s", u.Host)
	}

	// Parse path components
	// Remove leading/trailing slashes for consistent parsing
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")

	// Validate path structure: owner/repo/pull/number
	if len(parts) != 4 {
		return "", "", fmt.Errorf("invalid PR URL path structure: expected /owner/repo/pull/number, got: %s", u.Path)
	}

	if parts[2] != "pull" {
		return "", "", fmt.Errorf("URL is not a pull request URL (missing 'pull' in path): %s", prURL)
	}

	// Extract components
	owner := parts[0]
	repoName := parts[1]
	prNumber = parts[3]

	// Validate owner and repo names are not empty
	if owner == "" {
		return "", "", fmt.Errorf("owner cannot be empty in PR URL: %s", prURL)
	}

	if repoName == "" {
		return "", "", fmt.Errorf("repository name cannot be empty in PR URL: %s", prURL)
	}

	// Validate PR number is not empty and is numeric
	if prNumber == "" {
		return "", "", fmt.Errorf("PR number cannot be empty in URL: %s", prURL)
	}

	if _, err := strconv.Atoi(prNumber); err != nil {
		return "", "", fmt.Errorf("PR number must be numeric, got: %s", prNumber)
	}

	return prNumber, fmt.Sprintf("%s/%s", owner, repoName), nil
}