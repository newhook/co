package cmd

import (
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
	flagPlanAutoGroup     bool
	flagPlanBudget        int
	flagPlanProject       string
	flagPlanWork          string
	flagPlanForceEstimate bool
)

var planCmd = &cobra.Command{
	Use:   "plan [bead-groups...]",
	Short: "Create tasks from beads",
	Long: `Plan creates tasks from beads for later execution with 'co run'.

Without arguments, creates one task per ready bead.

With --auto-group, uses LLM to estimate complexity and group beads
into tasks using bin-packing. Can be combined with bead arguments to
auto-group specific beads:
  co plan bead-1 bead-2 bead-3 --auto-group  # auto-group these specific beads

With positional arguments (without --auto-group), manually specify task groupings:
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
	planCmd.Flags().BoolVar(&flagPlanForceEstimate, "force-estimate", false, "force re-estimation even if cached (used with --auto-group)")
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx := GetContext()
	proj, err := project.Find(ctx, flagPlanProject)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	fmt.Printf("Using project: %s\n", proj.Config.Project.Name)

	// Determine work context
	workID := flagPlanWork
	if workID == "" {
		// Try to detect work from current directory
		workID, _ = detectWorkFromDirectory(proj)
	}

	// Validate work exists if specified
	var work *db.Work
	if workID != "" {
		work, err = proj.DB.GetWork(GetContext(), workID)
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

	// Manual grouping mode - only if args provided WITHOUT --auto-group
	if len(args) > 0 && !flagPlanAutoGroup {
		return planManualGroups(proj, args, workID, work)
	}

	// Get beads - either all ready beads or the specified ones
	var beadList []beads.Bead
	if len(args) > 0 {
		// Auto-group mode with specific beads
		fmt.Printf("Getting specified beads for auto-grouping...\n")
		// Collect all specified bead IDs (treating commas as separators like in manual mode)
		var requestedIDs []string
		for _, arg := range args {
			ids := strings.Split(arg, ",")
			for _, id := range ids {
				id = strings.TrimSpace(id)
				if id != "" {
					requestedIDs = append(requestedIDs, id)
				}
			}
		}

		// Fetch each bead
		for _, id := range requestedIDs {
			bead, err := beads.GetBeadInDir(id, proj.MainRepoPath())
			if err != nil {
				return fmt.Errorf("failed to get bead %s: %w", id, err)
			}
			beadList = append(beadList, *bead)
		}
		fmt.Printf("Found %d specified bead(s)\n", len(beadList))
	} else {
		// Get all ready beads
		beadList, err = beads.GetReadyBeadsInDir(proj.MainRepoPath())
		if err != nil {
			return fmt.Errorf("failed to get ready beads: %w", err)
		}
	}

	if len(beadList) == 0 {
		fmt.Println("No ready beads to plan")
		return nil
	}

	// Check for beads already in pending tasks
	pendingTasks, err := proj.DB.ListTasks(GetContext(), db.StatusPending)
	if err != nil {
		return fmt.Errorf("failed to check pending tasks: %w", err)
	}

	// Build set of beads that are already in pending tasks
	beadsInPendingTasks := make(map[string]bool)
	for _, task := range pendingTasks {
		beadIDs, err := proj.DB.GetTaskBeads(GetContext(), task.ID)
		if err != nil {
			return fmt.Errorf("failed to get beads for task %s: %w", task.ID, err)
		}
		for _, beadID := range beadIDs {
			beadsInPendingTasks[beadID] = true
		}
	}

	// Filter out beads that are already in pending tasks
	var availableBeads []beads.Bead
	var skippedBeads []string
	for _, bead := range beadList {
		if beadsInPendingTasks[bead.ID] {
			skippedBeads = append(skippedBeads, bead.ID)
		} else {
			availableBeads = append(availableBeads, bead)
		}
	}

	// Report on what we're doing
	if len(skippedBeads) > 0 {
		fmt.Printf("Skipping %d bead(s) already in pending tasks: %s\n",
			len(skippedBeads), strings.Join(skippedBeads, ", "))
		if len(pendingTasks) > 0 {
			fmt.Printf("  Pending tasks: ")
			for i, task := range pendingTasks {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(task.ID)
			}
			fmt.Println()
		}
	}

	if len(availableBeads) == 0 {
		fmt.Println("No beads available to plan (all ready beads are already in pending tasks)")
		fmt.Println("To re-plan these beads, first delete their pending tasks with 'co task delete <task-id>'")
		return nil
	}

	fmt.Printf("Planning %d available bead(s)\n", len(availableBeads))
	beadList = availableBeads

	// Auto-group mode
	if flagPlanAutoGroup {
		return planAutoGroup(proj, beadList, workID, work)
	}

	// Default: single-bead tasks
	return planSingleBead(proj, beadList, workID)
}

// taskGroup represents a user-defined grouping of beads for a single task.
type taskGroup struct {
	index   int          // original order in args
	beadIDs []string     // bead IDs in this group
	beads   []beads.Bead // resolved beads
}

// planManualGroups creates tasks from manual groupings like "bead-1,bead-2 bead-3"
func planManualGroups(proj *project.Project, args []string, workID string, work *db.Work) error {
	mainRepoPath := proj.MainRepoPath()

	// First, check for beads already in pending tasks
	pendingTasks, err := proj.DB.ListTasks(GetContext(), db.StatusPending)
	if err != nil {
		return fmt.Errorf("failed to check pending tasks: %w", err)
	}

	// Build set of beads that are already in pending tasks
	beadsInPendingTasks := make(map[string]bool)
	for _, t := range pendingTasks {
		beadIDs, err := proj.DB.GetTaskBeads(GetContext(), t.ID)
		if err != nil {
			return fmt.Errorf("failed to get beads for task %s: %w", t.ID, err)
		}
		for _, beadID := range beadIDs {
			beadsInPendingTasks[beadID] = true
		}
	}

	// Parse and validate all groups first
	var groups []taskGroup
	var allBeadIDs []string

	for i, arg := range args {
		beadIDs := strings.Split(arg, ",")
		var groupBeadIDs []string
		var groupBeads []beads.Bead
		var conflictingBeads []string

		// Validate and fetch each bead
		for _, id := range beadIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}

			// Check if this bead is already in a pending task
			if beadsInPendingTasks[id] {
				conflictingBeads = append(conflictingBeads, id)
				continue
			}

			bead, err := beads.GetBeadInDir(id, mainRepoPath)
			if err != nil {
				return fmt.Errorf("failed to get bead %s: %w", id, err)
			}
			groupBeadIDs = append(groupBeadIDs, id)
			groupBeads = append(groupBeads, *bead)
			allBeadIDs = append(allBeadIDs, id)
		}

		// Report conflicts for this group
		if len(conflictingBeads) > 0 {
			return fmt.Errorf("cannot plan beads already in pending tasks: %s", strings.Join(conflictingBeads, ", "))
		}

		if len(groupBeads) == 0 {
			continue
		}

		groups = append(groups, taskGroup{
			index:   i,
			beadIDs: groupBeadIDs,
			beads:   groupBeads,
		})
	}

	if len(groups) == 0 {
		fmt.Println("No tasks to create")
		return nil
	}

	// Fetch dependency information for all beads
	var allBeads []beads.Bead
	for _, g := range groups {
		allBeads = append(allBeads, g.beads...)
	}
	beadsWithDeps, err := getBeadsWithDepsForPlan(allBeads, mainRepoPath)
	if err != nil {
		return fmt.Errorf("failed to get bead dependencies: %w", err)
	}

	// Build map of bead ID to group index
	beadToGroup := make(map[string]int)
	for i, g := range groups {
		for _, id := range g.beadIDs {
			beadToGroup[id] = i
		}
	}

	// Build dependency graph between groups
	sortedGroups, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)
	if err != nil {
		return err
	}

	// Create tasks in dependency order
	for _, g := range sortedGroups {
		// Generate hierarchical task ID (work is always required)
		nextNum, err := proj.DB.GetNextTaskNumber(GetContext(), workID)
		if err != nil {
			return fmt.Errorf("failed to get next task number for work %s: %w", workID, err)
		}
		taskID := fmt.Sprintf("%s.%d", workID, nextNum)

		if err := proj.DB.CreateTask(GetContext(), taskID, "implement", g.beadIDs, 0, workID); err != nil {
			return fmt.Errorf("failed to create task %s: %w", taskID, err)
		}
		fmt.Printf("Created implement task %s with %d bead(s): %s\n", taskID, len(g.beadIDs), strings.Join(g.beadIDs, ", "))
	}

	fmt.Printf("\nCreated %d implement task(s). Run 'co run' to execute.\n", len(sortedGroups))
	return nil
}

// sortGroupsByDependencies reorders task groups so dependencies are satisfied.
// Groups are reordered based on bead dependencies between groups.
func sortGroupsByDependencies(groups []taskGroup, beadsWithDeps []beads.BeadWithDeps, beadToGroup map[string]int) ([]taskGroup, error) {
	if len(groups) <= 1 {
		return groups, nil
	}

	// Build bead dependency map
	beadDeps := make(map[string][]string)
	for _, b := range beadsWithDeps {
		for _, dep := range b.Dependencies {
			if dep.DependencyType == "depends_on" {
				beadDeps[b.ID] = append(beadDeps[b.ID], dep.ID)
			}
		}
	}

	// Build group dependency graph
	// Group A depends on Group B if any bead in A depends on a bead in B
	groupDependsOn := make(map[int]map[int]bool)
	for i := range groups {
		groupDependsOn[i] = make(map[int]bool)
	}

	for _, b := range beadsWithDeps {
		beadGroupIdx, ok := beadToGroup[b.ID]
		if !ok {
			continue
		}
		for _, depID := range beadDeps[b.ID] {
			depGroupIdx, ok := beadToGroup[depID]
			if !ok {
				// Dependency is not in any group (external dependency)
				continue
			}
			if depGroupIdx != beadGroupIdx {
				// This group depends on another group
				groupDependsOn[beadGroupIdx][depGroupIdx] = true
			}
		}
	}

	// Topological sort of groups using Kahn's algorithm
	inDegree := make(map[int]int)
	for i := range groups {
		inDegree[i] = len(groupDependsOn[i])
	}

	// Start with groups that have no dependencies
	var queue []int
	for i, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, i)
		}
	}

	var sortedIndices []int
	for len(queue) > 0 {
		// Pop from queue
		idx := queue[0]
		queue = queue[1:]
		sortedIndices = append(sortedIndices, idx)

		// Reduce in-degree of groups that depend on this one
		for otherIdx := range groups {
			if groupDependsOn[otherIdx][idx] {
				inDegree[otherIdx]--
				if inDegree[otherIdx] == 0 {
					queue = append(queue, otherIdx)
				}
			}
		}
	}

	// Check for cycles
	if len(sortedIndices) != len(groups) {
		return nil, fmt.Errorf("dependency cycle detected between task groups")
	}

	// Build sorted groups
	sortedGroups := make([]taskGroup, len(groups))
	for i, idx := range sortedIndices {
		sortedGroups[i] = groups[idx]
	}

	return sortedGroups, nil
}

// planAutoGroup uses LLM to group beads by complexity
func planAutoGroup(proj *project.Project, beadList []beads.Bead, workID string, work *db.Work) error {
	fmt.Println("Auto-grouping beads by complexity...")

	// Get beads with dependencies for planning
	beadsWithDeps, err := getBeadsWithDepsForPlan(beadList, proj.MainRepoPath())
	if err != nil {
		return fmt.Errorf("failed to get bead dependencies: %w", err)
	}

	// Create planner with complexity estimator
	// Use work's worktree path for estimation to avoid creating extra worktrees
	estimationPath := proj.MainRepoPath()
	if work != nil && work.WorktreePath != "" {
		estimationPath = work.WorktreePath
	}
	estimator := task.NewLLMEstimator(proj.DB, estimationPath, proj.Config.Project.Name, workID)

	// Estimate complexity for all beads in batch
	fmt.Println("Estimating complexity for beads...")
	ctx := GetContext()
	if err := estimator.EstimateBatch(ctx, beadList, flagPlanForceEstimate); err != nil {
		return fmt.Errorf("failed to estimate complexity: %w", err)
	}

	planner := task.NewDefaultPlanner(estimator)

	// Plan tasks
	fmt.Printf("Planning tasks with budget %d...\n", flagPlanBudget)
	tasks, err := planner.Plan(ctx, beadsWithDeps, flagPlanBudget)
	if err != nil {
		return fmt.Errorf("failed to plan tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks to create")
		return nil
	}

	// Update task IDs to use hierarchical format (work is always required)
	for i := range tasks {
		// Get next task number for this work
		nextNum, err := proj.DB.GetNextTaskNumber(GetContext(), workID)
		if err != nil {
			return fmt.Errorf("failed to get next task number for work %s: %w", workID, err)
		}
		// Update task ID to hierarchical format (w-abc.1, w-abc.2, etc.)
		tasks[i].ID = fmt.Sprintf("%s.%d", workID, nextNum)
	}

	// Create tasks in proj.DB
	for _, t := range tasks {
		if err := proj.DB.CreateTask(GetContext(), t.ID, "implement", t.BeadIDs, t.Complexity, workID); err != nil {
			return fmt.Errorf("failed to create task %s: %w", t.ID, err)
		}
		fmt.Printf("Created implement task %s (complexity: %d) with %d bead(s): %s\n",
			t.ID, t.Complexity, len(t.BeadIDs), strings.Join(t.BeadIDs, ", "))
	}

	fmt.Printf("\nCreated %d implement task(s). Run 'co run' to execute.\n", len(tasks))
	return nil
}

// planSingleBead creates one task per bead in dependency order
func planSingleBead(proj *project.Project, beadList []beads.Bead, workID string) error {
	fmt.Printf("Creating %d single-bead task(s)...\n", len(beadList))

	// Fetch dependency information for all beads
	beadsWithDeps, err := getBeadsWithDepsForPlan(beadList, proj.MainRepoPath())
	if err != nil {
		return fmt.Errorf("failed to get bead dependencies: %w", err)
	}

	// Build dependency graph and sort beads
	graph := task.BuildDependencyGraph(beadsWithDeps)
	sortedBeads, err := task.TopologicalSort(graph, beadsWithDeps)
	if err != nil {
		return fmt.Errorf("failed to sort beads by dependencies: %w", err)
	}

	// Create tasks in dependency order
	for _, bead := range sortedBeads {
		// Generate hierarchical task ID (work is always required)
		nextNum, err := proj.DB.GetNextTaskNumber(GetContext(), workID)
		if err != nil {
			return fmt.Errorf("failed to get next task number for work %s: %w", workID, err)
		}
		taskID := fmt.Sprintf("%s.%d", workID, nextNum)

		if err := proj.DB.CreateTask(GetContext(), taskID, "implement", []string{bead.ID}, 0, workID); err != nil {
			return fmt.Errorf("failed to create task %s: %w", taskID, err)
		}
		fmt.Printf("Created implement task %s: %s\n", taskID, bead.Title)
	}

	fmt.Printf("\nCreated %d implement task(s). Run 'co run' to execute.\n", len(sortedBeads))
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
func detectWorkFromDirectory(proj *project.Project) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Check if we're in a work subdirectory (format: /project/w-xxx/tree)
	rel, err := filepath.Rel(proj.Root, cwd)
	if err != nil {
		return "", nil
	}

	// Check if we're in a work directory (w-xxx or w-xxx/tree/...)
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) >= 1 && strings.HasPrefix(parts[0], "w-") {
		return parts[0], nil
	}

	return "", nil
}
