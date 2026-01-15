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

// generateBranchNameFromBead creates a git-friendly branch name from a bead's title.
// It converts the title to lowercase, replaces spaces with hyphens,
// removes special characters, and prefixes with "feat/".
func generateBranchNameFromBead(bead *beads.Bead) string {
	title := bead.Title

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

// generateBranchNameFromBeads creates a git-friendly branch name from multiple beads' titles.
// For a single bead, it uses that bead's title.
// For multiple beads, it combines titles (truncated) or uses a generic name if too long.
func generateBranchNameFromBeads(beadList []*beads.Bead) string {
	if len(beadList) == 0 {
		return "feat/automated-work"
	}

	if len(beadList) == 1 {
		return generateBranchNameFromBead(beadList[0])
	}

	// For multiple beads, combine their titles
	var titles []string
	for _, bead := range beadList {
		titles = append(titles, bead.Title)
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

// collectBeadsForMultipleIDs collects all beads to include for multiple bead IDs.
// It collects transitive dependencies for each bead and deduplicates the results.
func collectBeadsForMultipleIDs(ctx context.Context, beadIDList []string, dir string) ([]beads.BeadWithDeps, error) {
	// Use a map to deduplicate beads by ID
	beadMap := make(map[string]beads.BeadWithDeps)

	for _, beadID := range beadIDList {
		beadsForID, err := collectBeadsForAutomatedWorkflow(ctx, beadID, dir)
		if err != nil {
			return nil, err
		}
		for _, b := range beadsForID {
			// Only add if not already present (first occurrence wins)
			if _, exists := beadMap[b.ID]; !exists {
				beadMap[b.ID] = b
			}
		}
	}

	// Convert map to slice
	var result []beads.BeadWithDeps
	for _, b := range beadMap {
		result = append(result, b)
	}

	return result, nil
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

// collectBeadsForAutomatedWorkflow collects all beads to include in the workflow.
// For a bead with dependencies, it includes all transitive dependencies.
// For an epic bead, it includes all child beads.
func collectBeadsForAutomatedWorkflow(ctx context.Context, beadID, dir string) ([]beads.BeadWithDeps, error) {
	// First, get the main bead
	mainBead, err := beads.GetBeadWithDeps(ctx, beadID, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}

	// Check if this bead has children (is an epic)
	// Children are in the Dependents field with parent-child relationship
	var hasChildren bool
	for _, dep := range mainBead.Dependents {
		if dep.DependencyType == "parent-child" {
			hasChildren = true
			break
		}
	}

	if hasChildren {
		// For epics, collect all children
		allBeads, err := beads.GetBeadWithChildren(ctx, beadID, dir)
		if err != nil {
			return nil, fmt.Errorf("failed to get children for epic %s: %w", beadID, err)
		}
		// Filter to only include non-epic beads (skip the epic itself)
		var result []beads.BeadWithDeps
		for _, b := range allBeads {
			// Check if this is an epic (has children in dependents)
			isEpic := false
			for _, dep := range b.Dependents {
				if dep.DependencyType == "parent-child" {
					isEpic = true
					break
				}
			}
			if !isEpic {
				result = append(result, b)
			}
		}
		return result, nil
	}

	// For regular beads, collect transitive dependencies
	return beads.GetTransitiveDependencies(ctx, beadID, dir)
}

// runWorkCreateWithBeads creates a work unit with an auto-generated branch name from beads.
// Unlike runAutomatedWorkflow, this does NOT plan tasks or spawn the orchestrator.
// The user is expected to manually run 'co plan' and 'co run' afterward.
//
// The beadIDs parameter can be a single bead ID or comma-delimited list of bead IDs.
func runWorkCreateWithBeads(proj *project.Project, beadIDs string, baseBranch string) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	// Parse comma-delimited bead IDs
	beadIDList := parseBeadIDs(beadIDs)
	if len(beadIDList) == 0 {
		return fmt.Errorf("no bead IDs provided")
	}

	if len(beadIDList) == 1 {
		fmt.Printf("Creating work for bead: %s\n", beadIDList[0])
	} else {
		fmt.Printf("Creating work for beads: %s\n", strings.Join(beadIDList, ", "))
	}

	// Get the beads and generate branch name
	var mainBeads []*beads.Bead
	for _, beadID := range beadIDList {
		bead, err := beads.GetBead(ctx,beadID, mainRepoPath)
		if err != nil {
			return fmt.Errorf("failed to get bead %s: %w", beadID, err)
		}
		mainBeads = append(mainBeads, bead)
	}

	branchName := generateBranchNameFromBeads(mainBeads)
	branchName, err := ensureUniqueBranchName(mainRepoPath, branchName)
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

	// Create work record in database
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	// Spawn the orchestrator for this work
	fmt.Println("\nSpawning orchestrator...")
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, worktreePath, os.Stdout); err != nil {
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

// runAutomatedWorkflow creates a work unit from beads and spawns the orchestrator.
// The workflow:
// 1. Creates work unit with worktree and branch (auto-generated from bead title(s))
// 2. Collects all beads to include (transitive dependencies or epic children)
// 3. Creates tasks with explicit dependencies: estimate -> implement -> review -> pr
// 4. Spawns orchestrator which polls for ready tasks and executes them
//
// The beadIDs parameter can be a single bead ID or comma-delimited list of bead IDs.
func runAutomatedWorkflow(proj *project.Project, beadIDs string, baseBranch string) error {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	// Parse comma-delimited bead IDs
	beadIDList := parseBeadIDs(beadIDs)
	if len(beadIDList) == 0 {
		return fmt.Errorf("no bead IDs provided")
	}

	if len(beadIDList) == 1 {
		fmt.Printf("Starting automated workflow for bead: %s\n", beadIDList[0])
	} else {
		fmt.Printf("Starting automated workflow for beads: %s\n", strings.Join(beadIDList, ", "))
	}

	// Step 1: Get the main beads and generate branch name
	var mainBeads []*beads.Bead
	for _, beadID := range beadIDList {
		bead, err := beads.GetBead(ctx,beadID, mainRepoPath)
		if err != nil {
			return fmt.Errorf("failed to get bead %s: %w", beadID, err)
		}
		mainBeads = append(mainBeads, bead)
	}

	branchName := generateBranchNameFromBeads(mainBeads)
	branchName, err := ensureUniqueBranchName(mainRepoPath, branchName)
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

	// Create work record in database
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch); err != nil {
		worktree.RemoveForce(mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	fmt.Printf("Created work: %s\n", workID)
	if workerName != "" {
		fmt.Printf("Worker: %s\n", workerName)
	}
	fmt.Printf("Worktree: %s\n", worktreePath)

	// Step 3: Collect beads
	fmt.Println("Collecting beads...")
	beadsToProcess, err := collectBeadsForMultipleIDs(ctx, beadIDList, mainRepoPath)
	if err != nil {
		return fmt.Errorf("failed to collect beads: %w", err)
	}

	if len(beadsToProcess) == 0 {
		return fmt.Errorf("no beads to process for %s", strings.Join(beadIDList, ", "))
	}

	fmt.Printf("Collected %d bead(s):\n", len(beadsToProcess))
	var collectedBeadIDs []string
	for _, b := range beadsToProcess {
		fmt.Printf("  - %s: %s\n", b.ID, b.Title)
		collectedBeadIDs = append(collectedBeadIDs, b.ID)
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
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, worktreePath, os.Stdout); err != nil {
		return fmt.Errorf("failed to spawn orchestrator: %w", err)
	}

	fmt.Println("\nWorkflow is now running in a zellij tab.")
	fmt.Println("Switch to the zellij session to monitor progress.")
	return nil
}
