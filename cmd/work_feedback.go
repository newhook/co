package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/feedback"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var workFeedbackCmd = &cobra.Command{
	Use:   "feedback [<work-id>]",
	Short: "Process PR feedback and create beads",
	Long: `Process PR feedback for a work unit and create beads from actionable items.

Fetches PR status checks, workflow runs, comments, and review comments,
then creates beads for failures and requested changes.

If no work ID is provided, detects from current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkFeedback,
}

var (
	feedbackDryRun      bool
	feedbackMinPriority int
)

func init() {
	workFeedbackCmd.Flags().BoolVar(&feedbackDryRun, "dry-run", false, "Show what beads would be created without creating them")
	workFeedbackCmd.Flags().IntVar(&feedbackMinPriority, "min-priority", 2, "Minimum priority for created beads (0-4, 0=critical)")
}

// ProcessPRFeedbackQuiet processes PR feedback without outputting to stdout.
// Delegates to internal/feedback.ProcessPRFeedbackQuiet.
func ProcessPRFeedbackQuiet(ctx context.Context, proj *project.Project, database *db.DB, workID string, minPriority int) (int, error) {
	return feedback.ProcessPRFeedbackQuiet(ctx, proj, database, workID, minPriority)
}

// ProcessPRFeedback processes PR feedback for a work and creates beads.
// Delegates to internal/feedback.ProcessPRFeedback.
func ProcessPRFeedback(ctx context.Context, proj *project.Project, database *db.DB, workID string, minPriority int) (int, error) {
	return feedback.ProcessPRFeedback(ctx, proj, database, workID, minPriority)
}

func runWorkFeedback(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	// Find project
	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to find project: %w", err)
	}
	defer proj.Close()

	// Get work ID
	var workID string
	if len(args) > 0 {
		workID = args[0]
	} else {
		// Try to detect from current directory
		work, err := detectWork(ctx, proj, proj.DB)
		if err != nil {
			return fmt.Errorf("failed to detect work from current directory: %w", err)
		}
		workID = work.ID
	}

	// Skip dry-run as it's not needed for internal calls
	if feedbackDryRun {
		fmt.Println("[DRY RUN MODE - Not creating beads]")
	}

	// Call the internal function
	_, err = ProcessPRFeedback(ctx, proj, proj.DB, workID, feedbackMinPriority)
	return err
}

// detectWork tries to detect the work from the current directory
func detectWork(ctx context.Context, proj *project.Project, database *db.DB) (*db.Work, error) {
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
