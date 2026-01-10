package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/newhook/autoclaude/internal/beads"
	"github.com/newhook/autoclaude/internal/claude"
	"github.com/newhook/autoclaude/internal/git"
	"github.com/newhook/autoclaude/internal/github"
	"github.com/spf13/cobra"
)

var (
	flagBranch  string
	flagLimit   int
	flagDryRun  bool
	flagNoMerge bool
	flagDeps    bool
)

var runCmd = &cobra.Command{
	Use:   "run [bead-id]",
	Short: "Process ready beads with Claude Code",
	Long: `Run processes all ready beads by invoking Claude Code to implement
each task and creating PRs for the changes.

If a bead ID is provided as an argument, only that bead will be processed.

When --branch is specified (not "main"), PRs target that feature branch.
After all beads complete, a final PR is created from the feature branch to main.

The workflow for each bead:
1. Claude Code creates a branch and implements the changes
2. A PR is created and merged
3. The bead is closed with a summary`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBeads,
}

func init() {
	runCmd.Flags().StringVarP(&flagBranch, "branch", "b", "main", "target branch for PRs")
	runCmd.Flags().IntVarP(&flagLimit, "limit", "n", 0, "maximum number of beads to process (0 = unlimited)")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "show plan without executing")
	runCmd.Flags().BoolVar(&flagNoMerge, "no-merge", false, "create PRs but don't merge them")
	runCmd.Flags().BoolVar(&flagDeps, "deps", false, "also process open dependencies of the specified bead")
}

func runBeads(cmd *cobra.Command, args []string) error {
	// Check if a specific bead ID was provided as positional argument
	var beadID string
	if len(args) > 0 {
		beadID = args[0]
	}

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
		if err := processBead(bead, workDir); err != nil {
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
	// If specific bead requested, get just that one (optionally with deps)
	if beadID != "" {
		if flagDeps {
			return getBeadWithDeps(beadID)
		}
		bead, err := beads.GetBead(beadID)
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
	return beads.GetReadyBeads()
}

// getBeadWithDeps returns open dependencies followed by the bead itself.
func getBeadWithDeps(beadID string) ([]beads.Bead, error) {
	beadWithDeps, err := beads.GetBeadWithDeps(beadID)
	if err != nil {
		return nil, err
	}

	var result []beads.Bead

	// Add open dependencies first (in order they appear)
	for _, dep := range beadWithDeps.Dependencies {
		if dep.DependencyType == "depends_on" && dep.Status == "open" {
			// Recursively get this dependency with its own deps
			depBeads, err := getBeadWithDeps(dep.ID)
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
	// Try to checkout existing branch first
	if err := git.Checkout(branch); err == nil {
		return nil
	}

	// Branch doesn't exist, create it from main
	if err := git.Checkout("main"); err != nil {
		return fmt.Errorf("failed to checkout main: %w", err)
	}
	if err := git.CreateBranch(branch); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", branch, err)
	}
	// Push the new branch to remote
	if err := git.Push(branch); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branch, err)
	}
	return nil
}

func processBead(bead beads.Bead, workDir string) error {
	fmt.Printf("\n=== Processing bead %s: %s ===\n", bead.ID, bead.Title)

	branchName := fmt.Sprintf("bead/%s", bead.ID)

	// Create branch from target branch (feature branch or main)
	if err := git.Checkout(flagBranch); err != nil {
		return fmt.Errorf("failed to checkout base branch: %w", err)
	}
	if err := git.CreateBranch(branchName); err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}

	// Build prompt for Claude
	prompt := buildPrompt(bead)

	// Run Claude
	fmt.Println("Running Claude Code...")
	ctx := context.Background()
	if err := claude.Run(ctx, prompt, workDir); err != nil {
		// Cleanup on failure
		git.Checkout(flagBranch)
		git.DeleteBranch(branchName)
		return fmt.Errorf("claude failed: %w", err)
	}

	// Check if there are changes to commit
	hasCommits, err := git.HasCommitsAhead(flagBranch)
	if err != nil {
		git.Checkout(flagBranch)
		git.DeleteBranch(branchName)
		return fmt.Errorf("failed to check for commits: %w", err)
	}

	if !hasCommits {
		fmt.Println("No changes made, skipping PR creation")
		git.Checkout(flagBranch)
		git.DeleteBranch(branchName)
		return nil
	}

	// Push branch
	fmt.Println("Pushing branch...")
	if err := git.Push(branchName); err != nil {
		git.Checkout(flagBranch)
		git.DeleteBranch(branchName)
		return fmt.Errorf("failed to push: %w", err)
	}

	// Close bead before PR (per workflow spec - close while context is fresh)
	closeReason := fmt.Sprintf("Implemented in branch %s", branchName)
	fmt.Printf("Closing bead %s...\n", bead.ID)
	if err := beads.CloseBead(bead.ID, closeReason); err != nil {
		return fmt.Errorf("failed to close bead: %w", err)
	}

	// Create PR targeting the base branch (feature branch or main)
	prTitle := fmt.Sprintf("%s: %s", bead.ID, bead.Title)
	prBody := fmt.Sprintf("Implements bead %s\n\n%s", bead.ID, bead.Description)
	fmt.Println("Creating PR...")
	prURL, err := github.CreatePR(branchName, flagBranch, prTitle, prBody)
	if err != nil {
		return fmt.Errorf("failed to create PR: %w", err)
	}
	fmt.Printf("Created PR: %s\n", prURL)

	// Merge PR unless --no-merge
	if !flagNoMerge {
		fmt.Println("Merging PR...")
		if err := github.MergePR(prURL); err != nil {
			return fmt.Errorf("failed to merge PR: %w", err)
		}
		fmt.Println("PR merged successfully")
	}

	// Return to base branch and pull latest
	if err := git.Checkout(flagBranch); err != nil {
		return fmt.Errorf("failed to return to base branch: %w", err)
	}
	if err := git.Pull(); err != nil {
		return fmt.Errorf("failed to pull base branch: %w", err)
	}

	return nil
}

func createFinalPR(featureBranch string, processedBeads []beads.Bead) error {
	fmt.Printf("\n=== Creating final PR: %s → main ===\n", featureBranch)

	// Check if there are commits ahead of main
	hasCommits, err := git.HasCommitsAhead("main")
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
	prURL, err := github.CreatePR(featureBranch, "main", prTitle, prBody)
	if err != nil {
		return fmt.Errorf("failed to create final PR: %w", err)
	}
	fmt.Printf("Created final PR: %s\n", prURL)

	fmt.Println("Merging final PR...")
	if err := github.MergePR(prURL); err != nil {
		return fmt.Errorf("failed to merge final PR: %w", err)
	}
	fmt.Println("Final PR merged successfully")

	// Return to main
	if err := git.Checkout("main"); err != nil {
		return fmt.Errorf("failed to checkout main: %w", err)
	}
	if err := git.Pull(); err != nil {
		return fmt.Errorf("failed to pull main: %w", err)
	}

	return nil
}

func buildPrompt(bead beads.Bead) string {
	prompt := fmt.Sprintf(`You are implementing a task from the beads issue tracker.

Bead ID: %s
Title: %s

Description:
%s

Instructions:
1. Implement the changes described above
2. Make commits as you work (with clear commit messages)
3. Do NOT create a PR - that will be handled separately
4. Do NOT close the bead - that will be handled separately

Focus on implementing the task correctly and completely.`, bead.ID, bead.Title, bead.Description)

	return prompt
}
