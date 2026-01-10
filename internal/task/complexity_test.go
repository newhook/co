package task

import (
	"testing"

	"github.com/newhook/co/internal/beads"
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
	hash1 := hashDescription("Title", "Description")
	hash2 := hashDescription("Title", "Description")
	if hash1 != hash2 {
		t.Error("same inputs should produce same hash")
	}

	// Different inputs should produce different hash
	hash3 := hashDescription("Different Title", "Description")
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

	estimator := &LLMEstimator{database: database}

	// Cache a complexity estimate
	err := estimator.cacheComplexity("bead-1", "hash123", 5, 10000)
	if err != nil {
		t.Fatalf("cacheComplexity failed: %v", err)
	}

	// Retrieve it
	cached, err := estimator.getCachedComplexity("bead-1", "hash123")
	if err != nil {
		t.Fatalf("getCachedComplexity failed: %v", err)
	}

	if cached.ComplexityScore != 5 {
		t.Errorf("expected score 5, got %d", cached.ComplexityScore)
	}
	if cached.EstimatedTokens != 10000 {
		t.Errorf("expected tokens 10000, got %d", cached.EstimatedTokens)
	}

	// Different hash should not match
	cached, err = estimator.getCachedComplexity("bead-1", "differenthash")
	if err == nil && cached != nil {
		t.Error("expected no result for different hash")
	}
}

func TestCacheUpdate(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	estimator := &LLMEstimator{database: database}

	// Cache initial estimate
	estimator.cacheComplexity("bead-1", "hash1", 3, 5000)

	// Update with new hash (description changed)
	err := estimator.cacheComplexity("bead-1", "hash2", 7, 15000)
	if err != nil {
		t.Fatalf("cacheComplexity update failed: %v", err)
	}

	// Old hash should not match
	cached, _ := estimator.getCachedComplexity("bead-1", "hash1")
	if cached != nil {
		t.Error("old hash should not match after update")
	}

	// New hash should match
	cached, err = estimator.getCachedComplexity("bead-1", "hash2")
	if err != nil {
		t.Fatalf("getCachedComplexity failed: %v", err)
	}
	if cached.ComplexityScore != 7 {
		t.Errorf("expected score 7, got %d", cached.ComplexityScore)
	}
}

func TestNewLLMEstimator(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Test creation with database
	estimator := NewLLMEstimator(database)
	if estimator == nil {
		t.Fatal("expected non-nil estimator")
	}
	if estimator.database != database {
		t.Error("database not set correctly")
	}

	// Test creation with nil database (should still work for non-cached usage)
	estimator = NewLLMEstimator(nil)
	if estimator == nil {
		t.Fatal("expected non-nil estimator even with nil database")
	}
}

// Note: Testing the actual LLM call would require mocking or integration tests
// with a real API key. The Estimate function's caching behavior is tested above.

func TestEstimateWithCache(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	estimator := &LLMEstimator{database: database}

	// Pre-populate cache
	bead := beads.Bead{
		ID:          "bead-1",
		Title:       "Test Bead",
		Description: "Test description",
	}
	hash := hashDescription(bead.Title, bead.Description)
	estimator.cacheComplexity(bead.ID, hash, 5, 10000)

	// Estimate should return cached value without calling LLM
	score, tokens, err := estimator.Estimate(bead)
	if err != nil {
		t.Fatalf("Estimate failed: %v", err)
	}
	if score != 5 {
		t.Errorf("expected score 5, got %d", score)
	}
	if tokens != 10000 {
		t.Errorf("expected tokens 10000, got %d", tokens)
	}
}
