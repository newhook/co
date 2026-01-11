package task

import (
	"testing"

	"github.com/newhook/co/internal/db"
)

func setupTestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()

	database, err := db.OpenPath(":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	cleanup := func() {
		database.Close()
	}

	return database, cleanup
}

func TestHashDescription(t *testing.T) {
	// Same inputs should produce same hash
	hash1 := db.HashDescription("Title\nDescription")
	hash2 := db.HashDescription("Title\nDescription")
	if hash1 != hash2 {
		t.Error("same inputs should produce same hash")
	}

	// Different inputs should produce different hash
	hash3 := db.HashDescription("Different Title\nDescription")
	if hash1 == hash3 {
		t.Error("different inputs should produce different hash")
	}

	// Hash should be hex encoded
	if len(hash1) != 64 { // SHA256 produces 32 bytes = 64 hex chars
		t.Errorf("expected 64 char hex hash, got %d chars", len(hash1))
	}
}

func TestCacheComplexity(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Cache a complexity estimate
	err := database.CacheComplexity("bead-1", "hash123", 5, 10000)
	if err != nil {
		t.Fatalf("CacheComplexity failed: %v", err)
	}

	// Retrieve it
	score, tokens, found, err := database.GetCachedComplexity("bead-1", "hash123")
	if err != nil {
		t.Fatalf("GetCachedComplexity failed: %v", err)
	}

	if !found {
		t.Error("expected to find cached complexity")
	}
	if score != 5 {
		t.Errorf("expected score 5, got %d", score)
	}
	if tokens != 10000 {
		t.Errorf("expected tokens 10000, got %d", tokens)
	}

	// Different hash should not match
	_, _, found, err = database.GetCachedComplexity("bead-1", "differenthash")
	if err != nil {
		t.Fatalf("GetCachedComplexity failed: %v", err)
	}
	if found {
		t.Error("expected no result for different hash")
	}
}

func TestCacheUpdate(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Cache initial estimate
	database.CacheComplexity("bead-1", "hash1", 3, 5000)

	// Update with new hash (description changed)
	err := database.CacheComplexity("bead-1", "hash2", 7, 15000)
	if err != nil {
		t.Fatalf("CacheComplexity update failed: %v", err)
	}

	// Old hash should not match
	_, _, found, _ := database.GetCachedComplexity("bead-1", "hash1")
	if found {
		t.Error("old hash should not match after update")
	}

	// New hash should match
	score, tokens, found, err := database.GetCachedComplexity("bead-1", "hash2")
	if err != nil {
		t.Fatalf("GetCachedComplexity failed: %v", err)
	}
	if !found {
		t.Error("expected to find updated complexity")
	}
	if score != 7 {
		t.Errorf("expected score 7, got %d", score)
	}
	if tokens != 15000 {
		t.Errorf("expected tokens 15000, got %d", tokens)
	}
}

func TestNewLLMEstimator(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Test creation with database
	estimator := NewLLMEstimator(database, "/tmp/test", "test-project", "work-test")
	if estimator == nil {
		t.Fatal("expected non-nil estimator")
	}
	if estimator.database != database {
		t.Error("database not set correctly")
	}

	// Test creation with nil database (should still work for non-cached usage)
	estimator = NewLLMEstimator(nil, "/tmp/test", "test-project", "work-test")
	if estimator == nil {
		t.Fatal("expected non-nil estimator even with nil database")
	}
}

// Note: Testing the actual Estimate and EstimateBatch functions would require
// a running Claude Code instance, which is beyond the scope of unit tests.
// The caching behavior is tested via the database methods above.
