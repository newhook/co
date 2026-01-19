package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/newhook/co/internal/db/sqlc"
)

// ScheduledTask represents a scheduled task in the database.
type ScheduledTask struct {
	ID          string
	WorkID      string
	TaskType    string
	ScheduledAt time.Time
	ExecutedAt  *time.Time
	Status      string
	ErrorMessage *string
	Metadata    map[string]string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Task types for the scheduler
const (
	TaskTypePRFeedback        = "pr_feedback"
	TaskTypeCommentResolution = "comment_resolution"
)

// Task statuses
const (
	TaskStatusPending   = "pending"
	TaskStatusExecuting = "executing"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
)

// ScheduleTask schedules a new task for a work.
func (db *DB) ScheduleTask(ctx context.Context, workID string, taskType string, scheduledAt time.Time, metadata map[string]string) (*ScheduledTask, error) {
	id := uuid.New().String()

	metadataJSON := "{}"
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON = string(data)
	}

	err := db.queries.CreateScheduledTask(ctx, sqlc.CreateScheduledTaskParams{
		ID:          id,
		WorkID:      workID,
		TaskType:    taskType,
		ScheduledAt: scheduledAt,
		Status:      TaskStatusPending,
		Metadata:    metadataJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to schedule task: %w", err)
	}

	return &ScheduledTask{
		ID:          id,
		WorkID:      workID,
		TaskType:    taskType,
		ScheduledAt: scheduledAt,
		Status:      TaskStatusPending,
		Metadata:    metadata,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

// GetNextScheduledTask gets the next pending task that's ready to run.
func (db *DB) GetNextScheduledTask(ctx context.Context) (*ScheduledTask, error) {
	task, err := db.queries.GetNextScheduledTask(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get next scheduled task: %w", err)
	}

	return schedulerToLocal(&task), nil
}

// GetScheduledTasksForWork gets all pending scheduled tasks for a work.
func (db *DB) GetScheduledTasksForWork(ctx context.Context, workID string) ([]*ScheduledTask, error) {
	tasks, err := db.queries.GetScheduledTasksForWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get scheduled tasks: %w", err)
	}

	result := make([]*ScheduledTask, len(tasks))
	for i, t := range tasks {
		result[i] = schedulerToLocal(&t)
	}
	return result, nil
}

// GetPendingTaskByType gets the next pending task of a specific type for a work.
func (db *DB) GetPendingTaskByType(ctx context.Context, workID string, taskType string) (*ScheduledTask, error) {
	task, err := db.queries.GetPendingTaskByType(ctx, sqlc.GetPendingTaskByTypeParams{
		WorkID:   workID,
		TaskType: taskType,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get pending task by type: %w", err)
	}

	return schedulerToLocal(&task), nil
}

// UpdateScheduledTaskTime updates the scheduled time for a task.
func (db *DB) UpdateScheduledTaskTime(ctx context.Context, taskID string, scheduledAt time.Time) error {
	err := db.queries.UpdateScheduledTaskTime(ctx, sqlc.UpdateScheduledTaskTimeParams{
		ID:          taskID,
		ScheduledAt: scheduledAt,
	})
	if err != nil {
		return fmt.Errorf("failed to update scheduled task time: %w", err)
	}
	return nil
}

// MarkTaskExecuting marks a task as currently executing.
func (db *DB) MarkTaskExecuting(ctx context.Context, taskID string) error {
	err := db.queries.MarkTaskExecuting(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to mark task as executing: %w", err)
	}
	return nil
}

// MarkTaskCompleted marks a task as completed.
func (db *DB) MarkTaskCompleted(ctx context.Context, taskID string) error {
	err := db.queries.MarkTaskCompleted(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to mark task as completed: %w", err)
	}
	return nil
}

// MarkTaskFailed marks a task as failed with an error message.
func (db *DB) MarkTaskFailed(ctx context.Context, taskID string, errorMessage string) error {
	err := db.queries.MarkTaskFailed(ctx, sqlc.MarkTaskFailedParams{
		ID:           taskID,
		ErrorMessage: sql.NullString{String: errorMessage, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to mark task as failed: %w", err)
	}
	return nil
}

// ScheduleOrUpdateTask schedules a new task or updates existing one.
// If a pending task of the same type exists, it updates the scheduled time.
// Otherwise, it creates a new scheduled task.
func (db *DB) ScheduleOrUpdateTask(ctx context.Context, workID string, taskType string, scheduledAt time.Time, metadata map[string]string) (*ScheduledTask, error) {
	// Check if there's already a pending task of this type
	existing, err := db.GetPendingTaskByType(ctx, workID, taskType)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		// Update the existing task's scheduled time
		err = db.UpdateScheduledTaskTime(ctx, existing.ID, scheduledAt)
		if err != nil {
			return nil, err
		}
		existing.ScheduledAt = scheduledAt
		return existing, nil
	}

	// Create a new scheduled task
	return db.ScheduleTask(ctx, workID, taskType, scheduledAt, metadata)
}

// TriggerTaskNow schedules a task to run immediately.
func (db *DB) TriggerTaskNow(ctx context.Context, workID string, taskType string, metadata map[string]string) (*ScheduledTask, error) {
	return db.ScheduleOrUpdateTask(ctx, workID, taskType, time.Now(), metadata)
}

// WatchSchedulerChanges returns tasks that have been updated since the given time.
func (db *DB) WatchSchedulerChanges(ctx context.Context, since time.Time) ([]*ScheduledTask, error) {
	tasks, err := db.queries.WatchSchedulerChanges(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("failed to watch scheduler changes: %w", err)
	}

	result := make([]*ScheduledTask, len(tasks))
	for i, t := range tasks {
		result[i] = schedulerToLocal(&t)
	}
	return result, nil
}

// CleanupOldTasks removes completed/failed tasks older than the specified duration.
func (db *DB) CleanupOldTasks(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	err := db.queries.DeleteCompletedTasksOlderThan(ctx, sql.NullTime{Time: cutoff, Valid: true})
	if err != nil {
		return fmt.Errorf("failed to cleanup old tasks: %w", err)
	}
	return nil
}

// Helper function to convert SQLC Scheduler to local ScheduledTask
func schedulerToLocal(s *sqlc.Scheduler) *ScheduledTask {
	task := &ScheduledTask{
		ID:          s.ID,
		WorkID:      s.WorkID,
		TaskType:    s.TaskType,
		ScheduledAt: s.ScheduledAt,
		Status:      s.Status,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}

	if s.ExecutedAt.Valid {
		task.ExecutedAt = &s.ExecutedAt.Time
	}

	if s.ErrorMessage.Valid {
		task.ErrorMessage = &s.ErrorMessage.String
	}

	// Parse metadata JSON
	task.Metadata = make(map[string]string)
	if s.Metadata != "" && s.Metadata != "{}" {
		_ = json.Unmarshal([]byte(s.Metadata), &task.Metadata)
	}

	return task
}