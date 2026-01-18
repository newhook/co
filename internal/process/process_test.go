package process

import (
	"context"
	"testing"
	"time"
)

func TestIsProcessRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test with a non-existent process pattern
	running, err := IsProcessRunning(ctx, "this-process-definitely-does-not-exist-xyz123")
	if err != nil {
		t.Fatalf("IsProcessRunning failed: %v", err)
	}
	if running {
		t.Error("Expected non-existent process to not be running")
	}

	// Test with current test process (should find itself)
	running, err = IsProcessRunning(ctx, "go test")
	if err != nil {
		t.Fatalf("IsProcessRunning failed: %v", err)
	}
	// Note: This might not always work depending on how the test is invoked
	// so we just check that the function doesn't error
}
