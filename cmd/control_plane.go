package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/process"
	"github.com/newhook/co/internal/project"
	trackingwatcher "github.com/newhook/co/internal/tracking/watcher"
	"github.com/newhook/co/internal/worktree"
	"github.com/newhook/co/internal/zellij"
	"github.com/spf13/cobra"
)

// ControlPlaneTabName is the name of the control plane tab in zellij
const ControlPlaneTabName = "control-plane"

var controlPlaneCmd = &cobra.Command{
	Use:    "control-plane",
	Short:  "Run the control plane for background task execution",
	Long:   `The control plane runs as a long-lived process that watches for scheduled tasks across all works and executes them with retry support.`,
	Hidden: true,
	RunE:   runControlPlane,
}

func init() {
	rootCmd.AddCommand(controlPlaneCmd)
}

func runControlPlane(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	// Apply hooks.env to current process - inherited by child processes
	applyHooksEnv(proj.Config.Hooks.Env)

	fmt.Println("=== Control Plane Started ===")
	fmt.Printf("Project: %s\n", proj.Config.Project.Name)
	fmt.Println("Watching for scheduled tasks across all works...")

	// Start the control plane loop
	return runControlPlaneLoop(ctx, proj)
}

// runControlPlaneLoop runs the main control plane event loop
func runControlPlaneLoop(ctx context.Context, proj *project.Project) error {
	// Initialize tracking database watcher
	trackingDBPath := filepath.Join(proj.Root, ".co", "tracking.db")
	watcher, err := trackingwatcher.New(trackingwatcher.DefaultConfig(trackingDBPath))
	if err != nil {
		return fmt.Errorf("failed to create tracking watcher: %w", err)
	}

	if err := watcher.Start(); err != nil {
		watcher.Stop()
		return fmt.Errorf("failed to start tracking watcher: %w", err)
	}
	defer watcher.Stop()

	logging.Info("Control plane started with database events")

	// Subscribe to watcher events
	sub := watcher.Broker().Subscribe(ctx)

	// Set up periodic check timer (safety net)
	checkInterval := 30 * time.Second
	checkTimer := time.NewTimer(checkInterval)
	defer checkTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			logging.Debug("Control plane stopping due to context cancellation")
			fmt.Println("\nControl plane stopped.")
			return nil

		case event, ok := <-sub:
			if !ok {
				logging.Debug("Watcher subscription closed")
				return nil
			}

			// Handle database change event
			if event.Payload.Type == trackingwatcher.DBChanged {
				logging.Debug("Database changed, checking scheduled tasks")
				processAllDueTasks(ctx, proj)
			}

		case <-checkTimer.C:
			// Periodic check as a safety net
			logging.Debug("Control plane periodic check")
			processAllDueTasks(ctx, proj)
			checkTimer.Reset(checkInterval)
		}
	}
}

// processAllDueTasks checks for and executes any scheduled tasks that are due across all works
func processAllDueTasks(ctx context.Context, proj *project.Project) {
	// Get the next due task globally (not work-specific)
	for {
		task, err := proj.DB.GetNextScheduledTask(ctx)
		if err != nil {
			logging.Warn("failed to get next scheduled task", "error", err)
			return
		}

		if task == nil {
			// No more due tasks
			return
		}

		logging.Info("Executing scheduled task",
			"task_id", task.ID,
			"task_type", task.TaskType,
			"work_id", task.WorkID,
			"scheduled_at", task.ScheduledAt.Format(time.RFC3339))

		// Mark as executing
		if err := proj.DB.MarkTaskExecuting(ctx, task.ID); err != nil {
			logging.Warn("failed to mark task as executing", "error", err)
			continue
		}

		// Execute based on task type
		var taskErr error
		switch task.TaskType {
		case db.TaskTypeCreateWorktree:
			taskErr = handleCreateWorktreeTask(ctx, proj, task)
		case db.TaskTypeSpawnOrchestrator:
			taskErr = handleSpawnOrchestratorTask(ctx, proj, task)
		case db.TaskTypePRFeedback:
			handlePRFeedbackTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeCommentResolution:
			handleCommentResolutionTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeGitPush:
			taskErr = handleGitPushTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeGitHubComment:
			taskErr = handleGitHubCommentTask(ctx, proj, task.WorkID, task)
		case db.TaskTypeGitHubResolveThread:
			taskErr = handleGitHubResolveThreadTask(ctx, proj, task.WorkID, task)
		default:
			taskErr = fmt.Errorf("unknown task type: %s", task.TaskType)
		}

		// Handle task result
		if taskErr != nil {
			handleTaskError(ctx, proj, task, taskErr.Error())
		}
	}
}

