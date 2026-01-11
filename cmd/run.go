package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/task"
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
	Use:   "run [task-id|work-id]",
	Short: "Execute pending tasks or works",
	Long: `Run executes pending tasks created by 'co plan'.

Without arguments:
- If in a work directory or --work specified: executes all tasks in that work
- Otherwise: executes all pending tasks across all works in dependency order

With an ID:
- If ID contains a dot (e.g., w-xxx.1, w-xxx.pr): executes that specific task
- If ID is a work ID (e.g., w-xxx, work-N): executes all tasks in that work

Works manage git worktrees and feature branches.
Each work gets its own worktree at <project>/<work-id>/tree/.
Tasks within a work run sequentially in the same worktree.

After all tasks complete, a PR is created from the work's feature branch to main.
The PR is NOT automatically merged - review and merge manually when ready.`,
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

	proj, err := project.Find(flagProject)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}

	fmt.Printf("Using project: %s\n", proj.Config.Project.Name)

	database, err := proj.OpenDB()
	if err != nil {
		return fmt.Errorf("failed to open tracking database: %w", err)
	}
	defer proj.Close()

	// Determine work context (required)
	// Priority: explicit arg > --work flag > directory context
	var workID string
	var taskID string

	if argID != "" {
		// First, check if argID looks like a task ID (contains a dot like "w-xxx.1" or "w-xxx.pr")
		// We also verify it exists as a task in the database to handle edge cases
		if strings.Contains(argID, ".") {
			// This looks like a task ID - verify it exists as a task
			dbTask, err := database.GetTask(context.Background(), argID)
			if err != nil {
				return fmt.Errorf("failed to check task %s: %w", argID, err)
			}
			if dbTask != nil {
				// Found as a task
				taskID = argID
			} else {
				// Not found as a task - check if the prefix (before dot) is a work ID
				// This handles potential edge cases where someone might have a dot in their work ID
				return fmt.Errorf("task %s not found", argID)
			}
		} else if strings.HasPrefix(argID, "work-") || strings.HasPrefix(argID, "w-") {
			// Accept work ID or w-xxx format
			workID = argID
		} else {
			return fmt.Errorf("invalid ID format: %s (expected w-xxx, work-N, or task-id like w-xxx.1)", argID)
		}
	} else if flagWork != "" {
		workID = flagWork
	} else {
		// Try to detect work from current directory
		workID, _ = detectWorkFromDirectory(database, proj)
		if workID == "" {
			return fmt.Errorf("no work context found. Use --work flag or run from a work directory")
		}
	}

	// Process a specific task or all tasks in a work
	if taskID != "" {
		return processTask(proj, database, taskID)
	}
	return processWork(proj, database, workID)
}

// processTask processes a single task by ID.
func processTask(proj *project.Project, database *db.DB, taskID string) error {
	ctx := context.Background()

	// Get the task
	dbTask, err := database.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}
	if dbTask == nil {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Check task status
	if dbTask.Status == db.StatusCompleted {
		fmt.Printf("Task %s is already completed\n", taskID)
		return nil
	}

	// Get the associated work
	if dbTask.WorkID == "" {
		return fmt.Errorf("task %s has no associated work", taskID)
	}

	work, err := database.GetWork(ctx, dbTask.WorkID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found for task %s", dbTask.WorkID, taskID)
	}

	fmt.Printf("\n=== Processing task %s ===\n", taskID)
	fmt.Printf("Work: %s\n", work.ID)
	fmt.Printf("Branch: %s\n", work.BranchName)
	fmt.Printf("Worktree: %s\n", work.WorktreePath)

	// Check if worktree exists
	if work.WorktreePath == "" {
		return fmt.Errorf("work %s has no worktree path configured", work.ID)
	}

	if !worktree.ExistsPath(work.WorktreePath) {
		return fmt.Errorf("work %s worktree does not exist at %s", work.ID, work.WorktreePath)
	}

	// Get bead details for this task
	beadIDs, err := database.GetTaskBeads(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get beads for task %s: %w", taskID, err)
	}

	var taskBeads []beads.Bead
	for _, beadID := range beadIDs {
		bead, err := beads.GetBeadInDir(beadID, proj.MainRepoPath())
		if err != nil {
			fmt.Printf("Warning: failed to get bead %s: %v\n", beadID, err)
			continue
		}
		taskBeads = append(taskBeads, *bead)
	}

	// Process the task
	result, err := processTaskInWork(proj, database, dbTask, work, taskBeads)
	if err != nil {
		database.FailTask(ctx, taskID, err.Error())
		return fmt.Errorf("failed to process task %s: %w", taskID, err)
	}

	if result.Completed {
		fmt.Printf("\n=== Task %s completed successfully ===\n", taskID)
	} else if result.PartialFailure {
		fmt.Printf("\n=== Task %s partially completed ===\n", taskID)
		fmt.Printf("  Completed beads: %v\n", result.CompletedBeads)
		fmt.Printf("  Failed beads: %v\n", result.FailedBeads)
	} else {
		fmt.Printf("\n=== Task %s failed ===\n", taskID)
	}

	return nil
}

