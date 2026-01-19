package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/names"
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
	Use:   "create <bead-id>",
	Short: "Create a new work unit from a bead",
	Long: `Create a new work unit from a single bead.
Creates a subdirectory with a git worktree for isolated development.

If the bead is an epic, all child beads are automatically included.
Transitive dependencies are also included.

Branch name is auto-generated from the bead title - you'll be prompted to accept or customize.

With --auto flag, runs the full automated workflow:
1. Creates tasks from beads
2. Executes all tasks
3. Runs review-fix loop until clean
4. Creates a pull request`,
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
	Use:   "add <bead-ids...>",
	Short: "Add beads to work",
	Long: `Add beads to an existing work unit.

Multiple beads can be specified separated by spaces or commas.
Epics are automatically expanded to include all child beads.

Use --plan when running to let the LLM group beads intelligently,
or --auto for a fully automated workflow.`,
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

var workConsoleCmd = &cobra.Command{
	Use:   "console [<id>]",
	Short: "Open a console tab in the work's worktree",
	Long: `Open a zellij tab with a shell in the work's worktree.
If no ID is provided, uses the work for the current directory context.

This is useful for running tests, exploring the codebase, or debugging
while the orchestrator runs in a separate tab.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkConsole,
}

var workClaudeCmd = &cobra.Command{
	Use:   "claude [<id>]",
	Short: "Open a Claude Code session in the work's worktree",
	Long: `Open a zellij tab with an interactive Claude Code session in the work's worktree.
If no ID is provided, uses the work for the current directory context.

This is useful for manual exploration, debugging, or ad-hoc changes
while the orchestrator runs in a separate tab.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkClaude,
}

var (
	flagBaseBranch  string
	flagAutoRun     bool
	flagReviewAuto  bool
	flagAddWork     string
	flagRemoveWork  string
	flagBranchName  string
	flagYes         bool
)

