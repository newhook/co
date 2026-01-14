package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/newhook/co/internal/db"
)

// Run executes Claude directly in the current terminal (fork/exec).
// This blocks until Claude exits or the task is marked complete in the database.
func Run(ctx context.Context, database *db.DB, taskID string, prompt string, workDir string) error {
	// Get task to verify it exists
	task, err := database.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task %s: %w", taskID, err)
	}
	if task == nil {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Mark task as processing
	if err := database.StartTask(ctx, taskID); err != nil {
		return fmt.Errorf("failed to start task: %w", err)
	}

	startTime := time.Now()
	fmt.Printf("\n=== Starting Claude for task %s at %s ===\n", taskID, startTime.Format("15:04:05"))

	// Set up Claude command with prompt as argument
	claudeArgs := []string{"--dangerously-skip-permissions", prompt}
	claudeCmd := exec.CommandContext(ctx, "claude", claudeArgs...)
	claudeCmd.Dir = workDir
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr

	// Start Claude
	if err := claudeCmd.Start(); err != nil {
		if dbErr := database.FailTask(ctx, taskID, fmt.Sprintf("failed to start Claude: %v", err)); dbErr != nil {
			fmt.Printf("Warning: failed to mark task as failed: %v\n", dbErr)
		}
		return fmt.Errorf("failed to start Claude: %w", err)
	}

	// Run the main monitoring loop
	return monitorClaude(ctx, database, taskID, claudeCmd, startTime)
}

// monitorClaude handles the main event loop for monitoring Claude execution.
// It watches for Claude exit, task completion in database, signals, and context cancellation.
func monitorClaude(ctx context.Context, database *db.DB, taskID string, claudeCmd *exec.Cmd, startTime time.Time) error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Wait for Claude to complete
	done := make(chan error, 1)
	go func() {
		done <- claudeCmd.Wait()
	}()

	// Poll database for task completion
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			// Claude exited on its own - no termination needed
			return handleClaudeExit(ctx, database, taskID, err, startTime)

		case <-ticker.C:
			// Check if task is marked as completed in database
			task, err := database.GetTask(ctx, taskID)
			if err != nil {
				fmt.Printf("Warning: failed to check task status: %v\n", err)
				continue
			}
			if task == nil {
				fmt.Printf("\nTask %s no longer exists, terminating Claude...\n", taskID)
				terminateGracefully(claudeCmd, done)
				return fmt.Errorf("task %s was deleted", taskID)
			}
			if task.Status == db.StatusCompleted || task.Status == db.StatusFailed {
				fmt.Printf("\nTask marked as %s in database, terminating Claude...\n", task.Status)
				terminateGracefully(claudeCmd, done)
				elapsed := time.Since(startTime)
				fmt.Printf("\n=== Task %s %s (took %s) ===\n", taskID, task.Status, elapsed.Round(time.Second))
				return nil
			}

		case sig := <-sigChan:
			fmt.Printf("\nReceived signal %v, forwarding to Claude...\n", sig)
			if sysSig, ok := sig.(syscall.Signal); ok {
				claudeCmd.Process.Signal(sysSig)
			} else {
				claudeCmd.Process.Signal(syscall.SIGTERM)
			}
			terminateGracefully(claudeCmd, done)
			return fmt.Errorf("interrupted by signal %v", sig)

		case <-ctx.Done():
			fmt.Println("\nContext cancelled, terminating Claude...")
			terminateGracefully(claudeCmd, done)
			return ctx.Err()
		}
	}
}

// terminateGracefully sends SIGTERM and waits for exit, force killing after 5 seconds.
func terminateGracefully(cmd *exec.Cmd, done <-chan error) {
	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
		// Exited gracefully
	case <-time.After(5 * time.Second):
		fmt.Println("Claude didn't exit gracefully, force killing...")
		cmd.Process.Kill()
		<-done
	}
}

// handleClaudeExit processes Claude's exit and returns the appropriate result.
func handleClaudeExit(ctx context.Context, database *db.DB, taskID string, exitErr error, startTime time.Time) error {
	elapsed := time.Since(startTime)

	if exitErr != nil {
		// Check if it was killed by us due to completion
		task, dbErr := database.GetTask(ctx, taskID)
		if dbErr == nil && task != nil && (task.Status == db.StatusCompleted || task.Status == db.StatusFailed) {
			fmt.Printf("\n=== Task %s %s (took %s) ===\n", taskID, task.Status, elapsed.Round(time.Second))
			return nil
		}
		// Actual error
		if dbErr := database.FailTask(ctx, taskID, fmt.Sprintf("Claude exited with error: %v", exitErr)); dbErr != nil {
			fmt.Printf("Warning: failed to mark task as failed: %v\n", dbErr)
		}
		return fmt.Errorf("Claude exited with error: %w", exitErr)
	}

	// Claude exited successfully - check task status
	task, err := database.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task %s: %w", taskID, err)
	}
	if task != nil && task.Status == db.StatusCompleted {
		fmt.Printf("\n=== Task %s completed (took %s) ===\n", taskID, elapsed.Round(time.Second))
	} else if task != nil && task.Status == db.StatusFailed {
		fmt.Printf("\n=== Task %s failed (took %s) ===\n", taskID, elapsed.Round(time.Second))
	} else {
		fmt.Printf("\n=== Claude exited for task %s (took %s) ===\n", taskID, elapsed.Round(time.Second))
	}
	return nil
}
