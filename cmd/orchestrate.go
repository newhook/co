package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

const maxReviewIterations = 5

var (
	flagOrchestrateWork string
)

var orchestrateCmd = &cobra.Command{
	Use:    "orchestrate",
	Short:  "Execute tasks for a work unit (internal command)",
	Long:   `Internal command that polls for ready tasks and executes them. Used by zellij orchestration.`,
	Hidden: true,
	RunE:   runOrchestrate,
}

func init() {
	orchestrateCmd.Flags().StringVar(&flagOrchestrateWork, "work", "", "work ID to orchestrate")
}

func runOrchestrate(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	// Apply hooks.env to current process - inherited by child processes (Claude)
	applyHooksEnv(proj.Config.Hooks.Env)

	// Get work ID
	workID := flagOrchestrateWork
	if workID == "" {
		return fmt.Errorf("--work is required")
	}

	// Get work to verify it exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	fmt.Printf("=== Orchestrating work: %s ===\n", workID)
	fmt.Printf("Worktree: %s\n", work.WorktreePath)
	fmt.Printf("Branch: %s (base: %s)\n", work.BranchName, work.BaseBranch)

	// Main orchestration loop: poll for ready tasks and execute them
	for {
		// Get ready tasks (pending with all dependencies completed)
		readyTasks, err := proj.DB.GetReadyTasksForWork(ctx, workID)
		if err != nil {
			return fmt.Errorf("failed to get ready tasks: %w", err)
		}

		if len(readyTasks) == 0 {
			// No ready tasks - check if we're done or blocked
			allTasks, err := proj.DB.GetWorkTasks(ctx, workID)
			if err != nil {
				return fmt.Errorf("failed to get work tasks: %w", err)
			}

			pendingCount := 0
			processingCount := 0
			failedCount := 0
			completedCount := 0

			for _, t := range allTasks {
				switch t.Status {
				case db.StatusPending:
					pendingCount++
				case db.StatusProcessing:
					processingCount++
				case db.StatusFailed:
					failedCount++
				case db.StatusCompleted:
					completedCount++
				}
			}

			// If there are failures, abort
			if failedCount > 0 {
				return fmt.Errorf("work has %d failed task(s), aborting", failedCount)
			}

			// If tasks are processing, wait and retry
			if processingCount > 0 {
				fmt.Printf("Waiting for %d processing task(s)...\n", processingCount)
				time.Sleep(5 * time.Second)
				continue
			}

			// If pending tasks exist but none are ready, they're blocked
			if pendingCount > 0 {
				return fmt.Errorf("work has %d pending task(s) but none are ready (blocked by dependencies)", pendingCount)
			}

			// All tasks completed
			fmt.Printf("\n=== All tasks completed (%d total) ===\n", completedCount)
			break
		}

		// Execute the first ready task
		task := readyTasks[0]
		fmt.Printf("\n=== Executing task: %s (type: %s) ===\n", task.ID, task.TaskType)

		if err := executeTask(proj, task, work); err != nil {
			return fmt.Errorf("task %s failed: %w", task.ID, err)
		}
	}

	// Mark work as completed
	if err := proj.DB.CompleteWork(ctx, workID, work.PRURL); err != nil {
		fmt.Printf("Warning: failed to mark work as completed: %v\n", err)
	}

	fmt.Println("\n=== Work orchestration completed successfully ===")
	return nil
}

// executeTask executes a single task inline based on its type.
func executeTask(proj *project.Project, task *db.Task, work *db.Work) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	var prompt string
	var err error

	switch task.TaskType {
	case "estimate":
		// Build estimation prompt
		beadIDs, err := proj.DB.GetTaskBeads(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("failed to get task beads: %w", err)
		}
		var beadList []beads.Bead
		for _, beadID := range beadIDs {
			bead, err := beads.GetBeadInDir(beadID, mainRepoPath)
			if err != nil {
				fmt.Printf("Warning: failed to get bead %s: %v\n", beadID, err)
				continue
			}
			beadList = append(beadList, *bead)
		}
		prompt = claude.BuildEstimatePrompt(task.ID, beadList)

	case "implement":
		// Build implementation prompt
		beadIDs, err := proj.DB.GetTaskBeads(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("failed to get task beads: %w", err)
		}
		var beadList []beads.Bead
		for _, beadID := range beadIDs {
			bead, err := beads.GetBeadInDir(beadID, mainRepoPath)
			if err != nil {
				fmt.Printf("Warning: failed to get bead %s: %v\n", beadID, err)
				continue
			}
			beadList = append(beadList, *bead)
		}
		prompt = claude.BuildTaskPrompt(task.ID, beadList, work.BranchName, work.BaseBranch)

	case "review":
		// Build review prompt
		prompt = claude.BuildReviewPrompt(task.ID, work.ID, work.BranchName, work.BaseBranch)

	case "pr":
		// Build PR prompt
		prompt = claude.BuildPRPrompt(task.ID, work.ID, work.BranchName, work.BaseBranch)

	case "update-pr-description":
		// Build update PR description prompt
		prompt = claude.BuildUpdatePRDescriptionPrompt(task.ID, work.ID, work.PRURL, work.BranchName, work.BaseBranch)

	default:
		return fmt.Errorf("unknown task type: %s", task.TaskType)
	}

	// Execute Claude inline
	if err = claude.RunInline(ctx, proj.DB, task.ID, prompt, work.WorktreePath); err != nil {
		return err
	}

	// Post-execution: handle review-fix loop for review tasks
	if task.TaskType == "review" {
		if err := handleReviewFixLoop(proj, task, work); err != nil {
			return fmt.Errorf("failed to handle review-fix loop: %w", err)
		}
	}

	return nil
}

