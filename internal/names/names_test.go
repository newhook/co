package names

import (
	"context"
	"database/sql"
	"strings"
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

func TestGenerateName(t *testing.T) {
	name := GenerateName()
	assert.NotEmpty(t, name)

	// Name should be in "adjective_noun" format
	parts := strings.Split(name, "_")
	assert.Equal(t, 2, len(parts), "name should have exactly two parts separated by underscore")
	assert.NotEmpty(t, parts[0], "adjective should not be empty")
	assert.NotEmpty(t, parts[1], "noun should not be empty")
}

func TestGenerateName_Variety(t *testing.T) {
	// Generate many names and verify we get variety
	names := make(map[string]bool)
	for i := 0; i < 100; i++ {
		name := GenerateName()
		names[name] = true
	}

	// With random generation, we should get many unique names
	// Even with just 100 attempts, we should get at least 90 unique (very likely all 100)
	assert.GreaterOrEqual(t, len(names), 90, "should generate many unique names")
}

func TestGetNextAvailableName_Empty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.NotEmpty(t, name, "should return a name when no names are in use")

	// Verify format
	parts := strings.Split(name, "_")
	assert.Equal(t, 2, len(parts), "name should be in adjective_noun format")
}

func TestGetNextAvailableName_SomeUsed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert some works with names
	_, err := db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-1', 'processing', 'brave_einstein')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-2', 'pending', 'clever_curie')`)
	require.NoError(t, err)

	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.NotEmpty(t, name, "should return a name")
	assert.NotEqual(t, "brave_einstein", name, "should not return an in-use name")
	assert.NotEqual(t, "clever_curie", name, "should not return an in-use name")
}

func TestGetNextAvailableName_CompletedReleased(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert work with a name, but completed (should be released)
	_, err := db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-1', 'completed', 'brave_einstein')`)
	require.NoError(t, err)

	// Should be able to get a new name (the completed one's name is released)
	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.NotEmpty(t, name, "should return a name since completed names are released")
}

func TestGetNextAvailableName_FailedReleased(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Insert work with a name, but failed (should be released)
	_, err := db.Exec(`INSERT INTO works (id, status, name) VALUES ('w-1', 'failed', 'brave_einstein')`)
	require.NoError(t, err)

	// Should be able to get a new name (the failed one's name is released)
	name, err := GetNextAvailableName(ctx, db)
	require.NoError(t, err)
	assert.NotEmpty(t, name, "should return a name since failed names are released")
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
	assert.NotEmpty(t, name, "should return a name since empty names are ignored")
}

func TestGetAllAdjectives(t *testing.T) {
	adjectives := GetAllAdjectives()
	assert.GreaterOrEqual(t, len(adjectives), 100, "should have at least 100 adjectives")

	// Verify GetAllAdjectives returns a copy
	adjectives[0] = "modified"
	original := GetAllAdjectives()
	assert.NotEqual(t, "modified", original[0], "GetAllAdjectives should return a copy")
}

func TestGetAllNouns(t *testing.T) {
	nouns := GetAllNouns()
	assert.GreaterOrEqual(t, len(nouns), 100, "should have at least 100 nouns")

	// Verify GetAllNouns returns a copy
	nouns[0] = "modified"
	original := GetAllNouns()
	assert.NotEqual(t, "modified", original[0], "GetAllNouns should return a copy")
}

func TestGetCombinationCount(t *testing.T) {
	count := GetCombinationCount()
	// Should have at least 10,000 combinations (100 adj * 100 nouns minimum)
	assert.GreaterOrEqual(t, count, 10000, "should have at least 10,000 possible combinations")
}

func TestParseName(t *testing.T) {
	tests := []struct {
		name    string
		wantAdj string
		wantNoun string
	}{
		{"brave_einstein", "brave", "einstein"},
		{"clever_curie", "clever", "curie"},
		{"", "", ""},
		{"nounderscores", "", ""},
		{"multiple_under_scores", "multiple", "under_scores"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adj, noun := ParseName(tt.name)
			assert.Equal(t, tt.wantAdj, adj)
			assert.Equal(t, tt.wantNoun, noun)
		})
	}
}

func TestReleaseName(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// ReleaseName is a no-op, just verify it doesn't error
	err := ReleaseName(ctx, db, "brave_einstein")
	require.NoError(t, err)
}
