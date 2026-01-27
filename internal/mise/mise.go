package mise

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// configFiles lists all mise config file locations to check.
var configFiles = []string{
	".mise.toml",
	"mise.toml",
	".mise/config.toml",
	".tool-versions",
}

// findConfigFile returns the first mise config file found in dir, or empty string if none.
func findConfigFile(dir string) string {
	for _, file := range configFiles {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); err == nil {
			return file
		}
	}
	return ""
}

// IsManaged returns true if the directory has a mise config file.
func IsManaged(dir string) bool {
	return findConfigFile(dir) != ""
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
	return InitializeWithOutput(dir, os.Stdout)
}

// InitializeWithOutput runs mise trust, install, and setup task if available.
// Progress messages are written to the provided writer.
// Pass io.Discard to suppress output (useful for TUI contexts).
// Returns nil if mise is not enabled in the directory.
func InitializeWithOutput(dir string, w io.Writer) error {
	configFile := findConfigFile(dir)
	if configFile == "" {
		fmt.Fprintf(w, "  Mise: not enabled (no config file found)\n")
		return nil
	}

	fmt.Fprintf(w, "  Mise: found %s\n", configFile)

	fmt.Fprintf(w, "  Mise: running trust...\n")
	if err := Trust(dir); err != nil {
		return err
	}

	fmt.Fprintf(w, "  Mise: running install...\n")
	if err := Install(dir); err != nil {
		return err
	}

	// Run setup task if it exists
	if HasTask(dir, "setup") {
		fmt.Fprintf(w, "  Mise: running setup task...\n")
		if err := RunTask(dir, "setup"); err != nil {
			return err
		}
	}

	fmt.Fprintf(w, "  Mise: initialization complete\n")
	return nil
}
