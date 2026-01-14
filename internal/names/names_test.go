package names

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Create minimal works table
	_, err = db.Exec(`
		CREATE TABLE works (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL DEFAULT 'pending',
			name TEXT NOT NULL DEFAULT ''
		)
	`)
	require.NoError(t, err)

	return db
}

func TestGetNextAvailableName_Empty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "Alice", name, "first available name should be Alice")
}

func TestGetNextAvailableName_SomeUsed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert some works with names
	_, err := db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-1', 'processing', 'Alice')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-2', 'pending', 'Bob')`)
	require.NoError(t, err)

	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "Charlie", name, "next available name should be Charlie")
}

func TestGetNextAvailableName_CompletedReleased(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert work with Alice, but completed (should be released)
	_, err := db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-1', 'completed', 'Alice')`)
	require.NoError(t, err)

	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "Alice", name, "Alice should be available since work is completed")
}

func TestGetNextAvailableName_FailedReleased(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert work with Alice, but failed (should be released)
	_, err := db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-1', 'failed', 'Alice')`)
	require.NoError(t, err)

	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "Alice", name, "Alice should be available since work failed")
}

func TestGetNextAvailableName_AllUsed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert works for all predefined names
	for i, name := range predefinedNames {
		_, err := db.Exec(`INSERT INTO works (id, status, name) VALUES (?, 'processing', ?)`,
			"w-"+string(rune('a'+i)), name)
		require.NoError(t, err)
	}

	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.Empty(t, name, "should return empty string when all names are in use")
}

func TestGetNextAvailableName_EmptyNameIgnored(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert work without a name (legacy)
	_, err := db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-1', 'processing', '')`)
	require.NoError(t, err)

	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "Alice", name, "Alice should be available since empty names are ignored")
}

func TestGetAllNames(t *testing.T) {
	names := GetAllNames()
	assert.Equal(t, 26, len(names), "should have 26 predefined names")
	assert.Equal(t, "Alice", names[0])
	assert.Equal(t, "Zack", names[len(names)-1])

	// Verify GetAllNames returns a copy
	names[0] = "Modified"
	original := GetAllNames()
	assert.Equal(t, "Alice", original[0], "GetAllNames should return a copy")
}

func TestReleaseName(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// ReleaseName is a no-op, just verify it doesn't error
	err := ReleaseName(ctx, db, "Alice")
	require.NoError(t, err)
}