func init() {
	workCreateCmd.Flags().StringVar(&flagBaseBranch, "base", "main", "base branch to create feature branch from (also used as PR target)")
	workCreateCmd.Flags().BoolVar(&flagAutoRun, "auto", false, "run full automated workflow (implement, review, fix, PR)")
	workCreateCmd.Flags().StringVar(&flagBranchName, "branch", "", "branch name to use (skip prompt)")
	workCreateCmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "skip confirmation prompts")
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
	workCmd.AddCommand(workConsoleCmd)
	workCmd.AddCommand(workClaudeCmd)
	workCmd.AddCommand(workFeedbackCmd)
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
	beadID := args[0]

	// Expand the bead (handles epics and transitive deps)
	expandedIssueIDs, err := collectIssueIDsForAutomatedWorkflow(ctx, beadID, proj.Beads)
	if err != nil {
		return fmt.Errorf("failed to expand bead %s: %w", beadID, err)
	}

	if len(expandedIssueIDs) == 0 {
		return fmt.Errorf("no beads found for %s", beadID)
	}

	// Get issue details for branch name generation
	issuesResult, err := proj.Beads.GetBeadsWithDeps(ctx, expandedIssueIDs)
	if err != nil {
		return fmt.Errorf("failed to get issue details: %w", err)
	}

	// Convert to slice of issue pointers for branch name generation
	var groupIssues []*beads.Bead
	for _, issueID := range expandedIssueIDs {
		if issue, ok := issuesResult.Beads[issueID]; ok {
			issueCopy := issue
			groupIssues = append(groupIssues, &issueCopy)
		}
	}

	// Determine branch name
	var branchName string
	if flagBranchName != "" {
		// Use provided branch name
		branchName = flagBranchName
	} else {
		// Generate branch name from issue titles
		branchName = generateBranchNameFromIssues(groupIssues)
		branchName, err = ensureUniqueBranchName(mainRepoPath, branchName)
		if err != nil {
			return fmt.Errorf("failed to find unique branch name: %w", err)
		}

		// Prompt user unless -y flag is set
		if !flagYes {
			branchName, err = promptForBranchName(branchName)
			if err != nil {
				return err
			}
		}
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

	// Get a human-readable name for this worker
	workerName, err := names.GetNextAvailableName(ctx, proj.DB.DB)
	if err != nil {
		fmt.Printf("Warning: failed to get worker name: %v\n", err)
	}

	// Create work record in database with the root issue ID (the original bead that was expanded)
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch, beadID); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	// Add beads to work_beads
	if err := addBeadsToWork(ctx, proj, workID, expandedIssueIDs); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to add beads to work: %w", err)
	}

	fmt.Printf("\nCreated work: %s\n", workID)
	if workerName != "" {
		fmt.Printf("Worker: %s\n", workerName)
	}
	fmt.Printf("Directory: %s\n", workDir)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("Branch: %s\n", branchName)
	fmt.Printf("Base Branch: %s\n", baseBranch)

	// Display beads
	fmt.Printf("\nBeads (%d):\n", len(groupIssues))
	for _, issue := range groupIssues {
		fmt.Printf("  - %s: %s\n", issue.ID, issue.Title)
	}

	// If --auto, run the full automated workflow
	if flagAutoRun {
		fmt.Println("\nRunning automated workflow...")
		return runAutomatedWorkflowForWork(proj, workID, worktreePath, os.Stdout)
	}

	// Spawn the orchestrator for this work
	fmt.Println("\nSpawning orchestrator...")
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, worktreePath, workerName, os.Stdout); err != nil {
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

// parseBeadArgs parses positional args into bead IDs.
// Both commas and spaces are treated as separators.
// Epics are expanded to their child beads.
// Returns an error if duplicate bead IDs are found.
func parseBeadArgs(ctx context.Context, args []string, beadsClient *beads.Client) ([]string, error) {
	seenBeads := make(map[string]bool)
	var allIssueIDs []string

	for _, arg := range args {
		// Split comma-separated bead IDs (commas and spaces both work as separators)
		beadIDs := parseBeadIDs(arg)
		if len(beadIDs) == 0 {
			continue
		}

		for _, beadID := range beadIDs {
			// Expand this bead (handles epics and transitive deps)
			expandedIDs, err := collectIssueIDsForAutomatedWorkflow(ctx, beadID, beadsClient)
			if err != nil {
				return nil, fmt.Errorf("failed to expand bead %s: %w", beadID, err)
			}
			for _, issueID := range expandedIDs {
				// Check for duplicates
				if seenBeads[issueID] {
					return nil, fmt.Errorf("duplicate bead %s specified", issueID)
				}
				seenBeads[issueID] = true
				allIssueIDs = append(allIssueIDs, issueID)
			}
		}
	}

	return allIssueIDs, nil
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

// addBeadsToWork adds beads to work_beads table.
func addBeadsToWork(ctx context.Context, proj *project.Project, workID string, beadIDs []string) error {
	if len(beadIDs) == 0 {
		return nil
	}
	if err := proj.DB.AddWorkBeads(ctx, workID, beadIDs); err != nil {
		return fmt.Errorf("failed to add beads: %w", err)
	}
	return nil
}

// AddBeadsToWorkResult contains the result of adding beads to a work.
type AddBeadsToWorkResult struct {
	BeadsAdded int
}

// AddBeadsToWork adds beads to an existing work.
// This is the core logic for adding beads that can be called from both the CLI and TUI.
// Each bead is added as its own group (no grouping).
func AddBeadsToWork(ctx context.Context, proj *project.Project, workID string, beadIDs []string) (*AddBeadsToWorkResult, error) {
	if len(beadIDs) == 0 {
		return nil, fmt.Errorf("no beads specified")
	}

	// Verify work exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	// Check if any bead is already in a task
	for _, beadID := range beadIDs {
		inTask, err := proj.DB.IsBeadInTask(ctx, workID, beadID)
		if err != nil {
			return nil, fmt.Errorf("failed to check bead %s: %w", beadID, err)
		}
		if inTask {
			return nil, fmt.Errorf("bead %s is already assigned to a task", beadID)
		}
	}

	// Add beads to work
	if err := proj.DB.AddWorkBeads(ctx, workID, beadIDs); err != nil {
		return nil, fmt.Errorf("failed to add beads: %w", err)
	}

	return &AddBeadsToWorkResult{
		BeadsAdded: len(beadIDs),
	}, nil
}

// WorkCreateResult contains the result of creating a work unit.
type WorkCreateResult struct {
	WorkID       string
	WorkerName   string
	WorkDir      string
	WorktreePath string
	BranchName   string
	BaseBranch   string
	RootIssueID  string
}

// WorkCreateOptions contains options for work creation.
type WorkCreateOptions struct {
	// Silent suppresses progress output (useful for TUI contexts).
	Silent bool
}

// CreateWorkWithBranch creates a new work unit with the given branch name and root issue.
// This is the core work creation logic that can be called from both the CLI and TUI.
// The rootIssueID is required and represents the single root issue this work is associated with.
func CreateWorkWithBranch(ctx context.Context, proj *project.Project, branchName, baseBranch, rootIssueID string, opts ...WorkCreateOptions) (*WorkCreateResult, error) {
	// Apply options
	var opt WorkCreateOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	// Determine output writer based on silent option
	var output io.Writer = os.Stdout
	if opt.Silent {
		output = io.Discard
	}

	if baseBranch == "" {
		baseBranch = "main"
	}

	mainRepoPath := proj.MainRepoPath()

	// Ensure unique branch name
	var err error
	branchName, err = ensureUniqueBranchName(mainRepoPath, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to find unique branch name: %w", err)
	}

	// Generate work ID
	workID, err := proj.DB.GenerateWorkID(ctx, branchName, proj.Config.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate work ID: %w", err)
	}

	// Block signals during critical worktree creation
	cosignal.BlockSignals()
	defer cosignal.UnblockSignals()

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	// Create git worktree inside work directory
	worktreePath := filepath.Join(workDir, "tree")

	// Create worktree with new branch
	if err := worktree.Create(mainRepoPath, worktreePath, branchName, baseBranch); err != nil {
		os.RemoveAll(workDir)
		return nil, err
	}

	// Push branch and set upstream
	if err := git.PushSetUpstreamInDir(branchName, worktreePath); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return nil, err
	}

	// Initialize mise in worktree if needed
	if err := mise.InitializeWithOutput(worktreePath, output); err != nil {
		// Non-fatal warning
		fmt.Fprintf(output, "Warning: mise initialization failed: %v\n", err)
	}

	// Get a human-readable name for this worker
	workerName, err := names.GetNextAvailableName(ctx, proj.DB.DB)
	if err != nil {
		// Non-fatal warning
		fmt.Fprintf(output, "Warning: failed to get worker name: %v\n", err)
	}

	// Create work record in database
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch, rootIssueID); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return nil, fmt.Errorf("failed to create work record: %w", err)
	}

	return &WorkCreateResult{
		WorkID:       workID,
		WorkerName:   workerName,
		WorkDir:      workDir,
		WorktreePath: worktreePath,
		BranchName:   branchName,
		BaseBranch:   baseBranch,
		RootIssueID:  rootIssueID,
	}, nil
}

