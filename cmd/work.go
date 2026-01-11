package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var (
	workID string
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Manage work units",
	Long:  `Manage work units that group tasks together. Each work has its own git worktree and feature branch.`,
}

var workCreateCmd = &cobra.Command{
	Use:   "create <branch>",
	Short: "Create a new work unit with the specified branch",
	Long: `Create a new work unit with the specified branch name.
Creates a subdirectory with a git worktree for isolated development.

The branch argument is required and specifies the git branch name to create.
If no --id is provided, an ID will be auto-generated (w-abc format, similar to bead IDs).`,
	Args: cobra.ExactArgs(1),
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

func init() {
	workCreateCmd.Flags().StringVar(&workID, "id", "", "Custom work ID (defaults to auto-generated w-XXX)")

	workCmd.AddCommand(workCreateCmd)
	workCmd.AddCommand(workListCmd)
	workCmd.AddCommand(workShowCmd)
	workCmd.AddCommand(workDestroyCmd)
}

func runWorkCreate(cmd *cobra.Command, args []string) error {
	// Get branch name from args
	branchName := args[0]

	// Find project
	proj, err := project.Find("")
	if err != nil {
		return err
	}
	defer proj.Close()

	// Open database
	database, err := proj.OpenDB()
	if err != nil {
		return err
	}

	// Use custom work ID or generate one
	if workID == "" {
		// Auto-generate work ID (work-1, work-2, etc.)
		generatedID, err := database.GenerateNextWorkID(context.Background())
		if err != nil {
			return fmt.Errorf("failed to generate work ID: %w", err)
		}
		workID = generatedID
	} else {
		// Check if custom work ID already exists
		existingWork, err := database.GetWork(context.Background(), workID)
		if err != nil {
			return fmt.Errorf("failed to check for existing work: %w", err)
		}
		if existingWork != nil {
			return fmt.Errorf("work with ID %s already exists", workID)
		}
	}

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Create git worktree inside work directory
	worktreePath := filepath.Join(workDir, "tree")

	// Create worktree with new branch
	cmd1 := exec.Command("git", "worktree", "add", worktreePath, "-b", branchName)
	cmd1.Dir = proj.MainRepoPath()
	if output, err := cmd1.CombinedOutput(); err != nil {
		// Clean up on failure
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create worktree: %w\n%s", err, output)
	}

	// Initialize mise in worktree if needed
	if err := mise.Initialize(worktreePath); err != nil {
		fmt.Printf("Warning: mise initialization failed: %v\n", err)
	}

	// Create work record in database
	if err := database.CreateWork(context.Background(), workID, worktreePath, branchName); err != nil {
		// Clean up on failure
		exec.Command("git", "worktree", "remove", worktreePath).Run()
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	// Create zellij tab for this work
	sessionName := claude.SessionNameForProject(proj.Config.Project.Name)
	tabName := workID // Use work ID as tab name

	// Ensure zellij session exists
	if err := ensureZellijSession(context.Background(), sessionName); err != nil {
		fmt.Printf("Warning: failed to ensure zellij session: %v\n", err)
	} else {
		// Create tab for this work
		tabArgs := []string{
			"-s", sessionName,
			"action", "new-tab",
			"--cwd", worktreePath,
			"--name", tabName,
		}
		tabCmd := exec.Command("zellij", tabArgs...)
		if err := tabCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to create zellij tab for work: %v\n", err)
		} else {
			fmt.Printf("Created zellij tab: %s\n", tabName)

			// Update work with session info
			if err := database.StartWork(context.Background(), workID, sessionName, tabName); err != nil {
				fmt.Printf("Warning: failed to update work with zellij info: %v\n", err)
			}
		}
	}

	fmt.Printf("Created work: %s\n", workID)
	fmt.Printf("Directory: %s\n", workDir)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("Branch: %s\n", branchName)
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

	// Open database
	database, err := proj.OpenDB()
	if err != nil {
		return err
	}

	// List all works
	works, err := database.ListWorks(context.Background(), "")
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

	// Open database
	database, err := proj.OpenDB()
	if err != nil {
		return err
	}

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
	work, err := database.GetWork(context.Background(), workID)
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
	tasks, err := database.GetWorkTasks(context.Background(), workID)
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

	// Open database
	database, err := proj.OpenDB()
	if err != nil {
		return err
	}

	// Get work to verify it exists
	work, err := database.GetWork(context.Background(), workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Check if work has uncompleted tasks
	tasks, err := database.GetWorkTasks(context.Background(), workID)
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

	// TODO: Remove work from database
	// This requires adding a DeleteWork method to the database
	// For now, we'll just mark it as failed
	if err := database.FailWork(context.Background(), workID, "Work destroyed by user"); err != nil {
		return fmt.Errorf("failed to update work status: %w", err)
	}

	fmt.Printf("Destroyed work: %s\n", workID)
	return nil
}

// ensureZellijSession ensures a zellij session exists with the given name.
func ensureZellijSession(ctx context.Context, sessionName string) error {
	// Check if session exists
	listCmd := exec.CommandContext(ctx, "zellij", "list-sessions")
	output, err := listCmd.Output()
	if err != nil {
		// No sessions, create one
		return createZellijSession(ctx, sessionName)
	}

	// Check if requested session exists
	if strings.Contains(string(output), sessionName) {
		return nil
	}

	return createZellijSession(ctx, sessionName)
}

// createZellijSession creates a new zellij session.
func createZellijSession(ctx context.Context, sessionName string) error {
	// Start session detached
	cmd := exec.CommandContext(ctx, "zellij", "-s", sessionName)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}
	// Give it time to start
	time.Sleep(1 * time.Second)
	return nil
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

	// Try to find work by worktree path
	database, err := proj.OpenDB()
	if err != nil {
		return "", err
	}

	// Look for a work that has this path as its worktree
	work, err := database.GetWorkByDirectory(context.Background(), cwd + "%")
	if err != nil {
		return "", err
	}
	if work != nil {
		return work.ID, nil
	}

	return "", fmt.Errorf("not in a work directory")
}