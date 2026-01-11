package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math/big"
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
		BaseBranch:    w.BaseBranch.String,
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
	BaseBranch    string
	PRURL         string
	ErrorMessage  string
	StartedAt     *time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
}

// CreateWork creates a new work unit.
func (db *DB) CreateWork(ctx context.Context, id, worktreePath, branchName, baseBranch string) error {
	// Use transaction to create work and initialize counter atomically
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.queries.WithTx(tx)

	err = qtx.CreateWork(ctx, sqlc.CreateWorkParams{
		ID:           id,
		WorktreePath: nullString(worktreePath),
		BranchName:   nullString(branchName),
		BaseBranch:   nullString(baseBranch),
	})
	if err != nil {
		return fmt.Errorf("failed to create work %s: %w", id, err)
	}

	// Initialize task counter for this work
	err = qtx.InitializeTaskCounter(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to initialize task counter for work %s: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
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

// toBase36 converts a byte array to a base36 string.
func toBase36(bytes []byte) string {
	// Convert bytes to a big integer
	num := new(big.Int).SetBytes(bytes)
	// Convert to base36
	return strings.ToLower(num.Text(36))
}

// GenerateWorkID generates a content-based hash ID for a work.
// Uses the branch name as the primary content for hashing.
func (db *DB) GenerateWorkID(ctx context.Context, branchName string, projectName string) (string, error) {
	// Start with a base length of 3 characters for work IDs
	// (we expect fewer works than issues)
	baseLength := 3
	maxAttempts := 30

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Calculate target length based on attempts
		targetLength := baseLength + (attempt / 10)
		if targetLength > 8 {
			targetLength = 8 // Cap at 8 characters
		}

		// Create content for hashing
		nonce := attempt % 10
		content := fmt.Sprintf("%s:%s:%d:%d",
			branchName,
			projectName,
			time.Now().UnixNano(),
			nonce)

		// Generate SHA256 hash
		hash := sha256.Sum256([]byte(content))

		// Convert to base36 and truncate to target length
		hashStr := toBase36(hash[:])
		if len(hashStr) > targetLength {
			hashStr = hashStr[:targetLength]
		}

		// Create work ID with prefix
		workID := fmt.Sprintf("w-%s", hashStr)

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

	// If we exhausted all attempts, generate a fallback ID
	return fmt.Sprintf("w-%d", time.Now().UnixNano()/1000000), nil
}

// GenerateNextWorkID generates a work ID using content-based hashing.
// This is a compatibility wrapper that generates a temporary ID.
func (db *DB) GenerateNextWorkID(ctx context.Context) (string, error) {
	// Generate a temporary work ID based on timestamp
	// This should only be used when we don't have branch information yet
	return db.GenerateWorkID(ctx, fmt.Sprintf("temp-%d", time.Now().UnixNano()), "unknown")
}

// GetNextTaskNumber returns the next available task number for a work.
// Tasks are numbered sequentially within each work (w-abc.1, w-abc.2, etc.)
// This uses an atomic counter to avoid race conditions.
func (db *DB) GetNextTaskNumber(ctx context.Context, workID string) (int, error) {
	// First ensure the counter exists (for backwards compatibility with existing works)
	err := db.queries.InitializeTaskCounter(ctx, workID)
	if err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		return 0, fmt.Errorf("failed to initialize task counter: %w", err)
	}

	// Get and increment the counter atomically
	taskNum, err := db.queries.GetAndIncrementTaskCounter(ctx, workID)
	if err != nil {
		return 0, fmt.Errorf("failed to get next task number: %w", err)
	}
	return int(taskNum), nil
}

// DeleteWork deletes a work and all associated records.
// This includes:
// - Task beads associations for all tasks in the work
// - Tasks belonging to the work
// - Work-task relationships
// - The work record itself
func (db *DB) DeleteWork(ctx context.Context, workID string) error {
	// Start a transaction to ensure all deletes happen atomically
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.queries.WithTx(tx)

	// Delete task_beads entries for all tasks in this work
	if _, err := qtx.DeleteTaskBeadsForWork(ctx, workID); err != nil {
		return fmt.Errorf("failed to delete task beads for work %s: %w", workID, err)
	}

	// Delete work_tasks relationships
	if _, err := qtx.DeleteWorkTasks(ctx, workID); err != nil {
		return fmt.Errorf("failed to delete work tasks for work %s: %w", workID, err)
	}

	// Delete all tasks belonging to this work
	if _, err := qtx.DeleteTasksForWork(ctx, sql.NullString{String: workID, Valid: true}); err != nil {
		return fmt.Errorf("failed to delete tasks for work %s: %w", workID, err)
	}

	// Finally, delete the work itself
	rows, err := qtx.DeleteWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to delete work %s: %w", workID, err)
	}
	if rows == 0 {
		return fmt.Errorf("work %s not found", workID)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}