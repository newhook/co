package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
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
	// Add any flags here if needed (e.g., --no-auto-close)
}

func runClaude(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	// Find project
	proj, err := project.Find("")
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	// Open database
	database, err := proj.OpenDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	ctx := context.Background()

	// Get task to verify it exists
	task, err := database.GetTask(ctx, taskID)
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

	// Handle signals gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start Claude
	if err := claudeCmd.Start(); err != nil {
		database.FailTask(ctx, taskID, fmt.Sprintf("Failed to start Claude: %v", err))
		return fmt.Errorf("failed to start Claude: %w", err)
	}

	// Wait for Claude to complete or for signal
	done := make(chan error, 1)
	go func() {
		done <- claudeCmd.Wait()
	}()

	var exitErr error
	select {
	case <-sigChan:
		// Received signal, pass it to Claude
		fmt.Println("\nReceived interrupt, terminating Claude...")
		claudeCmd.Process.Signal(syscall.SIGTERM)

		// Give it some time to exit gracefully
		time.Sleep(2 * time.Second)

		// Force kill if still running
		claudeCmd.Process.Kill()
		exitErr = fmt.Errorf("interrupted by signal")

	case err := <-done:
		exitErr = err
	}

	// Calculate duration
	endTime := time.Now()
	duration := endTime.Sub(startTime)

	// Update task status based on exit code
	if exitErr != nil {
		// Claude exited with error
		fmt.Printf("Claude exited with error for task %s after %v: %v\n", taskID, duration, exitErr)
		database.FailTask(ctx, taskID, fmt.Sprintf("Claude exited with error after %v: %v", duration, exitErr))
		return exitErr
	}

	// Claude completed successfully
	fmt.Printf("Claude completed successfully for task %s after %v\n", taskID, duration)

	// Note: The actual task completion is handled by Claude calling `co complete`
	// We just update that Claude itself ran successfully
	if task.Status != db.StatusCompleted {
		// Task wasn't marked complete by Claude - this might be a partial completion
		fmt.Printf("Warning: Task %s was not marked as completed by Claude\n", taskID)
	}

	return nil
}