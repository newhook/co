// Package signal provides centralized signal handling utilities for graceful shutdown.
package signal

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	// mu protects the blocked state
	mu sync.Mutex
	// blocked indicates if signals are currently blocked
	blocked bool
	// blockCount tracks nested blocking calls
	blockCount int
	// pendingCancel holds a cancel func to call when signals are unblocked
	pendingCancel context.CancelFunc
)

// WithSignalCancel returns a context that is cancelled when SIGINT or SIGTERM is received.
// The returned cancel function should be called to clean up resources when done.
func WithSignalCancel(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigChan:
			mu.Lock()
			if blocked {
				// Store the cancel to call later when unblocked
				pendingCancel = cancel
				mu.Unlock()
				return
			}
			mu.Unlock()
			cancel()
		case <-ctx.Done():
			// Context was cancelled, clean up
		}
		signal.Stop(sigChan)
		close(sigChan)
	}()

	return ctx, cancel
}

// BlockSignals prevents signal-based context cancellation during critical operations.
// Call UnblockSignals when the critical section is complete.
// BlockSignals/UnblockSignals calls can be nested.
func BlockSignals() {
	mu.Lock()
	defer mu.Unlock()
	blockCount++
	blocked = true
}

// UnblockSignals re-enables signal-based context cancellation.
// If a signal was received while blocked, the pending cancellation will be executed.
func UnblockSignals() {
	mu.Lock()
	defer mu.Unlock()
	if blockCount > 0 {
		blockCount--
	}
	if blockCount == 0 {
		blocked = false
		if pendingCancel != nil {
			// Signal was received while blocked, cancel now
			pendingCancel()
			pendingCancel = nil
		}
	}
}

