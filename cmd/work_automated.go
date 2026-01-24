package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/names"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/work"
	"github.com/newhook/co/internal/worktree"
)

// generateBranchNameFromIssue creates a git-friendly branch name from an issue's title.
// Delegates to internal/work.GenerateBranchNameFromIssue.
func generateBranchNameFromIssue(issue *beads.Bead) string {
	return work.GenerateBranchNameFromIssue(issue)
}

// parseBeadIDs parses a comma-delimited string of bead IDs into a slice.
// Delegates to internal/work.ParseBeadIDs.
func parseBeadIDs(beadIDStr string) []string {
	return work.ParseBeadIDs(beadIDStr)
}

// generateBranchNameFromIssues creates a git-friendly branch name from multiple issues' titles.
// Delegates to internal/work.GenerateBranchNameFromIssues.
func generateBranchNameFromIssues(issues []*beads.Bead) string {
	return work.GenerateBranchNameFromIssues(issues)
}

// collectIssuesForMultipleIDs collects all issues to include for multiple bead IDs.
// Delegates to internal/work.CollectIssuesForMultipleIDs.
func collectIssuesForMultipleIDs(ctx context.Context, beadIDList []string, beadsClient *beads.Client) (*beads.BeadsWithDepsResult, error) {
	return work.CollectIssuesForMultipleIDs(ctx, beadIDList, beadsClient)
}

// ensureUniqueBranchName checks if a branch already exists and appends a suffix if needed.
// Delegates to internal/work.EnsureUniqueBranchName.
func ensureUniqueBranchName(ctx context.Context, repoPath, baseName string) (string, error) {
	return work.EnsureUniqueBranchName(ctx, repoPath, baseName)
}

// collectIssueIDsForAutomatedWorkflow collects all issue IDs to include in the workflow.
// Delegates to internal/work.CollectIssueIDsForAutomatedWorkflow.
func collectIssueIDsForAutomatedWorkflow(ctx context.Context, beadID string, beadsClient *beads.Client) ([]string, error) {
	return work.CollectIssueIDsForAutomatedWorkflow(ctx, beadID, beadsClient)
}

// runWorkCreateWithBeads creates a work unit with an auto-generated branch name from a single root bead.
// Unlike runAutomatedWorkflow, this does NOT plan tasks or spawn the orchestrator.
// The user is expected to manually run 'co plan' and 'co run' afterward.
//
// The beadID parameter should be a single bead ID which becomes the root issue.
// If the bead is an epic, child beads will be expanded.
func runWorkCreateWithBeads(proj *project.Project, beadID string, baseBranch string) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	if beadID == "" {
		return fmt.Errorf("no bead ID provided")
	}

	fmt.Printf("Creating work for bead: %s\n", beadID)

	// Get the root issue and generate branch name
	rootIssue, err := proj.Beads.GetBead(ctx, beadID)
	if err != nil {
		return fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}
	if rootIssue == nil {
		return fmt.Errorf("bead %s not found", beadID)
	}

	branchName := generateBranchNameFromIssue(rootIssue.Bead)
	branchName, err = ensureUniqueBranchName(ctx, mainRepoPath, branchName)
	if err != nil {
		return fmt.Errorf("failed to find unique branch name: %w", err)
	}
	fmt.Printf("Branch name: %s\n", branchName)

	// Generate work ID and create work directory
	workID, err := proj.DB.GenerateWorkID(ctx, branchName, proj.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("failed to generate work ID: %w", err)
	}
	fmt.Printf("Work ID: %s\n", workID)

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Create git worktree inside work directory
	worktreePath := filepath.Join(workDir, "tree")
	if baseBranch == "" {
		baseBranch = "main"
	}

	// Create worktree with new branch
	if err := worktree.Create(ctx, mainRepoPath, worktreePath, branchName, baseBranch); err != nil {
		os.RemoveAll(workDir)
		return err
	}

	// Push branch and set upstream
	if err := git.PushSetUpstreamInDir(ctx, branchName, worktreePath); err != nil {
		worktree.RemoveForce(ctx, mainRepoPath, worktreePath)
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

	// Create work record in database with the root issue ID
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch, beadID, false); err != nil {
		worktree.RemoveForce(ctx, mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	// Spawn the orchestrator for this work
	fmt.Println("\nSpawning orchestrator...")
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, worktreePath, workerName, os.Stdout); err != nil {
		fmt.Printf("Warning: failed to spawn orchestrator: %v\n", err)
		fmt.Println("You can start it manually with: co run")
	} else {
		fmt.Println("Orchestrator is running in zellij tab.")
	}

	fmt.Printf("\nCreated work: %s\n", workID)
	if workerName != "" {
		fmt.Printf("Worker: %s\n", workerName)
	}
	fmt.Printf("Directory: %s\n", workDir)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("Branch: %s\n", branchName)
	fmt.Printf("Base Branch: %s\n", baseBranch)

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  cd %s\n", workID)
	fmt.Printf("  co plan              # Plan tasks for this work\n")

	return nil
}

