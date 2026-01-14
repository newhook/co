package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/project"
	cosignal "github.com/newhook/co/internal/signal"
	"github.com/newhook/co/internal/worktree"
	"github.com/spf13/cobra"
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Manage work units",
	Long:  `Manage work units that group tasks together. Each work has its own git worktree and feature branch.`,
}

var workCreateCmd = &cobra.Command{
	Use:   "create <bead-args...>",
	Short: "Create a new work unit from beads",
	Long: `Create a new work unit from one or more beads.
Creates a subdirectory with a git worktree for isolated development.

Beads can be specified with grouping syntax:
  co work create bead-1,bead-2 bead-3
  - Comma-separated beads are grouped together for a single task
  - Space-separated arguments create separate task groups

Epics are automatically expanded to include all child beads.
Transitive dependencies are also included.

Branch name is auto-generated from bead titles - you'll be prompted to accept or customize.

With --auto flag, runs the full automated workflow:
1. Creates tasks from beads
2. Executes all tasks
3. Runs review-fix loop until clean
4. Creates a pull request`,
	Args: cobra.MinimumNArgs(1),
	RunE: runWorkCreate,
}

var workListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all work units",
	Long:  `List all work units with their status and details.`,
	Args:  cobra.NoArgs,
	RunE:  runWorkList,
}

var workShowCmd = &cobra.Command{
	Use:   "show [<id>]",
	Short: "Show work details (current directory or specified)",
	Long: `Show detailed information about a work unit.
If no ID is provided, shows the work for the current directory context.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkShow,
}

var workDestroyCmd = &cobra.Command{
	Use:   "destroy <id>",
	Short: "Destroy a work unit and its worktree",
	Long: `Destroy a work unit, removing its subdirectory and database records.
This is a destructive operation that cannot be undone.`,
	Args: cobra.ExactArgs(1),
	RunE: runWorkDestroy,
}

var workPRCmd = &cobra.Command{
	Use:   "pr [<id>]",
	Short: "Create a PR task for Claude to generate pull request",
	Long: `Create a special task for Claude to review the work and create a pull request.
If no ID is provided, uses the work for the current directory context.

Claude will analyze all completed tasks and beads to generate a comprehensive PR description.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkPR,
}

var workReviewCmd = &cobra.Command{
	Use:   "review [<id>]",
	Short: "Create a review task to examine code changes",
	Long: `Create a task for Claude to review code changes in a work unit.
If no ID is provided, uses the work for the current directory context.

Claude will examine the work's branch/PR for quality, security issues,
and adherence to project standards.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkReview,
}

var workAddCmd = &cobra.Command{
	Use:   "add <bead-args...>",
	Short: "Add beads to work",
	Long: `Add beads to an existing work unit.

Beads can be specified with grouping syntax:
  co work add bead-4,bead-5 bead-6
  - Comma-separated beads are grouped together for a single task
  - Space-separated arguments create separate task groups

Epics are automatically expanded to include all child beads.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runWorkAdd,
}

