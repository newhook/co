package work

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/names"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/task"
	"github.com/newhook/co/internal/worktree"
)

// WorkService provides work operations with injectable dependencies.
// This enables both CLI and TUI to share the same tested core logic,
// and allows integration testing without external dependencies.
type WorkService struct {
	DB                  *db.DB
	Git                 git.Operations
	Worktree            worktree.Operations
	BeadsReader         beads.Reader
	BeadsCLI            beads.CLI
	OrchestratorManager claude.OrchestratorManager
	TaskPlanner         task.Planner
	NameGenerator       names.Generator
	Config              *project.Config
	ProjectRoot         string // Root directory of the project
	MainRepoPath        string // Path to the main repository
}

// NewWorkService creates a WorkService with production dependencies from a project.
func NewWorkService(proj *project.Project) *WorkService {
	// Compute beads directory from project config
	beadsDir := filepath.Join(proj.Root, proj.Config.Beads.Path)

	return &WorkService{
		DB:                  proj.DB,
		Git:                 git.NewOperations(),
		Worktree:            worktree.NewOperations(),
		BeadsReader:         proj.Beads,
		BeadsCLI:            beads.NewCLI(beadsDir),
		OrchestratorManager: claude.NewOrchestratorManager(proj.DB),
		TaskPlanner:         nil, // Planner needs specific initialization, set separately if needed
		NameGenerator:       names.NewGenerator(),
		Config:              proj.Config,
		ProjectRoot:         proj.Root,
		MainRepoPath:        proj.MainRepoPath(),
	}
}

// WorkServiceDeps contains all dependencies for a WorkService.
// Used for testing to inject mocks for all dependencies.
type WorkServiceDeps struct {
	DB                  *db.DB
	Git                 git.Operations
	Worktree            worktree.Operations
	BeadsReader         beads.Reader
	BeadsCLI            beads.CLI
	OrchestratorManager claude.OrchestratorManager
	TaskPlanner         task.Planner
	NameGenerator       names.Generator
	Config              *project.Config
	ProjectRoot         string
	MainRepoPath        string
}

// NewWorkServiceWithDeps creates a WorkService with explicitly provided dependencies.
// This is the preferred constructor for testing.
func NewWorkServiceWithDeps(deps WorkServiceDeps) *WorkService {
	return &WorkService{
		DB:                  deps.DB,
		Git:                 deps.Git,
		Worktree:            deps.Worktree,
		BeadsReader:         deps.BeadsReader,
		BeadsCLI:            deps.BeadsCLI,
		OrchestratorManager: deps.OrchestratorManager,
		TaskPlanner:         deps.TaskPlanner,
		NameGenerator:       deps.NameGenerator,
		Config:              deps.Config,
		ProjectRoot:         deps.ProjectRoot,
		MainRepoPath:        deps.MainRepoPath,
	}
}

