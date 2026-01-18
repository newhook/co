// Package process provides cross-platform process detection utilities.
package process

import (
	"context"
	"fmt"
	"strings"
)

// IsProcessRunning checks if a process with the given pattern is running.
// The pattern is matched against the full command line of the process.
func IsProcessRunning(ctx context.Context, pattern string) (bool, error) {
	processes, err := getProcessList(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get process list: %w", err)
	}

	for _, proc := range processes {
		if strings.Contains(proc, pattern) {
			return true, nil
		}
	}

	return false, nil
}

// KillProcess kills all processes matching the given pattern.
// The pattern is matched against the full command line of the process.
func KillProcess(ctx context.Context, pattern string) error {
	return killProcessByPattern(ctx, pattern)
}
