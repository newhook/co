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

// createSchema creates the database tables if they don't exist.
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

	-- Tasks table: tracks virtual tasks (groups of beads)
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL DEFAULT 'pending',
		complexity_budget INT,
		actual_complexity INT,
		zellij_session TEXT,
		zellij_pane TEXT,
		worktree_path TEXT,
		pr_url TEXT,
		error_message TEXT,
		started_at DATETIME,
		completed_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);

	-- Task-beads junction table: links tasks to their beads
	CREATE TABLE IF NOT EXISTS task_beads (
		task_id TEXT NOT NULL,
		bead_id TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		PRIMARY KEY (task_id, bead_id),
		FOREIGN KEY (task_id) REFERENCES tasks(id)
	);

	CREATE INDEX IF NOT EXISTS idx_task_beads_task_id ON task_beads(task_id);
	CREATE INDEX IF NOT EXISTS idx_task_beads_bead_id ON task_beads(bead_id);

	-- Complexity cache: stores LLM complexity estimates
	CREATE TABLE IF NOT EXISTS complexity_cache (
		bead_id TEXT PRIMARY KEY,
		description_hash TEXT NOT NULL,
		complexity_score INT NOT NULL,
		estimated_tokens INT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_complexity_cache_hash ON complexity_cache(description_hash);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Migrate existing databases: add worktree_path column if missing
	if err := migrateWorktreePath(db); err != nil {
		return err
	}

	// Migrate existing databases: add task_type column if missing
	if err := migrateTaskType(db); err != nil {
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

// migrateTaskType adds the task_type column if it doesn't exist.
func migrateTaskType(db *sql.DB) error {
	// Check if column exists in tasks table
	rows, err := db.Query("PRAGMA table_info(tasks)")
	if err != nil {
		return fmt.Errorf("failed to check table info: %w", err)
	}
	defer rows.Close()

	hasTaskType := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}
		if name == "task_type" {
			hasTaskType = true
			break
		}
	}

	if !hasTaskType {
		_, err := db.Exec("ALTER TABLE tasks ADD COLUMN task_type TEXT NOT NULL DEFAULT 'implement'")
		if err != nil {
			return fmt.Errorf("failed to add task_type column: %w", err)
		}
	}

	return nil
}
