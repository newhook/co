package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/newhook/co/internal/db/sqlc"
)

// taskToLocal converts an sqlc.Task to local Task
func taskToLocal(t *sqlc.Task) *Task {
	task := &Task{
		ID:               t.ID,
		Status:           t.Status,
		TaskType:         t.TaskType,
		ComplexityBudget: int(t.ComplexityBudget),
		ActualComplexity: int(t.ActualComplexity),
		WorkID:           t.WorkID,
		ZellijSession:    t.ZellijSession,
		ZellijPane:       t.ZellijPane,
		WorktreePath:     t.WorktreePath,
		PRURL:            t.PrUrl,
		ErrorMessage:     t.ErrorMessage,
		CreatedAt:        t.CreatedAt,
	}
	if t.StartedAt.Valid {
		task.StartedAt = &t.StartedAt.Time
	}
	if t.CompletedAt.Valid {
		task.CompletedAt = &t.CompletedAt.Time
	}
	return task
}

// Task represents a virtual task (group of beads) in the database.
type Task struct {
	ID               string
	Status           string
	TaskType         string
	ComplexityBudget int
	ActualComplexity int
	WorkID           string
	ZellijSession    string
	ZellijPane       string
	WorktreePath     string
	PRURL            string
	ErrorMessage     string
	StartedAt        *time.Time
	CompletedAt      *time.Time
	CreatedAt        time.Time
}

// TaskBead represents a bead within a task.
type TaskBead struct {
	TaskID string
	BeadID string
	Status string
}

