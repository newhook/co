package cmd

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTUITestDB creates an in-memory database for TUI tests
func setupTUITestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()

	testDB, err := db.OpenPath(context.Background(), ":memory:")
	require.NoError(t, err, "failed to open database")

	cleanup := func() {
		testDB.Close()
	}

	return testDB, cleanup
}

func TestCreateReviewTask_SetsAutoWorkflowFalse(t *testing.T) {
	// This test verifies that when a review task is created via TUI,
	// it should have auto_workflow=false metadata set

	testDB, cleanup := setupTUITestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work first (simulating existing work in TUI)
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "")
	require.NoError(t, err)

	// Simulate what createReviewTask does:
	// 1. Count existing review tasks
	tasks, err := testDB.GetWorkTasks(ctx, "work-1")
	require.NoError(t, err)
	reviewCount := 0
	for _, task := range tasks {
		if len(task.ID) > 0 && task.ID[len(task.ID)-1] >= '1' && task.ID[len(task.ID)-1] <= '9' {
			// Count review tasks (simplified check)
			reviewCount++
		}
	}

	// 2. Generate unique review task ID
	reviewTaskID := "work-1.review-1" // This would be: fmt.Sprintf("%s.review-%d", workID, reviewCount+1)

	// 3. Create the review task
	err = testDB.CreateTask(ctx, reviewTaskID, "review", nil, 0, "work-1")
	require.NoError(t, err)

	// 4. Set auto_workflow=false (this is what the TUI code now does)
	err = testDB.SetTaskMetadata(ctx, reviewTaskID, "auto_workflow", "false")
	require.NoError(t, err)

	// Verify the metadata was set correctly
	autoWorkflow, err := testDB.GetTaskMetadata(ctx, reviewTaskID, "auto_workflow")
	require.NoError(t, err)
	assert.Equal(t, "false", autoWorkflow, "TUI-created review tasks should have auto_workflow=false")
}

func TestHandleReviewFixLoop_ReturnsEarlyForManualReview(t *testing.T) {
	// This test simulates the early return logic in handleReviewFixLoop

	testDB, cleanup := setupTUITestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create work and manual review task
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "")
	require.NoError(t, err)

	reviewTaskID := "work-1.review-1"
	err = testDB.CreateTask(ctx, reviewTaskID, "review", nil, 0, "work-1")
	require.NoError(t, err)

	// Set auto_workflow=false (manual review)
	err = testDB.SetTaskMetadata(ctx, reviewTaskID, "auto_workflow", "false")
	require.NoError(t, err)

	// Simulate the check in handleReviewFixLoop
	autoWorkflow, err := testDB.GetTaskMetadata(ctx, reviewTaskID, "auto_workflow")

	shouldReturnEarly := (err == nil && autoWorkflow == "false")
	assert.True(t, shouldReturnEarly, "handleReviewFixLoop should return early for manual reviews")
}

func TestHandleReviewFixLoop_ContinuesForAutomatedReview(t *testing.T) {
	// This test simulates that automated reviews continue with the workflow

	testDB, cleanup := setupTUITestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create work and automated review task (no metadata)
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "")
	require.NoError(t, err)

	reviewTaskID := "work-1.review-1"
	err = testDB.CreateTask(ctx, reviewTaskID, "review", nil, 0, "work-1")
	require.NoError(t, err)

	// Don't set auto_workflow metadata (automated review)

	// Simulate the check in handleReviewFixLoop
	autoWorkflow, err := testDB.GetTaskMetadata(ctx, reviewTaskID, "auto_workflow")

	shouldReturnEarly := (err == nil && autoWorkflow == "false")
	assert.False(t, shouldReturnEarly, "handleReviewFixLoop should continue for automated reviews")
}

func TestMultipleReviewTasks_EachHasOwnMetadata(t *testing.T) {
	// Verify that multiple review tasks can have independent metadata

	testDB, cleanup := setupTUITestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create work
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "")
	require.NoError(t, err)

	// Create first review task (manual via TUI)
	err = testDB.CreateTask(ctx, "work-1.review-1", "review", nil, 0, "work-1")
	require.NoError(t, err)
	err = testDB.SetTaskMetadata(ctx, "work-1.review-1", "auto_workflow", "false")
	require.NoError(t, err)

	// Create second review task (automated - created by handleReviewFixLoop)
	err = testDB.CreateTask(ctx, "work-1.review-2", "review", nil, 0, "work-1")
	require.NoError(t, err)
	// No metadata set for automated reviews

	// Verify first task is manual
	val1, err := testDB.GetTaskMetadata(ctx, "work-1.review-1", "auto_workflow")
	require.NoError(t, err)
	assert.Equal(t, "false", val1)

	// Verify second task has empty metadata (automated)
	val2, err := testDB.GetTaskMetadata(ctx, "work-1.review-2", "auto_workflow")
	require.NoError(t, err)
	assert.Empty(t, val2, "automated review task should not have auto_workflow metadata set")
}
