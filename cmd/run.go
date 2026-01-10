package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/task"
	"github.com/newhook/co/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	flagBranch    string
	flagLimit     int
	flagDryRun    bool
	flagNoMerge   bool
	flagDeps      bool
	flagProject   string
	flagAutoGroup bool
	flagBudget    int
)

var runCmd = &cobra.Command{
	Use:   "run [bead-id]",
	Short: "Process ready issues with Claude Code",
	Long: `Run processes ready issues by invoking Claude Code to implement
each one and creating PRs for the changes.

If a bead ID is provided, only that issue will be processed.

Each task gets its own worktree at <project>/<task-id>/.
Worktrees are cleaned up on success, kept on failure for debugging.

When --branch is specified (not "main"), PRs target that feature branch.
After all issues complete, a final PR is created from the feature branch to main.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBeads,
}

func init() {
	runCmd.Flags().StringVarP(&flagBranch, "branch", "b", "main", "target branch for PRs")
	runCmd.Flags().IntVarP(&flagLimit, "limit", "n", 0, "maximum number of issues to process (0 = unlimited)")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "show plan without executing")
	runCmd.Flags().BoolVar(&flagNoMerge, "no-merge", false, "create PRs but don't merge them")
	runCmd.Flags().BoolVar(&flagDeps, "deps", false, "also process open dependencies of the specified bead")
	runCmd.Flags().StringVar(&flagProject, "project", "", "project directory (default: auto-detect from cwd)")
	runCmd.Flags().BoolVar(&flagAutoGroup, "auto-group", false, "automatically group beads by complexity using LLM estimation")
	runCmd.Flags().IntVar(&flagBudget, "budget", 70, "complexity budget per task (1-100, used with --auto-group)")
}

func runBeads(cmd *cobra.Command, args []string) error {
	var beadID string
	if len(args) > 0 {
		beadID = args[0]
	}

	proj, err := findProject()
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}

	fmt.Printf("Using project: %s\n", proj.Config.Project.Name)

	// Get beads to process from the main repo directory
	beadList, err := getBeadsToProcess(beadID, proj.MainRepoPath())
	if err != nil {
		return err
	}

	if len(beadList) == 0 {
		fmt.Println("No beads to process")
		return nil
	}

	// Apply limit
	if flagLimit > 0 && len(beadList) > flagLimit {
		beadList = beadList[:flagLimit]
	}

	// Determine if we're using a feature branch workflow
	useFeatureBranch := flagBranch != "main"

	// Open project's tracking database (needed for both dry-run with --task and actual processing)
	database, err := proj.OpenDB()
	if err != nil {
		return fmt.Errorf("failed to open tracking database: %w", err)
	}
	defer proj.Close()

	// Task mode: group beads into tasks using bin-packing
	if flagAutoGroup {
		return runTaskMode(proj, database, beadList, useFeatureBranch)
	}

	// Default mode: create single-bead tasks for each bead
	return runSingleBeadMode(proj, database, beadList, useFeatureBranch)
}

// runSingleBeadMode runs the default mode where each bead gets its own task.
func runSingleBeadMode(proj *project.Project, database *db.DB, beadList []beads.Bead, useFeatureBranch bool) error {
	// Dry run - just show plan
	if flagDryRun {
		fmt.Printf("Dry run: would process %d bead(s):\n", len(beadList))
		for _, b := range beadList {
			fmt.Printf("  - %s: %s\n", b.ID, b.Title)
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

	// Process each bead as a single-bead task
	processedCount := 0
	partialCount := 0
	for _, bead := range beadList {
		// Create a task containing just this bead
		t := task.Task{
			ID:         bead.ID, // Use bead ID as task ID for single-bead tasks
			BeadIDs:    []string{bead.ID},
			Beads:      []beads.Bead{bead},
			Complexity: 0, // No complexity estimation in single-bead mode
			Status:     task.StatusPending,
		}

		// Create task in database
		if err := database.CreateTask(t.ID, t.BeadIDs, t.Complexity); err != nil {
			return fmt.Errorf("failed to create task in database: %w", err)
		}

		result, err := processTaskWithWorktree(proj, database, t)
		if err != nil {
			// Record failure in database
			database.FailTask(t.ID, err.Error())
			return fmt.Errorf("failed to process bead %s: %w", bead.ID, err)
		}

		if result.Completed {
			processedCount++
		} else if result.PartialFailure {
			partialCount++
			fmt.Printf("Bead %s had partial failure, continuing with remaining beads...\n", bead.ID)
		}
	}

	if partialCount > 0 {
		fmt.Printf("Processed %d bead(s) successfully, %d bead(s) partially completed\n", processedCount, partialCount)
	} else {
		fmt.Printf("Successfully processed %d bead(s)\n", processedCount)
	}

	// Create final PR from feature branch to main if applicable
	if useFeatureBranch && processedCount > 0 && !flagNoMerge {
		if err := createFinalPR(flagBranch, beadList, proj.MainRepoPath()); err != nil {
			return fmt.Errorf("failed to create final PR: %w", err)
		}
	}

	return nil
}

// runTaskMode runs the task-based processing mode.
func runTaskMode(proj *project.Project, database *db.DB, beadList []beads.Bead, useFeatureBranch bool) error {
	fmt.Println("Task mode enabled: grouping beads by complexity...")

	// Get beads with dependencies for planning
	beadsWithDeps, err := getBeadsWithDeps(beadList, proj.MainRepoPath())
	if err != nil {
		return fmt.Errorf("failed to get bead dependencies: %w", err)
	}

	// Create planner with complexity estimator
	estimator := task.NewLLMEstimator(database)
	planner := task.NewDefaultPlanner(estimator)

	// Plan tasks
	fmt.Printf("Planning tasks with budget %d...\n", flagBudget)
	tasks, err := planner.Plan(beadsWithDeps, flagBudget)
	if err != nil {
		return fmt.Errorf("failed to plan tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks to process")
		return nil
	}

	// Dry run - show task assignments
	if flagDryRun {
		fmt.Printf("\nDry run: would process %d task(s):\n", len(tasks))
		for _, t := range tasks {
			fmt.Printf("\n  Task %s (complexity: %d):\n", t.ID, t.Complexity)
			for _, b := range t.Beads {
				fmt.Printf("    - %s: %s\n", b.ID, b.Title)
			}
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

	// Process each task
	processedCount := 0
	partialCount := 0
	for _, t := range tasks {
		// Create task in database
		if err := database.CreateTask(t.ID, t.BeadIDs, t.Complexity); err != nil {
			return fmt.Errorf("failed to create task in database: %w", err)
		}

		result, err := processTaskWithWorktree(proj, database, t)
		if err != nil {
			// Record failure in database
			database.FailTask(t.ID, err.Error())
			return fmt.Errorf("failed to process task %s: %w", t.ID, err)
		}

		if result.Completed {
			processedCount++
		} else if result.PartialFailure {
			partialCount++
			// Don't fail the whole run for partial failures
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
		// Collect all beads from tasks for PR description
		var allBeads []beads.Bead
		for _, t := range tasks {
			allBeads = append(allBeads, t.Beads...)
		}
		if err := createFinalPR(flagBranch, allBeads, proj.MainRepoPath()); err != nil {
			return fmt.Errorf("failed to create final PR: %w", err)
		}
	}

	return nil
}

// getBeadsWithDeps retrieves full dependency information for beads.
func getBeadsWithDeps(beadList []beads.Bead, dir string) ([]beads.BeadWithDeps, error) {
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

// processTaskWithWorktree processes a task using an isolated worktree.
// Returns the TaskResult and any error encountered.
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
	}

	// Start task in database
	sessionName := claude.SessionNameForProject(proj.Config.Project.Name)
	if err := database.StartTask(t.ID, sessionName, t.ID, worktreePath); err != nil {
		return nil, fmt.Errorf("failed to start task in database: %w", err)
	}

	// Build prompt for Claude
	prompt := claude.BuildTaskPrompt(t.ID, t.Beads, branchName, flagBranch)

	// Run Claude in the worktree directory
	fmt.Println("Running Claude Code...")
	ctx := context.Background()
	projectName := proj.Config.Project.Name
	result, err := claude.RunTaskInProject(ctx, database, t.ID, t.Beads, prompt, worktreePath, projectName)
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
		fmt.Printf("To retry failed beads, run: co run <failed-bead-id>\n")
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
	prBody += "\n**Note:** Run `co run <bead-id>` to retry failed beads.\n"

	// Create PR
	prURL, err := github.CreatePRInDir(branchName, flagBranch, prTitle, prBody, worktreePath)
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %w", err)
	}

	return prURL, nil
}

// findProject finds the project from --project flag or current directory.
func findProject() (*project.Project, error) {
	if flagProject != "" {
		return project.Find(flagProject)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return project.Find(cwd)
}

func getBeadsToProcess(beadID, dir string) ([]beads.Bead, error) {
	if beadID != "" {
		if flagDeps {
			return getBeadWithDeps(beadID, dir)
		}
		bead, err := beads.GetBeadInDir(beadID, dir)
		if err != nil {
			return nil, err
		}
		return []beads.Bead{*bead}, nil
	}

	if flagDeps {
		return nil, fmt.Errorf("--deps requires a bead ID argument")
	}

	return beads.GetReadyBeadsInDir(dir)
}

func getBeadWithDeps(beadID, dir string) ([]beads.Bead, error) {
	beadWithDeps, err := beads.GetBeadWithDepsInDir(beadID, dir)
	if err != nil {
		return nil, err
	}

	var result []beads.Bead

	for _, dep := range beadWithDeps.Dependencies {
		if dep.DependencyType == "depends_on" && dep.Status == "open" {
			depBeads, err := getBeadWithDeps(dep.ID, dir)
			if err != nil {
				return nil, fmt.Errorf("failed to get dependency %s: %w", dep.ID, err)
			}
			result = append(result, depBeads...)
		}
	}

	result = append(result, beads.Bead{
		ID:          beadWithDeps.ID,
		Title:       beadWithDeps.Title,
		Description: beadWithDeps.Description,
	})

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
