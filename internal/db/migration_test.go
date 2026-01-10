package db

import (
	"context"
	"database/sql"
	"embed"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

//go:embed test_migrations/*.sql
var testMigrationsFS embed.FS

// hasColumn checks if a table has a specific column
func hasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// tableExists checks if a table exists in the database
func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TestRunMigrations tests the basic migration functionality
func TestRunMigrations(t *testing.T) {
	// Create an in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Run migrations with test filesystem
	if err := RunMigrationsForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Verify schema_migrations table exists
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for schema_migrations table: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected schema_migrations table to exist, but it doesn't")
	}

	// Verify migrations were recorded
	versions, err := MigrationStatus(db)
	if err != nil {
		t.Fatalf("Failed to get migration status: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("Expected 2 migrations to be recorded, got %d", len(versions))
	}

	// Verify test tables were created (from test migrations)
	tables := []string{"test_table1", "test_table2"}
	for _, table := range tables {
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("Expected table %s to exist, but it doesn't", table)
		}
	}

	// Verify index was created
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_test_table1_name'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for index: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected index idx_test_table1_name to exist")
	}
}

// TestRunMigrationsIdempotent tests that running migrations multiple times is safe
func TestRunMigrationsIdempotent(t *testing.T) {
	// Create an in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Run migrations first time
	ctx := context.Background()
	if err := RunMigrationsForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to run migrations first time: %v", err)
	}

	// Run migrations second time - should be idempotent
	if err := RunMigrationsForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to run migrations second time: %v", err)
	}

	// Verify only one migration record exists
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = '001'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count migration records: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 migration record, got %d", count)
	}
}

// TestMigrationStatus tests the MigrationStatus function
func TestMigrationStatus(t *testing.T) {
	// Create an in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Check status before migrations
	versions, err := MigrationStatus(db)
	if err != nil {
		t.Fatalf("Failed to get migration status: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("Expected no migrations before running, got %v", versions)
	}

	// Run migrations
	ctx := context.Background()
	if err := RunMigrationsForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Check status after migrations
	versions, err = MigrationStatus(db)
	if err != nil {
		t.Fatalf("Failed to get migration status: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("Expected 2 migrations, got %d", len(versions))
	}
	if versions[0] != "001" {
		t.Errorf("Expected first migration 001, got %s", versions[0])
	}
	if versions[1] != "002" {
		t.Errorf("Expected second migration 002, got %s", versions[1])
	}
}

// TestSplitSQLStatements tests the SQL statement splitter
func TestSplitSQLStatements(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:  "Simple statements",
			input: "CREATE TABLE t1 (id INT);\nCREATE TABLE t2 (id INT);",
			expected: []string{
				"CREATE TABLE t1 (id INT)",
				"CREATE TABLE t2 (id INT)",
			},
		},
		{
			name:  "Statement with string containing semicolon",
			input: `INSERT INTO t1 VALUES ('test;value'); CREATE TABLE t2 (id INT);`,
			expected: []string{
				`INSERT INTO t1 VALUES ('test;value')`,
				"CREATE TABLE t2 (id INT)",
			},
		},
		{
			name:  "Statement with line comment",
			input: "-- This is a comment\nCREATE TABLE t1 (id INT);",
			expected: []string{
				"-- This is a comment\nCREATE TABLE t1 (id INT)",
			},
		},
		{
			name:  "Statement with block comment",
			input: "/* Multi\n   line\n   comment */\nCREATE TABLE t1 (id INT);",
			expected: []string{
				"/* Multi\n   line\n   comment */\nCREATE TABLE t1 (id INT)",
			},
		},
		{
			name:  "Multiple statements with various features",
			input: `CREATE TABLE users (
				id INT PRIMARY KEY,
				name TEXT DEFAULT 'John;Doe'
			);
			-- Create index
			CREATE INDEX idx_name ON users(name);
			/* Another table */
			CREATE TABLE posts (
				id INT,
				content TEXT
			);`,
			expected: []string{
				`CREATE TABLE users (
				id INT PRIMARY KEY,
				name TEXT DEFAULT 'John;Doe'
			)`,
				`-- Create index
			CREATE INDEX idx_name ON users(name)`,
				`/* Another table */
			CREATE TABLE posts (
				id INT,
				content TEXT
			)`,
			},
		},
		{
			name:     "Empty input",
			input:    "",
			expected: []string{},
		},
		{
			name:     "Only whitespace and comments",
			input:    "  \n-- comment\n  ",
			expected: []string{"-- comment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitSQLStatements(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d statements, got %d", len(tt.expected), len(result))
				t.Errorf("Result: %v", result)
				return
			}
			for i, stmt := range result {
				if stmt != tt.expected[i] {
					t.Errorf("Statement %d mismatch:\nExpected: %q\nGot:      %q", i, tt.expected[i], stmt)
				}
			}
		})
	}
}