// runAutomatedWorkflow creates a work unit from a single root bead and spawns the orchestrator.
// The workflow:
// 1. Creates work unit with worktree and branch (auto-generated from bead title)
// 2. Collects all beads to include (transitive dependencies or epic children)
// 3. Creates tasks with explicit dependencies: estimate -> implement -> review -> pr
// 4. Spawns orchestrator which polls for ready tasks and executes them
//
// The beadID parameter should be a single bead ID which becomes the root issue.
// If the bead is an epic, child beads will be expanded.
func runAutomatedWorkflow(proj *project.Project, beadID string, baseBranch string) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	if beadID == "" {
		return fmt.Errorf("no bead ID provided")
	}

	fmt.Printf("Starting automated workflow for bead: %s\n", beadID)

	// Step 1: Get the root bead and generate branch name
	rootIssue, err := proj.Beads.GetBead(ctx, beadID)
	if err != nil {
		return fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}
	if rootIssue == nil {
		return fmt.Errorf("bead %s not found", beadID)
	}

	branchName := generateBranchNameFromIssue(rootIssue.Bead)
	branchName, err = ensureUniqueBranchName(ctx, mainRepoPath, branchName)
	if err != nil {
		return fmt.Errorf("failed to find unique branch name: %w", err)
	}
	fmt.Printf("Branch name: %s\n", branchName)

	// Step 2: Generate work ID and create work directory
	workID, err := proj.DB.GenerateWorkID(ctx, branchName, proj.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("failed to generate work ID: %w", err)
	}
	fmt.Printf("Work ID: %s\n", workID)

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Create git worktree inside work directory
	worktreePath := filepath.Join(workDir, "tree")
	if baseBranch == "" {
		baseBranch = "main"
	}

	// Create worktree with new branch
	if err := worktree.Create(ctx, mainRepoPath, worktreePath, branchName, baseBranch); err != nil {
		os.RemoveAll(workDir)
		return err
	}

	// Push branch and set upstream
	if err := git.PushSetUpstreamInDir(ctx, branchName, worktreePath); err != nil {
		worktree.RemoveForce(ctx, mainRepoPath, worktreePath)
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

	// Create work record in database with the root issue ID
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch, beadID, true); err != nil {
		worktree.RemoveForce(ctx, mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	fmt.Printf("Created work: %s\n", workID)
	if workerName != "" {
		fmt.Printf("Worker: %s\n", workerName)
	}
	fmt.Printf("Worktree: %s\n", worktreePath)

	// Step 3: Collect issues (expand epic children and transitive dependencies)
	fmt.Println("Collecting beads...")
	issuesResult, err := collectIssuesForMultipleIDs(ctx, []string{beadID}, proj.Beads)
	if err != nil {
		return fmt.Errorf("failed to collect beads: %w", err)
	}

	if len(issuesResult.Beads) == 0 {
		return fmt.Errorf("no beads to process for %s", beadID)
	}

	fmt.Printf("Collected %d bead(s):\n", len(issuesResult.Beads))
	var collectedBeadIDs []string
	for id, issue := range issuesResult.Beads {
		fmt.Printf("  - %s: %s\n", id, issue.Title)
		collectedBeadIDs = append(collectedBeadIDs, id)
	}

	// Step 4: Create estimate task only (implement/review/pr tasks created after estimation completes)
	fmt.Println("\nCreating estimate task...")

	// Use sequential task numbering instead of timestamps
	nextNum, err := proj.DB.GetNextTaskNumber(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get next task number: %w", err)
	}
	estimateTaskID := fmt.Sprintf("%s.%d", workID, nextNum)
	if err := proj.DB.CreateTask(ctx, estimateTaskID, "estimate", collectedBeadIDs, 0, workID); err != nil {
		return fmt.Errorf("failed to create estimate task: %w", err)
	}
	fmt.Printf("Created estimate task: %s\n", estimateTaskID)
	fmt.Println("Note: Implement, review, and PR tasks will be created after estimation completes.")

	// Step 5: Spawn the orchestrator
	fmt.Println("\nSpawning orchestrator...")
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, worktreePath, workerName, os.Stdout); err != nil {
		return fmt.Errorf("failed to spawn orchestrator: %w", err)
	}

	fmt.Println("\nWorkflow is now running in a zellij tab.")
	fmt.Println("Switch to the zellij session to monitor progress.")
	return nil
}
