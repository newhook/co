package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/task"
	"github.com/spf13/cobra"
)

// Spinner frames for animated waiting display
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
	if work.Auto {
		fmt.Printf("Mode: Automated workflow\n")
	}

	// If this is an automated workflow work and no tasks exist yet, set up the automated workflow
	if work.Auto {
		tasks, err := proj.DB.GetWorkTasks(ctx, workID)
		if err != nil {
			return fmt.Errorf("failed to check for existing tasks: %w", err)
		}

		// Only set up automated workflow if no tasks exist yet
		if len(tasks) == 0 {
			fmt.Println("\nSetting up automated workflow...")

			// Create estimate task from unassigned work beads (post-estimation will create implement tasks)
			mainRepoPath := proj.MainRepoPath()
			err := CreateEstimateTaskFromWorkBeads(ctx, proj, workID, mainRepoPath, os.Stdout)
			if err != nil {
				return fmt.Errorf("failed to create estimate task: %w", err)
			}

			// The orchestrator loop will handle executing the tasks
			fmt.Println("Automated workflow tasks created. Starting execution...")
		}
	}

	// Reset any stuck processing tasks from a previous run
	// When the orchestrator restarts, any tasks that were processing are now orphaned
	// since the Claude process was killed along with the orchestrator
	if err := resetStuckProcessingTasks(ctx, proj, workID); err != nil {
		return fmt.Errorf("failed to reset stuck tasks: %w", err)
	}

	// Start activity tracker for health monitoring in a separate goroutine
	// This avoids the busy loop issue from having a select with default in the main loop
	activityInterval := proj.Config.Scheduler.GetActivityUpdateInterval()
	activityTicker := time.NewTicker(activityInterval)
	defer activityTicker.Stop()

	// Run activity updates in background
	go func() {
		// Recover from any panics to prevent health monitoring from stopping
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Error: activity tracker panicked: %v\n", r)
				// Log the panic but don't crash the entire orchestrator
				// The main loop will continue running
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-activityTicker.C:
				// Update last_activity for all processing tasks of this work
				if err := updateWorkTaskActivity(ctx, proj.DB, workID); err != nil {
					// Log but don't fail - this is just health monitoring
					fmt.Printf("Warning: failed to update task activity: %v\n", err)
				}
			}
		}
	}()

	// Start the scheduler watcher for this work
	// This replaces file-based polling and fixed timers with a database-driven scheduler
	StartSchedulerWatcher(ctx, proj, workID)

	// Main orchestration loop: poll for ready tasks and execute them
	for {

		// Check if work still exists (may have been destroyed)
		work, err = proj.DB.GetWork(ctx, workID)
		if err != nil {
			return fmt.Errorf("failed to check work status: %w", err)
		}
		if work == nil {
			fmt.Println("Work has been destroyed, exiting orchestrator.")
			return nil
		}

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

			// If tasks are processing, wait and retry
			if processingCount > 0 {
				msg := fmt.Sprintf("Waiting for %d processing task(s)...", processingCount)
				spinnerWait(msg, 5*time.Second)
				continue
			}

			// If there are failures, wait for manual intervention or new tasks
			if failedCount > 0 {
				msg := fmt.Sprintf("Work has %d failed task(s). Waiting for manual intervention or new tasks...", failedCount)
				spinnerWait(msg, 10*time.Second)
				continue
			}

			// If pending tasks exist but none are ready, they're blocked
			if pendingCount > 0 {
				msg := fmt.Sprintf("Waiting: %d pending task(s) blocked by dependencies...", pendingCount)
				spinnerWait(msg, 5*time.Second)
				continue
			}

			// All tasks completed - transition work to completed status if not already
			if completedCount > 0 && work.Status != db.StatusCompleted {
				// Find PR URL from the PR task (if one exists)
				prURL := ""
				for _, t := range allTasks {
					if t.TaskType == "pr" && t.Status == db.StatusCompleted && t.PRURL != "" {
						prURL = t.PRURL
						break
					}
				}

				// Use transactional function to complete work and schedule feedback polling atomically
				prFeedbackInterval := proj.Config.Scheduler.GetPRFeedbackInterval()
				commentResolutionInterval := proj.Config.Scheduler.GetCommentResolutionInterval()
				if err := proj.DB.CompleteWorkAndScheduleFeedback(ctx, workID, prURL, prFeedbackInterval, commentResolutionInterval); err != nil {
					fmt.Printf("Warning: failed to mark work as completed: %v\n", err)
				} else {
					fmt.Printf("\n=== Work %s completed ===\n", workID)
					if prURL != "" {
						fmt.Printf("PR: %s\n", prURL)
						fmt.Println("PR feedback polling scheduled")
					}
					// Refresh work status
					work, _ = proj.DB.GetWork(ctx, workID)
				}
			}

			// Wait for new tasks with spinner
			var msg string
			if completedCount > 0 {
				msg = fmt.Sprintf("All %d task(s) completed. Waiting for new tasks...", completedCount)
			} else {
				msg = "No tasks yet. Waiting for tasks to be created..."
			}
			spinnerWait(msg, 5*time.Second)
			continue
		}

		// Execute the first ready task
		task := readyTasks[0]
		fmt.Printf("\n=== Executing task: %s (type: %s) ===\n", task.ID, task.TaskType)

		// Update activity when starting execution
		if err := proj.DB.UpdateTaskActivity(ctx, task.ID, time.Now()); err != nil {
			fmt.Printf("Warning: failed to update task activity at start: %v\n", err)
		}

		if err := executeTask(proj, task, work); err != nil {
			return fmt.Errorf("task %s failed: %w", task.ID, err)
		}
	}
}

