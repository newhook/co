package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	require.NotNil(t, client, "NewClient returned nil")
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name       string
		prURL      string
		wantNumber string
		wantRepo   string
		wantErr    bool
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

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.wantNumber, prNumber)
			require.Equal(t, tt.wantRepo, repo)
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
			require.NotNil(t, status, "GetPRStatus returned nil status without error")
			require.Equal(t, prURL, status.URL)
		}
	})

	t.Run("Invalid PR URL", func(t *testing.T) {
		prURL := "not-a-valid-url"
		_, err := client.GetPRStatus(ctx, prURL)

		require.Error(t, err, "Expected error for invalid PR URL")
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
			require.NotEmpty(t, status.State, "PR state should not be empty")
			require.NotEmpty(t, status.MergeableState, "PR mergeable state should not be empty")
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
			require.NotNil(t, status.StatusChecks, "StatusChecks should be initialized even if empty")
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
			require.NotNil(t, status.Comments, "Comments should be initialized even if empty")
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
			require.NotNil(t, status.Reviews, "Reviews should be initialized even if empty")

			// Check that review comments are fetched for each review
			for _, review := range status.Reviews {
				require.NotNil(t, review.Comments, "Review comments should be initialized even if empty")
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
			require.NotNil(t, status.Workflows, "Workflows should be initialized even if empty")

			// Check that jobs are fetched for each workflow
			for _, workflow := range status.Workflows {
				require.NotNil(t, workflow.Jobs, "Workflow jobs should be initialized even if empty")

				// Check that steps are fetched for each job
				for _, job := range workflow.Jobs {
					require.NotNil(t, job.Steps, "Job steps should be initialized even if empty")
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

	require.Equal(t, "https://github.com/owner/repo/pull/123", status.URL, "PRStatus URL not set correctly")
	require.Equal(t, "OPEN", status.State, "PRStatus State not set correctly")
	require.True(t, status.Mergeable, "PRStatus Mergeable not set correctly")
	require.Equal(t, "clean", status.MergeableState, "PRStatus MergeableState not set correctly")
}

func TestStatusCheckStructure(t *testing.T) {
	check := StatusCheck{
		Context:     "continuous-integration/travis-ci",
		State:       "SUCCESS",
		Description: "The Travis CI build passed",
		TargetURL:   "https://travis-ci.org/owner/repo/builds/123",
	}

	require.Equal(t, "continuous-integration/travis-ci", check.Context, "StatusCheck Context not set correctly")
	require.Equal(t, "SUCCESS", check.State, "StatusCheck State not set correctly")
	require.Equal(t, "The Travis CI build passed", check.Description, "StatusCheck Description not set correctly")
	require.Equal(t, "https://travis-ci.org/owner/repo/builds/123", check.TargetURL, "StatusCheck TargetURL not set correctly")
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

	require.Equal(t, int64(123456), workflow.ID, "WorkflowRun ID not set correctly")
	require.Equal(t, "CI Pipeline", workflow.Name, "WorkflowRun Name not set correctly")
	require.Len(t, workflow.Jobs, 1, "WorkflowRun Jobs not set correctly")
	require.Len(t, workflow.Jobs[0].Steps, 1, "Job Steps not set correctly")
	require.Equal(t, 3, workflow.Jobs[0].Steps[0].Number, "Step Number not set correctly")
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

	require.Equal(t, 999, review.ID, "Review ID not set correctly")
	require.Equal(t, "APPROVED", review.State, "Review State not set correctly")
	require.Equal(t, "LGTM!", review.Body, "Review Body not set correctly")
	require.Equal(t, "reviewer1", review.Author, "Review Author not set correctly")
	require.Len(t, review.Comments, 1, "Review Comments not set correctly")
	require.Equal(t, 42, review.Comments[0].Line, "ReviewComment Line not set correctly")
}
