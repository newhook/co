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

func TestAddWorkBeads(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work first
	err := db.CreateWork(ctx, "w-test", "/tmp/tree", "feature/test", "main")
	require.NoError(t, err)

	// Add beads to work
	err = db.AddWorkBeads(ctx, "w-test", []string{"bead-1", "bead-2"}, 0)
	require.NoError(t, err)

	// Verify beads were added
	beads, err := db.GetWorkBeads(ctx, "w-test")
	require.NoError(t, err)
	assert.Len(t, beads, 2)
}

func TestAddWorkBeadsDuplicateError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work first
	err := db.CreateWork(ctx, "w-test", "/tmp/tree", "feature/test", "main")
	require.NoError(t, err)

	// Add initial beads
	err = db.AddWorkBeads(ctx, "w-test", []string{"bead-1", "bead-2"}, 0)
	require.NoError(t, err)

	// Try to add duplicate beads - should error
	err = db.AddWorkBeads(ctx, "w-test", []string{"bead-2", "bead-3"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "beads already exist in work")
	assert.Contains(t, err.Error(), "bead-2")
}

func TestAddWorkBeadsEmptyList(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work first
	err := db.CreateWork(ctx, "w-test", "/tmp/tree", "feature/test", "main")
	require.NoError(t, err)

	// Add empty list - should succeed with no effect
	err = db.AddWorkBeads(ctx, "w-test", []string{}, 0)
	require.NoError(t, err)

	beads, err := db.GetWorkBeads(ctx, "w-test")
	require.NoError(t, err)
	assert.Empty(t, beads)
}

func TestAddWorkBeadsWithGroup(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a work first
	err := db.CreateWork(ctx, "w-test", "/tmp/tree", "feature/test", "main")
	require.NoError(t, err)

	// Add beads with different groups
	err = db.AddWorkBeads(ctx, "w-test", []string{"bead-1", "bead-2"}, 1)
	require.NoError(t, err)
	err = db.AddWorkBeads(ctx, "w-test", []string{"bead-3"}, 0)
	require.NoError(t, err)

	// Verify groups
	beads, err := db.GetWorkBeads(ctx, "w-test")
	require.NoError(t, err)
	assert.Len(t, beads, 3)

	// Check that grouped beads have the correct group ID
	groupedCount := 0
	for _, b := range beads {
		if b.GroupID == 1 {
			groupedCount++
		}
	}
	assert.Equal(t, 2, groupedCount)
}
