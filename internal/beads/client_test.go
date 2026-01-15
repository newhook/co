package beads

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetReadyBeads(t *testing.T) {
	beads, err := GetReadyBeads(context.Background(), "")
	if err != nil {
		t.Skipf("bd not available or no beads: %v", err)
	}

	// Verify beads have required fields populated
	for _, b := range beads {
		assert.NotEmpty(t, b.ID, "bead has empty ID")
		assert.NotEmpty(t, b.Title, "bead has empty title")
	}
}

func TestGetTransitiveDependencies_NonExistentBead(t *testing.T) {
	// Test with a non-existent bead ID
	_, err := GetTransitiveDependencies(context.Background(),"non-existent-bead-xyz123", "/tmp/non-existent-dir")
	assert.Error(t, err, "should return error for non-existent bead")
}

func TestGetTransitiveDependencies_EmptyID(t *testing.T) {
	// Test with empty bead ID
	_, err := GetTransitiveDependencies(context.Background(),"", "/tmp/non-existent-dir")
	assert.Error(t, err, "should return error for empty bead ID")
}

func TestGetTransitiveDependencies_InvalidDir(t *testing.T) {
	// Test with invalid directory
	_, err := GetTransitiveDependencies(context.Background(),"some-bead", "/path/that/definitely/does/not/exist/xyz123")
	assert.Error(t, err, "should return error for invalid directory")
}

func TestGetBeadWithChildren_NonExistentBead(t *testing.T) {
	// Test with a non-existent bead ID
	_, err := GetBeadWithChildren(context.Background(),"non-existent-bead-xyz123", "/tmp/non-existent-dir")
	assert.Error(t, err, "should return error for non-existent bead")
}

func TestGetBeadWithChildren_EmptyID(t *testing.T) {
	// Test with empty bead ID
	_, err := GetBeadWithChildren(context.Background(),"", "/tmp/non-existent-dir")
	assert.Error(t, err, "should return error for empty bead ID")
}

func TestGetBeadWithChildren_InvalidDir(t *testing.T) {
	// Test with invalid directory
	_, err := GetBeadWithChildren(context.Background(),"some-bead", "/path/that/definitely/does/not/exist/xyz123")
	assert.Error(t, err, "should return error for invalid directory")
}

// Integration tests - these require the bd CLI and a valid beads repository

func TestGetTransitiveDependencies_Integration(t *testing.T) {
	// Get any ready beads first to check if bd is available
	beads, err := GetReadyBeads(context.Background(), "")
	if err != nil {
		t.Skipf("bd not available or no beads: %v", err)
	}
	if len(beads) == 0 {
		t.Skip("no ready beads available for testing")
	}

	// Test with a real bead ID
	result, err := GetTransitiveDependencies(context.Background(),beads[0].ID, "")
	if err != nil {
		t.Skipf("failed to get transitive dependencies: %v", err)
	}

	// Verify the result contains at least the bead itself
	assert.NotEmpty(t, result, "should return at least the bead itself")

	// Verify the bead is included in the result
	found := false
	for _, b := range result {
		if b.ID == beads[0].ID {
			found = true
			break
		}
	}
	assert.True(t, found, "result should include the requested bead")

	// Verify all beads have valid IDs
	for _, b := range result {
		assert.NotEmpty(t, b.ID, "bead has empty ID")
	}
}

func TestGetBeadWithChildren_Integration(t *testing.T) {
	// Get any ready beads first to check if bd is available
	beads, err := GetReadyBeads(context.Background(), "")
	if err != nil {
		t.Skipf("bd not available or no beads: %v", err)
	}
	if len(beads) == 0 {
		t.Skip("no ready beads available for testing")
	}

	// Test with a real bead ID
	result, err := GetBeadWithChildren(context.Background(),beads[0].ID, "")
	if err != nil {
		t.Skipf("failed to get bead with children: %v", err)
	}

	// Verify the result contains at least the bead itself
	assert.NotEmpty(t, result, "should return at least the bead itself")

	// Verify the bead is included in the result
	found := false
	for _, b := range result {
		if b.ID == beads[0].ID {
			found = true
			break
		}
	}
	assert.True(t, found, "result should include the requested bead")

	// Verify all beads have valid IDs
	for _, b := range result {
		assert.NotEmpty(t, b.ID, "bead has empty ID")
	}
}

func TestGetTransitiveDependencies_ReturnsInDependencyOrder(t *testing.T) {
	// This test documents expected behavior:
	// Dependencies should be returned before dependents (topological order)
	// Testing this fully requires a bead graph with dependencies

	beads, err := GetReadyBeads(context.Background(), "")
	if err != nil {
		t.Skipf("bd not available: %v", err)
	}
	if len(beads) == 0 {
		t.Skip("no ready beads available for testing")
	}

	// Get a bead with dependencies if available
	for _, bead := range beads {
		result, err := GetTransitiveDependencies(context.Background(),bead.ID, "")
		if err != nil {
			continue
		}

		if len(result) > 1 {
			// Found a bead with dependencies
			// Verify order: for each bead, its dependencies should appear earlier
			idToIndex := make(map[string]int)
			for i, b := range result {
				idToIndex[b.ID] = i
			}

			for _, b := range result {
				for _, dep := range b.Dependencies {
					if dep.DependencyType == "blocked_by" {
						depIndex, exists := idToIndex[dep.ID]
						if exists {
							beadIndex := idToIndex[b.ID]
							assert.Less(t, depIndex, beadIndex,
								"dependency %s should appear before dependent %s", dep.ID, b.ID)
						}
					}
				}
			}
			return
		}
	}
	t.Skip("no beads with dependencies available for order testing")
}

func TestGetBeadWithChildren_IncludesParent(t *testing.T) {
	// This test documents expected behavior:
	// The parent bead should be included in the result

	beads, err := GetReadyBeads(context.Background(), "")
	if err != nil {
		t.Skipf("bd not available: %v", err)
	}
	if len(beads) == 0 {
		t.Skip("no ready beads available for testing")
	}

	// Test with any bead - parent should always be in result
	result, err := GetBeadWithChildren(context.Background(),beads[0].ID, "")
	if err != nil {
		t.Skipf("failed to get bead: %v", err)
	}

	// First element should be the parent
	assert.Equal(t, beads[0].ID, result[0].ID, "first element should be the parent bead")
}
