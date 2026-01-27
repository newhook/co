package git

import (
	"context"
	"fmt"
	"os/exec"
)

// PushSetUpstreamInDir pushes the specified branch and sets upstream tracking.
func PushSetUpstreamInDir(ctx context.Context, branch, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "push", "--set-upstream", "origin", branch)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push and set upstream for branch %s: %w\n%s", branch, err, output)
	}
	return nil
}

// PullInDir pulls the latest changes in a specific directory.
func PullInDir(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "pull")
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}
	return nil
}

// Clone clones a repository from source to dest.
func Clone(ctx context.Context, source, dest string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", source, dest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone repository: %w\n%s", err, output)
	}
	return nil
}

// FetchBranch fetches a specific branch from origin.
func FetchBranch(ctx context.Context, repoPath, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", branch)
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch branch %s: %w\n%s", branch, err, output)
	}
	return nil
}

// BranchExists checks if a branch exists locally or remotely.
func BranchExists(ctx context.Context, repoPath, branchName string) bool {
	// Check local branches
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
	cmd.Dir = repoPath
	if cmd.Run() == nil {
		return true
	}

	// Check remote branches
	cmd = exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branchName)
	cmd.Dir = repoPath
	return cmd.Run() == nil
}
