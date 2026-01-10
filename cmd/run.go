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
	"github.com/newhook/co/internal/worktree"
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
		if err := ensureFeatureBranch(flagBranch, proj.MainRepoPath()); err != nil {
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
		if err := createFinalPR(flagBranch, beadList, proj.MainRepoPath()); err != nil {
			return fmt.Errorf("failed to create final PR: %w", err)
		}
	}

	return nil
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

// processBeadWithWorktree processes a bead using an isolated worktree.
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

	// Build prompt for Claude
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
	}

	return true, nil
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

func buildPrompt(bead beads.Bead, branchName, baseBranch string) string {
	return fmt.Sprintf(`You are implementing a task from the beads issue tracker.

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
}