// CreateWorkAsync creates a work unit asynchronously by scheduling tasks.
// This is the async work creation for the control plane architecture:
// 1. Creates work record in DB (without worktree path)
// 2. Schedules TaskTypeCreateWorktree task for the control plane
// The control plane will handle worktree creation, git push, and orchestrator spawning.
func (s *WorkService) CreateWorkAsync(ctx context.Context, branchName, baseBranch, rootIssueID string, auto bool) (*CreateWorkAsyncResult, error) {
	if baseBranch == "" {
		baseBranch = s.Config.Repo.GetBaseBranch()
	}

	// Ensure unique branch name
	var err error
	branchName, err = EnsureUniqueBranchName(ctx, s.Git, s.MainRepoPath, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to find unique branch name: %w", err)
	}

	// Generate work ID
	workID, err := s.DB.GenerateWorkID(ctx, branchName, s.Config.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate work ID: %w", err)
	}

	// Get a human-readable name for this worker
	workerName, err := s.NameGenerator.GetNextAvailableName(ctx, s.DB.DB)
	if err != nil {
		workerName = "" // Non-fatal
	}

	// Create work record in DB (without worktree path - control plane will set it)
	if err := s.DB.CreateWork(ctx, workID, workerName, "", branchName, baseBranch, rootIssueID, auto); err != nil {
		return nil, fmt.Errorf("failed to create work record: %w", err)
	}

	// Schedule the worktree creation task for the control plane
	autoStr := "false"
	if auto {
		autoStr = "true"
	}
	err = s.DB.ScheduleTaskWithRetry(ctx, workID, db.TaskTypeCreateWorktree, time.Now(), map[string]string{
		"branch":        branchName,
		"base_branch":   baseBranch,
		"root_issue_id": rootIssueID,
		"worker_name":   workerName,
		"auto":          autoStr,
	}, fmt.Sprintf("create-worktree-%s", workID), db.DefaultMaxAttempts)
	if err != nil {
		// Work record created but task scheduling failed - cleanup
		_ = s.DB.DeleteWork(ctx, workID)
		return nil, fmt.Errorf("failed to schedule worktree creation: %w", err)
	}

	return &CreateWorkAsyncResult{
		WorkID:      workID,
		WorkerName:  workerName,
		BranchName:  branchName,
		BaseBranch:  baseBranch,
		RootIssueID: rootIssueID,
	}, nil
}

// CreateWorkAsyncWithOptions creates a work unit asynchronously with the given options.
// This is similar to CreateWorkAsync but supports additional options like using an existing branch.
func (s *WorkService) CreateWorkAsyncWithOptions(ctx context.Context, opts CreateWorkAsyncOptions) (*CreateWorkAsyncResult, error) {
	baseBranch := opts.BaseBranch
	if baseBranch == "" {
		baseBranch = s.Config.Repo.GetBaseBranch()
	}

	branchName := opts.BranchName

	// For new branches, ensure unique name; for existing branches, use as-is
	if !opts.UseExistingBranch {
		var err error
		branchName, err = EnsureUniqueBranchName(ctx, s.Git, s.MainRepoPath, branchName)
		if err != nil {
			return nil, fmt.Errorf("failed to find unique branch name: %w", err)
		}
	}

	// Generate work ID
	workID, err := s.DB.GenerateWorkID(ctx, branchName, s.Config.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate work ID: %w", err)
	}

	// Get a human-readable name for this worker
	workerName, err := s.NameGenerator.GetNextAvailableName(ctx, s.DB.DB)
	if err != nil {
		workerName = "" // Non-fatal
	}

	// Create work record in DB (without worktree path - control plane will set it)
	if err := s.DB.CreateWork(ctx, workID, workerName, "", branchName, baseBranch, opts.RootIssueID, opts.Auto); err != nil {
		return nil, fmt.Errorf("failed to create work record: %w", err)
	}

	// Add beads to work_beads (done immediately, not by control plane)
	if len(opts.BeadIDs) > 0 {
		if err := s.addBeadsInternal(ctx, workID, opts.BeadIDs); err != nil {
			_ = s.DB.DeleteWork(ctx, workID)
			return nil, fmt.Errorf("failed to add beads to work: %w", err)
		}
	}

	// Schedule the worktree creation task for the control plane
	autoStr := "false"
	if opts.Auto {
		autoStr = "true"
	}
	useExistingStr := "false"
	if opts.UseExistingBranch {
		useExistingStr = "true"
	}
	err = s.DB.ScheduleTaskWithRetry(ctx, workID, db.TaskTypeCreateWorktree, time.Now(), map[string]string{
		"branch":        branchName,
		"base_branch":   baseBranch,
		"root_issue_id": opts.RootIssueID,
		"worker_name":   workerName,
		"auto":          autoStr,
		"use_existing":  useExistingStr,
	}, fmt.Sprintf("create-worktree-%s", workID), db.DefaultMaxAttempts)
	if err != nil {
		// Work record created but task scheduling failed - cleanup
		_ = s.DB.DeleteWork(ctx, workID)
		return nil, fmt.Errorf("failed to schedule worktree creation: %w", err)
	}

	return &CreateWorkAsyncResult{
		WorkID:      workID,
		WorkerName:  workerName,
		BranchName:  branchName,
		BaseBranch:  baseBranch,
		RootIssueID: opts.RootIssueID,
	}, nil
}

