package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var (
	claudeAutoClose bool
)

var claudeCmd = &cobra.Command{
	Use:    "claude <task-id>",
	Short:  "Wrapper for Claude execution with state tracking",
	Long:   `Internal wrapper that tracks Claude execution state, timing, and exit codes.`,
	Hidden: true, // Hide from normal help since it's internal
	Args:   cobra.ExactArgs(1),
	RunE:   runClaude,
}

func init() {
	claudeCmd.Flags().BoolVar(&claudeAutoClose, "auto-close", false, "automatically close tab after completion")
}

func runClaude(cmd *cobra.Command, args []string) error {
	taskID := args[0]
	ctx := GetContext()

	// Find project
	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	// Get task to verify it exists
	task, err := proj.DB.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task %s: %w", taskID, err)
	}
	if task == nil {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Note: StartTask already sets status to 'processing' in the database
	// We're just tracking the actual Claude execution here
	startTime := time.Now()
	fmt.Printf("Starting Claude for task %s at %s\n", taskID, startTime.Format("15:04:05"))

	// Set up Claude command
	claudeCmd := exec.Command("claude", "--dangerously-skip-permissions")
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr

	// Start Claude
	if err := claudeCmd.Start(); err != nil {
		proj.DB.FailTask(ctx, taskID, fmt.Sprintf("Failed to start Claude: %v", err))
		return fmt.Errorf("failed to start Claude: %w", err)
	}

	// Wait for Claude to complete, task completion in database, or context cancellation (signal)
	done := make(chan error, 1)
	go func() {
		done <- claudeCmd.Wait()
	}()

	// Poll database for task completion
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var exitErr error
	for {
		select {
		case <-ticker.C:
			// Check if task is marked as completed in database
			task, err := proj.DB.GetTask(ctx, taskID)
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

					if task.Status == db.StatusFailed {
						exitErr = fmt.Errorf("task marked as failed: %s", task.ErrorMessage)
					}
					goto exit
				}
			}

		case <-ctx.Done():
			// Context cancelled (SIGINT/SIGTERM received via root context)
			fmt.Println("\nReceived interrupt, terminating Claude...")
			claudeCmd.Process.Signal(syscall.SIGTERM)

			// Give it some time to exit gracefully
			select {
			case <-done:
				// Claude exited gracefully
			case <-time.After(2 * time.Second):
				// Force kill if still running
				fmt.Println("Claude didn't exit gracefully, force killing...")
				claudeCmd.Process.Kill()
				<-done // Wait for process to actually exit
			}
			exitErr = fmt.Errorf("interrupted by signal")
			goto exit

		case err := <-done:
			// Claude exited on its own
			exitErr = err
			goto exit
		}
	}

exit:

	// Calculate duration
	endTime := time.Now()
	duration := endTime.Sub(startTime)

	// Update task status based on exit code
	if exitErr != nil {
		// Claude exited with error
		fmt.Printf("Claude exited with error for task %s after %v: %v\n", taskID, duration, exitErr)
		proj.DB.FailTask(ctx, taskID, fmt.Sprintf("Claude exited with error after %v: %v", duration, exitErr))
		return exitErr
	}

	// Claude exited successfully - but did it complete the task?
	fmt.Printf("Claude exited cleanly for task %s after %v\n", taskID, duration)

	// Re-fetch task to check final status
	task, err = proj.DB.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to check final task status: %w", err)
	}

	if task.Status != db.StatusCompleted {
		// Claude didn't complete the assigned work - mark as failed
		failureMsg := fmt.Sprintf("Claude exited without completing the task after %v", duration)
		fmt.Printf("ERROR: %s\n", failureMsg)

		// Mark task as failed
		if err := proj.DB.FailTask(ctx, taskID, failureMsg); err != nil {
			fmt.Printf("Warning: failed to mark task as failed: %v\n", err)
		}

		fmt.Printf("\nTask %s has been marked as failed.\n", taskID)
		fmt.Printf("To retry, run: co task reset %s && co run %s\n", taskID, taskID)

		// Return error to indicate failure
		return fmt.Errorf("task not completed by Claude")
	}

	// Task was completed successfully
	fmt.Printf("Task %s completed successfully after %v\n", taskID, duration)

	// Close the tab if auto-close is enabled
	if claudeAutoClose {
		fmt.Println("Auto-closing tab...")
		sessionName := claude.SessionNameForProject(proj.Config.Project.Name)

		// Close the current tab (the one this wrapper is running in)
		closeArgs := []string{"-s", sessionName, "action", "close-tab"}
		closeCmd := exec.Command("zellij", closeArgs...)
		if err := closeCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to auto-close tab: %v\n", err)
		}
	}

	return nil
}
