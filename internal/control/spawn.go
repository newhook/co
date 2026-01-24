package control

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/process"
	"github.com/newhook/co/internal/zellij"
)

// TabName is the name of the control plane tab in zellij
const TabName = "control"

// SpawnControlPlane spawns the control plane in a zellij tab
func SpawnControlPlane(ctx context.Context, projectName string, projectRoot string, w io.Writer) error {
	sessionName := claude.SessionNameForProject(projectName)
	zc := zellij.New()

	// Ensure session exists
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		return err
	}

	// Check if control plane tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, TabName)
	if tabExists {
		fmt.Fprintf(w, "Control plane tab already exists\n")
		return nil
	}

	// Build the control plane command with project root for identification
	controlPlaneCommand := fmt.Sprintf("co control --root %s", projectRoot)

	// Create a new tab
	fmt.Fprintf(w, "Creating control plane tab in session %s\n", sessionName)
	if err := zc.CreateTab(ctx, sessionName, TabName, projectRoot); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	// Switch to the tab and execute
	if err := zc.SwitchToTab(ctx, sessionName, TabName); err != nil {
		fmt.Fprintf(w, "Warning: failed to switch to tab: %v\n", err)
	}

	fmt.Fprintf(w, "Executing: %s\n", controlPlaneCommand)
	if err := zc.ExecuteCommand(ctx, sessionName, controlPlaneCommand); err != nil {
		return fmt.Errorf("failed to execute control plane command: %w", err)
	}

	fmt.Fprintf(w, "Control plane spawned in zellij session %s, tab %s\n", sessionName, TabName)
	return nil
}

// EnsureControlPlane ensures the control plane is running, spawning it if needed
func EnsureControlPlane(ctx context.Context, projectName string, projectRoot string, w io.Writer) (bool, error) {
	sessionName := claude.SessionNameForProject(projectName)
	zc := zellij.New()

	// Check if session exists
	exists, _ := zc.SessionExists(ctx, sessionName)
	if !exists {
		// No session yet - will be created when needed
		return false, nil
	}

	// Check if control plane tab exists
	tabExists, _ := zc.TabExists(ctx, sessionName, TabName)
	if !tabExists {
		// No tab - spawn control plane
		if err := SpawnControlPlane(ctx, projectName, projectRoot, w); err != nil {
			return false, err
		}
		return true, nil
	}

	// Tab exists - check if process is running for this specific project
	if IsControlPlaneRunning(ctx, projectRoot) {
		// Process is running
		return false, nil
	}

	// Tab exists but process is dead - restart
	fmt.Fprintf(w, "Control plane tab exists but process is dead - restarting...\n")

	// Try to close the dead tab first
	if err := zc.SwitchToTab(ctx, sessionName, TabName); err == nil {
		zc.SendCtrlC(ctx, sessionName)
		time.Sleep(zc.CtrlCDelay)
		zc.CloseTab(ctx, sessionName)
		time.Sleep(500 * time.Millisecond)
	}

	// Spawn a new one
	if err := SpawnControlPlane(ctx, projectName, projectRoot, w); err != nil {
		return false, err
	}
	return true, nil
}

// IsControlPlaneRunning checks if the control plane is running for a specific project
func IsControlPlaneRunning(ctx context.Context, projectRoot string) bool {
	pattern := fmt.Sprintf("co control --root %s", projectRoot)
	running, _ := process.IsProcessRunning(ctx, pattern)
	return running
}
