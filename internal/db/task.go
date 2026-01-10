package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Task represents a virtual task (group of beads) in the database.
type Task struct {
	ID               string
	Status           string
	TaskType         string
	ComplexityBudget int
	ActualComplexity int
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
func (db *DB) CreateTask(id string, taskType string, beadIDs []string, complexityBudget int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert task
	_, err = tx.Exec(`
		INSERT INTO tasks (id, status, task_type, complexity_budget)
		VALUES (?, ?, ?, ?)
	`, id, StatusPending, taskType, complexityBudget)
	if err != nil {
		return fmt.Errorf("failed to create task %s: %w", id, err)
	}

	// Insert task_beads
	for _, beadID := range beadIDs {
		_, err = tx.Exec(`
			INSERT INTO task_beads (task_id, bead_id, status)
			VALUES (?, ?, ?)
		`, id, beadID, StatusPending)
		if err != nil {
			return fmt.Errorf("failed to add bead %s to task %s: %w", beadID, id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// StartTask marks a task as processing with session info.
func (db *DB) StartTask(id, zellijSession, zellijPane, worktreePath string) error {
	now := time.Now()
	result, err := db.Exec(`
		UPDATE tasks SET
			status = ?,
			zellij_session = ?,
			zellij_pane = ?,
			worktree_path = ?,
			started_at = ?
		WHERE id = ?
	`, StatusProcessing, zellijSession, zellijPane, worktreePath, now, id)
	if err != nil {
		return fmt.Errorf("failed to start task %s: %w", id, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// CompleteTask marks a task as completed with a PR URL.
func (db *DB) CompleteTask(id, prURL string) error {
	now := time.Now()
	result, err := db.Exec(`
		UPDATE tasks SET status = ?, pr_url = ?, completed_at = ?
		WHERE id = ?
	`, StatusCompleted, prURL, now, id)
	if err != nil {
		return fmt.Errorf("failed to complete task %s: %w", id, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// FailTask marks a task as failed with an error message.
func (db *DB) FailTask(id, errMsg string) error {
	now := time.Now()
	result, err := db.Exec(`
		UPDATE tasks SET status = ?, error_message = ?, completed_at = ?
		WHERE id = ?
	`, StatusFailed, errMsg, now, id)
	if err != nil {
		return fmt.Errorf("failed to mark task %s as failed: %w", id, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// GetTask retrieves a task by ID.
func (db *DB) GetTask(id string) (*Task, error) {
	row := db.QueryRow(`
		SELECT id, status, COALESCE(task_type, 'implement') as task_type,
		       complexity_budget, actual_complexity, zellij_session, zellij_pane,
		       worktree_path, pr_url, error_message, started_at, completed_at, created_at
		FROM tasks WHERE id = ?
	`, id)

	var t Task
	var budget, actual sql.NullInt64
	var session, pane, worktree, prURL, errMsg sql.NullString
	var startedAt, completedAt sql.NullTime

	err := row.Scan(&t.ID, &t.Status, &t.TaskType, &budget, &actual, &session, &pane,
		&worktree, &prURL, &errMsg, &startedAt, &completedAt, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan task: %w", err)
	}

	t.ComplexityBudget = int(budget.Int64)
	t.ActualComplexity = int(actual.Int64)
	t.ZellijSession = session.String
	t.ZellijPane = pane.String
	t.WorktreePath = worktree.String
	t.PRURL = prURL.String
	t.ErrorMessage = errMsg.String
	if startedAt.Valid {
		t.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		t.CompletedAt = &completedAt.Time
	}
	return &t, nil
}

// GetTaskBeads returns all bead IDs for a task.
func (db *DB) GetTaskBeads(taskID string) ([]string, error) {
	rows, err := db.Query(`
		SELECT bead_id FROM task_beads WHERE task_id = ?
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task beads: %w", err)
	}
	defer rows.Close()

	var beadIDs []string
	for rows.Next() {
		var beadID string
		if err := rows.Scan(&beadID); err != nil {
			return nil, fmt.Errorf("failed to scan bead_id: %w", err)
		}
		beadIDs = append(beadIDs, beadID)
	}
	return beadIDs, rows.Err()
}

// GetTaskForBead returns the task ID that contains the given bead.
func (db *DB) GetTaskForBead(beadID string) (string, error) {
	var taskID string
	err := db.QueryRow(`
		SELECT task_id FROM task_beads WHERE bead_id = ?
	`, beadID).Scan(&taskID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get task for bead: %w", err)
	}
	return taskID, nil
}

// CompleteTaskBead marks a specific bead within a task as completed.
func (db *DB) CompleteTaskBead(taskID, beadID string) error {
	result, err := db.Exec(`
		UPDATE task_beads SET status = ? WHERE task_id = ? AND bead_id = ?
	`, StatusCompleted, taskID, beadID)
	if err != nil {
		return fmt.Errorf("failed to complete task bead: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task bead %s/%s not found", taskID, beadID)
	}
	return nil
}

// FailTaskBead marks a specific bead within a task as failed.
func (db *DB) FailTaskBead(taskID, beadID string) error {
	result, err := db.Exec(`
		UPDATE task_beads SET status = ? WHERE task_id = ? AND bead_id = ?
	`, StatusFailed, taskID, beadID)
	if err != nil {
		return fmt.Errorf("failed to fail task bead: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task bead %s/%s not found", taskID, beadID)
	}
	return nil
}

// IsTaskCompleted checks if all beads in a task are completed.
// Returns true if all beads are completed (not failed), false otherwise.
func (db *DB) IsTaskCompleted(taskID string) (bool, error) {
	var total, completed int
	err := db.QueryRow(`
		SELECT COUNT(*), COUNT(CASE WHEN status = ? THEN 1 END)
		FROM task_beads WHERE task_id = ?
	`, StatusCompleted, taskID).Scan(&total, &completed)
	if err != nil {
		return false, fmt.Errorf("failed to check task completion: %w", err)
	}
	if total == 0 {
		return false, nil
	}
	return completed == total, nil
}

// CheckAndCompleteTask checks if all beads are completed and auto-completes the task.
// Returns true if the task was auto-completed, false otherwise.
func (db *DB) CheckAndCompleteTask(taskID, prURL string) (bool, error) {
	completed, err := db.IsTaskCompleted(taskID)
	if err != nil {
		return false, err
	}
	if !completed {
		return false, nil
	}

	// Auto-complete the task
	if err := db.CompleteTask(taskID, prURL); err != nil {
		return false, err
	}
	return true, nil
}

// ListTasks returns all tasks, optionally filtered by status.
func (db *DB) ListTasks(statusFilter string) ([]*Task, error) {
	var rows *sql.Rows
	var err error

	if statusFilter == "" {
		rows, err = db.Query(`
			SELECT id, status, COALESCE(task_type, 'implement') as task_type,
			       complexity_budget, actual_complexity, zellij_session, zellij_pane,
			       worktree_path, pr_url, error_message, started_at, completed_at, created_at
			FROM tasks ORDER BY created_at DESC
		`)
	} else {
		rows, err = db.Query(`
			SELECT id, status, COALESCE(task_type, 'implement') as task_type,
			       complexity_budget, actual_complexity, zellij_session, zellij_pane,
			       worktree_path, pr_url, error_message, started_at, completed_at, created_at
			FROM tasks WHERE status = ? ORDER BY created_at DESC
		`, statusFilter)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		var t Task
		var budget, actual sql.NullInt64
		var session, pane, worktree, prURL, errMsg sql.NullString
		var startedAt, completedAt sql.NullTime

		err := rows.Scan(&t.ID, &t.Status, &t.TaskType, &budget, &actual, &session, &pane,
			&worktree, &prURL, &errMsg, &startedAt, &completedAt, &t.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}

		t.ComplexityBudget = int(budget.Int64)
		t.ActualComplexity = int(actual.Int64)
		t.ZellijSession = session.String
		t.ZellijPane = pane.String
		t.WorktreePath = worktree.String
		t.PRURL = prURL.String
		t.ErrorMessage = errMsg.String
		if startedAt.Valid {
			t.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, &t)
	}
	return tasks, rows.Err()
}