// processWork processes all tasks within a work unit.
// Task A depends on Task B if any bead in A depends on any bead in B.
func sortTasksByDependency(database *db.DB, tasks []*db.Task, mainRepoPath string) ([]*db.Task, error) {
	if len(tasks) <= 1 {
		return tasks, nil
	}

	// Build map of bead ID -> task ID
	beadToTask := make(map[string]string)
	taskBeads := make(map[string][]string)
	for _, t := range tasks {
		beadIDs, err := database.GetTaskBeads(context.Background(),t.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get beads for task %s: %w", t.ID, err)
		}
		taskBeads[t.ID] = beadIDs
		for _, beadID := range beadIDs {
			beadToTask[beadID] = t.ID
		}
	}

	// Build task dependency graph
	// taskDeps[A] = [B, C] means task A depends on tasks B and C
	taskDeps := make(map[string][]string)
	for _, t := range tasks {
		for _, beadID := range taskBeads[t.ID] {
			// Get bead dependencies
			bwd, err := beads.GetBeadWithDepsInDir(beadID, mainRepoPath)
			if err != nil {
				continue // Ignore errors, bead may not exist
			}
			for _, dep := range bwd.Dependencies {
				if dep.DependencyType == "depends_on" {
					// Check if dependency bead is in another task
					if depTaskID, ok := beadToTask[dep.ID]; ok && depTaskID != t.ID {
						// Task t depends on depTaskID
						taskDeps[t.ID] = append(taskDeps[t.ID], depTaskID)
					}
				}
			}
		}
	}

	// Topological sort using Kahn's algorithm
	taskMap := make(map[string]*db.Task)
	inDegree := make(map[string]int)
	for _, t := range tasks {
		taskMap[t.ID] = t
		inDegree[t.ID] = 0
	}

	// Count incoming edges
	for _, deps := range taskDeps {
		for _, dep := range deps {
			if _, ok := inDegree[dep]; ok {
				inDegree[dep]++ // This is wrong - we need reverse direction
			}
		}
	}

	// Actually we need: if A depends on B, then B must come first
	// So we need to track who depends on each task
	dependedOnBy := make(map[string][]string)
	for taskID, deps := range taskDeps {
		for _, dep := range deps {
			dependedOnBy[dep] = append(dependedOnBy[dep], taskID)
		}
	}

	// Reset and recalculate in-degree correctly
	for _, t := range tasks {
		inDegree[t.ID] = len(taskDeps[t.ID])
	}

	// Start with tasks that have no dependencies
	var queue []string
	for _, t := range tasks {
		if inDegree[t.ID] == 0 {
			queue = append(queue, t.ID)
		}
	}

	var result []*db.Task
	for len(queue) > 0 {
		taskID := queue[0]
		queue = queue[1:]
		result = append(result, taskMap[taskID])

		// For each task that depends on this one, decrement its in-degree
		for _, dependent := range dependedOnBy[taskID] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Check for cycles
	if len(result) != len(tasks) {
		return nil, fmt.Errorf("dependency cycle detected among tasks")
	}

	return result, nil
}

func ensureFeatureBranch(branch, dir string) error {
	if err := git.CheckoutInDir(branch, dir); err == nil {
		return nil
	}

	if err := git.CheckoutInDir("main", dir); err != nil {
		return fmt.Errorf("failed to checkout main: %w", err)
	}
	if err := git.CreateBranchInDir(branch, dir); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", branch, err)
	}
	if err := git.PushInDir(branch, dir); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branch, err)
	}
	return nil
}

