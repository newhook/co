package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()

	db, err := OpenPath(context.Background(), ":memory:")
	require.NoError(t, err, "failed to open database")

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

func TestOpen(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	require.NotNil(t, db, "expected non-nil database")

	// Verify schema was created by querying the table
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM beads").Scan(&count)
	require.NoError(t, err, "failed to query beads table")
	assert.Equal(t, 0, count, "expected 0 beads")
}

func TestStartBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := db.StartBead(ctx, "test-1", "Test Bead", "session-1", "pane-1")
	require.NoError(t, err, "StartBead failed")

	// Verify bead was created
	bead, err := db.GetBead(ctx, "test-1")
	require.NoError(t, err, "GetBead failed")
	require.NotNil(t, bead, "expected bead, got nil")
	assert.Equal(t, "test-1", bead.ID)
	assert.Equal(t, "Test Bead", bead.Title)
	assert.Equal(t, StatusProcessing, bead.Status)
	assert.Equal(t, "session-1", bead.ZellijSession)
	assert.Equal(t, "pane-1", bead.ZellijPane)
	assert.NotNil(t, bead.StartedAt, "expected StartedAt to be set")
}

func TestStartBeadUpsert(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create initial bead
	err := db.StartBead(ctx, "test-1", "Original Title", "session-1", "pane-1")
	require.NoError(t, err, "first StartBead failed")

	// Update with new values (upsert)
	err = db.StartBead(ctx, "test-1", "Updated Title", "session-2", "pane-2")
	require.NoError(t, err, "second StartBead failed")

	// Verify bead was updated
	bead, err := db.GetBead(ctx, "test-1")
	require.NoError(t, err, "GetBead failed")
	assert.Equal(t, "Updated Title", bead.Title)
	assert.Equal(t, "session-2", bead.ZellijSession)
}

func TestCompleteBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create bead first
	err := db.StartBead(ctx, "test-1", "Test Bead", "session-1", "pane-1")
	require.NoError(t, err, "StartBead failed")

	// Complete it
	err = db.CompleteBead(ctx, "test-1", "https://github.com/example/pr/1")
	require.NoError(t, err, "CompleteBead failed")

	// Verify status and PR URL
	bead, err := db.GetBead(ctx, "test-1")
	require.NoError(t, err, "GetBead failed")
	assert.Equal(t, StatusCompleted, bead.Status)
	assert.Equal(t, "https://github.com/example/pr/1", bead.PRURL)
	assert.NotNil(t, bead.CompletedAt, "expected CompletedAt to be set")
}

func TestCompleteBeadNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := db.CompleteBead(ctx, "nonexistent", "")
	assert.Error(t, err, "expected error for nonexistent bead")
}

func TestFailBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create bead first
	err := db.StartBead(ctx, "test-1", "Test Bead", "session-1", "pane-1")
	require.NoError(t, err, "StartBead failed")

	// Fail it
	err = db.FailBead(ctx, "test-1", "something went wrong")
	require.NoError(t, err, "FailBead failed")

	// Verify status and error message
	bead, err := db.GetBead(ctx, "test-1")
	require.NoError(t, err, "GetBead failed")
	assert.Equal(t, StatusFailed, bead.Status)
	assert.Equal(t, "something went wrong", bead.ErrorMessage)
	assert.NotNil(t, bead.CompletedAt, "expected CompletedAt to be set")
}

func TestFailBeadNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := db.FailBead(ctx, "nonexistent", "error")
	assert.Error(t, err, "expected error for nonexistent bead")
}

func TestGetBeadNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	bead, err := db.GetBead(ctx, "nonexistent")
	require.NoError(t, err, "GetBead failed")
	assert.Nil(t, bead, "expected nil for nonexistent bead")
}

func TestIsCompleted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Nonexistent bead
	completed, err := db.IsCompleted(ctx, "nonexistent")
	require.NoError(t, err, "IsCompleted failed")
	assert.False(t, completed, "expected false for nonexistent bead")

	// Processing bead
	db.StartBead(ctx, "test-1", "Test", "s", "p")
	completed, err = db.IsCompleted(ctx, "test-1")
	require.NoError(t, err, "IsCompleted failed")
	assert.False(t, completed, "expected false for processing bead")

	// Completed bead
	db.CompleteBead(ctx, "test-1", "")
	completed, err = db.IsCompleted(ctx, "test-1")
	require.NoError(t, err, "IsCompleted failed")
	assert.True(t, completed, "expected true for completed bead")

	// Failed bead also counts as completed
	db.StartBead(ctx, "test-2", "Test 2", "s", "p")
	db.FailBead(ctx, "test-2", "error")
	completed, err = db.IsCompleted(ctx, "test-2")
	require.NoError(t, err, "IsCompleted failed")
	assert.True(t, completed, "expected true for failed bead")
}

