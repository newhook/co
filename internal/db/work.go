package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
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
func (db *DB) CreateWork(ctx context.Context, id, worktreePath, branchName string) error {
	err := db.queries.CreateWork(ctx, sqlc.CreateWorkParams{
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
func (db *DB) StartWork(ctx context.Context, id, zellijSession, zellijTab string) error {
	now := time.Now()
	rows, err := db.queries.StartWork(ctx, sqlc.StartWorkParams{
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
func (db *DB) CompleteWork(ctx context.Context, id, prURL string) error {
	now := time.Now()
	rows, err := db.queries.CompleteWork(ctx, sqlc.CompleteWorkParams{
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
func (db *DB) FailWork(ctx context.Context, id, errMsg string) error {
	now := time.Now()
	rows, err := db.queries.FailWork(ctx, sqlc.FailWorkParams{
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
func (db *DB) GetWork(ctx context.Context, id string) (*Work, error) {
	work, err := db.queries.GetWork(ctx, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get work: %w", err)
	}
	return workToLocal(&work), nil
}

// ListWorks returns all works, optionally filtered by status.
func (db *DB) ListWorks(ctx context.Context, statusFilter string) ([]*Work, error) {
	var works []sqlc.Work
	var err error

	if statusFilter == "" {
		works, err = db.queries.ListWorks(ctx)
	} else {
		works, err = db.queries.ListWorksByStatus(ctx, statusFilter)
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
func (db *DB) GetLastWorkID(ctx context.Context) (string, error) {
	id, err := db.queries.GetLastWorkID(ctx)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get last work ID: %w", err)
	}
	return id, nil
}

// GetWorkByDirectory returns the work that has a worktree path matching the pattern.
func (db *DB) GetWorkByDirectory(ctx context.Context, pathPattern string) (*Work, error) {
	work, err := db.queries.GetWorkByDirectory(ctx, nullString(pathPattern))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get work by directory: %w", err)
	}
	return workToLocal(&work), nil
}

// AddTaskToWork associates a task with a work.
func (db *DB) AddTaskToWork(ctx context.Context, workID, taskID string, position int) error {
	err := db.queries.AddTaskToWork(ctx, sqlc.AddTaskToWorkParams{
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
func (db *DB) GetWorkTasks(ctx context.Context, workID string) ([]*Task, error) {
	tasks, err := db.queries.GetWorkTasks(ctx, workID)
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

// generateRandomSuffix generates a random alphanumeric suffix.
func generateRandomSuffix(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based generation if random fails
		return fmt.Sprintf("%x", time.Now().UnixNano())[:length]
	}
	for i := range b {
		b[i] = chars[b[i]%byte(len(chars))]
	}
	return string(b)
}

// GenerateNextWorkID generates the next available work ID with format "w-XXX".
// This uses a pattern similar to beads IDs for consistency.
func (db *DB) GenerateNextWorkID(ctx context.Context) (string, error) {
	// Try up to 10 times to generate a unique ID
	for i := 0; i < 10; i++ {
		// Generate ID with format "w-XXX" (w for work)
		suffix := generateRandomSuffix(3)
		workID := fmt.Sprintf("w-%s", suffix)

		// Check if this ID already exists
		existing, err := db.GetWork(ctx, workID)
		if err != nil {
			return "", fmt.Errorf("failed to check for existing work: %w", err)
		}
		if existing == nil {
			// ID is unique, return it
			return workID, nil
		}
	}

	// If we couldn't generate a unique ID in 10 tries, fall back to timestamp
	return fmt.Sprintf("w-%d", time.Now().UnixNano()/1000000), nil
}