package task

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/claude"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/worktree"
)

// LLMEstimator uses Claude Code via estimate tasks to estimate bead complexity.
type LLMEstimator struct {
	database    *db.DB
	workDir     string
	projectName string
}

// NewLLMEstimator creates a new LLM-based complexity estimator.
func NewLLMEstimator(database *db.DB, workDir, projectName string) *LLMEstimator {
	return &LLMEstimator{
		database:    database,
		workDir:     workDir,
		projectName: projectName,
	}
}

// Estimate returns a complexity score (1-10) and estimated context tokens for a bead.
// Results are cached based on the description hash.
func (e *LLMEstimator) Estimate(bead beads.Bead) (score int, tokens int, err error) {
	// Calculate description hash for caching
	fullDescription := bead.Title + "\n" + bead.Description
	descHash := db.HashDescription(fullDescription)

	// Check cache first
	if e.database != nil {
		score, tokens, found, err := e.database.GetCachedComplexity(bead.ID, descHash)
		if err == nil && found {
			return score, tokens, nil
		}
	}

	// For single estimates, run a batch of one
	ctx := context.Background()
	if err := e.EstimateBatch(ctx, []beads.Bead{bead}); err != nil {
		return 0, 0, err
	}

	// Retrieve the cached result
	score, tokens, found, err := e.database.GetCachedComplexity(bead.ID, descHash)
	if err != nil || !found {
		return 0, 0, fmt.Errorf("failed to retrieve estimate after batch: %w", err)
	}

	return score, tokens, nil
}

// EstimateBatch estimates complexity for multiple beads using an estimate task.
func (e *LLMEstimator) EstimateBatch(ctx context.Context, beadList []beads.Bead) error {
	if len(beadList) == 0 {
		return nil
	}

	// Filter out already cached beads
	var uncachedBeads []beads.Bead
	var uncachedIDs []string
	for _, bead := range beadList {
		fullDescription := bead.Title + "\n" + bead.Description
		descHash := db.HashDescription(fullDescription)
		_, _, found, _ := e.database.GetCachedComplexity(bead.ID, descHash)
		if !found {
			uncachedBeads = append(uncachedBeads, bead)
			uncachedIDs = append(uncachedIDs, bead.ID)
		}
	}

	if len(uncachedBeads) == 0 {
		// All beads already cached
		return nil
	}

	// Create estimate task (no work context for estimation)
	taskID := fmt.Sprintf("estimate-%d", time.Now().Unix())
	if err := e.database.CreateTask(taskID, "estimate", uncachedIDs, 0, ""); err != nil {
		return fmt.Errorf("failed to create estimate task: %w", err)
	}

	// Check if we're already in a worktree (work context)
	worktreePath := e.workDir
	var shouldRemoveWorktree bool

	// Only create a new worktree if we're not already in one (e.g., not in a work's worktree)
	if !worktree.ExistsPath(e.workDir) {
		// Create worktree for estimation task
		branchName := fmt.Sprintf("task/%s", taskID)
		worktreePath = filepath.Join(filepath.Dir(e.workDir), taskID)
		shouldRemoveWorktree = true

		fmt.Printf("Creating worktree for estimation task at %s...\n", worktreePath)
		if err := worktree.Create(e.workDir, worktreePath, branchName); err != nil {
			return fmt.Errorf("failed to create worktree for estimation: %w", err)
		}

		// Initialize mise in worktree (optional - warn on error)
		if err := mise.Initialize(worktreePath); err != nil {
			fmt.Printf("Warning: mise initialization in worktree failed: %v\n", err)
		}
	} else {
		fmt.Printf("Using existing worktree at %s for estimation...\n", worktreePath)
	}

	// Start task in database (worktree is now managed at work level)
	sessionName := claude.SessionNameForProject(e.projectName)
	if err := e.database.StartTask(taskID, sessionName, taskID); err != nil {
		return fmt.Errorf("failed to start estimation task in database: %w", err)
	}

	// Build prompt
	prompt := claude.BuildEstimatePrompt(taskID, uncachedBeads)

	// Run Claude to perform estimation in the worktree
	result, err := claude.Run(ctx, e.database, taskID, uncachedBeads, prompt, worktreePath, e.projectName)
	if err != nil {
		fmt.Printf("Estimation failed: %v\n", err)
		if shouldRemoveWorktree {
			fmt.Printf("Worktree kept for debugging at: %s\n", worktreePath)
		}
		return fmt.Errorf("failed to run estimation: %w", err)
	}

	if !result.Completed {
		if shouldRemoveWorktree {
			fmt.Printf("Worktree kept for debugging at: %s\n", worktreePath)
		}
		return fmt.Errorf("estimation task did not complete successfully")
	}

	// Estimation succeeded - remove worktree only if we created one
	if shouldRemoveWorktree {
		fmt.Printf("Estimation completed successfully, removing worktree...\n")
		if err := worktree.Remove(e.workDir, worktreePath); err != nil {
			fmt.Printf("Warning: failed to remove estimation worktree: %v\n", err)
		}
	}

	// Verify all beads were estimated
	allEstimated, err := e.database.AreAllBeadsEstimated(uncachedIDs)
	if err != nil {
		return fmt.Errorf("failed to verify estimates: %w", err)
	}
	if !allEstimated {
		return fmt.Errorf("not all beads were estimated")
	}

	return nil
}
