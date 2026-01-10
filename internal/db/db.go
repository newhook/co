package db

import (
	"database/sql"
	"fmt"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// Status constants for bead tracking.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

// DB wraps the SQLite database connection.
type DB struct {
	*sql.DB
}

// OpenPath initializes the database at the specified path and creates the schema.
func OpenPath(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &DB{db}, nil
}

// createSchema creates the beads table if it doesn't exist.
func createSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS beads (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL DEFAULT 'pending',
		title TEXT,
		pr_url TEXT,
		error_message TEXT,
		zellij_session TEXT,
		zellij_pane TEXT,
		worktree_path TEXT,
		started_at DATETIME,
		completed_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_beads_status ON beads(status);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Migrate existing databases: add worktree_path column if missing
	if err := migrateWorktreePath(db); err != nil {
		return err
	}

	return nil
}

// migrateWorktreePath adds the worktree_path column if it doesn't exist.
func migrateWorktreePath(db *sql.DB) error {
	// Check if column exists
	rows, err := db.Query("PRAGMA table_info(beads)")
	if err != nil {
		return fmt.Errorf("failed to check table info: %w", err)
	}
	defer rows.Close()

	hasWorktreePath := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}
		if name == "worktree_path" {
			hasWorktreePath = true
			break
		}
	}

	if !hasWorktreePath {
		_, err := db.Exec("ALTER TABLE beads ADD COLUMN worktree_path TEXT")
		if err != nil {
			return fmt.Errorf("failed to add worktree_path column: %w", err)
		}
	}

	return nil
}
