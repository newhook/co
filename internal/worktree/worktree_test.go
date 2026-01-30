package worktree

import (
	"testing"
)

func TestNewOperations(t *testing.T) {
	ops := NewOperations()
	if ops == nil {
		t.Fatal("NewOperations returned nil")
	}

	// Verify it returns a CLIOperations
	_, ok := ops.(*CLIOperations)
	if !ok {
		t.Error("NewOperations should return *CLIOperations")
	}
}

func TestCLIOperationsImplementsInterface(t *testing.T) {
	// Compile-time check that CLIOperations implements Operations
	var _ Operations = (*CLIOperations)(nil)
}

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Worktree
	}{
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name: "single worktree",
			input: `worktree /path/to/main
HEAD abc123def456
branch refs/heads/main
`,
			expected: []Worktree{
				{Path: "/path/to/main", HEAD: "abc123def456", Branch: "main"},
			},
		},
		{
			name: "multiple worktrees",
			input: `worktree /path/to/main
HEAD abc123def456
branch refs/heads/main

worktree /path/to/feature
HEAD def789ghi012
branch refs/heads/feature-branch

`,
			expected: []Worktree{
				{Path: "/path/to/main", HEAD: "abc123def456", Branch: "main"},
				{Path: "/path/to/feature", HEAD: "def789ghi012", Branch: "feature-branch"},
			},
		},
		{
			name: "detached HEAD worktree",
			input: `worktree /path/to/detached
HEAD abc123def456
detached

`,
			expected: []Worktree{
				{Path: "/path/to/detached", HEAD: "abc123def456", Branch: ""},
			},
		},
		{
			name: "no trailing newline",
			input: `worktree /path/to/main
HEAD abc123def456
branch refs/heads/main`,
			expected: []Worktree{
				{Path: "/path/to/main", HEAD: "abc123def456", Branch: "main"},
			},
		},
		{
			name: "path with spaces",
			input: `worktree /path/with spaces/to/main
HEAD abc123def456
branch refs/heads/main
`,
			expected: []Worktree{
				{Path: "/path/with spaces/to/main", HEAD: "abc123def456", Branch: "main"},
			},
		},
		{
			name: "branch with slashes",
			input: `worktree /path/to/feature
HEAD abc123def456
branch refs/heads/feature/sub-feature/deep
`,
			expected: []Worktree{
				{Path: "/path/to/feature", HEAD: "abc123def456", Branch: "feature/sub-feature/deep"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseWorktreeList(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d worktrees, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				if result[i].Path != expected.Path {
					t.Errorf("worktree[%d] path: expected %q, got %q", i, expected.Path, result[i].Path)
				}
				if result[i].HEAD != expected.HEAD {
					t.Errorf("worktree[%d] HEAD: expected %q, got %q", i, expected.HEAD, result[i].HEAD)
				}
				if result[i].Branch != expected.Branch {
					t.Errorf("worktree[%d] branch: expected %q, got %q", i, expected.Branch, result[i].Branch)
				}
			}
		})
	}
}

