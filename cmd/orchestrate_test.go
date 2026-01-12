package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/newhook/co/internal/db"
)

// setupTestDB creates an in-memory database for testing
func setupTestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()

	database, err := db.OpenPath(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	cleanup := func() {
		database.Close()
	}

	return database, cleanup
}

func TestStepDataSerialization(t *testing.T) {
	// Test that StepData can be properly serialized and deserialized
	original := StepData{
		BeadID:      "test-bead-123",
		BaseBranch:  "main",
		WorkID:      "w-abc",
		BranchName:  "feat/test-feature",
		BeadIDs:     []string{"bead-1", "bead-2", "bead-3"},
		ReviewCount: 2,
	}

	// Serialize
	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal StepData: %v", err)
	}

	// Deserialize
	var restored StepData
	if err := json.Unmarshal(jsonBytes, &restored); err != nil {
		t.Fatalf("failed to unmarshal StepData: %v", err)
	}

	// Verify
	if restored.BeadID != original.BeadID {
		t.Errorf("BeadID mismatch: got %q, want %q", restored.BeadID, original.BeadID)
	}
	if restored.BaseBranch != original.BaseBranch {
		t.Errorf("BaseBranch mismatch: got %q, want %q", restored.BaseBranch, original.BaseBranch)
	}
	if restored.WorkID != original.WorkID {
		t.Errorf("WorkID mismatch: got %q, want %q", restored.WorkID, original.WorkID)
	}
	if restored.BranchName != original.BranchName {
		t.Errorf("BranchName mismatch: got %q, want %q", restored.BranchName, original.BranchName)
	}
	if len(restored.BeadIDs) != len(original.BeadIDs) {
		t.Errorf("BeadIDs length mismatch: got %d, want %d", len(restored.BeadIDs), len(original.BeadIDs))
	}
	if restored.ReviewCount != original.ReviewCount {
		t.Errorf("ReviewCount mismatch: got %d, want %d", restored.ReviewCount, original.ReviewCount)
	}
}

func TestStepConstants(t *testing.T) {
	// Test that step constants are correctly ordered
	steps := []int{
		StepCreateWork,
		StepCollectBeads,
		StepPlanTasks,
		StepExecuteTasks,
		StepWaitCompletion,
		StepReviewFix,
		StepCreatePR,
		StepCompleted,
	}

	for i := 0; i < len(steps)-1; i++ {
		if steps[i] >= steps[i+1] {
			t.Errorf("steps not in order: %d >= %d", steps[i], steps[i+1])
		}
	}
}

func TestStepName(t *testing.T) {
	tests := []struct {
		step int
		name string
	}{
		{StepCreateWork, "Create Work"},
		{StepCollectBeads, "Collect Beads"},
		{StepPlanTasks, "Plan Tasks"},
		{StepExecuteTasks, "Execute Tasks"},
		{StepWaitCompletion, "Wait for Completion"},
		{StepReviewFix, "Review-Fix Loop"},
		{StepCreatePR, "Create PR"},
		{StepCompleted, "Completed"},
		{99, "Unknown Step 99"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stepName(tt.step)
			if got != tt.name {
				t.Errorf("stepName(%d) = %q, want %q", tt.step, got, tt.name)
			}
		})
	}
}

func TestWorkflowStateDatabase(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Workflow ID is separate from work ID
	workflowID := "workflow-test123"
	workID := "w-test123"

	// Create a work record (optional, for when work_id is set)
	err := database.CreateWork(ctx, workID, "/tmp/worktree", "feat/test", "main")
	if err != nil {
		t.Fatalf("CreateWork failed: %v", err)
	}

	stepData := StepData{
		BeadID:     "test-bead",
		BaseBranch: "main",
	}
	stepDataJSON, _ := json.Marshal(stepData)

	// Test CreateWorkflowState (initially without work_id)
	err = database.CreateWorkflowState(ctx, workflowID, "", StepCreateWork, "pending", string(stepDataJSON))
	if err != nil {
		t.Fatalf("CreateWorkflowState failed: %v", err)
	}

	// Test GetWorkflowState
	state, err := database.GetWorkflowState(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetWorkflowState failed: %v", err)
	}
	if state == nil {
		t.Fatal("GetWorkflowState returned nil")
	}
	if state.WorkflowID != workflowID {
		t.Errorf("WorkflowID mismatch: got %q, want %q", state.WorkflowID, workflowID)
	}
	if state.WorkID != "" {
		t.Errorf("WorkID should be empty initially, got %q", state.WorkID)
	}
	if state.CurrentStep != StepCreateWork {
		t.Errorf("CurrentStep mismatch: got %d, want %d", state.CurrentStep, StepCreateWork)
	}
	if state.StepStatus != "pending" {
		t.Errorf("StepStatus mismatch: got %q, want %q", state.StepStatus, "pending")
	}

	// Test SetWorkflowWorkID (links workflow to work after StepCreateWork)
	err = database.SetWorkflowWorkID(ctx, workflowID, workID)
	if err != nil {
		t.Fatalf("SetWorkflowWorkID failed: %v", err)
	}

	state, err = database.GetWorkflowState(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetWorkflowState after SetWorkflowWorkID failed: %v", err)
	}
	if state.WorkID != workID {
		t.Errorf("WorkID mismatch after SetWorkflowWorkID: got %q, want %q", state.WorkID, workID)
	}

	// Test UpdateWorkflowStep
	newStepData := StepData{
		BeadID:     "test-bead",
		BaseBranch: "main",
		WorkID:     workID,
		BranchName: "feat/test",
	}
	newStepDataJSON, _ := json.Marshal(newStepData)
	err = database.UpdateWorkflowStep(ctx, workflowID, StepCollectBeads, "processing", string(newStepDataJSON))
	if err != nil {
		t.Fatalf("UpdateWorkflowStep failed: %v", err)
	}

	state, err = database.GetWorkflowState(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetWorkflowState after update failed: %v", err)
	}
	if state.CurrentStep != StepCollectBeads {
		t.Errorf("CurrentStep after update: got %d, want %d", state.CurrentStep, StepCollectBeads)
	}
	if state.StepStatus != "processing" {
		t.Errorf("StepStatus after update: got %q, want %q", state.StepStatus, "processing")
	}

	// Test FailWorkflowStep
	errMsg := "test error message"
	err = database.FailWorkflowStep(ctx, workflowID, errMsg)
	if err != nil {
		t.Fatalf("FailWorkflowStep failed: %v", err)
	}

	state, err = database.GetWorkflowState(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetWorkflowState after fail failed: %v", err)
	}
	if state.StepStatus != "failed" {
		t.Errorf("StepStatus after fail: got %q, want %q", state.StepStatus, "failed")
	}
	if state.ErrorMessage != errMsg {
		t.Errorf("ErrorMessage: got %q, want %q", state.ErrorMessage, errMsg)
	}

	// Reset to test CompleteWorkflowStep
	database.UpdateWorkflowStep(ctx, workflowID, StepCreatePR, "processing", "{}")

	err = database.CompleteWorkflowStep(ctx, workflowID)
	if err != nil {
		t.Fatalf("CompleteWorkflowStep failed: %v", err)
	}

	state, err = database.GetWorkflowState(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetWorkflowState after complete failed: %v", err)
	}
	if state.StepStatus != "completed" {
		t.Errorf("StepStatus after complete: got %q, want %q", state.StepStatus, "completed")
	}
}

