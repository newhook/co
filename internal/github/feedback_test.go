package github

import (
	"reflect"
	"testing"
)

func TestDefaultFeedbackRules(t *testing.T) {
	rules := DefaultFeedbackRules()

	if rules == nil {
		t.Fatal("DefaultFeedbackRules returned nil")
	}

	if !rules.CreateBeadForFailedChecks {
		t.Error("CreateBeadForFailedChecks should be true by default")
	}

	if !rules.CreateBeadForTestFailures {
		t.Error("CreateBeadForTestFailures should be true by default")
	}

	if !rules.CreateBeadForLintErrors {
		t.Error("CreateBeadForLintErrors should be true by default")
	}

	if !rules.CreateBeadForReviewComments {
		t.Error("CreateBeadForReviewComments should be true by default")
	}

	if !rules.IgnoreDraftPRs {
		t.Error("IgnoreDraftPRs should be true by default")
	}

	if rules.MinimumPriority != 2 {
		t.Errorf("MinimumPriority should be 2, got %d", rules.MinimumPriority)
	}
}

func TestNewFeedbackProcessor(t *testing.T) {
	client := &Client{}

	// Test with nil rules (should use defaults)
	processor := NewFeedbackProcessor(client, nil)
	if processor == nil {
		t.Fatal("NewFeedbackProcessor returned nil")
	}
	if processor.rules == nil {
		t.Error("processor.rules should not be nil")
	}

	// Test with custom rules
	customRules := &FeedbackRules{
		CreateBeadForFailedChecks: false,
		MinimumPriority:          1,
	}
	processor = NewFeedbackProcessor(client, customRules)
	if processor.rules.CreateBeadForFailedChecks {
		t.Error("Custom rules not applied")
	}
	if processor.rules.MinimumPriority != 1 {
		t.Errorf("Custom MinimumPriority not applied, got %d", processor.rules.MinimumPriority)
	}
}

func TestCategorizeCheckFailure(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name     string
		check    string
		expected FeedbackType
	}{
		{"Test check", "unit-tests", FeedbackTypeTest},
		{"Test check uppercase", "Unit-Tests", FeedbackTypeTest},
		{"Lint check", "eslint", FeedbackTypeLint},
		{"Style check", "code-style", FeedbackTypeLint},
		{"Build check", "build-project", FeedbackTypeBuild},
		{"Compile check", "compile", FeedbackTypeBuild},
		{"Security check", "security-scan", FeedbackTypeSecurity},
		{"Vulnerability check", "vulnerability-scan", FeedbackTypeSecurity},
		{"Generic CI", "ci-check", FeedbackTypeCI},
		{"Unknown check", "something-else", FeedbackTypeCI},
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
		name           string
		workflowName   string
		failureDetail  string
		expected       FeedbackType
	}{
		{"Test workflow", "Test Suite", "unit tests failed", FeedbackTypeTest},
		{"Lint workflow", "Linting", "eslint errors", FeedbackTypeLint},
		{"Format workflow", "Code Format", "formatting issues", FeedbackTypeLint},
		{"Build workflow", "Build", "compilation error", FeedbackTypeBuild},
		{"Security workflow", "Security Scan", "vulnerabilities found", FeedbackTypeSecurity},
		{"Generic CI", "CI Pipeline", "step failed", FeedbackTypeCI},
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
		feedbackType FeedbackType
		expected     int
	}{
		{FeedbackTypeSecurity, 0},  // Critical
		{FeedbackTypeBuild, 1},      // High
		{FeedbackTypeCI, 1},         // High
		{FeedbackTypeTest, 2},       // Medium
		{FeedbackTypeLint, 2},       // Medium
		{FeedbackTypeReview, 2},     // Medium
		{FeedbackTypeGeneral, 3},    // Low
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

func TestGetFileName(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"Simple file", "file.go", "file.go"},
		{"Path with folders", "src/pkg/file.go", "file.go"},
		{"Deep path", "a/b/c/d/file.go", "file.go"},
		{"Empty path", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.getFileName(tt.path)
			if result != tt.expected {
				t.Errorf("getFileName(%s) = %s, want %s", tt.path, result, tt.expected)
			}
		})
	}
}

