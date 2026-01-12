package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/project"
	cosignal "github.com/newhook/co/internal/signal"
	"github.com/spf13/cobra"
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Manage work units",
	Long:  `Manage work units that group tasks together. Each work has its own git worktree and feature branch.`,
}

var workCreateCmd = &cobra.Command{
	Use:   "create [<branch>]",
	Short: "Create a new work unit with the specified branch",
	Long: `Create a new work unit with the specified branch name.
Creates a subdirectory with a git worktree for isolated development.

The branch argument specifies the git branch name to create.
A unique work ID will be auto-generated using content-based hashing (w-abc format).

With --bead flag, runs an automated end-to-end workflow:
1. Auto-generates branch name from bead title
2. Collects transitive dependencies (if any)
3. Plans tasks with auto-grouping
4. Executes all tasks
5. Runs review-fix loop until clean
6. Creates a pull request`,
	Args: cobra.MaximumNArgs(1),
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

var (
	flagBaseBranch string
	flagBeadID     string
)

func init() {
	workCreateCmd.Flags().StringVar(&flagBaseBranch, "base", "main", "base branch to create feature branch from (also used as PR target)")
	workCreateCmd.Flags().StringVar(&flagBeadID, "bead", "", "bead ID to run automated end-to-end workflow (plan, run, review-fix, PR)")
	workCmd.AddCommand(workCreateCmd)
	workCmd.AddCommand(workListCmd)
	workCmd.AddCommand(workShowCmd)
	workCmd.AddCommand(workDestroyCmd)
	workCmd.AddCommand(workPRCmd)
	workCmd.AddCommand(workReviewCmd)
}

func runWorkCreate(cmd *cobra.Command, args []string) error {
	baseBranch := flagBaseBranch

	// Find project
	proj, err := project.Find("")
	if err != nil {
		return err
	}
	defer proj.Close()

	// Check if --bead flag is used for automated workflow
	if flagBeadID != "" {
		return runAutomatedWorkflow(proj, flagBeadID, baseBranch)
	}

	// Traditional mode: require branch name argument
	if len(args) == 0 {
		return fmt.Errorf("branch name is required (use --bead for automated workflow)")
	}
	branchName := args[0]

	// Generate content-based hash ID from branch name
	workID, err := proj.DB.GenerateWorkID(GetContext(), branchName, proj.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("failed to generate work ID: %w", err)
	}
	fmt.Printf("Generated work ID: %s (from branch: %s)\n", workID, branchName)

	// Block signals during critical worktree creation to avoid leaving inconsistent state
	cosignal.BlockSignals()
	defer cosignal.UnblockSignals()

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Create git worktree inside work directory
	worktreePath := filepath.Join(workDir, "tree")

	// Create worktree with new branch based on the specified base branch
	cmd1 := exec.Command("git", "worktree", "add", worktreePath, "-b", branchName, baseBranch)
	cmd1.Dir = proj.MainRepoPath()
	if output, err := cmd1.CombinedOutput(); err != nil {
		// Clean up on failure
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create worktree: %w\n%s", err, output)
	}

	// Push branch and set upstream to avoid "no upstream branch" errors later
	cmd2 := exec.Command("git", "push", "--set-upstream", "origin", branchName)
	cmd2.Dir = worktreePath
	if output, err := cmd2.CombinedOutput(); err != nil {
		// Clean up on failure
		exec.Command("git", "worktree", "remove", worktreePath).Run()
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to push and set upstream: %w\n%s", err, output)
	}

	// Initialize mise in worktree if needed
	if err := mise.Initialize(worktreePath); err != nil {
		fmt.Printf("Warning: mise initialization failed: %v\n", err)
	}

	// Create work record in database
	if err := proj.DB.CreateWork(GetContext(), workID, worktreePath, branchName, baseBranch); err != nil {
		// Clean up on failure
		exec.Command("git", "worktree", "remove", worktreePath).Run()
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	// Note: We don't create a zellij tab here - tasks create their own tabs when they run.
	// This avoids creating an unused tab that would sit empty until tasks are executed.

	fmt.Printf("Created work: %s\n", workID)
	fmt.Printf("Directory: %s\n", workDir)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("Branch: %s\n", branchName)
	fmt.Printf("Base Branch: %s\n", baseBranch)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  cd %s\n", workID)
	fmt.Printf("  co plan              # Plan tasks for this work\n")
	fmt.Printf("  co run               # Execute tasks\n")

	return nil
}

func runWorkList(cmd *cobra.Command, args []string) error {
	// Find project
	proj, err := project.Find("")
	if err != nil {
		return err
	}
	defer proj.Close()

	// List all works
	works, err := proj.DB.ListWorks(GetContext(), "")
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
	proj, err := project.Find("")
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
	work, err := proj.DB.GetWork(GetContext(), workID)
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
	tasks, err := proj.DB.GetWorkTasks(GetContext(), workID)
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
	proj, err := project.Find("")
	if err != nil {
		return err
	}
	defer proj.Close()

	// Get work to verify it exists
	work, err := proj.DB.GetWork(GetContext(), workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Check if work has uncompleted tasks
	tasks, err := proj.DB.GetWorkTasks(GetContext(), workID)
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

	// Remove git worktree if it exists
	if work.WorktreePath != "" {
		cmd := exec.Command("git", "worktree", "remove", "--force", work.WorktreePath)
		cmd.Dir = proj.MainRepoPath()
		if output, err := cmd.CombinedOutput(); err != nil {
			// Warn but continue - worktree might not exist
			fmt.Printf("Warning: failed to remove worktree: %v\n%s", err, output)
		}
	}

	// Remove work directory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.RemoveAll(workDir); err != nil {
		// Warn but continue - directory might not exist
		fmt.Printf("Warning: failed to remove directory: %v\n", err)
	}

	// Delete work from database (also deletes associated tasks and relationships)
	if err := proj.DB.DeleteWork(GetContext(), workID); err != nil {
		return fmt.Errorf("failed to delete work from database: %w", err)
	}

	fmt.Printf("Destroyed work: %s\n", workID)
	return nil
}

func runWorkPR(cmd *cobra.Command, args []string) error {
	// Find project
	proj, err := project.Find("")
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
	work, err := proj.DB.GetWork(GetContext(), workID)
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
	err = proj.DB.CreateTask(GetContext(), prTaskID, "pr", []string{}, 0, workID)
	if err != nil {
		return fmt.Errorf("failed to create PR task: %w", err)
	}

	fmt.Printf("Created PR task: %s\n", prTaskID)

	// Auto-run the PR task
	fmt.Printf("Running PR task...\n")
	return processTask(proj, prTaskID)
}

func runWorkReview(cmd *cobra.Command, args []string) error {
	// Find project
	proj, err := project.Find("")
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
	work, err := proj.DB.GetWork(GetContext(), workID)
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

	// Generate unique task ID for review
	// Find the next available review task number
	tasks, err := proj.DB.GetWorkTasks(GetContext(), workID)
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
	err = proj.DB.CreateTask(GetContext(), reviewTaskID, "review", []string{}, 0, workID)
	if err != nil {
		return fmt.Errorf("failed to create review task: %w", err)
	}

	fmt.Printf("Created review task: %s\n", reviewTaskID)

	// Auto-run the review task
	fmt.Printf("Running review task...\n")
	return processTask(proj, reviewTaskID)
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

	// Check if we're in a work directory (work-N or work-N/tree/...)
	parts := strings.Split(relPath, string(os.PathSeparator))
	if len(parts) > 0 && strings.HasPrefix(parts[0], "work-") {
		return parts[0], nil
	}

	// Look for a work that has this path as its worktree
	work, err := proj.DB.GetWorkByDirectory(GetContext(), cwd+"%")
	if err != nil {
		return "", err
	}
	if work != nil {
		return work.ID, nil
	}

	return "", fmt.Errorf("not in a work directory")
}