// DestroyWork destroys a work unit and all its resources.
// This is the core work destruction logic that can be called from both the CLI and TUI.
// It does not perform interactive confirmation - that should be handled by the caller.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func DestroyWork(ctx context.Context, proj *project.Project, workID string, w io.Writer) error {
	// Get work to verify it exists
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Close the root issue if it exists
	if work.RootIssueID != "" {
		fmt.Fprintf(w, "Closing root issue %s...\n", work.RootIssueID)
		if err := beads.Close(ctx, work.RootIssueID, proj.MainRepoPath()); err != nil {
			// Warn but continue - issue might already be closed or deleted
			fmt.Fprintf(w, "Warning: failed to close root issue %s: %v\n", work.RootIssueID, err)
		}
	}

	// Terminate any running zellij tabs (orchestrator and task tabs) for this work
	if err := claude.TerminateWorkTabs(ctx, workID, proj.Config.Project.Name, w); err != nil {
		// Continue with destruction even if tab termination fails
		// Caller can log this warning if needed
	}

	// Remove git worktree if it exists
	if work.WorktreePath != "" {
		if err := worktree.RemoveForce(proj.MainRepoPath(), work.WorktreePath); err != nil {
			// Warn but continue - worktree might not exist
		}
	}

	// Remove work directory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.RemoveAll(workDir); err != nil {
		// Warn but continue - directory might not exist
	}

	// Delete work from database (also deletes associated tasks and relationships)
	if err := proj.DB.DeleteWork(ctx, workID); err != nil {
		return fmt.Errorf("failed to delete work from database: %w", err)
	}

	return nil
}

