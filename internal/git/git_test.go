package git_test

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/testutil"
)

func TestNewOperations(t *testing.T) {
	ops := git.NewOperations()
	if ops == nil {
		t.Fatal("NewOperations returned nil")
	}
}

func TestOperationsInterface(t *testing.T) {
	// Compile-time check that GitOperationsMock implements git.Operations
	var _ git.Operations = (*testutil.GitOperationsMock)(nil)
}

// TestGitOperationsMock verifies the mock works correctly with function fields.
func TestGitOperationsMock(t *testing.T) {
	ctx := context.Background()

	t.Run("BranchExists returns configured value", func(t *testing.T) {
		mock := &testutil.GitOperationsMock{
			BranchExistsFunc: func(ctx context.Context, repoPath, branchName string) bool {
				return branchName == "main"
			},
		}

		if !mock.BranchExists(ctx, "/repo", "main") {
			t.Error("expected BranchExists to return true for 'main'")
		}
		if mock.BranchExists(ctx, "/repo", "nonexistent") {
			t.Error("expected BranchExists to return false for 'nonexistent'")
		}

		// Verify calls are tracked
		calls := mock.BranchExistsCalls()
		if len(calls) != 2 {
			t.Errorf("expected 2 calls, got %d", len(calls))
		}
	})

	t.Run("ValidateExistingBranch returns configured values", func(t *testing.T) {
		mock := &testutil.GitOperationsMock{
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
		if err != nil || !localMain || !remoteMain {
			t.Error("expected main to exist locally and remotely")
		}

		localOnly, remoteOnly, err := mock.ValidateExistingBranch(ctx, "/repo", "local-only")
		if err != nil || !localOnly || remoteOnly {
			t.Error("expected local-only to exist only locally")
		}

		localNew, remoteNew, err := mock.ValidateExistingBranch(ctx, "/repo", "new-branch")
		if err != nil || localNew || remoteNew {
			t.Error("expected new-branch to not exist")
		}
	})

	t.Run("ListBranches returns configured branches", func(t *testing.T) {
		expectedBranches := []string{"feature-1", "feature-2", "develop"}
		mock := &testutil.GitOperationsMock{
			ListBranchesFunc: func(ctx context.Context, repoPath string) ([]string, error) {
				return expectedBranches, nil
			},
		}

		branches, err := mock.ListBranches(ctx, "/repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(branches) != len(expectedBranches) {
			t.Errorf("expected %d branches, got %d", len(expectedBranches), len(branches))
		}
	})

	t.Run("FetchPRRef tracks call arguments", func(t *testing.T) {
		mock := &testutil.GitOperationsMock{
			FetchPRRefFunc: func(ctx context.Context, repoPath string, prNumber int, localBranch string) error {
				return nil
			},
		}

		_ = mock.FetchPRRef(ctx, "/repo", 123, "pr-123-branch")

		calls := mock.FetchPRRefCalls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].RepoPath != "/repo" {
			t.Errorf("expected repoPath '/repo', got %s", calls[0].RepoPath)
		}
		if calls[0].PrNumber != 123 {
			t.Errorf("expected prNumber 123, got %d", calls[0].PrNumber)
		}
		if calls[0].LocalBranch != "pr-123-branch" {
			t.Errorf("expected localBranch 'pr-123-branch', got %s", calls[0].LocalBranch)
		}
	})

	t.Run("PushSetUpstream tracks call arguments", func(t *testing.T) {
		mock := &testutil.GitOperationsMock{
			PushSetUpstreamFunc: func(ctx context.Context, branch, dir string) error {
				return nil
			},
		}

		_ = mock.PushSetUpstream(ctx, "feature-branch", "/work/tree")

		calls := mock.PushSetUpstreamCalls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Branch != "feature-branch" {
			t.Errorf("expected branch 'feature-branch', got %s", calls[0].Branch)
		}
		if calls[0].Dir != "/work/tree" {
			t.Errorf("expected dir '/work/tree', got %s", calls[0].Dir)
		}
	})

	t.Run("Pull and Clone track calls", func(t *testing.T) {
		mock := &testutil.GitOperationsMock{
			PullFunc: func(ctx context.Context, dir string) error {
				return nil
			},
			CloneFunc: func(ctx context.Context, source, dest string) error {
				return nil
			},
		}

		_ = mock.Pull(ctx, "/repo")
		_ = mock.Clone(ctx, "git@github.com:user/repo.git", "/dest")

		if len(mock.PullCalls()) != 1 {
			t.Error("expected 1 Pull call")
		}
		if len(mock.CloneCalls()) != 1 {
			t.Error("expected 1 Clone call")
		}
	})

	t.Run("nil function returns zero value", func(t *testing.T) {
		mock := &testutil.GitOperationsMock{}

		// Without setting function, mock returns zero values
		if mock.BranchExists(ctx, "/repo", "any") {
			t.Error("expected false when BranchExistsFunc is nil")
		}

		branches, err := mock.ListBranches(ctx, "/repo")
		if err != nil || branches != nil {
			t.Error("expected nil branches and nil error when ListBranchesFunc is nil")
		}
	})
}
