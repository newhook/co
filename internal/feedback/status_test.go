package feedback

import (
	"testing"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/github"
)

func TestExtractCIStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   *github.PRStatus
		expected string
	}{
		{
			name:     "no checks or workflows",
			status:   &github.PRStatus{},
			expected: db.CIStatusPending,
		},
		{
			name: "all status checks success",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "test1", State: "SUCCESS"},
					{Context: "test2", State: "SUCCESS"},
					{Context: "test3", State: "SKIPPED"},
				},
			},
			expected: db.CIStatusSuccess,
		},
		{
			name: "one status check failure",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "test1", State: "SUCCESS"},
					{Context: "test2", State: "FAILURE"},
				},
			},
			expected: db.CIStatusFailure,
		},
		{
			name: "one status check pending",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "test1", State: "SUCCESS"},
					{Context: "test2", State: "PENDING"},
				},
			},
			expected: db.CIStatusPending,
		},
		{
			name: "status check with empty state is pending",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "test1", State: "SUCCESS"},
					{Context: "test2", State: ""},
				},
			},
			expected: db.CIStatusPending,
		},
		{
			name: "status check error",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "test1", State: "ERROR"},
				},
			},
			expected: db.CIStatusFailure,
		},
		{
			name: "all workflows success",
			status: &github.PRStatus{
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "success"},
					{Name: "Lint", Status: "completed", Conclusion: "success"},
				},
			},
			expected: db.CIStatusSuccess,
		},
		{
			name: "workflow failure",
			status: &github.PRStatus{
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "failure"},
				},
			},
			expected: db.CIStatusFailure,
		},
		{
			name: "workflow in progress",
			status: &github.PRStatus{
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "in_progress", Conclusion: ""},
				},
			},
			expected: db.CIStatusPending,
		},
		{
			name: "workflow queued",
			status: &github.PRStatus{
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "queued", Conclusion: ""},
				},
			},
			expected: db.CIStatusPending,
		},
		{
			name: "workflow skipped counts as success",
			status: &github.PRStatus{
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "skipped"},
				},
			},
			expected: db.CIStatusSuccess,
		},
		{
			name: "mixed checks and workflows all success",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "test", State: "SUCCESS"},
				},
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "success"},
				},
			},
			expected: db.CIStatusSuccess,
		},
		{
			name: "latest workflow success ignores historical failures",
			status: &github.PRStatus{
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "failure", CreatedAt: time.Now().Add(-2 * time.Hour)},
					{Name: "CI", Status: "completed", Conclusion: "success", CreatedAt: time.Now().Add(-1 * time.Hour)},
					{Name: "CI", Status: "completed", Conclusion: "success", CreatedAt: time.Now()},
				},
			},
			expected: db.CIStatusSuccess,
		},
		{
			name: "latest workflow failure takes precedence over historical success",
			status: &github.PRStatus{
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "success", CreatedAt: time.Now().Add(-1 * time.Hour)},
					{Name: "CI", Status: "completed", Conclusion: "failure", CreatedAt: time.Now()},
				},
			},
			expected: db.CIStatusFailure,
		},
		{
			name: "multiple workflows each using latest run",
			status: &github.PRStatus{
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "failure", CreatedAt: time.Now().Add(-2 * time.Hour)},
					{Name: "CI", Status: "completed", Conclusion: "success", CreatedAt: time.Now()},
					{Name: "Lint", Status: "completed", Conclusion: "success", CreatedAt: time.Now().Add(-1 * time.Hour)},
					{Name: "Lint", Status: "completed", Conclusion: "success", CreatedAt: time.Now()},
				},
			},
			expected: db.CIStatusSuccess,
		},
		{
			name: "check failure takes precedence over workflow success",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "test", State: "FAILURE"},
				},
				Workflows: []github.WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "success"},
				},
			},
			expected: db.CIStatusFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCIStatus(tt.status)
			if result != tt.expected {
				t.Errorf("extractCIStatus() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractApprovalStatus(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	tests := []struct {
		name              string
		status            *github.PRStatus
		expectedStatus    string
		expectedApprovers []string
	}{
		{
			name:              "no reviews",
			status:            &github.PRStatus{},
			expectedStatus:    db.ApprovalStatusPending,
			expectedApprovers: []string{},
		},
		{
			name: "one approval",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusApproved,
			expectedApprovers: []string{"user1"},
		},
		{
			name: "multiple approvals",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: now},
					{ID: 2, State: "APPROVED", Author: "user2", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusApproved,
			expectedApprovers: []string{"user1", "user2"},
		},
		{
			name: "changes requested",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "CHANGES_REQUESTED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusChangesRequested,
			expectedApprovers: []string{},
		},
		{
			name: "changes requested takes precedence over approval",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: now},
					{ID: 2, State: "CHANGES_REQUESTED", Author: "user2", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusChangesRequested,
			expectedApprovers: []string{"user1"},
		},
		{
			name: "commented reviews are ignored",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "COMMENTED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusPending,
			expectedApprovers: []string{},
		},
		{
			name: "later approval overrides earlier changes requested",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "CHANGES_REQUESTED", Author: "user1", CreatedAt: earlier},
					{ID: 2, State: "APPROVED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusApproved,
			expectedApprovers: []string{"user1"},
		},
		{
			name: "later changes requested overrides earlier approval",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: earlier},
					{ID: 2, State: "CHANGES_REQUESTED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusChangesRequested,
			expectedApprovers: []string{},
		},
		{
			name: "bot approval counts",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "APPROVED", Author: "github-actions[bot]", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusApproved,
			expectedApprovers: []string{"github-actions[bot]"},
		},
		{
			name: "mixed commented and approved",
			status: &github.PRStatus{
				Reviews: []github.Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: now},
					{ID: 2, State: "COMMENTED", Author: "user2", CreatedAt: now},
					{ID: 3, State: "COMMENTED", Author: "user3", CreatedAt: now},
				},
			},
			expectedStatus:    db.ApprovalStatusApproved,
			expectedApprovers: []string{"user1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, approvers := extractApprovalStatus(tt.status)
			if status != tt.expectedStatus {
				t.Errorf("extractApprovalStatus() status = %q, want %q", status, tt.expectedStatus)
			}

			// Check approvers (order may vary)
			if len(approvers) != len(tt.expectedApprovers) {
				t.Errorf("extractApprovalStatus() approvers count = %d, want %d", len(approvers), len(tt.expectedApprovers))
			} else {
				approverSet := make(map[string]bool)
				for _, a := range approvers {
					approverSet[a] = true
				}
				for _, expected := range tt.expectedApprovers {
					if !approverSet[expected] {
						t.Errorf("extractApprovalStatus() missing approver %q", expected)
					}
				}
			}
		})
	}
}

func TestExtractStatusFromPRStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name              string
		status            *github.PRStatus
		expectedCI        string
		expectedApproval  string
		expectedApprovers []string
	}{
		{
			name:              "empty status",
			status:            &github.PRStatus{},
			expectedCI:        db.CIStatusPending,
			expectedApproval:  db.ApprovalStatusPending,
			expectedApprovers: []string{},
		},
		{
			name: "all passing and approved",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "CI", State: "SUCCESS"},
				},
				Reviews: []github.Review{
					{ID: 1, State: "APPROVED", Author: "reviewer", CreatedAt: now},
				},
			},
			expectedCI:        db.CIStatusSuccess,
			expectedApproval:  db.ApprovalStatusApproved,
			expectedApprovers: []string{"reviewer"},
		},
		{
			name: "ci failing but approved",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "CI", State: "FAILURE"},
				},
				Reviews: []github.Review{
					{ID: 1, State: "APPROVED", Author: "reviewer", CreatedAt: now},
				},
			},
			expectedCI:        db.CIStatusFailure,
			expectedApproval:  db.ApprovalStatusApproved,
			expectedApprovers: []string{"reviewer"},
		},
		{
			name: "ci passing but changes requested",
			status: &github.PRStatus{
				StatusChecks: []github.StatusCheck{
					{Context: "CI", State: "SUCCESS"},
				},
				Reviews: []github.Review{
					{ID: 1, State: "CHANGES_REQUESTED", Author: "reviewer", CreatedAt: now},
				},
			},
			expectedCI:        db.CIStatusSuccess,
			expectedApproval:  db.ApprovalStatusChangesRequested,
			expectedApprovers: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := ExtractStatusFromPRStatus(tt.status)

			if info.CIStatus != tt.expectedCI {
				t.Errorf("CIStatus = %q, want %q", info.CIStatus, tt.expectedCI)
			}
			if info.ApprovalStatus != tt.expectedApproval {
				t.Errorf("ApprovalStatus = %q, want %q", info.ApprovalStatus, tt.expectedApproval)
			}
			if len(info.Approvers) != len(tt.expectedApprovers) {
				t.Errorf("Approvers count = %d, want %d", len(info.Approvers), len(tt.expectedApprovers))
			}
		})
	}
}
