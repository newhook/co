package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// CheckoutInDir switches to the specified branch in a specific directory.
func CheckoutInDir(branch, dir string) error {
	cmd := exec.Command("git", "checkout", branch)
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
	}
	return nil
}

// PushInDir pushes the specified branch to origin in a specific directory.
func PushInDir(branch, dir string) error {
	cmd := exec.Command("git", "push", "-u", "origin", branch)
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branch, err)
	}
	return nil
}

// CreateBranchInDir creates a new branch in a specific directory.
func CreateBranchInDir(branch, dir string) error {
	// Try to create new branch
	cmd := exec.Command("git", "checkout", "-b", branch)
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		// Branch likely exists, try to check it out
		if checkoutErr := CheckoutInDir(branch, dir); checkoutErr != nil {
			return fmt.Errorf("failed to create or checkout branch %s: %w", branch, err)
		}
	}
	return nil
}

// HasCommitsAheadInDir returns true if the current branch has commits ahead of the given base branch in a specific directory.
func HasCommitsAheadInDir(base, dir string) (bool, error) {
	cmd := exec.Command("git", "rev-list", "--count", base+"..HEAD")
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check commits ahead: %w", err)
	}
	count := strings.TrimSpace(string(output))
	return count != "0", nil
}

// PullInDir pulls the latest changes in a specific directory.
func PullInDir(dir string) error {
	cmd := exec.Command("git", "pull")
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}
	return nil
}