// handleCreateWorktreeTask handles a scheduled worktree creation task
func handleCreateWorktreeTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	branchName := task.Metadata["branch"]
	baseBranch := task.Metadata["base_branch"]
	workerName := task.Metadata["worker_name"]

	if baseBranch == "" {
		baseBranch = "main"
	}

	logging.Info("Creating worktree for work",
		"work_id", workID,
		"branch", branchName,
		"base_branch", baseBranch,
		"attempt", task.AttemptCount+1)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		// Work was deleted - mark task as completed (nothing to do)
		logging.Info("Work not found, marking task as completed", "work_id", workID)
		proj.DB.MarkTaskCompleted(ctx, task.ID)
		return nil
	}

	// If worktree path is already set and exists, skip creation
	if work.WorktreePath != "" {
		// Worktree already created - just need to ensure git push
		logging.Info("Worktree already exists, skipping creation", "work_id", workID, "path", work.WorktreePath)
	} else {
		// Create the worktree
		workDir := filepath.Join(proj.Root, workID)
		worktreePath := filepath.Join(workDir, "tree")
		mainRepoPath := proj.MainRepoPath()

		// Create work directory
		if err := os.Mkdir(workDir, 0755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("failed to create work directory: %w", err)
		}

		// Create git worktree with new branch
		if err := worktree.Create(ctx, mainRepoPath, worktreePath, branchName, baseBranch); err != nil {
			os.RemoveAll(workDir)
			return fmt.Errorf("failed to create worktree: %w", err)
		}

		// Initialize mise if configured
		if err := mise.InitializeWithOutput(worktreePath, io.Discard); err != nil {
			logging.Warn("mise initialization failed", "error", err)
			// Non-fatal, continue
		}

		// Update work with worktree path
		if err := proj.DB.UpdateWorkWorktreePath(ctx, workID, worktreePath); err != nil {
			return fmt.Errorf("failed to update work worktree path: %w", err)
		}
	}

	// Attempt git push
	work, _ = proj.DB.GetWork(ctx, workID) // Refresh work
	if work != nil && work.WorktreePath != "" {
		if err := git.PushSetUpstreamInDir(ctx, branchName, work.WorktreePath); err != nil {
			return fmt.Errorf("git push failed: %w", err)
		}
	}

	logging.Info("Worktree created and pushed successfully", "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark task as completed", "error", err)
	}

	// Schedule orchestrator spawn task
	_, err = proj.DB.ScheduleTask(ctx, workID, db.TaskTypeSpawnOrchestrator, time.Now(), map[string]string{
		"worker_name": workerName,
	})
	if err != nil {
		logging.Warn("failed to schedule orchestrator spawn", "error", err, "work_id", workID)
	}

	return nil
}

// handleSpawnOrchestratorTask handles a scheduled orchestrator spawn task
func handleSpawnOrchestratorTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	workerName := task.Metadata["worker_name"]

	logging.Info("Spawning orchestrator for work",
		"work_id", workID,
		"attempt", task.AttemptCount+1)

	// Get work details
	work, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		// Work was deleted - mark task as completed (nothing to do)
		logging.Info("Work not found, marking task as completed", "work_id", workID)
		proj.DB.MarkTaskCompleted(ctx, task.ID)
		return nil
	}

	if work.WorktreePath == "" {
		return fmt.Errorf("work %s has no worktree path", workID)
	}

	// Spawn the orchestrator
	if err := claude.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, work.WorktreePath, workerName, io.Discard); err != nil {
		return fmt.Errorf("failed to spawn orchestrator: %w", err)
	}

	logging.Info("Orchestrator spawned successfully", "work_id", workID)

	// Mark task as completed
	if err := proj.DB.MarkTaskCompleted(ctx, task.ID); err != nil {
		logging.Warn("failed to mark task as completed", "error", err)
	}

	return nil
}

// SpawnControlPlane spawns the control plane in a zellij tab
func SpawnControlPlane(ctx context.Context, projectName string, projectRoot string, w io.Writer) error {
	sessionName := claude.SessionNameForProject(projectName)
	zc := zellij.New()

	// Ensure session exists
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		return err
	}

	// Check if control plane tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, ControlPlaneTabName)
	if tabExists {
		fmt.Fprintf(w, "Control plane tab already exists\n")
		return nil
	}

	// Build the control plane command
	controlPlaneCommand := "co control-plane"

	// Create a new tab
	fmt.Fprintf(w, "Creating control plane tab in session %s\n", sessionName)
	if err := zc.CreateTab(ctx, sessionName, ControlPlaneTabName, projectRoot); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	// Switch to the tab and execute
	if err := zc.SwitchToTab(ctx, sessionName, ControlPlaneTabName); err != nil {
		fmt.Fprintf(w, "Warning: failed to switch to tab: %v\n", err)
	}

	fmt.Fprintf(w, "Executing: %s\n", controlPlaneCommand)
	if err := zc.ExecuteCommand(ctx, sessionName, controlPlaneCommand); err != nil {
		return fmt.Errorf("failed to execute control plane command: %w", err)
	}

	fmt.Fprintf(w, "Control plane spawned in zellij session %s, tab %s\n", sessionName, ControlPlaneTabName)
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
	tabExists, _ := zc.TabExists(ctx, sessionName, ControlPlaneTabName)
	if !tabExists {
		// No tab - spawn control plane
		if err := SpawnControlPlane(ctx, projectName, projectRoot, w); err != nil {
			return false, err
		}
		return true, nil
	}

	// Tab exists - check if process is running
	pattern := "co control-plane"
	if running, err := process.IsProcessRunning(ctx, pattern); err == nil && running {
		// Process is running
		return false, nil
	}

	// Tab exists but process is dead - restart
	fmt.Fprintf(w, "Control plane tab exists but process is dead - restarting...\n")

	// Try to close the dead tab first
	if err := zc.SwitchToTab(ctx, sessionName, ControlPlaneTabName); err == nil {
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

// IsControlPlaneRunning checks if the control plane is running
func IsControlPlaneRunning(ctx context.Context) bool {
	pattern := "co control-plane"
	running, _ := process.IsProcessRunning(ctx, pattern)
	return running
}
