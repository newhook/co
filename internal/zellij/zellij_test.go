package zellij

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	client := New()
	if client == nil {
		t.Fatal("New() returned nil")
	}

	// Check default values
	if client.TabCreateDelay != 500*time.Millisecond {
		t.Errorf("TabCreateDelay = %v, want %v", client.TabCreateDelay, 500*time.Millisecond)
	}
	if client.CtrlCDelay != 500*time.Millisecond {
		t.Errorf("CtrlCDelay = %v, want %v", client.CtrlCDelay, 500*time.Millisecond)
	}
	if client.CommandDelay != 100*time.Millisecond {
		t.Errorf("CommandDelay = %v, want %v", client.CommandDelay, 100*time.Millisecond)
	}
	if client.SessionStartWait != 1*time.Second {
		t.Errorf("SessionStartWait = %v, want %v", client.SessionStartWait, 1*time.Second)
	}
}

func TestASCIIConstants(t *testing.T) {
	// Verify ASCII constants are correct
	if ASCIICtrlC != 3 {
		t.Errorf("ASCIICtrlC = %d, want 3", ASCIICtrlC)
	}
	if ASCIIEnter != 13 {
		t.Errorf("ASCIIEnter = %d, want 13", ASCIIEnter)
	}
}

func TestClientConfiguration(t *testing.T) {
	client := New()

	// Test that we can modify configuration
	client.TabCreateDelay = 1 * time.Second
	client.CtrlCDelay = 250 * time.Millisecond
	client.CommandDelay = 50 * time.Millisecond
	client.SessionStartWait = 2 * time.Second

	if client.TabCreateDelay != 1*time.Second {
		t.Errorf("TabCreateDelay not updated correctly")
	}
	if client.CtrlCDelay != 250*time.Millisecond {
		t.Errorf("CtrlCDelay not updated correctly")
	}
	if client.CommandDelay != 50*time.Millisecond {
		t.Errorf("CommandDelay not updated correctly")
	}
	if client.SessionStartWait != 2*time.Second {
		t.Errorf("SessionStartWait not updated correctly")
	}
}