// CreateTask creates a new task with the given beads.
func (db *DB) CreateTask(ctx context.Context, id string, taskType string, beadIDs []string, complexityBudget int, workID string) error {
	// Use a transaction for atomicity
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.queries.WithTx(tx)

	// Insert task
	err = qtx.CreateTask(ctx, sqlc.CreateTaskParams{
		ID:               id,
		TaskType:         taskType,
		ComplexityBudget: int64(complexityBudget),
		WorkID:           workID,
	})
	if err != nil {
		return fmt.Errorf("failed to create task %s: %w", id, err)
	}

	// Insert task_beads
	for _, beadID := range beadIDs {
		err = qtx.CreateTaskBead(ctx, sqlc.CreateTaskBeadParams{
			TaskID: id,
			BeadID: beadID,
		})
		if err != nil {
			return fmt.Errorf("failed to add bead %s to task %s: %w", beadID, id, err)
		}
	}

	// Create work_tasks junction entry if workID is provided
	if workID != "" {
		// Get the current number of tasks to determine position
		existingTasks, err := qtx.GetWorkTasks(ctx, workID)
		if err != nil {
			return fmt.Errorf("failed to get existing tasks for work %s: %w", workID, err)
		}
		position := len(existingTasks)

		// Add task to work
		err = qtx.AddTaskToWork(ctx, sqlc.AddTaskToWorkParams{
			WorkID:   workID,
			TaskID:   id,
			Position: int64(position),
		})
		if err != nil {
			return fmt.Errorf("failed to add task %s to work %s: %w", id, workID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// CreateTasksBatch creates multiple tasks in a single transaction.
// This ensures consistent task numbering within a work.
func (db *DB) CreateTasksBatch(ctx context.Context, tasks []struct {
	ID               string
	TaskType         string
	BeadIDs          []string
	ComplexityBudget int
	WorkID           string
}) error {
	// Use a transaction for atomicity
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.queries.WithTx(tx)

	for _, task := range tasks {
		// Insert task
		err = qtx.CreateTask(ctx, sqlc.CreateTaskParams{
			ID:               task.ID,
			TaskType:         task.TaskType,
			ComplexityBudget: int64(task.ComplexityBudget),
			WorkID:           task.WorkID,
		})
		if err != nil {
			return fmt.Errorf("failed to create task %s: %w", task.ID, err)
		}

		// Insert task_beads
		for _, beadID := range task.BeadIDs {
			err = qtx.CreateTaskBead(ctx, sqlc.CreateTaskBeadParams{
				TaskID: task.ID,
				BeadID: beadID,
			})
			if err != nil {
				return fmt.Errorf("failed to add bead %s to task %s: %w", beadID, task.ID, err)
			}
		}

		// Create work_tasks junction entry if workID is provided
		if task.WorkID != "" {
			// Get the current number of tasks to determine position
			existingTasks, err := qtx.GetWorkTasks(ctx, task.WorkID)
			if err != nil {
				return fmt.Errorf("failed to get existing tasks for work %s: %w", task.WorkID, err)
			}
			position := len(existingTasks)

			// Add task to work
			err = qtx.AddTaskToWork(ctx, sqlc.AddTaskToWorkParams{
				WorkID:   task.WorkID,
				TaskID:   task.ID,
				Position: int64(position),
			})
			if err != nil {
				return fmt.Errorf("failed to add task %s to work %s: %w", task.ID, task.WorkID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// StartTask marks a task as processing with session info.
// Note: worktree_path is now managed at the work level
func (db *DB) StartTask(ctx context.Context, id, zellijSession, zellijPane string) error {
	now := time.Now()
	rows, err := db.queries.StartTask(ctx, sqlc.StartTaskParams{
		ZellijSession: zellijSession,
		ZellijPane:    zellijPane,
		WorktreePath:  "", // Deprecated, kept for compatibility
		StartedAt:     nullTime(now),
		ID:            id,
	})
	if err != nil {
		return fmt.Errorf("failed to start task %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// CompleteTask marks a task as completed with a PR URL.
func (db *DB) CompleteTask(ctx context.Context, id, prURL string) error {
	now := time.Now()
	rows, err := db.queries.CompleteTask(ctx, sqlc.CompleteTaskParams{
		PrUrl:       prURL,
		CompletedAt: nullTime(now),
		ID:          id,
	})
	if err != nil {
		return fmt.Errorf("failed to complete task %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// FailTask marks a task as failed with an error message.
func (db *DB) FailTask(ctx context.Context, id, errMsg string) error {
	now := time.Now()
	rows, err := db.queries.FailTask(ctx, sqlc.FailTaskParams{
		ErrorMessage: errMsg,
		CompletedAt:  nullTime(now),
		ID:           id,
	})
	if err != nil {
		return fmt.Errorf("failed to mark task %s as failed: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// ResetTaskStatus resets a stuck task from processing back to pending.
func (db *DB) ResetTaskStatus(ctx context.Context, id string) error {
	rows, err := db.queries.ResetTaskStatus(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to reset task %s to pending: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// GetTask retrieves a task by ID.
func (db *DB) GetTask(ctx context.Context, id string) (*Task, error) {
	task, err := db.queries.GetTask(ctx, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return taskToLocal(&task), nil
}

// GetTaskBeads returns all bead IDs for a task.
func (db *DB) GetTaskBeads(ctx context.Context, taskID string) ([]string, error) {
	beadIDs, err := db.queries.GetTaskBeads(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task beads: %w", err)
	}
	return beadIDs, nil
}

// GetTaskForBead returns the task ID that contains the given bead.
func (db *DB) GetTaskForBead(ctx context.Context, beadID string) (string, error) {
	taskID, err := db.queries.GetTaskForBead(ctx, beadID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get task for bead: %w", err)
	}
	return taskID, nil
}

// CompleteTaskBead marks a specific bead within a task as completed.
func (db *DB) CompleteTaskBead(ctx context.Context, taskID, beadID string) error {
	rows, err := db.queries.CompleteTaskBead(ctx, sqlc.CompleteTaskBeadParams{
		TaskID: taskID,
		BeadID: beadID,
	})
	if err != nil {
		return fmt.Errorf("failed to complete task bead: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task bead %s/%s not found", taskID, beadID)
	}
	return nil
}

// FailTaskBead marks a specific bead within a task as failed.
func (db *DB) FailTaskBead(ctx context.Context, taskID, beadID string) error {
	rows, err := db.queries.FailTaskBead(ctx, sqlc.FailTaskBeadParams{
		TaskID: taskID,
		BeadID: beadID,
	})
	if err != nil {
		return fmt.Errorf("failed to fail task bead: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task bead %s/%s not found", taskID, beadID)
	}
	return nil
}

// IsTaskCompleted checks if all beads in a task are completed.
// Returns true if all beads are completed (not failed), false otherwise.
func (db *DB) IsTaskCompleted(ctx context.Context, taskID string) (bool, error) {
	counts, err := db.queries.CountTaskBeadStatuses(ctx, taskID)
	if err != nil {
		return false, fmt.Errorf("failed to check task completion: %w", err)
	}
	if counts.Total == 0 {
		return false, nil
	}
	return counts.Completed == counts.Total, nil
}

// CheckAndCompleteTask checks if all beads are completed and auto-completes the task.
// Returns true if the task was auto-completed, false otherwise.
func (db *DB) CheckAndCompleteTask(ctx context.Context, taskID, prURL string) (bool, error) {
	completed, err := db.IsTaskCompleted(ctx, taskID)
	if err != nil {
		return false, err
	}
	if !completed {
		return false, nil
	}

	// Auto-complete the task
	if err := db.CompleteTask(ctx, taskID, prURL); err != nil {
		return false, err
	}
	return true, nil
}

// ListTasks returns all tasks, optionally filtered by status.
func (db *DB) ListTasks(ctx context.Context, statusFilter string) ([]*Task, error) {
	var tasks []sqlc.Task
	var err error

	if statusFilter == "" {
		tasks, err = db.queries.ListTasks(ctx)
	} else {
		tasks, err = db.queries.ListTasksByStatus(ctx, statusFilter)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	result := make([]*Task, len(tasks))
	for i, t := range tasks {
		result[i] = taskToLocal(&t)
	}
	return result, nil
}
