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
