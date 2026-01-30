package zellij

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	client := New()
	require.NotNil(t, client, "New() returned nil")

	// Check default values
	require.Equal(t, 500*time.Millisecond, client.TabCreateDelay)
	require.Equal(t, 500*time.Millisecond, client.CtrlCDelay)
	require.Equal(t, 100*time.Millisecond, client.CommandDelay)
	require.Equal(t, 1*time.Second, client.SessionStartWait)
}

func TestASCIIConstants(t *testing.T) {
	// Verify ASCII constants are correct
	require.Equal(t, 3, ASCIICtrlC)
	require.Equal(t, 13, ASCIIEnter)
}

func TestClientConfiguration(t *testing.T) {
	client := New()

	// Test that we can modify configuration
	client.TabCreateDelay = 1 * time.Second
	client.CtrlCDelay = 250 * time.Millisecond
	client.CommandDelay = 50 * time.Millisecond
	client.SessionStartWait = 2 * time.Second

	require.Equal(t, 1*time.Second, client.TabCreateDelay, "TabCreateDelay not updated correctly")
	require.Equal(t, 250*time.Millisecond, client.CtrlCDelay, "CtrlCDelay not updated correctly")
	require.Equal(t, 50*time.Millisecond, client.CommandDelay, "CommandDelay not updated correctly")
	require.Equal(t, 2*time.Second, client.SessionStartWait, "SessionStartWait not updated correctly")
}