var workRemoveCmd = &cobra.Command{
	Use:   "remove <bead-ids...>",
	Short: "Remove beads from work",
	Long: `Remove beads from an existing work unit.
Beads that are already assigned to a pending or processing task cannot be removed.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runWorkRemove,
}

var (
	flagBaseBranch  string
	flagAutoRun     bool
	flagReviewAuto  bool
	flagAddWork     string
	flagRemoveWork  string
)

func init() {
	workCreateCmd.Flags().StringVar(&flagBaseBranch, "base", "main", "base branch to create feature branch from (also used as PR target)")
	workCreateCmd.Flags().BoolVar(&flagAutoRun, "auto", false, "run full automated workflow (implement, review, fix, PR)")
	workReviewCmd.Flags().BoolVar(&flagReviewAuto, "auto", false, "run review-fix loop until clean")
	workAddCmd.Flags().StringVar(&flagAddWork, "work", "", "work ID (default: auto-detect from current directory)")
	workRemoveCmd.Flags().StringVar(&flagRemoveWork, "work", "", "work ID (default: auto-detect from current directory)")
	workCmd.AddCommand(workCreateCmd)
	workCmd.AddCommand(workListCmd)
	workCmd.AddCommand(workShowCmd)
	workCmd.AddCommand(workDestroyCmd)
	workCmd.AddCommand(workPRCmd)
	workCmd.AddCommand(workReviewCmd)
	workCmd.AddCommand(workAddCmd)
	workCmd.AddCommand(workRemoveCmd)
}

func runWorkCreate(cmd *cobra.Command, args []string) error {
	baseBranch := flagBaseBranch
	ctx := GetContext()

	// Find project
	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	mainRepoPath := proj.MainRepoPath()

	// Parse positional args into bead groups
	// Each arg is a comma-separated list of bead IDs (same group)
	// Different args are different groups
	beadGroups, err := parseBeadGroups(args, mainRepoPath)
	if err != nil {
		return err
	}

	if len(beadGroups) == 0 {
		return fmt.Errorf("no beads specified")
	}

	// Collect all beads for branch name generation
	var allBeads []*beads.Bead
	for _, group := range beadGroups {
		allBeads = append(allBeads, group.beads...)
	}

	// Generate branch name from bead titles
	branchName := generateBranchNameFromBeads(allBeads)
	branchName, err = ensureUniqueBranchName(mainRepoPath, branchName)
	if err != nil {
		return fmt.Errorf("failed to find unique branch name: %w", err)
	}

	// Prompt user to accept or customize branch name
	branchName, err = promptForBranchName(branchName)
	if err != nil {
		return err
	}

	// Generate work ID
	workID, err := proj.DB.GenerateWorkID(ctx, branchName, proj.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("failed to generate work ID: %w", err)
	}
	fmt.Printf("Work ID: %s\n", workID)

	// Block signals during critical worktree creation
	cosignal.BlockSignals()
	defer cosignal.UnblockSignals()

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Create git worktree inside work directory
	worktreePath := filepath.Join(workDir, "tree")

	// Create worktree with new branch
	if err := worktree.Create(mainRepoPath, worktreePath, branchName, baseBranch); err != nil {
		os.RemoveAll(workDir)
		return err
	}

	// Push branch and set upstream
	if err := git.PushSetUpstreamInDir(branchName, worktreePath); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return err
	}

	// Initialize mise in worktree if needed
	if err := mise.Initialize(worktreePath); err != nil {
		fmt.Printf("Warning: mise initialization failed: %v\n", err)
	}

	// Create work record in database
	if err := proj.DB.CreateWork(ctx, workID, worktreePath, branchName, baseBranch); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	// Initialize bead group counter
	if err := proj.DB.InitializeBeadGroupCounter(ctx, workID); err != nil {
		fmt.Printf("Warning: failed to initialize bead group counter: %v\n", err)
	}

	// Add beads to work_beads with group assignments
	if err := addBeadGroupsToWork(ctx, proj, workID, beadGroups); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to add beads to work: %w", err)
	}

	fmt.Printf("\nCreated work: %s\n", workID)
	fmt.Printf("Directory: %s\n", workDir)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("Branch: %s\n", branchName)
	fmt.Printf("Base Branch: %s\n", baseBranch)

	// Display beads
	fmt.Printf("\nBeads (%d):\n", countBeadsInGroups(beadGroups))
	for i, group := range beadGroups {
		if len(group.beads) > 1 {
			fmt.Printf("  Group %d:\n", i+1)
			for _, b := range group.beads {
				fmt.Printf("    - %s: %s\n", b.ID, b.Title)
			}
		} else if len(group.beads) == 1 {
			fmt.Printf("  - %s: %s\n", group.beads[0].ID, group.beads[0].Title)
		}
	}

	// If --auto, run the full automated workflow
	if flagAutoRun {
		fmt.Println("\nRunning automated workflow...")
		return runAutomatedWorkflowForWork(proj, workID, worktreePath)
	}

	// Spawn the orchestrator for this work
	fmt.Println("\nSpawning orchestrator...")
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, worktreePath); err != nil {
		fmt.Printf("Warning: failed to spawn orchestrator: %v\n", err)
		fmt.Println("You can start it manually with: co run")
	} else {
		fmt.Println("Orchestrator is running in zellij tab.")
	}

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  cd %s\n", workID)
	fmt.Printf("  co run               # Execute tasks\n")

	return nil
}

// beadGroup represents a group of beads that should be in the same task.
type beadGroup struct {
	beads []*beads.Bead
}

// parseBeadGroups parses positional args into bead groups.
// Each arg is a comma-separated list of bead IDs (same group).
// Epics are expanded to their child beads.
// Returns an error if duplicate bead IDs are found across groups.
func parseBeadGroups(args []string, mainRepoPath string) ([]beadGroup, error) {
	var groups []beadGroup
	seenBeads := make(map[string]bool)

	for _, arg := range args {
		// Split comma-separated bead IDs
		beadIDs := parseBeadIDs(arg)
		if len(beadIDs) == 0 {
			continue
		}

		var groupBeads []*beads.Bead
		for _, beadID := range beadIDs {
			// Expand this bead (handles epics and transitive deps)
			expanded, err := collectBeadsForAutomatedWorkflow(beadID, mainRepoPath)
			if err != nil {
				return nil, fmt.Errorf("failed to expand bead %s: %w", beadID, err)
			}
			for _, b := range expanded {
				// Check for duplicates
				if seenBeads[b.ID] {
					return nil, fmt.Errorf("duplicate bead %s specified", b.ID)
				}
				seenBeads[b.ID] = true

				// Convert BeadWithDeps to Bead
				bead := &beads.Bead{
					ID:          b.ID,
					Title:       b.Title,
					Description: b.Description,
				}
				groupBeads = append(groupBeads, bead)
			}
		}

		if len(groupBeads) > 0 {
			groups = append(groups, beadGroup{beads: groupBeads})
		}
	}

	return groups, nil
}

// promptForBranchName prompts the user to accept or customize the branch name.
func promptForBranchName(proposed string) (string, error) {
	fmt.Printf("\nProposed branch name: %s\n", proposed)
	fmt.Print("Accept? [Y/n/custom]: ")

	var response string
	fmt.Scanln(&response)
	response = strings.TrimSpace(response)

	if response == "" || strings.ToLower(response) == "y" {
		return proposed, nil
	}

	if strings.ToLower(response) == "n" {
		fmt.Print("Enter branch name: ")
		fmt.Scanln(&response)
		response = strings.TrimSpace(response)
		if response == "" {
			return "", fmt.Errorf("branch name cannot be empty")
		}
		return response, nil
	}

	// User entered a custom branch name directly
	return response, nil
}

// addBeadGroupsToWork adds bead groups to work_beads table.
func addBeadGroupsToWork(ctx context.Context, proj *project.Project, workID string, groups []beadGroup) error {
	for _, group := range groups {
		var groupID int64
		if len(group.beads) > 1 {
			// Get next group ID for grouped beads
			var err error
			groupID, err = proj.DB.GetNextBeadGroupID(ctx, workID)
			if err != nil {
				return fmt.Errorf("failed to get next group ID: %w", err)
			}
		}
		// groupID = 0 for ungrouped beads (single bead in group)

		// Extract bead IDs
		var beadIDs []string
		for _, b := range group.beads {
			beadIDs = append(beadIDs, b.ID)
		}

		// Add beads to work
		if err := proj.DB.AddWorkBeads(ctx, workID, beadIDs, groupID); err != nil {
			return fmt.Errorf("failed to add beads: %w", err)
		}
	}
	return nil
}

// countBeadsInGroups counts total beads across all groups.
func countBeadsInGroups(groups []beadGroup) int {
	count := 0
	for _, g := range groups {
		count += len(g.beads)
	}
	return count
}

// runAutomatedWorkflowForWork runs the full automated workflow for an existing work.
// This includes: create tasks -> execute -> review/fix loop -> PR
// Delegates to runFullAutomatedWorkflow in run.go for the actual implementation.
func runAutomatedWorkflowForWork(proj *project.Project, workID, worktreePath string) error {
	return runFullAutomatedWorkflow(proj, workID, worktreePath)
}

// runWorkAdd adds beads to an existing work.
func runWorkAdd(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	// Get work ID
	workID := flagAddWork
	if workID == "" {
		workID, err = getCurrentWork(proj)
		if err != nil {
			return fmt.Errorf("not in a work directory and no --work specified")
		}
	}

	// Verify work exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	mainRepoPath := proj.MainRepoPath()

	// Parse bead groups
	beadGroups, err := parseBeadGroups(args, mainRepoPath)
	if err != nil {
		return err
	}

	if len(beadGroups) == 0 {
		return fmt.Errorf("no beads specified")
	}

	// Check if any bead is already in a task
	for _, group := range beadGroups {
		for _, bead := range group.beads {
			inTask, err := proj.DB.IsBeadInTask(ctx, workID, bead.ID)
			if err != nil {
				return fmt.Errorf("failed to check bead %s: %w", bead.ID, err)
			}
			if inTask {
				return fmt.Errorf("bead %s is already assigned to a task", bead.ID)
			}
		}
	}

	// Add beads to work
	if err := addBeadGroupsToWork(ctx, proj, workID, beadGroups); err != nil {
		return fmt.Errorf("failed to add beads: %w", err)
	}

	fmt.Printf("Added %d bead(s) to work %s\n", countBeadsInGroups(beadGroups), workID)
	return nil
}

// runWorkRemove removes beads from an existing work.
func runWorkRemove(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	// Get work ID
	workID := flagRemoveWork
	if workID == "" {
		workID, err = getCurrentWork(proj)
		if err != nil {
			return fmt.Errorf("not in a work directory and no --work specified")
		}
	}

	// Verify work exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Remove each bead
	removed := 0
	for _, beadID := range args {
		beadID = strings.TrimSpace(beadID)
		if beadID == "" {
			continue
		}

		// Check if bead is in a task
		inTask, err := proj.DB.IsBeadInTask(ctx, workID, beadID)
		if err != nil {
			return fmt.Errorf("failed to check bead %s: %w", beadID, err)
		}
		if inTask {
			return fmt.Errorf("bead %s is assigned to a task and cannot be removed", beadID)
		}

		// Remove the bead
		if err := proj.DB.RemoveWorkBead(ctx, workID, beadID); err != nil {
			fmt.Printf("Warning: failed to remove bead %s: %v\n", beadID, err)
			continue
		}
		removed++
	}

	fmt.Printf("Removed %d bead(s) from work %s\n", removed, workID)
	return nil
}

func runWorkList(cmd *cobra.Command, args []string) error {
	// Find project
	ctx := GetContext()
	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	// List all works
	works, err := proj.DB.ListWorks(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to list works: %w", err)
	}

	if len(works) == 0 {
		fmt.Println("No work units found.")
		return nil
	}

	// Display works
	fmt.Printf("%-10s %-12s %-20s %s\n", "ID", "Status", "Branch", "PR URL")
	fmt.Printf("%-10s %-12s %-20s %s\n", strings.Repeat("-", 10), strings.Repeat("-", 12), strings.Repeat("-", 20), strings.Repeat("-", 30))

	for _, work := range works {
		prURL := work.PRURL
		if prURL == "" {
			prURL = "-"
		}
		fmt.Printf("%-10s %-12s %-20s %s\n", work.ID, work.Status, work.BranchName, prURL)
	}

	// Show summary
	statusCounts := make(map[string]int)
	for _, work := range works {
		statusCounts[work.Status]++
	}

	fmt.Printf("\nTotal: %d work(s)", len(works))
	if len(statusCounts) > 0 {
		fmt.Print(" (")
		first := true
		for status, count := range statusCounts {
			if !first {
				fmt.Print(", ")
			}
			fmt.Printf("%d %s", count, status)
			first = false
		}
		fmt.Print(")")
	}
	fmt.Println()

	return nil
}

func runWorkShow(cmd *cobra.Command, args []string) error {
	// Find project
	ctx := GetContext()
	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	var workID string
	if len(args) > 0 {
		workID = args[0]
	} else {
		// Try to detect work from current directory
		workID, err = getCurrentWork(proj)
		if err != nil {
			return fmt.Errorf("not in a work directory and no work ID specified")
		}
	}

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Display work details
	fmt.Printf("Work: %s\n", work.ID)
	fmt.Printf("Status: %s\n", work.Status)
	fmt.Printf("Branch: %s\n", work.BranchName)
	fmt.Printf("Base Branch: %s\n", work.BaseBranch)
	fmt.Printf("Worktree: %s\n", work.WorktreePath)

	if work.PRURL != "" {
		fmt.Printf("PR URL: %s\n", work.PRURL)
	}

	if work.ErrorMessage != "" {
		fmt.Printf("Error: %s\n", work.ErrorMessage)
	}

	if work.ZellijSession != "" {
		fmt.Printf("Zellij Session: %s\n", work.ZellijSession)
		if work.ZellijTab != "" {
			fmt.Printf("Zellij Tab: %s\n", work.ZellijTab)
		}
	}

	fmt.Printf("Created: %s\n", work.CreatedAt.Format("2006-01-02 15:04:05"))

	if work.StartedAt != nil {
		fmt.Printf("Started: %s\n", work.StartedAt.Format("2006-01-02 15:04:05"))
	}

	if work.CompletedAt != nil {
		fmt.Printf("Completed: %s\n", work.CompletedAt.Format("2006-01-02 15:04:05"))
	}

	// Get tasks for this work
	tasks, err := proj.DB.GetWorkTasks(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work tasks: %w", err)
	}

	if len(tasks) > 0 {
		fmt.Printf("\nTasks (%d):\n", len(tasks))
		for i, task := range tasks {
			fmt.Printf("  %d. %s [%s]\n", i+1, task.ID, task.Status)
		}
	} else {
		fmt.Println("\nNo tasks planned for this work yet.")
	}

	return nil
}

func runWorkDestroy(cmd *cobra.Command, args []string) error {
	workID := args[0]

	// Find project
	ctx := GetContext()
	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	// Get work to verify it exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Check if work has uncompleted tasks
	tasks, err := proj.DB.GetWorkTasks(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work tasks: %w", err)
	}

	activeTaskCount := 0
	for _, task := range tasks {
		if task.Status != db.StatusCompleted && task.Status != db.StatusFailed {
			activeTaskCount++
		}
	}

	if activeTaskCount > 0 {
		fmt.Printf("Warning: Work %s has %d active task(s). Are you sure you want to destroy it? (y/N): ", workID, activeTaskCount)
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Destruction cancelled.")
			return nil
		}
	}

	// Terminate any running zellij tabs (orchestrator and task tabs) for this work
	if err := claude.TerminateWorkTabs(ctx, workID, proj.Config.Project.Name); err != nil {
		fmt.Printf("Warning: failed to terminate zellij tabs: %v\n", err)
		// Continue with destruction even if tab termination fails
	}

	// Remove git worktree if it exists
	if work.WorktreePath != "" {
		if err := worktree.RemoveForce(proj.MainRepoPath(), work.WorktreePath); err != nil {
			// Warn but continue - worktree might not exist
			fmt.Printf("Warning: failed to remove worktree: %v\n", err)
		}
	}

	// Remove work directory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.RemoveAll(workDir); err != nil {
		// Warn but continue - directory might not exist
		fmt.Printf("Warning: failed to remove directory: %v\n", err)
	}

	// Delete work from database (also deletes associated tasks and relationships)
	if err := proj.DB.DeleteWork(ctx, workID); err != nil {
		return fmt.Errorf("failed to delete work from database: %w", err)
	}

	fmt.Printf("Destroyed work: %s\n", workID)
	return nil
}

func runWorkPR(cmd *cobra.Command, args []string) error {
	// Find project
	ctx := GetContext()
	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	var workID string
	if len(args) > 0 {
		workID = args[0]
	} else {
		// Try to detect work from current directory
		workID, err = getCurrentWork(proj)
		if err != nil {
			return fmt.Errorf("not in a work directory and no work ID specified")
		}
	}

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Check if work is completed
	if work.Status != db.StatusCompleted {
		return fmt.Errorf("work %s is not completed (status: %s)", workID, work.Status)
	}

	// Check if PR already exists
	if work.PRURL != "" {
		fmt.Printf("PR already exists for work %s: %s\n", workID, work.PRURL)
		return nil
	}

	// Generate task ID for PR creation
	// Use a special ".pr" suffix for PR tasks
	prTaskID := fmt.Sprintf("%s.pr", workID)

	// Create a PR creation task
	err = proj.DB.CreateTask(ctx, prTaskID, "pr", []string{}, 0, workID)
	if err != nil {
		return fmt.Errorf("failed to create PR task: %w", err)
	}

	fmt.Printf("Created PR task: %s\n", prTaskID)

	// Auto-run the PR task
	fmt.Printf("Running PR task...\n")
	return processTask(proj, prTaskID)
}

func runWorkReview(cmd *cobra.Command, args []string) error {
	const maxReviewIterations = 3

	// Find project
	ctx := GetContext()
	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	var workID string
	if len(args) > 0 {
		workID = args[0]
	} else {
		// Try to detect work from current directory
		workID, err = getCurrentWork(proj)
		if err != nil {
			return fmt.Errorf("not in a work directory and no work ID specified")
		}
	}

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Check if work has changes to review
	// Work should have at least been started (has commits on the branch)
	if work.Status == db.StatusPending {
		return fmt.Errorf("work %s has not been started yet (status: pending)", workID)
	}

	mainRepoPath := proj.MainRepoPath()

	// Run review-fix loop if --auto is set
	for iteration := 0; ; iteration++ {
		// Check max iterations
		if flagReviewAuto && iteration >= maxReviewIterations {
			fmt.Printf("Warning: Maximum review iterations (%d) reached\n", maxReviewIterations)
			break
		}

		// Generate unique task ID for review
		tasks, err := proj.DB.GetWorkTasks(ctx, workID)
		if err != nil {
			return fmt.Errorf("failed to get work tasks: %w", err)
		}

		// Count existing review tasks to generate unique ID
		reviewCount := 0
		reviewPrefix := fmt.Sprintf("%s.review", workID)
		for _, task := range tasks {
			if strings.HasPrefix(task.ID, reviewPrefix) {
				reviewCount++
			}
		}

		// Generate unique review task ID (e.g., w-xxx.review-1, w-xxx.review-2)
		reviewTaskID := fmt.Sprintf("%s.review-%d", workID, reviewCount+1)

		// Create a review task
		err = proj.DB.CreateTask(ctx, reviewTaskID, "review", []string{}, 0, workID)
		if err != nil {
			return fmt.Errorf("failed to create review task: %w", err)
		}

		fmt.Printf("Created review task: %s\n", reviewTaskID)

		// Run the review task
		fmt.Printf("Running review task...\n")
		if err := processTask(proj, reviewTaskID); err != nil {
			return fmt.Errorf("review task failed: %w", err)
		}

		// If not in auto mode, we're done after one review
		if !flagReviewAuto {
			break
		}

		// Check if the review created any issues via review_epic_id
		epicID, err := proj.DB.GetReviewEpicID(ctx, reviewTaskID)
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
			break
		}

		fmt.Printf("Review found %d issue(s) - creating fix tasks...\n", len(beadsToFix))

		// Create and run fix tasks for each bead
		for _, b := range beadsToFix {
			nextNum, err := proj.DB.GetNextTaskNumber(ctx, workID)
			if err != nil {
				return fmt.Errorf("failed to get next task number: %w", err)
			}
			taskID := fmt.Sprintf("%s.%d", workID, nextNum)

			if err := proj.DB.CreateTask(ctx, taskID, "implement", []string{b.ID}, 0, workID); err != nil {
				return fmt.Errorf("failed to create fix task: %w", err)
			}

			fmt.Printf("Created fix task %s for bead %s: %s\n", taskID, b.ID, b.Title)

			// Run the fix task
			if err := processTask(proj, taskID); err != nil {
				return fmt.Errorf("fix task %s failed: %w", taskID, err)
			}
		}

		// Loop back for another review
	}

	return nil
}

// detectWorkFromDirectory tries to detect the work from the current directory.
// Returns empty string if not in a work directory.
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

// getCurrentWork tries to detect the work context from the current directory.
func getCurrentWork(proj *project.Project) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Check if we're in a subdirectory of the project
	if !strings.HasPrefix(cwd, proj.Root) {
		return "", fmt.Errorf("not in project directory")
	}

	// Get relative path from project root
	relPath, err := filepath.Rel(proj.Root, cwd)
	if err != nil {
		return "", err
	}

	// Check if we're in a work directory (w-xxx or w-xxx/tree/...)
	parts := strings.Split(relPath, string(os.PathSeparator))
	if len(parts) > 0 && strings.HasPrefix(parts[0], "w-") {
		return parts[0], nil
	}

	return "", fmt.Errorf("not in a work directory")
}