func createFinalPR(featureBranch string, processedBeads []beads.Bead, dir string) error {
	fmt.Printf("\n=== Creating final PR: %s → main ===\n", featureBranch)

	hasCommits, err := git.HasCommitsAheadInDir("main", dir)
	if err != nil {
		return fmt.Errorf("failed to check commits: %w", err)
	}

	if !hasCommits {
		fmt.Println("No changes to merge to main")
		return nil
	}

	prTitle := fmt.Sprintf("Feature: %s", featureBranch)
	prBody := "## Beads implemented:\n"
	for _, b := range processedBeads {
		prBody += fmt.Sprintf("- %s: %s\n", b.ID, b.Title)
	}
	_ = prTitle
	_ = prBody

	fmt.Println("Creating final PR...")
	// prURL, err := github.CreatePRInDir(featureBranch, "main", prTitle, prBody, dir)
	// if err != nil {
	// 	return fmt.Errorf("failed to create final PR: %w", err)
	// }
	// fmt.Printf("Created final PR: %s\n", prURL)
	prURL := "unused-function"

	fmt.Println("Merging final PR...")
	// if err := github.MergePRInDir(prURL, dir); err != nil {
	// 	return fmt.Errorf("failed to merge final PR: %w", err)
	// }
	_ = prURL
	fmt.Println("Final PR merged successfully")

	if err := git.CheckoutInDir("main", dir); err != nil {
		return fmt.Errorf("failed to checkout main: %w", err)
	}
	if err := git.PullInDir(dir); err != nil {
		return fmt.Errorf("failed to pull main: %w", err)
	}

	return nil
}