// addBeadsInternal adds beads to work_beads table without validation.
func (s *WorkService) addBeadsInternal(ctx context.Context, workID string, beadIDs []string) error {
	if len(beadIDs) == 0 {
		return nil
	}
	if err := s.DB.AddWorkBeads(ctx, workID, beadIDs); err != nil {
		return fmt.Errorf("failed to add beads: %w", err)
	}
	return nil
}

// AddBeads adds beads to an existing work.
// This is the core logic for adding beads that can be called from both the CLI and TUI.
// Each bead is added as its own group (no grouping).
func (s *WorkService) AddBeads(ctx context.Context, workID string, beadIDs []string) (*AddBeadsToWorkResult, error) {
	if len(beadIDs) == 0 {
		return nil, fmt.Errorf("no beads specified")
	}

	// Verify work exists
	work, err := s.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	// Check if any bead is already in a task
	for _, beadID := range beadIDs {
		inTask, err := s.DB.IsBeadInTask(ctx, workID, beadID)
		if err != nil {
			return nil, fmt.Errorf("failed to check bead %s: %w", beadID, err)
		}
		if inTask {
			return nil, fmt.Errorf("bead %s is already assigned to a task", beadID)
		}
	}

	// Add beads to work
	if err := s.DB.AddWorkBeads(ctx, workID, beadIDs); err != nil {
		return nil, fmt.Errorf("failed to add beads: %w", err)
	}

	return &AddBeadsToWorkResult{
		BeadsAdded: len(beadIDs),
	}, nil
}

// RemoveBeadsResult contains the result of removing beads from a work.
type RemoveBeadsResult struct {
	BeadsRemoved int
}

// RemoveBeads removes beads from an existing work.
// Beads that are already assigned to a task cannot be removed.
func (s *WorkService) RemoveBeads(ctx context.Context, workID string, beadIDs []string) (*RemoveBeadsResult, error) {
	if len(beadIDs) == 0 {
		return nil, fmt.Errorf("no beads specified")
	}

	// Verify work exists
	work, err := s.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	// Check if any bead is assigned to a task and remove those that aren't
	removed := 0
	for _, beadID := range beadIDs {
		inTask, err := s.DB.IsBeadInTask(ctx, workID, beadID)
		if err != nil {
			return nil, fmt.Errorf("failed to check bead %s: %w", beadID, err)
		}
		if inTask {
			return nil, fmt.Errorf("bead %s is assigned to a task and cannot be removed", beadID)
		}

		// Remove the bead
		if err := s.DB.RemoveWorkBead(ctx, workID, beadID); err != nil {
			return nil, fmt.Errorf("failed to remove bead %s: %w", beadID, err)
		}
		removed++
	}

	return &RemoveBeadsResult{
		BeadsRemoved: removed,
	}, nil
}

// RunWorkOptions contains options for running work.
type RunWorkOptions struct {
	UsePlan       bool
	ForceEstimate bool
}

// RunWork creates tasks from unassigned beads and ensures an orchestrator is running.
// This is the core logic used by both the CLI `co run` command and the TUI.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func (s *WorkService) RunWork(ctx context.Context, workID string, usePlan bool, w io.Writer) (*RunWorkResult, error) {
	return s.RunWorkWithOptions(ctx, workID, RunWorkOptions{UsePlan: usePlan}, w)
}

