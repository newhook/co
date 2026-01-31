//go:generate moq -stub -out control_mock_test.go -pkg control_test . OrchestratorSpawner WorkDestroyer

package control

import (
	"context"
	"io"

	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/feedback"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/work"
	"github.com/newhook/co/internal/worktree"
	"github.com/newhook/co/internal/zellij"
)

// OrchestratorSpawner defines the interface for spawning work orchestrators.
// This abstraction enables testing without actual zellij operations.
type OrchestratorSpawner interface {
	SpawnWorkOrchestrator(ctx context.Context, workID, projectName, workDir, friendlyName string, w io.Writer) error
}

// WorkDestroyer defines the interface for destroying work units.
// This abstraction enables testing without actual file system operations.
type WorkDestroyer interface {
	DestroyWork(ctx context.Context, proj *project.Project, workID string, w io.Writer) error
}

// DefaultOrchestratorSpawner implements OrchestratorSpawner using the claude package.
type DefaultOrchestratorSpawner struct{}

// SpawnWorkOrchestrator implements OrchestratorSpawner.
func (d *DefaultOrchestratorSpawner) SpawnWorkOrchestrator(ctx context.Context, workID, projectName, workDir, friendlyName string, w io.Writer) error {
	return claude.SpawnWorkOrchestrator(ctx, workID, projectName, workDir, friendlyName, w)
}

// DefaultWorkDestroyer implements WorkDestroyer using the work package.
type DefaultWorkDestroyer struct{}

// DestroyWork implements WorkDestroyer.
func (d *DefaultWorkDestroyer) DestroyWork(ctx context.Context, proj *project.Project, workID string, w io.Writer) error {
	return work.DestroyWork(ctx, proj, workID, w)
}

// ControlPlane manages the execution of scheduled tasks with injectable dependencies.
// It allows for testing without actual CLI tools, services, or file system operations.
type ControlPlane struct {
	Git                 git.Operations
	Worktree            worktree.Operations
	Zellij              zellij.SessionManager
	Mise                func(dir string) mise.Operations
	FeedbackProcessor   feedback.Processor
	OrchestratorSpawner OrchestratorSpawner
	WorkDestroyer       WorkDestroyer
}

// NewControlPlane creates a new ControlPlane with default production dependencies.
func NewControlPlane() *ControlPlane {
	return &ControlPlane{
		Git:                 git.NewOperations(),
		Worktree:            worktree.NewOperations(),
		Zellij:              zellij.New(),
		Mise:                mise.NewOperations,
		FeedbackProcessor:   feedback.NewProcessor(),
		OrchestratorSpawner: &DefaultOrchestratorSpawner{},
		WorkDestroyer:       &DefaultWorkDestroyer{},
	}
}

// NewControlPlaneWithDeps creates a new ControlPlane with provided dependencies for testing.
func NewControlPlaneWithDeps(
	gitOps git.Operations,
	wtOps worktree.Operations,
	zellijMgr zellij.SessionManager,
	miseOps func(dir string) mise.Operations,
	feedbackProc feedback.Processor,
	orchestratorSpawner OrchestratorSpawner,
	workDestroyer WorkDestroyer,
) *ControlPlane {
	return &ControlPlane{
		Git:                 gitOps,
		Worktree:            wtOps,
		Zellij:              zellijMgr,
		Mise:                miseOps,
		FeedbackProcessor:   feedbackProc,
		OrchestratorSpawner: orchestratorSpawner,
		WorkDestroyer:       workDestroyer,
	}
}

// GetTaskHandlers returns the task handler map for the control plane.
func (cp *ControlPlane) GetTaskHandlers() map[string]TaskHandler {
	return map[string]TaskHandler{
		db.TaskTypeCreateWorktree:      cp.HandleCreateWorktreeTask,
		db.TaskTypeSpawnOrchestrator:   cp.HandleSpawnOrchestratorTask,
		db.TaskTypePRFeedback:          cp.HandlePRFeedbackTask,
		db.TaskTypeGitPush:             cp.HandleGitPushTask,
		db.TaskTypeDestroyWorktree:     cp.HandleDestroyWorktreeTask,
		db.TaskTypeWatchWorkflowRun:    cp.HandleWatchWorkflowRunTask,
		// These handlers don't need ControlPlane dependencies - keep as standalone functions
		db.TaskTypeImportPR:            HandleImportPRTask,
		db.TaskTypeCommentResolution:   HandleCommentResolutionTask,
		db.TaskTypeGitHubComment:       HandleGitHubCommentTask,
		db.TaskTypeGitHubResolveThread: HandleGitHubResolveThreadTask,
	}
}
