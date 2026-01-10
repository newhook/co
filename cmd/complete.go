package cmd

import (
	"fmt"

	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var flagPRURL string

var completeCmd = &cobra.Command{
	Use:   "complete <bead-id>",
	Short: "Mark a bead as completed",
	Long:  `Mark a bead as completed in the tracking database. Called by Claude Code when a task is done.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runComplete,
}

func init() {
	completeCmd.Flags().StringVar(&flagPRURL, "pr", "", "PR URL to associate with completion")
}

func runComplete(cmd *cobra.Command, args []string) error {
	beadID := args[0]

	proj, err := project.FindWithFlag("")
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}

	database, err := proj.OpenDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer proj.Close()

	// Check if this bead is part of a task
	taskID, err := database.GetTaskForBead(beadID)
	if err != nil {
		return fmt.Errorf("failed to look up task for bead: %w", err)
	}

	if taskID != "" {
		// Bead is part of a task - mark it complete in task_beads
		if err := database.CompleteTaskBead(taskID, beadID); err != nil {
			return fmt.Errorf("failed to complete task bead: %w", err)
		}
		fmt.Printf("Marked bead %s as completed in task %s\n", beadID, taskID)

		// Check if all beads in the task are complete and auto-complete the task
		autoCompleted, err := database.CheckAndCompleteTask(taskID, flagPRURL)
		if err != nil {
			return fmt.Errorf("failed to check task completion: %w", err)
		}
		if autoCompleted {
			fmt.Printf("All beads complete - task %s marked as completed", taskID)
			if flagPRURL != "" {
				fmt.Printf(" (PR: %s)", flagPRURL)
			}
			fmt.Println()
		}
	}

	// Also mark the bead as complete in the beads table (backwards compatibility)
	if err := database.CompleteBead(beadID, flagPRURL); err != nil {
		return fmt.Errorf("failed to complete bead: %w", err)
	}

	if taskID == "" {
		// Only print this if not part of a task (already printed above)
		fmt.Printf("Marked bead %s as completed", beadID)
		if flagPRURL != "" {
			fmt.Printf(" (PR: %s)", flagPRURL)
		}
		fmt.Println()
	}

	return nil
}
