package git

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
	// BranchExists checks if a branch exists locally or remotely.
	BranchExists(ctx context.Context, repoPath, branchName string) bool
	// ValidateExistingBranch checks if a branch exists locally, remotely, or both.
	ValidateExistingBranch(ctx context.Context, repoPath, branchName string) (existsLocal, existsRemote bool, err error)
	// ListBranches returns a deduplicated list of all branches (local and remote).
	ListBranches(ctx context.Context, repoPath string) ([]string, error)
}

// cliOperations implements Operations using the git CLI.
type cliOperations struct{}

// Compile-time check that cliOperations implements Operations.
var _ Operations = (*cliOperations)(nil)

// Default is the default Operations implementation using the git CLI.
var Default Operations = &cliOperations{}

// PushSetUpstream implements Operations.PushSetUpstream.
func (c *cliOperations) PushSetUpstream(ctx context.Context, branch, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "push", "--set-upstream", "origin", branch)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push and set upstream for branch %s: %w\n%s", branch, err, output)
	}
	return nil
}

// PushSetUpstreamInDir pushes the specified branch and sets upstream tracking.
// Deprecated: Use Default.PushSetUpstream instead.
func PushSetUpstreamInDir(ctx context.Context, branch, dir string) error {
	return Default.PushSetUpstream(ctx, branch, dir)
}

// Pull implements Operations.Pull.
func (c *cliOperations) Pull(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "pull")
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}
	return nil
}

// PullInDir pulls the latest changes in a specific directory.
// Deprecated: Use Default.Pull instead.
func PullInDir(ctx context.Context, dir string) error {
	return Default.Pull(ctx, dir)
}

// Clone implements Operations.Clone.
func (c *cliOperations) Clone(ctx context.Context, source, dest string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", source, dest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone repository: %w\n%s", err, output)
	}
	return nil
}

// Clone clones a repository from source to dest.
// Deprecated: Use Default.Clone instead.
func Clone(ctx context.Context, source, dest string) error {
	return Default.Clone(ctx, source, dest)
}

// FetchBranch implements Operations.FetchBranch.
func (c *cliOperations) FetchBranch(ctx context.Context, repoPath, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", branch)
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch branch %s: %w\n%s", branch, err, output)
	}
	return nil
}

// FetchBranch fetches a specific branch from origin.
// Deprecated: Use Default.FetchBranch instead.
func FetchBranch(ctx context.Context, repoPath, branch string) error {
	return Default.FetchBranch(ctx, repoPath, branch)
}

// BranchExists implements Operations.BranchExists.
func (c *cliOperations) BranchExists(ctx context.Context, repoPath, branchName string) bool {
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

// BranchExists checks if a branch exists locally or remotely.
// Deprecated: Use Default.BranchExists instead.
func BranchExists(ctx context.Context, repoPath, branchName string) bool {
	return Default.BranchExists(ctx, repoPath, branchName)
}

// ValidateExistingBranch implements Operations.ValidateExistingBranch.
func (c *cliOperations) ValidateExistingBranch(ctx context.Context, repoPath, branchName string) (bool, bool, error) {
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

// ValidateExistingBranch checks if a branch exists locally, remotely, or both.
// Deprecated: Use Default.ValidateExistingBranch instead.
func ValidateExistingBranch(ctx context.Context, repoPath, branchName string) (bool, bool, error) {
	return Default.ValidateExistingBranch(ctx, repoPath, branchName)
}

// ListBranches implements Operations.ListBranches.
func (c *cliOperations) ListBranches(ctx context.Context, repoPath string) ([]string, error) {
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

// ListBranches returns a deduplicated list of all branches (local and remote).
// Deprecated: Use Default.ListBranches instead.
func ListBranches(ctx context.Context, repoPath string) ([]string, error) {
	return Default.ListBranches(ctx, repoPath)
}