// TestParseUpDownSections tests parsing of up and down sections
func TestParseUpDownSections(t *testing.T) {
	migration := `-- Initial migration
-- +up
CREATE TABLE users (
    id INT PRIMARY KEY,
    name TEXT
);
CREATE INDEX idx_users_name ON users(name);

-- +down
DROP TABLE users;
`

	upSQL := parseUpSection(migration)
	expectedUp := `CREATE TABLE users (
    id INT PRIMARY KEY,
    name TEXT
);
CREATE INDEX idx_users_name ON users(name);
`
	if upSQL != expectedUp {
		t.Errorf("Up section mismatch:\nExpected: %q\nGot:      %q", expectedUp, upSQL)
	}

	downSQL := parseDownSection(migration)
	expectedDown := "DROP TABLE users;\n"
	if downSQL != expectedDown {
		t.Errorf("Down section mismatch:\nExpected: %q\nGot:      %q", expectedDown, downSQL)
	}
}

// TestOpenPath tests the OpenPath function which integrates migration running
func TestOpenPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	// Open database (should run migrations)
	db, err := OpenPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Verify migrations were run by checking for tables
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='beads'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for beads table: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected beads table to exist after OpenPath")
	}

	// Verify we can use the queries
	if db.queries == nil {
		t.Errorf("Expected queries to be initialized")
	}
}

// TestRollbackMigration tests the rollback functionality
func TestRollbackMigration(t *testing.T) {
	// Create an in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Run migrations (runs both 001 and 002)
	if err := RunMigrationsForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Verify we have 2 migrations
	versions, err := MigrationStatus(db)
	if err != nil {
		t.Fatalf("Failed to get migration status: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("Expected 2 migrations before rollback, got %d", len(versions))
	}

	// Verify the test_field column exists (from migration 002)
	hasTestField, err := hasColumn(ctx, db, "test_table1", "test_field")
	if err != nil {
		t.Fatalf("Failed to check for test_field column: %v", err)
	}
	if !hasTestField {
		t.Errorf("Expected test_field column to exist before rollback")
	}

	// Rollback the latest migration (002)
	if err := RollbackMigrationForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to rollback migration: %v", err)
	}

	// Verify we now have 1 migration left (001)
	versions, err = MigrationStatus(db)
	if err != nil {
		t.Fatalf("Failed to get migration status after rollback: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("Expected 1 migration after rollback, got %d", len(versions))
	}
	if versions[0] != "001" {
		t.Errorf("Expected migration 001 to remain, got %s", versions[0])
	}

	// Verify test_table1 still exists (from migration 001)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='test_table1'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for test_table1 after rollback: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected test_table1 to still exist after rolling back 002")
	}

	// Verify test_field column is gone (from migration 002)
	hasTestField, err = hasColumn(ctx, db, "test_table1", "test_field")
	if err != nil {
		t.Fatalf("Failed to check for test_field column after rollback: %v", err)
	}
	if hasTestField {
		t.Errorf("Expected test_field column to be gone after rollback")
	}

	// Rollback the remaining migration (001)
	if err := RollbackMigrationForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to rollback migration 001: %v", err)
	}

	// Verify tables are gone after rolling back all migrations
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='test_table1'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for test_table1 after final rollback: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected test_table1 to be gone after rolling back all migrations")
	}

	// Verify no migration records remain
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check migration records: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected no migration records after rolling back all")
	}
}

// TestRollbackNoMigrations tests rollback when there are no migrations
func TestRollbackNoMigrations(t *testing.T) {
	// Create an in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Create migrations table but don't run migrations
	if err := createMigrationsTable(ctx, db); err != nil {
		t.Fatalf("Failed to create migrations table: %v", err)
	}

	// Try to rollback when there are no migrations
	err = RollbackMigrationForFS(ctx, db, testMigrationsFS)
	if err == nil {
		t.Errorf("Expected error when rolling back with no migrations")
	}
	if err != nil && err.Error() != "no migrations to rollback" {
		t.Errorf("Expected 'no migrations to rollback' error, got: %v", err)
	}
}

// TestMultipleMigrations tests running multiple migrations in sequence
func TestMultipleMigrations(t *testing.T) {
	// Create an in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Run migrations (should run both 001 and 002)
	if err := RunMigrationsForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Check that both migrations were applied
	versions, err := MigrationStatus(db)
	if err != nil {
		t.Fatalf("Failed to get migration status: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("Expected 2 migrations, got %d: %v", len(versions), versions)
	}

	// Verify the test_field column was added
	hasTestField, err := hasColumn(ctx, db, "test_table1", "test_field")
	if err != nil {
		t.Fatalf("Failed to check for test_field column: %v", err)
	}
	if !hasTestField {
		t.Errorf("Expected test_field column to exist after migration 002")
	}

	// Rollback second migration
	if err := RollbackMigrationForFS(ctx, db, testMigrationsFS); err != nil {
		t.Fatalf("Failed to rollback migration: %v", err)
	}

	// Verify only first migration remains
	versions, err = MigrationStatus(db)
	if err != nil {
		t.Fatalf("Failed to get migration status after rollback: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("Expected 1 migration after rollback, got %d", len(versions))
	}
	if versions[0] != "001" {
		t.Errorf("Expected migration 001 to remain, got %s", versions[0])
	}

	// Verify test_field column was removed
	hasTestField, err = hasColumn(ctx, db, "test_table1", "test_field")
	if err != nil {
		t.Fatalf("Failed to check for test_field column after rollback: %v", err)
	}
	if hasTestField {
		t.Errorf("Expected test_field column to be removed after rollback")
	}
}