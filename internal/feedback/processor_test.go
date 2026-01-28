package feedback

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/github"
)

func TestNewFeedbackProcessor(t *testing.T) {
	client := &github.Client{}

	processor := NewFeedbackProcessor(client, 2)
	if processor == nil {
		t.Fatal("NewFeedbackProcessor returned nil")
	}
	if processor.minPriority != 2 {
		t.Errorf("minPriority = %d, want 2", processor.minPriority)
	}

	processor = NewFeedbackProcessor(client, 1)
	if processor.minPriority != 1 {
		t.Errorf("minPriority = %d, want 1", processor.minPriority)
	}
}

func TestCategorizeCheckFailure(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name     string
		check    string
		expected github.FeedbackType
	}{
		{"Test check", "unit-tests", github.FeedbackTypeTest},
		{"Test check uppercase", "Unit-Tests", github.FeedbackTypeTest},
		{"Lint check", "eslint", github.FeedbackTypeLint},
		{"Style check", "code-style", github.FeedbackTypeLint},
		{"Build check", "build-project", github.FeedbackTypeBuild},
		{"Compile check", "compile", github.FeedbackTypeBuild},
		{"Security check", "security-scan", github.FeedbackTypeSecurity},
		{"Vulnerability check", "vulnerability-scan", github.FeedbackTypeSecurity},
		{"Generic CI", "ci-check", github.FeedbackTypeCI},
		{"Unknown check", "something-else", github.FeedbackTypeCI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.categorizeCheckFailure(tt.check)
			if result != tt.expected {
				t.Errorf("categorizeCheckFailure(%s) = %v, want %v", tt.check, result, tt.expected)
			}
		})
	}
}

func TestCategorizeWorkflowFailure(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name          string
		workflowName  string
		failureDetail string
		expected      github.FeedbackType
	}{
		{"Test workflow", "Test Suite", "unit tests failed", github.FeedbackTypeTest},
		{"Lint workflow", "Linting", "eslint errors", github.FeedbackTypeLint},
		{"Format workflow", "Code Format", "formatting issues", github.FeedbackTypeLint},
		{"Build workflow", "Build", "compilation error", github.FeedbackTypeBuild},
		{"Security workflow", "Security Scan", "vulnerabilities found", github.FeedbackTypeSecurity},
		{"Generic CI", "CI Pipeline", "step failed", github.FeedbackTypeCI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.categorizeWorkflowFailure(tt.workflowName, tt.failureDetail)
			if result != tt.expected {
				t.Errorf("categorizeWorkflowFailure(%s, %s) = %v, want %v",
					tt.workflowName, tt.failureDetail, result, tt.expected)
			}
		})
	}
}

func TestGetPriorityForType(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		feedbackType github.FeedbackType
		expected     int
	}{
		{github.FeedbackTypeSecurity, 0}, // Critical
		{github.FeedbackTypeBuild, 1},    // High
		{github.FeedbackTypeCI, 1},       // High
		{github.FeedbackTypeTest, 2},     // Medium
		{github.FeedbackTypeLint, 2},     // Medium
		{github.FeedbackTypeReview, 2},   // Medium
		{github.FeedbackTypeGeneral, 3},  // Low
	}

	for _, tt := range tests {
		t.Run(string(tt.feedbackType), func(t *testing.T) {
			result := processor.getPriorityForType(tt.feedbackType)
			if result != tt.expected {
				t.Errorf("getPriorityForType(%v) = %d, want %d", tt.feedbackType, result, tt.expected)
			}
		})
	}
}

