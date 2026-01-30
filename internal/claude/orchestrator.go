package claude

import (
	"context"
	"io"

	"github.com/newhook/co/internal/db"
)

// OrchestratorManager provides operations for managing work orchestrators.
// This interface enables dependency injection and testing of orchestrator management.
//
//go:generate moq -stub -out orchestrator_mock.go . OrchestratorManager:OrchestratorManagerMock
type OrchestratorManager interface {
	// EnsureWorkOrchestrator checks if a work orchestrator tab exists and spawns one if not.
	// Returns true if the orchestrator was spawned, false if it was already running.
	EnsureWorkOrchestrator(ctx context.Context, workID, projName, workDir, friendlyName string, w io.Writer) (bool, error)

	// SpawnWorkOrchestrator creates a zellij tab and runs the orchestrate command for a work unit.
	SpawnWorkOrchestrator(ctx context.Context, workID, projName, workDir, friendlyName string, w io.Writer) error

	// TerminateWorkTabs terminates all zellij tabs associated with a work unit.
	TerminateWorkTabs(ctx context.Context, workID, projName string, w io.Writer) error
}

// DefaultOrchestratorManager is the default implementation of OrchestratorManager.
// It wraps the package-level functions and holds the database reference needed
// for orchestrator heartbeat checking.
type DefaultOrchestratorManager struct {
	database *db.DB
}

// NewOrchestratorManager creates a new DefaultOrchestratorManager with the given database.
func NewOrchestratorManager(database *db.DB) OrchestratorManager {
	return &DefaultOrchestratorManager{database: database}
}

// EnsureWorkOrchestrator implements OrchestratorManager.
func (m *DefaultOrchestratorManager) EnsureWorkOrchestrator(ctx context.Context, workID, projName, workDir, friendlyName string, w io.Writer) (bool, error) {
	return EnsureWorkOrchestrator(ctx, m.database, workID, projName, workDir, friendlyName, w)
}

// SpawnWorkOrchestrator implements OrchestratorManager.
func (m *DefaultOrchestratorManager) SpawnWorkOrchestrator(ctx context.Context, workID, projName, workDir, friendlyName string, w io.Writer) error {
	return SpawnWorkOrchestrator(ctx, workID, projName, workDir, friendlyName, w)
}

// TerminateWorkTabs implements OrchestratorManager.
func (m *DefaultOrchestratorManager) TerminateWorkTabs(ctx context.Context, workID, projName string, w io.Writer) error {
	return TerminateWorkTabs(ctx, workID, projName, w)
}
