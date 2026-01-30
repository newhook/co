package work

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/testutil"
)

func TestNewPRImporter(t *testing.T) {
	client := &testutil.GitHubClientMock{}
	importer := NewPRImporter(client)

	if importer == nil {
		t.Fatal("NewPRImporter returned nil")
	}
	if importer.client != client {
		t.Error("client not set correctly")
	}
	if importer.gitOps == nil {
		t.Error("gitOps should be initialized")
	}
	if importer.worktreeOps == nil {
		t.Error("worktreeOps should be initialized")
	}
}

func TestNewPRImporterWithOps(t *testing.T) {
	client := &testutil.GitHubClientMock{}
	gitOps := &testutil.GitOperationsMock{}
	worktreeOps := &testutil.WorktreeOperationsMock{}

	importer := NewPRImporterWithOps(client, gitOps, worktreeOps)

	if importer == nil {
		t.Fatal("NewPRImporterWithOps returned nil")
	}
	if importer.client != client {
		t.Error("client not set correctly")
	}
	if importer.gitOps == nil {
		t.Error("gitOps should be set")
	}
	if importer.worktreeOps == nil {
		t.Error("worktreeOps should be set")
	}
}

func TestSetupWorktreeFromPR_Success(t *testing.T) {
	ctx := context.Background()

	metadata := &github.PRMetadata{
		Number:      123,
		URL:         "https://github.com/owner/repo/pull/123",
		Title:       "Test PR",
		HeadRefName: "feature-branch",
		BaseRefName: "main",
	}

	client := &testutil.GitHubClientMock{
		GetPRMetadataFunc: func(ctx context.Context, prURLOrNumber string, repo string) (*github.PRMetadata, error) {
			return metadata, nil
		},
	}

	fetchPRRefCalled := false
	gitOps := &testutil.GitOperationsMock{
		FetchPRRefFunc: func(ctx context.Context, repoPath string, prNumber int, localBranch string) error {
			fetchPRRefCalled = true
			if prNumber != 123 {
				t.Errorf("expected PR number 123, got %d", prNumber)
			}
			if localBranch != "feature-branch" {
				t.Errorf("expected branch 'feature-branch', got %s", localBranch)
			}
			return nil
		},
	}

	createFromExistingCalled := false
	worktreeOps := &testutil.WorktreeOperationsMock{
		CreateFromExistingFunc: func(ctx context.Context, repoPath, worktreePath, branch string) error {
			createFromExistingCalled = true
			if branch != "feature-branch" {
				t.Errorf("expected branch 'feature-branch', got %s", branch)
			}
			if worktreePath != "/work/dir/tree" {
				t.Errorf("expected worktreePath '/work/dir/tree', got %s", worktreePath)
			}
			return nil
		},
	}

	importer := NewPRImporterWithOps(client, gitOps, worktreeOps)

	resultMetadata, worktreePath, err := importer.SetupWorktreeFromPR(ctx, "/repo/path", "https://github.com/owner/repo/pull/123", "", "/work/dir", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fetchPRRefCalled {
		t.Error("FetchPRRef was not called")
	}

	if !createFromExistingCalled {
		t.Error("CreateFromExisting was not called")
	}

	if resultMetadata.Number != 123 {
		t.Errorf("expected PR number 123, got %d", resultMetadata.Number)
	}

	if worktreePath != "/work/dir/tree" {
		t.Errorf("expected worktreePath '/work/dir/tree', got %s", worktreePath)
	}
}

func TestSetupWorktreeFromPR_CustomBranchName(t *testing.T) {
	ctx := context.Background()

	metadata := &github.PRMetadata{
		Number:      123,
		URL:         "https://github.com/owner/repo/pull/123",
		Title:       "Test PR",
		HeadRefName: "feature-branch",
		BaseRefName: "main",
	}

	client := &testutil.GitHubClientMock{
		GetPRMetadataFunc: func(ctx context.Context, prURLOrNumber string, repo string) (*github.PRMetadata, error) {
			return metadata, nil
		},
	}

	gitOps := &testutil.GitOperationsMock{
		FetchPRRefFunc: func(ctx context.Context, repoPath string, prNumber int, localBranch string) error {
			if localBranch != "custom-branch" {
				t.Errorf("expected branch 'custom-branch', got %s", localBranch)
			}
			return nil
		},
	}

	worktreeOps := &testutil.WorktreeOperationsMock{
		CreateFromExistingFunc: func(ctx context.Context, repoPath, worktreePath, branch string) error {
			if branch != "custom-branch" {
				t.Errorf("expected branch 'custom-branch', got %s", branch)
			}
			return nil
		},
	}

	importer := NewPRImporterWithOps(client, gitOps, worktreeOps)

	_, _, err := importer.SetupWorktreeFromPR(ctx, "/repo/path", "https://github.com/owner/repo/pull/123", "", "/work/dir", "custom-branch")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetupWorktreeFromPR_MetadataError(t *testing.T) {
	ctx := context.Background()

	client := &testutil.GitHubClientMock{
		GetPRMetadataFunc: func(ctx context.Context, prURLOrNumber string, repo string) (*github.PRMetadata, error) {
			return nil, errors.New("API error")
		},
	}

	gitOps := &testutil.GitOperationsMock{}
	worktreeOps := &testutil.WorktreeOperationsMock{}

	importer := NewPRImporterWithOps(client, gitOps, worktreeOps)

	_, _, err := importer.SetupWorktreeFromPR(ctx, "/repo/path", "https://github.com/owner/repo/pull/123", "", "/work/dir", "")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, errors.New("API error")) && err.Error() != "failed to get PR metadata: API error" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupWorktreeFromPR_FetchPRRefError(t *testing.T) {
	ctx := context.Background()

	metadata := &github.PRMetadata{
		Number:      123,
		HeadRefName: "feature-branch",
	}

	client := &testutil.GitHubClientMock{
		GetPRMetadataFunc: func(ctx context.Context, prURLOrNumber string, repo string) (*github.PRMetadata, error) {
			return metadata, nil
		},
	}

	gitOps := &testutil.GitOperationsMock{
		FetchPRRefFunc: func(ctx context.Context, repoPath string, prNumber int, localBranch string) error {
			return errors.New("fetch failed")
		},
	}

	worktreeOps := &testutil.WorktreeOperationsMock{}

	importer := NewPRImporterWithOps(client, gitOps, worktreeOps)

	resultMetadata, _, err := importer.SetupWorktreeFromPR(ctx, "/repo/path", "https://github.com/owner/repo/pull/123", "", "/work/dir", "")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Metadata should still be returned on fetch failure
	if resultMetadata == nil {
		t.Error("metadata should be returned even on fetch failure")
	}
}

func TestSetupWorktreeFromPR_WorktreeCreateError(t *testing.T) {
	ctx := context.Background()

	metadata := &github.PRMetadata{
		Number:      123,
		HeadRefName: "feature-branch",
	}

	client := &testutil.GitHubClientMock{
		GetPRMetadataFunc: func(ctx context.Context, prURLOrNumber string, repo string) (*github.PRMetadata, error) {
			return metadata, nil
		},
	}

	gitOps := &testutil.GitOperationsMock{
		FetchPRRefFunc: func(ctx context.Context, repoPath string, prNumber int, localBranch string) error {
			return nil
		},
	}

	worktreeOps := &testutil.WorktreeOperationsMock{
		CreateFromExistingFunc: func(ctx context.Context, repoPath, worktreePath, branch string) error {
			return errors.New("worktree create failed")
		},
	}

	importer := NewPRImporterWithOps(client, gitOps, worktreeOps)

	resultMetadata, _, err := importer.SetupWorktreeFromPR(ctx, "/repo/path", "https://github.com/owner/repo/pull/123", "", "/work/dir", "")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Metadata should still be returned on worktree create failure
	if resultMetadata == nil {
		t.Error("metadata should be returned even on worktree create failure")
	}
}

func TestFetchPRMetadata(t *testing.T) {
	ctx := context.Background()

	expectedMetadata := &github.PRMetadata{
		Number: 456,
		Title:  "Test PR",
	}

	client := &testutil.GitHubClientMock{
		GetPRMetadataFunc: func(ctx context.Context, prURLOrNumber string, repo string) (*github.PRMetadata, error) {
			if prURLOrNumber != "456" {
				t.Errorf("expected prURLOrNumber '456', got %s", prURLOrNumber)
			}
			if repo != "owner/repo" {
				t.Errorf("expected repo 'owner/repo', got %s", repo)
			}
			return expectedMetadata, nil
		},
	}

	importer := NewPRImporter(client)

	metadata, err := importer.FetchPRMetadata(ctx, "456", "owner/repo")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metadata.Number != 456 {
		t.Errorf("expected PR number 456, got %d", metadata.Number)
	}
}

func TestMapPRToBeadCreate(t *testing.T) {
	pr := &github.PRMetadata{
		Number:      123,
		URL:         "https://github.com/owner/repo/pull/123",
		Title:       "Add new feature",
		Body:        "This PR adds a new feature",
		HeadRefName: "feature-branch",
		BaseRefName: "main",
		Author:      "testuser",
		Labels:      []string{"feature", "enhancement"},
		State:       "OPEN",
		Repo:        "owner/repo",
	}

	opts := mapPRToBeadCreate(pr)

	if opts.title != "Add new feature" {
		t.Errorf("expected title 'Add new feature', got %s", opts.title)
	}

	if opts.description != "This PR adds a new feature" {
		t.Errorf("expected description 'This PR adds a new feature', got %s", opts.description)
	}

	// Should detect feature type from labels
	if opts.issueType != "feature" {
		t.Errorf("expected type 'feature', got %s", opts.issueType)
	}

	// Should have default P2 priority
	if opts.priority != "P2" {
		t.Errorf("expected priority 'P2', got %s", opts.priority)
	}

	// Labels should be passed through
	if len(opts.labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(opts.labels))
	}

	// Metadata should contain PR info
	if opts.metadata["pr_url"] != "https://github.com/owner/repo/pull/123" {
		t.Error("pr_url metadata not set correctly")
	}
	if opts.metadata["pr_number"] != "123" {
		t.Error("pr_number metadata not set correctly")
	}
	if opts.metadata["pr_branch"] != "feature-branch" {
		t.Error("pr_branch metadata not set correctly")
	}
	if opts.metadata["pr_author"] != "testuser" {
		t.Error("pr_author metadata not set correctly")
	}
}

func TestMapPRType(t *testing.T) {
	tests := []struct {
		name     string
		pr       *github.PRMetadata
		expected string
	}{
		{
			name: "Bug from label",
			pr: &github.PRMetadata{
				Title:  "Some change",
				Labels: []string{"bug"},
			},
			expected: "bug",
		},
		{
			name: "Bug from fix label",
			pr: &github.PRMetadata{
				Title:  "Some change",
				Labels: []string{"bugfix"},
			},
			expected: "bug",
		},
		{
			name: "Feature from label",
			pr: &github.PRMetadata{
				Title:  "Some change",
				Labels: []string{"feature"},
			},
			expected: "feature",
		},
		{
			name: "Feature from enhancement label",
			pr: &github.PRMetadata{
				Title:  "Some change",
				Labels: []string{"enhancement"},
			},
			expected: "feature",
		},
		{
			name: "Bug from title",
			pr: &github.PRMetadata{
				Title:  "Fix broken login",
				Labels: []string{},
			},
			expected: "bug",
		},
		{
			name: "Feature from title with feat",
			pr: &github.PRMetadata{
				Title:  "feat: Add new button",
				Labels: []string{},
			},
			expected: "feature",
		},
		{
			name: "Feature from title with add",
			pr: &github.PRMetadata{
				Title:  "Add user authentication",
				Labels: []string{},
			},
			expected: "feature",
		},
		{
			name: "Default to task",
			pr: &github.PRMetadata{
				Title:  "Update documentation",
				Labels: []string{},
			},
			expected: "task",
		},
		{
			name: "Label takes precedence over title",
			pr: &github.PRMetadata{
				Title:  "Fix: Add new feature", // Title suggests bug
				Labels: []string{"feature"},    // Label says feature
			},
			expected: "feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapPRType(tt.pr)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestMapPRPriority(t *testing.T) {
	tests := []struct {
		name     string
		pr       *github.PRMetadata
		expected string
	}{
		{
			name: "Critical from label",
			pr: &github.PRMetadata{
				Labels: []string{"critical"},
			},
			expected: "P0",
		},
		{
			name: "Urgent from label",
			pr: &github.PRMetadata{
				Labels: []string{"urgent"},
			},
			expected: "P0",
		},
		{
			name: "P0 from label",
			pr: &github.PRMetadata{
				Labels: []string{"p0"},
			},
			expected: "P0",
		},
		{
			name: "High priority from label",
			pr: &github.PRMetadata{
				Labels: []string{"high-priority"},
			},
			expected: "P1",
		},
		{
			name: "P1 from label",
			pr: &github.PRMetadata{
				Labels: []string{"priority-p1"},
			},
			expected: "P1",
		},
		{
			name: "Medium priority from label",
			pr: &github.PRMetadata{
				Labels: []string{"medium"},
			},
			expected: "P2",
		},
		{
			name: "P2 from label",
			pr: &github.PRMetadata{
				Labels: []string{"p2"},
			},
			expected: "P2",
		},
		{
			name: "Low priority from label",
			pr: &github.PRMetadata{
				Labels: []string{"low"},
			},
			expected: "P3",
		},
		{
			name: "P3 from label",
			pr: &github.PRMetadata{
				Labels: []string{"p3"},
			},
			expected: "P3",
		},
		{
			name: "Default to P2",
			pr: &github.PRMetadata{
				Labels: []string{"documentation"},
			},
			expected: "P2",
		},
		{
			name: "Empty labels default to P2",
			pr: &github.PRMetadata{
				Labels: []string{},
			},
			expected: "P2",
		},
		{
			name: "First matching priority wins",
			pr: &github.PRMetadata{
				Labels: []string{"low", "critical"}, // critical comes second but matches first in loop
			},
			expected: "P3", // low is checked before critical in the loop order
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapPRPriority(tt.pr)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestMapPRStatus(t *testing.T) {
	tests := []struct {
		name     string
		pr       *github.PRMetadata
		expected string
	}{
		{
			name: "Merged PR",
			pr: &github.PRMetadata{
				State:  "MERGED",
				Merged: true,
			},
			expected: "closed",
		},
		{
			name: "Open draft PR",
			pr: &github.PRMetadata{
				State:   "OPEN",
				IsDraft: true,
				Merged:  false,
			},
			expected: "open",
		},
		{
			name: "Open PR not draft",
			pr: &github.PRMetadata{
				State:   "OPEN",
				IsDraft: false,
				Merged:  false,
			},
			expected: "in_progress",
		},
		{
			name: "Closed PR",
			pr: &github.PRMetadata{
				State:  "CLOSED",
				Merged: false,
			},
			expected: "closed",
		},
		{
			name: "Merged state",
			pr: &github.PRMetadata{
				State:  "MERGED",
				Merged: false, // Even if Merged is false, MERGED state maps to closed
			},
			expected: "closed",
		},
		{
			name: "Unknown state defaults to open",
			pr: &github.PRMetadata{
				State:  "UNKNOWN",
				Merged: false,
			},
			expected: "open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapPRStatus(tt.pr)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFormatBeadDescription(t *testing.T) {
	mergedAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		pr       *github.PRMetadata
		contains []string
	}{
		{
			name: "Basic PR",
			pr: &github.PRMetadata{
				Number:      123,
				URL:         "https://github.com/owner/repo/pull/123",
				Body:        "Original description",
				HeadRefName: "feature-branch",
				BaseRefName: "main",
				Author:      "testuser",
				State:       "OPEN",
			},
			contains: []string{
				"Original description",
				"**Imported from GitHub PR**",
				"PR: #123",
				"URL: https://github.com/owner/repo/pull/123",
				"Branch: feature-branch → main",
				"Author: testuser",
				"State: OPEN",
			},
		},
		{
			name: "Draft PR",
			pr: &github.PRMetadata{
				Number:      456,
				URL:         "https://github.com/owner/repo/pull/456",
				HeadRefName: "draft-branch",
				BaseRefName: "main",
				Author:      "otheruser",
				State:       "OPEN",
				IsDraft:     true,
			},
			contains: []string{
				"Draft: yes",
			},
		},
		{
			name: "Merged PR",
			pr: &github.PRMetadata{
				Number:      789,
				URL:         "https://github.com/owner/repo/pull/789",
				HeadRefName: "merged-branch",
				BaseRefName: "main",
				Author:      "mergeduser",
				State:       "MERGED",
				Merged:      true,
				MergedAt:    mergedAt,
			},
			contains: []string{
				"Merged: 2024-01-15",
			},
		},
		{
			name: "PR with labels",
			pr: &github.PRMetadata{
				Number:      101,
				URL:         "https://github.com/owner/repo/pull/101",
				HeadRefName: "labeled-branch",
				BaseRefName: "main",
				Author:      "labeluser",
				State:       "OPEN",
				Labels:      []string{"bug", "urgent", "needs-review"},
			},
			contains: []string{
				"Labels: bug, urgent, needs-review",
			},
		},
		{
			name: "Empty body",
			pr: &github.PRMetadata{
				Number:      200,
				URL:         "https://github.com/owner/repo/pull/200",
				Body:        "",
				HeadRefName: "no-body-branch",
				BaseRefName: "main",
				Author:      "noBodyUser",
				State:       "OPEN",
			},
			contains: []string{
				"**Imported from GitHub PR**",
				"PR: #200",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBeadDescription(tt.pr)

			for _, expected := range tt.contains {
				if !containsString(result, expected) {
					t.Errorf("expected description to contain %q, got:\n%s", expected, result)
				}
			}
		})
	}
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"P0", 0},
		{"P1", 1},
		{"P2", 2},
		{"P3", 3},
		{"P4", 4},
		{"p0", 2},       // lowercase doesn't match, defaults to 2
		{"", 2},         // empty defaults to 2
		{"P5", 2},       // unknown P-level defaults to 2
		{"invalid", 2},
		{"P", 2},        // just P defaults to 2
		{"Priority1", 2}, // doesn't start with P followed by digit
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parsePriority(tt.input)
			if result != tt.expected {
				t.Errorf("parsePriority(%q) = %d, expected %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCreateBeadOptions(t *testing.T) {
	opts := &CreateBeadOptions{
		BeadsDir:         "/path/to/beads",
		SkipIfExists:     true,
		OverrideTitle:    "Custom Title",
		OverrideType:     "bug",
		OverridePriority: "P1",
	}

	if opts.BeadsDir != "/path/to/beads" {
		t.Error("BeadsDir not set correctly")
	}
	if !opts.SkipIfExists {
		t.Error("SkipIfExists not set correctly")
	}
	if opts.OverrideTitle != "Custom Title" {
		t.Error("OverrideTitle not set correctly")
	}
	if opts.OverrideType != "bug" {
		t.Error("OverrideType not set correctly")
	}
	if opts.OverridePriority != "P1" {
		t.Error("OverridePriority not set correctly")
	}
}

func TestCreateBeadResult(t *testing.T) {
	result := &CreateBeadResult{
		BeadID:     "bead-123",
		Created:    true,
		SkipReason: "",
	}

	if result.BeadID != "bead-123" {
		t.Error("BeadID not set correctly")
	}
	if !result.Created {
		t.Error("Created not set correctly")
	}

	// Test skip result
	skipResult := &CreateBeadResult{
		BeadID:     "existing-bead",
		Created:    false,
		SkipReason: "bead already exists for this PR",
	}

	if skipResult.Created {
		t.Error("Created should be false for skipped bead")
	}
	if skipResult.SkipReason == "" {
		t.Error("SkipReason should be set for skipped bead")
	}
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