func TestIsActionableComment(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name       string
		body       string
		actionable bool
	}{
		{"Please request", "Please update the documentation", true},
		{"Should request", "This should be refactored", true},
		{"Must request", "You must fix this issue", true},
		{"Need to request", "We need to address this", true},
		{"Fix request", "Fix the broken test", true},
		{"TODO comment", "TODO: implement this feature", true},
		{"FIXME comment", "FIXME: memory leak here", true},
		{"Error mention", "ERROR: compilation failed", true},
		{"Failed mention", "The test failed", true},
		{"Non-actionable", "Looks good to me!", false},
		{"Simple comment", "Thanks for the PR", false},
		{"Empty comment", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.isActionableComment(tt.body)
			if result != tt.actionable {
				t.Errorf("isActionableComment(%s) = %v, want %v", tt.body, result, tt.actionable)
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name     string
		text     string
		maxLen   int
		expected string
	}{
		{"Short text", "Hello", 10, "Hello"},
		{"Exact length", "Hello", 5, "Hello"},
		{"Long text", "Hello, World!", 5, "Hello..."},
		{"Empty text", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.truncateText(tt.text, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateText(%s, %d) = %s, want %s", tt.text, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestProcessStatusChecks(t *testing.T) {
	processor := &FeedbackProcessor{
		minPriority: 2,
	}

	status := &github.PRStatus{
		StatusChecks: []github.StatusCheck{
			{
				Context:     "unit-tests",
				State:       "FAILURE",
				Description: "Unit tests failed",
				TargetURL:   "https://example.com/checks/1",
			},
			{
				Context:     "lint",
				State:       "ERROR",
				Description: "Linting errors found",
				TargetURL:   "https://example.com/checks/2",
			},
			{
				Context:     "build",
				State:       "SUCCESS",
				Description: "Build passed",
				TargetURL:   "https://example.com/checks/3",
			},
		},
	}

	items := processor.processStatusChecks(status)

	// Should have 2 items (the two failures)
	if len(items) != 2 {
		t.Fatalf("Expected 2 feedback items, got %d", len(items))
	}

	// Check first item (unit-tests failure)
	if items[0].Type != github.FeedbackTypeTest {
		t.Errorf("First item type = %v, want %v", items[0].Type, github.FeedbackTypeTest)
	}
	if items[0].Title != "Fix unit-tests failure" {
		t.Errorf("First item title = %s, want 'Fix unit-tests failure'", items[0].Title)
	}
	if !items[0].Actionable {
		t.Error("First item should be actionable")
	}

	// Check second item (lint error)
	if items[1].Type != github.FeedbackTypeLint {
		t.Errorf("Second item type = %v, want %v", items[1].Type, github.FeedbackTypeLint)
	}
	if items[1].Title != "Fix lint failure" {
		t.Errorf("Second item title = %s, want 'Fix lint failure'", items[1].Title)
	}
}

func TestProcessWorkflowRuns(t *testing.T) {
	processor := &FeedbackProcessor{
		client:      &github.Client{},
		minPriority: 2,
	}

	status := &github.PRStatus{
		Workflows: []github.WorkflowRun{
			{
				ID:         123,
				Name:       "Test Suite",
				Status:     "completed",
				Conclusion: "failure",
				URL:        "https://example.com/runs/123",
				Jobs: []github.Job{
					{
						ID:         456,
						Name:       "Unit Tests",
						Conclusion: "failure",
						URL:        "https://example.com/jobs/456",
						Steps: []github.Step{
							{Name: "Run tests", Conclusion: "failure"},
						},
					},
				},
			},
			{
				ID:         124,
				Name:       "Build",
				Status:     "completed",
				Conclusion: "success",
				URL:        "https://example.com/runs/124",
			},
		},
	}

	// Note: This test will use generic fallback since GetJobLogs will fail
	// (we're not mocking the GitHub API). The test verifies the fallback works.
	ctx := context.Background()
	items := processor.processWorkflowRuns(ctx, "owner/repo", status)

	// Should have 1 item (the failed workflow with generic fallback)
	if len(items) != 1 {
		t.Fatalf("Expected 1 feedback item, got %d", len(items))
	}

	if items[0].Type != github.FeedbackTypeTest {
		t.Errorf("Item type = %v, want %v", items[0].Type, github.FeedbackTypeTest)
	}
	// Generic fallback format: "Fix {jobName}: {stepName} in {workflowName}"
	if items[0].Title != "Fix Unit Tests: Run tests in Test Suite" {
		t.Errorf("Item title = %s, want 'Fix Unit Tests: Run tests in Test Suite'", items[0].Title)
	}
	if !items[0].Actionable {
		t.Error("Item should be actionable")
	}
}

func TestProcessReviews(t *testing.T) {
	processor := &FeedbackProcessor{
		minPriority: 2,
	}

	status := &github.PRStatus{
		URL: "https://github.com/user/repo/pull/123",
		Reviews: []github.Review{
			{
				ID:     1,
				State:  "CHANGES_REQUESTED",
				Body:   "Please fix these issues",
				Author: "reviewer1",
			},
			{
				ID:     2,
				State:  "APPROVED",
				Body:   "LGTM",
				Author: "reviewer2",
			},
			{
				ID:     3,
				State:  "COMMENTED",
				Body:   "Some comments",
				Author: "reviewer3",
				Comments: []github.ReviewComment{
					{
						Path:   "file.go",
						Line:   42,
						Body:   "This needs to be fixed",
						Author: "reviewer3",
					},
				},
			},
		},
	}

	items := processor.processReviews(status)

	// Should have 2 items (CHANGES_REQUESTED and the actionable comment)
	if len(items) != 2 {
		t.Fatalf("Expected 2 feedback items, got %d", len(items))
	}

	// Check first item (CHANGES_REQUESTED)
	if items[0].Type != github.FeedbackTypeReview {
		t.Errorf("First item type = %v, want %v", items[0].Type, github.FeedbackTypeReview)
	}
	if items[0].Title != "Address review feedback from reviewer1" {
		t.Errorf("First item title = %s", items[0].Title)
	}
	if items[0].Priority != 1 {
		t.Errorf("First item priority = %d, want 1", items[0].Priority)
	}

	// Check second item (actionable comment)
	if items[1].Type != github.FeedbackTypeReview {
		t.Errorf("Second item type = %v, want %v", items[1].Type, github.FeedbackTypeReview)
	}
	if items[1].Priority != 2 {
		t.Errorf("Second item priority = %d, want 2", items[1].Priority)
	}
}

func TestFilterByMinimumPriority(t *testing.T) {
	processor := &FeedbackProcessor{
		client:      &github.Client{},
		minPriority: 2, // Only priority 0, 1, 2
	}

	// Create mock feedback items with different priorities
	items := []github.FeedbackItem{
		{Title: "Critical", Priority: 0, Actionable: true},
		{Title: "High", Priority: 1, Actionable: true},
		{Title: "Medium", Priority: 2, Actionable: true},
		{Title: "Low", Priority: 3, Actionable: true},
		{Title: "Lowest", Priority: 4, Actionable: true},
		{Title: "Not actionable", Priority: 0, Actionable: false},
	}

	// Simulate the filtering logic from ProcessPRFeedback
	filtered := make([]github.FeedbackItem, 0, len(items))
	for _, item := range items {
		if item.Priority <= processor.minPriority && item.Actionable {
			filtered = append(filtered, item)
		}
	}

	// Should have 3 items (priorities 0, 1, 2 that are actionable)
	if len(filtered) != 3 {
		t.Fatalf("Expected 3 filtered items, got %d", len(filtered))
	}

	expectedTitles := []string{"Critical", "High", "Medium"}
	for i, title := range expectedTitles {
		if filtered[i].Title != title {
			t.Errorf("Item %d title = %s, want %s", i, filtered[i].Title, title)
		}
	}
}

func TestCreateGenericFailureItem(t *testing.T) {
	processor := &FeedbackProcessor{
		client:      &github.Client{},
		minPriority: 2,
	}

	workflow := github.WorkflowRun{
		ID:   123,
		Name: "CI Pipeline",
		URL:  "https://example.com/runs/123",
	}

	tests := []struct {
		name          string
		job           github.Job
		expectedTitle string
	}{
		{
			name: "Job with failed step",
			job: github.Job{
				ID:         456,
				Name:       "Test",
				Conclusion: "failure",
				URL:        "https://example.com/jobs/456",
				Steps: []github.Step{
					{Name: "Setup", Conclusion: "success"},
					{Name: "Run tests", Conclusion: "failure"},
				},
			},
			expectedTitle: "Fix Test: Run tests in CI Pipeline",
		},
		{
			name: "Job without specific failed step",
			job: github.Job{
				ID:         789,
				Name:       "Lint",
				Conclusion: "failure",
				URL:        "https://example.com/jobs/789",
				Steps:      []github.Step{},
			},
			expectedTitle: "Fix Lint in CI Pipeline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := processor.createGenericFailureItem(workflow, tt.job)
			if item.Title != tt.expectedTitle {
				t.Errorf("createGenericFailureItem().Title = %s, want %s", item.Title, tt.expectedTitle)
			}
			if !item.Actionable {
				t.Error("Item should be actionable")
			}
		})
	}
}

func TestCategorizeComment(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name     string
		comment  github.Comment
		expected github.FeedbackType
	}{
		{
			name: "Security bot comment",
			comment: github.Comment{
				Author: "security-bot",
				Body:   "Found security vulnerability in dependencies",
			},
			expected: github.FeedbackTypeSecurity,
		},
		{
			name: "Test bot comment",
			comment: github.Comment{
				Author: "ci[bot]",
				Body:   "Test failures detected in unit tests",
			},
			expected: github.FeedbackTypeTest,
		},
		{
			name: "Lint bot comment",
			comment: github.Comment{
				Author: "linter-bot",
				Body:   "Lint errors found in files",
			},
			expected: github.FeedbackTypeLint,
		},
		{
			name: "Human comment",
			comment: github.Comment{
				Author: "user123",
				Body:   "Please fix this issue",
			},
			expected: github.FeedbackTypeGeneral,
		},
		{
			name: "Generic bot comment",
			comment: github.Comment{
				Author: "github-bot",
				Body:   "Automated message",
			},
			expected: github.FeedbackTypeGeneral,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.categorizeComment(tt.comment)
			if result != tt.expected {
				t.Errorf("categorizeComment(%+v) = %v, want %v",
					tt.comment, result, tt.expected)
			}
		})
	}
}

func TestExtractTitleFromComment(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "Short comment",
			body:     "Fix this issue",
			expected: "Fix this issue",
		},
		{
			name:     "Multi-line comment",
			body:     "Fix this issue\nHere are more details\nAnd even more",
			expected: "Fix this issue",
		},
		{
			name:     "Long first line",
			body:     "This is a very long comment that exceeds one hundred characters and should be truncated properly to fit within the limit we have set for titles",
			expected: "This is a very long comment that exceeds one hundred characters and should be truncated properly to ...",
		},
		{
			name:     "Empty comment",
			body:     "",
			expected: "Address comment feedback",
		},
		{
			name:     "Whitespace only",
			body:     "   \n  \n  ",
			expected: "Address comment feedback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.extractTitleFromComment(tt.body)
			if result != tt.expected {
				t.Errorf("extractTitleFromComment(%s) = %s, want %s",
					tt.body, result, tt.expected)
			}
		})
	}
}

