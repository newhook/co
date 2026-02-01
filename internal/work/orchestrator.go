package work

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/zellij"
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

// TabExists checks if a tab with the given name exists in the session.
func TabExists(ctx context.Context, sessionName, tabName string) bool {
	zc := zellij.New()
	exists, _ := zc.TabExists(ctx, sessionName, tabName)
	return exists
}

// TerminateWorkTabs terminates all zellij tabs associated with a work unit.
// This includes the work orchestrator tab (work-<workID>), task tabs (task-<workID>.*),
// console tabs (console-<workID>*), and claude tabs (claude-<workID>*).
// Each tab's running process is terminated with Ctrl+C before the tab is closed.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func TerminateWorkTabs(ctx context.Context, workID string, projectName string, w io.Writer) error {
	sessionName := project.SessionNameForProject(projectName)
	zc := zellij.New()

	logging.Debug("TerminateWorkTabs starting",
		"work_id", workID,
		"session_name", sessionName)

	// Check if session exists
	exists, err := zc.SessionExists(ctx, sessionName)
	if err != nil || !exists {
		logging.Debug("Session does not exist, nothing to terminate",
			"work_id", workID,
			"session_name", sessionName,
			"exists", exists,
			"error", err)
		return nil
	}

	// Get list of all tab names
	tabNames, err := zc.QueryTabNames(ctx, sessionName)
	if err != nil {
		logging.Warn("Failed to query tab names",
			"work_id", workID,
			"session_name", sessionName,
			"error", err)
		return nil
	}

	logging.Debug("Queried tab names",
		"work_id", workID,
		"tab_count", len(tabNames),
		"tabs", tabNames)

	// Find tabs to terminate
	// Use prefix matching for all tab types to handle friendly names
	// Tab names can be "prefix-workID" or "prefix-workID (friendlyName)"
	workTabPrefix := fmt.Sprintf("work-%s", workID)
	taskTabPrefix := fmt.Sprintf("task-%s.", workID)
	consoleTabPrefix := fmt.Sprintf("console-%s", workID)
	claudeTabPrefix := fmt.Sprintf("claude-%s", workID)

	var tabsToClose []string
	for _, tabName := range tabNames {
		tabName = strings.TrimSpace(tabName)
		if tabName == "" {
			continue
		}
		// Match work orchestrator tab, task tabs, console tabs, or claude tabs for this work
		// Use prefix matching for work tabs too since they may include a friendly name suffix
		if strings.HasPrefix(tabName, workTabPrefix) ||
			strings.HasPrefix(tabName, taskTabPrefix) ||
			strings.HasPrefix(tabName, consoleTabPrefix) ||
			strings.HasPrefix(tabName, claudeTabPrefix) {
			tabsToClose = append(tabsToClose, tabName)
		}
	}

	if len(tabsToClose) == 0 {
		logging.Debug("No matching tabs to close", "work_id", workID)
		return nil
	}

	logging.Debug("Found tabs to close",
		"work_id", workID,
		"tabs_to_close", tabsToClose)

	fmt.Fprintf(w, "Terminating %d zellij tab(s) for work %s...\n", len(tabsToClose), workID)

	for _, tabName := range tabsToClose {
		logging.Debug("Closing tab",
			"work_id", workID,
			"tab_name", tabName)
		if err := zc.TerminateAndCloseTab(ctx, sessionName, tabName); err != nil {
			logging.Warn("Failed to terminate tab",
				"work_id", workID,
				"tab_name", tabName,
				"error", err)
			fmt.Fprintf(w, "Warning: failed to terminate tab %s: %v\n", tabName, err)
			// Continue with other tabs
		} else {
			logging.Debug("Tab closed successfully",
				"work_id", workID,
				"tab_name", tabName)
			fmt.Fprintf(w, "  Terminated tab: %s\n", tabName)
		}
	}

	logging.Debug("TerminateWorkTabs completed", "work_id", workID)
	return nil
}

