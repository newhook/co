package github

import (
	"strings"
	"testing"
)

func TestCreateBeadFromFeedback_ExternalRef(t *testing.T) {
	tests := []struct {
		name           string
		beadInfo       BeadInfo
		wantExtRef     bool
		extRefContains string
	}{
		{
			name: "With GitHub comment URL",
			beadInfo: BeadInfo{
				Title:       "Fix test failure",
				Description: "Test failed",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Labels:      []string{"from-pr-feedback"},
				Metadata: map[string]string{
					"source_url": "https://github.com/owner/repo/pull/123#issuecomment-456789",
					"source_id":  "456789",
				},
			},
			wantExtRef:     true,
			extRefContains: "gh-comment-456789",
		},
		{
			name: "With PR URL",
			beadInfo: BeadInfo{
				Title:       "Address review feedback",
				Description: "Please fix this",
				Type:        "task",
				Priority:    1,
				ParentID:    "beads-123",
				Labels:      []string{"from-pr-feedback", "review-feedback"},
				Metadata: map[string]string{
					"source_url": "https://github.com/owner/repo/pull/456",
					"source_id":  "123",
				},
			},
			wantExtRef:     true,
			extRefContains: "gh-pr-456",
		},
		{
			name: "Without source URL",
			beadInfo: BeadInfo{
				Title:       "General task",
				Description: "Do something",
				Type:        "task",
				Priority:    3,
				ParentID:    "beads-123",
				Labels:      []string{},
				Metadata:    map[string]string{},
			},
			wantExtRef: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't actually run bd create in tests, but we can verify the logic
			// by checking that extractGitHubID works correctly
			if sourceURL, ok := tt.beadInfo.Metadata["source_url"]; ok && sourceURL != "" {
				extRef := extractGitHubID(sourceURL)
				fullRef := "gh-" + extRef

				if tt.wantExtRef && !strings.Contains(fullRef, tt.extRefContains) {
					t.Errorf("Expected external ref to contain %q, got %q", tt.extRefContains, fullRef)
				}
			} else if tt.wantExtRef {
				t.Error("Expected external ref but no source_url in metadata")
			}
		})
	}
}

func TestExtractGitHubID(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{
			url:      "https://github.com/owner/repo/pull/123#issuecomment-456789",
			expected: "comment-456789",
		},
		{
			url:      "https://github.com/owner/repo/pull/123",
			expected: "pr-123",
		},
		{
			url:      "https://github.com/owner/repo/issues/456",
			expected: "issue-456",
		},
		{
			url:      "https://github.com/owner/repo/actions/runs/789",
			expected: "789",
		},
		{
			url:      "https://example.com/something",
			expected: "something",
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := extractGitHubID(tt.url)
			if result != tt.expected {
				t.Errorf("extractGitHubID(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestFeedbackProcessing_SourceIDAdded(t *testing.T) {
	client := NewClient()
	processor := NewFeedbackProcessor(client, DefaultFeedbackRules())

	// Test that source_id is added to Context when processing reviews
	status := &PRStatus{
		URL:   "https://github.com/owner/repo/pull/123",
		State: "OPEN",
		Reviews: []Review{
			{
				ID:     999,
				Author: "reviewer1",
				State:  "CHANGES_REQUESTED",
				Body:   "Please fix this issue",
			},
		},
		Comments: []Comment{
			{
				ID:     888,
				Author: "user1",
				Body:   "This needs to be fixed urgently",
			},
		},
	}

	// Process reviews
	reviewItems := processor.processReviews(status)
	if len(reviewItems) > 0 {
		item := reviewItems[0]
		if sourceID, ok := item.Context["source_id"]; !ok {
			t.Error("Expected source_id in review feedback context")
		} else if sourceID != "999" {
			t.Errorf("Expected source_id to be '999', got %q", sourceID)
		}
	}

	// Process comments
	commentItems := processor.processComments(status)
	if len(commentItems) > 0 {
		item := commentItems[0]
		if sourceID, ok := item.Context["source_id"]; !ok {
			t.Error("Expected source_id in comment feedback context")
		} else if sourceID != "888" {
			t.Errorf("Expected source_id to be '888', got %q", sourceID)
		}
	}
}