package cmd

import (
	"fmt"
	"os"

	"github.com/newhook/autoclaude/internal/db"
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

	// Try to find project context for database
	var database *db.DB
	var closeFunc func() error

	cwd, _ := os.Getwd()
	if proj, err := project.Find(cwd); err == nil {
		database, err = proj.OpenDB()
		if err != nil {
			return fmt.Errorf("failed to open project database: %w", err)
		}
		closeFunc = proj.Close
	} else {
		var err error
		database, err = db.Open()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		closeFunc = database.Close
	}
	defer closeFunc()

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
