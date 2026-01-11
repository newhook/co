package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/task"
	"github.com/spf13/cobra"
)

var (
	flagPlanAutoGroup bool
	flagPlanBudget    int
	flagPlanProject   string
	flagPlanWork      string
)

var planCmd = &cobra.Command{
	Use:   "plan [bead-groups...]",
	Short: "Create tasks from beads",
	Long: `Plan creates tasks from beads for later execution with 'co run'.

Without arguments, creates one task per ready bead.

With --auto-group, uses LLM to estimate complexity and group beads
into tasks using bin-packing.

With positional arguments, manually specify task groupings:
  co plan bead-1,bead-2 bead-3    # task1=[bead-1,bead-2], task2=[bead-3]
  co plan bead-1,bead-2,bead-3    # all 3 beads in one task

Task dependencies are derived from bead dependencies at runtime.`,
	RunE: runPlan,
}

func init() {
	planCmd.Flags().BoolVar(&flagPlanAutoGroup, "auto-group", false, "automatically group beads by complexity using LLM estimation")
	planCmd.Flags().IntVar(&flagPlanBudget, "budget", 70, "complexity budget per task (1-100, used with --auto-group)")
	planCmd.Flags().StringVar(&flagPlanProject, "project", "", "project directory (default: auto-detect from cwd)")
	planCmd.Flags().StringVar(&flagPlanWork, "work", "", "work ID to plan tasks for (default: auto-detect from cwd)")
}

func runPlan(cmd *cobra.Command, args []string) error {
	proj, err := project.Find(flagPlanProject)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}

	fmt.Printf("Using project: %s\n", proj.Config.Project.Name)

	database, err := proj.OpenDB()
	if err != nil {
		return fmt.Errorf("failed to open tracking database: %w", err)
	}
	defer proj.Close()

	// Determine work context
	workID := flagPlanWork
	if workID == "" {
		// Try to detect work from current directory
		workID, _ = detectWorkFromDirectory(database, proj)
	}

	// Validate work exists if specified
	if workID != "" {
		work, err := database.GetWork(workID)
		if err != nil {
			return fmt.Errorf("failed to get work %s: %w", workID, err)
		}
		if work == nil {
			return fmt.Errorf("work %s not found", workID)
		}
		fmt.Printf("Planning tasks for work: %s\n", workID)
	} else {
		return fmt.Errorf("no work context specified. Use --work flag or run from a work directory")
	}

	// Check for existing pending tasks
	pendingTasks, err := database.ListTasks(db.StatusPending)
	if err != nil {
		return fmt.Errorf("failed to check pending tasks: %w", err)
	}
	if len(pendingTasks) > 0 {
		return fmt.Errorf("there are %d pending task(s) - run them first with 'co run' or clear them", len(pendingTasks))
	}

	// Manual grouping mode
	if len(args) > 0 {
		return planManualGroups(proj, database, args, workID)
	}

	// Get all ready beads
	beadList, err := beads.GetReadyBeadsInDir(proj.MainRepoPath())
	if err != nil {
		return fmt.Errorf("failed to get ready beads: %w", err)
	}

	if len(beadList) == 0 {
		fmt.Println("No ready beads to plan")
		return nil
	}

	// Auto-group mode
	if flagPlanAutoGroup {
		return planAutoGroup(proj, database, beadList, workID)
	}

	// Default: single-bead tasks
	return planSingleBead(proj, database, beadList, workID)
}

// planManualGroups creates tasks from manual groupings like "bead-1,bead-2 bead-3"
func planManualGroups(proj *project.Project, database *db.DB, args []string, workID string) error {
	var tasks []task.Task
	mainRepoPath := proj.MainRepoPath()

	for i, arg := range args {
		beadIDs := strings.Split(arg, ",")
		var taskBeads []beads.Bead

		// Validate and fetch each bead
		for _, id := range beadIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			bead, err := beads.GetBeadInDir(id, mainRepoPath)
			if err != nil {
				return fmt.Errorf("failed to get bead %s: %w", id, err)
			}
			taskBeads = append(taskBeads, *bead)
		}

		if len(taskBeads) == 0 {
			continue
		}

		// Generate task ID
		var taskID string
		if len(taskBeads) == 1 {
			taskID = taskBeads[0].ID
		} else {
			taskID = fmt.Sprintf("task-%d", i+1)
		}

		// Collect bead IDs
		var ids []string
		for _, b := range taskBeads {
			ids = append(ids, b.ID)
		}

		tasks = append(tasks, task.Task{
			ID:      taskID,
			BeadIDs: ids,
			Beads:   taskBeads,
			Status:  task.StatusPending,
		})
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks to create")
		return nil
	}

	// Create tasks in database
	for _, t := range tasks {
		if err := database.CreateTask(t.ID, "implement", t.BeadIDs, t.Complexity, workID); err != nil {
			return fmt.Errorf("failed to create task %s: %w", t.ID, err)
		}
		fmt.Printf("Created task %s with %d bead(s): %s\n", t.ID, len(t.BeadIDs), strings.Join(t.BeadIDs, ", "))
	}

	fmt.Printf("\nCreated %d task(s). Run 'co run' to execute.\n", len(tasks))
	return nil
}

