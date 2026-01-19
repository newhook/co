package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/task"
	"github.com/newhook/co/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	flagLimit         int
	flagDryRun        bool
	flagProject       string
	flagWork          string
	flagAutoClose     bool
	flagRunPlan       bool
	flagRunAuto       bool
	flagForceEstimate bool
)

var runCmd = &cobra.Command{
	Use:   "run [work-id]",
	Short: "Execute pending tasks for a work unit",
	Long: `Run creates tasks from work beads and executes them.

Before spawning the orchestrator, any unassigned beads in work_beads
are automatically converted to tasks based on their grouping.

Flags:
  --plan     Use LLM complexity estimation to auto-group beads into tasks
  --auto     Run full automated workflow (implement, review/fix loop, PR)

Without arguments:
- If in a work directory or --work specified: runs that work

With an ID:
- If ID is a work ID (e.g., w-xxx): runs that work`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTasks,
}

func init() {
	runCmd.Flags().IntVarP(&flagLimit, "limit", "n", 0, "maximum number of tasks to process (0 = unlimited)")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "show plan without executing")
	runCmd.Flags().StringVar(&flagProject, "project", "", "project directory (default: auto-detect from cwd)")
	runCmd.Flags().StringVar(&flagWork, "work", "", "work ID to run (default: auto-detect from cwd)")
	runCmd.Flags().BoolVar(&flagAutoClose, "auto-close", false, "automatically close tabs after task completion")
	runCmd.Flags().BoolVar(&flagRunPlan, "plan", false, "use LLM complexity estimation to auto-group beads")
	runCmd.Flags().BoolVar(&flagRunAuto, "auto", false, "run full automated workflow (implement, review/fix, PR)")
	runCmd.Flags().BoolVar(&flagForceEstimate, "force-estimate", false, "force re-estimation of complexity (with --plan)")
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
		if strings.HasPrefix(argID, "w-") {
			workID = argID
		} else {
			return fmt.Errorf("invalid ID format: %s (expected w-xxx)", argID)
		}
	} else if flagWork != "" {
		workID = flagWork
	} else {
		// Try to detect work from current directory
		workID, err = detectWorkFromDirectory(proj)
		if err != nil {
			return fmt.Errorf("failed to detect work directory: %w", err)
		}
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

	// Validate that work has a root issue
	if work.RootIssueID == "" {
		return fmt.Errorf("work %s has no root issue associated. Create work with a bead ID using 'co work create <bead-id>'", work.ID)
	}

	// Check if worktree exists
	if work.WorktreePath == "" {
		return fmt.Errorf("work %s has no worktree path configured", work.ID)
	}

	if !worktree.ExistsPath(work.WorktreePath) {
		return fmt.Errorf("work %s worktree does not exist at %s", work.ID, work.WorktreePath)
	}

	mainRepoPath := proj.MainRepoPath()

	// If --auto, create estimate task and run full automated workflow
	if flagRunAuto {
		// Create estimate task from unassigned work beads (post-estimation will create implement tasks)
		err := createEstimateTaskFromWorkBeads(ctx, proj, workID, mainRepoPath, os.Stdout)
		if err != nil {
			return fmt.Errorf("failed to create estimate task: %w", err)
		}
		fmt.Println("\nRunning automated workflow...")
		return runFullAutomatedWorkflow(proj, workID, work.WorktreePath, os.Stdout)
	}

	// Create tasks from unassigned work beads (non-auto mode)
	tasksCreated, err := createTasksFromWorkBeads(ctx, proj, workID, mainRepoPath, flagRunPlan, os.Stdout)
	if err != nil {
		return fmt.Errorf("failed to create tasks: %w", err)
	}
	if tasksCreated > 0 {
		fmt.Printf("\nCreated %d task(s) from work beads.\n", tasksCreated)
	}

	// Ensure orchestrator is running
	spawned, err := claude.EnsureWorkOrchestrator(ctx, workID, proj.Config.Project.Name, work.WorktreePath, work.Name, os.Stdout)
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

// createEstimateTaskFromWorkBeads creates an estimate task from unassigned work beads.
// This is used in --auto mode where the full automated workflow includes estimation.
// After the estimate task completes, handlePostEstimation creates implement tasks.
// Progress messages are written to w. Pass io.Discard to suppress output.
func createEstimateTaskFromWorkBeads(ctx context.Context, proj *project.Project, workID, _ string, w io.Writer) error {
	// Get unassigned beads
	unassigned, err := proj.DB.GetUnassignedWorkBeads(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get unassigned beads: %w", err)
	}

	if len(unassigned) == 0 {
		return fmt.Errorf("no unassigned beads found for work %s", workID)
	}

	fmt.Fprintf(w, "\nFound %d unassigned bead(s)\n", len(unassigned))

	// Collect bead IDs
	var beadIDs []string
	for _, wb := range unassigned {
		beadIDs = append(beadIDs, wb.BeadID)
	}

	// Get next task number
	taskNum, err := proj.DB.GetNextTaskNumber(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get next task number: %w", err)
	}

	// Create the estimate task
	taskID := fmt.Sprintf("%s.%d", workID, taskNum)
	if err := proj.DB.CreateTask(ctx, taskID, "estimate", beadIDs, 0, workID); err != nil {
		return fmt.Errorf("failed to create estimate task: %w", err)
	}

	fmt.Fprintf(w, "  Created estimate task %s with %d bead(s)\n", taskID, len(beadIDs))
	fmt.Fprintln(w, "  Implement tasks will be created after estimation completes.")

	return nil
}

// createTasksFromWorkBeads creates tasks from unassigned beads in work_beads.
// If usePlan is true, uses LLM complexity estimation to group beads.
// Returns the number of tasks created.
// Progress messages are written to w. Pass io.Discard to suppress output.
func createTasksFromWorkBeads(ctx context.Context, proj *project.Project, workID, mainRepoPath string, usePlan bool, w io.Writer) (int, error) {
	// Get unassigned beads
	unassigned, err := proj.DB.GetUnassignedWorkBeads(ctx, workID)
	if err != nil {
		return 0, fmt.Errorf("failed to get unassigned beads: %w", err)
	}

	if len(unassigned) == 0 {
		return 0, nil
	}

	fmt.Fprintf(w, "\nFound %d unassigned bead(s)\n", len(unassigned))

	// Collect bead IDs from unassigned work_beads
	beadIDs := make([]string, len(unassigned))
	for i, wb := range unassigned {
		beadIDs[i] = wb.BeadID
	}

	// Get all issues with dependencies in one call
	issuesResult, err := proj.Beads.GetBeadsWithDeps(ctx, beadIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to get bead details: %w", err)
	}

	// Verify all beads were found
	for _, beadID := range beadIDs {
		if _, found := issuesResult.Beads[beadID]; !found {
			return 0, fmt.Errorf("bead %s not found", beadID)
		}
	}

	// Group beads into tasks
	var taskGroups [][]string // Each inner slice is a group of bead IDs for one task

	if usePlan {
		// Use LLM complexity estimation to group beads
		fmt.Fprintln(w, "Using LLM complexity estimation to group beads...")
		taskGroups, err = planBeadsWithComplexity(proj, issuesResult, mainRepoPath, workID, flagForceEstimate)
		if err != nil {
			return 0, fmt.Errorf("failed to plan beads: %w", err)
		}
	} else {
		// Use existing group assignments from work_beads
		taskGroups = groupBeadsByWorkBeadGroup(unassigned)
	}

	// Create tasks from groups
	tasksCreated := 0
	for _, beadIDs := range taskGroups {
		if len(beadIDs) == 0 {
			continue
		}

		// Get next task number
		taskNum, err := proj.DB.GetNextTaskNumber(ctx, workID)
		if err != nil {
			return tasksCreated, fmt.Errorf("failed to get next task number: %w", err)
		}

		taskID := fmt.Sprintf("%s.%d", workID, taskNum)
		if err := proj.DB.CreateTask(ctx, taskID, "implement", beadIDs, 0, workID); err != nil {
			return tasksCreated, fmt.Errorf("failed to create task: %w", err)
		}

		fmt.Fprintf(w, "  Created task %s with %d bead(s)\n", taskID, len(beadIDs))
		tasksCreated++
	}

	return tasksCreated, nil
}

// groupBeadsByWorkBeadGroup returns each bead as its own task group.
// Grouping support has been removed - use --plan to let the LLM group beads
// intelligently based on complexity.
func groupBeadsByWorkBeadGroup(workBeads []*db.WorkBead) [][]string {
	var result [][]string
	for _, wb := range workBeads {
		result = append(result, []string{wb.BeadID})
	}
	return result
}

// planBeadsWithComplexity uses LLM complexity estimation to group beads.
// If forceEstimate is true, re-estimates complexity even if cached values exist.
func planBeadsWithComplexity(proj *project.Project, issuesResult *beads.BeadsWithDepsResult, mainRepoPath, workID string, forceEstimate bool) ([][]string, error) {
	ctx := GetContext()

	// Use the task planner with complexity estimation
	estimator := task.NewLLMEstimator(proj.DB, mainRepoPath, proj.Config.Project.Name, workID)
	planner := task.NewDefaultPlanner(estimator)

	// Convert map to slice of beads
	beadList := make([]beads.Bead, 0, len(issuesResult.Beads))
	for _, b := range issuesResult.Beads {
		beadList = append(beadList, b)
	}

	// Estimate complexity for each bead
	result, err := estimator.EstimateBatch(ctx, beadList, forceEstimate)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate complexity: %w", err)
	}

	// If a task was spawned for estimation, we need to wait for it
	if result.TaskSpawned {
		return nil, fmt.Errorf("estimation task %s spawned - re-run after it completes", result.TaskID)
	}

	// Plan tasks using the bin-packing algorithm with default budget of 70
	planned, err := planner.Plan(ctx, beadList, issuesResult.Dependencies, 70)
	if err != nil {
		return nil, fmt.Errorf("failed to plan tasks: %w", err)
	}

	// Convert planned tasks to bead ID groups
	var groups [][]string
	for _, p := range planned {
		groups = append(groups, p.BeadIDs)
	}

	return groups, nil
}

