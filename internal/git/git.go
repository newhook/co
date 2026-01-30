package git

//go:generate moq -stub -out ../testutil/git_mock.go -pkg testutil . Operations:GitOperationsMock

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Operations defines the interface for git operations.
// This abstraction enables testing without actual git commands.
type Operations interface {
	// PushSetUpstream pushes the specified branch and sets upstream tracking.
	PushSetUpstream(ctx context.Context, branch, dir string) error
	// Pull pulls the latest changes in a specific directory.
	Pull(ctx context.Context, dir string) error
	// Clone clones a repository from source to dest.
	Clone(ctx context.Context, source, dest string) error
	// FetchBranch fetches a specific branch from origin.
	FetchBranch(ctx context.Context, repoPath, branch string) error
	// FetchPRRef fetches a PR's head ref and creates/updates a local branch.
	// This handles both same-repo PRs and fork PRs via GitHub's pull/<n>/head refs.
	FetchPRRef(ctx context.Context, repoPath string, prNumber int, localBranch string) error
	// BranchExists checks if a branch exists locally or remotely.
	BranchExists(ctx context.Context, repoPath, branchName string) bool
	// ValidateExistingBranch checks if a branch exists locally, remotely, or both.
	ValidateExistingBranch(ctx context.Context, repoPath, branchName string) (existsLocal, existsRemote bool, err error)
	// ListBranches returns a deduplicated list of all branches (local and remote).
	ListBranches(ctx context.Context, repoPath string) ([]string, error)
}

// CLIOperations implements Operations using the git CLI.
type CLIOperations struct{}

// Compile-time check that CLIOperations implements Operations.
var _ Operations = (*CLIOperations)(nil)

// NewOperations creates a new Operations implementation using the git CLI.
func NewOperations() Operations {
	return &CLIOperations{}
}

// PushSetUpstream implements Operations.PushSetUpstream.
func (c *CLIOperations) PushSetUpstream(ctx context.Context, branch, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "push", "--set-upstream", "origin", branch)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push and set upstream for branch %s: %w\n%s", branch, err, output)
	}
	return nil
}

// Pull implements Operations.Pull.
func (c *CLIOperations) Pull(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "pull")
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}
	return nil
}

// Clone implements Operations.Clone.
func (c *CLIOperations) Clone(ctx context.Context, source, dest string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", source, dest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone repository: %w\n%s", err, output)
	}
	return nil
}

// FetchBranch implements Operations.FetchBranch.
func (c *CLIOperations) FetchBranch(ctx context.Context, repoPath, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", branch)
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch branch %s: %w\n%s", branch, err, output)
	}
	return nil
}

// FetchPRRef implements Operations.FetchPRRef.
// This fetches a PR's head ref using GitHub's special refs/pull/<n>/head ref
// and creates or updates a local branch pointing to it.
func (c *CLIOperations) FetchPRRef(ctx context.Context, repoPath string, prNumber int, localBranch string) error {
	// Fetch the PR's head ref from origin
	// GitHub makes PR branches available at refs/pull/<number>/head
	refSpec := fmt.Sprintf("refs/pull/%d/head:%s", prNumber, localBranch)
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", refSpec)
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch PR #%d: %w\n%s", prNumber, err, output)
	}
	return nil
}

// BranchExists implements Operations.BranchExists.
func (c *CLIOperations) BranchExists(ctx context.Context, repoPath, branchName string) bool {
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

// ValidateExistingBranch implements Operations.ValidateExistingBranch.
func (c *CLIOperations) ValidateExistingBranch(ctx context.Context, repoPath, branchName string) (bool, bool, error) {
	// Check local branches
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
	cmd.Dir = repoPath
	existsLocal := cmd.Run() == nil

	// Check remote branches
	cmd = exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branchName)
	cmd.Dir = repoPath
	existsRemote := cmd.Run() == nil

	return existsLocal, existsRemote, nil
}

// ListBranches implements Operations.ListBranches.
func (c *CLIOperations) ListBranches(ctx context.Context, repoPath string) ([]string, error) {
	// Get local branches
	cmd := exec.CommandContext(ctx, "git", "branch", "--format=%(refname:short)")
	cmd.Dir = repoPath
	localOutput, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list local branches: %w", err)
	}

	// Get remote branches
	cmd = exec.CommandContext(ctx, "git", "branch", "-r", "--format=%(refname:short)")
	cmd.Dir = repoPath
	remoteOutput, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list remote branches: %w", err)
	}

	// Get current branch to exclude it
	cmd = exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoPath
	currentBranchBytes, _ := cmd.Output()
	currentBranch := strings.TrimSpace(string(currentBranchBytes))

	// Deduplicate branches
	seen := make(map[string]bool)
	var branches []string

	// Process local branches
	for _, line := range strings.Split(strings.TrimSpace(string(localOutput)), "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" || branch == currentBranch {
			continue
		}
		if !seen[branch] {
			seen[branch] = true
			branches = append(branches, branch)
		}
	}

	// Process remote branches (strip origin/ prefix)
	for _, line := range strings.Split(strings.TrimSpace(string(remoteOutput)), "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" {
			continue
		}
		// Skip HEAD pointer
		if strings.HasSuffix(branch, "/HEAD") {
			continue
		}
		// Strip origin/ prefix
		branch = strings.TrimPrefix(branch, "origin/")
		if branch == currentBranch {
			continue
		}
		if !seen[branch] {
			seen[branch] = true
			branches = append(branches, branch)
		}
	}

	return branches, nil
}