// executeTask executes a single task inline based on its type.
func executeTask(proj *project.Project, t *db.Task, work *db.Work) error {
	ctx := GetContext()

	// Create a context with timeout from configuration
	timeout := proj.Config.Claude.GetTaskTimeout()
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fmt.Printf("Task timeout: %v\n", timeout)

	// Build prompt for Claude based on task type
	prompt, err := buildPromptForTask(taskCtx, proj, t, work)
	if err != nil {
		return err
	}

	// Execute Claude inline with timeout context
	if err = claude.Run(taskCtx, proj.DB, t.ID, prompt, work.WorktreePath, proj.Config); err != nil {
		// Check if it was a timeout error
		if errors.Is(err, context.DeadlineExceeded) {
			// Mark the task as failed due to timeout
			// Use context.Background() since the original context is cancelled
			if dbErr := proj.DB.FailTask(context.Background(), t.ID, fmt.Sprintf("Task timed out after %v", timeout)); dbErr != nil {
				fmt.Printf("Warning: failed to mark timed out task as failed: %v\n", dbErr)
			}
			return fmt.Errorf("task %s timed out after %v", t.ID, timeout)
		}
		return err
	}

	// Post-execution handling based on task type
	switch t.TaskType {
	case "estimate":
		if err := handlePostEstimation(proj, t, work); err != nil {
			return fmt.Errorf("failed to create post-estimation tasks: %w", err)
		}
	case "review":
		if err := handleReviewFixLoop(proj, t, work); err != nil {
			return fmt.Errorf("failed to handle review completion: %w", err)
		}
	}

	return nil
}

