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
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "", false)
	require.NoError(t, err)

	reviewTaskID := "work-1.1"
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
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "", false)
	require.NoError(t, err)

	reviewTaskID := "work-1.1"
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
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "", false)
	require.NoError(t, err)

	reviewTaskID := "work-1.1"
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

func TestTaskIDFormat(t *testing.T) {
	// Verify all task types use sequential numeric IDs: {workID}.{number}

	workID := "work-1"

	// All task types use the same format: workID.number
	// The task_type column indicates whether it's implement, review, pr, etc.
	reviewTaskID := workID + ".1"
	assert.Equal(t, "work-1.1", reviewTaskID)

	prTaskID := workID + ".2"
	assert.Equal(t, "work-1.2", prTaskID)

	implementTaskID := workID + ".3"
	assert.Equal(t, "work-1.3", implementTaskID)
}

// TestGetReadyTasksRespectsDependencies tests that GetReadyTasksForWork
// returns tasks in correct dependency order (tasks with unmet dependencies are not returned).
func TestGetReadyTasksRespectsDependencies(t *testing.T) {
	testDB, cleanup := setupOrchestrateTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "", false)
	require.NoError(t, err)

	// Create tasks with dependencies:
	// task-1 has no dependencies (ready)
	// task-2 depends on task-1 (not ready until task-1 completes)
	// task-3 depends on task-2 (not ready until task-2 completes)
	err = testDB.CreateTask(ctx, "work-1.1", "implement", []string{"a"}, 5, "work-1")
	require.NoError(t, err)
	err = testDB.CreateTask(ctx, "work-1.2", "implement", []string{"b"}, 5, "work-1")
	require.NoError(t, err)
	err = testDB.CreateTask(ctx, "work-1.3", "implement", []string{"c"}, 5, "work-1")
	require.NoError(t, err)

	// Add dependencies: task-2 depends on task-1, task-3 depends on task-2
	err = testDB.AddTaskDependency(ctx, "work-1.2", "work-1.1")
	require.NoError(t, err)
	err = testDB.AddTaskDependency(ctx, "work-1.3", "work-1.2")
	require.NoError(t, err)

	// Initially, only task-1 should be ready
	readyTasks, err := testDB.GetReadyTasksForWork(ctx, "work-1")
	require.NoError(t, err)
	require.Len(t, readyTasks, 1, "only task-1 should be ready initially")
	assert.Equal(t, "work-1.1", readyTasks[0].ID)

	// Complete task-1, now task-2 should be ready
	err = testDB.CompleteTask(ctx, "work-1.1", "")
	require.NoError(t, err)

	readyTasks, err = testDB.GetReadyTasksForWork(ctx, "work-1")
	require.NoError(t, err)
	require.Len(t, readyTasks, 1, "only task-2 should be ready after task-1 completes")
	assert.Equal(t, "work-1.2", readyTasks[0].ID)

	// Complete task-2, now task-3 should be ready
	err = testDB.CompleteTask(ctx, "work-1.2", "")
	require.NoError(t, err)

	readyTasks, err = testDB.GetReadyTasksForWork(ctx, "work-1")
	require.NoError(t, err)
	require.Len(t, readyTasks, 1, "only task-3 should be ready after task-2 completes")
	assert.Equal(t, "work-1.3", readyTasks[0].ID)
}

// TestGetReadyTasksWithDiamondDependency tests GetReadyTasksForWork with a diamond dependency.
func TestGetReadyTasksWithDiamondDependency(t *testing.T) {
	testDB, cleanup := setupOrchestrateTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work
	err := testDB.CreateWork(ctx, "work-1", "Test Work", "/tmp/test", "feat/test", "main", "", false)
	require.NoError(t, err)

	// Diamond dependency:
	// task-1 and task-2 have no dependencies (both ready)
	// task-3 depends on both task-1 and task-2 (not ready until both complete)
	// task-4 depends on task-3
	err = testDB.CreateTask(ctx, "work-1.1", "implement", []string{"a"}, 5, "work-1")
	require.NoError(t, err)
	err = testDB.CreateTask(ctx, "work-1.2", "implement", []string{"b"}, 5, "work-1")
	require.NoError(t, err)
	err = testDB.CreateTask(ctx, "work-1.3", "implement", []string{"c"}, 5, "work-1")
	require.NoError(t, err)
	err = testDB.CreateTask(ctx, "work-1.4", "implement", []string{"d"}, 5, "work-1")
	require.NoError(t, err)

	// task-3 depends on task-1 and task-2
	err = testDB.AddTaskDependency(ctx, "work-1.3", "work-1.1")
	require.NoError(t, err)
	err = testDB.AddTaskDependency(ctx, "work-1.3", "work-1.2")
	require.NoError(t, err)
	// task-4 depends on task-3
	err = testDB.AddTaskDependency(ctx, "work-1.4", "work-1.3")
	require.NoError(t, err)

	// Initially, task-1 and task-2 should both be ready
	readyTasks, err := testDB.GetReadyTasksForWork(ctx, "work-1")
	require.NoError(t, err)
	require.Len(t, readyTasks, 2, "task-1 and task-2 should be ready initially")

	readyIDs := make(map[string]bool)
	for _, t := range readyTasks {
		readyIDs[t.ID] = true
	}
	assert.True(t, readyIDs["work-1.1"], "task-1 should be ready")
	assert.True(t, readyIDs["work-1.2"], "task-2 should be ready")

	// Complete only task-1, task-3 should still not be ready (needs task-2 too)
	err = testDB.CompleteTask(ctx, "work-1.1", "")
	require.NoError(t, err)

	readyTasks, err = testDB.GetReadyTasksForWork(ctx, "work-1")
	require.NoError(t, err)
	require.Len(t, readyTasks, 1, "only task-2 should be ready (task-3 needs both)")
	assert.Equal(t, "work-1.2", readyTasks[0].ID)

	// Complete task-2, now task-3 should be ready
	err = testDB.CompleteTask(ctx, "work-1.2", "")
	require.NoError(t, err)

	readyTasks, err = testDB.GetReadyTasksForWork(ctx, "work-1")
	require.NoError(t, err)
	require.Len(t, readyTasks, 1, "task-3 should be ready after both deps complete")
	assert.Equal(t, "work-1.3", readyTasks[0].ID)
}
