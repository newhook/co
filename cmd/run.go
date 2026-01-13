package cmd

import (
	"fmt"
	"strings"

	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	flagLimit     int
	flagDryRun    bool
	flagProject   string
	flagWork      string
	flagAutoClose bool
)

var runCmd = &cobra.Command{
	Use:   "run [work-id]",
	Short: "Execute pending tasks for a work unit",
	Long: `Run ensures the work orchestrator is running for a work unit.

The orchestrator polls for ready tasks and executes them in dependency order.
Tasks run sequentially within the work's worktree until all are complete.

Without arguments:
- If in a work directory or --work specified: runs that work's orchestrator

With an ID:
- If ID is a work ID (e.g., w-xxx): runs that work's orchestrator

The orchestrator handles:
- Polling for ready tasks (pending with all dependencies satisfied)
- Executing each task inline in sequence
- Error handling and status updates`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTasks,
}

func init() {
	runCmd.Flags().IntVarP(&flagLimit, "limit", "n", 0, "maximum number of tasks to process (0 = unlimited)")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "show plan without executing")
	runCmd.Flags().StringVar(&flagProject, "project", "", "project directory (default: auto-detect from cwd)")
	runCmd.Flags().StringVar(&flagWork, "work", "", "work ID to run (default: auto-detect from cwd)")
	runCmd.Flags().BoolVar(&flagAutoClose, "auto-close", false, "automatically close tabs after task completion")
}

func runTasks(cmd *cobra.Command, args []string) error {
	var argID string
	if len(args) > 0 {
		argID = args[0]
	}
	ctx := GetContext()

	proj, err := project.Find(ctx, flagProject)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	fmt.Printf("Using project: %s\n", proj.Config.Project.Name)

	// Determine work context (required)
	// Priority: explicit arg > --work flag > directory context
	var workID string

	if argID != "" {
		// Check if it looks like a work ID
		if strings.HasPrefix(argID, "work-") || strings.HasPrefix(argID, "w-") {
			workID = argID
		} else {
			return fmt.Errorf("invalid ID format: %s (expected w-xxx or work-N)", argID)
		}
	} else if flagWork != "" {
		workID = flagWork
	} else {
		// Try to detect work from current directory
		workID, _ = detectWorkFromDirectory(proj)
		if workID == "" {
			return fmt.Errorf("no work context found. Use --work flag or run from a work directory")
		}
	}

	// Get work details to verify it exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	fmt.Printf("\n=== Running work %s ===\n", work.ID)
	fmt.Printf("Branch: %s\n", work.BranchName)
	fmt.Printf("Worktree: %s\n", work.WorktreePath)

	// Check if worktree exists
	if work.WorktreePath == "" {
		return fmt.Errorf("work %s has no worktree path configured", work.ID)
	}

	if !worktree.ExistsPath(work.WorktreePath) {
		return fmt.Errorf("work %s worktree does not exist at %s", work.ID, work.WorktreePath)
	}

	// Ensure orchestrator is running
	spawned, err := claude.EnsureWorkOrchestrator(ctx, workID, proj.Config.Project.Name, work.WorktreePath)
	if err != nil {
		return fmt.Errorf("failed to ensure orchestrator: %w", err)
	}

	if spawned {
		fmt.Println("\nOrchestrator spawned in zellij tab.")
	} else {
		fmt.Println("\nOrchestrator is already running.")
	}

	fmt.Println("Switch to the zellij session to monitor progress.")
	return nil
}
