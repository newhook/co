package github

import (
	"context"
	"strings"
	"testing"
)

func TestNewBeadCreator(t *testing.T) {
	processor := &FeedbackProcessor{}
	creator := NewBeadCreator(processor)

	if creator == nil {
		t.Fatal("NewBeadCreator returned nil")
	}
	if creator.processor != processor {
		t.Error("BeadCreator processor not set correctly")
	}
}

func TestFeedbackToBeadInfo(t *testing.T) {
	processor := NewFeedbackProcessor(nil, nil)
	creator := NewBeadCreator(processor)

	item := FeedbackItem{
		Type:        FeedbackTypeTest,
		Title:       "Fix failing tests",
		Description: "Unit tests are failing",
		Source:      "CI: test-suite",
		SourceURL:   "https://example.com/runs/123",
		Priority:    2,
		Actionable:  true,
		Context: map[string]string{
			"workflow": "Test Suite",
			"failure":  "unit tests",
		},
	}

	rootIssueID := "beads-123"
	beadInfo := creator.feedbackToBeadInfo(item, rootIssueID)

	// Check basic fields
	if beadInfo.Title != "Fix failing tests" {
		t.Errorf("Title = %s, want %s", beadInfo.Title, "Fix failing tests")
	}
	if beadInfo.Type != "bug" {
		t.Errorf("Type = %s, want bug", beadInfo.Type)
	}
	if beadInfo.Priority != 2 {
		t.Errorf("Priority = %d, want 2", beadInfo.Priority)
	}
	if beadInfo.ParentID != rootIssueID {
		t.Errorf("ParentID = %s, want %s", beadInfo.ParentID, rootIssueID)
	}

	// Check labels
	expectedLabels := []string{"test-failure", "from-pr-feedback"}
	if len(beadInfo.Labels) != len(expectedLabels) {
		t.Errorf("Labels count = %d, want %d", len(beadInfo.Labels), len(expectedLabels))
	}
	for i, label := range expectedLabels {
		if i >= len(beadInfo.Labels) || beadInfo.Labels[i] != label {
			t.Errorf("Label[%d] = %s, want %s", i, beadInfo.Labels[i], label)
		}
	}

	// Check metadata
	if beadInfo.Metadata["source"] != "CI: test-suite" {
		t.Errorf("Metadata[source] = %s, want CI: test-suite", beadInfo.Metadata["source"])
	}
	if beadInfo.Metadata["feedback_type"] != "test_failure" {
		t.Errorf("Metadata[feedback_type] = %s, want test_failure", beadInfo.Metadata["feedback_type"])
	}
}

func TestGetBeadType(t *testing.T) {
	creator := &BeadCreator{}

	tests := []struct {
		feedbackType FeedbackType
		expected     string
	}{
		{FeedbackTypeTest, "bug"},
		{FeedbackTypeBuild, "bug"},
		{FeedbackTypeCI, "bug"},
		{FeedbackTypeLint, "task"},
		{FeedbackTypeSecurity, "task"},
		{FeedbackTypeReview, "task"},
		{FeedbackTypeGeneral, "task"},
	}

	for _, tt := range tests {
		t.Run(string(tt.feedbackType), func(t *testing.T) {
			result := creator.getBeadType(tt.feedbackType)
			if result != tt.expected {
				t.Errorf("getBeadType(%v) = %s, want %s", tt.feedbackType, result, tt.expected)
			}
		})
	}
}

func TestGetLabels(t *testing.T) {
	creator := &BeadCreator{}

	tests := []struct {
		feedbackType   FeedbackType
		expectedLabels []string
	}{
		{FeedbackTypeCI, []string{"ci-failure"}},
		{FeedbackTypeTest, []string{"test-failure"}},
		{FeedbackTypeLint, []string{"lint-issue"}},
		{FeedbackTypeBuild, []string{"build-failure"}},
		{FeedbackTypeReview, []string{"review-feedback"}},
		{FeedbackTypeSecurity, []string{"security"}},
		{FeedbackTypeGeneral, []string{}},
	}

	for _, tt := range tests {
		t.Run(string(tt.feedbackType), func(t *testing.T) {
			result := creator.getLabels(tt.feedbackType)
			if len(result) != len(tt.expectedLabels) {
				t.Errorf("getLabels(%v) returned %d labels, want %d",
					tt.feedbackType, len(result), len(tt.expectedLabels))
			}
			for i, label := range tt.expectedLabels {
				if i >= len(result) || result[i] != label {
					t.Errorf("Label[%d] = %s, want %s", i, result[i], label)
				}
			}
		})
	}
}