// handleReviewFixLoop checks if a review task found issues and creates fix tasks.
// It also creates a new review task and updates the PR task dependencies.
func handleReviewFixLoop(proj *project.Project, reviewTask *db.Task, work *db.Work) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	// Count how many review iterations we've had
	reviewCount := countReviewIterations(proj, work.ID)
	if reviewCount >= maxReviewIterations {
		fmt.Printf("Warning: Maximum review iterations (%d) reached, proceeding to PR\n", maxReviewIterations)
		return nil
	}

	// Check if the review created any issue beads via review_epic_id
	epicID, err := proj.DB.GetReviewEpicID(ctx, reviewTask.ID)
	if err != nil {
		return fmt.Errorf("failed to get review epic ID: %w", err)
	}

	var beadsToFix []beads.BeadWithDeps
	if epicID != "" {
		// Get all children of the review epic
		epicChildren, err := beads.GetBeadWithChildrenInDir(epicID, mainRepoPath)
		if err != nil {
			return fmt.Errorf("failed to get children of review epic %s: %w", epicID, err)
		}

		// Filter to only ready beads (excluding the epic itself)
		for _, b := range epicChildren {
			if b.ID != epicID && (b.Status == "" || b.Status == "ready" || b.Status == "open") {
				beadsToFix = append(beadsToFix, b)
			}
		}
	}

	if len(beadsToFix) == 0 {
		fmt.Println("Review passed - no issues found!")
		return nil
	}

	fmt.Printf("Review found %d issue(s) - creating fix tasks...\n", len(beadsToFix))

	// Create fix tasks for each bead
	var fixTaskIDs []string
	for _, b := range beadsToFix {
		nextNum, err := proj.DB.GetNextTaskNumber(ctx, work.ID)
		if err != nil {
			return fmt.Errorf("failed to get next task number: %w", err)
		}
		taskID := fmt.Sprintf("%s.%d", work.ID, nextNum)

		if err := proj.DB.CreateTask(ctx, taskID, "implement", []string{b.ID}, 0, work.ID); err != nil {
			return fmt.Errorf("failed to create fix task: %w", err)
		}

		// Fix task depends on the current review task
		if err := proj.DB.AddTaskDependency(ctx, taskID, reviewTask.ID); err != nil {
			return fmt.Errorf("failed to add dependency for fix task %s: %w", taskID, err)
		}

		fixTaskIDs = append(fixTaskIDs, taskID)
		fmt.Printf("Created fix task %s for bead %s: %s\n", taskID, b.ID, b.Title)
	}

	// Create a new review task that depends on all fix tasks
	newReviewTaskID := fmt.Sprintf("%s.review-%d", work.ID, reviewCount+1)
	if err := proj.DB.CreateTask(ctx, newReviewTaskID, "review", nil, 0, work.ID); err != nil {
		return fmt.Errorf("failed to create new review task: %w", err)
	}
	for _, fixID := range fixTaskIDs {
		if err := proj.DB.AddTaskDependency(ctx, newReviewTaskID, fixID); err != nil {
			return fmt.Errorf("failed to add dependency for new review task: %w", err)
		}
	}
	fmt.Printf("Created new review task: %s (depends on %d fix tasks)\n", newReviewTaskID, len(fixTaskIDs))

	// Update PR task to depend on the new review task instead of this one
	prTaskID := fmt.Sprintf("%s.pr", work.ID)
	// Remove old dependency and add new one
	if err := proj.DB.DeleteTaskDependency(ctx, prTaskID, reviewTask.ID); err != nil {
		fmt.Printf("Warning: failed to remove old PR dependency: %v\n", err)
	}
	if err := proj.DB.AddTaskDependency(ctx, prTaskID, newReviewTaskID); err != nil {
		return fmt.Errorf("failed to add new PR dependency: %w", err)
	}
	fmt.Printf("Updated PR task %s to depend on new review %s\n", prTaskID, newReviewTaskID)

	return nil
}

// countReviewIterations counts how many review tasks exist for a work unit.
func countReviewIterations(proj *project.Project, workID string) int {
	ctx := GetContext()
	tasks, err := proj.DB.GetWorkTasks(ctx, workID)
	if err != nil {
		return 0
	}

	count := 0
	reviewPrefix := workID + ".review"
	for _, task := range tasks {
		if strings.HasPrefix(task.ID, reviewPrefix) {
			count++
		}
	}
	return count
}

// applyHooksEnv sets environment variables from the hooks.env config.
// Variables are set on the current process and inherited by child processes.
// Format: ["KEY=value", "PATH=/a/b:$PATH"]
func applyHooksEnv(env []string) {
	for _, e := range env {
		// Split on first '=' only
		parts := splitEnvVar(e)
		if len(parts) == 2 {
			// Expand any environment variable references in the value
			expandedValue := os.ExpandEnv(parts[1])
			os.Setenv(parts[0], expandedValue)
		}
	}
}

// splitEnvVar splits "KEY=value" into ["KEY", "value"], handling values with '='
func splitEnvVar(s string) []string {
	idx := -1
	for i, c := range s {
		if c == '=' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+1:]}
}
