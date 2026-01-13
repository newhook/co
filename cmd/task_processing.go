package cmd

import (
	"fmt"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/worktree"
)

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
	mainRepoPath := proj.MainRepoPath()
	var prompt string
	baseBranch := work.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	switch dbTask.TaskType {
	case "estimate":
		// Build estimation prompt
		beadIDs, err := proj.DB.GetTaskBeads(ctx, taskID)
		if err != nil {
			return fmt.Errorf("failed to get task beads: %w", err)
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
		prompt = claude.BuildEstimatePrompt(taskID, beadList)

	case "implement":
		// Build implementation prompt
		beadIDs, err := proj.DB.GetTaskBeads(ctx, taskID)
		if err != nil {
			return fmt.Errorf("failed to get task beads: %w", err)
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
		prompt = claude.BuildTaskPrompt(taskID, beadList, work.BranchName, baseBranch)

	case "review":
		prompt = claude.BuildReviewPrompt(taskID, work.ID, work.BranchName, baseBranch)

	case "pr":
		prompt = claude.BuildPRPrompt(taskID, work.ID, work.BranchName, baseBranch)

	case "update-pr-description":
		if work.PRURL == "" {
			return fmt.Errorf("work %s has no PR URL set", work.ID)
		}
		prompt = claude.BuildUpdatePRDescriptionPrompt(taskID, work.ID, work.PRURL, work.BranchName, baseBranch)

	default:
		return fmt.Errorf("unknown task type: %s", dbTask.TaskType)
	}

	// Execute Claude inline (blocking)
	if err := claude.RunInline(ctx, proj.DB, taskID, prompt, work.WorktreePath); err != nil {
		return fmt.Errorf("task %s failed: %w", taskID, err)
	}

	fmt.Printf("\n=== Task %s completed ===\n", taskID)
	return nil
}
