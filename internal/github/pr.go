package github

import (
	"fmt"
	"os/exec"
	"strings"
)

// CreatePR creates a pull request from the specified branch to the base branch.
// Returns the PR URL on success.
func CreatePR(branch, base, title, body string) (string, error) {
	return CreatePRInDir(branch, base, title, body, "")
}

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

// MergePR merges the pull request at the given URL.
func MergePR(prURL string) error {
	return MergePRInDir(prURL, "")
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
