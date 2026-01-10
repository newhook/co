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
	flagBead    string
	flagLimit   int
	flagDryRun  bool
	flagNoMerge bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Process ready beads with Claude Code",
	Long: `Run processes all ready beads by invoking Claude Code to implement
each task and creating PRs for the changes.

The workflow for each bead:
1. Claude Code creates a branch and implements the changes
2. A PR is created and merged
3. The bead is closed with a summary`,
	RunE: runBeads,
}

func init() {
	runCmd.Flags().StringVarP(&flagBranch, "branch", "b", "main", "target branch for PRs")
	runCmd.Flags().StringVar(&flagBead, "bead", "", "process only this specific bead ID")
	runCmd.Flags().IntVarP(&flagLimit, "limit", "n", 0, "maximum number of beads to process (0 = unlimited)")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "show plan without executing")
	runCmd.Flags().BoolVar(&flagNoMerge, "no-merge", false, "create PRs but don't merge them")
}

func runBeads(cmd *cobra.Command, args []string) error {
	// Get beads to process
	beadList, err := getBeadsToProcess()
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

	// Dry run - just show plan
	if flagDryRun {
		fmt.Printf("Dry run: would process %d bead(s):\n", len(beadList))
		for _, b := range beadList {
			fmt.Printf("  - %s: %s\n", b.ID, b.Title)
		}
		return nil
	}

	// Get current working directory for Claude
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Process each bead
	for _, bead := range beadList {
		if err := processBead(bead, workDir); err != nil {
			return fmt.Errorf("failed to process bead %s: %w", bead.ID, err)
		}
	}

	fmt.Printf("Successfully processed %d bead(s)\n", len(beadList))
	return nil
}

func getBeadsToProcess() ([]beads.Bead, error) {
	// If specific bead requested, get just that one
	if flagBead != "" {
		bead, err := beads.GetBead(flagBead)
		if err != nil {
			return nil, err
		}
		return []beads.Bead{*bead}, nil
	}

	// Otherwise get all ready beads
	return beads.GetReadyBeads()
}

func processBead(bead beads.Bead, workDir string) error {
	fmt.Printf("\n=== Processing bead %s: %s ===\n", bead.ID, bead.Title)

	branchName := fmt.Sprintf("bead/%s", bead.ID)

	// Create branch from base
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

	// Create PR
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

	// Return to base branch
	if err := git.Checkout(flagBranch); err != nil {
		return fmt.Errorf("failed to return to base branch: %w", err)
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
