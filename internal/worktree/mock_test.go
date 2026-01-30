package worktree_test

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/testutil"
	"github.com/newhook/co/internal/worktree"
)

func TestMockImplementsInterface(t *testing.T) {
	// Compile-time check that WorktreeOperationsMock implements Operations
	var _ worktree.Operations = (*testutil.WorktreeOperationsMock)(nil)
}

// TestWorktreeOperationsMock verifies the mock works correctly with function fields.
func TestWorktreeOperationsMock(t *testing.T) {
	ctx := context.Background()

	t.Run("List returns configured worktrees", func(t *testing.T) {
		expectedWorktrees := []worktree.Worktree{
			{Path: "/main", HEAD: "abc123", Branch: "main"},
			{Path: "/feature", HEAD: "def456", Branch: "feature"},
		}
		mock := &testutil.WorktreeOperationsMock{
			ListFunc: func(ctx context.Context, repoPath string) ([]worktree.Worktree, error) {
				return expectedWorktrees, nil
			},
		}

		worktrees, err := mock.List(ctx, "/repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(worktrees) != len(expectedWorktrees) {
			t.Errorf("expected %d worktrees, got %d", len(expectedWorktrees), len(worktrees))
		}
	})

	t.Run("ExistsPath returns configured value", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{
			ExistsPathFunc: func(worktreePath string) bool {
				return worktreePath == "/existing/path"
			},
		}

		if !mock.ExistsPath("/existing/path") {
			t.Error("expected ExistsPath to return true for existing path")
		}
		if mock.ExistsPath("/nonexistent/path") {
			t.Error("expected ExistsPath to return false for nonexistent path")
		}

		// Verify calls are tracked
		calls := mock.ExistsPathCalls()
		if len(calls) != 2 {
			t.Errorf("expected 2 calls, got %d", len(calls))
		}
	})

	t.Run("Create tracks call arguments", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{
			CreateFunc: func(ctx context.Context, repoPath, worktreePath, branch, baseBranch string) error {
				return nil
			},
		}

		_ = mock.Create(ctx, "/repo", "/worktree/path", "feature-branch", "main")

		calls := mock.CreateCalls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].RepoPath != "/repo" {
			t.Errorf("expected repoPath '/repo', got %s", calls[0].RepoPath)
		}
		if calls[0].WorktreePath != "/worktree/path" {
			t.Errorf("expected worktreePath '/worktree/path', got %s", calls[0].WorktreePath)
		}
		if calls[0].Branch != "feature-branch" {
			t.Errorf("expected branch 'feature-branch', got %s", calls[0].Branch)
		}
		if calls[0].BaseBranch != "main" {
			t.Errorf("expected baseBranch 'main', got %s", calls[0].BaseBranch)
		}
	})

	t.Run("CreateFromExisting tracks call arguments", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{
			CreateFromExistingFunc: func(ctx context.Context, repoPath, worktreePath, branch string) error {
				return nil
			},
		}

		_ = mock.CreateFromExisting(ctx, "/repo", "/worktree/path", "existing-branch")

		calls := mock.CreateFromExistingCalls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Branch != "existing-branch" {
			t.Errorf("expected branch 'existing-branch', got %s", calls[0].Branch)
		}
	})

	t.Run("RemoveForce tracks call arguments", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{
			RemoveForceFunc: func(ctx context.Context, repoPath, worktreePath string) error {
				return nil
			},
		}

		_ = mock.RemoveForce(ctx, "/repo", "/worktree/to/remove")

		calls := mock.RemoveForceCalls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].WorktreePath != "/worktree/to/remove" {
			t.Errorf("expected worktreePath '/worktree/to/remove', got %s", calls[0].WorktreePath)
		}
	})

	t.Run("nil function returns zero value", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{}

		// Without setting function, mock returns zero values
		if mock.ExistsPath("/any") {
			t.Error("expected false when ExistsPathFunc is nil")
		}

		worktrees, err := mock.List(ctx, "/repo")
		if err != nil || worktrees != nil {
			t.Error("expected nil worktrees and nil error when ListFunc is nil")
		}
	})
}
