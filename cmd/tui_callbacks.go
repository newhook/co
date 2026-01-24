package cmd

import (
	"context"
	"io"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/tui"
)

// tuiCallbacks implements tui.Callbacks by delegating to cmd functions.
type tuiCallbacks struct{}

func newTUICallbacks() *tuiCallbacks {
	return &tuiCallbacks{}
}

// Work operations

func (c *tuiCallbacks) CollectIssueIDsForAutomatedWorkflow(ctx context.Context, beadID string, beadsClient *beads.Client) ([]string, error) {
	return collectIssueIDsForAutomatedWorkflow(ctx, beadID, beadsClient)
}

func (c *tuiCallbacks) CreateWorkAsync(ctx context.Context, proj *project.Project, branchName, baseBranch, rootIssueID string, auto bool) (*tui.CreateWorkAsyncResult, error) {
	result, err := CreateWorkAsync(ctx, proj, branchName, baseBranch, rootIssueID, auto)
	if err != nil {
		return nil, err
	}
	return &tui.CreateWorkAsyncResult{WorkID: result.WorkID}, nil
}

func (c *tuiCallbacks) AddBeadsToWork(ctx context.Context, proj *project.Project, workID string, beadIDs []string) (*tui.AddBeadsToWorkResult, error) {
	result, err := AddBeadsToWork(ctx, proj, workID, beadIDs)
	if err != nil {
		return nil, err
	}
	return &tui.AddBeadsToWorkResult{BeadsAdded: result.BeadsAdded}, nil
}

func (c *tuiCallbacks) AddBeadsToWorkInternal(ctx context.Context, proj *project.Project, workID string, beadIDs []string) error {
	return addBeadsToWork(ctx, proj, workID, beadIDs)
}

// Control plane operations

func (c *tuiCallbacks) EnsureControlPlane(ctx context.Context, projectName string, projectRoot string, w io.Writer) (bool, error) {
	return EnsureControlPlane(ctx, projectName, projectRoot, w)
}

func (c *tuiCallbacks) ScheduleDestroyWorktree(ctx context.Context, proj *project.Project, workID string) error {
	return ScheduleDestroyWorktree(ctx, proj, workID)
}

// Run operations

func (c *tuiCallbacks) RunWork(ctx context.Context, proj *project.Project, workID string, usePlan bool, w io.Writer) (*tui.RunWorkResult, error) {
	result, err := RunWork(ctx, proj, workID, usePlan, w)
	if err != nil {
		return nil, err
	}
	return &tui.RunWorkResult{TasksCreated: result.TasksCreated}, nil
}

func (c *tuiCallbacks) RunWorkAuto(ctx context.Context, proj *project.Project, workID string, w io.Writer) (*tui.RunWorkAutoResult, error) {
	result, err := RunWorkAuto(ctx, proj, workID, w)
	if err != nil {
		return nil, err
	}
	return &tui.RunWorkAutoResult{
		WorkID:              result.WorkID,
		EstimateTaskCreated: result.EstimateTaskCreated,
		OrchestratorSpawned: result.OrchestratorSpawned,
	}, nil
}

// Poll operations

func (c *tuiCallbacks) FetchAllWorksPollData(ctx context.Context, proj *project.Project) ([]*tui.WorkProgress, error) {
	works, err := fetchAllWorksPollData(ctx, proj)
	if err != nil {
		return nil, err
	}

	// Convert cmd.workProgress to tui.WorkProgress
	result := make([]*tui.WorkProgress, len(works))
	for i, w := range works {
		result[i] = convertWorkProgress(w)
	}
	return result, nil
}

// Feedback operations

func (c *tuiCallbacks) TriggerPRFeedbackCheck(ctx context.Context, proj *project.Project, workID string) error {
	return TriggerPRFeedbackCheck(ctx, proj, workID)
}

// Helper function to convert cmd.workProgress to tui.WorkProgress
func convertWorkProgress(w *workProgress) *tui.WorkProgress {
	if w == nil {
		return nil
	}

	// Convert tasks
	tasks := make([]*tui.TaskProgress, len(w.tasks))
	for i, t := range w.tasks {
		tasks[i] = convertTaskProgress(t)
	}

	// Convert beads
	workBeads := make([]tui.BeadProgress, len(w.workBeads))
	for i, b := range w.workBeads {
		workBeads[i] = convertBeadProgress(b)
	}

	unassignedBeads := make([]tui.BeadProgress, len(w.unassignedBeads))
	for i, b := range w.unassignedBeads {
		unassignedBeads[i] = convertBeadProgress(b)
	}

	return &tui.WorkProgress{
		Work:                w.work,
		Tasks:               tasks,
		WorkBeads:           workBeads,
		UnassignedBeads:     unassignedBeads,
		UnassignedBeadCount: w.unassignedBeadCount,
		FeedbackCount:       w.feedbackCount,
		FeedbackBeadIDs:     w.feedbackBeadIDs,
		CIStatus:            w.ciStatus,
		ApprovalStatus:      w.approvalStatus,
		Approvers:           w.approvers,
		HasUnseenPRChanges:  w.hasUnseenPRChanges,
	}
}

func convertTaskProgress(t *taskProgress) *tui.TaskProgress {
	if t == nil {
		return nil
	}

	beads := make([]tui.BeadProgress, len(t.beads))
	for i, b := range t.beads {
		beads[i] = convertBeadProgress(b)
	}

	return &tui.TaskProgress{
		Task:  t.task,
		Beads: beads,
	}
}

func convertBeadProgress(b beadProgress) tui.BeadProgress {
	return tui.BeadProgress{
		ID:          b.id,
		Status:      b.status,
		Title:       b.title,
		Description: b.description,
		BeadStatus:  b.beadStatus,
		Priority:    b.priority,
		IssueType:   b.issueType,
	}
}
