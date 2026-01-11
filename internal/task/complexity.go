package task

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
	workID      string  // Work context for estimation tasks
}

// NewLLMEstimator creates a new LLM-based complexity estimator.
func NewLLMEstimator(database *db.DB, workDir, projectName, workID string) *LLMEstimator {
	return &LLMEstimator{
		database:    database,
		workDir:     workDir,
		projectName: projectName,
		workID:      workID,
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

	// For single estimates, run a batch of one (never force)
	ctx := context.Background()
	if err := e.EstimateBatch(ctx, []beads.Bead{bead}, false); err != nil {
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
func (e *LLMEstimator) EstimateBatch(ctx context.Context, beadList []beads.Bead, forceEstimate bool) error {
	if len(beadList) == 0 {
		return nil
	}

	// Filter out already cached beads (unless forcing re-estimation)
	var uncachedBeads []beads.Bead
	var uncachedIDs []string

	if forceEstimate {
		// Force re-estimation of all beads
		fmt.Println("Force re-estimation enabled, ignoring cached estimates")
		uncachedBeads = beadList
		for _, bead := range beadList {
			uncachedIDs = append(uncachedIDs, bead.ID)
		}
	} else {
		// Normal flow: filter out cached beads
		for _, bead := range beadList {
			fullDescription := bead.Title + "\n" + bead.Description
			descHash := db.HashDescription(fullDescription)
			_, _, found, _ := e.database.GetCachedComplexity(bead.ID, descHash)
			if !found {
				uncachedBeads = append(uncachedBeads, bead)
				uncachedIDs = append(uncachedIDs, bead.ID)
			}
		}
	}

	if len(uncachedBeads) == 0 {
		// All beads already cached
		fmt.Printf("All %d bead(s) already have cached complexity estimates\n", len(beadList))
		return nil
	}

	// Create estimate task under the work
	if e.workID == "" {
		return fmt.Errorf("workID is required for creating estimate tasks")
	}
	taskID := fmt.Sprintf("%s.estimate-%d", e.workID, time.Now().Unix())
	if err := e.database.CreateTask(ctx, taskID, "estimate", uncachedIDs, 0, e.workID); err != nil {
		return fmt.Errorf("failed to create estimate task: %w", err)
	}
	fmt.Printf("Created estimate task %s for %d bead(s): %s\n",
		taskID, len(uncachedIDs), strings.Join(uncachedIDs, ", "))

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

	// Start task in database
	sessionName := claude.SessionNameForProject(e.projectName)
	// The tab name will be derived from the hierarchical task ID in claude.Run
	if err := e.database.StartTask(ctx, taskID, sessionName, ""); err != nil {
		return fmt.Errorf("failed to start estimation task in database: %w", err)
	}

	// Build prompt
	prompt := claude.BuildEstimatePrompt(taskID, uncachedBeads)

	// Run Claude to perform estimation in the worktree
	fmt.Println("Running estimation task...")
	// Never auto-close estimation task tabs - they're system tasks
	result, err := claude.Run(ctx, e.database, taskID, uncachedBeads, prompt, worktreePath, e.projectName, false)
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

	// Estimation succeeded
	fmt.Println("Estimation completed successfully")

	// Remove worktree only if we created one
	if shouldRemoveWorktree {
		fmt.Printf("Removing estimation worktree...\n")
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
