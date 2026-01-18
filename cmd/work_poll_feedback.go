package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var workPollFeedbackCmd = &cobra.Command{
	Use:   "poll-feedback [<work-id>]",
	Short: "Manually trigger PR feedback polling",
	Long: `Manually trigger PR feedback polling for a work unit.

Signals the orchestrator to immediately check for PR feedback,
including status checks, workflow runs, comments, and review comments.

If no work ID is provided, detects from current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkPollFeedback,
}

func runWorkPollFeedback(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Find project
	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to find project: %w", err)
	}
	defer proj.Close()

	// Open database
	database, err := proj.OpenDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	// Get work ID
	var workID string
	if len(args) > 0 {
		workID = args[0]
	} else {
		// Try to detect from current directory
		work, err := detectWorkFromPath(proj, database)
		if err != nil {
			return fmt.Errorf("failed to detect work from current directory: %w", err)
		}
		workID = work.ID
	}

	// Get work details
	work, err := database.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work %s: %w", workID, err)
	}

	if work.PRURL == "" {
		return fmt.Errorf("work %s does not have an associated PR URL", workID)
	}

	fmt.Printf("Triggering PR feedback poll for work %s\n", workID)
	fmt.Printf("PR URL: %s\n", work.PRURL)

	// Create a signal file that the orchestrator will watch for
	// This is a simple way to communicate with the orchestrator
	signalPath := filepath.Join(proj.Root, ".co", fmt.Sprintf("poll-feedback-%s-%d", workID, time.Now().UnixNano()))

	// Write the signal file
	if err := os.WriteFile(signalPath, []byte(work.PRURL), 0644); err != nil {
		return fmt.Errorf("failed to create poll signal: %w", err)
	}

	// Clean up the signal file after a short delay
	// The orchestrator should pick it up quickly
	go func() {
		time.Sleep(2 * time.Second)
		_ = os.Remove(signalPath)
	}()

	fmt.Println("✓ Poll request sent to orchestrator")
	fmt.Println("The orchestrator will check for PR feedback and create beads for any actionable items.")

	return nil
}

// detectWorkFromPath tries to detect the work from the current directory
func detectWorkFromPath(proj *project.Project, database *db.DB) (*db.Work, error) {
	ctx := context.Background()

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if we're in a work directory
	if !strings.Contains(cwd, proj.Root) {
		return nil, fmt.Errorf("not in a project directory")
	}

	// Extract work ID from path
	rel, err := filepath.Rel(proj.Root, cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to get relative path: %w", err)
	}

	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) == 0 {
		return nil, fmt.Errorf("could not detect work ID from path")
	}

	// The work ID should be the first directory component (e.g., "w-abc")
	workID := parts[0]
	if !strings.HasPrefix(workID, "w-") {
		return nil, fmt.Errorf("not in a work directory (expected w-* prefix)")
	}

	// Verify work exists
	work, err := database.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("work %s not found: %w", workID, err)
	}

	return work, nil
}