// runFullAutomatedWorkflow runs the complete automated workflow:
// 1. Execute all implement tasks
// 2. Run review-fix loop until clean
// 3. Create PR
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func runFullAutomatedWorkflow(proj *project.Project, workID, worktreePath string, w io.Writer) error {
	ctx := GetContext()

	// Get work details for friendly name
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Spawn the orchestrator which will handle the tasks
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, worktreePath, work.Name, w); err != nil {
		return fmt.Errorf("failed to spawn orchestrator: %w", err)
	}

	fmt.Fprintln(w, "Automated workflow started in zellij tab.")
	fmt.Fprintln(w, "The orchestrator will:")
	fmt.Fprintln(w, "  1. Execute all implement tasks")
	fmt.Fprintln(w, "  2. Run review-fix loop until clean")
	fmt.Fprintln(w, "  3. Create a pull request")

	return nil
}

// RunWorkResult contains the result of running work.
type RunWorkResult struct {
	WorkID           string
	TasksCreated     int
	OrchestratorSpawned bool
}

// RunWork creates tasks from unassigned beads and ensures an orchestrator is running.
// This is the core logic used by both the CLI `co run` command and the TUI.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func RunWork(ctx context.Context, proj *project.Project, workID string, usePlan bool, w io.Writer) (*RunWorkResult, error) {
	// Get work details to verify it exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	// Check if worktree exists
	if work.WorktreePath == "" {
		return nil, fmt.Errorf("work %s has no worktree path configured", work.ID)
	}

	if !worktree.ExistsPath(work.WorktreePath) {
		return nil, fmt.Errorf("work %s worktree does not exist at %s", work.ID, work.WorktreePath)
	}

	mainRepoPath := proj.MainRepoPath()

	// Create tasks from unassigned work beads
	tasksCreated, err := createTasksFromWorkBeads(ctx, proj, workID, mainRepoPath, usePlan, w)
	if err != nil {
		return nil, fmt.Errorf("failed to create tasks: %w", err)
	}

	// Ensure orchestrator is running
	spawned, err := claude.EnsureWorkOrchestrator(ctx, workID, proj.Config.Project.Name, work.WorktreePath, work.Name, w)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure orchestrator: %w", err)
	}

	return &RunWorkResult{
		WorkID:           workID,
		TasksCreated:     tasksCreated,
		OrchestratorSpawned: spawned,
	}, nil
}

// PlanWorkTasksResult contains the result of planning work tasks.
type PlanWorkTasksResult struct {
	TasksCreated int
}

// PlanWorkTasks creates tasks from unassigned beads in a work unit without spawning an orchestrator.
// If autoGroup is true, uses LLM complexity estimation to group beads into tasks.
// Otherwise, uses existing group assignments from work_beads (one task per bead or group).
// Progress messages are written to w. Pass io.Discard to suppress output.
func PlanWorkTasks(ctx context.Context, proj *project.Project, workID string, autoGroup bool, w io.Writer) (*PlanWorkTasksResult, error) {
	// Get work details to verify it exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	mainRepoPath := proj.MainRepoPath()

	// Create tasks from unassigned work beads
	tasksCreated, err := createTasksFromWorkBeads(ctx, proj, workID, mainRepoPath, autoGroup, w)
	if err != nil {
		return nil, fmt.Errorf("failed to create tasks: %w", err)
	}

	return &PlanWorkTasksResult{
		TasksCreated: tasksCreated,
	}, nil
}
