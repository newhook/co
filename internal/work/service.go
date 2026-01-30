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
