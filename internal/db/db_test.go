package db

import (
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()

	db, err := OpenPath(":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

func TestOpen(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if db == nil {
		t.Fatal("expected non-nil database")
	}

	// Verify schema was created by querying the table
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM beads").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query beads table: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 beads, got %d", count)
	}
}

func TestStartBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.StartBead("test-1", "Test Bead", "session-1", "pane-1")
	if err != nil {
		t.Fatalf("StartBead failed: %v", err)
	}

	// Verify bead was created
	bead, err := db.GetBead("test-1")
	if err != nil {
		t.Fatalf("GetBead failed: %v", err)
	}
	if bead == nil {
		t.Fatal("expected bead, got nil")
	}
	if bead.ID != "test-1" {
		t.Errorf("expected ID 'test-1', got %q", bead.ID)
	}
	if bead.Title != "Test Bead" {
		t.Errorf("expected title 'Test Bead', got %q", bead.Title)
	}
	if bead.Status != StatusProcessing {
		t.Errorf("expected status %q, got %q", StatusProcessing, bead.Status)
	}
	if bead.ZellijSession != "session-1" {
		t.Errorf("expected session 'session-1', got %q", bead.ZellijSession)
	}
	if bead.ZellijPane != "pane-1" {
		t.Errorf("expected pane 'pane-1', got %q", bead.ZellijPane)
	}
	if bead.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
}

func TestStartBeadUpsert(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create initial bead
	err := db.StartBead("test-1", "Original Title", "session-1", "pane-1")
	if err != nil {
		t.Fatalf("first StartBead failed: %v", err)
	}

	// Update with new values (upsert)
	err = db.StartBead("test-1", "Updated Title", "session-2", "pane-2")
	if err != nil {
		t.Fatalf("second StartBead failed: %v", err)
	}

	// Verify bead was updated
	bead, err := db.GetBead("test-1")
	if err != nil {
		t.Fatalf("GetBead failed: %v", err)
	}
	if bead.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", bead.Title)
	}
	if bead.ZellijSession != "session-2" {
		t.Errorf("expected session 'session-2', got %q", bead.ZellijSession)
	}
}

func TestCompleteBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create bead first
	err := db.StartBead("test-1", "Test Bead", "session-1", "pane-1")
	if err != nil {
		t.Fatalf("StartBead failed: %v", err)
	}

	// Complete it
	err = db.CompleteBead("test-1", "https://github.com/example/pr/1")
	if err != nil {
		t.Fatalf("CompleteBead failed: %v", err)
	}

	// Verify status and PR URL
	bead, err := db.GetBead("test-1")
	if err != nil {
		t.Fatalf("GetBead failed: %v", err)
	}
	if bead.Status != StatusCompleted {
		t.Errorf("expected status %q, got %q", StatusCompleted, bead.Status)
	}
	if bead.PRURL != "https://github.com/example/pr/1" {
		t.Errorf("expected PR URL, got %q", bead.PRURL)
	}
	if bead.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestCompleteBeadNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.CompleteBead("nonexistent", "")
	if err == nil {
		t.Error("expected error for nonexistent bead")
	}
}

func TestFailBead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create bead first
	err := db.StartBead("test-1", "Test Bead", "session-1", "pane-1")
	if err != nil {
		t.Fatalf("StartBead failed: %v", err)
	}

	// Fail it
	err = db.FailBead("test-1", "something went wrong")
	if err != nil {
		t.Fatalf("FailBead failed: %v", err)
	}

	// Verify status and error message
	bead, err := db.GetBead("test-1")
	if err != nil {
		t.Fatalf("GetBead failed: %v", err)
	}
	if bead.Status != StatusFailed {
		t.Errorf("expected status %q, got %q", StatusFailed, bead.Status)
	}
	if bead.ErrorMessage != "something went wrong" {
		t.Errorf("expected error message, got %q", bead.ErrorMessage)
	}
	if bead.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestFailBeadNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	err := db.FailBead("nonexistent", "error")
	if err == nil {
		t.Error("expected error for nonexistent bead")
	}
}

func TestGetBeadNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	bead, err := db.GetBead("nonexistent")
	if err != nil {
		t.Fatalf("GetBead failed: %v", err)
	}
	if bead != nil {
		t.Error("expected nil for nonexistent bead")
	}
}

func TestIsCompleted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Nonexistent bead
	completed, err := db.IsCompleted("nonexistent")
	if err != nil {
		t.Fatalf("IsCompleted failed: %v", err)
	}
	if completed {
		t.Error("expected false for nonexistent bead")
	}

	// Processing bead
	db.StartBead("test-1", "Test", "s", "p")
	completed, err = db.IsCompleted("test-1")
	if err != nil {
		t.Fatalf("IsCompleted failed: %v", err)
	}
	if completed {
		t.Error("expected false for processing bead")
	}

	// Completed bead
	db.CompleteBead("test-1", "")
	completed, err = db.IsCompleted("test-1")
	if err != nil {
		t.Fatalf("IsCompleted failed: %v", err)
	}
	if !completed {
		t.Error("expected true for completed bead")
	}

	// Failed bead also counts as completed
	db.StartBead("test-2", "Test 2", "s", "p")
	db.FailBead("test-2", "error")
	completed, err = db.IsCompleted("test-2")
	if err != nil {
		t.Fatalf("IsCompleted failed: %v", err)
	}
	if !completed {
		t.Error("expected true for failed bead")
	}
}

