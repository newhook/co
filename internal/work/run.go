package work

import (
	"context"
	"io"

	"github.com/newhook/co/internal/project"
)

// RunWorkResult contains the result of running work.
type RunWorkResult struct {
	WorkID              string
	TasksCreated        int
	OrchestratorSpawned bool
}

// RunWork creates tasks from unassigned beads and ensures an orchestrator is running.
// This is the core logic used by both the CLI `co run` command and the TUI.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
//
// Deprecated: Use WorkService.RunWork instead. This wrapper exists for backward compatibility.
func RunWork(ctx context.Context, proj *project.Project, workID string, usePlan bool, w io.Writer) (*RunWorkResult, error) {
	svc := NewWorkService(proj)
	return svc.RunWork(ctx, workID, usePlan, w)
}

// RunWorkWithOptions creates tasks from unassigned beads and ensures an orchestrator is running.
// If forceEstimate is true, re-estimates complexity even if cached values exist.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
//
// Deprecated: Use WorkService.RunWorkWithOptions instead. This wrapper exists for backward compatibility.
func RunWorkWithOptions(ctx context.Context, proj *project.Project, workID string, usePlan bool, forceEstimate bool, w io.Writer) (*RunWorkResult, error) {
	svc := NewWorkService(proj)
	return svc.RunWorkWithOptions(ctx, workID, RunWorkOptions{UsePlan: usePlan, ForceEstimate: forceEstimate}, w)
}

// RunWorkAutoResult contains the result of running work in auto mode.
type RunWorkAutoResult struct {
	WorkID              string
	EstimateTaskCreated bool
	OrchestratorSpawned bool
}

// RunWorkAuto creates an estimate task and spawns the orchestrator for automated workflow.
// This mirrors the 'co run --auto' behavior: create estimate task, let orchestrator handle
// estimation and create implement tasks afterward.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
//
// Deprecated: Use WorkService.RunWorkAuto instead. This wrapper exists for backward compatibility.
func RunWorkAuto(ctx context.Context, proj *project.Project, workID string, w io.Writer) (*RunWorkAutoResult, error) {
	svc := NewWorkService(proj)
	return svc.RunWorkAuto(ctx, workID, w)
}

// PlanWorkTasksResult contains the result of planning work tasks.
type PlanWorkTasksResult struct {
	TasksCreated int
}

// PlanWorkTasks creates tasks from unassigned beads in a work unit without spawning an orchestrator.
// If autoGroup is true, uses LLM complexity estimation to group beads into tasks.
// Otherwise, uses existing group assignments from work_beads (one task per bead or group).
// Progress messages are written to w. Pass io.Discard to suppress output.
//
// Deprecated: Use WorkService.PlanWorkTasks instead. This wrapper exists for backward compatibility.
func PlanWorkTasks(ctx context.Context, proj *project.Project, workID string, autoGroup bool, w io.Writer) (*PlanWorkTasksResult, error) {
	svc := NewWorkService(proj)
	return svc.PlanWorkTasks(ctx, workID, autoGroup, w)
}

// CreateEstimateTaskFromWorkBeads creates an estimate task from unassigned work beads.
// This is used in --auto mode where the full automated workflow includes estimation.
// After the estimate task completes, handlePostEstimation creates implement tasks.
// Progress messages are written to w. Pass io.Discard to suppress output.
//
// Deprecated: Use WorkService.CreateEstimateTaskFromWorkBeads instead. This wrapper exists for backward compatibility.
func CreateEstimateTaskFromWorkBeads(ctx context.Context, proj *project.Project, workID, _ string, w io.Writer) error {
	svc := NewWorkService(proj)
	return svc.CreateEstimateTaskFromWorkBeads(ctx, workID, w)
}
