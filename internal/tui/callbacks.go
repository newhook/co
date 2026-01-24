package tui

import (
	"context"
	"io"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/progress"
	"github.com/newhook/co/internal/project"
)

// Re-export types from internal/progress for use in TUI.
// This avoids circular imports while keeping TUI code unchanged.
type (
	WorkProgress = progress.WorkProgress
	TaskProgress = progress.TaskProgress
	BeadProgress = progress.BeadProgress
)

// CreateWorkAsyncResult holds the result of async work creation.
type CreateWorkAsyncResult struct {
	WorkID string
}

// AddBeadsToWorkResult holds the result of adding beads to work.
type AddBeadsToWorkResult struct {
	BeadsAdded int
}

// RunWorkResult holds the result of running work.
type RunWorkResult struct {
	TasksCreated int
}

// RunWorkAutoResult holds the result of running work in auto mode.
type RunWorkAutoResult struct {
	WorkID              string
	EstimateTaskCreated bool
	OrchestratorSpawned bool
}

// Callbacks defines the interface for TUI to call back into cmd.
// This breaks the circular dependency between tui and cmd packages.
type Callbacks interface {
	// Work operations
	CollectIssueIDsForAutomatedWorkflow(ctx context.Context, beadID string, beadsClient *beads.Client) ([]string, error)
	CreateWorkAsync(ctx context.Context, proj *project.Project, branchName, baseBranch, rootIssueID string, auto bool) (*CreateWorkAsyncResult, error)
	AddBeadsToWork(ctx context.Context, proj *project.Project, workID string, beadIDs []string) (*AddBeadsToWorkResult, error)
	AddBeadsToWorkInternal(ctx context.Context, proj *project.Project, workID string, beadIDs []string) error

	// Control plane operations
	EnsureControlPlane(ctx context.Context, projectName string, projectRoot string, w io.Writer) (bool, error)
	ScheduleDestroyWorktree(ctx context.Context, proj *project.Project, workID string) error

	// Run operations
	RunWork(ctx context.Context, proj *project.Project, workID string, usePlan bool, w io.Writer) (*RunWorkResult, error)
	RunWorkAuto(ctx context.Context, proj *project.Project, workID string, w io.Writer) (*RunWorkAutoResult, error)

	// Poll operations
	FetchAllWorksPollData(ctx context.Context, proj *project.Project) ([]*WorkProgress, error)

	// Feedback operations
	TriggerPRFeedbackCheck(ctx context.Context, proj *project.Project, workID string) error
}

// callbacks holds the registered callbacks from cmd
var callbacks Callbacks

// SetCallbacks registers the callbacks from cmd.
// This must be called before RunRootTUI.
func SetCallbacks(cb Callbacks) {
	callbacks = cb
}

// getCallbacks returns the registered callbacks.
// Panics if SetCallbacks was not called.
func getCallbacks() Callbacks {
	if callbacks == nil {
		panic("tui: SetCallbacks must be called before using TUI")
	}
	return callbacks
}

// Helper functions that delegate to the callbacks

func collectIssueIDsForAutomatedWorkflow(ctx context.Context, beadID string, beadsClient *beads.Client) ([]string, error) {
	return getCallbacks().CollectIssueIDsForAutomatedWorkflow(ctx, beadID, beadsClient)
}

func CreateWorkAsync(ctx context.Context, proj *project.Project, branchName, baseBranch, rootIssueID string, auto bool) (*CreateWorkAsyncResult, error) {
	return getCallbacks().CreateWorkAsync(ctx, proj, branchName, baseBranch, rootIssueID, auto)
}

func AddBeadsToWork(ctx context.Context, proj *project.Project, workID string, beadIDs []string) (*AddBeadsToWorkResult, error) {
	return getCallbacks().AddBeadsToWork(ctx, proj, workID, beadIDs)
}

func addBeadsToWork(ctx context.Context, proj *project.Project, workID string, beadIDs []string) error {
	return getCallbacks().AddBeadsToWorkInternal(ctx, proj, workID, beadIDs)
}

func EnsureControlPlane(ctx context.Context, projectName string, projectRoot string, w io.Writer) (bool, error) {
	return getCallbacks().EnsureControlPlane(ctx, projectName, projectRoot, w)
}

func ScheduleDestroyWorktree(ctx context.Context, proj *project.Project, workID string) error {
	return getCallbacks().ScheduleDestroyWorktree(ctx, proj, workID)
}

func RunWork(ctx context.Context, proj *project.Project, workID string, usePlan bool, w io.Writer) (*RunWorkResult, error) {
	return getCallbacks().RunWork(ctx, proj, workID, usePlan, w)
}

func RunWorkAuto(ctx context.Context, proj *project.Project, workID string, w io.Writer) (*RunWorkAutoResult, error) {
	return getCallbacks().RunWorkAuto(ctx, proj, workID, w)
}

func fetchAllWorksPollData(ctx context.Context, proj *project.Project) ([]*WorkProgress, error) {
	return getCallbacks().FetchAllWorksPollData(ctx, proj)
}

func TriggerPRFeedbackCheck(ctx context.Context, proj *project.Project, workID string) error {
	return getCallbacks().TriggerPRFeedbackCheck(ctx, proj, workID)
}
