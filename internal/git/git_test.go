package git_test

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/git"
	"github.com/stretchr/testify/require"
)

func TestNewOperations(t *testing.T) {
	ops := git.NewOperations()
	require.NotNil(t, ops, "NewOperations returned nil")
}

func TestOperationsInterface(t *testing.T) {
	// Compile-time check that GitOperationsMock implements git.Operations
	var _ git.Operations = (*git.GitOperationsMock)(nil)
}

// TestGitOperationsMock verifies the mock works correctly with function fields.
func TestGitOperationsMock(t *testing.T) {
	ctx := context.Background()

	t.Run("BranchExists returns configured value", func(t *testing.T) {
		mock := &git.GitOperationsMock{
			BranchExistsFunc: func(ctx context.Context, repoPath, branchName string) bool {
				return branchName == "main"
			},
		}

		require.True(t, mock.BranchExists(ctx, "/repo", "main"), "expected BranchExists to return true for 'main'")
		require.False(t, mock.BranchExists(ctx, "/repo", "nonexistent"), "expected BranchExists to return false for 'nonexistent'")

		// Verify calls are tracked
		calls := mock.BranchExistsCalls()
		require.Len(t, calls, 2)
	})

	t.Run("ValidateExistingBranch returns configured values", func(t *testing.T) {
		mock := &git.GitOperationsMock{
			ValidateExistingBranchFunc: func(ctx context.Context, repoPath, branchName string) (bool, bool, error) {
				if branchName == "main" {
					return true, true, nil // exists locally and remotely
				}
				if branchName == "local-only" {
					return true, false, nil // exists only locally
				}
				return false, false, nil
			},
		}

		localMain, remoteMain, err := mock.ValidateExistingBranch(ctx, "/repo", "main")
		require.NoError(t, err)
		require.True(t, localMain, "expected main to exist locally")
		require.True(t, remoteMain, "expected main to exist remotely")

		localOnly, remoteOnly, err := mock.ValidateExistingBranch(ctx, "/repo", "local-only")
		require.NoError(t, err)
		require.True(t, localOnly, "expected local-only to exist locally")
		require.False(t, remoteOnly, "expected local-only to not exist remotely")

		localNew, remoteNew, err := mock.ValidateExistingBranch(ctx, "/repo", "new-branch")
		require.NoError(t, err)
		require.False(t, localNew, "expected new-branch to not exist locally")
		require.False(t, remoteNew, "expected new-branch to not exist remotely")
	})

	t.Run("ListBranches returns configured branches", func(t *testing.T) {
		expectedBranches := []string{"feature-1", "feature-2", "develop"}
		mock := &git.GitOperationsMock{
			ListBranchesFunc: func(ctx context.Context, repoPath string) ([]string, error) {
				return expectedBranches, nil
			},
		}

		branches, err := mock.ListBranches(ctx, "/repo")
		require.NoError(t, err)
		require.Len(t, branches, len(expectedBranches))
	})

	t.Run("FetchPRRef tracks call arguments", func(t *testing.T) {
		mock := &git.GitOperationsMock{
			FetchPRRefFunc: func(ctx context.Context, repoPath string, prNumber int, localBranch string) error {
				return nil
			},
		}

		_ = mock.FetchPRRef(ctx, "/repo", 123, "pr-123-branch")

		calls := mock.FetchPRRefCalls()
		require.Len(t, calls, 1)
		require.Equal(t, "/repo", calls[0].RepoPath)
		require.Equal(t, 123, calls[0].PrNumber)
		require.Equal(t, "pr-123-branch", calls[0].LocalBranch)
	})

	t.Run("PushSetUpstream tracks call arguments", func(t *testing.T) {
		mock := &git.GitOperationsMock{
			PushSetUpstreamFunc: func(ctx context.Context, branch, dir string) error {
				return nil
			},
		}

		_ = mock.PushSetUpstream(ctx, "feature-branch", "/work/tree")

		calls := mock.PushSetUpstreamCalls()
		require.Len(t, calls, 1)
		require.Equal(t, "feature-branch", calls[0].Branch)
		require.Equal(t, "/work/tree", calls[0].Dir)
	})

	t.Run("Pull and Clone track calls", func(t *testing.T) {
		mock := &git.GitOperationsMock{
			PullFunc: func(ctx context.Context, dir string) error {
				return nil
			},
			CloneFunc: func(ctx context.Context, source, dest string) error {
				return nil
			},
		}

		_ = mock.Pull(ctx, "/repo")
		_ = mock.Clone(ctx, "git@github.com:user/repo.git", "/dest")

		require.Len(t, mock.PullCalls(), 1, "expected 1 Pull call")
		require.Len(t, mock.CloneCalls(), 1, "expected 1 Clone call")
	})

	t.Run("nil function returns zero value", func(t *testing.T) {
		mock := &git.GitOperationsMock{}

		// Without setting function, mock returns zero values
		require.False(t, mock.BranchExists(ctx, "/repo", "any"), "expected false when BranchExistsFunc is nil")

		branches, err := mock.ListBranches(ctx, "/repo")
		require.NoError(t, err)
		require.Nil(t, branches, "expected nil branches when ListBranchesFunc is nil")
	})
}