// RunWorkWithOptions creates tasks from unassigned beads and ensures an orchestrator is running.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func (s *WorkService) RunWorkWithOptions(ctx context.Context, workID string, opts RunWorkOptions, w io.Writer) (*RunWorkResult, error) {
	// Get work details to verify it exists
	work, err := s.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	// Check if worktree exists
	if work.WorktreePath == "" {
		return nil, fmt.Errorf("work %s has no worktree path configured", work.ID)
	}

	if !s.Worktree.ExistsPath(work.WorktreePath) {
		return nil, fmt.Errorf("work %s worktree does not exist at %s", work.ID, work.WorktreePath)
	}

	// Create tasks from unassigned work beads
	tasksCreated, err := s.createTasksFromWorkBeads(ctx, workID, opts.UsePlan, opts.ForceEstimate, w)
	if err != nil {
		return nil, fmt.Errorf("failed to create tasks: %w", err)
	}

	// Ensure orchestrator is running
	spawned, err := s.OrchestratorManager.EnsureWorkOrchestrator(ctx, workID, s.Config.Project.Name, work.WorktreePath, work.Name, w)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure orchestrator: %w", err)
	}

	return &RunWorkResult{
		WorkID:              workID,
		TasksCreated:        tasksCreated,
		OrchestratorSpawned: spawned,
	}, nil
}

// RunWorkAuto creates an estimate task and spawns the orchestrator for automated workflow.
// This mirrors the 'co run --auto' behavior: create estimate task, let orchestrator handle
// estimation and create implement tasks afterward.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func (s *WorkService) RunWorkAuto(ctx context.Context, workID string, w io.Writer) (*RunWorkAutoResult, error) {
	// Get work details to verify it exists
	work, err := s.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	// Check if worktree exists
	if work.WorktreePath == "" {
		return nil, fmt.Errorf("work %s has no worktree path configured", work.ID)
	}

	if !s.Worktree.ExistsPath(work.WorktreePath) {
		return nil, fmt.Errorf("work %s worktree does not exist at %s", work.ID, work.WorktreePath)
	}

	// Create estimate task from unassigned work beads (post-estimation will create implement tasks)
	err = s.createEstimateTaskFromWorkBeads(ctx, workID, w)
	if err != nil {
		return nil, fmt.Errorf("failed to create estimate task: %w", err)
	}

	// Ensure orchestrator is running
	spawned, err := s.OrchestratorManager.EnsureWorkOrchestrator(ctx, workID, s.Config.Project.Name, work.WorktreePath, work.Name, w)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure orchestrator: %w", err)
	}

	return &RunWorkAutoResult{
		WorkID:              workID,
		EstimateTaskCreated: true,
		OrchestratorSpawned: spawned,
	}, nil
}

// PlanWorkTasks creates tasks from unassigned beads in a work unit without spawning an orchestrator.
// If autoGroup is true, uses LLM complexity estimation to group beads into tasks.
// Otherwise, uses existing group assignments from work_beads (one task per bead or group).
// Progress messages are written to w. Pass io.Discard to suppress output.
func (s *WorkService) PlanWorkTasks(ctx context.Context, workID string, autoGroup bool, w io.Writer) (*PlanWorkTasksResult, error) {
	// Get work details to verify it exists
	work, err := s.DB.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return nil, fmt.Errorf("work %s not found", workID)
	}

	// Create tasks from unassigned work beads
	tasksCreated, err := s.createTasksFromWorkBeads(ctx, workID, autoGroup, false, w)
	if err != nil {
		return nil, fmt.Errorf("failed to create tasks: %w", err)
	}

	return &PlanWorkTasksResult{
		TasksCreated: tasksCreated,
	}, nil
}