// handlePostEstimation creates implement, review, and PR tasks after estimation completes.
// Uses bin-packing to group beads based on their complexity estimates.
func handlePostEstimation(proj *project.Project, estimateTask *db.Task, work *db.Work) error {
	ctx := GetContext()

	fmt.Println("Creating implement, review, and PR tasks based on complexity estimates...")

	// Get the beads that were estimated
	beadIDs, err := proj.DB.GetTaskBeads(ctx, estimateTask.ID)
	if err != nil {
		return fmt.Errorf("failed to get task beads: %w", err)
	}

	if len(beadIDs) == 0 {
		return fmt.Errorf("no beads found for estimate task %s", estimateTask.ID)
	}

	// Get issues with dependencies for planning
	issuesResult, err := proj.Beads.GetBeadsWithDeps(ctx, beadIDs)
	if err != nil {
		return fmt.Errorf("failed to get bead details: %w", err)
	}

	// Verify all beads were found
	for _, beadID := range beadIDs {
		if _, found := issuesResult.Beads[beadID]; !found {
			return fmt.Errorf("bead %s not found", beadID)
		}
	}

	// Convert map to slice
	beadList := make([]beads.Bead, 0, len(issuesResult.Beads))
	for _, b := range issuesResult.Beads {
		beadList = append(beadList, b)
	}

	// Create planner with cached complexity estimator
	estimator := task.NewLLMEstimator(proj.DB, work.WorktreePath, proj.Config.Project.Name, work.ID)
	planner := task.NewDefaultPlanner(estimator)

	// Use default budget of 70 for bin-packing
	const budget = 70
	fmt.Printf("Planning tasks with budget %d...\n", budget)

	tasks, err := planner.Plan(ctx, beadList, issuesResult.Dependencies, budget)
	if err != nil {
		return fmt.Errorf("failed to plan tasks: %w", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("planner returned no tasks for %d beads", len(beadIDs))
	}

	// Create implement tasks
	var implementTaskIDs []string
	for _, t := range tasks {
		nextNum, err := proj.DB.GetNextTaskNumber(ctx, work.ID)
		if err != nil {
			return fmt.Errorf("failed to get next task number: %w", err)
		}
		taskID := fmt.Sprintf("%s.%d", work.ID, nextNum)

		if err := proj.DB.CreateTask(ctx, taskID, "implement", t.BeadIDs, t.Complexity, work.ID); err != nil {
			return fmt.Errorf("failed to create implement task: %w", err)
		}

		// Add dependency: implement depends on estimate
		if err := proj.DB.AddTaskDependency(ctx, taskID, estimateTask.ID); err != nil {
			return fmt.Errorf("failed to add dependency for %s: %w", taskID, err)
		}

		implementTaskIDs = append(implementTaskIDs, taskID)
		fmt.Printf("Created implement task %s (complexity: %d) with %d bead(s): %v\n",
			taskID, t.Complexity, len(t.BeadIDs), t.BeadIDs)
	}

	// Create review task (depends on all implement tasks)
	// PR task is NOT created here - it will be created after review passes
	reviewTaskID := fmt.Sprintf("%s.review-1", work.ID)
	if err := proj.DB.CreateTask(ctx, reviewTaskID, "review", nil, 0, work.ID); err != nil {
		return fmt.Errorf("failed to create review task: %w", err)
	}
	for _, implID := range implementTaskIDs {
		if err := proj.DB.AddTaskDependency(ctx, reviewTaskID, implID); err != nil {
			return fmt.Errorf("failed to add dependency for review: %w", err)
		}
	}
	fmt.Printf("Created review task: %s (depends on %d implement tasks)\n", reviewTaskID, len(implementTaskIDs))

	fmt.Printf("Successfully created %d implement task(s) and 1 review task\n", len(implementTaskIDs))
	fmt.Println("PR task will be created after review passes.")
	return nil
}


// updateWorkTaskActivity updates the last_activity timestamp for all processing tasks of a work.
func updateWorkTaskActivity(ctx context.Context, db *db.DB, workID string) error {
	// Get all processing tasks for this work
	tasks, err := db.GetWorkTasks(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work tasks: %w", err)
	}

	// Update activity for each processing task
	for _, task := range tasks {
		if task.Status == "processing" {
			if err := db.UpdateTaskActivity(ctx, task.ID, time.Now()); err != nil {
				// Log but don't fail on individual task updates
				fmt.Printf("Warning: failed to update activity for task %s: %v\n", task.ID, err)
			}
		}
	}
	return nil
}

// spinnerWait displays an animated spinner with a message for the specified duration.
// The spinner updates every 100ms to create a smooth animation effect.
// Does not print a newline so the spinner can continue on the same line.
func spinnerWait(msg string, duration time.Duration) {
	start := time.Now()
	frameIdx := 0
	for time.Since(start) < duration {
		fmt.Printf("\r%s %s", spinnerFrames[frameIdx], msg)
		frameIdx = (frameIdx + 1) % len(spinnerFrames)
		time.Sleep(100 * time.Millisecond)
	}
	// Don't print newline - let caller decide or let next spinnerWait overwrite
}

// handleReviewFixLoop checks if a review task found issues and creates fix tasks.
// If review passes (no issues), creates the PR task.
// If review finds issues, creates fix tasks and a new review task.
func handleReviewFixLoop(proj *project.Project, reviewTask *db.Task, work *db.Work) error {
	ctx := GetContext()

	// Check if this is a manual review task (auto_workflow=false)
	// Manual review tasks should not trigger automated workflow (fix tasks or PR creation)
	autoWorkflow, err := proj.DB.GetTaskMetadata(ctx, reviewTask.ID, "auto_workflow")
	if err == nil && autoWorkflow == "false" {
		fmt.Println("Manual review task completed - skipping automated workflow")
		return nil
	}

	// Count how many review iterations we've had
	reviewCount := countReviewIterations(proj, work.ID)
	maxIterations := proj.Config.Workflow.GetMaxReviewIterations()
	if reviewCount >= maxIterations {
		fmt.Printf("Warning: Maximum review iterations (%d) reached, proceeding to PR\n", maxIterations)
		return createPRTask(proj, work, reviewTask.ID)
	}

	// Check if the review created any issue beads under the root issue
	var beadsToFix []beads.Bead
	if work.RootIssueID != "" {
		// Get all children of the root issue
		rootChildrenIssues, err := proj.Beads.GetBeadWithChildren(ctx, work.RootIssueID)
		if err != nil {
			return fmt.Errorf("failed to get children of root issue %s: %w", work.RootIssueID, err)
		}

		// Filter to only ready beads that were created by this review task
		// (excluding the root issue itself)
		expectedExternalRef := fmt.Sprintf("review-%s", reviewTask.ID)
		for _, issue := range rootChildrenIssues {
			if issue.ID != work.RootIssueID &&
				beads.IsWorkableStatus(issue.Status) &&
				issue.ExternalRef == expectedExternalRef {
				beadsToFix = append(beadsToFix, issue)
			}
		}
	}

	// If work has a PR URL, also check for PR feedback
	if len(beadsToFix) == 0 && work.PRURL != "" {
		fmt.Println("Review passed - checking for PR feedback...")

		// Process PR feedback - creates beads but doesn't add them to work
		_, err := ProcessPRFeedback(ctx, proj, proj.DB, work.ID, 2)
		if err != nil {
			fmt.Printf("Warning: failed to check PR feedback: %v\n", err)
		} else {
			// Re-check for new beads from PR feedback
			if work.RootIssueID != "" {
				// Re-fetch children of root issue to see if feedback created new beads
				rootChildrenIssues, err := proj.Beads.GetBeadWithChildren(ctx, work.RootIssueID)
				if err == nil {
					// Filter for any new open beads (not just review-created ones)
					for _, issue := range rootChildrenIssues {
						if issue.ID != work.RootIssueID &&
							beads.IsWorkableStatus(issue.Status) {
							// Check if this bead is already in a task
							inTask, _ := proj.DB.IsBeadInTask(ctx, work.ID, issue.ID)
							if !inTask {
								beadsToFix = append(beadsToFix, issue)
							}
						}
					}
				}
			}
		}
	}

	if len(beadsToFix) == 0 {
		fmt.Println("Review passed and no PR feedback issues found!")
		return createPRTask(proj, work, reviewTask.ID)
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

	return nil
}

// createPRTask creates the PR task that depends on a review task.
func createPRTask(proj *project.Project, work *db.Work, reviewTaskID string) error {
	ctx := GetContext()

	prTaskID := fmt.Sprintf("%s.pr", work.ID)
	if err := proj.DB.CreateTask(ctx, prTaskID, "pr", nil, 0, work.ID); err != nil {
		return fmt.Errorf("failed to create PR task: %w", err)
	}
	if err := proj.DB.AddTaskDependency(ctx, prTaskID, reviewTaskID); err != nil {
		return fmt.Errorf("failed to add dependency for PR: %w", err)
	}
	fmt.Printf("Created PR task: %s (depends on %s)\n", prTaskID, reviewTaskID)
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

// resetStuckProcessingTasks finds tasks that are stuck in "processing" status
// and resets them to "pending". This happens when the orchestrator is killed
// while a task is running - the Claude process is also killed, but the task
// remains marked as processing in the database.
func resetStuckProcessingTasks(ctx context.Context, proj *project.Project, workID string) error {
	// Get all tasks for this work
	tasks, err := proj.DB.GetWorkTasks(ctx, workID)
	if err != nil {
		return err
	}

	resetCount := 0
	for _, t := range tasks {
		if t.Status == db.StatusProcessing {
			fmt.Printf("Resetting stuck task %s from processing to pending...\n", t.ID)
			if err := proj.DB.ResetTaskStatus(ctx, t.ID); err != nil {
				return fmt.Errorf("failed to reset task %s: %w", t.ID, err)
			}
			// Also reset bead statuses for this task
			if err := proj.DB.ResetTaskBeadStatuses(ctx, t.ID); err != nil {
				return fmt.Errorf("failed to reset task bead statuses for %s: %w", t.ID, err)
			}
			resetCount++
		}
	}

	if resetCount > 0 {
		fmt.Printf("Reset %d stuck task(s)\n", resetCount)
	}

	return nil
}

// checkAndResolveComments checks for feedback items where the bead is closed and posts resolution comments to GitHub
func checkAndResolveComments(ctx context.Context, proj *project.Project, workID, prURL string) {
	checkAndResolveCommentsInternal(ctx, proj, workID, prURL, false)
}

func checkAndResolveCommentsQuiet(ctx context.Context, proj *project.Project, workID, prURL string) {
	checkAndResolveCommentsInternal(ctx, proj, workID, prURL, true)
}

func checkAndResolveCommentsInternal(ctx context.Context, proj *project.Project, workID, prURL string, quiet bool) {
	// Get unresolved feedback items for this work
	feedbacks, err := proj.DB.GetUnresolvedFeedbackForClosedBeads(ctx, workID)
	if err != nil {
		if !quiet {
			fmt.Printf("Error getting unresolved feedback: %v\n", err)
		}
		logging.Error("failed to get unresolved feedback", "error", err)
		return
	}

	if len(feedbacks) == 0 {
		return
	}

	if !quiet {
		fmt.Printf("\nChecking %d feedback items for resolution...\n", len(feedbacks))
	}
	logging.Debug("checking feedback items for resolution", "count", len(feedbacks))

	// Collect closed bead IDs
	var closedBeadIDs []string
	for _, feedback := range feedbacks {
		if feedback.BeadID == nil || feedback.SourceID == nil {
			continue
		}

		// Check if the bead is actually closed
		bead, err := proj.Beads.GetBead(ctx, *feedback.BeadID)
		if err != nil {
			if !quiet {
				fmt.Printf("Error getting bead %s: %v\n", *feedback.BeadID, err)
			}
			logging.Error("failed to get bead", "bead_id", *feedback.BeadID, "error", err)
			continue
		}

		if bead != nil && bead.Status == beads.StatusClosed {
			closedBeadIDs = append(closedBeadIDs, *feedback.BeadID)
		}
	}

	// Resolve feedback for all closed beads
	if len(closedBeadIDs) > 0 {
		if err := ResolveFeedbackForBeads(ctx, proj.DB, proj.Beads, workID, closedBeadIDs); err != nil {
			if !quiet {
				fmt.Printf("Error resolving feedback comments: %v\n", err)
			}
			logging.Error("failed to resolve feedback comments", "error", err)
		}
	}
}