func TestProcessStatusChecks(t *testing.T) {
	processor := &FeedbackProcessor{
		rules: DefaultFeedbackRules(),
	}

	status := &PRStatus{
		StatusChecks: []StatusCheck{
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
	if items[0].Type != FeedbackTypeTest {
		t.Errorf("First item type = %v, want %v", items[0].Type, FeedbackTypeTest)
	}
	if items[0].Title != "Fix unit-tests failure" {
		t.Errorf("First item title = %s, want 'Fix unit-tests failure'", items[0].Title)
	}
	if !items[0].Actionable {
		t.Error("First item should be actionable")
	}

	// Check second item (lint error)
	if items[1].Type != FeedbackTypeLint {
		t.Errorf("Second item type = %v, want %v", items[1].Type, FeedbackTypeLint)
	}
	if items[1].Title != "Fix lint failure" {
		t.Errorf("Second item title = %s, want 'Fix lint failure'", items[1].Title)
	}
}

func TestProcessWorkflowRuns(t *testing.T) {
	processor := &FeedbackProcessor{
		rules: DefaultFeedbackRules(),
	}

	status := &PRStatus{
		Workflows: []WorkflowRun{
			{
				ID:         123,
				Name:       "Test Suite",
				Status:     "completed",
				Conclusion: "failure",
				URL:        "https://example.com/runs/123",
				Jobs: []Job{
					{
						Name:       "Unit Tests",
						Conclusion: "failure",
						Steps: []Step{
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

	items := processor.processWorkflowRuns(status)

	// Should have 1 item (the failed workflow)
	if len(items) != 1 {
		t.Fatalf("Expected 1 feedback item, got %d", len(items))
	}

	if items[0].Type != FeedbackTypeTest {
		t.Errorf("Item type = %v, want %v", items[0].Type, FeedbackTypeTest)
	}
	if items[0].Title != "Fix Unit Tests: Run tests in Test Suite" {
		t.Errorf("Item title = %s, want specific title", items[0].Title)
	}
	if !items[0].Actionable {
		t.Error("Item should be actionable")
	}
}

func TestProcessReviews(t *testing.T) {
	processor := &FeedbackProcessor{
		rules: DefaultFeedbackRules(),
	}

	status := &PRStatus{
		URL: "https://github.com/user/repo/pull/123",
		Reviews: []Review{
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
				Comments: []ReviewComment{
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
	if items[0].Type != FeedbackTypeReview {
		t.Errorf("First item type = %v, want %v", items[0].Type, FeedbackTypeReview)
	}
	if items[0].Title != "Address review feedback from reviewer1" {
		t.Errorf("First item title = %s", items[0].Title)
	}
	if items[0].Priority != 1 {
		t.Errorf("First item priority = %d, want 1", items[0].Priority)
	}

	// Check second item (actionable comment)
	if items[1].Type != FeedbackTypeReview {
		t.Errorf("Second item type = %v, want %v", items[1].Type, FeedbackTypeReview)
	}
	if items[1].Priority != 2 {
		t.Errorf("Second item priority = %d, want 2", items[1].Priority)
	}
}

func TestFilterByMinimumPriority(t *testing.T) {
	processor := &FeedbackProcessor{
		client: &Client{},
		rules: &FeedbackRules{
			CreateBeadForFailedChecks:    true,
			CreateBeadForTestFailures:    true,
			CreateBeadForLintErrors:      true,
			CreateBeadForReviewComments:  true,
			IgnoreDraftPRs:               false,
			MinimumPriority:              2, // Only priority 0, 1, 2
		},
	}

	// Create mock feedback items with different priorities
	items := []FeedbackItem{
		{Title: "Critical", Priority: 0, Actionable: true},
		{Title: "High", Priority: 1, Actionable: true},
		{Title: "Medium", Priority: 2, Actionable: true},
		{Title: "Low", Priority: 3, Actionable: true},
		{Title: "Lowest", Priority: 4, Actionable: true},
		{Title: "Not actionable", Priority: 0, Actionable: false},
	}

	// Simulate the filtering logic from ProcessPRFeedback
	filtered := make([]FeedbackItem, 0, len(items))
	for _, item := range items {
		if item.Priority <= processor.rules.MinimumPriority && item.Actionable {
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

func TestShouldCreateBeadForWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		rules        *FeedbackRules
		feedbackType FeedbackType
		expected     bool
	}{
		{
			name: "Test failure with test beads enabled",
			rules: &FeedbackRules{
				CreateBeadForTestFailures:  true,
				CreateBeadForFailedChecks:  false,
				CreateBeadForLintErrors:    false,
			},
			feedbackType: FeedbackTypeTest,
			expected:     true,
		},
		{
			name: "Test failure with test beads disabled",
			rules: &FeedbackRules{
				CreateBeadForTestFailures:  false,
			},
			feedbackType: FeedbackTypeTest,
			expected:     false,
		},
		{
			name: "Lint error with lint beads enabled",
			rules: &FeedbackRules{
				CreateBeadForLintErrors:    true,
			},
			feedbackType: FeedbackTypeLint,
			expected:     true,
		},
		{
			name: "Build failure with failed checks enabled",
			rules: &FeedbackRules{
				CreateBeadForFailedChecks:  true,
			},
			feedbackType: FeedbackTypeBuild,
			expected:     true,
		},
		{
			name: "Security issue (always created)",
			rules: &FeedbackRules{
				CreateBeadForFailedChecks:  false,
				CreateBeadForTestFailures:  false,
				CreateBeadForLintErrors:    false,
			},
			feedbackType: FeedbackTypeSecurity,
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := &FeedbackProcessor{rules: tt.rules}
			result := processor.shouldCreateBeadForWorkflow(tt.feedbackType)
			if result != tt.expected {
				t.Errorf("shouldCreateBeadForWorkflow(%v) = %v, want %v",
					tt.feedbackType, result, tt.expected)
			}
		})
	}
}

func TestExtractWorkflowFailures(t *testing.T) {
	processor := &FeedbackProcessor{}

	workflow := WorkflowRun{
		Name: "CI Pipeline",
		Jobs: []Job{
			{
				Name:       "Build",
				Conclusion: "success",
			},
			{
				Name:       "Test",
				Conclusion: "failure",
				Steps: []Step{
					{Name: "Setup", Conclusion: "success"},
					{Name: "Run tests", Conclusion: "failure"},
				},
			},
			{
				Name:       "Lint",
				Conclusion: "failure",
				Steps: []Step{}, // No specific step failed
			},
		},
	}

	failures := processor.extractWorkflowFailures(workflow)

	// Should have 2 failures
	if len(failures) != 2 {
		t.Fatalf("Expected 2 failures, got %d", len(failures))
	}

	expected := []string{"Test: Run tests", "Lint"}
	if !reflect.DeepEqual(failures, expected) {
		t.Errorf("extractWorkflowFailures() = %v, want %v", failures, expected)
	}
}

func TestCategorizeComment(t *testing.T) {
	processor := &FeedbackProcessor{}

	tests := []struct {
		name     string
		comment  Comment
		expected FeedbackType
	}{
		{
			name: "Security bot comment",
			comment: Comment{
				Author: "security-bot",
				Body:   "Found security vulnerability in dependencies",
			},
			expected: FeedbackTypeSecurity,
		},
		{
			name: "Test bot comment",
			comment: Comment{
				Author: "ci[bot]",
				Body:   "Test failures detected in unit tests",
			},
			expected: FeedbackTypeTest,
		},
		{
			name: "Lint bot comment",
			comment: Comment{
				Author: "linter-bot",
				Body:   "Lint errors found in files",
			},
			expected: FeedbackTypeLint,
		},
		{
			name: "Human comment",
			comment: Comment{
				Author: "user123",
				Body:   "Please fix this issue",
			},
			expected: FeedbackTypeGeneral,
		},
		{
			name: "Generic bot comment",
			comment: Comment{
				Author: "github-bot",
				Body:   "Automated message",
			},
			expected: FeedbackTypeGeneral,
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
		rules: DefaultFeedbackRules(),
	}

	status := &PRStatus{
		URL: "https://github.com/user/repo/pull/123",
		Comments: []Comment{
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
	if items[0].Type != FeedbackTypeSecurity {
		t.Errorf("First item type = %v, want %v", items[0].Type, FeedbackTypeSecurity)
	}

	// Second should be test failure
	if items[1].Type != FeedbackTypeTest {
		t.Errorf("Second item type = %v, want %v", items[1].Type, FeedbackTypeTest)
	}
}