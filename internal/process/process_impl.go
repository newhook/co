package process

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

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

// killProcessByPattern kills all processes matching the given pattern.
func killProcessByPattern(ctx context.Context, pattern string) error {
	// First, find the processes that match the pattern
	processes, err := getProcessList(ctx)
	if err != nil {
		return fmt.Errorf("failed to get process list: %w", err)
	}

	// Check if any process matches the pattern
	found := false
	for _, proc := range processes {
		if strings.Contains(proc, pattern) {
			found = true
			break
		}
	}

	if !found {
		// No process found, nothing to kill
		return nil
	}

	// Use pkill to kill processes matching the pattern
	// -f flag matches against the full command line
	cmd := exec.CommandContext(ctx, "pkill", "-f", pattern)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		// Check if the error is because no processes were found (exit code 1)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				// No processes matched, this is not an error
				return nil
			}
		}
		return fmt.Errorf("failed to kill process: %w, stderr: %s", err, stderr.String())
	}

	return nil
}
