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

	logging.Debug("SpawnControlPlane started", "sessionName", sessionName, "projectRoot", projectRoot)

	// Ensure session exists
	if _, err := zc.EnsureSession(ctx, sessionName); err != nil {
		logging.Error("SpawnControlPlane EnsureSession failed", "error", err)
		return err
	}
	logging.Debug("SpawnControlPlane EnsureSession completed")

	// Check if control plane tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, TabName)
	logging.Debug("SpawnControlPlane TabExists check", "tabExists", tabExists)
	if tabExists {
		return nil
	}

	// Create control plane tab with command using layout
	// This avoids race conditions from creating a tab then executing a command
	logging.Debug("SpawnControlPlane creating tab with command", "tabName", TabName)
	if err := zc.CreateTabWithCommand(ctx, sessionName, TabName, projectRoot, "co", []string{"control", "--root", projectRoot}, "control"); err != nil {
		logging.Error("SpawnControlPlane CreateTabWithCommand failed", "error", err)
		return fmt.Errorf("failed to create control plane tab: %w", err)
	}
	logging.Debug("SpawnControlPlane completed successfully")

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
		// No session yet - initialize it (which spawns control plane via layout)
		_, err := InitializeSession(ctx, proj)
		return err
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
		_ = zc.SendCtrlC(ctx, sessionName)
		time.Sleep(zc.CtrlCDelay)
		_ = zc.CloseTab(ctx, sessionName)
		time.Sleep(500 * time.Millisecond)
	}

	// Spawn a new one
	if err := SpawnControlPlane(ctx, proj); err != nil {
		return err
	}
	return nil
}

// SessionInitResult contains information about session initialization
type SessionInitResult struct {
	// SessionCreated is true if a new zellij session was created
	SessionCreated bool
	// SessionName is the name of the zellij session (e.g., "co-myproject")
	SessionName string
}

// InitializeSession ensures a zellij session exists with the control plane running.
// When a new session is created, it uses a layout to start the control plane automatically.
// Returns information about whether a new session was created.
func InitializeSession(ctx context.Context, proj *project.Project) (*SessionInitResult, error) {
	projectName := proj.Config.Project.Name
	sessionName := claude.SessionNameForProject(projectName)
	zc := zellij.New()

	// Ensure session exists with layout (control plane starts automatically)
	sessionCreated, err := zc.EnsureSessionWithLayout(ctx, sessionName, proj.Root)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure zellij session: %w", err)
	}

	result := &SessionInitResult{
		SessionCreated: sessionCreated,
		SessionName:    sessionName,
	}

	if sessionCreated {
		logging.Debug("New zellij session created with control plane layout", "sessionName", sessionName)
	}

	return result, nil
}