func TestProcessComments(t *testing.T) {
	processor := &FeedbackProcessor{
		minPriority: 2,
	}

	status := &github.PRStatus{
		URL: "https://github.com/user/repo/pull/123",
		Comments: []github.Comment{
			{
				ID:     1,
				Author: "security-bot",
				Body:   "Security vulnerability detected: SQL injection risk",
			},
			{
				ID:     2,
				Author: "user123",
				Body:   "Thanks for the PR!",
			},
			{
				ID:     3,
				Author: "ci[bot]",
				Body:   "Test failure: 5 tests failed",
			},
		},
	}

	items := processor.processComments(status)

	// Should have 2 actionable items (security and test failure)
	if len(items) != 2 {
		t.Fatalf("Expected 2 feedback items, got %d", len(items))
	}

	// First should be security (higher priority)
	if items[0].Type != github.FeedbackTypeSecurity {
		t.Errorf("First item type = %v, want %v", items[0].Type, github.FeedbackTypeSecurity)
	}

	// Second should be test failure
	if items[1].Type != github.FeedbackTypeTest {
		t.Errorf("Second item type = %v, want %v", items[1].Type, github.FeedbackTypeTest)
	}
}

func TestProcessConflicts(t *testing.T) {
	processor := &FeedbackProcessor{
		minPriority: 2,
	}

	tests := []struct {
		name           string
		mergeableState string
		expectItems    int
	}{
		{"DIRTY state returns conflict item", db.MergeableStateDirty, 1},
		{"CLEAN state returns empty", db.MergeableStateClean, 0},
		{"BLOCKED state returns empty", db.MergeableStateBlocked, 0},
		{"UNSTABLE state returns empty", db.MergeableStateUnstable, 0},
		{"Empty state returns empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &github.PRStatus{
				URL:            "https://github.com/user/repo/pull/123",
				MergeableState: tt.mergeableState,
			}

			items := processor.processConflicts(status)

			if len(items) != tt.expectItems {
				t.Errorf("processConflicts() returned %d items, want %d", len(items), tt.expectItems)
			}

			if tt.expectItems > 0 {
				item := items[0]
				if item.Type != github.FeedbackTypeConflict {
					t.Errorf("Item type = %v, want %v", item.Type, github.FeedbackTypeConflict)
				}
				if item.Title != "Resolve merge conflicts with main" {
					t.Errorf("Item title = %s, want 'Resolve merge conflicts with main'", item.Title)
				}
				if item.Priority != 1 {
					t.Errorf("Item priority = %d, want 1", item.Priority)
				}
				if !item.Actionable {
					t.Error("Item should be actionable")
				}
				if item.Source.ID != "merge-conflict" {
					t.Errorf("Item source ID = %s, want 'merge-conflict'", item.Source.ID)
				}
			}
		})
	}
}