func TestFormatDescription(t *testing.T) {
	creator := &BeadCreator{}

	item := FeedbackItem{
		Type:        FeedbackTypeTest,
		Description: "Tests are failing",
		Source:      "CI: test-suite",
		SourceURL:   "https://example.com/runs/123",
		Context: map[string]string{
			"workflow":    "Test Suite",
			"failed_step": "unit tests",
		},
	}

	description := creator.formatDescription(item)

	// Check that description contains expected sections
	if !strings.Contains(description, "Tests are failing") {
		t.Error("Description should contain original description")
	}
	if !strings.Contains(description, "## Source") {
		t.Error("Description should contain Source section")
	}
	if !strings.Contains(description, "## Context") {
		t.Error("Description should contain Context section")
	}
	if !strings.Contains(description, "## Resolution") {
		t.Error("Description should contain Resolution section")
	}
	if !strings.Contains(description, "Fix the failing tests") {
		t.Error("Description should contain test-specific resolution guidance")
	}
}

func TestCreateDeduplicationKey(t *testing.T) {
	creator := &BeadCreator{}

	tests := []struct {
		name     string
		bead     BeadInfo
		expected string
	}{
		{
			name: "File-specific issue",
			bead: BeadInfo{
				Type:  "bug",
				Title: "Fix test failure",
				Metadata: map[string]string{
					"file": "src/main.go",
				},
			},
			expected: "bug:src/main.go:test failure",
		},
		{
			name: "Workflow issue",
			bead: BeadInfo{
				Type:  "bug",
				Title: "Fix build error",
				Metadata: map[string]string{
					"workflow": "CI Pipeline",
				},
			},
			expected: "bug:workflow:CI Pipeline",
		},
		{
			name: "Check issue",
			bead: BeadInfo{
				Type:  "task",
				Title: "Address lint issues",
				Metadata: map[string]string{
					"check_name": "eslint",
				},
			},
			expected: "task:check:eslint",
		},
		{
			name: "Generic issue",
			bead: BeadInfo{
				Type:     "task",
				Title:    "Fix security vulnerability",
				Metadata: map[string]string{},
			},
			expected: "task:security vulnerability",
		},
		{
			name: "Remove common prefixes",
			bead: BeadInfo{
				Type:     "bug",
				Title:    "Fix authentication bug",
				Metadata: map[string]string{},
			},
			expected: "bug:authentication bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := creator.createDeduplicationKey(tt.bead)
			if result != tt.expected {
				t.Errorf("createDeduplicationKey() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDeduplicateBeads(t *testing.T) {
	creator := &BeadCreator{}

	beads := []BeadInfo{
		{
			Title:    "Fix test failure",
			Type:     "bug",
			Priority: 2,
			Metadata: map[string]string{"workflow": "Tests"},
		},
		{
			Title:    "Fix test failure", // Duplicate with same workflow
			Type:     "bug",
			Priority: 3,
			Metadata: map[string]string{"workflow": "Tests"},
		},
		{
			Title:    "Fix build error",
			Type:     "bug",
			Priority: 1,
			Metadata: map[string]string{"workflow": "Build"},
		},
		{
			Title:    "Fix build error", // Duplicate with higher priority
			Type:     "bug",
			Priority: 0,
			Metadata: map[string]string{"workflow": "Build"},
		},
		{
			Title:    "Fix lint issues in file.go",
			Type:     "task",
			Priority: 2,
			Metadata: map[string]string{"file": "file.go"},
		},
	}

	deduplicated := creator.deduplicateBeads(beads)

	// Should have 3 unique beads (2 test duplicates merged, 2 build duplicates merged)
	if len(deduplicated) != 3 {
		t.Fatalf("Expected 3 unique beads, got %d", len(deduplicated))
	}

	// Check that higher priority version was kept for build error
	var buildBead *BeadInfo
	for _, bead := range deduplicated {
		if strings.Contains(bead.Title, "build") {
			buildBead = &bead
			break
		}
	}
	if buildBead == nil {
		t.Error("Build bead not found")
	} else if buildBead.Priority != 0 {
		t.Errorf("Build bead priority = %d, want 0 (higher priority)", buildBead.Priority)
	}
}

func TestGroupBeadsByType(t *testing.T) {
	creator := &BeadCreator{}

	beads := []BeadInfo{
		{Title: "Bug 1", Type: "bug"},
		{Title: "Task 1", Type: "task"},
		{Title: "Bug 2", Type: "bug"},
		{Title: "Feature 1", Type: "feature"},
		{Title: "Task 2", Type: "task"},
	}

	grouped := creator.GroupBeadsByType(beads)

	// Should have 3 groups
	if len(grouped) != 3 {
		t.Fatalf("Expected 3 groups, got %d", len(grouped))
	}

	// Check group sizes
	if len(grouped["bug"]) != 2 {
		t.Errorf("Bug group size = %d, want 2", len(grouped["bug"]))
	}
	if len(grouped["task"]) != 2 {
		t.Errorf("Task group size = %d, want 2", len(grouped["task"]))
	}
	if len(grouped["feature"]) != 1 {
		t.Errorf("Feature group size = %d, want 1", len(grouped["feature"]))
	}
}

func TestPrioritizeBeads(t *testing.T) {
	creator := &BeadCreator{}

	beads := []BeadInfo{
		{Title: "Low", Priority: 3},
		{Title: "Critical", Priority: 0},
		{Title: "Medium", Priority: 2},
		{Title: "High", Priority: 1},
		{Title: "Lowest", Priority: 4},
	}

	sorted := creator.PrioritizeBeads(beads)

	// Check that beads are sorted by priority (ascending)
	expectedOrder := []string{"Critical", "High", "Medium", "Low", "Lowest"}
	for i, expected := range expectedOrder {
		if sorted[i].Title != expected {
			t.Errorf("Sorted[%d].Title = %s, want %s", i, sorted[i].Title, expected)
		}
	}

	// Verify priorities are in ascending order
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Priority < sorted[i-1].Priority {
			t.Errorf("Beads not properly sorted at index %d", i)
		}
	}
}

func TestProcessPRAndCreateBeadInfo(t *testing.T) {
	// This is an integration test that requires mocking the processor
	// We'll create a minimal test to verify the flow

	processor := &FeedbackProcessor{
		client: &Client{},
		rules:  DefaultFeedbackRules(),
	}
	creator := NewBeadCreator(processor)
	ctx := context.Background()
	prURL := "https://github.com/user/repo/pull/123"
	rootIssueID := "beads-root"

	t.Run("Process PR and create bead info", func(t *testing.T) {
		// This would fail without mocked GitHub API
		t.Skip("Skipping test that requires GitHub API access")

		beadInfos, err := creator.ProcessPRAndCreateBeadInfo(ctx, prURL, rootIssueID)
		if err == nil {
			// Verify all bead infos have the correct parent
			for _, info := range beadInfos {
				if info.ParentID != rootIssueID {
					t.Errorf("BeadInfo.ParentID = %s, want %s", info.ParentID, rootIssueID)
				}
				// Verify all have the from-pr-feedback label
				hasLabel := false
				for _, label := range info.Labels {
					if label == "from-pr-feedback" {
						hasLabel = true
						break
					}
				}
				if !hasLabel {
					t.Error("BeadInfo missing 'from-pr-feedback' label")
				}
			}
		}
	})
}

func TestFormatDescriptionResolutionGuidance(t *testing.T) {
	creator := &BeadCreator{}

	tests := []struct {
		feedbackType     FeedbackType
		expectedGuidance string
	}{
		{FeedbackTypeTest, "Fix the failing tests and ensure all test suites pass"},
		{FeedbackTypeBuild, "Resolve the build errors and ensure the project compiles successfully"},
		{FeedbackTypeLint, "Fix the linting issues to meet code style requirements"},
		{FeedbackTypeSecurity, "Address the security vulnerability with appropriate fixes"},
		{FeedbackTypeReview, "Address the reviewer's feedback and update the code accordingly"},
		{FeedbackTypeCI, "Fix the CI pipeline failure and ensure all checks pass"},
		{FeedbackTypeGeneral, "Address the issue as described above"},
	}

	for _, tt := range tests {
		t.Run(string(tt.feedbackType), func(t *testing.T) {
			item := FeedbackItem{
				Type:        tt.feedbackType,
				Description: "Test description",
			}
			description := creator.formatDescription(item)
			if !strings.Contains(description, tt.expectedGuidance) {
				t.Errorf("Description for %v should contain '%s'", tt.feedbackType, tt.expectedGuidance)
			}
		})
	}
}

func TestContextFormatting(t *testing.T) {
	creator := &BeadCreator{}

	item := FeedbackItem{
		Type:        FeedbackTypeTest,
		Description: "Test",
		Source:      "Source",
		Context: map[string]string{
			"check_name":    "unit-tests",
			"workflow_name": "CI Pipeline",
			"file_path":     "/src/main.go",
		},
	}

	description := creator.formatDescription(item)

	// Check that context keys are properly formatted
	if !strings.Contains(description, "Check Name:") {
		t.Error("Context key 'check_name' should be formatted as 'Check Name:'")
	}
	if !strings.Contains(description, "Workflow Name:") {
		t.Error("Context key 'workflow_name' should be formatted as 'Workflow Name:'")
	}
	if !strings.Contains(description, "File Path:") {
		t.Error("Context key 'file_path' should be formatted as 'File Path:'")
	}
}