// planAutoGroup uses LLM to group beads by complexity
func planAutoGroup(proj *project.Project, database *db.DB, beadList []beads.Bead, workID string) error {
	fmt.Println("Auto-grouping beads by complexity...")

	// Get beads with dependencies for planning
	beadsWithDeps, err := getBeadsWithDepsForPlan(beadList, proj.MainRepoPath())
	if err != nil {
		return fmt.Errorf("failed to get bead dependencies: %w", err)
	}

	// Create planner with complexity estimator
	estimator := task.NewLLMEstimator(database, proj.MainRepoPath(), proj.Config.Project.Name)

	// Estimate complexity for all beads in batch
	fmt.Println("Estimating complexity for beads...")
	ctx := context.Background()
	if err := estimator.EstimateBatch(ctx, beadList); err != nil {
		return fmt.Errorf("failed to estimate complexity: %w", err)
	}

	planner := task.NewDefaultPlanner(estimator)

	// Plan tasks
	fmt.Printf("Planning tasks with budget %d...\n", flagPlanBudget)
	tasks, err := planner.Plan(beadsWithDeps, flagPlanBudget)
	if err != nil {
		return fmt.Errorf("failed to plan tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks to create")
		return nil
	}

	// Create tasks in database
	for _, t := range tasks {
		if err := database.CreateTask(t.ID, "implement", t.BeadIDs, t.Complexity, workID); err != nil {
			return fmt.Errorf("failed to create task %s: %w", t.ID, err)
		}
		fmt.Printf("Created task %s (complexity: %d) with %d bead(s): %s\n",
			t.ID, t.Complexity, len(t.BeadIDs), strings.Join(t.BeadIDs, ", "))
	}

	fmt.Printf("\nCreated %d task(s). Run 'co run' to execute.\n", len(tasks))
	return nil
}

// planSingleBead creates one task per bead
func planSingleBead(_ *project.Project, database *db.DB, beadList []beads.Bead, workID string) error {
	fmt.Printf("Creating %d single-bead task(s)...\n", len(beadList))

	for _, bead := range beadList {
		if err := database.CreateTask(bead.ID, "implement", []string{bead.ID}, 0, workID); err != nil {
			return fmt.Errorf("failed to create task %s: %w", bead.ID, err)
		}
		fmt.Printf("Created task %s: %s\n", bead.ID, bead.Title)
	}

	fmt.Printf("\nCreated %d task(s). Run 'co run' to execute.\n", len(beadList))
	return nil
}

// getBeadsWithDepsForPlan retrieves full dependency information for beads.
func getBeadsWithDepsForPlan(beadList []beads.Bead, dir string) ([]beads.BeadWithDeps, error) {
	var result []beads.BeadWithDeps
	for _, b := range beadList {
		bwd, err := beads.GetBeadWithDepsInDir(b.ID, dir)
		if err != nil {
			return nil, fmt.Errorf("failed to get deps for %s: %w", b.ID, err)
		}
		result = append(result, *bwd)
	}
	return result, nil
}

// detectWorkFromDirectory attempts to detect work ID from the current directory.
// Returns the work ID if found, or empty string if not in a work directory.
func detectWorkFromDirectory(database *db.DB, proj *project.Project) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Check if we're in a work subdirectory (format: /project/work-id/tree)
	rel, err := filepath.Rel(proj.Root, cwd)
	if err != nil {
		return "", nil
	}

	// Check if path starts with "work-" and contains "tree"
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) >= 1 && strings.HasPrefix(parts[0], "work-") {
		workID := parts[0]
		// Verify work exists in database
		work, err := database.GetWork(workID)
		if err != nil {
			return "", err
		}
		if work != nil {
			return workID, nil
		}
	}

	// Try to match by worktree path pattern
	pattern := fmt.Sprintf("%%%s%%", cwd)
	work, err := database.GetWorkByDirectory(pattern)
	if err != nil {
		return "", err
	}
	if work != nil {
		return work.ID, nil
	}

	return "", nil
}