// createEstimateTaskFromWorkBeads creates an estimate task from unassigned work beads.
// This is used in --auto mode where the full automated workflow includes estimation.
// After the estimate task completes, handlePostEstimation creates implement tasks.
func (s *WorkService) createEstimateTaskFromWorkBeads(ctx context.Context, workID string, w io.Writer) error {
	// Get unassigned beads
	unassigned, err := s.DB.GetUnassignedWorkBeads(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get unassigned beads: %w", err)
	}

	if len(unassigned) == 0 {
		return fmt.Errorf("no unassigned beads found for work %s", workID)
	}

	fmt.Fprintf(w, "\nFound %d unassigned bead(s)\n", len(unassigned))

	// Collect bead IDs
	var beadIDs []string
	for _, wb := range unassigned {
		beadIDs = append(beadIDs, wb.BeadID)
	}

	// Get next task number
	taskNum, err := s.DB.GetNextTaskNumber(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get next task number: %w", err)
	}

	// Create the estimate task
	taskID := fmt.Sprintf("%s.%d", workID, taskNum)
	if err := s.DB.CreateTask(ctx, taskID, "estimate", beadIDs, 0, workID); err != nil {
		return fmt.Errorf("failed to create estimate task: %w", err)
	}

	fmt.Fprintf(w, "  Created estimate task %s with %d bead(s)\n", taskID, len(beadIDs))
	fmt.Fprintln(w, "  Implement tasks will be created after estimation completes.")

	return nil
}

// createTasksFromWorkBeads creates tasks from unassigned beads in work_beads.
// If usePlan is true, uses LLM complexity estimation to group beads.
// Returns the number of tasks created.
func (s *WorkService) createTasksFromWorkBeads(ctx context.Context, workID string, usePlan bool, forceEstimate bool, w io.Writer) (int, error) {
	// Get unassigned beads
	unassigned, err := s.DB.GetUnassignedWorkBeads(ctx, workID)
	if err != nil {
		return 0, fmt.Errorf("failed to get unassigned beads: %w", err)
	}

	if len(unassigned) == 0 {
		return 0, nil
	}

	fmt.Fprintf(w, "\nFound %d unassigned bead(s)\n", len(unassigned))

	// Collect bead IDs from unassigned work_beads
	beadIDs := make([]string, len(unassigned))
	for i, wb := range unassigned {
		beadIDs[i] = wb.BeadID
	}

	// Get all issues with dependencies in one call
	issuesResult, err := s.BeadsReader.GetBeadsWithDeps(ctx, beadIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to get bead details: %w", err)
	}

	// Verify all beads were found
	for _, beadID := range beadIDs {
		if _, found := issuesResult.Beads[beadID]; !found {
			return 0, fmt.Errorf("bead %s not found", beadID)
		}
	}

	// Group beads into tasks
	var taskGroups [][]string // Each inner slice is a group of bead IDs for one task

	if usePlan {
		// Use LLM complexity estimation to group beads
		fmt.Fprintln(w, "Using LLM complexity estimation to group beads...")
		taskGroups, err = s.planBeadsWithComplexity(ctx, issuesResult, workID, forceEstimate)
		if err != nil {
			return 0, fmt.Errorf("failed to plan beads: %w", err)
		}
	} else {
		// Each bead becomes its own task
		for _, wb := range unassigned {
			taskGroups = append(taskGroups, []string{wb.BeadID})
		}
	}

	// Create tasks from groups
	tasksCreated := 0
	for _, groupBeadIDs := range taskGroups {
		if len(groupBeadIDs) == 0 {
			continue
		}

		// Get next task number
		taskNum, err := s.DB.GetNextTaskNumber(ctx, workID)
		if err != nil {
			return tasksCreated, fmt.Errorf("failed to get next task number: %w", err)
		}

		taskID := fmt.Sprintf("%s.%d", workID, taskNum)
		if err := s.DB.CreateTask(ctx, taskID, "implement", groupBeadIDs, 0, workID); err != nil {
			return tasksCreated, fmt.Errorf("failed to create task: %w", err)
		}

		fmt.Fprintf(w, "  Created task %s with %d bead(s)\n", taskID, len(groupBeadIDs))
		tasksCreated++
	}

	return tasksCreated, nil
}