// runAutomatedWorkflowForWork runs the full automated workflow for an existing work.
// This includes: create estimate task -> execute -> review/fix loop -> PR
// Delegates to runFullAutomatedWorkflow in run.go for the actual implementation.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func runAutomatedWorkflowForWork(proj *project.Project, workID, worktreePath string, w io.Writer) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	// Create estimate task from unassigned work beads (post-estimation will create implement tasks)
	err := createEstimateTaskFromWorkBeads(ctx, proj, workID, mainRepoPath, w)
	if err != nil {
		return fmt.Errorf("failed to create estimate task: %w", err)
	}

	return runFullAutomatedWorkflow(proj, workID, worktreePath, w)
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

	// Parse bead IDs from args
	beadIDs, err := parseBeadArgs(ctx, args, proj.Beads)
	if err != nil {
		return err
	}

	if len(beadIDs) == 0 {
		return fmt.Errorf("no beads specified")
	}

	// Check if any bead is already in a task
	for _, beadID := range beadIDs {
		inTask, err := proj.DB.IsBeadInTask(ctx, workID, beadID)
		if err != nil {
			return fmt.Errorf("failed to check bead %s: %w", beadID, err)
		}
		if inTask {
			return fmt.Errorf("bead %s is already assigned to a task", beadID)
		}
	}

	// Add beads to work
	if err := addBeadsToWork(ctx, proj, workID, beadIDs); err != nil {
		return fmt.Errorf("failed to add beads: %w", err)
	}

	fmt.Printf("Added %d bead(s) to work %s\n", len(beadIDs), workID)
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
	fmt.Printf("%-10s %-12s %-15s %-20s %s\n", "ID", "Status", "Root Issue", "Branch", "PR URL")
	fmt.Printf("%-10s %-12s %-15s %-20s %s\n", strings.Repeat("-", 10), strings.Repeat("-", 12), strings.Repeat("-", 15), strings.Repeat("-", 20), strings.Repeat("-", 30))

	for _, work := range works {
		prURL := work.PRURL
		if prURL == "" {
			prURL = "-"
		}
		rootIssue := work.RootIssueID
		if rootIssue == "" {
			rootIssue = "-"
		}
		fmt.Printf("%-10s %-12s %-15s %-20s %s\n", work.ID, work.Status, rootIssue, work.BranchName, prURL)
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
	if work.RootIssueID != "" {
		fmt.Printf("Root Issue: %s\n", work.RootIssueID)
	}
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

	// Check if work has uncompleted tasks (for interactive confirmation)
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

	// Destroy the work
	if err := DestroyWork(ctx, proj, workID, os.Stdout); err != nil {
		return err
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
	if err := processTask(proj, prTaskID); err != nil {
		return err
	}

	// Close the root issue now that PR has been created
	if work.RootIssueID != "" {
		fmt.Printf("Closing root issue %s...\n", work.RootIssueID)
		if err := beads.Close(ctx, work.RootIssueID, proj.MainRepoPath()); err != nil {
			fmt.Printf("Warning: failed to close root issue %s: %v\n", work.RootIssueID, err)
		}
	}

	return nil
}

func runWorkReview(cmd *cobra.Command, args []string) error {
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

	// Run review-fix loop if --auto is set
	maxIterations := proj.Config.Workflow.GetMaxReviewIterations()
	for iteration := 0; ; iteration++ {
		// Check max iterations
		if flagReviewAuto && iteration >= maxIterations {
			fmt.Printf("Warning: Maximum review iterations (%d) reached\n", maxIterations)
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

		// Get the review task to check its creation timestamp
		reviewTask, err := proj.DB.GetTask(ctx, reviewTaskID)
		if err != nil {
			return fmt.Errorf("failed to get review task: %w", err)
		}

		// Check if the review created any issues under the root issue
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
					(issue.Status == "" || issue.Status == "ready" || issue.Status == "open") &&
					issue.ExternalRef == expectedExternalRef {
					beadsToFix = append(beadsToFix, issue)
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

func runWorkConsole(cmd *cobra.Command, args []string) error {
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

	// Open console in the work's worktree
	return claude.OpenConsole(ctx, workID, proj.Config.Project.Name, work.WorktreePath, work.Name, proj.Config.Hooks.Env, os.Stdout)
}

func runWorkClaude(cmd *cobra.Command, args []string) error {
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

	// Open Claude Code session in the work's worktree
	return claude.OpenClaudeSession(ctx, workID, proj.Config.Project.Name, work.WorktreePath, work.Name, proj.Config.Hooks.Env, proj.Config, os.Stdout)
}
