package github

import (
	"fmt"
	"os/exec"
	"strings"
)

// CreatePRInDir creates a pull request in a specific directory.
func CreatePRInDir(branch, base, title, body, dir string) (string, error) {
	cmd := exec.Command("gh", "pr", "create",
		"--head", branch,
		"--base", base,
		"--title", title,
		"--body", body,
	)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// MergePRInDir merges the pull request in a specific directory.
func MergePRInDir(prURL, dir string) error {
	cmd := exec.Command("gh", "pr", "merge", prURL, "--merge", "--delete-branch")
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to merge PR %s: %w", prURL, err)
	}
	return nil
}

// UpdatePRInDir updates the title and body of a pull request in a specific directory.
func UpdatePRInDir(prURL, title, body, dir string) error {
	cmd := exec.Command("gh", "pr", "edit", prURL,
		"--title", title,
		"--body", body,
	)
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update PR %s: %w", prURL, err)
	}
	return nil
}
