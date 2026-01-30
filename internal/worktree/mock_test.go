package worktree_test

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/testutil"
	"github.com/newhook/co/internal/worktree"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err)
		require.Len(t, worktrees, len(expectedWorktrees))
	})

	t.Run("ExistsPath returns configured value", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{
			ExistsPathFunc: func(worktreePath string) bool {
				return worktreePath == "/existing/path"
			},
		}

		require.True(t, mock.ExistsPath("/existing/path"), "expected ExistsPath to return true for existing path")
		require.False(t, mock.ExistsPath("/nonexistent/path"), "expected ExistsPath to return false for nonexistent path")

		// Verify calls are tracked
		calls := mock.ExistsPathCalls()
		require.Len(t, calls, 2)
	})

	t.Run("Create tracks call arguments", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{
			CreateFunc: func(ctx context.Context, repoPath, worktreePath, branch, baseBranch string) error {
				return nil
			},
		}

		_ = mock.Create(ctx, "/repo", "/worktree/path", "feature-branch", "main")

		calls := mock.CreateCalls()
		require.Len(t, calls, 1)
		require.Equal(t, "/repo", calls[0].RepoPath)
		require.Equal(t, "/worktree/path", calls[0].WorktreePath)
		require.Equal(t, "feature-branch", calls[0].Branch)
		require.Equal(t, "main", calls[0].BaseBranch)
	})

	t.Run("CreateFromExisting tracks call arguments", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{
			CreateFromExistingFunc: func(ctx context.Context, repoPath, worktreePath, branch string) error {
				return nil
			},
		}

		_ = mock.CreateFromExisting(ctx, "/repo", "/worktree/path", "existing-branch")

		calls := mock.CreateFromExistingCalls()
		require.Len(t, calls, 1)
		require.Equal(t, "existing-branch", calls[0].Branch)
	})

	t.Run("RemoveForce tracks call arguments", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{
			RemoveForceFunc: func(ctx context.Context, repoPath, worktreePath string) error {
				return nil
			},
		}

		_ = mock.RemoveForce(ctx, "/repo", "/worktree/to/remove")

		calls := mock.RemoveForceCalls()
		require.Len(t, calls, 1)
		require.Equal(t, "/worktree/to/remove", calls[0].WorktreePath)
	})

	t.Run("nil function returns zero value", func(t *testing.T) {
		mock := &testutil.WorktreeOperationsMock{}

		// Without setting function, mock returns zero values
		require.False(t, mock.ExistsPath("/any"), "expected false when ExistsPathFunc is nil")

		worktrees, err := mock.List(ctx, "/repo")
		require.NoError(t, err)
		require.Nil(t, worktrees, "expected nil worktrees when ListFunc is nil")
	})
}
