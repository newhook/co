package zellij

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	mgr := New()
	require.NotNil(t, mgr, "New() returned nil")

	// Type assert to access internal fields
	c := mgr.(*client)

	// Check default values
	require.Equal(t, 500*time.Millisecond, c.TabCreateDelay)
	require.Equal(t, 500*time.Millisecond, c.CtrlCDelay)
	require.Equal(t, 100*time.Millisecond, c.CommandDelay)
	require.Equal(t, 1*time.Second, c.SessionStartWait)
}

func TestASCIIConstants(t *testing.T) {
	// Verify ASCII constants are correct
	require.Equal(t, 3, ASCIICtrlC)
	require.Equal(t, 13, ASCIIEnter)
}

func TestClientConfiguration(t *testing.T) {
	c := New().(*client)

	// Test that we can modify configuration
	c.TabCreateDelay = 1 * time.Second
	c.CtrlCDelay = 250 * time.Millisecond
	c.CommandDelay = 50 * time.Millisecond
	c.SessionStartWait = 2 * time.Second

	require.Equal(t, 1*time.Second, c.TabCreateDelay, "TabCreateDelay not updated correctly")
	require.Equal(t, 250*time.Millisecond, c.CtrlCDelay, "CtrlCDelay not updated correctly")
	require.Equal(t, 50*time.Millisecond, c.CommandDelay, "CommandDelay not updated correctly")
	require.Equal(t, 2*time.Second, c.SessionStartWait, "SessionStartWait not updated correctly")
}

func TestSessionInheritsConfig(t *testing.T) {
	c := New().(*client)
	c.TabCreateDelay = 1 * time.Second
	c.CtrlCDelay = 250 * time.Millisecond

	sess := c.Session("test-session").(*session)

	require.Equal(t, "test-session", sess.name)
	require.Equal(t, 1*time.Second, sess.TabCreateDelay)
	require.Equal(t, 250*time.Millisecond, sess.CtrlCDelay)
}
