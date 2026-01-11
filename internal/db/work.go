package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/newhook/co/internal/db/sqlc"
)

// workToLocal converts an sqlc.Work to local Work
func workToLocal(w *sqlc.Work) *Work {
	work := &Work{
		ID:            w.ID,
		Status:        w.Status,
		ZellijSession: w.ZellijSession.String,
		ZellijTab:     w.ZellijTab.String,
		WorktreePath:  w.WorktreePath.String,
		BranchName:    w.BranchName.String,
		PRURL:         w.PrUrl.String,
		ErrorMessage:  w.ErrorMessage.String,
	}
	if w.StartedAt.Valid {
		work.StartedAt = &w.StartedAt.Time
	}
	if w.CompletedAt.Valid {
		work.CompletedAt = &w.CompletedAt.Time
	}
	if w.CreatedAt.Valid {
		work.CreatedAt = w.CreatedAt.Time
	}
	return work
}

// Work represents a work unit (group of tasks) in the database.
type Work struct {
	ID            string
	Status        string
	ZellijSession string
	ZellijTab     string
	WorktreePath  string
	BranchName    string
	PRURL         string
	ErrorMessage  string
	StartedAt     *time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
}

// CreateWork creates a new work unit.
func (db *DB) CreateWork(id, worktreePath, branchName string) error {
	err := db.queries.CreateWork(context.Background(), sqlc.CreateWorkParams{
		ID:           id,
		WorktreePath: nullString(worktreePath),
		BranchName:   nullString(branchName),
	})
	if err != nil {
		return fmt.Errorf("failed to create work %s: %w", id, err)
	}
	return nil
}

// StartWork marks a work as processing with session info.
func (db *DB) StartWork(id, zellijSession, zellijTab string) error {
	now := time.Now()
	rows, err := db.queries.StartWork(context.Background(), sqlc.StartWorkParams{
		ZellijSession: nullString(zellijSession),
		ZellijTab:     nullString(zellijTab),
		StartedAt:     nullTime(now),
		ID:            id,
	})
	if err != nil {
		return fmt.Errorf("failed to start work %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("work %s not found", id)
	}
	return nil
}

// CompleteWork marks a work as completed with a PR URL.
func (db *DB) CompleteWork(id, prURL string) error {
	now := time.Now()
	rows, err := db.queries.CompleteWork(context.Background(), sqlc.CompleteWorkParams{
		PrUrl:       nullString(prURL),
		CompletedAt: nullTime(now),
		ID:          id,
	})
	if err != nil {
		return fmt.Errorf("failed to complete work %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("work %s not found", id)
	}
	return nil
}

// FailWork marks a work as failed with an error message.
func (db *DB) FailWork(id, errMsg string) error {
	now := time.Now()
	rows, err := db.queries.FailWork(context.Background(), sqlc.FailWorkParams{
		ErrorMessage: nullString(errMsg),
		CompletedAt:  nullTime(now),
		ID:           id,
	})
	if err != nil {
		return fmt.Errorf("failed to mark work %s as failed: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("work %s not found", id)
	}
	return nil
}

// GetWork retrieves a work by ID.
func (db *DB) GetWork(id string) (*Work, error) {
	work, err := db.queries.GetWork(context.Background(), id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	return workToLocal(&work), nil
}

// ListWorks returns all works, optionally filtered by status.
func (db *DB) ListWorks(statusFilter string) ([]*Work, error) {
	var works []sqlc.Work
	var err error

	if statusFilter == "" {
		works, err = db.queries.ListWorks(context.Background())
	} else {
		works, err = db.queries.ListWorksByStatus(context.Background(), statusFilter)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list works: %w", err)
	}

	result := make([]*Work, len(works))
	for i, w := range works {
		result[i] = workToLocal(&w)
	}
	return result, nil
}

// GetLastWorkID returns the ID of the most recently created work.
func (db *DB) GetLastWorkID() (string, error) {
	id, err := db.queries.GetLastWorkID(context.Background())
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get last work ID: %w", err)
	}
	return id, nil
}

// GetWorkByDirectory returns the work that has a worktree path matching the pattern.
func (db *DB) GetWorkByDirectory(pathPattern string) (*Work, error) {
	work, err := db.queries.GetWorkByDirectory(context.Background(), nullString(pathPattern))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get work by directory: %w", err)
	}
	return workToLocal(&work), nil
}

// AddTaskToWork associates a task with a work.
func (db *DB) AddTaskToWork(workID, taskID string, position int) error {
	err := db.queries.AddTaskToWork(context.Background(), sqlc.AddTaskToWorkParams{
		WorkID:   workID,
		TaskID:   taskID,
		Position: sql.NullInt64{Int64: int64(position), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to add task %s to work %s: %w", taskID, workID, err)
	}
	return nil
}

// GetWorkTasks returns all tasks for a work in order.
func (db *DB) GetWorkTasks(workID string) ([]*Task, error) {
	tasks, err := db.queries.GetWorkTasks(context.Background(), workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work tasks: %w", err)
	}

	result := make([]*Task, len(tasks))
	for i, t := range tasks {
		result[i] = taskToLocal(&t)
	}
	return result, nil
}

// IsWorkCompleted checks if all tasks in a work are completed.
func (db *DB) IsWorkCompleted(workID string) (bool, error) {
	var total, completed int
	err := db.QueryRow(`
		SELECT COUNT(*), COUNT(CASE WHEN t.status = ? THEN 1 END)
		FROM work_tasks wt
		JOIN tasks t ON wt.task_id = t.id
		WHERE wt.work_id = ?
	`, StatusCompleted, workID).Scan(&total, &completed)
	if err != nil {
		return false, fmt.Errorf("failed to check work completion: %w", err)
	}
	if total == 0 {
		return false, nil
	}
	return completed == total, nil
}

// GenerateNextWorkID generates the next available work ID.
func (db *DB) GenerateNextWorkID() (string, error) {
	lastID, err := db.GetLastWorkID()
	if err != nil {
		return "", err
	}

	// If no works exist yet, start with work-1
	if lastID == "" {
		return "work-1", nil
	}

	// Parse the number from the last ID (format: "work-N")
	parts := strings.Split(lastID, "-")
	if len(parts) < 2 {
		return "work-1", nil
	}

	var nextNum int
	if _, err := fmt.Sscanf(parts[1], "%d", &nextNum); err != nil {
		return "work-1", nil
	}

	nextNum++
	return fmt.Sprintf("work-%d", nextNum), nil
}