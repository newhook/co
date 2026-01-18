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
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/names"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/worktree"
)

// generateBranchNameFromIssue creates a git-friendly branch name from an issue's title.
// It converts the title to lowercase, replaces spaces with hyphens,
// removes special characters, and prefixes with "feat/".
func generateBranchNameFromIssue(issue *beads.Bead) string {
	title := issue.Title

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

// parseBeadIDs parses a comma-delimited string of bead IDs into a slice.
// It trims whitespace from each ID and filters out empty strings.
func parseBeadIDs(beadIDStr string) []string {
	if beadIDStr == "" {
		return nil
	}

	parts := strings.Split(beadIDStr, ",")
	var result []string
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id != "" {
			result = append(result, id)
		}
	}
	return result
}

// generateBranchNameFromIssues creates a git-friendly branch name from multiple issues' titles.
// For a single issue, it uses that issue's title.
// For multiple issues, it combines titles (truncated) or uses a generic name if too long.
func generateBranchNameFromIssues(issues []*beads.Bead) string {
	if len(issues) == 0 {
		return "feat/automated-work"
	}

	if len(issues) == 1 {
		return generateBranchNameFromIssue(issues[0])
	}

	// For multiple issues, combine their titles
	var titles []string
	for _, issue := range issues {
		titles = append(titles, issue.Title)
	}
	combined := strings.Join(titles, " and ")

	// Convert to lowercase
	combined = strings.ToLower(combined)

	// Replace spaces and underscores with hyphens
	combined = strings.ReplaceAll(combined, " ", "-")
	combined = strings.ReplaceAll(combined, "_", "-")

	// Remove characters that aren't alphanumeric or hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	combined = reg.ReplaceAllString(combined, "")

	// Collapse multiple hyphens into one
	reg = regexp.MustCompile(`-+`)
	combined = reg.ReplaceAllString(combined, "-")

	// Trim leading/trailing hyphens
	combined = strings.Trim(combined, "-")

	// Truncate if too long (git branch names can be long, but let's be reasonable)
	if len(combined) > 50 {
		combined = combined[:50]
		// Don't end with a hyphen
		combined = strings.TrimRight(combined, "-")
	}

	return fmt.Sprintf("feat/%s", combined)
}

// collectIssuesForMultipleIDs collects all issues to include for multiple bead IDs.
// It collects transitive dependencies for each bead and deduplicates the results.
func collectIssuesForMultipleIDs(ctx context.Context, beadIDList []string, dir string) (*beads.BeadsWithDepsResult, error) {
	// Use a map to deduplicate issue IDs
	issueIDSet := make(map[string]bool)

	for _, beadID := range beadIDList {
		issueIDs, err := collectIssueIDsForAutomatedWorkflow(ctx, beadID, dir)
		if err != nil {
			return nil, err
		}
		for _, id := range issueIDs {
			issueIDSet[id] = true
		}
	}

	// Convert set to slice
	var issueIDs []string
	for id := range issueIDSet {
		issueIDs = append(issueIDs, id)
	}

	// Get all issues with dependencies in one call
	beadsDBPath := filepath.Join(dir, ".beads", "beads.db")
	beadsClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client: %w", err)
	}
	defer beadsClient.Close()

	return beadsClient.GetBeadsWithDeps(ctx, issueIDs)
}

