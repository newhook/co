package github

import (
	"context"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name        string
		prURL       string
		wantNumber  string
		wantRepo    string
		wantErr     bool
	}{
		{
			name:       "Valid GitHub PR URL",
			prURL:      "https://github.com/owner/repo/pull/123",
			wantNumber: "123",
			wantRepo:   "owner/repo",
			wantErr:    false,
		},
		{
			name:       "Valid GitHub PR URL with trailing slash",
			prURL:      "https://github.com/owner/repo/pull/456/",
			wantNumber: "456",
			wantRepo:   "owner/repo",
			wantErr:    false, // Trailing slashes are trimmed, so this should work
		},
		{
			name:       "Valid GitHub PR URL with subdomain",
			prURL:      "https://github.com/my-org/my-repo/pull/789",
			wantNumber: "789",
			wantRepo:   "my-org/my-repo",
			wantErr:    false,
		},
		{
			name:       "Invalid URL - not a PR",
			prURL:      "https://github.com/owner/repo/issues/123",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true,
		},
		{
			name:       "Invalid URL - missing PR number",
			prURL:      "https://github.com/owner/repo/pull/",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true, // Now properly validates empty PR number
		},
		{
			name:       "Invalid URL - not GitHub",
			prURL:      "https://gitlab.com/owner/repo/pull/123",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true, // Now properly validates domain
		},
		{
			name:       "Invalid URL - malformed",
			prURL:      "not-a-url",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true,
		},
		{
			name:       "Invalid URL - too short",
			prURL:      "https://github.com/owner",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true,
		},
		{
			name:       "Invalid URL - non-numeric PR number",
			prURL:      "https://github.com/owner/repo/pull/abc",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true,
		},
		{
			name:       "Invalid URL - HTTP instead of HTTPS",
			prURL:      "http://github.com/owner/repo/pull/123",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true,
		},
		{
			name:       "Invalid URL - extra path components",
			prURL:      "https://github.com/owner/repo/pull/123/files",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true,
		},
		{
			name:       "Invalid URL - empty owner",
			prURL:      "https://github.com//repo/pull/123",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true,
		},
		{
			name:       "Invalid URL - empty repo",
			prURL:      "https://github.com/owner//pull/123",
			wantNumber: "",
			wantRepo:   "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prNumber, repo, err := parsePRURL(tt.prURL)

			if (err != nil) != tt.wantErr {
				t.Errorf("parsePRURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if prNumber != tt.wantNumber {
				t.Errorf("parsePRURL() prNumber = %v, want %v", prNumber, tt.wantNumber)
			}

			if repo != tt.wantRepo {
				t.Errorf("parsePRURL() repo = %v, want %v", repo, tt.wantRepo)
			}
		})
	}
}

func TestGetPRStatus(t *testing.T) {
	client := NewClient()
	ctx := context.Background()

	t.Run("Valid PR URL", func(t *testing.T) {
		// This test requires GitHub API access and will fail without proper auth
		t.Skip("Skipping test that requires GitHub API access")

		prURL := "https://github.com/torvalds/linux/pull/1"
		status, err := client.GetPRStatus(ctx, prURL)

		if err == nil {
			if status == nil {
				t.Error("GetPRStatus returned nil status without error")
			}
			if status.URL != prURL {
				t.Errorf("Status URL = %s, want %s", status.URL, prURL)
			}
		}
	})

	t.Run("Invalid PR URL", func(t *testing.T) {
		prURL := "not-a-valid-url"
		_, err := client.GetPRStatus(ctx, prURL)

		if err == nil {
			t.Error("Expected error for invalid PR URL")
		}
	})
}

func TestFetchPRInfo(t *testing.T) {
	client := NewClient()
	ctx := context.Background()

	t.Run("Fetch PR info", func(t *testing.T) {
		// This test requires GitHub CLI to be installed and authenticated
		t.Skip("Skipping test that requires gh CLI")

		status := &PRStatus{}
		err := client.fetchPRInfo(ctx, "torvalds/linux", "1", status)

		if err == nil {
			// Basic validation of the status fields
			if status.State == "" {
				t.Error("PR state should not be empty")
			}
			if status.MergeableState == "" {
				t.Error("PR mergeable state should not be empty")
			}
		}
	})
}

func TestFetchStatusChecks(t *testing.T) {
	client := NewClient()
	ctx := context.Background()

	t.Run("Fetch status checks", func(t *testing.T) {
		// This test requires GitHub CLI to be installed and authenticated
		t.Skip("Skipping test that requires gh CLI")

		status := &PRStatus{}
		err := client.fetchStatusChecks(ctx, "owner/repo", "123", status)

		if err == nil {
			// Status checks might be empty, which is valid
			if status.StatusChecks == nil {
				t.Error("StatusChecks should be initialized even if empty")
			}
		}
	})
}

func TestFetchComments(t *testing.T) {
	client := NewClient()
	ctx := context.Background()

	t.Run("Fetch comments", func(t *testing.T) {
		// This test requires GitHub CLI to be installed and authenticated
		t.Skip("Skipping test that requires gh CLI")

		status := &PRStatus{}
		err := client.fetchComments(ctx, "owner/repo", "123", status)

		if err == nil {
			// Comments might be empty, which is valid
			if status.Comments == nil {
				t.Error("Comments should be initialized even if empty")
			}
		}
	})
}

