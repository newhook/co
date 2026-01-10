package cmd

import (
	"fmt"
	"os"

	"github.com/newhook/autoclaude/internal/db"
	"github.com/newhook/autoclaude/internal/project"
	"github.com/spf13/cobra"
)

var flagStatusFilter string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tracked beads",
	Long:  `List all beads in the tracking database with optional status filter.`,
	RunE:  runList,
}

func init() {
	listCmd.Flags().StringVarP(&flagStatusFilter, "status", "s", "", "filter by status (pending, processing, completed, failed)")
}

func runList(cmd *cobra.Command, args []string) error {
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

	beads, err := database.ListBeads(flagStatusFilter)
	if err != nil {
		return fmt.Errorf("failed to list beads: %w", err)
	}

	if len(beads) == 0 {
		if flagStatusFilter != "" {
			fmt.Printf("No beads with status '%s'\n", flagStatusFilter)
		} else {
			fmt.Println("No beads tracked")
		}
		return nil
	}

	fmt.Printf("%-12s %-12s %-40s %s\n", "ID", "STATUS", "TITLE", "PR")
	fmt.Printf("%-12s %-12s %-40s %s\n", "---", "------", "-----", "--")

	for _, bead := range beads {
		title := bead.Title
		if len(title) > 38 {
			title = title[:35] + "..."
		}
		prURL := bead.PRURL
		if prURL == "" {
			prURL = "-"
		}
		fmt.Printf("%-12s %-12s %-40s %s\n", bead.ID, bead.Status, title, prURL)
	}

	return nil
}
