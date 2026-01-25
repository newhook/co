package control

import (
	"context"
	"fmt"
	"time"

	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/zellij"
)

// TabName is the name of the control plane tab in zellij
const TabName = "control"

// SpawnControlPlane spawns the control plane in a zellij tab
func SpawnControlPlane(ctx context.Context, proj *project.Project) error {
	projectName := proj.Config.Project.Name
	projectRoot := proj.Root
	sessionName := claude.SessionNameForProject(projectName)
	zc := zellij.New()

	// Ensure session exists
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		return err
	}

	// Check if control plane tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, TabName)
	if tabExists {
		return nil
	}

	// Build the control plane command with project root for identification
	controlPlaneCommand := fmt.Sprintf("co control --root %s", projectRoot)

	// Create a new tab
	if err := zc.CreateTab(ctx, sessionName, TabName, projectRoot); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	// Switch to the tab and execute
	if err := zc.SwitchToTab(ctx, sessionName, TabName); err != nil {
		return fmt.Errorf("switching to tab failed: %w", err)
	}

	if err := zc.ExecuteCommand(ctx, sessionName, controlPlaneCommand); err != nil {
		return fmt.Errorf("failed to execute control plane command: %w", err)
	}

	return nil
}

// EnsureControlPlane ensures the control plane is running, spawning it if needed
func EnsureControlPlane(ctx context.Context, proj *project.Project) error {
	projectName := proj.Config.Project.Name
	sessionName := claude.SessionNameForProject(projectName)
	zc := zellij.New()

	// Check if session exists
	exists, err := zc.SessionExists(ctx, sessionName)
	if err != nil {
		return fmt.Errorf("failed to check session existence: %w", err)
	}
	if !exists {
		// No session yet - will be created when needed
		return nil
	}

	// Check if control plane tab exists
	tabExists, err := zc.TabExists(ctx, sessionName, TabName)
	if err != nil {
		return fmt.Errorf("failed to check tab existence: %w", err)
	}
	if !tabExists {
		// No tab - spawn control plane
		if err := SpawnControlPlane(ctx, proj); err != nil {
			return err
		}
		return nil
	}

	// Tab exists - check if control plane has a recent heartbeat
	alive, err := proj.DB.IsControlPlaneAlive(ctx, db.DefaultStalenessThreshold)
	if err != nil {
		return fmt.Errorf("failed to check control plane status: %w", err)
	}
	if alive {
		// Control plane is alive
		return nil
	}

	// Tab exists but process is dead - restart
	logging.Debug("Control plane tab exists but process is dead - restarting...")

	// Try to close the dead tab first
	if err := zc.SwitchToTab(ctx, sessionName, TabName); err == nil {
		zc.SendCtrlC(ctx, sessionName)
		time.Sleep(zc.CtrlCDelay)
		zc.CloseTab(ctx, sessionName)
		time.Sleep(500 * time.Millisecond)
	}

	// Spawn a new one
	if err := SpawnControlPlane(ctx, proj); err != nil {
		return err
	}
	return nil
}

// IsControlPlaneRunning checks if the control plane is running using database heartbeat.
// Deprecated: Use proj.DB.IsControlPlaneAlive directly instead.
func IsControlPlaneRunning(ctx context.Context, proj *project.Project) (bool, error) {
	alive, err := proj.DB.IsControlPlaneAlive(ctx, db.DefaultStalenessThreshold)
	if err != nil {
		return false, fmt.Errorf("failed to check control plane status: %w", err)
	}
	return alive, nil
}