func TestFetchReviews(t *testing.T) {
	client := NewClient()
	ctx := context.Background()

	t.Run("Fetch reviews", func(t *testing.T) {
		// This test requires GitHub CLI to be installed and authenticated
		t.Skip("Skipping test that requires gh CLI")

		status := &PRStatus{}
		err := client.fetchReviews(ctx, "owner/repo", "123", status)

		if err == nil {
			// Reviews might be empty, which is valid
			if status.Reviews == nil {
				t.Error("Reviews should be initialized even if empty")
			}

			// Check that review comments are fetched for each review
			for _, review := range status.Reviews {
				if review.Comments == nil {
					t.Error("Review comments should be initialized even if empty")
				}
			}
		}
	})
}

func TestFetchWorkflowRuns(t *testing.T) {
	client := NewClient()
	ctx := context.Background()

	t.Run("Fetch workflow runs", func(t *testing.T) {
		// This test requires GitHub CLI to be installed and authenticated
		t.Skip("Skipping test that requires gh CLI")

		status := &PRStatus{}
		err := client.fetchWorkflowRuns(ctx, "owner/repo", "123", status)

		if err == nil {
			// Workflows might be empty, which is valid
			if status.Workflows == nil {
				t.Error("Workflows should be initialized even if empty")
			}

			// Check that jobs are fetched for each workflow
			for _, workflow := range status.Workflows {
				if workflow.Jobs == nil {
					t.Error("Workflow jobs should be initialized even if empty")
				}

				// Check that steps are fetched for each job
				for _, job := range workflow.Jobs {
					if job.Steps == nil {
						t.Error("Job steps should be initialized even if empty")
					}
				}
			}
		}
	})
}

// Test data structures
func TestPRStatusStructure(t *testing.T) {
	// Test that PRStatus can be properly initialized
	status := &PRStatus{
		URL:            "https://github.com/owner/repo/pull/123",
		State:          "OPEN",
		Mergeable:      true,
		MergeableState: "clean",
		StatusChecks:   []StatusCheck{},
		Comments:       []Comment{},
		Reviews:        []Review{},
		Workflows:      []WorkflowRun{},
	}

	if status.URL != "https://github.com/owner/repo/pull/123" {
		t.Error("PRStatus URL not set correctly")
	}
	if status.State != "OPEN" {
		t.Error("PRStatus State not set correctly")
	}
	if !status.Mergeable {
		t.Error("PRStatus Mergeable not set correctly")
	}
	if status.MergeableState != "clean" {
		t.Error("PRStatus MergeableState not set correctly")
	}
}

func TestStatusCheckStructure(t *testing.T) {
	check := StatusCheck{
		Context:     "continuous-integration/travis-ci",
		State:       "SUCCESS",
		Description: "The Travis CI build passed",
		TargetURL:   "https://travis-ci.org/owner/repo/builds/123",
	}

	if check.Context != "continuous-integration/travis-ci" {
		t.Error("StatusCheck Context not set correctly")
	}
	if check.State != "SUCCESS" {
		t.Error("StatusCheck State not set correctly")
	}
	if check.Description != "The Travis CI build passed" {
		t.Error("StatusCheck Description not set correctly")
	}
	if check.TargetURL != "https://travis-ci.org/owner/repo/builds/123" {
		t.Error("StatusCheck TargetURL not set correctly")
	}
}

func TestWorkflowRunStructure(t *testing.T) {
	workflow := WorkflowRun{
		ID:         123456,
		Name:       "CI Pipeline",
		Status:     "completed",
		Conclusion: "success",
		URL:        "https://github.com/owner/repo/actions/runs/123456",
		Jobs: []Job{
			{
				ID:         789,
				Name:       "Test",
				Status:     "completed",
				Conclusion: "success",
				Steps: []Step{
					{
						Name:       "Run tests",
						Status:     "completed",
						Conclusion: "success",
						Number:     3,
					},
				},
			},
		},
	}

	if workflow.ID != 123456 {
		t.Error("WorkflowRun ID not set correctly")
	}
	if workflow.Name != "CI Pipeline" {
		t.Error("WorkflowRun Name not set correctly")
	}
	if len(workflow.Jobs) != 1 {
		t.Error("WorkflowRun Jobs not set correctly")
	}
	if len(workflow.Jobs[0].Steps) != 1 {
		t.Error("Job Steps not set correctly")
	}
	if workflow.Jobs[0].Steps[0].Number != 3 {
		t.Error("Step Number not set correctly")
	}
}

func TestReviewStructure(t *testing.T) {
	review := Review{
		ID:     999,
		State:  "APPROVED",
		Body:   "LGTM!",
		Author: "reviewer1",
		Comments: []ReviewComment{
			{
				ID:     111,
				Path:   "src/main.go",
				Line:   42,
				Body:   "Consider using a constant here",
				Author: "reviewer1",
			},
		},
	}

	if review.ID != 999 {
		t.Error("Review ID not set correctly")
	}
	if review.State != "APPROVED" {
		t.Error("Review State not set correctly")
	}
	if review.Body != "LGTM!" {
		t.Error("Review Body not set correctly")
	}
	if review.Author != "reviewer1" {
		t.Error("Review Author not set correctly")
	}
	if len(review.Comments) != 1 {
		t.Error("Review Comments not set correctly")
	}
	if review.Comments[0].Line != 42 {
		t.Error("ReviewComment Line not set correctly")
	}
}