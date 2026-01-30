package control

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/feedback"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/work"
	"github.com/newhook/co/internal/worktree"
	"github.com/newhook/co/internal/zellij"
)

// OrchestratorSpawner defines the interface for spawning work orchestrators.
// This abstraction enables testing without actual zellij operations.
type OrchestratorSpawner interface {
	SpawnWorkOrchestrator(ctx context.Context, workID, projectName, workDir, friendlyName string, w io.Writer) error
}

// WorkDestroyer defines the interface for destroying work units.
// This abstraction enables testing without actual file system operations.
type WorkDestroyer interface {
	DestroyWork(ctx context.Context, proj *project.Project, workID string, w io.Writer) error
}

// DefaultOrchestratorSpawner implements OrchestratorSpawner using the claude package.
type DefaultOrchestratorSpawner struct{}

// SpawnWorkOrchestrator implements OrchestratorSpawner.
func (d *DefaultOrchestratorSpawner) SpawnWorkOrchestrator(ctx context.Context, workID, projectName, workDir, friendlyName string, w io.Writer) error {
	return claude.SpawnWorkOrchestrator(ctx, workID, projectName, workDir, friendlyName, w)
}

// DefaultWorkDestroyer implements WorkDestroyer using the work package.
type DefaultWorkDestroyer struct{}

// DestroyWork implements WorkDestroyer.
func (d *DefaultWorkDestroyer) DestroyWork(ctx context.Context, proj *project.Project, workID string, w io.Writer) error {
	return work.DestroyWork(ctx, proj, workID, w)
}

// ControlPlane manages the execution of scheduled tasks with injectable dependencies.
// It allows for testing without actual CLI tools, services, or file system operations.
type ControlPlane struct {
	Git                  git.Operations
	Worktree             worktree.Operations
	Zellij               zellij.SessionManager
	Mise                 func(dir string) mise.Operations
	FeedbackProcessor    feedback.Processor
	OrchestratorSpawner  OrchestratorSpawner
	WorkDestroyer        WorkDestroyer
}

// NewControlPlane creates a new ControlPlane with default production dependencies.
func NewControlPlane() *ControlPlane {
	return &ControlPlane{
		Git:                  git.NewOperations(),
		Worktree:             worktree.NewOperations(),
		Zellij:               zellij.New(),
		Mise:                 mise.NewOperations,
		FeedbackProcessor:    feedback.NewProcessor(),
		OrchestratorSpawner:  &DefaultOrchestratorSpawner{},
		WorkDestroyer:        &DefaultWorkDestroyer{},
	}
}

// NewControlPlaneWithDeps creates a new ControlPlane with provided dependencies for testing.
func NewControlPlaneWithDeps(
	gitOps git.Operations,
	wtOps worktree.Operations,
	zellijMgr zellij.SessionManager,
	miseOps func(dir string) mise.Operations,
	feedbackProc feedback.Processor,
	orchestratorSpawner OrchestratorSpawner,
	workDestroyer WorkDestroyer,
) *ControlPlane {
	return &ControlPlane{
		Git:                  gitOps,
		Worktree:             wtOps,
		Zellij:               zellijMgr,
		Mise:                 miseOps,
		FeedbackProcessor:    feedbackProc,
		OrchestratorSpawner:  orchestratorSpawner,
		WorkDestroyer:        workDestroyer,
	}
}

