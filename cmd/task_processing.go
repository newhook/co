package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/worktree"
)

// buildPromptForTask builds the appropriate prompt for a task based on its type.
// This centralizes prompt building logic for different task types.
func buildPromptForTask(ctx context.Context, proj *project.Project, task *db.Task, work *db.Work) (string, error) {
	mainRepoPath := proj.MainRepoPath()
	baseBranch := work.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	switch task.TaskType {
	case "estimate":
		issues, err := getBeadsForTask(ctx, proj, task.ID, mainRepoPath)
		if err != nil {
			return "", err
		}
		return claude.BuildEstimatePrompt(task.ID, issues), nil

	case "implement":
		issues, err := getBeadsForTask(ctx, proj, task.ID, mainRepoPath)
		if err != nil {
			return "", err
		}
		return claude.BuildTaskPrompt(task.ID, issues, work.BranchName, baseBranch), nil

	case "review":
		return claude.BuildReviewPrompt(task.ID, work.ID, work.BranchName, baseBranch, work.RootIssueID), nil

	case "pr":
		return claude.BuildPRPrompt(task.ID, work.ID, work.BranchName, baseBranch), nil

	case "update-pr-description":
		if work.PRURL == "" {
			return "", fmt.Errorf("work %s has no PR URL set", work.ID)
		}
		return claude.BuildUpdatePRDescriptionPrompt(task.ID, work.ID, work.PRURL, work.BranchName, baseBranch), nil

	default:
		return "", fmt.Errorf("unknown task type: %s", task.TaskType)
	}
}

// getBeadsForTask retrieves the beads associated with a task.
func getBeadsForTask(ctx context.Context, proj *project.Project, taskID, mainRepoPath string) ([]beads.Bead, error) {
	beadIDs, err := proj.DB.GetTaskBeads(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task beads: %w", err)
	}

	// Create beads client
	beadsDBPath := filepath.Join(mainRepoPath, ".beads", "beads.db")
	beadsClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client: %w", err)
	}
	defer beadsClient.Close()

	// Get beads with dependencies
	result, err := beadsClient.GetBeadsWithDeps(ctx, beadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get beads: %w", err)
	}

	// Convert map to slice in order of beadIDs
	var beadList []beads.Bead
	for _, beadID := range beadIDs {
		if b, ok := result.Beads[beadID]; ok {
			beadList = append(beadList, b)
		} else {
			fmt.Printf("Warning: bead %s not found\n", beadID)
		}
	}

	return beadList, nil
}

// processTask processes a single task by ID using inline execution.
// This blocks until the task is complete.
func processTask(proj *project.Project, taskID string) error {
	ctx := GetContext()

	// Get the task
	dbTask, err := proj.DB.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}
	if dbTask == nil {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Check task status
	if dbTask.Status == db.StatusCompleted {
		fmt.Printf("Task %s is already completed\n", taskID)
		return nil
	}

	// Get the associated work
	if dbTask.WorkID == "" {
		return fmt.Errorf("task %s has no associated work", taskID)
	}

	work, err := proj.DB.GetWork(ctx, dbTask.WorkID)
	if err != nil {
		return fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return fmt.Errorf("work %s not found for task %s", dbTask.WorkID, taskID)
	}

	// Mark work as started/processing if it's still pending
	if work.Status == db.StatusPending {
		if err := proj.DB.StartWork(ctx, work.ID, "", ""); err != nil {
			fmt.Printf("Warning: failed to update work status: %v\n", err)
		}
	}

	fmt.Printf("\n=== Processing task %s ===\n", taskID)
	fmt.Printf("Work: %s\n", work.ID)
	fmt.Printf("Branch: %s\n", work.BranchName)
	fmt.Printf("Worktree: %s\n", work.WorktreePath)

	// Check if worktree exists
	if work.WorktreePath == "" {
		return fmt.Errorf("work %s has no worktree path configured", work.ID)
	}

	if !worktree.ExistsPath(work.WorktreePath) {
		return fmt.Errorf("work %s worktree does not exist at %s", work.ID, work.WorktreePath)
	}

	// Build prompt for Claude based on task type
	prompt, err := buildPromptForTask(ctx, proj, dbTask, work)
	if err != nil {
		return err
	}

	// Execute Claude inline (blocking)
	if err := claude.Run(ctx, proj.DB, taskID, prompt, work.WorktreePath); err != nil {
		return fmt.Errorf("task %s failed: %w", taskID, err)
	}

	fmt.Printf("\n=== Task %s completed ===\n", taskID)
	return nil
}
