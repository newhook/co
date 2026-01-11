package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/task"
	"github.com/newhook/co/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	flagBranch  string
	flagLimit   int
	flagDryRun  bool
	flagNoMerge bool
	flagProject string
	flagWork    string
)

var runCmd = &cobra.Command{
	Use:   "run [task-id|work-id]",
	Short: "Execute pending tasks or works",
	Long: `Run executes pending tasks created by 'co plan'.

Without arguments:
- If in a work directory or --work specified: executes all tasks in that work
- Otherwise: executes all pending tasks across all works in dependency order

With an ID:
- If ID starts with "work-": executes all tasks in that work
- Otherwise: executes only that specific task

Works manage git worktrees and feature branches.
Each work gets its own worktree at <project>/<work-id>/tree/.
Tasks within a work run sequentially in the same worktree.

When --branch is specified (not "main"), PRs target that feature branch.
After all tasks complete, a final PR is created from the feature branch to main.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTasks,
}

func init() {
	runCmd.Flags().StringVarP(&flagBranch, "branch", "b", "main", "target branch for PRs")
	runCmd.Flags().IntVarP(&flagLimit, "limit", "n", 0, "maximum number of tasks to process (0 = unlimited)")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "show plan without executing")
	runCmd.Flags().BoolVar(&flagNoMerge, "no-merge", false, "create PRs but don't merge them")
	runCmd.Flags().StringVar(&flagProject, "project", "", "project directory (default: auto-detect from cwd)")
	runCmd.Flags().StringVar(&flagWork, "work", "", "work ID to run (default: auto-detect from cwd)")
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

	// Determine if we're running a work or a task
	// Priority: explicit arg > --work flag > directory context
	var workID string
	var taskID string

	if argID != "" {
		if strings.HasPrefix(argID, "work-") {
			workID = argID
		} else {
			taskID = argID
		}
	} else if flagWork != "" {
		workID = flagWork
	} else {
		// Try to detect work from current directory
		workID, _ = detectWorkFromDirectory(database, proj)
	}

	// If we have a work ID, process the work
	if workID != "" {
		return processWork(proj, database, workID)
	}

	// Get tasks to execute
	var tasks []*db.Task
	if taskID != "" {
		// Run specific task
		t, err := database.GetTask(context.Background(), taskID)
		if err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}
		if t == nil {
			return fmt.Errorf("task %s not found", taskID)
		}

		// Check if task is stuck in processing state without an active tab
		if t.Status == db.StatusProcessing {
			sessionName := fmt.Sprintf("co-%s", proj.Config.Project.Name)
			paneName := fmt.Sprintf("task-%s", t.ID)

			// A processing task must have both a session and an active tab
			ctx := context.Background()
			if t.ZellijSession == "" || !claude.TabExists(ctx, sessionName, paneName) {
				fmt.Printf("Task %s was marked as processing but no active tab found. Resetting to pending...\n", taskID)
				if err := database.ResetTaskStatus(context.Background(),t.ID); err != nil {
					return fmt.Errorf("failed to reset task status: %w", err)
				}
				t.Status = db.StatusPending
			}
		}

		// Allow retrying failed tasks
		if t.Status == db.StatusFailed {
			fmt.Printf("Task %s previously failed. Resetting to pending for retry...\n", taskID)
			if err := database.ResetTaskStatus(context.Background(),t.ID); err != nil {
				return fmt.Errorf("failed to reset task status: %w", err)
			}
			t.Status = db.StatusPending
		}

		if t.Status != db.StatusPending {
			return fmt.Errorf("task %s is not pending (status: %s)", taskID, t.Status)
		}
		tasks = []*db.Task{t}
	} else {
		// Get all pending tasks
		tasks, err = database.ListTasks(context.Background(),db.StatusPending)
		if err != nil {
			return fmt.Errorf("failed to list pending tasks: %w", err)
		}
	}

	if len(tasks) == 0 {
		fmt.Println("No pending tasks to execute. Run 'co plan' first to create tasks.")
		return nil
	}

	// Apply limit
	if flagLimit > 0 && len(tasks) > flagLimit {
		tasks = tasks[:flagLimit]
	}

	// Sort tasks by dependency order
	sortedTasks, err := sortTasksByDependency(database, tasks, proj.MainRepoPath())
	if err != nil {
		return fmt.Errorf("failed to sort tasks by dependency: %w", err)
	}

	// Determine if we're using a feature branch workflow
	useFeatureBranch := flagBranch != "main"

	// Dry run - show execution plan
	if flagDryRun {
		fmt.Printf("\nDry run: would execute %d task(s) in order:\n", len(sortedTasks))
		for i, t := range sortedTasks {
			beadIDs, _ := database.GetTaskBeads(context.Background(),t.ID)
			fmt.Printf("  %d. Task %s: %v\n", i+1, t.ID, beadIDs)
		}
		if useFeatureBranch {
			fmt.Printf("\nFeature branch workflow: PRs target '%s', final PR to 'main'\n", flagBranch)
		}
		fmt.Printf("\nWorktrees will be created at: %s/<task-id>/\n", proj.Root)
		return nil
	}

	// If using feature branch, ensure it exists in the main repo
	if useFeatureBranch {
		if err := ensureFeatureBranch(flagBranch, proj.MainRepoPath()); err != nil {
			return fmt.Errorf("failed to setup feature branch: %w", err)
		}
	}

	// Execute tasks in order
	processedCount := 0
	partialCount := 0
	var allBeads []beads.Bead

	for _, t := range sortedTasks {
		// Get bead details for this task
		beadIDs, err := database.GetTaskBeads(context.Background(),t.ID)
		if err != nil {
			return fmt.Errorf("failed to get beads for task %s: %w", t.ID, err)
		}

		var taskBeads []beads.Bead
		for _, beadID := range beadIDs {
			bead, err := beads.GetBeadInDir(beadID, proj.MainRepoPath())
			if err != nil {
				return fmt.Errorf("failed to get bead %s: %w", beadID, err)
			}
			taskBeads = append(taskBeads, *bead)
		}

		taskObj := task.Task{
			ID:         t.ID,
			BeadIDs:    beadIDs,
			Beads:      taskBeads,
			Complexity: t.ComplexityBudget,
			Status:     task.StatusPending,
		}

		result, err := processTaskWithWorktree(proj, database, taskObj)
		if err != nil {
			database.FailTask(context.Background(),t.ID, err.Error())
			return fmt.Errorf("failed to process task %s: %w", t.ID, err)
		}

		if result.Completed {
			processedCount++
			allBeads = append(allBeads, taskBeads...)
		} else if result.PartialFailure {
			partialCount++
			fmt.Printf("Task %s had partial failure, continuing with remaining tasks...\n", t.ID)
		}
	}

	if partialCount > 0 {
		fmt.Printf("Processed %d task(s) successfully, %d task(s) partially completed\n", processedCount, partialCount)
	} else {
		fmt.Printf("Successfully processed %d task(s)\n", processedCount)
	}

	// Create final PR from feature branch to main if applicable
	if useFeatureBranch && processedCount > 0 && !flagNoMerge {
		if err := createFinalPR(flagBranch, allBeads, proj.MainRepoPath()); err != nil {
			return fmt.Errorf("failed to create final PR: %w", err)
		}
	}

	return nil
}

// sortTasksByDependency sorts tasks so that dependencies are executed first.
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

// processTaskWithWorktree processes a task using an isolated worktree.
func processTaskWithWorktree(proj *project.Project, database *db.DB, t task.Task) (*claude.TaskResult, error) {
	fmt.Printf("\n=== Processing task %s (%d beads) ===\n", t.ID, len(t.Beads))
	for _, b := range t.Beads {
		fmt.Printf("  - %s: %s\n", b.ID, b.Title)
	}

	branchName := fmt.Sprintf("task/%s", t.ID)
	worktreePath := proj.WorktreePath(t.ID)
	mainRepoPath := proj.MainRepoPath()

	// Check if worktree already exists
	if worktree.ExistsPath(worktreePath) {
		fmt.Printf("Worktree already exists at %s, resuming...\n", worktreePath)
	} else {
		// Create worktree from the base branch
		fmt.Printf("Creating worktree at %s...\n", worktreePath)
		if err := worktree.Create(mainRepoPath, worktreePath, branchName); err != nil {
			return nil, fmt.Errorf("failed to create worktree: %w", err)
		}

		// Initialize mise in worktree (optional - warn on error)
		// Note: bd init/hooks NOT needed - worktrees share .beads/ with main
		if err := mise.Initialize(worktreePath); err != nil {
			fmt.Printf("Warning: mise initialization in worktree failed: %v\n", err)
		}
	}

	// Start task in database (worktree is now managed at work level)
	sessionName := claude.SessionNameForProject(proj.Config.Project.Name)
	if err := database.StartTask(context.Background(),t.ID, sessionName, t.ID); err != nil {
		return nil, fmt.Errorf("failed to start task in database: %w", err)
	}

	// Get task type from database to determine which prompt to use
	dbTask, err := database.GetTask(context.Background(),t.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task type: %w", err)
	}

	// Build appropriate prompt for Claude based on task type
	var prompt string
	if dbTask != nil && dbTask.TaskType == "estimate" {
		// For estimate tasks, use the estimation prompt
		prompt = claude.BuildEstimatePrompt(t.ID, t.Beads)
		fmt.Println("Running Claude Code for estimation task...")
	} else {
		// For implementation tasks, use the task prompt
		prompt = claude.BuildTaskPrompt(t.ID, t.Beads, branchName, flagBranch)
		fmt.Println("Running Claude Code for implementation task...")
	}

	// Run Claude in the worktree directory
	ctx := context.Background()
	projectName := proj.Config.Project.Name
	result, err := claude.Run(ctx, database, t.ID, t.Beads, prompt, worktreePath, projectName)
	if err != nil {
		fmt.Printf("Claude failed: %v\n", err)
		fmt.Printf("Worktree kept for debugging at: %s\n", worktreePath)
		return nil, fmt.Errorf("claude failed: %w", err)
	}

	// Handle partial failure - create PR with completed work
	if result.PartialFailure {
		fmt.Printf("\nPartial failure detected:\n")
		fmt.Printf("  Completed beads: %v\n", result.CompletedBeads)
		fmt.Printf("  Failed beads: %v\n", result.FailedBeads)

		// Check if there are commits to create a partial PR
		hasCommits, err := git.HasCommitsAheadInDir(flagBranch, worktreePath)
		if err == nil && hasCommits {
			fmt.Println("Creating partial PR with completed work...")
			prURL, prErr := createPartialPR(t, result, branchName, worktreePath)
			if prErr != nil {
				fmt.Printf("Warning: failed to create partial PR: %v\n", prErr)
			} else {
				fmt.Printf("Created partial PR: %s\n", prURL)
			}
		}

		// Keep worktree for debugging
		fmt.Printf("Worktree kept for debugging at: %s\n", worktreePath)
		fmt.Printf("To retry failed beads, run: co plan <failed-bead-id> && co run\n")
		return result, nil
	}

	// Full success - clean up worktree
	if result.Completed {
		fmt.Printf("Cleaning up worktree %s...\n", worktreePath)
		if err := worktree.Remove(mainRepoPath, worktreePath); err != nil {
			fmt.Printf("Warning: failed to remove worktree: %v\n", err)
		}
	} else {
		// Full failure - keep worktree
		fmt.Printf("Worktree kept for debugging at: %s\n", worktreePath)
	}

	return result, nil
}

// createPartialPR creates a PR with partial work from a failed task.
func createPartialPR(t task.Task, result *claude.TaskResult, branchName, worktreePath string) (string, error) {
	// Push the branch first
	if err := git.PushInDir(branchName, worktreePath); err != nil {
		return "", fmt.Errorf("failed to push branch: %w", err)
	}

	// Build PR body
	prTitle := fmt.Sprintf("[Partial] Task %s", t.ID)
	prBody := "## Partial completion - some beads failed\n\n"
	prBody += "### Completed beads:\n"
	for _, id := range result.CompletedBeads {
		prBody += fmt.Sprintf("- %s\n", id)
	}
	prBody += "\n### Failed beads (require retry):\n"
	for _, id := range result.FailedBeads {
		prBody += fmt.Sprintf("- %s\n", id)
	}
	prBody += "\n**Note:** Run `co plan <bead-id> && co run` to retry failed beads.\n"

	// Create PR
	prURL, err := github.CreatePRInDir(branchName, flagBranch, prTitle, prBody, worktreePath)
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %w", err)
	}

	return prURL, nil
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

	fmt.Println("Creating final PR...")
	prURL, err := github.CreatePRInDir(featureBranch, "main", prTitle, prBody, dir)
	if err != nil {
		return fmt.Errorf("failed to create final PR: %w", err)
	}
	fmt.Printf("Created final PR: %s\n", prURL)

	fmt.Println("Merging final PR...")
	if err := github.MergePRInDir(prURL, dir); err != nil {
		return fmt.Errorf("failed to merge final PR: %w", err)
	}
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

	// Create PR for the entire work if tasks completed successfully
	if completedTasks > 0 {
		fmt.Printf("\n=== Creating PR for work %s ===\n", work.ID)

		// Create PR from work branch
		prTitle := fmt.Sprintf("Work %s: %d task(s) completed", work.ID, completedTasks)
		prBody := fmt.Sprintf("## Work %s\n\n", work.ID)
		prBody += fmt.Sprintf("Completed %d task(s) with %d bead(s):\n\n", completedTasks, len(allBeads))

		for _, bead := range allBeads {
			prBody += fmt.Sprintf("- %s: %s\n", bead.ID, bead.Title)
		}

		prURL, err := github.CreatePRInDir(work.BranchName, flagBranch, prTitle, prBody, work.WorktreePath)
		if err != nil {
			return fmt.Errorf("failed to create PR for work: %w", err)
		}

		fmt.Printf("Created PR: %s\n", prURL)

		// Complete work in database
		if err := database.CompleteWork(context.Background(),workID, prURL); err != nil {
			return fmt.Errorf("failed to complete work: %w", err)
		}
	} else {
		// No tasks completed, mark work as failed
		if err := database.FailWork(context.Background(),workID, "No tasks completed successfully"); err != nil {
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

	// Build prompt for Claude
	prompt := claude.BuildTaskPrompt(dbTask.ID, taskBeads, work.BranchName, flagBranch)
	fmt.Println("Running Claude Code...")

	// Run Claude in the work's worktree directory
	ctx := context.Background()
	projectName := proj.Config.Project.Name
	result, err := claude.Run(ctx, database, dbTask.ID, taskBeads, prompt, work.WorktreePath, projectName)
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
