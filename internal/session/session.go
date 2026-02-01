// Package session provides zellij session management for the co orchestrator.
// This package is separate from control to avoid import cycles - work can import
// session without creating a cycle with control (which imports work).
package session

import (
	"context"
	"fmt"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/zellij"
)

// ControlPlaneTabName is the name of the control plane tab in zellij
const ControlPlaneTabName = "control"

// InitResult contains information about session initialization
type InitResult struct {
	// SessionCreated is true if a new zellij session was created
	SessionCreated bool
	// SessionName is the name of the zellij session (e.g., "co-myproject")
	SessionName string
}

// Initialize ensures a zellij session exists with the control plane running.
// When a new session is created, it starts the control plane in the initial tab.
// Returns information about whether a new session was created.
func Initialize(ctx context.Context, proj *project.Project) (*InitResult, error) {
	projectName := proj.Config.Project.Name
	sessionName := project.SessionNameForProject(projectName)
	zc := zellij.New()

	// Ensure session exists with control plane as the initial tab
	sessionCreated, err := zc.EnsureSessionWithCommand(ctx, sessionName, ControlPlaneTabName, proj.Root, "co", []string{"control", "--root", proj.Root})
	if err != nil {
		return nil, fmt.Errorf("failed to ensure zellij session: %w", err)
	}

	result := &InitResult{
		SessionCreated: sessionCreated,
		SessionName:    sessionName,
	}

	if sessionCreated {
		logging.Debug("New zellij session created with control plane", "sessionName", sessionName)
	}

	return result, nil
}

// EnsureControlPlane ensures the control plane is running, spawning it if needed
func EnsureControlPlane(ctx context.Context, proj *project.Project) error {
	projectName := proj.Config.Project.Name
	sessionName := project.SessionNameForProject(projectName)
	zc := zellij.New()

	// Check if session exists
	exists, err := zc.SessionExists(ctx, sessionName)
	if err != nil {
		return fmt.Errorf("failed to check session existence: %w", err)
	}
	if !exists {
		// No session yet - initialize it (which spawns control plane via layout)
		_, err := Initialize(ctx, proj)
		return err
	}

	// Check if control plane tab exists
	session := zc.Session(sessionName)
	tabExists, err := session.TabExists(ctx, ControlPlaneTabName)
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
	if err := session.SwitchToTab(ctx, ControlPlaneTabName); err == nil {
		_ = session.SendCtrlC(ctx)
		time.Sleep(zc.CtrlCDelay)
		_ = session.CloseTab(ctx)
		time.Sleep(500 * time.Millisecond)
	}

	// Spawn a new one
	if err := SpawnControlPlane(ctx, proj); err != nil {
		return err
	}
	return nil
}

// SpawnControlPlane spawns the control plane in a zellij tab.
// The session must already exist (use Initialize or EnsureControlPlane instead
// of calling this directly).
func SpawnControlPlane(ctx context.Context, proj *project.Project) error {
	projectName := proj.Config.Project.Name
	projectRoot := proj.Root
	sessionName := project.SessionNameForProject(projectName)
	zc := zellij.New()

	logging.Debug("SpawnControlPlane started", "sessionName", sessionName, "projectRoot", projectRoot)

	// Check if control plane tab already exists
	session := zc.Session(sessionName)
	tabExists, _ := session.TabExists(ctx, ControlPlaneTabName)
	logging.Debug("SpawnControlPlane TabExists check", "tabExists", tabExists)
	if tabExists {
		return nil
	}

	// Create control plane tab with command using layout
	// This avoids race conditions from creating a tab then executing a command
	logging.Debug("SpawnControlPlane creating tab with command", "tabName", ControlPlaneTabName)
	if err := session.CreateTabWithCommand(ctx, ControlPlaneTabName, projectRoot, "co", []string{"control", "--root", projectRoot}, "control"); err != nil {
		logging.Error("SpawnControlPlane CreateTabWithCommand failed", "error", err)
		return fmt.Errorf("failed to create control plane tab: %w", err)
	}
	logging.Debug("SpawnControlPlane completed successfully")

	return nil
}
