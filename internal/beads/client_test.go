package beads

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetReadyBeads(t *testing.T) {
	beads, err := GetReadyBeadsInDir("")
	if err != nil {
		t.Skipf("bd not available or no beads: %v", err)
	}

	// Verify beads have required fields populated
	for _, b := range beads {
		assert.NotEmpty(t, b.ID, "bead has empty ID")
		assert.NotEmpty(t, b.Title, "bead has empty title")
	}
}