func TestListBeads(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create several beads with different statuses
	db.StartBead("test-1", "Processing 1", "s", "p")
	db.StartBead("test-2", "Processing 2", "s", "p")
	db.StartBead("test-3", "Will Complete", "s", "p")
	db.CompleteBead("test-3", "")
	db.StartBead("test-4", "Will Fail", "s", "p")
	db.FailBead("test-4", "error")

	// List all
	beads, err := db.ListBeads("")
	if err != nil {
		t.Fatalf("ListBeads failed: %v", err)
	}
	if len(beads) != 4 {
		t.Errorf("expected 4 beads, got %d", len(beads))
	}

	// List processing only
	beads, err = db.ListBeads(StatusProcessing)
	if err != nil {
		t.Fatalf("ListBeads failed: %v", err)
	}
	if len(beads) != 2 {
		t.Errorf("expected 2 processing beads, got %d", len(beads))
	}

	// List completed only
	beads, err = db.ListBeads(StatusCompleted)
	if err != nil {
		t.Fatalf("ListBeads failed: %v", err)
	}
	if len(beads) != 1 {
		t.Errorf("expected 1 completed bead, got %d", len(beads))
	}

	// List failed only
	beads, err = db.ListBeads(StatusFailed)
	if err != nil {
		t.Fatalf("ListBeads failed: %v", err)
	}
	if len(beads) != 1 {
		t.Errorf("expected 1 failed bead, got %d", len(beads))
	}
}

func TestTimestamps(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	before := time.Now().Add(-time.Second)
	db.StartBead("test-1", "Test", "s", "p")
	after := time.Now().Add(time.Second)

	bead, _ := db.GetBead("test-1")

	if bead.CreatedAt.Before(before) || bead.CreatedAt.After(after) {
		t.Error("CreatedAt not within expected range")
	}
	if bead.UpdatedAt.Before(before) || bead.UpdatedAt.After(after) {
		t.Error("UpdatedAt not within expected range")
	}
	if bead.StartedAt.Before(before) || bead.StartedAt.After(after) {
		t.Error("StartedAt not within expected range")
	}
}

func TestTasksTableExists(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Verify tasks table was created
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query tasks table: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tasks, got %d", count)
	}

	// Insert a task to verify schema
	_, err = db.Exec(`
		INSERT INTO tasks (id, status, complexity_budget, actual_complexity)
		VALUES ('task-1', 'pending', 100, 50)
	`)
	if err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}

	// Verify insertion
	var id, status string
	var budget, actual int
	err = db.QueryRow("SELECT id, status, complexity_budget, actual_complexity FROM tasks WHERE id = 'task-1'").
		Scan(&id, &status, &budget, &actual)
	if err != nil {
		t.Fatalf("failed to query task: %v", err)
	}
	if id != "task-1" || status != "pending" || budget != 100 || actual != 50 {
		t.Errorf("unexpected task values: id=%s, status=%s, budget=%d, actual=%d", id, status, budget, actual)
	}
}

func TestTaskBeadsTableExists(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a task first (foreign key reference)
	_, err := db.Exec(`INSERT INTO tasks (id, status) VALUES ('task-1', 'pending')`)
	if err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}

	// Insert task_beads entries
	_, err = db.Exec(`
		INSERT INTO task_beads (task_id, bead_id, status)
		VALUES ('task-1', 'bead-1', 'pending'), ('task-1', 'bead-2', 'completed')
	`)
	if err != nil {
		t.Fatalf("failed to insert task_beads: %v", err)
	}

	// Verify count
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM task_beads WHERE task_id = 'task-1'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query task_beads: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 task_beads, got %d", count)
	}
}

func TestComplexityCacheTableExists(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert complexity cache entry
	_, err := db.Exec(`
		INSERT INTO complexity_cache (bead_id, description_hash, complexity_score, estimated_tokens)
		VALUES ('bead-1', 'abc123hash', 5, 1000)
	`)
	if err != nil {
		t.Fatalf("failed to insert complexity_cache: %v", err)
	}

	// Verify insertion
	var beadID, hash string
	var score, tokens int
	err = db.QueryRow("SELECT bead_id, description_hash, complexity_score, estimated_tokens FROM complexity_cache WHERE bead_id = 'bead-1'").
		Scan(&beadID, &hash, &score, &tokens)
	if err != nil {
		t.Fatalf("failed to query complexity_cache: %v", err)
	}
	if beadID != "bead-1" || hash != "abc123hash" || score != 5 || tokens != 1000 {
		t.Errorf("unexpected complexity_cache values: bead_id=%s, hash=%s, score=%d, tokens=%d", beadID, hash, score, tokens)
	}
}
