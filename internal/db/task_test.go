package db

import (
	"context"
	"testing"
)

func TestCreateTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.CreateTask(context.Background(), "task-1", "implement", []string{"bead-1", "bead-2"}, 100, "")
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	// Verify task was created
	task, err := db.GetTask(context.Background(),"task-1")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task == nil {
		t.Fatal("expected task, got nil")
	}
	if task.ID != "task-1" {
		t.Errorf("expected ID 'task-1', got %q", task.ID)
	}
	if task.Status != StatusPending {
		t.Errorf("expected status %q, got %q", StatusPending, task.Status)
	}
	if task.ComplexityBudget != 100 {
		t.Errorf("expected complexity budget 100, got %d", task.ComplexityBudget)
	}

	// Verify beads were added
	beads, err := db.GetTaskBeads(context.Background(),"task-1")
	if err != nil {
		t.Fatalf("GetTaskBeads failed: %v", err)
	}
	if len(beads) != 2 {
		t.Errorf("expected 2 beads, got %d", len(beads))
	}
}

func TestStartTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1"}, 100, "")
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	err = db.StartTask(context.Background(),"task-1", "session-1", "pane-1")
	if err != nil {
		t.Fatalf("StartTask failed: %v", err)
	}

	task, err := db.GetTask(context.Background(),"task-1")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task.Status != StatusProcessing {
		t.Errorf("expected status %q, got %q", StatusProcessing, task.Status)
	}
	if task.ZellijSession != "session-1" {
		t.Errorf("expected session 'session-1', got %q", task.ZellijSession)
	}
	if task.ZellijPane != "pane-1" {
		t.Errorf("expected pane 'pane-1', got %q", task.ZellijPane)
	}
	// WorktreePath is now managed at work level, should be empty
	if task.WorktreePath != "" {
		t.Errorf("expected empty worktree path (managed at work level), got %q", task.WorktreePath)
	}
	if task.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
}

func TestStartTaskNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.StartTask(context.Background(),"nonexistent", "s", "p")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestCompleteTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1"}, 100, "")
	db.StartTask(context.Background(),"task-1", "s", "p")

	err := db.CompleteTask(context.Background(),"task-1", "https://github.com/example/pr/1")
	if err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}

	task, _ := db.GetTask(context.Background(),"task-1")
	if task.Status != StatusCompleted {
		t.Errorf("expected status %q, got %q", StatusCompleted, task.Status)
	}
	if task.PRURL != "https://github.com/example/pr/1" {
		t.Errorf("expected PR URL, got %q", task.PRURL)
	}
	if task.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestCompleteTaskNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.CompleteTask(context.Background(),"nonexistent", "")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestFailTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1"}, 100, "")
	db.StartTask(context.Background(),"task-1", "s", "p")

	err := db.FailTask(context.Background(),"task-1", "something went wrong")
	if err != nil {
		t.Fatalf("FailTask failed: %v", err)
	}

	task, _ := db.GetTask(context.Background(),"task-1")
	if task.Status != StatusFailed {
		t.Errorf("expected status %q, got %q", StatusFailed, task.Status)
	}
	if task.ErrorMessage != "something went wrong" {
		t.Errorf("expected error message, got %q", task.ErrorMessage)
	}
	if task.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestFailTaskNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.FailTask(context.Background(),"nonexistent", "error")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestGetTaskNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.GetTask(context.Background(),"nonexistent")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestGetTaskForBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1", "bead-2"}, 100, "")

	taskID, err := db.GetTaskForBead(context.Background(),"bead-1")
	if err != nil {
		t.Fatalf("GetTaskForBead failed: %v", err)
	}
	if taskID != "task-1" {
		t.Errorf("expected task-1, got %q", taskID)
	}

	taskID, err = db.GetTaskForBead(context.Background(),"bead-2")
	if err != nil {
		t.Fatalf("GetTaskForBead failed: %v", err)
	}
	if taskID != "task-1" {
		t.Errorf("expected task-1, got %q", taskID)
	}

	// Nonexistent bead
	taskID, err = db.GetTaskForBead(context.Background(),"nonexistent")
	if err != nil {
		t.Fatalf("GetTaskForBead failed: %v", err)
	}
	if taskID != "" {
		t.Errorf("expected empty string, got %q", taskID)
	}
}

func TestCompleteTaskBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1", "bead-2"}, 100, "")

	err := db.CompleteTaskBead(context.Background(),"task-1", "bead-1")
	if err != nil {
		t.Fatalf("CompleteTaskBead failed: %v", err)
	}

	// Verify via IsTaskCompleted (should be false since bead-2 is still pending)
	completed, err := db.IsTaskCompleted(context.Background(),"task-1")
	if err != nil {
		t.Fatalf("IsTaskCompleted failed: %v", err)
	}
	if completed {
		t.Error("expected task to not be completed yet")
	}
}

func TestCompleteTaskBeadNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1"}, 100, "")

	err := db.CompleteTaskBead(context.Background(),"task-1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task bead")
	}
}

func TestFailTaskBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1"}, 100, "")

	err := db.FailTaskBead(context.Background(),"task-1", "bead-1")
	if err != nil {
		t.Fatalf("FailTaskBead failed: %v", err)
	}

	// Task should not be considered completed since bead is failed
	completed, _ := db.IsTaskCompleted(context.Background(),"task-1")
	if completed {
		t.Error("expected task to not be completed when bead is failed")
	}
}

func TestFailTaskBeadNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1"}, 100, "")

	err := db.FailTaskBead(context.Background(),"task-1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task bead")
	}
}

func TestIsTaskCompleted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1", "bead-2"}, 100, "")

	// Initially not completed
	completed, err := db.IsTaskCompleted(context.Background(),"task-1")
	if err != nil {
		t.Fatalf("IsTaskCompleted failed: %v", err)
	}
	if completed {
		t.Error("expected task to not be completed initially")
	}

	// Complete first bead
	db.CompleteTaskBead(context.Background(),"task-1", "bead-1")
	completed, _ = db.IsTaskCompleted(context.Background(),"task-1")
	if completed {
		t.Error("expected task to not be completed with one bead pending")
	}

	// Complete second bead
	db.CompleteTaskBead(context.Background(),"task-1", "bead-2")
	completed, _ = db.IsTaskCompleted(context.Background(),"task-1")
	if !completed {
		t.Error("expected task to be completed when all beads are completed")
	}
}

func TestIsTaskCompletedEmpty(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Task with no beads
	_, err := db.Exec(`INSERT INTO tasks (id, status) VALUES ('empty-task', 'pending')`)
	if err != nil {
		t.Fatalf("failed to create empty task: %v", err)
	}

	completed, err := db.IsTaskCompleted(context.Background(),"empty-task")
	if err != nil {
		t.Fatalf("IsTaskCompleted failed: %v", err)
	}
	if completed {
		t.Error("expected empty task to not be considered completed")
	}
}

func TestCheckAndCompleteTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1", "bead-2"}, 100, "")
	db.StartTask(context.Background(),"task-1", "s", "p")

	// Not all beads completed yet
	autoCompleted, err := db.CheckAndCompleteTask(context.Background(),"task-1", "https://github.com/pr/1")
	if err != nil {
		t.Fatalf("CheckAndCompleteTask failed: %v", err)
	}
	if autoCompleted {
		t.Error("expected not auto-completed when beads are pending")
	}

	task, _ := db.GetTask(context.Background(),"task-1")
	if task.Status != StatusProcessing {
		t.Errorf("expected status %q, got %q", StatusProcessing, task.Status)
	}

	// Complete all beads
	db.CompleteTaskBead(context.Background(),"task-1", "bead-1")
	db.CompleteTaskBead(context.Background(),"task-1", "bead-2")

	autoCompleted, err = db.CheckAndCompleteTask(context.Background(),"task-1", "https://github.com/pr/1")
	if err != nil {
		t.Fatalf("CheckAndCompleteTask failed: %v", err)
	}
	if !autoCompleted {
		t.Error("expected auto-completed when all beads are completed")
	}

	task, _ = db.GetTask(context.Background(),"task-1")
	if task.Status != StatusCompleted {
		t.Errorf("expected status %q, got %q", StatusCompleted, task.Status)
	}
	if task.PRURL != "https://github.com/pr/1" {
		t.Errorf("expected PR URL, got %q", task.PRURL)
	}
}

func TestListTasks(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create several tasks with different statuses
	db.CreateTask(context.Background(),"task-1", "implement", []string{"bead-1"}, 100, "")
	db.CreateTask(context.Background(),"task-2", "implement", []string{"bead-2"}, 100, "")
	db.StartTask(context.Background(),"task-2", "s", "p")
	db.CreateTask(context.Background(),"task-3", "implement", []string{"bead-3"}, 100, "")
	db.StartTask(context.Background(),"task-3", "s", "p")
	db.CompleteTask(context.Background(),"task-3", "")
	db.CreateTask(context.Background(),"task-4", "implement", []string{"bead-4"}, 100, "")
	db.StartTask(context.Background(),"task-4", "s", "p")
	db.FailTask(context.Background(),"task-4", "error")

	// List all
	tasks, err := db.ListTasks(context.Background(),"")
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 4 {
		t.Errorf("expected 4 tasks, got %d", len(tasks))
	}

	// List pending only
	tasks, err = db.ListTasks(context.Background(),StatusPending)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 pending task, got %d", len(tasks))
	}

	// List processing only
	tasks, err = db.ListTasks(context.Background(),StatusProcessing)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 processing task, got %d", len(tasks))
	}

	// List completed only
	tasks, err = db.ListTasks(context.Background(),StatusCompleted)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 completed task, got %d", len(tasks))
	}

	// List failed only
	tasks, err = db.ListTasks(context.Background(),StatusFailed)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 failed task, got %d", len(tasks))
	}
}
