package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/newhook/autoclaude/internal/beads"
	"github.com/newhook/autoclaude/internal/claude"
	"github.com/newhook/autoclaude/internal/db"
	"github.com/newhook/autoclaude/internal/git"
	"github.com/newhook/autoclaude/internal/github"
	"github.com/newhook/autoclaude/internal/project"
	"github.com/newhook/autoclaude/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	flagBranch  string
	flagLimit   int
	flagDryRun  bool
	flagNoMerge bool
	flagDeps    bool
	flagProject string
)

var runCmd = &cobra.Command{
	Use:   "run [bead-id]",
	Short: "Process ready issues with Claude Code",
	Long: `Run processes ready issues by invoking Claude Code to implement
each one and creating PRs for the changes.

If a bead ID is provided, only that issue will be processed.

When run from a project directory (with .co/), uses worktree isolation:
- Each task gets its own worktree at <project>/<task-id>/
- Worktrees are cleaned up on success, kept on failure for debugging

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
}

func runBeads(cmd *cobra.Command, args []string) error {
	// Check if a specific bead ID was provided as positional argument
	var beadID string
	if len(args) > 0 {
		beadID = args[0]
	}

	// Try to find project context
	proj, err := findProject()
	if err != nil {
		// No project found - run in legacy mode
		return runBeadsLegacy(beadID)
	}

	return runBeadsWithProject(proj, beadID)
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

// runBeadsWithProject runs beads using project-based worktree isolation.
func runBeadsWithProject(proj *project.Project, beadID string) error {
	fmt.Printf("Using project: %s\n", proj.Config.Project.Name)

	// Get beads to process from the main repo directory
	beadList, err := getBeadsToProcessInDir(beadID, proj.MainRepoPath())
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

	// Open project's tracking database
	database, err := proj.OpenDB()
	if err != nil {
		return fmt.Errorf("failed to open tracking database: %w", err)
	}
	defer proj.Close()

	// If using feature branch, ensure it exists in the main repo
	if useFeatureBranch {
		if err := ensureFeatureBranchInDir(flagBranch, proj.MainRepoPath()); err != nil {
			return fmt.Errorf("failed to setup feature branch: %w", err)
		}
	}

	// Process each bead with worktree isolation
	processedCount := 0
	for _, bead := range beadList {
		sessionName := claude.SessionNameForProject(proj.Config.Project.Name)

		// Record bead as processing in database
		if err := database.StartBead(bead.ID, bead.Title, sessionName, bead.ID); err != nil {
			return fmt.Errorf("failed to record bead start: %w", err)
		}

		success, err := processBeadWithWorktree(proj, database, bead)
		if err != nil {
			// Record failure in database
			database.FailBead(bead.ID, err.Error())
			return fmt.Errorf("failed to process bead %s: %w", bead.ID, err)
		}

		if success {
			processedCount++
		}
	}

	fmt.Printf("Successfully processed %d bead(s)\n", processedCount)

	// Create final PR from feature branch to main if applicable
	if useFeatureBranch && processedCount > 0 && !flagNoMerge {
		if err := createFinalPRInDir(flagBranch, beadList, proj.MainRepoPath()); err != nil {
			return fmt.Errorf("failed to create final PR: %w", err)
		}
	}

	return nil
}

// processBeadWithWorktree processes a bead using an isolated worktree.
// Returns true if successful, false if failed but recoverable.
func processBeadWithWorktree(proj *project.Project, database *db.DB, bead beads.Bead) (bool, error) {
	fmt.Printf("\n=== Processing bead %s: %s ===\n", bead.ID, bead.Title)

	branchName := fmt.Sprintf("bead/%s", bead.ID)
	worktreePath := proj.WorktreePath(bead.ID)
	mainRepoPath := proj.MainRepoPath()

	// Check if worktree already exists
	if worktree.ExistsPath(worktreePath) {
		fmt.Printf("Worktree already exists at %s, resuming...\n", worktreePath)
	} else {
		// Create worktree from the base branch
		fmt.Printf("Creating worktree at %s...\n", worktreePath)
		if err := worktree.Create(mainRepoPath, worktreePath, branchName); err != nil {
			return false, fmt.Errorf("failed to create worktree: %w", err)
		}
	}

	// Build prompt for Claude (includes branch info so Claude can create PR)
	prompt := buildPrompt(bead, branchName, flagBranch)

	// Run Claude in the worktree directory
	fmt.Println("Running Claude Code...")
	ctx := context.Background()
	projectName := proj.Config.Project.Name
	if err := claude.RunInProject(ctx, database, bead.ID, prompt, worktreePath, projectName); err != nil {
		fmt.Printf("Claude failed: %v\n", err)
		fmt.Printf("Worktree kept for debugging at: %s\n", worktreePath)
		return false, fmt.Errorf("claude failed: %w", err)
	}

	// Success - clean up worktree
	fmt.Printf("Cleaning up worktree %s...\n", worktreePath)
	if err := worktree.Remove(mainRepoPath, worktreePath); err != nil {
		fmt.Printf("Warning: failed to remove worktree: %v\n", err)
		// Don't fail the whole process for cleanup issues
	}

	return true, nil
}

// runBeadsLegacy runs beads without project context (original behavior).
func runBeadsLegacy(beadID string) error {
	// Get beads to process
	beadList, err := getBeadsToProcess(beadID)
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

	// Dry run - just show plan
	if flagDryRun {
		fmt.Printf("Dry run: would process %d bead(s):\n", len(beadList))
		for _, b := range beadList {
			fmt.Printf("  - %s: %s\n", b.ID, b.Title)
		}
		if useFeatureBranch {
			fmt.Printf("\nFeature branch workflow: PRs target '%s', final PR to 'main'\n", flagBranch)
		}
		return nil
	}

	// Open tracking database
	database, err := db.Open()
	if err != nil {
		return fmt.Errorf("failed to open tracking database: %w", err)
	}
	defer database.Close()

	// Get current working directory for Claude
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// If using feature branch, ensure it exists
	if useFeatureBranch {
		if err := ensureFeatureBranch(flagBranch); err != nil {
			return fmt.Errorf("failed to setup feature branch: %w", err)
		}
	}

	// Process each bead
	processedCount := 0
	for _, bead := range beadList {
		// Record bead as processing in database
		if err := database.StartBead(bead.ID, bead.Title, claude.SessionName, bead.ID); err != nil {
			return fmt.Errorf("failed to record bead start: %w", err)
		}

		if err := processBead(database, bead, workDir); err != nil {
			// Record failure in database
			database.FailBead(bead.ID, err.Error())
			return fmt.Errorf("failed to process bead %s: %w", bead.ID, err)
		}
		processedCount++
	}

	fmt.Printf("Successfully processed %d bead(s)\n", processedCount)

	// Create final PR from feature branch to main if applicable
	if useFeatureBranch && processedCount > 0 && !flagNoMerge {
		if err := createFinalPR(flagBranch, beadList); err != nil {
			return fmt.Errorf("failed to create final PR: %w", err)
		}
	}

	return nil
}

func getBeadsToProcess(beadID string) ([]beads.Bead, error) {
	return getBeadsToProcessInDir(beadID, "")
}

func getBeadsToProcessInDir(beadID, dir string) ([]beads.Bead, error) {
	// If specific bead requested, get just that one (optionally with deps)
	if beadID != "" {
		if flagDeps {
			return getBeadWithDepsInDir(beadID, dir)
		}
		bead, err := beads.GetBeadInDir(beadID, dir)
		if err != nil {
			return nil, err
		}
		return []beads.Bead{*bead}, nil
	}

	// --deps requires a bead ID
	if flagDeps {
		return nil, fmt.Errorf("--deps requires a bead ID argument")
	}

	// Otherwise get all ready beads
	return beads.GetReadyBeadsInDir(dir)
}

// getBeadWithDeps returns open dependencies followed by the bead itself.
func getBeadWithDeps(beadID string) ([]beads.Bead, error) {
	return getBeadWithDepsInDir(beadID, "")
}

func getBeadWithDepsInDir(beadID, dir string) ([]beads.Bead, error) {
	beadWithDeps, err := beads.GetBeadWithDepsInDir(beadID, dir)
	if err != nil {
		return nil, err
	}

	var result []beads.Bead

	// Add open dependencies first (in order they appear)
	for _, dep := range beadWithDeps.Dependencies {
		if dep.DependencyType == "depends_on" && dep.Status == "open" {
			// Recursively get this dependency with its own deps
			depBeads, err := getBeadWithDepsInDir(dep.ID, dir)
			if err != nil {
				return nil, fmt.Errorf("failed to get dependency %s: %w", dep.ID, err)
			}
			result = append(result, depBeads...)
		}
	}

	// Add the requested bead last
	result = append(result, beads.Bead{
		ID:          beadWithDeps.ID,
		Title:       beadWithDeps.Title,
		Description: beadWithDeps.Description,
	})

	return result, nil
}

func ensureFeatureBranch(branch string) error {
	return ensureFeatureBranchInDir(branch, "")
}

func ensureFeatureBranchInDir(branch, dir string) error {
	// Try to checkout existing branch first
	if err := git.CheckoutInDir(branch, dir); err == nil {
		return nil
	}

	// Branch doesn't exist, create it from main
	if err := git.CheckoutInDir("main", dir); err != nil {
		return fmt.Errorf("failed to checkout main: %w", err)
	}
	if err := git.CreateBranchInDir(branch, dir); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", branch, err)
	}
	// Push the new branch to remote
	if err := git.PushInDir(branch, dir); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branch, err)
	}
	return nil
}

func processBead(database *db.DB, bead beads.Bead, workDir string) error {
	fmt.Printf("\n=== Processing bead %s: %s ===\n", bead.ID, bead.Title)

	branchName := fmt.Sprintf("bead/%s", bead.ID)

	// Create branch from target branch (feature branch or main)
	if err := git.Checkout(flagBranch); err != nil {
		return fmt.Errorf("failed to checkout base branch: %w", err)
	}
	if err := git.CreateBranch(branchName); err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}

	// Build prompt for Claude (includes branch info so Claude can create PR)
	prompt := buildPrompt(bead, branchName, flagBranch)

	// Run Claude - it will implement, create PR, close bead, and merge
	fmt.Println("Running Claude Code...")
	ctx := context.Background()
	if err := claude.Run(ctx, database, bead.ID, prompt, workDir); err != nil {
		return fmt.Errorf("claude failed: %w", err)
	}

	// Return to base branch and pull latest (Claude should have merged)
	if err := git.Checkout(flagBranch); err != nil {
		return fmt.Errorf("failed to return to base branch: %w", err)
	}
	if err := git.Pull(); err != nil {
		return fmt.Errorf("failed to pull base branch: %w", err)
	}

	return nil
}

func createFinalPR(featureBranch string, processedBeads []beads.Bead) error {
	return createFinalPRInDir(featureBranch, processedBeads, "")
}

func createFinalPRInDir(featureBranch string, processedBeads []beads.Bead, dir string) error {
	fmt.Printf("\n=== Creating final PR: %s → main ===\n", featureBranch)

	// Check if there are commits ahead of main
	hasCommits, err := git.HasCommitsAheadInDir("main", dir)
	if err != nil {
		return fmt.Errorf("failed to check commits: %w", err)
	}

	if !hasCommits {
		fmt.Println("No changes to merge to main")
		return nil
	}

	// Build PR title and body
	prTitle := fmt.Sprintf("Feature: %s", featureBranch)
	var prBody string
	prBody = "## Beads implemented:\n"
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

	// Return to main
	if err := git.CheckoutInDir("main", dir); err != nil {
		return fmt.Errorf("failed to checkout main: %w", err)
	}
	if err := git.PullInDir(dir); err != nil {
		return fmt.Errorf("failed to pull main: %w", err)
	}

	return nil
}

func buildPrompt(bead beads.Bead, branchName, baseBranch string) string {
	prompt := fmt.Sprintf(`You are implementing a task from the beads issue tracker.

Bead ID: %s
Title: %s
Branch: %s
Base Branch: %s

Description:
%s

Instructions:
1. First, check git log and git status to see if there is existing work on this branch from a previous session
2. If there is existing work, review it and continue from where it left off
3. Implement the changes described above
4. Make commits as you work (with clear commit messages)
5. When implementation is complete, push the branch and create a PR targeting %s
6. Close the bead with: bd close %s --reason "<brief summary of what was implemented>"
7. Merge the PR using: gh pr merge --squash --delete-branch
8. Mark completion by running: co complete %s --pr <PR_URL>

Focus on implementing the task correctly and completely.`, bead.ID, bead.Title, branchName, baseBranch, bead.Description, baseBranch, bead.ID, bead.ID)

	return prompt
}
