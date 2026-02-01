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
	m := mgr.(*sessionManager)

	// Check default values
	require.Equal(t, 500*time.Millisecond, m.TabCreateDelay)
	require.Equal(t, 500*time.Millisecond, m.CtrlCDelay)
	require.Equal(t, 100*time.Millisecond, m.CommandDelay)
	require.Equal(t, 1*time.Second, m.SessionStartWait)
}

func TestASCIIConstants(t *testing.T) {
	require.Equal(t, 3, ASCIICtrlC)
}

func TestSessionManagerConfiguration(t *testing.T) {
	m := New().(*sessionManager)

	// Test that we can modify configuration
	m.TabCreateDelay = 1 * time.Second
	m.CtrlCDelay = 250 * time.Millisecond
	m.CommandDelay = 50 * time.Millisecond
	m.SessionStartWait = 2 * time.Second

	require.Equal(t, 1*time.Second, m.TabCreateDelay)
	require.Equal(t, 250*time.Millisecond, m.CtrlCDelay)
	require.Equal(t, 50*time.Millisecond, m.CommandDelay)
	require.Equal(t, 2*time.Second, m.SessionStartWait)
}

func TestSessionInheritsConfig(t *testing.T) {
	m := New().(*sessionManager)
	m.TabCreateDelay = 1 * time.Second
	m.CtrlCDelay = 250 * time.Millisecond

	sess := m.Session("test-session").(*session)

	require.Equal(t, "test-session", sess.name)
	require.Equal(t, 1*time.Second, sess.TabCreateDelay)
	require.Equal(t, 250*time.Millisecond, sess.CtrlCDelay)
}
