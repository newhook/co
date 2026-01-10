package cmd

import (
	"fmt"
	"os"

	"github.com/newhook/autoclaude/internal/project"
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

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	proj, err := project.Find(cwd)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}

	database, err := proj.OpenDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer proj.Close()

	if err := database.CompleteBead(beadID, flagPRURL); err != nil {
		return fmt.Errorf("failed to complete bead: %w", err)
	}

	fmt.Printf("Marked bead %s as completed", beadID)
	if flagPRURL != "" {
		fmt.Printf(" (PR: %s)", flagPRURL)
	}
	fmt.Println()

	return nil
}
