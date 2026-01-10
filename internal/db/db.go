package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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

// Open initializes the database connection and creates the schema.
// Uses CO_DB_PATH env var if set, otherwise ~/.config/co/tracking.db.
func Open() (*DB, error) {
	dbPath := os.Getenv("CO_DB_PATH")
	if dbPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		configDir := filepath.Join(homeDir, ".config", "co")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}
		dbPath = filepath.Join(configDir, "tracking.db")
	}

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
		started_at DATETIME,
		completed_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_beads_status ON beads(status);
	`
	_, err := db.Exec(schema)
	return err
}
