package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/mise"
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
// This includes:
// 1. Creating work unit with auto-generated branch name
// 2. Planning tasks (with auto-grouping)
// 3. Executing tasks
// 4. Running review-fix loop until clean
// 5. Creating PR
func runAutomatedWorkflow(proj *project.Project, beadID string, baseBranch string) error {
	ctx := context.Background()
	mainRepoPath := proj.MainRepoPath()

	fmt.Printf("Starting automated workflow for bead: %s\n", beadID)

	// Step 1: Get the main bead to generate branch name
	mainBead, err := beads.GetBeadInDir(beadID, mainRepoPath)
	if err != nil {
		return fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}

	branchName := generateBranchNameFromBead(mainBead)
	fmt.Printf("Generated branch name: %s\n", branchName)

	// Step 2: Collect all beads to include
	beadsToProcess, err := collectBeadsForAutomatedWorkflow(beadID, mainRepoPath)
	if err != nil {
		return fmt.Errorf("failed to collect beads: %w", err)
	}

	if len(beadsToProcess) == 0 {
		return fmt.Errorf("no beads to process for %s", beadID)
	}

	fmt.Printf("Collected %d bead(s) for workflow:\n", len(beadsToProcess))
	for _, b := range beadsToProcess {
		fmt.Printf("  - %s: %s\n", b.ID, b.Title)
	}

	// Step 3: Create work unit
	workID, err := proj.DB.GenerateWorkID(ctx, branchName, proj.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("failed to generate work ID: %w", err)
	}
	fmt.Printf("Generated work ID: %s\n", workID)

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Create git worktree inside work directory
	worktreePath := filepath.Join(workDir, "tree")

	// Create worktree with new branch based on the specified base branch
	cmd := exec.Command("git", "worktree", "add", worktreePath, "-b", branchName, baseBranch)
	cmd.Dir = mainRepoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create worktree: %w\n%s", err, output)
	}

	// Push branch and set upstream
	cmd = exec.Command("git", "push", "--set-upstream", "origin", branchName)
	cmd.Dir = worktreePath
	if output, err := cmd.CombinedOutput(); err != nil {
		exec.Command("git", "worktree", "remove", worktreePath).Run()
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to push and set upstream: %w\n%s", err, output)
	}

	// Initialize mise in worktree if needed
	if err := mise.Initialize(worktreePath); err != nil {
		fmt.Printf("Warning: mise initialization failed: %v\n", err)
	}

	// Create work record in database
	if err := proj.DB.CreateWork(ctx, workID, worktreePath, branchName, baseBranch); err != nil {
		exec.Command("git", "worktree", "remove", worktreePath).Run()
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	fmt.Printf("Created work: %s\n", workID)
	fmt.Printf("Directory: %s\n", workDir)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("Branch: %s\n", branchName)

	// Step 4: Plan tasks with auto-grouping
	fmt.Println("\n=== Planning tasks ===")

	// Convert beads for planning
	var beadList []beads.Bead
	for _, b := range beadsToProcess {
		beadList = append(beadList, beads.Bead{
			ID:          b.ID,
			Title:       b.Title,
			Description: b.Description,
		})
	}

	// Use auto-grouping to plan tasks
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}

	if err := planAutoGroupForWork(proj, beadList, workID, work); err != nil {
		return fmt.Errorf("failed to plan tasks: %w", err)
	}

	// Step 5: Execute tasks
	fmt.Println("\n=== Executing tasks ===")
	if err := processWork(proj, workID); err != nil {
		return fmt.Errorf("failed to execute tasks: %w", err)
	}

	// Step 6: Run review-fix loop
	fmt.Println("\n=== Running review-fix loop ===")
	if err := runReviewFixLoop(proj, workID); err != nil {
		return fmt.Errorf("review-fix loop failed: %w", err)
	}

	// Step 7: Create PR
	fmt.Println("\n=== Creating PR ===")
	if err := createWorkPR(proj, workID); err != nil {
		return fmt.Errorf("failed to create PR: %w", err)
	}

	fmt.Printf("\n=== Automated workflow completed for bead %s ===\n", beadID)
	return nil
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

	for i := range maxIterations {
		fmt.Printf("\n--- Review iteration %d/%d ---\n", i+1, maxIterations)

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
		hasIssues, err := checkForReviewIssues(proj, workID)
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
	ctx := context.Background()

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
// It first checks for a review epic with children, then falls back to heuristic scanning.
func checkForReviewIssues(proj *project.Project, workID string) (bool, error) {
	ctx := context.Background()
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

	// Fallback: heuristic check for beads that look like review issues
	readyBeads, err := beads.GetReadyBeadsInDir(mainRepoPath)
	if err != nil {
		return false, fmt.Errorf("failed to get ready beads: %w", err)
	}

	// Look for beads that look like review issues
	// Convention: review creates beads under an epic with title "Review: <work-id>"
	for _, b := range readyBeads {
		if strings.Contains(b.Title, "Review:") || strings.Contains(b.Title, workID) {
			return true, nil
		}
	}

	return false, nil
}

// planAndExecuteFixTasks plans and executes tasks to fix review issues.
// It only processes beads that are children of the review epic, not all ready beads.
func planAndExecuteFixTasks(proj *project.Project, workID string) error {
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()

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
