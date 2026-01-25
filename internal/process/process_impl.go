package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// systemProcessLister implements ProcessLister using system commands.
type systemProcessLister struct{}

// GetProcessList returns a list of all running processes with their command lines.
func (s *systemProcessLister) GetProcessList(ctx context.Context) ([]string, error) {
	return getProcessList(ctx)
}

// systemProcessKiller implements ProcessKiller using system commands.
type systemProcessKiller struct{}

// KillByPattern kills processes matching the pattern using pkill.
func (s *systemProcessKiller) KillByPattern(ctx context.Context, pattern string) error {
	return killByPattern(ctx, pattern)
}

// getProcessList returns a list of all running processes with their command lines.
// On Unix systems, we use 'ps' command to get the process list.
func getProcessList(ctx context.Context) ([]string, error) {
	// Use ps to get all processes with full command line
	// -e: all processes, -o comm=: command only, -w: wide output (no truncation)
	cmd := exec.CommandContext(ctx, "ps", "-ewo", "command")
	output, err := cmd.Output()
	if err != nil {
		// If ps with -w flag fails, try without it (some systems may not support it)
		cmd = exec.CommandContext(ctx, "ps", "-eo", "command")
		output, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to execute ps command: %w", err)
		}
	}

	lines := strings.Split(string(output), "\n")
	// Remove header line if present and empty lines
	var processes []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(strings.ToUpper(line), "COMMAND") {
			processes = append(processes, line)
		}
	}

	return processes, nil
}

// escapePattern safely escapes a pattern for use in shell commands.
// It uses single quotes to prevent shell expansion and handles embedded single quotes.
func escapePattern(pattern string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote)
	// This is the standard way to include a single quote within a single-quoted string
	escaped := strings.ReplaceAll(pattern, "'", "'\\''")
	// Wrap the entire pattern in single quotes
	return "'" + escaped + "'"
}

// killByPattern kills all processes matching the given pattern using pkill.
func killByPattern(ctx context.Context, pattern string) error {
	// Escape the pattern to prevent command injection
	escapedPattern := escapePattern(pattern)

	// Use pkill to kill processes matching the pattern
	// -f flag matches against the full command line
	// We use sh -c to properly handle the escaped pattern
	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("pkill -f %s", escapedPattern))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Check if the error is because no processes were found (exit code 1)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 1 {
				// No processes matched, this is not an error
				return nil
			}
		}
		return fmt.Errorf("failed to kill process: %w, stderr: %s", err, stderr.String())
	}

	return nil
}
