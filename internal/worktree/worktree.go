package worktree

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Worktree represents a git worktree.
type Worktree struct {
	Path   string // Absolute path to the worktree
	HEAD   string // Current HEAD commit SHA
	Branch string // Branch name (empty if detached)
}

// Operations defines the interface for worktree operations.
// This abstraction enables testing without actual git commands.
type Operations interface {
	// Create creates a new worktree at worktreePath from repoPath with a new branch.
	Create(ctx context.Context, repoPath, worktreePath, branch, baseBranch string) error
	// CreateFromExisting creates a worktree at worktreePath for an existing branch.
	CreateFromExisting(ctx context.Context, repoPath, worktreePath, branch string) error
	// RemoveForce forcefully removes a worktree even if it has uncommitted changes.
	RemoveForce(ctx context.Context, repoPath, worktreePath string) error
	// List returns all worktrees for the given repository.
	List(ctx context.Context, repoPath string) ([]Worktree, error)
	// ExistsPath checks if the worktree path exists on disk.
	ExistsPath(worktreePath string) bool
}

// cliOperations implements Operations using the git CLI.
type cliOperations struct{}

// Compile-time check that cliOperations implements Operations.
var _ Operations = (*cliOperations)(nil)

// Default is the default Operations implementation using the git CLI.
var Default Operations = &cliOperations{}

// Create implements Operations.Create.
func (c *cliOperations) Create(ctx context.Context, repoPath, worktreePath, branch, baseBranch string) error {
	args := []string{"-C", repoPath, "worktree", "add", worktreePath, "-b", branch}
	if baseBranch != "" {
		args = append(args, baseBranch)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree: %w\n%s", err, output)
	}
	return nil
}

// Create creates a new worktree at worktreePath from repoPath with a new branch.
// If baseBranch is non-empty, the new branch is created from that base.
// Uses: git -C <repo> worktree add <path> -b <branch> [<base>]
func Create(ctx context.Context, repoPath, worktreePath, branch, baseBranch string) error {
	return Default.Create(ctx, repoPath, worktreePath, branch, baseBranch)
}

// CreateFromExisting implements Operations.CreateFromExisting.
func (c *cliOperations) CreateFromExisting(ctx context.Context, repoPath, worktreePath, branch string) error {
	args := []string{"-C", repoPath, "worktree", "add", worktreePath, branch}
	cmd := exec.CommandContext(ctx, "git", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree from existing branch: %w\n%s", err, output)
	}
	return nil
}

// CreateFromExisting creates a worktree at worktreePath for an existing branch.
// If the branch only exists on remote (not locally), git will auto-track origin/<branch>.
// Uses: git -C <repo> worktree add <path> <branch>
func CreateFromExisting(ctx context.Context, repoPath, worktreePath, branch string) error {
	return Default.CreateFromExisting(ctx, repoPath, worktreePath, branch)
}

// RemoveForce implements Operations.RemoveForce.
func (c *cliOperations) RemoveForce(ctx context.Context, repoPath, worktreePath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "remove", "--force", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to force remove worktree: %w\n%s", err, output)
	}
	return nil
}

// RemoveForce forcefully removes a worktree even if it has uncommitted changes.
// Uses: git -C <repo> worktree remove --force <path>
func RemoveForce(ctx context.Context, repoPath, worktreePath string) error {
	return Default.RemoveForce(ctx, repoPath, worktreePath)
}

// List implements Operations.List.
func (c *cliOperations) List(ctx context.Context, repoPath string) ([]Worktree, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	return parseWorktreeList(string(output))
}

// List returns all worktrees for the given repository.
// Uses: git -C <repo> worktree list --porcelain
func List(ctx context.Context, repoPath string) ([]Worktree, error) {
	return Default.List(ctx, repoPath)
}

// parseWorktreeList parses the porcelain output of git worktree list.
// Format:
// worktree /path/to/worktree
// HEAD <sha>
// branch refs/heads/<name>
// (blank line)
func parseWorktreeList(output string) ([]Worktree, error) {
	var worktrees []Worktree
	var current Worktree

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// End of worktree entry
			if current.Path != "" {
				worktrees = append(worktrees, current)
			}
			current = Worktree{}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "HEAD ") {
			current.HEAD = strings.TrimPrefix(line, "HEAD ")
		} else if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			// Strip refs/heads/ prefix to get branch name
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
		}
		// Ignore "detached" and other entries
	}

	// Don't forget the last entry if output doesn't end with blank line
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees, scanner.Err()
}

// ExistsPath implements Operations.ExistsPath.
func (c *cliOperations) ExistsPath(worktreePath string) bool {
	info, err := os.Stat(worktreePath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ExistsPath checks if the worktree path exists on disk.
func ExistsPath(worktreePath string) bool {
	return Default.ExistsPath(worktreePath)
}
