package db

import (
	"context"
	"testing"
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
	if err != nil {
		t.Fatalf("Failed to create work: %v", err)
	}

	// Create tasks for the work
	task1ID := "w-test.1"
	task2ID := "w-test.2"

	err = db.CreateTask(ctx, task1ID, "implement", []string{"bead-1", "bead-2"}, 50, workID)
	if err != nil {
		t.Fatalf("Failed to create task 1: %v", err)
	}

	err = db.CreateTask(ctx, task2ID, "implement", []string{"bead-3"}, 30, workID)
	if err != nil {
		t.Fatalf("Failed to create task 2: %v", err)
	}

	// Note: CreateTask already adds the task to work_tasks when workID is provided,
	// so we don't need to call AddTaskToWork separately.

	// Verify work exists
	work, err := db.GetWork(ctx, workID)
	if err != nil {
		t.Fatalf("Failed to get work: %v", err)
	}
	if work == nil {
		t.Fatalf("Work should exist")
	}

	// Verify tasks exist
	tasks, err := db.GetWorkTasks(ctx, workID)
	if err != nil {
		t.Fatalf("Failed to get work tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(tasks))
	}

	// Delete the work
	err = db.DeleteWork(ctx, workID)
	if err != nil {
		t.Fatalf("Failed to delete work: %v", err)
	}

	// Verify work is deleted
	work, err = db.GetWork(ctx, workID)
	if err != nil {
		t.Fatalf("Failed to get work after deletion: %v", err)
	}
	if work != nil {
		t.Errorf("Work should be deleted")
	}

	// Verify tasks are deleted
	task1, err := db.GetTask(ctx, task1ID)
	if err != nil {
		t.Fatalf("Failed to get task 1 after deletion: %v", err)
	}
	if task1 != nil {
		t.Errorf("Task 1 should be deleted")
	}

	task2, err := db.GetTask(ctx, task2ID)
	if err != nil {
		t.Fatalf("Failed to get task 2 after deletion: %v", err)
	}
	if task2 != nil {
		t.Errorf("Task 2 should be deleted")
	}

	// Verify work_tasks relationships are deleted
	tasks, err = db.GetWorkTasks(ctx, workID)
	if err != nil {
		t.Fatalf("Failed to get work tasks after deletion: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("Expected 0 tasks after deletion, got %d", len(tasks))
	}
}

func TestDeleteWorkNotFound(t *testing.T) {
	// Create a test database
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Try to delete a non-existent work
	err := db.DeleteWork(ctx, "w-nonexistent")
	if err == nil {
		t.Errorf("Expected error when deleting non-existent work")
	}
}