func TestListBeads(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create several beads with different statuses
	db.StartBead(ctx, "test-1", "Processing 1", "s", "p")
	db.StartBead(ctx, "test-2", "Processing 2", "s", "p")
	db.StartBead(ctx, "test-3", "Will Complete", "s", "p")
	db.CompleteBead(ctx, "test-3", "")
	db.StartBead(ctx, "test-4", "Will Fail", "s", "p")
	db.FailBead(ctx, "test-4", "error")

	// List all
	beads, err := db.ListBeads(ctx, "")
	require.NoError(t, err, "ListBeads failed")
	assert.Len(t, beads, 4, "expected 4 beads")

	// List processing only
	beads, err = db.ListBeads(ctx, StatusProcessing)
	require.NoError(t, err, "ListBeads failed")
	assert.Len(t, beads, 2, "expected 2 processing beads")

	// List completed only
	beads, err = db.ListBeads(ctx, StatusCompleted)
	require.NoError(t, err, "ListBeads failed")
	assert.Len(t, beads, 1, "expected 1 completed bead")

	// List failed only
	beads, err = db.ListBeads(ctx, StatusFailed)
	require.NoError(t, err, "ListBeads failed")
	assert.Len(t, beads, 1, "expected 1 failed bead")
}

func TestTimestamps(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	before := time.Now().Add(-time.Second)
	db.StartBead(ctx, "test-1", "Test", "s", "p")
	after := time.Now().Add(time.Second)

	bead, _ := db.GetBead(ctx, "test-1")

	assert.True(t, bead.CreatedAt.After(before) && bead.CreatedAt.Before(after), "CreatedAt not within expected range")
	assert.True(t, bead.UpdatedAt.After(before) && bead.UpdatedAt.Before(after), "UpdatedAt not within expected range")
	assert.True(t, bead.StartedAt.After(before) && bead.StartedAt.Before(after), "StartedAt not within expected range")
}

func TestTasksTableExists(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Verify tasks table was created
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&count)
	require.NoError(t, err, "failed to query tasks table")
	assert.Equal(t, 0, count, "expected 0 tasks")

	// Insert a task to verify schema
	_, err = db.Exec(`
		INSERT INTO tasks (id, status, complexity_budget, actual_complexity)
		VALUES ('task-1', 'pending', 100, 50)
	`)
	require.NoError(t, err, "failed to insert task")

	// Verify insertion
	var id, status string
	var budget, actual int
	err = db.QueryRow("SELECT id, status, complexity_budget, actual_complexity FROM tasks WHERE id = 'task-1'").
		Scan(&id, &status, &budget, &actual)
	require.NoError(t, err, "failed to query task")
	assert.Equal(t, "task-1", id)
	assert.Equal(t, "pending", status)
	assert.Equal(t, 100, budget)
	assert.Equal(t, 50, actual)
}

func TestTaskBeadsTableExists(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a task first (foreign key reference)
	_, err := db.Exec(`INSERT INTO tasks (id, status) VALUES ('task-1', 'pending')`)
	require.NoError(t, err, "failed to insert task")

	// Insert task_beads entries
	_, err = db.Exec(`
		INSERT INTO task_beads (task_id, bead_id, status)
		VALUES ('task-1', 'bead-1', 'pending'), ('task-1', 'bead-2', 'completed')
	`)
	require.NoError(t, err, "failed to insert task_beads")

	// Verify count
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM task_beads WHERE task_id = 'task-1'").Scan(&count)
	require.NoError(t, err, "failed to query task_beads")
	assert.Equal(t, 2, count, "expected 2 task_beads")
}

func TestComplexityCacheTableExists(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert complexity cache entry
	_, err := db.Exec(`
		INSERT INTO complexity_cache (bead_id, description_hash, complexity_score, estimated_tokens)
		VALUES ('bead-1', 'abc123hash', 5, 1000)
	`)
	require.NoError(t, err, "failed to insert complexity_cache")

	// Verify insertion
	var beadID, hash string
	var score, tokens int
	err = db.QueryRow("SELECT bead_id, description_hash, complexity_score, estimated_tokens FROM complexity_cache WHERE bead_id = 'bead-1'").
		Scan(&beadID, &hash, &score, &tokens)
	require.NoError(t, err, "failed to query complexity_cache")
	assert.Equal(t, "bead-1", beadID)
	assert.Equal(t, "abc123hash", hash)
	assert.Equal(t, 5, score)
	assert.Equal(t, 1000, tokens)
}