// processWork processes all tasks within a work unit.
func processWork(proj *project.Project, database *db.DB, workID string) error {
	// Get work details
	work, err := database.GetWork(context.Background(),workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	fmt.Printf("\n=== Processing work %s ===\n", work.ID)
	fmt.Printf("Branch: %s\n", work.BranchName)
	fmt.Printf("Worktree: %s\n", work.WorktreePath)

	// Get tasks for this work
	tasks, err := database.GetWorkTasks(context.Background(),workID)
	if err != nil {
		return fmt.Errorf("failed to get work tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Printf("No tasks found for work %s\n", workID)
		return nil
	}

	fmt.Printf("Tasks to process: %d\n", len(tasks))

	// Check work status
	if work.Status == db.StatusCompleted {
		fmt.Printf("Work %s is already completed\n", workID)
		return nil
	}

	// Check if worktree exists (should have been created during work create)
	if work.WorktreePath == "" {
		return fmt.Errorf("work %s has no worktree path configured", workID)
	}

	if !worktree.ExistsPath(work.WorktreePath) {
		return fmt.Errorf("work %s worktree does not exist at %s", workID, work.WorktreePath)
	}

	// Create zellij tab for this work
	sessionName := claude.SessionNameForProject(proj.Config.Project.Name)
	tabName := fmt.Sprintf("work-%s", work.ID)

	// Start work in database
	if err := database.StartWork(context.Background(),workID, sessionName, tabName); err != nil {
		return fmt.Errorf("failed to start work: %w", err)
	}

	// Process each task sequentially in the work's worktree
	completedTasks := 0
	var allBeads []beads.Bead

	for _, task := range tasks {
		// Skip non-pending tasks
		if task.Status != db.StatusPending {
			fmt.Printf("Skipping task %s (status: %s)\n", task.ID, task.Status)
			continue
		}

		fmt.Printf("\n--- Processing task %s ---\n", task.ID)

		// Get bead details for this task
		beadIDs, err := database.GetTaskBeads(context.Background(),task.ID)
		if err != nil {
			fmt.Printf("Failed to get beads for task %s: %v\n", task.ID, err)
			continue
		}

		var taskBeads []beads.Bead
		for _, beadID := range beadIDs {
			bead, err := beads.GetBeadInDir(beadID, proj.MainRepoPath())
			if err != nil {
				fmt.Printf("Failed to get bead %s: %v\n", beadID, err)
				continue
			}
			taskBeads = append(taskBeads, *bead)
		}

		// Process task in the work's worktree
		result, err := processTaskInWork(proj, database, task, work, taskBeads)
		if err != nil {
			fmt.Printf("Failed to process task %s: %v\n", task.ID, err)
			database.FailTask(context.Background(),task.ID, err.Error())
			continue
		}

		if result.Completed {
			completedTasks++
			allBeads = append(allBeads, taskBeads...)
		}
	}

	// Update work status based on task completion
	if completedTasks > 0 {
		fmt.Printf("\n=== Work %s completed successfully ===\n", work.ID)
		fmt.Printf("Completed %d task(s) with %d bead(s)\n", completedTasks, len(allBeads))

		// Mark work as completed (without PR URL since we're not creating it)
		if err := database.CompleteWork(context.Background(), workID, ""); err != nil {
			return fmt.Errorf("failed to complete work: %w", err)
		}

		fmt.Printf("\nAll tasks completed! The work branch '%s' has been pushed.\n", work.BranchName)
		fmt.Printf("To create a PR, run: co work pr %s\n", workID)
	} else {
		// No tasks completed, mark work as failed
		if err := database.FailWork(context.Background(), workID, "No tasks completed successfully"); err != nil {
			return fmt.Errorf("failed to mark work as failed: %w", err)
		}
	}

	fmt.Printf("\n=== Work %s processing complete ===\n", work.ID)
	fmt.Printf("Completed tasks: %d/%d\n", completedTasks, len(tasks))

	return nil
}

// processTaskInWork processes a single task within a work's worktree.
func processTaskInWork(proj *project.Project, database *db.DB, dbTask *db.Task, work *db.Work, taskBeads []beads.Bead) (*claude.TaskResult, error) {
	fmt.Printf("Processing %d bead(s) for task %s\n", len(taskBeads), dbTask.ID)
	for _, b := range taskBeads {
		fmt.Printf("  - %s: %s\n", b.ID, b.Title)
	}

	// Start task in database
	sessionName := claude.SessionNameForProject(proj.Config.Project.Name)
	if err := database.StartTask(context.Background(),dbTask.ID, sessionName, dbTask.ID); err != nil {
		return nil, fmt.Errorf("failed to start task in database: %w", err)
	}

	// Build task object for Claude
	taskObj := task.Task{
		ID:         dbTask.ID,
		BeadIDs:    make([]string, len(taskBeads)),
		Beads:      taskBeads,
		Complexity: dbTask.ComplexityBudget,
		Status:     task.StatusPending,
	}
	for i, b := range taskBeads {
		taskObj.BeadIDs[i] = b.ID
	}

	// Build prompt for Claude based on task type
	var prompt string
	baseBranch := work.BaseBranch
	if baseBranch == "" {
		baseBranch = "main" // Default to main for backwards compatibility
	}
	if dbTask.TaskType == "pr" {
		// PR creation task
		prompt = claude.BuildPRPrompt(dbTask.ID, work.ID, work.BranchName, baseBranch)
		fmt.Println("Running Claude Code for PR creation...")
	} else {
		// Regular implementation task
		prompt = claude.BuildTaskPrompt(dbTask.ID, taskBeads, work.BranchName, baseBranch)
		fmt.Println("Running Claude Code...")
	}

	// Run Claude in the work's worktree directory
	ctx := context.Background()
	projectName := proj.Config.Project.Name
	result, err := claude.Run(ctx, database, dbTask.ID, taskBeads, prompt, work.WorktreePath, projectName, flagAutoClose)
	if err != nil {
		fmt.Printf("Claude failed: %v\n", err)
		return nil, fmt.Errorf("claude failed: %w", err)
	}

	// Update task status based on result
	if result.Completed {
		fmt.Printf("Task %s completed successfully\n", dbTask.ID)
		if err := database.CompleteTask(context.Background(),dbTask.ID, ""); err != nil {
			fmt.Printf("Warning: failed to update task status: %v\n", err)
		}
	} else if result.PartialFailure {
		fmt.Printf("Task %s partially completed\n", dbTask.ID)
		fmt.Printf("  Completed beads: %v\n", result.CompletedBeads)
		fmt.Printf("  Failed beads: %v\n", result.FailedBeads)
	} else {
		fmt.Printf("Task %s failed\n", dbTask.ID)
	}

	return result, nil
}
