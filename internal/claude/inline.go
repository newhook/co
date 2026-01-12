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

// RunInline executes Claude directly in the current terminal (fork/exec).
// This blocks until Claude exits or the task is marked complete in the database.
// Unlike Run(), this does not create a separate zellij tab.
func RunInline(ctx context.Context, database *db.DB, taskID string, prompt string, workDir string) error {
	// Get task to verify it exists
	task, err := database.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task %s: %w", taskID, err)
	}
	if task == nil {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Mark task as processing (empty session/pane since we're running inline)
	if err := database.StartTask(ctx, taskID, "", ""); err != nil {
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
		database.FailTask(ctx, taskID, fmt.Sprintf("Failed to start Claude: %v", err))
		return fmt.Errorf("failed to start Claude: %w", err)
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Wait for Claude to complete, task completion in database, or signal
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
			// Claude exited
			elapsed := time.Since(startTime)
			if err != nil {
				// Check if it was killed by us due to completion
				task, dbErr := database.GetTask(ctx, taskID)
				if dbErr == nil && task != nil && (task.Status == db.StatusCompleted || task.Status == db.StatusFailed) {
					fmt.Printf("\n=== Task %s %s (took %s) ===\n", taskID, task.Status, elapsed.Round(time.Second))
					return nil
				}
				// Actual error
				database.FailTask(ctx, taskID, fmt.Sprintf("Claude exited with error: %v", err))
				return fmt.Errorf("Claude exited with error: %w", err)
			}
			// Claude exited successfully - check task status
			task, _ := database.GetTask(ctx, taskID)
			if task != nil && task.Status == db.StatusCompleted {
				fmt.Printf("\n=== Task %s completed (took %s) ===\n", taskID, elapsed.Round(time.Second))
			} else if task != nil && task.Status == db.StatusFailed {
				fmt.Printf("\n=== Task %s failed (took %s) ===\n", taskID, elapsed.Round(time.Second))
			} else {
				fmt.Printf("\n=== Claude exited for task %s (took %s) ===\n", taskID, elapsed.Round(time.Second))
			}
			return nil

		case <-ticker.C:
			// Check if task is marked as completed in database
			task, err := database.GetTask(ctx, taskID)
			if err == nil && task != nil {
				if task.Status == db.StatusCompleted || task.Status == db.StatusFailed {
					fmt.Printf("\nTask marked as %s in database, terminating Claude...\n", task.Status)

					// Send SIGTERM to Claude
					claudeCmd.Process.Signal(syscall.SIGTERM)

					// Give it 5 seconds to exit gracefully
					select {
					case <-done:
						// Claude exited gracefully
					case <-time.After(5 * time.Second):
						// Force kill if still running
						fmt.Println("Claude didn't exit gracefully, force killing...")
						claudeCmd.Process.Kill()
						<-done // Wait for process to actually exit
					}

					elapsed := time.Since(startTime)
					fmt.Printf("\n=== Task %s %s (took %s) ===\n", taskID, task.Status, elapsed.Round(time.Second))
					return nil
				}
			}

		case sig := <-sigChan:
			fmt.Printf("\nReceived signal %v, forwarding to Claude...\n", sig)
			claudeCmd.Process.Signal(sig)

			// Wait for Claude to exit
			select {
			case <-done:
				// Claude exited
			case <-time.After(5 * time.Second):
				fmt.Println("Claude didn't exit, force killing...")
				claudeCmd.Process.Kill()
				<-done
			}

			return fmt.Errorf("interrupted by signal %v", sig)

		case <-ctx.Done():
			fmt.Println("\nContext cancelled, terminating Claude...")
			claudeCmd.Process.Signal(syscall.SIGTERM)

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				claudeCmd.Process.Kill()
				<-done
			}

			return ctx.Err()
		}
	}
}
