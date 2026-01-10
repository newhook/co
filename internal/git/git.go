package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// GetCurrentBranch returns the name of the current git branch.
func GetCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// Checkout switches to the specified branch.
func Checkout(branch string) error {
	cmd := exec.Command("git", "checkout", branch)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
	}
	return nil
}

// DeleteBranch deletes the specified branch locally.
func DeleteBranch(branch string) error {
	cmd := exec.Command("git", "branch", "-D", branch)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branch, err)
	}
	return nil
}

// Push pushes the specified branch to origin.
func Push(branch string) error {
	cmd := exec.Command("git", "push", "-u", "origin", branch)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branch, err)
	}
	return nil
}

// CreateBranch creates a new branch from the current HEAD and switches to it.
// If the branch already exists, it checks out the existing branch instead.
func CreateBranch(branch string) error {
	// Try to create new branch
	cmd := exec.Command("git", "checkout", "-b", branch)
	if err := cmd.Run(); err != nil {
		// Branch likely exists, try to check it out
		if checkoutErr := Checkout(branch); checkoutErr != nil {
			return fmt.Errorf("failed to create or checkout branch %s: %w", branch, err)
		}
	}
	return nil
}

// HasChanges returns true if there are uncommitted changes in the working tree.
func HasChanges() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check git status: %w", err)
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// HasCommitsAhead returns true if the current branch has commits ahead of the given base branch.
func HasCommitsAhead(base string) (bool, error) {
	cmd := exec.Command("git", "rev-list", "--count", base+"..HEAD")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check commits ahead: %w", err)
	}
	count := strings.TrimSpace(string(output))
	return count != "0", nil
}

// Pull pulls the latest changes from the remote for the current branch.
func Pull() error {
	cmd := exec.Command("git", "pull")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}
	return nil
}
