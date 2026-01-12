package cmd

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
)

// generateBranchNameFromBead creates a git-friendly branch name from a bead's title.
// It converts the title to lowercase, replaces spaces with hyphens,
// removes special characters, and prefixes with "feat/".
func generateBranchNameFromBead(bead *beads.Bead) string {
	title := bead.Title

	// Convert to lowercase
	title = strings.ToLower(title)

	// Replace spaces and underscores with hyphens
	title = strings.ReplaceAll(title, " ", "-")
	title = strings.ReplaceAll(title, "_", "-")

	// Remove characters that aren't alphanumeric or hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	title = reg.ReplaceAllString(title, "")

	// Collapse multiple hyphens into one
	reg = regexp.MustCompile(`-+`)
	title = reg.ReplaceAllString(title, "-")

	// Trim leading/trailing hyphens
	title = strings.Trim(title, "-")

	// Truncate if too long (git branch names can be long, but let's be reasonable)
	if len(title) > 50 {
		title = title[:50]
		// Don't end with a hyphen
		title = strings.TrimRight(title, "-")
	}

	// Add prefix based on common conventions
	return fmt.Sprintf("feat/%s", title)
}

// ensureUniqueBranchName checks if a branch already exists and appends a suffix if needed.
// Returns a unique branch name that doesn't conflict with existing branches.
func ensureUniqueBranchName(repoPath, baseName string) (string, error) {
	// Check if the base name is available
	if !branchExists(repoPath, baseName) {
		return baseName, nil
	}

	// Try appending suffixes until we find an available name
	for i := 2; i <= 100; i++ {
		candidate := fmt.Sprintf("%s-%d", baseName, i)
		if !branchExists(repoPath, candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not find unique branch name after 100 attempts (base: %s)", baseName)
}

// branchExists checks if a branch exists locally or remotely.
func branchExists(repoPath, branchName string) bool {
	// Check local branches
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
	cmd.Dir = repoPath
	if cmd.Run() == nil {
		return true
	}

	// Check remote branches
	cmd = exec.Command("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branchName)
	cmd.Dir = repoPath
	if cmd.Run() == nil {
		return true
	}

	return false
}

// collectBeadsForAutomatedWorkflow collects all beads to include in the workflow.
// For a bead with dependencies, it includes all transitive dependencies.
// For an epic bead, it includes all child beads.
func collectBeadsForAutomatedWorkflow(beadID, dir string) ([]beads.BeadWithDeps, error) {
	// First, get the main bead
	mainBead, err := beads.GetBeadWithDepsInDir(beadID, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}

	// Check if this bead has children (is an epic)
	var hasChildren bool
	for _, dep := range mainBead.Dependencies {
		if dep.DependencyType == "parent_of" {
			hasChildren = true
			break
		}
	}

	if hasChildren {
		// For epics, collect all children
		allBeads, err := beads.GetBeadWithChildrenInDir(beadID, dir)
		if err != nil {
			return nil, fmt.Errorf("failed to get children for epic %s: %w", beadID, err)
		}
		// Filter to only include non-epic beads (skip the epic itself)
		var result []beads.BeadWithDeps
		for _, b := range allBeads {
			// Check if this is an epic (has children)
			isEpic := false
			for _, dep := range b.Dependencies {
				if dep.DependencyType == "parent_of" {
					isEpic = true
					break
				}
			}
			if !isEpic {
				result = append(result, b)
			}
		}
		return result, nil
	}

	// For regular beads, collect transitive dependencies
	return beads.GetTransitiveDependenciesInDir(beadID, dir)
}

// runAutomatedWorkflow executes the complete automated workflow from bead to PR.
// This is a convenience wrapper that initializes and starts an orchestrated workflow.
// The workflow runs through 7 steps:
// 1. Creating work unit with auto-generated branch name (StepCreateWork)
// 2. Collecting all beads to include (StepCollectBeads)
// 3. Planning tasks with auto-grouping (StepPlanTasks)
// 4. Executing tasks (StepExecuteTasks)
// 5. Waiting for task completion (StepWaitCompletion)
// 6. Running review-fix loop until clean (StepReviewFix)
// 7. Creating PR (StepCreatePR)
//
// Each step is independently executable and resumable through the orchestration system.
// Use 'co orchestrate --work <id>' to resume a workflow from where it left off.
func runAutomatedWorkflow(proj *project.Project, beadID string, baseBranch string) error {
	fmt.Printf("Starting automated workflow for bead: %s\n", beadID)

	// Initialize workflow state - the workflow ID is temporary until StepCreateWork creates the actual work
	workflowID, err := InitWorkflow(proj, beadID, baseBranch)
	if err != nil {
		return fmt.Errorf("failed to initialize workflow: %w", err)
	}

	fmt.Printf("Initialized workflow: %s\n", workflowID)
	fmt.Println("Workflow will execute steps:")
	fmt.Println("  0. Create work (worktree, branch)")
	fmt.Println("  1. Collect beads")
	fmt.Println("  2. Plan tasks")
	fmt.Println("  3. Execute tasks")
	fmt.Println("  4. Wait for completion")
	fmt.Println("  5. Review-fix loop")
	fmt.Println("  6. Create PR")

	// Run the workflow using the orchestration system
	// This executes step-by-step with state persistence
	return runOrchestratedWorkflow(proj, workflowID)
}

// runOrchestratedWorkflow executes the workflow step by step using the orchestration system.
// Each step is independently resumable if the workflow is interrupted.
func runOrchestratedWorkflow(proj *project.Project, workflowID string) error {
	ctx := GetContext()

	for {
		// Get current workflow state
		state, err := proj.DB.GetWorkflowState(ctx, workflowID)
		if err != nil {
			return fmt.Errorf("failed to get workflow state: %w", err)
		}
		if state == nil {
			return fmt.Errorf("workflow %s not found", workflowID)
		}

		// Check if workflow is completed or failed
		if state.StepStatus == "completed" {
			fmt.Println("\n=== Workflow completed successfully ===")
			return nil
		}
		if state.StepStatus == "failed" {
			return fmt.Errorf("workflow failed at step %d: %s", state.CurrentStep, state.ErrorMessage)
		}

		// Execute the current step by calling orchestrate command logic directly
		// This avoids spawning a subprocess and keeps everything in the same process
		fmt.Printf("\n=== Step %d: %s ===\n", state.CurrentStep, stepName(state.CurrentStep))

		// Run the orchestration step
		flagOrchestrateWorkflow = workflowID
		flagOrchestrateStep = state.CurrentStep
		if err := runOrchestrate(nil, nil); err != nil {
			return err
		}

		// Check if the workflow ID changed (happens after StepCreateWork)
		newState, err := proj.DB.GetWorkflowState(ctx, workflowID)
		if err != nil {
			return fmt.Errorf("failed to get updated workflow state: %w", err)
		}

		// If the step was completed, the next step should be set
		if newState != nil && newState.StepStatus == "completed" {
			return nil
		}
	}
}

// stepName returns a human-readable name for each step
func stepName(step int) string {
	switch step {
	case StepCreateWork:
		return "Create Work"
	case StepCollectBeads:
		return "Collect Beads"
	case StepPlanTasks:
		return "Plan Tasks"
	case StepExecuteTasks:
		return "Execute Tasks"
	case StepWaitCompletion:
		return "Wait for Completion"
	case StepReviewFix:
		return "Review-Fix Loop"
	case StepCreatePR:
		return "Create PR"
	case StepCompleted:
		return "Completed"
	default:
		return fmt.Sprintf("Unknown Step %d", step)
	}
}

// planAutoGroupForWork is a helper that runs auto-grouping planning for the given beads.
func planAutoGroupForWork(proj *project.Project, beadList []beads.Bead, workID string, work *db.Work) error {
	// Temporarily set the flag values
	origAutoGroup := flagPlanAutoGroup
	origBudget := flagPlanBudget
	origForceEstimate := flagPlanForceEstimate

	flagPlanAutoGroup = true
	flagPlanBudget = 70
	flagPlanForceEstimate = false

	defer func() {
		flagPlanAutoGroup = origAutoGroup
		flagPlanBudget = origBudget
		flagPlanForceEstimate = origForceEstimate
	}()

	return planAutoGroup(proj, beadList, workID, work)
}

// runReviewFixLoop runs a loop of code review followed by fixes until no issues are found.
// Maximum iterations is limited to prevent infinite loops.
func runReviewFixLoop(proj *project.Project, workID string) error {
	const maxIterations = 5
	mainRepoPath := proj.MainRepoPath()

	for i := range maxIterations {
		fmt.Printf("\n--- Review iteration %d/%d ---\n", i+1, maxIterations)

		// Capture the set of ready bead IDs before review (for fallback detection)
		preReviewBeadIDs := make(map[string]bool)
		preReviewBeads, err := beads.GetReadyBeadsInDir(mainRepoPath)
		if err != nil {
			return fmt.Errorf("failed to get pre-review beads: %w", err)
		}
		for _, b := range preReviewBeads {
			preReviewBeadIDs[b.ID] = true
		}

		// Create and run a review task
		reviewTaskID, err := createReviewTask(proj, workID)
		if err != nil {
			return fmt.Errorf("failed to create review task: %w", err)
		}

		// Process the review task
		if err := processTask(proj, reviewTaskID); err != nil {
			return fmt.Errorf("review task failed: %w", err)
		}

		// Check if review created any issue beads
		hasIssues, err := checkForReviewIssues(proj, workID, preReviewBeadIDs)
		if err != nil {
			return fmt.Errorf("failed to check for review issues: %w", err)
		}

		if !hasIssues {
			fmt.Println("Review passed - no issues found!")
			return nil
		}

		fmt.Println("Review found issues - fixing...")

		// Plan and execute fix tasks for the new issues
		if err := planAndExecuteFixTasks(proj, workID); err != nil {
			return fmt.Errorf("failed to fix review issues: %w", err)
		}
	}

	return fmt.Errorf("review-fix loop exceeded maximum iterations (%d)", maxIterations)
}

// createReviewTask creates a review task for a work unit.
func createReviewTask(proj *project.Project, workID string) (string, error) {
	ctx := GetContext()

	// Get work to ensure it exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return "", fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return "", fmt.Errorf("work %s not found", workID)
	}

	// Generate unique review task ID
	tasks, err := proj.DB.GetWorkTasks(ctx, workID)
	if err != nil {
		return "", fmt.Errorf("failed to get work tasks: %w", err)
	}

	reviewCount := 0
	reviewPrefix := fmt.Sprintf("%s.review", workID)
	for _, task := range tasks {
		if strings.HasPrefix(task.ID, reviewPrefix) {
			reviewCount++
		}
	}

	reviewTaskID := fmt.Sprintf("%s.review-%d", workID, reviewCount+1)

	// Create the review task
	err = proj.DB.CreateTask(ctx, reviewTaskID, "review", []string{}, 0, workID)
	if err != nil {
		return "", fmt.Errorf("failed to create review task: %w", err)
	}

	fmt.Printf("Created review task: %s\n", reviewTaskID)
	return reviewTaskID, nil
}

// checkForReviewIssues checks if the review created any new issue beads.
// It first checks for a review epic with children, then falls back to detecting new beads.
// preReviewBeadIDs contains the IDs of beads that existed before the review ran.
func checkForReviewIssues(proj *project.Project, workID string, preReviewBeadIDs map[string]bool) (bool, error) {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	// First, try to find a review task with an epic set
	reviewTaskID, err := proj.DB.GetLatestReviewTaskWithEpic(ctx, workID)
	if err != nil {
		return false, fmt.Errorf("failed to find review task: %w", err)
	}

	if reviewTaskID != "" {
		// Get the review epic ID
		epicID, err := proj.DB.GetReviewEpicID(ctx, reviewTaskID)
		if err != nil {
			return false, fmt.Errorf("failed to get review epic ID: %w", err)
		}
		if epicID != "" {
			// Check if the epic has any ready children
			epicChildren, err := beads.GetBeadWithChildrenInDir(epicID, mainRepoPath)
			if err != nil {
				return false, fmt.Errorf("failed to get children of review epic: %w", err)
			}

			for _, b := range epicChildren {
				if b.ID != epicID && (b.Status == "" || b.Status == "ready") {
					return true, nil
				}
			}
			return false, nil
		}
	}

	// Fallback: check for any NEW beads that didn't exist before the review
	readyBeads, err := beads.GetReadyBeadsInDir(mainRepoPath)
	if err != nil {
		return false, fmt.Errorf("failed to get ready beads: %w", err)
	}

	// Only consider beads that are NEW (not in the pre-review set)
	for _, b := range readyBeads {
		if !preReviewBeadIDs[b.ID] {
			return true, nil
		}
	}

	return false, nil
}

// planAndExecuteFixTasks plans and executes tasks to fix review issues.
// It only processes beads that are children of the review epic, not all ready beads.
func planAndExecuteFixTasks(proj *project.Project, workID string) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	// Find the most recent review task that has a review_epic_id set
	reviewTaskID, err := proj.DB.GetLatestReviewTaskWithEpic(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to find review task: %w", err)
	}
	if reviewTaskID == "" {
		// Fallback to old behavior if no review task with epic found
		fmt.Println("Warning: No review task with epic ID found, falling back to ready beads scan")
		return planAndExecuteFixTasksLegacy(proj, workID)
	}

	// Get the review epic ID
	epicID, err := proj.DB.GetReviewEpicID(ctx, reviewTaskID)
	if err != nil {
		return fmt.Errorf("failed to get review epic ID: %w", err)
	}
	if epicID == "" {
		return fmt.Errorf("review task %s has no review_epic_id set", reviewTaskID)
	}

	fmt.Printf("Looking for fix beads under review epic: %s\n", epicID)

	// Get all children of the review epic
	epicChildren, err := beads.GetBeadWithChildrenInDir(epicID, mainRepoPath)
	if err != nil {
		return fmt.Errorf("failed to get children of review epic %s: %w", epicID, err)
	}

	// Filter to only ready beads (excluding the epic itself)
	var beadsToFix []beads.BeadWithDeps
	for _, b := range epicChildren {
		// Skip the epic itself
		if b.ID == epicID {
			continue
		}
		// Only include beads that are ready (status == "ready" or empty for new beads)
		if b.Status == "" || b.Status == "ready" {
			beadsToFix = append(beadsToFix, b)
		}
	}

	if len(beadsToFix) == 0 {
		fmt.Println("No beads to fix under review epic")
		return nil
	}

	// Plan fix tasks
	fmt.Printf("Planning fix tasks for %d bead(s) under review epic %s...\n", len(beadsToFix), epicID)

	for _, b := range beadsToFix {
		// Generate task ID
		nextNum, err := proj.DB.GetNextTaskNumber(ctx, workID)
		if err != nil {
			return fmt.Errorf("failed to get next task number: %w", err)
		}
		taskID := fmt.Sprintf("%s.%d", workID, nextNum)

		if err := proj.DB.CreateTask(ctx, taskID, "implement", []string{b.ID}, 0, workID); err != nil {
			return fmt.Errorf("failed to create fix task: %w", err)
		}
		fmt.Printf("Created fix task %s for bead %s: %s\n", taskID, b.ID, b.Title)
	}

	// Execute fix tasks
	return processWork(proj, workID)
}

// planAndExecuteFixTasksLegacy is the original implementation that processes all ready beads.
// This is kept as a fallback for backwards compatibility.
func planAndExecuteFixTasksLegacy(proj *project.Project, workID string) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	// Get ready beads for fixing
	readyBeads, err := beads.GetReadyBeadsInDir(mainRepoPath)
	if err != nil {
		return fmt.Errorf("failed to get ready beads: %w", err)
	}

	if len(readyBeads) == 0 {
		fmt.Println("No beads to fix")
		return nil
	}

	// Plan fix tasks
	fmt.Printf("Planning fix tasks for %d bead(s)...\n", len(readyBeads))

	for _, b := range readyBeads {
		// Generate task ID
		nextNum, err := proj.DB.GetNextTaskNumber(ctx, workID)
		if err != nil {
			return fmt.Errorf("failed to get next task number: %w", err)
		}
		taskID := fmt.Sprintf("%s.%d", workID, nextNum)

		if err := proj.DB.CreateTask(ctx, taskID, "implement", []string{b.ID}, 0, workID); err != nil {
			return fmt.Errorf("failed to create fix task: %w", err)
		}
		fmt.Printf("Created fix task %s for bead %s\n", taskID, b.ID)
	}

	// Execute fix tasks
	return processWork(proj, workID)
}

// createWorkPR creates a PR for a completed work unit.
func createWorkPR(proj *project.Project, workID string) error {
	ctx := GetContext()

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Generate PR task ID
	prTaskID := fmt.Sprintf("%s.pr", workID)

	// Create PR task
	err = proj.DB.CreateTask(ctx, prTaskID, "pr", []string{}, 0, workID)
	if err != nil {
		return fmt.Errorf("failed to create PR task: %w", err)
	}

	fmt.Printf("Created PR task: %s\n", prTaskID)

	// Execute PR task
	return processTask(proj, prTaskID)
}