// HandleCreateWorktreeTask handles a scheduled worktree creation task.
func (cp *ControlPlane) HandleCreateWorktreeTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	branchName := task.Metadata["branch"]
	baseBranch := task.Metadata["base_branch"]
	workerName := task.Metadata["worker_name"]
	useExisting := task.Metadata["use_existing"] == "true"

	if baseBranch == "" {
		baseBranch = proj.Config.Repo.GetBaseBranch()
	}

	logging.Info("Creating worktree for work",
		"work_id", workID,
		"branch", branchName,
		"base_branch", baseBranch,
		"use_existing", useExisting,
		"attempt", task.AttemptCount+1)

	// Get work details
	workRecord, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if workRecord == nil {
		// Work was deleted - nothing to do
		logging.Info("Work not found, task will be marked completed", "work_id", workID)
		return nil
	}

	mainRepoPath := proj.MainRepoPath()
	var branchExistsOnRemote bool

	// For existing branches, check if branch exists on remote
	if useExisting {
		_, branchExistsOnRemote, _ = cp.Git.ValidateExistingBranch(ctx, mainRepoPath, branchName)
	}

	// If worktree path is already set and exists, skip creation
	if workRecord.WorktreePath != "" {
		// Worktree already created - just need to ensure git push
		logging.Info("Worktree already exists, skipping creation", "work_id", workID, "path", workRecord.WorktreePath)
	} else {
		// Create the worktree
		workDir := filepath.Join(proj.Root, workID)
		worktreePath := filepath.Join(workDir, "tree")

		// Create work directory
		if err := os.Mkdir(workDir, 0750); err != nil && !os.IsExist(err) {
			return fmt.Errorf("failed to create work directory: %w", err)
		}

		if useExisting {
			// For existing branches, fetch the branch first
			if err := cp.Git.FetchBranch(ctx, mainRepoPath, branchName); err != nil {
				// Ignore fetch errors - branch might only exist locally
				logging.Debug("Could not fetch branch from origin (may only exist locally)", "branch", branchName)
			}

			// Create worktree from existing branch
			if err := cp.Worktree.CreateFromExisting(ctx, mainRepoPath, worktreePath, branchName); err != nil {
				_ = os.RemoveAll(workDir)
				return fmt.Errorf("failed to create worktree from existing branch: %w", err)
			}
		} else {
			// Fetch latest from origin for the base branch
			if err := cp.Git.FetchBranch(ctx, mainRepoPath, baseBranch); err != nil {
				_ = os.RemoveAll(workDir)
				return fmt.Errorf("failed to fetch base branch: %w", err)
			}

			// Create git worktree with new branch based on origin/<baseBranch>
			if err := cp.Worktree.Create(ctx, mainRepoPath, worktreePath, branchName, "origin/"+baseBranch); err != nil {
				_ = os.RemoveAll(workDir)
				return fmt.Errorf("failed to create worktree: %w", err)
			}
		}

		// Initialize mise if configured
		miseOps := cp.Mise(worktreePath)
		if err := miseOps.InitializeWithOutput(io.Discard); err != nil {
			logging.Warn("mise initialization failed", "error", err)
			// Non-fatal, continue
		}

		// Update work with worktree path
		if err := proj.DB.UpdateWorkWorktreePath(ctx, workID, worktreePath); err != nil {
			return fmt.Errorf("failed to update work worktree path: %w", err)
		}
	}

	// Attempt git push (skip for existing branches that already exist on remote)
	workRecord, _ = proj.DB.GetWork(ctx, workID) // Refresh work
	if workRecord != nil && workRecord.WorktreePath != "" {
		if useExisting && branchExistsOnRemote {
			logging.Info("Skipping git push - branch already exists on remote", "branch", branchName)
		} else {
			if err := cp.Git.PushSetUpstream(ctx, branchName, workRecord.WorktreePath); err != nil {
				return fmt.Errorf("git push failed: %w", err)
			}
		}
	}

	logging.Info("Worktree created and pushed successfully", "work_id", workID)

	// Schedule orchestrator spawn task
	_, err = proj.DB.ScheduleTask(ctx, workID, db.TaskTypeSpawnOrchestrator, time.Now(), map[string]string{
		"worker_name": workerName,
	})
	if err != nil {
		logging.Warn("failed to schedule orchestrator spawn", "error", err, "work_id", workID)
	}

	return nil
}

// HandleSpawnOrchestratorTask handles a scheduled orchestrator spawn task.
func (cp *ControlPlane) HandleSpawnOrchestratorTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	workerName := task.Metadata["worker_name"]

	logging.Info("Spawning orchestrator for work",
		"work_id", workID,
		"attempt", task.AttemptCount+1)

	// Get work details
	workRecord, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if workRecord == nil {
		// Work was deleted - nothing to do
		logging.Info("Work not found, task will be marked completed", "work_id", workID)
		return nil
	}

	if workRecord.WorktreePath == "" {
		return fmt.Errorf("work %s has no worktree path", workID)
	}

	// Spawn the orchestrator
	if err := cp.OrchestratorSpawner.SpawnWorkOrchestrator(ctx, workID, proj.Config.Project.Name, workRecord.WorktreePath, workerName, io.Discard); err != nil {
		return fmt.Errorf("failed to spawn orchestrator: %w", err)
	}

	logging.Info("Orchestrator spawned successfully", "work_id", workID)

	return nil
}

