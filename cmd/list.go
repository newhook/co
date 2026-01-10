package cmd

import (
	"fmt"
	"os"

	"github.com/newhook/co/internal/project"
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
