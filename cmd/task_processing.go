package cmd

import (
	"context"
	"fmt"

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
		beadList, err := getBeadsForTask(ctx, proj, task.ID, mainRepoPath)
		if err != nil {
			return "", err
		}
		return claude.BuildEstimatePrompt(task.ID, beadList), nil

	case "implement":
		beadList, err := getBeadsForTask(ctx, proj, task.ID, mainRepoPath)
		if err != nil {
			return "", err
		}
		return claude.BuildTaskPrompt(task.ID, beadList, work.BranchName, baseBranch), nil

	case "review":
		return claude.BuildReviewPrompt(task.ID, work.ID, work.BranchName, baseBranch), nil

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

	var beadList []beads.Bead
	for _, beadID := range beadIDs {
		bead, err := beads.GetBeadInDir(beadID, mainRepoPath)
		if err != nil {
			fmt.Printf("Warning: failed to get bead %s: %v\n", beadID, err)
			continue
		}
		beadList = append(beadList, *bead)
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