func TestGetPriorityForConflictType(t *testing.T) {
	processor := &FeedbackProcessor{}

	result := processor.getPriorityForType(github.FeedbackTypeConflict)
	if result != 1 {
		t.Errorf("getPriorityForType(FeedbackTypeConflict) = %d, want 1", result)
	}
}

func TestNewFeedbackProcessorWithProject(t *testing.T) {
	client := &github.Client{}

	t.Run("with nil project", func(t *testing.T) {
		processor := NewFeedbackProcessorWithProject(client, 2, nil, "work-123")
		if processor == nil {
			t.Fatal("NewFeedbackProcessorWithProject returned nil")
		}
		if processor.minPriority != 2 {
			t.Errorf("minPriority = %d, want 2", processor.minPriority)
		}
		if processor.proj != nil {
			t.Error("Expected proj to be nil")
		}
		if processor.workID != "work-123" {
			t.Errorf("workID = %s, want work-123", processor.workID)
		}
	})

	t.Run("stores all parameters", func(t *testing.T) {
		// Can't test with real project, but we can verify struct fields are set
		processor := NewFeedbackProcessorWithProject(client, 1, nil, "w-abc")
		if processor.client != client {
			t.Error("Expected client to be set")
		}
		if processor.minPriority != 1 {
			t.Errorf("minPriority = %d, want 1", processor.minPriority)
		}
		if processor.workID != "w-abc" {
			t.Errorf("workID = %s, want w-abc", processor.workID)
		}
	})
}

