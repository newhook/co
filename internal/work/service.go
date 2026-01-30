package work

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
