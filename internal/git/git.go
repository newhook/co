package git

import (
	"fmt"
	"os/exec"
)

// PushSetUpstreamInDir pushes the specified branch and sets upstream tracking.
func PushSetUpstreamInDir(branch, dir string) error {
	cmd := exec.Command("git", "push", "--set-upstream", "origin", branch)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push and set upstream for branch %s: %w\n%s", branch, err, output)
	}
	return nil
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