// SpawnWorkOrchestrator creates a zellij tab and runs the orchestrate command for a work unit.
// The tab is named "work-<work-id>" or "work-<work-id> (friendlyName)" for easy identification.
// The function returns immediately after spawning - the orchestrator runs in the tab.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
//
// IMPORTANT: The zellij session must already exist before calling this function.
// Callers should use control.InitializeSession or control.EnsureControlPlane to ensure
// the session exists with the control plane running.
func SpawnWorkOrchestrator(ctx context.Context, workID string, projectName string, workDir string, friendlyName string, w io.Writer) error {
	logging.Debug("SpawnWorkOrchestrator called", "workID", workID, "projectName", projectName, "workDir", workDir)
	sessionName := project.SessionNameForProject(projectName)
	tabName := project.FormatTabName("work", workID, friendlyName)
	zc := zellij.New()

	// Verify session exists - callers must initialize it with control plane
	logging.Debug("SpawnWorkOrchestrator checking session exists", "sessionName", sessionName)
	exists, err := zc.SessionExists(ctx, sessionName)
	if err != nil {
		logging.Error("SpawnWorkOrchestrator SessionExists check failed", "sessionName", sessionName, "error", err)
		return fmt.Errorf("failed to check session existence: %w", err)
	}
	if !exists {
		logging.Error("SpawnWorkOrchestrator session does not exist", "sessionName", sessionName)
		return fmt.Errorf("zellij session %s does not exist - call control.InitializeSession first", sessionName)
	}

	// Check if tab already exists
	tabExists, err := zc.TabExists(ctx, sessionName, tabName)
	if err != nil {
		return fmt.Errorf("failed to check if tab exists: %w", err)
	}
	if tabExists {
		fmt.Fprintf(w, "Tab %s already exists, terminating and recreating...\n", tabName)

		// Terminate and close the existing tab
		if err := zc.TerminateAndCloseTab(ctx, sessionName, tabName); err != nil {
			fmt.Fprintf(w, "Warning: failed to terminate existing tab: %v\n", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Create a new tab with the orchestrate command using a layout
	fmt.Fprintf(w, "Creating tab: %s in session %s\n", tabName, sessionName)
	if err := zc.CreateTabWithCommand(ctx, sessionName, tabName, workDir, "co", []string{"orchestrate", "--work", workID}, "orchestrator"); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	logging.Debug("SpawnWorkOrchestrator completed successfully", "workID", workID, "sessionName", sessionName, "tabName", tabName)
	fmt.Fprintf(w, "Work orchestrator spawned in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}

// EnsureWorkOrchestrator checks if a work orchestrator tab exists and spawns one if not.
// This is used for resilience - if the orchestrator crashes or is killed, it can be restarted.
// Returns true if the orchestrator was spawned, false if it was already running.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
// The database parameter is used to check orchestrator heartbeat status.
func EnsureWorkOrchestrator(ctx context.Context, database *db.DB, workID string, projectName string, workDir string, friendlyName string, w io.Writer) (bool, error) {
	sessionName := project.SessionNameForProject(projectName)
	tabName := project.FormatTabName("work", workID, friendlyName)

	// Check if the orchestrator is alive via database heartbeat
	if TabExists(ctx, sessionName, tabName) {
		if alive, err := database.IsOrchestratorAlive(ctx, workID, db.DefaultStalenessThreshold); err == nil && alive {
			fmt.Fprintf(w, "Work orchestrator tab %s already exists and orchestrator is alive\n", tabName)
			return false, nil
		}
		// Tab exists but orchestrator is dead - SpawnWorkOrchestrator will terminate and recreate
		fmt.Fprintf(w, "Work orchestrator tab %s exists but orchestrator is dead - restarting...\n", tabName)
	}

	// Spawn the orchestrator (handles existing tab termination)
	if err := SpawnWorkOrchestrator(ctx, workID, projectName, workDir, friendlyName, w); err != nil {
		return false, err
	}

	return true, nil
}