// HandleDestroyWorktreeTask handles a scheduled worktree destruction task.
func (cp *ControlPlane) HandleDestroyWorktreeTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID

	logging.Info("Destroying worktree for work",
		"work_id", workID,
		"attempt", task.AttemptCount+1)

	// Check if work still exists
	workRecord, err := proj.DB.GetWork(ctx, workID)
	if err != nil {
		return err
	}
	if workRecord == nil {
		// Work was already deleted - nothing to do
		logging.Info("Work not found, task will be marked completed", "work_id", workID)
		return nil
	}

	// Delegate to the work destroyer
	if err := cp.WorkDestroyer.DestroyWork(ctx, proj, workID, io.Discard); err != nil {
		return err
	}

	logging.Info("Worktree destroyed successfully", "work_id", workID)

	return nil
}

// HandleGitPushTask handles a scheduled git push task with retry support.
func (cp *ControlPlane) HandleGitPushTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	// Get branch and directory from metadata
	branch := task.Metadata["branch"]
	dir := task.Metadata["dir"]

	if branch == "" {
		// Try to get from work
		workRecord, err := proj.DB.GetWork(ctx, workID)
		if err != nil || workRecord == nil {
			return fmt.Errorf("failed to get work for git push: work not found")
		}
		branch = workRecord.BranchName
		dir = workRecord.WorktreePath
	}

	if branch == "" || dir == "" {
		return fmt.Errorf("git push task missing branch or dir metadata")
	}

	logging.Info("Executing git push", "branch", branch, "dir", dir, "attempt", task.AttemptCount+1)

	if err := cp.Git.PushSetUpstream(ctx, branch, dir); err != nil {
		return err
	}

	logging.Info("Git push succeeded", "branch", branch, "work_id", workID)

	return nil
}

// HandlePRFeedbackTask handles a scheduled PR feedback check.
func (cp *ControlPlane) HandlePRFeedbackTask(ctx context.Context, proj *project.Project, task *db.ScheduledTask) error {
	workID := task.WorkID
	logging.Debug("Starting PR feedback check task", "task_id", task.ID, "work_id", workID)

	// Get work details
	workRecord, err := proj.DB.GetWork(ctx, workID)
	if err != nil || workRecord == nil || workRecord.PRURL == "" {
		logging.Debug("No PR URL for work, not rescheduling", "work_id", workID, "has_pr", workRecord != nil && workRecord.PRURL != "")
		// Don't reschedule - scheduling happens when PR is created
		return nil
	}

	logging.Debug("Checking PR feedback", "pr_url", workRecord.PRURL, "work_id", workID)

	// Process PR feedback - creates beads but doesn't add them to work
	createdCount, err := cp.FeedbackProcessor.ProcessPRFeedback(ctx, proj, proj.DB, workID)
	if err != nil {
		return fmt.Errorf("failed to check PR feedback: %w", err)
	}

	if createdCount > 0 {
		logging.Info("Created beads from PR feedback", "count", createdCount, "work_id", workID)
	} else {
		logging.Debug("No new PR feedback found", "work_id", workID)
	}

	// Schedule next check using configured interval
	nextInterval := proj.Config.Scheduler.GetPRFeedbackInterval()
	nextCheck := time.Now().Add(nextInterval)
	_, err = proj.DB.ScheduleOrUpdateTask(ctx, workID, db.TaskTypePRFeedback, nextCheck)
	if err != nil {
		logging.Warn("failed to schedule next PR feedback check", "error", err, "work_id", workID)
	} else {
		logging.Info("Scheduled next PR feedback check", "work_id", workID, "next_check", nextCheck.Format(time.RFC3339), "interval", nextInterval)
	}

	return nil
}

// GetTaskHandlers returns the task handler map for the control plane.
func (cp *ControlPlane) GetTaskHandlers() map[string]TaskHandler {
	return map[string]TaskHandler{
		db.TaskTypeCreateWorktree:      cp.HandleCreateWorktreeTask,
		db.TaskTypeSpawnOrchestrator:   cp.HandleSpawnOrchestratorTask,
		db.TaskTypePRFeedback:          cp.HandlePRFeedbackTask,
		db.TaskTypeGitPush:             cp.HandleGitPushTask,
		db.TaskTypeDestroyWorktree:     cp.HandleDestroyWorktreeTask,
		// These handlers don't need ControlPlane dependencies - keep as standalone functions
		db.TaskTypeImportPR:            HandleImportPRTask,
		db.TaskTypeCommentResolution:   HandleCommentResolutionTask,
		db.TaskTypeGitHubComment:       HandleGitHubCommentTask,
		db.TaskTypeGitHubResolveThread: HandleGitHubResolveThreadTask,
	}
}
