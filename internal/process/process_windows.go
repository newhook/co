//go:build windows
// +build windows

package process

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// getProcessList returns a list of all running processes with their command lines.
// On Windows, we use 'wmic' command to get the process list.
func getProcessList(ctx context.Context) ([]string, error) {
	// Use wmic to get all processes with full command line
	cmd := exec.CommandContext(ctx, "wmic", "process", "get", "CommandLine", "/format:list")
	output, err := cmd.Output()
	if err != nil {
		// Try alternative command if wmic fails
		cmd = exec.CommandContext(ctx, "powershell", "-Command", "Get-Process | Select-Object -Property ProcessName,CommandLine | Format-List")
		output, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to execute process list command: %w", err)
		}
	}

	lines := strings.Split(string(output), "\n")
	var processes []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Extract command line from wmic output
		if strings.HasPrefix(line, "CommandLine=") {
			cmdLine := strings.TrimPrefix(line, "CommandLine=")
			if cmdLine != "" {
				processes = append(processes, cmdLine)
			}
		} else if line != "" && !strings.HasPrefix(line, "ProcessName") {
			// For PowerShell output
			processes = append(processes, line)
		}
	}

	return processes, nil
}

// escapeForPowerShell escapes a string for safe use in PowerShell commands
func escapeForPowerShell(s string) string {
	// Escape single quotes by doubling them (PowerShell escaping convention)
	// and wrap the whole string in single quotes for safety
	escaped := strings.ReplaceAll(s, "'", "''")
	return escaped
}

// killProcessByPattern kills all processes matching the given pattern.
func killProcessByPattern(ctx context.Context, pattern string) error {
	// Get process list to find PIDs
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

	// Use taskkill with filter to kill processes
	// Escape the pattern to prevent command injection in PowerShell
	escapedPattern := escapeForPowerShell(pattern)
	// Use single quotes and properly escaped pattern to prevent injection
	psScript := fmt.Sprintf(`Get-Process | Where-Object { $_.CommandLine -like '*%s*' } | Stop-Process -Force`, escapedPattern)
	cmd := exec.CommandContext(ctx, "powershell", "-Command", psScript)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		// Try alternative approach with taskkill
		// This is less precise but works when PowerShell scripting is restricted
		// Extract the executable name from the pattern (first word)
		exeName := pattern
		if idx := strings.IndexAny(pattern, " \t"); idx > 0 {
			exeName = pattern[:idx]
		}
		// Add .exe extension if not present and add wildcards for taskkill
		if !strings.HasSuffix(strings.ToLower(exeName), ".exe") {
			exeName = exeName + ".exe"
		}
		// Use the actual executable name instead of hardcoded "*co*"
		cmd = exec.CommandContext(ctx, "taskkill", "/F", "/IM", exeName)
		if err2 := cmd.Run(); err2 != nil {
			return fmt.Errorf("failed to kill process: %w, stderr: %s", err, stderr.String())
		}
	}

	return nil
}
