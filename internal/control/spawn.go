package control

import (
	"context"

	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/session"
)

// TabName is the name of the control plane tab in zellij
// Deprecated: Use session.ControlPlaneTabName instead
const TabName = session.ControlPlaneTabName

// SessionInitResult contains information about session initialization
// Deprecated: Use session.InitResult instead
type SessionInitResult = session.InitResult

// SpawnControlPlane spawns the control plane in a zellij tab.
// Deprecated: Use session.SpawnControlPlane instead
func SpawnControlPlane(ctx context.Context, proj *project.Project) error {
	return session.SpawnControlPlane(ctx, proj)
}

// EnsureControlPlane ensures the control plane is running, spawning it if needed
// Deprecated: Use session.EnsureControlPlane instead
func EnsureControlPlane(ctx context.Context, proj *project.Project) error {
	return session.EnsureControlPlane(ctx, proj)
}

// InitializeSession ensures a zellij session exists with the control plane running.
// Deprecated: Use session.Initialize instead
func InitializeSession(ctx context.Context, proj *project.Project) (*SessionInitResult, error) {
	return session.Initialize(ctx, proj)
}