func TestWorkflowStateNotFound(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Test GetWorkflowState for non-existent workflow
	state, err := database.GetWorkflowState(ctx, "nonexistent-workflow")
	if err != nil {
		t.Fatalf("GetWorkflowState for nonexistent should not error: %v", err)
	}
	if state != nil {
		t.Error("GetWorkflowState for nonexistent should return nil")
	}
}

func TestListPendingWorkflows(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create multiple workflows with different statuses
	// Note: workflow_id is the primary key, work_id can be set later
	workflows := []struct {
		workflowID string
		status     string
	}{
		{"workflow-wf1", "pending"},
		{"workflow-wf2", "processing"},
		{"workflow-wf3", "completed"},
		{"workflow-wf4", "failed"},
		{"workflow-wf5", "pending"},
	}

	for _, wf := range workflows {
		// Create workflow state with empty work_id (work not yet created)
		err := database.CreateWorkflowState(ctx, wf.workflowID, "", 0, wf.status, "{}")
		if err != nil {
			t.Fatalf("Failed to create workflow %s: %v", wf.workflowID, err)
		}
	}

	// Test ListPendingWorkflows - should return pending and processing only
	pending, err := database.ListPendingWorkflows(ctx)
	if err != nil {
		t.Fatalf("ListPendingWorkflows failed: %v", err)
	}

	// Should have workflow-wf1, workflow-wf2, workflow-wf5 (pending/processing)
	if len(pending) != 3 {
		t.Errorf("Expected 3 pending workflows, got %d", len(pending))
	}

	// Verify none are completed or failed
	for _, wf := range pending {
		if wf.StepStatus == "completed" || wf.StepStatus == "failed" {
			t.Errorf("ListPendingWorkflows returned workflow with status %s", wf.StepStatus)
		}
	}
}

func TestDeleteWorkflowState(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	workflowID := "workflow-todelete"

	// Create workflow state (no work_id needed for this test)
	err := database.CreateWorkflowState(ctx, workflowID, "", 0, "pending", "{}")
	if err != nil {
		t.Fatalf("CreateWorkflowState failed: %v", err)
	}

	// Verify it exists
	state, _ := database.GetWorkflowState(ctx, workflowID)
	if state == nil {
		t.Fatal("Workflow should exist before delete")
	}

	// Delete it
	err = database.DeleteWorkflowState(ctx, workflowID)
	if err != nil {
		t.Fatalf("DeleteWorkflowState failed: %v", err)
	}

	// Verify it's gone
	state, _ = database.GetWorkflowState(ctx, workflowID)
	if state != nil {
		t.Error("Workflow should not exist after delete")
	}
}

func TestStepDataEmptyBeadIDs(t *testing.T) {
	// Test that StepData handles empty BeadIDs correctly
	original := StepData{
		BeadID:     "test-bead",
		BaseBranch: "main",
		BeadIDs:    nil,
	}

	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal StepData with nil BeadIDs: %v", err)
	}

	var restored StepData
	if err := json.Unmarshal(jsonBytes, &restored); err != nil {
		t.Fatalf("failed to unmarshal StepData with nil BeadIDs: %v", err)
	}

	if restored.BeadIDs != nil && len(restored.BeadIDs) != 0 {
		t.Errorf("BeadIDs should be nil or empty, got %v", restored.BeadIDs)
	}
}

func TestReviewCountLimit(t *testing.T) {
	// Test that review count tracking works for the max iterations limit
	data := StepData{
		ReviewCount: 0,
	}

	maxIterations := 5
	for i := 0; i < maxIterations; i++ {
		data.ReviewCount++
		if data.ReviewCount > maxIterations {
			t.Errorf("ReviewCount exceeded max: %d > %d", data.ReviewCount, maxIterations)
		}
	}

	if data.ReviewCount != maxIterations {
		t.Errorf("ReviewCount should be %d, got %d", maxIterations, data.ReviewCount)
	}
}
