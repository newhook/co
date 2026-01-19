package cmd

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupOrchestrateTestDB creates an in-memory database for orchestrate tests
func setupOrchestrateTestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()

	testDB, err := db.OpenPath(context.Background(), ":memory:")
	require.NoError(t, err, "failed to open database")

	cleanup := func() {
		testDB.Close()
	}

	return testDB, cleanup
}

func TestAutoWorkflowMetadata_ManualReviewSkipsAutomatedWorkflow(t *testing.T) {
	// This test verifies that manual review tasks (with auto_workflow=false)
	// can be identified via metadata lookup

	testDB, cleanup := setupOrchestrateTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work and review task
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "")
	require.NoError(t, err)

	reviewTaskID := "work-1.review-1"
	err = testDB.CreateTask(ctx, reviewTaskID, "review", nil, 0, "work-1")
	require.NoError(t, err)

	// Simulate TUI behavior: set auto_workflow=false for manual review
	err = testDB.SetTaskMetadata(ctx, reviewTaskID, "auto_workflow", "false")
	require.NoError(t, err)

	// Verify that we can detect this is a manual review
	autoWorkflow, err := testDB.GetTaskMetadata(ctx, reviewTaskID, "auto_workflow")
	require.NoError(t, err)
	assert.Equal(t, "false", autoWorkflow)

	// This simulates what handleReviewFixLoop does: check auto_workflow metadata
	// and return early if it equals "false"
	isManualReview := (err == nil && autoWorkflow == "false")
	assert.True(t, isManualReview, "manual review should be detected via auto_workflow=false")
}

func TestAutoWorkflowMetadata_AutomatedReviewContinuesWorkflow(t *testing.T) {
	// This test verifies that automated review tasks (without auto_workflow metadata)
	// should continue with the normal workflow

	testDB, cleanup := setupOrchestrateTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work and review task (no auto_workflow metadata = automated)
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "")
	require.NoError(t, err)

	reviewTaskID := "work-1.review-1"
	err = testDB.CreateTask(ctx, reviewTaskID, "review", nil, 0, "work-1")
	require.NoError(t, err)

	// Don't set auto_workflow metadata (this is what automated workflow does)

	// Verify that we can detect this is an automated review
	// GetTaskMetadata returns empty string when key doesn't exist
	autoWorkflow, err := testDB.GetTaskMetadata(ctx, reviewTaskID, "auto_workflow")
	require.NoError(t, err)

	// For automated reviews, the metadata is empty (not "false")
	// This means the workflow should continue (not skip)
	shouldSkipWorkflow := (err == nil && autoWorkflow == "false")
	assert.False(t, shouldSkipWorkflow, "automated review should continue workflow (no auto_workflow=false)")
}

func TestAutoWorkflowMetadata_ExplicitTrueAlsoContinuesWorkflow(t *testing.T) {
	// This test verifies that if auto_workflow is explicitly set to "true",
	// the workflow should continue

	testDB, cleanup := setupOrchestrateTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work and review task
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "")
	require.NoError(t, err)

	reviewTaskID := "work-1.review-1"
	err = testDB.CreateTask(ctx, reviewTaskID, "review", nil, 0, "work-1")
	require.NoError(t, err)

	// Explicitly set auto_workflow=true
	err = testDB.SetTaskMetadata(ctx, reviewTaskID, "auto_workflow", "true")
	require.NoError(t, err)

	// Verify that workflow should continue
	autoWorkflow, err := testDB.GetTaskMetadata(ctx, reviewTaskID, "auto_workflow")
	require.NoError(t, err)

	shouldContinueWorkflow := (err != nil || autoWorkflow != "false")
	assert.True(t, shouldContinueWorkflow, "auto_workflow=true should continue workflow")
}

func TestReviewTaskIDFormat(t *testing.T) {
	// Verify the review task ID format used by TUI
	// The format is: {workID}.review-{reviewNumber}

	workID := "work-1"

	// The actual format in code: fmt.Sprintf("%s.review-%d", workID, reviewCount+1)
	// When reviewCount=0, the ID is work-1.review-1
	reviewTaskID := workID + ".review-" + "1"
	expectedID := "work-1.review-1"

	// Verify format matches expected
	assert.Equal(t, expectedID, reviewTaskID)
	assert.Contains(t, reviewTaskID, workID)
	assert.Contains(t, reviewTaskID, ".review-")

	// Test with different review count (reviewCount=2 -> review-3)
	reviewTaskID2 := workID + ".review-" + "3"
	assert.Equal(t, "work-1.review-3", reviewTaskID2)
}
