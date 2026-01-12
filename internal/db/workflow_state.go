package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/newhook/co/internal/db/sqlc"
)

// WorkflowState represents the state of an automated workflow for a work unit.
type WorkflowState struct {
	WorkID       string
	CurrentStep  int
	StepStatus   string
	StepData     string
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// workflowStateToLocal converts an sqlc.WorkflowState to local WorkflowState
func workflowStateToLocal(w *sqlc.WorkflowState) *WorkflowState {
	return &WorkflowState{
		WorkID:       w.WorkID,
		CurrentStep:  int(w.CurrentStep),
		StepStatus:   w.StepStatus,
		StepData:     w.StepData,
		ErrorMessage: w.ErrorMessage,
		CreatedAt:    w.CreatedAt,
		UpdatedAt:    w.UpdatedAt,
	}
}

// CreateWorkflowState creates a new workflow state for a work unit.
func (db *DB) CreateWorkflowState(ctx context.Context, workID string, currentStep int, stepStatus, stepData string) error {
	err := db.queries.CreateWorkflowState(ctx, sqlc.CreateWorkflowStateParams{
		WorkID:      workID,
		CurrentStep: int64(currentStep),
		StepStatus:  stepStatus,
		StepData:    stepData,
	})
	if err != nil {
		return fmt.Errorf("failed to create workflow state for work %s: %w", workID, err)
	}
	return nil
}

// GetWorkflowState retrieves the workflow state for a work unit.
func (db *DB) GetWorkflowState(ctx context.Context, workID string) (*WorkflowState, error) {
	state, err := db.queries.GetWorkflowState(ctx, workID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow state: %w", err)
	}
	return workflowStateToLocal(&state), nil
}

// UpdateWorkflowStep updates the current step and status of a workflow.
func (db *DB) UpdateWorkflowStep(ctx context.Context, workID string, currentStep int, stepStatus, stepData string) error {
	rows, err := db.queries.UpdateWorkflowStep(ctx, sqlc.UpdateWorkflowStepParams{
		CurrentStep: int64(currentStep),
		StepStatus:  stepStatus,
		StepData:    stepData,
		WorkID:      workID,
	})
	if err != nil {
		return fmt.Errorf("failed to update workflow step for work %s: %w", workID, err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow state for work %s not found", workID)
	}
	return nil
}

// FailWorkflowStep marks the workflow step as failed with an error message.
func (db *DB) FailWorkflowStep(ctx context.Context, workID, errMsg string) error {
	rows, err := db.queries.FailWorkflowStep(ctx, sqlc.FailWorkflowStepParams{
		ErrorMessage: errMsg,
		WorkID:       workID,
	})
	if err != nil {
		return fmt.Errorf("failed to mark workflow step as failed for work %s: %w", workID, err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow state for work %s not found", workID)
	}
	return nil
}

// CompleteWorkflowStep marks the workflow as completed.
func (db *DB) CompleteWorkflowStep(ctx context.Context, workID string) error {
	rows, err := db.queries.CompleteWorkflowStep(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to complete workflow for work %s: %w", workID, err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow state for work %s not found", workID)
	}
	return nil
}

// DeleteWorkflowState deletes the workflow state for a work unit.
func (db *DB) DeleteWorkflowState(ctx context.Context, workID string) error {
	_, err := db.queries.DeleteWorkflowState(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to delete workflow state for work %s: %w", workID, err)
	}
	return nil
}

// ListPendingWorkflows returns all workflows that are pending or processing.
func (db *DB) ListPendingWorkflows(ctx context.Context) ([]*WorkflowState, error) {
	states, err := db.queries.ListPendingWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending workflows: %w", err)
	}

	result := make([]*WorkflowState, len(states))
	for i, s := range states {
		result[i] = workflowStateToLocal(&s)
	}
	return result, nil
}
