package db

import (
	"database/sql"
	"fmt"
	"time"
)

// TrackedBead represents a bead tracking record in the database.
type TrackedBead struct {
	ID            string
	Status        string
	Title         string
	PRURL         string
	ErrorMessage  string
	ZellijSession string
	ZellijPane    string
	StartedAt     *time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// StartBead marks a bead as processing with session info.
func (db *DB) StartBead(id, title, zellijSession, zellijPane string) error {
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO beads (id, status, title, zellij_session, zellij_pane, started_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = ?,
			title = ?,
			zellij_session = ?,
			zellij_pane = ?,
			started_at = ?,
			updated_at = ?
	`, id, StatusProcessing, title, zellijSession, zellijPane, now, now,
		StatusProcessing, title, zellijSession, zellijPane, now, now)
	if err != nil {
		return fmt.Errorf("failed to start bead %s: %w", id, err)
	}
	return nil
}

// CompleteBead marks a bead as completed with a PR URL.
func (db *DB) CompleteBead(id, prURL string) error {
	now := time.Now()
	result, err := db.Exec(`
		UPDATE beads SET status = ?, pr_url = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, StatusCompleted, prURL, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to complete bead %s: %w", id, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("bead %s not found", id)
	}
	return nil
}

// FailBead marks a bead as failed with an error message.
func (db *DB) FailBead(id, errMsg string) error {
	now := time.Now()
	result, err := db.Exec(`
		UPDATE beads SET status = ?, error_message = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, StatusFailed, errMsg, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to mark bead %s as failed: %w", id, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("bead %s not found", id)
	}
	return nil
}

// GetBead retrieves a tracking record by ID.
func (db *DB) GetBead(id string) (*TrackedBead, error) {
	row := db.QueryRow(`
		SELECT id, status, title, pr_url, error_message, zellij_session, zellij_pane,
		       started_at, completed_at, created_at, updated_at
		FROM beads WHERE id = ?
	`, id)

	return scanBead(row)
}

// IsCompleted checks if a bead is completed or failed.
func (db *DB) IsCompleted(id string) (bool, error) {
	var status string
	err := db.QueryRow(`SELECT status FROM beads WHERE id = ?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check bead status: %w", err)
	}
	return status == StatusCompleted || status == StatusFailed, nil
}

// ListBeads returns all beads, optionally filtered by status.
func (db *DB) ListBeads(statusFilter string) ([]*TrackedBead, error) {
	var rows *sql.Rows
	var err error

	if statusFilter == "" {
		rows, err = db.Query(`
			SELECT id, status, title, pr_url, error_message, zellij_session, zellij_pane,
			       started_at, completed_at, created_at, updated_at
			FROM beads ORDER BY created_at DESC
		`)
	} else {
		rows, err = db.Query(`
			SELECT id, status, title, pr_url, error_message, zellij_session, zellij_pane,
			       started_at, completed_at, created_at, updated_at
			FROM beads WHERE status = ? ORDER BY created_at DESC
		`, statusFilter)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list beads: %w", err)
	}
	defer rows.Close()

	var beads []*TrackedBead
	for rows.Next() {
		bead, err := scanBeadRow(rows)
		if err != nil {
			return nil, err
		}
		beads = append(beads, bead)
	}
	return beads, rows.Err()
}

// GetBeadSession returns the zellij session and pane for a bead.
func (db *DB) GetBeadSession(id string) (session, pane string, err error) {
	err = db.QueryRow(`
		SELECT zellij_session, zellij_pane FROM beads WHERE id = ?
	`, id).Scan(&session, &pane)
	if err == sql.ErrNoRows {
		return "", "", fmt.Errorf("bead %s not found", id)
	}
	if err != nil {
		return "", "", fmt.Errorf("failed to get bead session: %w", err)
	}
	return session, pane, nil
}

// scanBead scans a single row into a TrackedBead.
func scanBead(row *sql.Row) (*TrackedBead, error) {
	var b TrackedBead
	var prURL, errMsg, session, pane sql.NullString
	var startedAt, completedAt sql.NullTime

	err := row.Scan(&b.ID, &b.Status, &b.Title, &prURL, &errMsg, &session, &pane,
		&startedAt, &completedAt, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan bead: %w", err)
	}

	b.PRURL = prURL.String
	b.ErrorMessage = errMsg.String
	b.ZellijSession = session.String
	b.ZellijPane = pane.String
	if startedAt.Valid {
		b.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		b.CompletedAt = &completedAt.Time
	}
	return &b, nil
}

// scanBeadRow scans a row from Rows into a TrackedBead.
func scanBeadRow(rows *sql.Rows) (*TrackedBead, error) {
	var b TrackedBead
	var prURL, errMsg, session, pane sql.NullString
	var startedAt, completedAt sql.NullTime

	err := rows.Scan(&b.ID, &b.Status, &b.Title, &prURL, &errMsg, &session, &pane,
		&startedAt, &completedAt, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to scan bead: %w", err)
	}

	b.PRURL = prURL.String
	b.ErrorMessage = errMsg.String
	b.ZellijSession = session.String
	b.ZellijPane = pane.String
	if startedAt.Valid {
		b.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		b.CompletedAt = &completedAt.Time
	}
	return &b, nil
}