// planBeadsWithComplexity uses LLM complexity estimation to group beads.
// If forceEstimate is true, re-estimates complexity even if cached values exist.
// If s.TaskPlanner is set, uses it directly; otherwise creates a default planner.
func (s *WorkService) planBeadsWithComplexity(ctx context.Context, issuesResult *beads.BeadsWithDepsResult, workID string, forceEstimate bool) ([][]string, error) {
	// Convert map to slice of beads
	beadList := make([]beads.Bead, 0, len(issuesResult.Beads))
	for _, b := range issuesResult.Beads {
		beadList = append(beadList, b)
	}

	// If a task planner is configured, use it directly
	if s.TaskPlanner != nil {
		// Plan tasks using token budget of 120K (context window is 200K, leave headroom)
		const tokenBudget = 120000
		planned, err := s.TaskPlanner.Plan(ctx, beadList, issuesResult.Dependencies, tokenBudget)
		if err != nil {
			return nil, fmt.Errorf("failed to plan tasks: %w", err)
		}

		// Convert planned tasks to bead ID groups
		var groups [][]string
		for _, p := range planned {
			groups = append(groups, p.BeadIDs)
		}
		return groups, nil
	}

	// Fall back to creating LLM estimator and planner inline
	estimator := task.NewLLMEstimator(s.DB, s.MainRepoPath, s.Config.Project.Name, workID)
	planner := task.NewDefaultPlanner(estimator)

	// Estimate complexity for each bead
	result, err := estimator.EstimateBatch(ctx, beadList, forceEstimate)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate complexity: %w", err)
	}

	// If a task was spawned for estimation, we need to wait for it
	if result.TaskSpawned {
		return nil, fmt.Errorf("estimation task %s spawned - re-run after it completes", result.TaskID)
	}

	// Plan tasks using token budget of 120K (context window is 200K, leave headroom)
	const tokenBudget = 120000
	planned, err := planner.Plan(ctx, beadList, issuesResult.Dependencies, tokenBudget)
	if err != nil {
		return nil, fmt.Errorf("failed to plan tasks: %w", err)
	}

	// Convert planned tasks to bead ID groups
	var groups [][]string
	for _, p := range planned {
		groups = append(groups, p.BeadIDs)
	}

	return groups, nil
}

// DestroyWork destroys a work unit and all its resources.
// This is the core work destruction logic that can be called from both the CLI and TUI.
// It does not perform interactive confirmation - that should be handled by the caller.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func (s *WorkService) DestroyWork(ctx context.Context, workID string, w io.Writer) error {
	// Get work to verify it exists
	work, err := s.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found", workID)
	}

	// Close the root issue if it exists
	if work.RootIssueID != "" {
		fmt.Fprintf(w, "Closing root issue %s...\n", work.RootIssueID)
		if err := s.BeadsCLI.Close(ctx, work.RootIssueID); err != nil {
			// Warn but continue - issue might already be closed or deleted
			fmt.Fprintf(w, "Warning: failed to close root issue %s: %v\n", work.RootIssueID, err)
		}
	}

	// Terminate any running zellij tabs (orchestrator, task, console, and claude tabs) for this work
	// Only if configured to do so (defaults to true)
	if s.Config.Zellij.ShouldKillTabsOnDestroy() {
		if err := s.OrchestratorManager.TerminateWorkTabs(ctx, workID, s.Config.Project.Name, w); err != nil {
			// Warn but continue - tab termination is non-fatal
			fmt.Fprintf(w, "Warning: failed to terminate work tabs: %v\n", err)
		}
	}

	// Remove git worktree if it exists
	if work.WorktreePath != "" {
		if err := s.Worktree.RemoveForce(ctx, s.MainRepoPath, work.WorktreePath); err != nil {
			fmt.Fprintf(w, "Warning: failed to remove worktree: %v\n", err)
		}
	}

	// Remove work directory
	workDir := filepath.Join(s.ProjectRoot, workID)
	if err := os.RemoveAll(workDir); err != nil {
		fmt.Fprintf(w, "Warning: failed to remove work directory %s: %v\n", workDir, err)
	}

	// Delete work from database (also deletes associated tasks and relationships)
	if err := s.DB.DeleteWork(ctx, workID); err != nil {
		return fmt.Errorf("failed to delete work from database: %w", err)
	}

	return nil
}
