package worktree

import (
	"bufio"
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

// Create creates a new worktree at worktreePath from repoPath with a new branch.
// Uses: git -C <repo> worktree add <path> -b <branch>
func Create(repoPath, worktreePath, branch string) error {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", worktreePath, "-b", branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree: %w\n%s", err, output)
	}
	return nil
}

// CreateFromBranch creates a new worktree at worktreePath from an existing branch.
// Uses: git -C <repo> worktree add <path> <branch>
func CreateFromBranch(repoPath, worktreePath, branch string) error {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", worktreePath, branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree: %w\n%s", err, output)
	}
	return nil
}

// Remove removes a worktree.
// Uses: git -C <repo> worktree remove <path>
func Remove(repoPath, worktreePath string) error {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "remove", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove worktree: %w\n%s", err, output)
	}
	return nil
}

// RemoveForce forcefully removes a worktree even if it has uncommitted changes.
// Uses: git -C <repo> worktree remove --force <path>
func RemoveForce(repoPath, worktreePath string) error {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to force remove worktree: %w\n%s", err, output)
	}
	return nil
}

// List returns all worktrees for the given repository.
// Uses: git -C <repo> worktree list --porcelain
func List(repoPath string) ([]Worktree, error) {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	return parseWorktreeList(string(output))
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

// Exists checks if a worktree exists at the given path.
func Exists(repoPath, worktreePath string) bool {
	worktrees, err := List(repoPath)
	if err != nil {
		return false
	}

	for _, wt := range worktrees {
		if wt.Path == worktreePath {
			return true
		}
	}
	return false
}

// ExistsPath checks if the worktree path exists on disk.
func ExistsPath(worktreePath string) bool {
	info, err := os.Stat(worktreePath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// Prune removes worktree administrative files for worktrees that no longer exist.
// Uses: git -C <repo> worktree prune
func Prune(repoPath string) error {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "prune")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to prune worktrees: %w\n%s", err, output)
	}
	return nil
}
