package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
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
	// Extract PR number from URL
	prNumber, repo, err := parsePRURL(prURL)
	if err != nil {
		return nil, fmt.Errorf("invalid PR URL: %w", err)
	}

	status := &PRStatus{
		URL: prURL,
	}

	// Fetch basic PR info
	if err := c.fetchPRInfo(ctx, repo, prNumber, status); err != nil {
		return nil, fmt.Errorf("failed to fetch PR info: %w", err)
	}

	// Fetch status checks
	if err := c.fetchStatusChecks(ctx, repo, prNumber, status); err != nil {
		return nil, fmt.Errorf("failed to fetch status checks: %w", err)
	}

	// Fetch comments
	if err := c.fetchComments(ctx, repo, prNumber, status); err != nil {
		return nil, fmt.Errorf("failed to fetch comments: %w", err)
	}

	// Fetch reviews
	if err := c.fetchReviews(ctx, repo, prNumber, status); err != nil {
		return nil, fmt.Errorf("failed to fetch reviews: %w", err)
	}

	// Fetch workflow runs
	if err := c.fetchWorkflowRuns(ctx, repo, prNumber, status); err != nil {
		return nil, fmt.Errorf("failed to fetch workflow runs: %w", err)
	}

	return status, nil
}

// fetchPRInfo fetches basic PR information.
func (c *Client) fetchPRInfo(ctx context.Context, repo, prNumber string, status *PRStatus) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber,
		"--repo", repo,
		"--json", "state,mergeable,mergeStateStatus")

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh pr view failed: %w", err)
	}

	var prInfo struct {
		State          string `json:"state"`
		Mergeable      bool   `json:"mergeable"`
		MergeStateStatus string `json:"mergeStateStatus"`
	}

	if err := json.Unmarshal(output, &prInfo); err != nil {
		return fmt.Errorf("failed to parse PR info: %w", err)
	}

	status.State = prInfo.State
	status.Mergeable = prInfo.Mergeable
	status.MergeableState = prInfo.MergeStateStatus

	return nil
}

// fetchStatusChecks fetches PR status checks.
func (c *Client) fetchStatusChecks(ctx context.Context, repo, prNumber string, status *PRStatus) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "checks", prNumber,
		"--repo", repo,
		"--json", "name,state,description,link,createdAt")

	output, err := cmd.Output()
	if err != nil {
		// Status checks might not exist
		if strings.Contains(err.Error(), "no checks") {
			return nil
		}
		return fmt.Errorf("gh pr checks failed: %w", err)
	}

	var checks []struct {
		Name        string    `json:"name"`
		State       string    `json:"state"`
		Description string    `json:"description"`
		Link        string    `json:"link"`
		CreatedAt   time.Time `json:"createdAt"`
	}

	if err := json.Unmarshal(output, &checks); err != nil {
		return fmt.Errorf("failed to parse status checks: %w", err)
	}

	for _, check := range checks {
		status.StatusChecks = append(status.StatusChecks, StatusCheck{
			Context:     check.Name,
			State:       check.State,
			Description: check.Description,
			TargetURL:   check.Link,
			CreatedAt:   check.CreatedAt,
		})
	}

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

// parsePRURL extracts the repo and PR number from a GitHub PR URL.
func parsePRURL(prURL string) (prNumber, repo string, err error) {
	// Expected format: https://github.com/owner/repo/pull/123
	parts := strings.Split(prURL, "/")
	if len(parts) < 7 || parts[5] != "pull" {
		return "", "", fmt.Errorf("invalid PR URL format: %s", prURL)
	}

	owner := parts[3]
	repoName := parts[4]
	prNumber = parts[6]

	return prNumber, fmt.Sprintf("%s/%s", owner, repoName), nil
}