// ensureUniqueBranchName checks if a branch already exists and appends a suffix if needed.
// Returns a unique branch name that doesn't conflict with existing branches.
func ensureUniqueBranchName(repoPath, baseName string) (string, error) {
	// Check if the base name is available
	if !branchExists(repoPath, baseName) {
		return baseName, nil
	}

	// Try appending suffixes until we find an available name
	for i := 2; i <= 100; i++ {
		candidate := fmt.Sprintf("%s-%d", baseName, i)
		if !branchExists(repoPath, candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not find unique branch name after 100 attempts (base: %s)", baseName)
}

// branchExists checks if a branch exists locally or remotely.
func branchExists(repoPath, branchName string) bool {
	// Check local branches
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
	cmd.Dir = repoPath
	if cmd.Run() == nil {
		return true
	}

	// Check remote branches
	cmd = exec.Command("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branchName)
	cmd.Dir = repoPath
	if cmd.Run() == nil {
		return true
	}

	return false
}

// collectIssueIDsForAutomatedWorkflow collects all issue IDs to include in the workflow.
// For an issue with dependencies, it includes all transitive dependencies.
// For an epic issue, it includes all child issues (non-epic).
func collectIssueIDsForAutomatedWorkflow(ctx context.Context, beadID, dir string) ([]string, error) {
	// Create beads client
	beadsDBPath := filepath.Join(dir, ".beads", "beads.db")
	beadsClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client: %w", err)
	}
	defer beadsClient.Close()

	// First, get the main issue
	mainIssue, err := beadsClient.GetBead(ctx, beadID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}
	if mainIssue == nil {
		return nil, fmt.Errorf("bead %s not found", beadID)
	}

	// Check if this issue has children (is an epic)
	// Children are in the dependents with parent-child relationship
	var hasChildren bool
	for _, dep := range mainIssue.Dependents {
		if dep.Type == "parent-child" {
			hasChildren = true
			break
		}
	}

	if hasChildren {
		// For epics, collect all children
		allIssues, err := beadsClient.GetBeadWithChildren(ctx, beadID)
		if err != nil {
			return nil, fmt.Errorf("failed to get children for epic %s: %w", beadID, err)
		}

		// Collect issue IDs
		issueIDs := make([]string, len(allIssues))
		for i, issue := range allIssues {
			issueIDs[i] = issue.ID
		}

		// Get dependencies/dependents for all issues in one call to determine which are epics
		depsResult, err := beadsClient.GetBeadsWithDeps(ctx, issueIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get dependencies for epic children: %w", err)
		}

		// Filter to only include non-epic issues
		var result []string
		for _, issue := range allIssues {
			// Check if this is an epic (has children in dependents)
			isEpic := false
			for _, dep := range depsResult.Dependents[issue.ID] {
				if dep.Type == "parent-child" {
					isEpic = true
					break
				}
			}
			if !isEpic {
				result = append(result, issue.ID)
			}
		}
		return result, nil
	}

	// For regular issues, collect transitive dependencies
	transitiveIssues, err := beadsClient.GetTransitiveDependencies(ctx, beadID)
	if err != nil {
		return nil, err
	}

	// Extract issue IDs
	issueIDs := make([]string, len(transitiveIssues))
	for i, issue := range transitiveIssues {
		issueIDs[i] = issue.ID
	}

	return issueIDs, nil
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

	// Create beads client
	beadsDBPath := filepath.Join(mainRepoPath, ".beads", "beads.db")
	beadsClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
	if err != nil {
		return fmt.Errorf("failed to create beads client: %w", err)
	}
	defer beadsClient.Close()

	// Get the root issue and generate branch name
	rootIssue, err := beadsClient.GetBead(ctx, beadID)
	if err != nil {
		return fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}
	if rootIssue == nil {
		return fmt.Errorf("bead %s not found", beadID)
	}

	branchName := generateBranchNameFromIssue(rootIssue.Bead)
	branchName, err = ensureUniqueBranchName(mainRepoPath, branchName)
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

	// Create work record in database with the root issue ID
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch, beadID); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
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
	beadsDBPath := filepath.Join(mainRepoPath, ".beads", "beads.db")
	beadsClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
	if err != nil {
		return fmt.Errorf("failed to create beads client: %w", err)
	}
	defer beadsClient.Close()

	rootIssue, err := beadsClient.GetBead(ctx, beadID)
	if err != nil {
		return fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}
	if rootIssue == nil {
		return fmt.Errorf("bead %s not found", beadID)
	}

	branchName := generateBranchNameFromIssue(rootIssue.Bead)
	branchName, err = ensureUniqueBranchName(mainRepoPath, branchName)
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

	// Create work record in database with the root issue ID
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch, beadID); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
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
	issuesResult, err := collectIssuesForMultipleIDs(ctx, []string{beadID}, mainRepoPath)
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
