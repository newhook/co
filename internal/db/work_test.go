package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteWork(t *testing.T) {
	// Create a test database
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work
	workID := "w-test"
	branchName := "feature/test"
	baseBranch := "main"
	worktreePath := "/tmp/test-work/tree"

	err := db.CreateWork(ctx, workID, worktreePath, branchName, baseBranch)
	require.NoError(t, err, "Failed to create work")

	// Create tasks for the work
	task1ID := "w-test.1"
	task2ID := "w-test.2"

	err = db.CreateTask(ctx, task1ID, "implement", []string{"bead-1", "bead-2"}, 50, workID)
	require.NoError(t, err, "Failed to create task 1")

	err = db.CreateTask(ctx, task2ID, "implement", []string{"bead-3"}, 30, workID)
	require.NoError(t, err, "Failed to create task 2")

	// Note: CreateTask already adds the task to work_tasks when workID is provided,
	// so we don't need to call AddTaskToWork separately.

	// Verify work exists
	work, err := db.GetWork(ctx, workID)
	require.NoError(t, err, "Failed to get work")
	require.NotNil(t, work, "Work should exist")

	// Verify tasks exist
	tasks, err := db.GetWorkTasks(ctx, workID)
	require.NoError(t, err, "Failed to get work tasks")
	require.Len(t, tasks, 2, "Expected 2 tasks")

	// Delete the work
	err = db.DeleteWork(ctx, workID)
	require.NoError(t, err, "Failed to delete work")

	// Verify work is deleted
	work, err = db.GetWork(ctx, workID)
	require.NoError(t, err, "Failed to get work after deletion")
	assert.Nil(t, work, "Work should be deleted")

	// Verify tasks are deleted
	task1, err := db.GetTask(ctx, task1ID)
	require.NoError(t, err, "Failed to get task 1 after deletion")
	assert.Nil(t, task1, "Task 1 should be deleted")

	task2, err := db.GetTask(ctx, task2ID)
	require.NoError(t, err, "Failed to get task 2 after deletion")
	assert.Nil(t, task2, "Task 2 should be deleted")

	// Verify work_tasks relationships are deleted
	tasks, err = db.GetWorkTasks(ctx, workID)
	require.NoError(t, err, "Failed to get work tasks after deletion")
	assert.Empty(t, tasks, "Expected 0 tasks after deletion")
}

func TestDeleteWorkNotFound(t *testing.T) {
	// Create a test database
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Try to delete a non-existent work
	err := db.DeleteWork(ctx, "w-nonexistent")
	assert.Error(t, err, "Expected error when deleting non-existent work")
}
