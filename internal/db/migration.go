package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	cosignal "github.com/newhook/co/internal/signal"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration represents a single migration file
type Migration struct {
	Version string
	Name    string
	UpSQL   string
	DownSQL string
}

// RunMigrations applies all pending migrations from the embedded migrationsFS
func RunMigrations(ctx context.Context, db *sql.DB) error {
	return RunMigrationsForFS(ctx, db, migrationsFS)
}

// RunMigrationsForFS applies all pending migrations from the specified filesystem
func RunMigrationsForFS(ctx context.Context, db *sql.DB, fsys embed.FS) error {
	// Create migrations table if it doesn't exist
	if err := createMigrationsTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get list of applied migrations
	applied, err := getAppliedMigrations(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Read all migration files
	migrations, err := readMigrationsFromFS(fsys)
	if err != nil {
		return fmt.Errorf("failed to read migrations: %w", err)
	}

	// Apply pending migrations with signal blocking to prevent inconsistent state
	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}

		// Block signals during migration to ensure atomicity
		cosignal.BlockSignals()
		fmt.Printf("Applying migration %s: %s\n", m.Version, m.Name)
		if err := applyMigration(ctx, db, m); err != nil {
			cosignal.UnblockSignals()
			return fmt.Errorf("failed to apply migration %s: %w", m.Version, err)
		}
		cosignal.UnblockSignals()
	}

	return nil
}

// RollbackMigration rolls back the last applied migration
func RollbackMigration(db *sql.DB) error {
	return RollbackMigrationForFS(context.Background(), db, migrationsFS)
}

// RollbackMigrationForFS rolls back the last applied migration from the specified filesystem
func RollbackMigrationForFS(ctx context.Context, db *sql.DB, fsys embed.FS) error {
	// Get the last applied migration
	var version string
	err := db.QueryRowContext(ctx, `
		SELECT version FROM schema_migrations
		ORDER BY version DESC
		LIMIT 1
	`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no migrations to rollback")
	}
	if err != nil {
		return fmt.Errorf("failed to get last migration: %w", err)
	}

	// Read all migrations to find the one to rollback
	migrations, err := readMigrationsFromFS(fsys)
	if err != nil {
		return fmt.Errorf("failed to read migrations: %w", err)
	}

	var migration *Migration
	for _, m := range migrations {
		if m.Version == version {
			migration = &m
			break
		}
	}

	if migration == nil {
		return fmt.Errorf("migration %s not found", version)
	}

	if migration.DownSQL == "" {
		return fmt.Errorf("migration %s has no down script", version)
	}

	fmt.Printf("Rolling back migration %s: %s\n", migration.Version, migration.Name)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute down statements
	statements := splitSQLStatements(migration.DownSQL)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to execute statement: %w", err)
		}
	}

	// Remove migration record
	if _, err := tx.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = ?", version); err != nil {
		return fmt.Errorf("failed to delete migration record: %w", err)
	}

	return tx.Commit()
}

func createMigrationsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

func getAppliedMigrations(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func readMigrationsFromFS(fsys embed.FS) ([]Migration, error) {
	var migrations []Migration

	// Walk the entire embedded filesystem to find SQL files
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}

		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		// Extract just the filename from the path
		filename := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			filename = path[idx+1:]
		}

		// Parse version and name from filename (e.g., "001_initial.sql")
		parts := strings.SplitN(strings.TrimSuffix(filename, ".sql"), "_", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid migration filename: %s", filename)
		}

		migration := Migration{
			Version: parts[0],
			Name:    parts[1],
			UpSQL:   parseUpSection(string(content)),
			DownSQL: parseDownSection(string(content)),
		}
		migrations = append(migrations, migration)
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func parseUpSection(content string) string {
	lines := strings.Split(content, "\n")
	var upLines []string
	inUp := false

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "-- +up") {
			inUp = true
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "-- +down") {
			break
		}
		if inUp {
			upLines = append(upLines, line)
		}
	}

	return strings.Join(upLines, "\n")
}

func parseDownSection(content string) string {
	lines := strings.Split(content, "\n")
	var downLines []string
	inDown := false

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "-- +down") {
			inDown = true
			continue
		}
		if inDown {
			downLines = append(downLines, line)
		}
	}

	return strings.Join(downLines, "\n")
}

func applyMigration(ctx context.Context, db *sql.DB, m Migration) error {
	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute up statements
	statements := splitSQLStatements(m.UpSQL)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to execute statement: %w", err)
		}
	}

	// Record migration
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", m.Version); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

// splitSQLStatements splits SQL content into individual statements
// It properly handles semicolons within strings and comments
func splitSQLStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	inString := false
	var stringChar rune
	inLineComment := false
	inBlockComment := false

	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		// Handle line comments
		if !inString && !inBlockComment && char == '-' && next == '-' {
			inLineComment = true
			current.WriteRune(char)
			continue
		}
		if inLineComment {
			current.WriteRune(char)
			if char == '\n' {
				inLineComment = false
			}
			continue
		}

		// Handle block comments
		if !inString && !inLineComment && char == '/' && next == '*' {
			inBlockComment = true
			current.WriteRune(char)
			continue
		}
		if inBlockComment {
			current.WriteRune(char)
			if char == '*' && next == '/' {
				current.WriteRune(next)
				i++ // Skip the next character
				inBlockComment = false
			}
			continue
		}

		// Handle strings
		if !inLineComment && !inBlockComment {
			if !inString && (char == '\'' || char == '"' || char == '`') {
				inString = true
				stringChar = char
			} else if inString && char == stringChar {
				// Check if it's escaped
				escaped := false
				for j := i - 1; j >= 0 && runes[j] == '\\'; j-- {
					escaped = !escaped
				}
				if !escaped {
					inString = false
				}
			}
		}

		// Handle statement separator
		if !inString && !inLineComment && !inBlockComment && char == ';' {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			continue
		}

		current.WriteRune(char)
	}

	// Add any remaining statement
	if stmt := strings.TrimSpace(current.String()); stmt != "" {
		statements = append(statements, stmt)
	}

	return statements
}

// MigrationStatus returns the current migration status
func MigrationStatus(db *sql.DB) ([]string, error) {
	return MigrationStatusContext(context.Background(), db)
}

// MigrationStatusContext returns the current migration status with context
func MigrationStatusContext(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil // No migrations applied yet
		}
		return nil, err
	}
	defer rows.Close()

	var versions []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}

	return versions, rows.Err()
}
