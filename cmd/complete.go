package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var (
	flagCompletePRURL   string
	flagCompleteProject string
	flagCompleteError   string
)

var completeCmd = &cobra.Command{
	Use:   "complete <bead-id|task-id>",
	Short: "Mark a bead or task as completed (or failed with --error)",
	Long:  `Mark a bead or task as completed in the tracking proj.DB. Called by Claude Code when work is done.
With --error flag, marks the task as failed instead.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runComplete,
}

func init() {
	completeCmd.Flags().StringVar(&flagCompletePRURL, "pr", "", "PR URL to associate with completion")
	completeCmd.Flags().StringVar(&flagCompleteProject, "project", "", "project directory (default: auto-detect from cwd)")
	completeCmd.Flags().StringVar(&flagCompleteError, "error", "", "Error message to mark task as failed")
}

func runComplete(cmd *cobra.Command, args []string) error {
	id := args[0]

	proj, err := project.Find(flagCompleteProject)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	// If error flag is set, mark task as failed
	if flagCompleteError != "" {
		// Try to fail it as a task
		if err := proj.DB.FailTask(context.Background(), id, flagCompleteError); err == nil {
			fmt.Printf("Task %s marked as failed: %s\n", id, flagCompleteError)
			return nil
		}
		// If that didn't work, it might not be a valid task ID
		return fmt.Errorf("failed to mark %s as failed (is it a valid task ID?)", id)
	}

	// Check if this is a task ID (contains a dot like "w-xxx.1" or "w-xxx.pr")
	if strings.Contains(id, ".") {
		// Try to complete as a task directly
		if err := proj.DB.CompleteTask(context.Background(), id, flagCompletePRURL); err == nil {
			fmt.Printf("Task %s marked as completed", id)
			if flagCompletePRURL != "" {
				fmt.Printf(" (PR: %s)", flagCompletePRURL)
			}
			fmt.Println()
			return nil
		}
		// Fall through to try as bead ID if task completion failed
	}

	// Otherwise, continue with normal bead completion logic
	beadID := id

	// Check if this bead is part of a task
	taskID, err := proj.DB.GetTaskForBead(context.Background(), beadID)
	if err != nil {
		return fmt.Errorf("failed to look up task for bead: %w", err)
	}

	if taskID != "" {
		// Bead is part of a task - mark it complete in task_beads
		if err := proj.DB.CompleteTaskBead(context.Background(), taskID, beadID); err != nil {
			return fmt.Errorf("failed to complete task bead: %w", err)
		}
		fmt.Printf("Marked bead %s as completed in task %s\n", beadID, taskID)

		// Check if all beads in the task are complete and auto-complete the task
		autoCompleted, err := proj.DB.CheckAndCompleteTask(context.Background(), taskID, flagCompletePRURL)
		if err != nil {
			return fmt.Errorf("failed to check task completion: %w", err)
		}
		if autoCompleted {
			fmt.Printf("All beads complete - task %s marked as completed", taskID)
			if flagCompletePRURL != "" {
				fmt.Printf(" (PR: %s)", flagCompletePRURL)
			}
			fmt.Println()
		}

		// Also update the beads table if the bead exists there (backwards compatibility)
		// Ignore "not found" errors since task_beads is the primary tracking for task-based beads
		_ = proj.DB.CompleteBead(beadID, flagCompletePRURL)
		return nil
	}

	// Standalone bead (not part of a task) - must exist in beads table
	if err := proj.DB.CompleteBead(beadID, flagCompletePRURL); err != nil {
		// Check if this might be a bead ID that doesn't exist in our tracking
		return fmt.Errorf("failed to complete bead %s: %w\nHint: If the bead was closed via 'bd close', it may not be tracked here. Use 'co complete <task-id>' instead.", beadID, err)
	}

	fmt.Printf("Marked bead %s as completed", beadID)
	if flagCompletePRURL != "" {
		fmt.Printf(" (PR: %s)", flagCompletePRURL)
	}
	fmt.Println()

	return nil
}
