package beads

import (
	"testing"
)

func TestGetReadyBeadsInDir(t *testing.T) {
	beads, err := GetReadyBeadsInDir("")
	if err != nil {
		t.Skipf("bd not available or no beads: %v", err)
	}

	// Verify beads have required fields populated
	for _, b := range beads {
		if b.ID == "" {
			t.Error("bead has empty ID")
		}
		if b.Title == "" {
			t.Error("bead has empty title")
		}
	}
}
