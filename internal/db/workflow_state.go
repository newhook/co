package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/newhook/co/internal/db/sqlc"
)

// WorkflowState represents the state of an automated workflow.
// WorkflowID is the primary identifier; WorkID is set after StepCreateWork completes.
type WorkflowState struct {
	WorkflowID   string
	WorkID       string // Empty until work is created in StepCreateWork
	CurrentStep  int
	StepStatus   string
	StepData     string
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// workflowStateToLocal converts an sqlc.WorkflowState to local WorkflowState
func workflowStateToLocal(w *sqlc.WorkflowState) *WorkflowState {
	workID := ""
	if w.WorkID.Valid {
		workID = w.WorkID.String
	}
	return &WorkflowState{
		WorkflowID:   w.WorkflowID,
		WorkID:       workID,
		CurrentStep:  int(w.CurrentStep),
		StepStatus:   w.StepStatus,
		StepData:     w.StepData,
		ErrorMessage: w.ErrorMessage,
		CreatedAt:    w.CreatedAt,
		UpdatedAt:    w.UpdatedAt,
	}
}

// CreateWorkflowState creates a new workflow state.
// The workID parameter is optional and can be empty (will be set later via SetWorkflowWorkID).
func (db *DB) CreateWorkflowState(ctx context.Context, workflowID, workID string, currentStep int, stepStatus, stepData string) error {
	var workIDParam sql.NullString
	if workID != "" {
		workIDParam = sql.NullString{String: workID, Valid: true}
	}

	err := db.queries.CreateWorkflowState(ctx, sqlc.CreateWorkflowStateParams{
		WorkflowID:  workflowID,
		WorkID:      workIDParam,
		CurrentStep: int64(currentStep),
		StepStatus:  stepStatus,
		StepData:    stepData,
	})
	if err != nil {
		return fmt.Errorf("failed to create workflow state for workflow %s: %w", workflowID, err)
	}
	return nil
}

// GetWorkflowState retrieves the workflow state by workflow ID.
func (db *DB) GetWorkflowState(ctx context.Context, workflowID string) (*WorkflowState, error) {
	state, err := db.queries.GetWorkflowState(ctx, workflowID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow state: %w", err)
	}
	return workflowStateToLocal(&state), nil
}

// UpdateWorkflowStep updates the current step and status of a workflow.
func (db *DB) UpdateWorkflowStep(ctx context.Context, workflowID string, currentStep int, stepStatus, stepData string) error {
	rows, err := db.queries.UpdateWorkflowStep(ctx, sqlc.UpdateWorkflowStepParams{
		CurrentStep: int64(currentStep),
		StepStatus:  stepStatus,
		StepData:    stepData,
		WorkflowID:  workflowID,
	})
	if err != nil {
		return fmt.Errorf("failed to update workflow step for workflow %s: %w", workflowID, err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow state for workflow %s not found", workflowID)
	}
	return nil
}

// SetWorkflowWorkID sets the work_id for a workflow after the work is created.
func (db *DB) SetWorkflowWorkID(ctx context.Context, workflowID, workID string) error {
	rows, err := db.queries.SetWorkflowWorkID(ctx, sqlc.SetWorkflowWorkIDParams{
		WorkID:     sql.NullString{String: workID, Valid: true},
		WorkflowID: workflowID,
	})
	if err != nil {
		return fmt.Errorf("failed to set work ID for workflow %s: %w", workflowID, err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow state for workflow %s not found", workflowID)
	}
	return nil
}

// FailWorkflowStep marks the workflow step as failed with an error message.
func (db *DB) FailWorkflowStep(ctx context.Context, workflowID, errMsg string) error {
	rows, err := db.queries.FailWorkflowStep(ctx, sqlc.FailWorkflowStepParams{
		ErrorMessage: errMsg,
		WorkflowID:   workflowID,
	})
	if err != nil {
		return fmt.Errorf("failed to mark workflow step as failed for workflow %s: %w", workflowID, err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow state for workflow %s not found", workflowID)
	}
	return nil
}

// CompleteWorkflowStep marks the workflow as completed.
func (db *DB) CompleteWorkflowStep(ctx context.Context, workflowID string) error {
	rows, err := db.queries.CompleteWorkflowStep(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("failed to complete workflow for workflow %s: %w", workflowID, err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow state for workflow %s not found", workflowID)
	}
	return nil
}

// DeleteWorkflowState deletes the workflow state.
func (db *DB) DeleteWorkflowState(ctx context.Context, workflowID string) error {
	_, err := db.queries.DeleteWorkflowState(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("failed to delete workflow state for workflow %s: %w", workflowID, err)
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

// GenerateWorkflowID generates a short, unique workflow ID.
// Uses the same pattern as work IDs but with "wf-" prefix.
func (db *DB) GenerateWorkflowID(ctx context.Context, beadID string) (string, error) {
	// Start with a base length of 3 characters
	baseLength := 3
	maxAttempts := 30

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Calculate target length based on attempts
		targetLength := baseLength + (attempt / 10)
		if targetLength > 8 {
			targetLength = 8 // Cap at 8 characters
		}

		// Create content for hashing
		nonce := attempt % 10
		content := fmt.Sprintf("workflow:%s:%d:%d",
			beadID,
			time.Now().UnixNano(),
			nonce)

		// Generate SHA256 hash
		hash := sha256.Sum256([]byte(content))

		// Convert to base36 and truncate to target length
		hashStr := toBase36(hash[:])
		if len(hashStr) > targetLength {
			hashStr = hashStr[:targetLength]
		}

		// Create workflow ID with prefix
		workflowID := fmt.Sprintf("wf-%s", hashStr)

		// Check if this ID already exists
		existing, err := db.GetWorkflowState(ctx, workflowID)
		if err != nil {
			return "", fmt.Errorf("failed to check for existing workflow: %w", err)
		}
		if existing == nil {
			// ID is unique, return it
			return workflowID, nil
		}
	}

	// If we exhausted all attempts, generate a fallback ID
	return fmt.Sprintf("wf-%d", time.Now().UnixNano()/1000000), nil
}
