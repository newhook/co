package claude

import (
	"strings"
	"testing"
)

func TestBuildLogAnalysisPrompt(t *testing.T) {
	tests := []struct {
		name           string
		params         LogAnalysisParams
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "Full parameters",
			params: LogAnalysisParams{
				TaskID:       "w-abc.5",
				WorkID:       "w-abc",
				BranchName:   "feature/test-branch",
				RootIssueID:  "beads-123",
				WorkflowName: "CI Pipeline",
				JobName:      "Unit Tests",
				LogContent:   "--- FAIL: TestSomething (0.02s)",
			},
			wantContains: []string{
				"Work w-abc",
				"Branch: feature/test-branch",
				"Job: Unit Tests",
				"Workflow: CI Pipeline",
				"--- FAIL: TestSomething (0.02s)",
				"co complete w-abc.5",
				"--parent beads-123",
			},
		},
		{
			name: "Without root issue ID",
			params: LogAnalysisParams{
				TaskID:       "w-xyz.1",
				WorkID:       "w-xyz",
				BranchName:   "main",
				RootIssueID:  "",
				WorkflowName: "Build",
				JobName:      "Compile",
				LogContent:   "compilation error: undefined reference",
			},
			wantContains: []string{
				"Work w-xyz",
				"Branch: main",
				"Job: Compile",
				"Workflow: Build",
				"compilation error: undefined reference",
				"co complete w-xyz.1",
			},
			wantNotContain: []string{
				"--parent",
			},
		},
		{
			name: "Empty log content",
			params: LogAnalysisParams{
				TaskID:       "w-test.2",
				WorkID:       "w-test",
				BranchName:   "dev",
				RootIssueID:  "beads-456",
				WorkflowName: "Tests",
				JobName:      "Integration",
				LogContent:   "",
			},
			wantContains: []string{
				"Work w-test",
				"Job: Integration",
				"--- CI Log Output ---",
			},
		},
		{
			name: "Multiline log content",
			params: LogAnalysisParams{
				TaskID:       "w-multi.3",
				WorkID:       "w-multi",
				BranchName:   "feature/multiline",
				RootIssueID:  "",
				WorkflowName: "CI",
				JobName:      "Test",
				LogContent: `FAIL internal/auth/user_test.go:145
    Error: Expected token to be valid
    Got: token expired
--- FAIL: TestUserAuth (0.03s)`,
			},
			wantContains: []string{
				"internal/auth/user_test.go:145",
				"Error: Expected token to be valid",
				"--- FAIL: TestUserAuth (0.03s)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildLogAnalysisPrompt(tt.params)

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("BuildLogAnalysisPrompt() missing expected content: %q\n\nGot:\n%s", want, result)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if strings.Contains(result, notWant) {
					t.Errorf("BuildLogAnalysisPrompt() contains unexpected content: %q\n\nGot:\n%s", notWant, result)
				}
			}
		})
	}
}

func TestLogAnalysisParams(t *testing.T) {
	// Test that the struct can be properly initialized
	params := LogAnalysisParams{
		TaskID:       "task-1",
		WorkID:       "work-1",
		BranchName:   "main",
		RootIssueID:  "issue-1",
		WorkflowName: "workflow-1",
		JobName:      "job-1",
		LogContent:   "content",
	}

	if params.TaskID != "task-1" {
		t.Errorf("TaskID = %s, want task-1", params.TaskID)
	}
	if params.WorkID != "work-1" {
		t.Errorf("WorkID = %s, want work-1", params.WorkID)
	}
	if params.BranchName != "main" {
		t.Errorf("BranchName = %s, want main", params.BranchName)
	}
	if params.RootIssueID != "issue-1" {
		t.Errorf("RootIssueID = %s, want issue-1", params.RootIssueID)
	}
	if params.WorkflowName != "workflow-1" {
		t.Errorf("WorkflowName = %s, want workflow-1", params.WorkflowName)
	}
	if params.JobName != "job-1" {
		t.Errorf("JobName = %s, want job-1", params.JobName)
	}
	if params.LogContent != "content" {
		t.Errorf("LogContent = %s, want content", params.LogContent)
	}
}

func TestBuildLogAnalysisPromptPriorityGuidelines(t *testing.T) {
	// Verify the prompt includes priority guidelines
	params := LogAnalysisParams{
		TaskID:       "w-test.1",
		WorkID:       "w-test",
		BranchName:   "main",
		WorkflowName: "CI",
		JobName:      "Test",
		LogContent:   "test failure",
	}

	result := BuildLogAnalysisPrompt(params)

	// Check that priority guidelines are included
	priorities := []string{
		"P0",
		"P1",
		"P2",
		"P3",
	}

	for _, p := range priorities {
		if !strings.Contains(result, p) {
			t.Errorf("BuildLogAnalysisPrompt() missing priority guideline: %s", p)
		}
	}
}

func TestBuildLogAnalysisPromptBdCreateCommand(t *testing.T) {
	// Verify the prompt includes bd create command examples
	params := LogAnalysisParams{
		TaskID:       "w-test.1",
		WorkID:       "w-test",
		BranchName:   "main",
		WorkflowName: "CI",
		JobName:      "Test",
		LogContent:   "test failure",
	}

	result := BuildLogAnalysisPrompt(params)

	// Check that bd create command format is included
	if !strings.Contains(result, "bd create") {
		t.Error("BuildLogAnalysisPrompt() missing bd create command")
	}

	// Check that it includes type options
	if !strings.Contains(result, "--type") {
		t.Error("BuildLogAnalysisPrompt() missing --type flag")
	}

	// Check that it includes priority option
	if !strings.Contains(result, "--priority") {
		t.Error("BuildLogAnalysisPrompt() missing --priority flag")
	}
}
