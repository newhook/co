package github

import (
	"testing"
	"time"
)

func TestExtractCIStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   *PRStatus
		expected string
	}{
		{
			name:     "no checks or workflows",
			status:   &PRStatus{},
			expected: "pending",
		},
		{
			name: "all status checks success",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "test1", State: "SUCCESS"},
					{Context: "test2", State: "SUCCESS"},
					{Context: "test3", State: "SKIPPED"},
				},
			},
			expected: "success",
		},
		{
			name: "one status check failure",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "test1", State: "SUCCESS"},
					{Context: "test2", State: "FAILURE"},
				},
			},
			expected: "failure",
		},
		{
			name: "one status check pending",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "test1", State: "SUCCESS"},
					{Context: "test2", State: "PENDING"},
				},
			},
			expected: "pending",
		},
		{
			name: "status check with empty state is pending",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "test1", State: "SUCCESS"},
					{Context: "test2", State: ""},
				},
			},
			expected: "pending",
		},
		{
			name: "status check error",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "test1", State: "ERROR"},
				},
			},
			expected: "failure",
		},
		{
			name: "all workflows success",
			status: &PRStatus{
				Workflows: []WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "success"},
					{Name: "Lint", Status: "completed", Conclusion: "success"},
				},
			},
			expected: "success",
		},
		{
			name: "workflow failure",
			status: &PRStatus{
				Workflows: []WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "failure"},
				},
			},
			expected: "failure",
		},
		{
			name: "workflow in progress",
			status: &PRStatus{
				Workflows: []WorkflowRun{
					{Name: "CI", Status: "in_progress", Conclusion: ""},
				},
			},
			expected: "pending",
		},
		{
			name: "workflow queued",
			status: &PRStatus{
				Workflows: []WorkflowRun{
					{Name: "CI", Status: "queued", Conclusion: ""},
				},
			},
			expected: "pending",
		},
		{
			name: "workflow skipped counts as success",
			status: &PRStatus{
				Workflows: []WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "skipped"},
				},
			},
			expected: "success",
		},
		{
			name: "mixed checks and workflows all success",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "test", State: "SUCCESS"},
				},
				Workflows: []WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "success"},
				},
			},
			expected: "success",
		},
		{
			name: "check failure takes precedence over workflow success",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "test", State: "FAILURE"},
				},
				Workflows: []WorkflowRun{
					{Name: "CI", Status: "completed", Conclusion: "success"},
				},
			},
			expected: "failure",
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
		status            *PRStatus
		expectedStatus    string
		expectedApprovers []string
	}{
		{
			name:              "no reviews",
			status:            &PRStatus{},
			expectedStatus:    "pending",
			expectedApprovers: []string{},
		},
		{
			name: "one approval",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    "approved",
			expectedApprovers: []string{"user1"},
		},
		{
			name: "multiple approvals",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: now},
					{ID: 2, State: "APPROVED", Author: "user2", CreatedAt: now},
				},
			},
			expectedStatus:    "approved",
			expectedApprovers: []string{"user1", "user2"},
		},
		{
			name: "changes requested",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "CHANGES_REQUESTED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    "changes_requested",
			expectedApprovers: []string{},
		},
		{
			name: "changes requested takes precedence over approval",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: now},
					{ID: 2, State: "CHANGES_REQUESTED", Author: "user2", CreatedAt: now},
				},
			},
			expectedStatus:    "changes_requested",
			expectedApprovers: []string{"user1"},
		},
		{
			name: "commented reviews are ignored",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "COMMENTED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    "pending",
			expectedApprovers: []string{},
		},
		{
			name: "later approval overrides earlier changes requested",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "CHANGES_REQUESTED", Author: "user1", CreatedAt: earlier},
					{ID: 2, State: "APPROVED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    "approved",
			expectedApprovers: []string{"user1"},
		},
		{
			name: "later changes requested overrides earlier approval",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: earlier},
					{ID: 2, State: "CHANGES_REQUESTED", Author: "user1", CreatedAt: now},
				},
			},
			expectedStatus:    "changes_requested",
			expectedApprovers: []string{},
		},
		{
			name: "bot approval counts",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "APPROVED", Author: "github-actions[bot]", CreatedAt: now},
				},
			},
			expectedStatus:    "approved",
			expectedApprovers: []string{"github-actions[bot]"},
		},
		{
			name: "mixed commented and approved",
			status: &PRStatus{
				Reviews: []Review{
					{ID: 1, State: "APPROVED", Author: "user1", CreatedAt: now},
					{ID: 2, State: "COMMENTED", Author: "user2", CreatedAt: now},
					{ID: 3, State: "COMMENTED", Author: "user3", CreatedAt: now},
				},
			},
			expectedStatus:    "approved",
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
		name               string
		status             *PRStatus
		expectedCI         string
		expectedApproval   string
		expectedApprovers  []string
	}{
		{
			name:             "empty status",
			status:           &PRStatus{},
			expectedCI:       "pending",
			expectedApproval: "pending",
			expectedApprovers: []string{},
		},
		{
			name: "all passing and approved",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "CI", State: "SUCCESS"},
				},
				Reviews: []Review{
					{ID: 1, State: "APPROVED", Author: "reviewer", CreatedAt: now},
				},
			},
			expectedCI:        "success",
			expectedApproval:  "approved",
			expectedApprovers: []string{"reviewer"},
		},
		{
			name: "ci failing but approved",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "CI", State: "FAILURE"},
				},
				Reviews: []Review{
					{ID: 1, State: "APPROVED", Author: "reviewer", CreatedAt: now},
				},
			},
			expectedCI:        "failure",
			expectedApproval:  "approved",
			expectedApprovers: []string{"reviewer"},
		},
		{
			name: "ci passing but changes requested",
			status: &PRStatus{
				StatusChecks: []StatusCheck{
					{Context: "CI", State: "SUCCESS"},
				},
				Reviews: []Review{
					{ID: 1, State: "CHANGES_REQUESTED", Author: "reviewer", CreatedAt: now},
				},
			},
			expectedCI:        "success",
			expectedApproval:  "changes_requested",
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
