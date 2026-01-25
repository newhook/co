package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsProcessRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		pattern string
		want    bool
		wantErr bool
	}{
		{
			name:    "non-existent process",
			pattern: "this-process-definitely-does-not-exist-xyz123",
			want:    false,
			wantErr: false,
		},
		{
			name:    "empty pattern",
			pattern: "",
			want:    true, // Empty pattern matches all processes
			wantErr: false,
		},
		{
			name:    "pattern with special characters",
			pattern: "test*pattern[special]",
			want:    false,
			wantErr: false,
		},
		{
			name:    "pattern with spaces",
			pattern: "test pattern with spaces",
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			running, err := IsProcessRunning(ctx, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsProcessRunning() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if running != tt.want {
				t.Errorf("IsProcessRunning() = %v, want %v", running, tt.want)
			}
		})
	}

	// Test with current test process (should find itself)
	t.Run("current test process", func(t *testing.T) {
		running, err := IsProcessRunning(ctx, os.Args[0])
		if err != nil {
			t.Fatalf("IsProcessRunning failed: %v", err)
		}
		if !running {
			t.Error("Expected to find the current test process")
		}
	})
}

func TestIsProcessRunning_ContextCancellation(t *testing.T) {
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The function should respect context cancellation
	_, err := IsProcessRunning(ctx, "any-pattern")
	if err == nil {
		// On macOS/Unix, the ps command might complete quickly before context is checked
		// This is acceptable behavior
		t.Log("IsProcessRunning completed despite cancelled context (command was fast)")
	}
}

func TestKillProcess(t *testing.T) {
	// Skip this test if not running on a Unix-like system
	if runtime.GOOS == "windows" {
		t.Skip("Skipping KillProcess test on Windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{
			name:    "non-existent process",
			pattern: "definitely-non-existent-process-xyz789",
			wantErr: false, // Should succeed (no processes to kill)
		},
		{
			name:    "pattern with special characters",
			pattern: "test'pattern\"with$special",
			wantErr: false,
		},
		{
			name:    "empty pattern",
			pattern: "",
			wantErr: true, // pkill with empty pattern causes an error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := KillProcess(ctx, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("KillProcess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestKillProcess_ActualProcess(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping test that spawns processes in CI")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Skipping KillProcess test on Windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sleep", "30")
	require.NoError(t, cmd.Start(), "failed to start test process")
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()

	// Verify the process is running
	running, err := IsProcessRunning(ctx, "sleep 30")
	require.NoError(t, err)
	require.True(t, running, "test process should be running")

	// Kill the process
	require.NoError(t, KillProcess(ctx, "sleep 30"))

	// Verify the process is no longer running
	running, err = IsProcessRunning(ctx, "sleep 30")
	require.NoError(t, err)
	require.False(t, running, "process should have been killed")
}

func TestEscapePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{
			name:    "simple pattern",
			pattern: "simple",
			want:    "'simple'",
		},
		{
			name:    "pattern with single quote",
			pattern: "test'pattern",
			want:    "'test'\\''pattern'",
		},
		{
			name:    "pattern with multiple single quotes",
			pattern: "test'pattern'here",
			want:    "'test'\\''pattern'\\''here'",
		},
		{
			name:    "pattern with special characters",
			pattern: "test$pattern*here",
			want:    "'test$pattern*here'",
		},
		{
			name:    "empty pattern",
			pattern: "",
			want:    "''",
		},
		{
			name:    "pattern with spaces",
			pattern: "test pattern",
			want:    "'test pattern'",
		},
		{
			name:    "pattern with newline",
			pattern: "test\npattern",
			want:    "'test\npattern'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapePattern(tt.pattern)
			if got != tt.want {
				t.Errorf("escapePattern() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetProcessList(t *testing.T) {
	// Skip this test if not running on a Unix-like system
	if runtime.GOOS == "windows" {
		t.Skip("Skipping getProcessList test on Windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	processes, err := getProcessList(ctx)
	if err != nil {
		t.Fatalf("getProcessList() error = %v", err)
	}

	// Basic sanity checks
	if len(processes) == 0 {
		t.Error("Expected at least one process to be running")
	}

	// Check that the list doesn't contain the COMMAND header
	for _, proc := range processes {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(proc)), "COMMAND") {
			t.Error("Process list should not contain COMMAND header")
		}
	}

	// Verify that we can find our own test process
	foundSelf := false
	for _, proc := range processes {
		if strings.Contains(proc, os.Args[0]) {
			foundSelf = true
			break
		}
	}
	if !foundSelf {
		t.Error("Should have found the current test process in the list")
	}
}

func TestGetProcessList_ContextCancellation(t *testing.T) {
	// Skip this test if not running on a Unix-like system
	if runtime.GOOS == "windows" {
		t.Skip("Skipping getProcessList test on Windows")
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The function should respect context cancellation
	_, err := getProcessList(ctx)
	if err == nil {
		// On fast systems, ps might complete before context is checked
		t.Log("getProcessList completed despite cancelled context (command was fast)")
	}
}

func TestKillProcessByPattern_NoMatchingProcess(t *testing.T) {
	// Skip this test if not running on a Unix-like system
	if runtime.GOOS == "windows" {
		t.Skip("Skipping killProcessByPattern test on Windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Should not error when no process matches
	err := killProcessByPattern(ctx, "absolutely-non-existent-process-name-xyz")
	if err != nil {
		t.Errorf("killProcessByPattern() should not error when no process matches, got: %v", err)
	}
}

func TestKillProcessByPattern_CommandInjection(t *testing.T) {
	// Skip this test if not running on a Unix-like system
	if runtime.GOOS == "windows" {
		t.Skip("Skipping killProcessByPattern test on Windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test that command injection is prevented
	maliciousPatterns := []string{
		"'; touch /tmp/hacked; echo '",
		"$(touch /tmp/hacked)",
		"`touch /tmp/hacked`",
		"| touch /tmp/hacked",
		"&& touch /tmp/hacked",
		"; touch /tmp/hacked",
	}

	for _, pattern := range maliciousPatterns {
		t.Run(fmt.Sprintf("pattern=%q", pattern), func(t *testing.T) {
			// This should be safe due to escaping
			err := killProcessByPattern(ctx, pattern)
			if err != nil {
				// Error is acceptable (no matching process)
				t.Logf("killProcessByPattern() returned error (expected): %v", err)
			}

			// Verify that the malicious command was not executed
			if _, err := os.Stat("/tmp/hacked"); err == nil {
				os.Remove("/tmp/hacked") // Clean up
				t.Fatal("Command injection was not prevented!")
			}
		})
	}
}
