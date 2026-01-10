package mise

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsEnabled checks if mise is configured in the given directory.
// Returns true if any mise config file exists.
func IsEnabled(dir string) bool {
	configFiles := []string{
		".mise.toml",
		"mise.toml",
		".mise/config.toml",
		".tool-versions",
	}

	for _, file := range configFiles {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	return false
}

// Trust runs `mise trust` in the given directory.
func Trust(dir string) error {
	cmd := exec.Command("mise", "trust")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mise trust failed: %w\n%s", err, output)
	}
	return nil
}

// Install runs `mise install` in the given directory.
func Install(dir string) error {
	cmd := exec.Command("mise", "install")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mise install failed: %w\n%s", err, output)
	}
	return nil
}

// HasTask checks if a mise task exists in the given directory.
func HasTask(dir, taskName string) bool {
	cmd := exec.Command("mise", "task", "ls")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// Check if task name appears in output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == taskName {
			return true
		}
	}
	return false
}

// RunTask runs a mise task in the given directory.
func RunTask(dir, taskName string) error {
	cmd := exec.Command("mise", "run", taskName)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mise run %s failed: %w\n%s", taskName, err, output)
	}
	return nil
}

// Initialize runs mise trust, install, and setup task if available.
// Returns nil if mise is not enabled in the directory.
// Errors are returned but callers may choose to treat them as warnings.
func Initialize(dir string) error {
	if !IsEnabled(dir) {
		return nil
	}

	if err := Trust(dir); err != nil {
		return err
	}

	if err := Install(dir); err != nil {
		return err
	}

	// Run setup task if it exists
	if HasTask(dir, "setup") {
		if err := RunTask(dir, "setup"); err != nil {
			return err
		}
	}

	return nil
}
