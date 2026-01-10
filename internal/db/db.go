package db

import (
	"database/sql"
	"fmt"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/newhook/co/internal/db/sqlc"
)

// Status constants for bead tracking.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

// DB wraps the SQLite database connection and sqlc queries.
type DB struct {
	*sql.DB
	queries *sqlc.Queries
}

// OpenPath initializes the database at the specified path and runs migrations.
func OpenPath(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Run migrations instead of creating schema directly
	if err := RunMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &DB{
		DB:      db,
		queries: sqlc.New(db),
	}, nil
}