func TestShouldUseClaude(t *testing.T) {
	client := &github.Client{}

	t.Run("returns false when project is nil", func(t *testing.T) {
		processor := NewFeedbackProcessorWithProject(client, 2, nil, "work-123")
		if processor.shouldUseClaude() {
			t.Error("Expected shouldUseClaude() to return false when project is nil")
		}
	})

	t.Run("returns false with basic processor", func(t *testing.T) {
		processor := NewFeedbackProcessor(client, 2)
		if processor.shouldUseClaude() {
			t.Error("Expected shouldUseClaude() to return false for basic processor")
		}
	})
}

func TestTruncateLogContent(t *testing.T) {
	tests := []struct {
		name     string
		logs     string
		maxBytes int
		expected string
	}{
		{
			name:     "short log under limit",
			logs:     "short log",
			maxBytes: 100,
			expected: "short log",
		},
		{
			name:     "log exactly at limit",
			logs:     "exact",
			maxBytes: 5,
			expected: "exact",
		},
		{
			name:     "long log truncated to last N bytes",
			logs:     "beginning middle end",
			maxBytes: 10,
			expected: "middle end",
		},
		{
			name:     "empty log",
			logs:     "",
			maxBytes: 100,
			expected: "",
		},
		{
			name:     "truncate to single byte",
			logs:     "hello",
			maxBytes: 1,
			expected: "o",
		},
		{
			name:     "preserves error at end of log",
			logs:     "INFO: Starting build\nINFO: Compiling\nERROR: Test failed at line 42",
			maxBytes: 30,
			expected: "\nERROR: Test failed at line 42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateLogContent(tt.logs, tt.maxBytes)
			if result != tt.expected {
				t.Errorf("truncateLogContent(%q, %d) = %q, want %q",
					tt.logs, tt.maxBytes, result, tt.expected)
			}
		})
	}
}
