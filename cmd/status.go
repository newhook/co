package cmd

import (
	"fmt"
	"os"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [bead-id]",
	Short: "Show bead tracking status",
	Long: `Show tracking status for beads.

With a bead ID: Show detailed status including zellij session/pane info.
Without ID: Show all beads currently processing with their session/pane.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
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

	// If specific bead requested
	if len(args) > 0 {
		beadID := args[0]
		bead, err := database.GetBead(beadID)
		if err != nil {
			return fmt.Errorf("failed to get bead: %w", err)
		}
		if bead == nil {
			return fmt.Errorf("bead %s not found in tracking database", beadID)
		}

		printBeadDetails(bead)
		return nil
	}

	// Show all processing beads
	beads, err := database.ListBeads(db.StatusProcessing)
	if err != nil {
		return fmt.Errorf("failed to list beads: %w", err)
	}

	if len(beads) == 0 {
		fmt.Println("No beads currently processing")
		return nil
	}

	fmt.Printf("Currently processing %d bead(s):\n\n", len(beads))
	for _, bead := range beads {
		printBeadDetails(bead)
		fmt.Println()
	}

	return nil
}

func printBeadDetails(bead *db.TrackedBead) {
	fmt.Printf("ID:      %s\n", bead.ID)
	fmt.Printf("Title:   %s\n", bead.Title)
	fmt.Printf("Status:  %s\n", bead.Status)

	if bead.ZellijSession != "" {
		fmt.Printf("Session: %s\n", bead.ZellijSession)
	}
	if bead.ZellijPane != "" {
		fmt.Printf("Pane:    %s\n", bead.ZellijPane)
	}
	if bead.WorktreePath != "" {
		fmt.Printf("Worktree: %s\n", bead.WorktreePath)
	}
	if bead.PRURL != "" {
		fmt.Printf("PR:      %s\n", bead.PRURL)
	}
	if bead.ErrorMessage != "" {
		fmt.Printf("Error:   %s\n", bead.ErrorMessage)
	}
	if bead.StartedAt != nil {
		fmt.Printf("Started: %s\n", bead.StartedAt.Format("2006-01-02 15:04:05"))
	}
	if bead.CompletedAt != nil {
		fmt.Printf("Done:    %s\n", bead.CompletedAt.Format("2006-01-02 15:04:05"))
	}
}
