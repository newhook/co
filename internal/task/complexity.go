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
	workID      string // Work context for estimation tasks
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
// Returns (0, 0, nil) if the bead needs estimation but an estimation task was spawned.
func (e *LLMEstimator) Estimate(ctx context.Context, bead beads.Bead) (score int, tokens int, err error) {
	// Calculate description hash for caching
	fullDescription := bead.Title + "\n" + bead.Description
	descHash := db.HashDescription(fullDescription)

	// Check cache first
	if e.database != nil {
		score, tokens, found, err := e.database.GetCachedComplexity(ctx, bead.ID, descHash)
		if err == nil && found {
			return score, tokens, nil
		}
	}

	// For single estimates, run a batch of one (never force)
	result, err := e.EstimateBatch(ctx, []beads.Bead{bead}, false)
	if err != nil {
		return 0, 0, err
	}

	// If a task was spawned, return zeros (estimation in progress)
	if result.TaskSpawned {
		return 0, 0, nil
	}

	// Retrieve the cached result
	score, tokens, found, err := e.database.GetCachedComplexity(ctx, bead.ID, descHash)
	if err != nil || !found {
		return 0, 0, fmt.Errorf("failed to retrieve estimate after batch: %w", err)
	}

	return score, tokens, nil
}

// EstimationResult contains the result of an estimation attempt.
type EstimationResult struct {
	AllCached    bool     // True if all beads already had cached estimates
	TaskSpawned  bool     // True if an estimation task was spawned
	TaskID       string   // The estimation task ID if spawned
	UncachedIDs  []string // IDs of beads that need estimation
}

// EstimateBatch spawns an estimation task for beads without cached complexity.
// This function is non-blocking - it spawns the task and returns immediately.
// Returns EstimationResult indicating whether all beads are cached or if a task was spawned.
func (e *LLMEstimator) EstimateBatch(ctx context.Context, beadList []beads.Bead, forceEstimate bool) (*EstimationResult, error) {
	result := &EstimationResult{}

	if len(beadList) == 0 {
		result.AllCached = true
		return result, nil
	}

	// Filter out already cached beads (unless forcing re-estimation)
	var uncachedBeads []beads.Bead

	if forceEstimate {
		// Force re-estimation of all beads
		fmt.Println("Force re-estimation enabled, ignoring cached estimates")
		uncachedBeads = beadList
		for _, bead := range beadList {
			result.UncachedIDs = append(result.UncachedIDs, bead.ID)
		}
	} else {
		// Normal flow: filter out cached beads
		for _, bead := range beadList {
			fullDescription := bead.Title + "\n" + bead.Description
			descHash := db.HashDescription(fullDescription)
			_, _, found, _ := e.database.GetCachedComplexity(ctx, bead.ID, descHash)
			if !found {
				uncachedBeads = append(uncachedBeads, bead)
				result.UncachedIDs = append(result.UncachedIDs, bead.ID)
			}
		}
	}

	if len(uncachedBeads) == 0 {
		// All beads already cached
		fmt.Printf("All %d bead(s) already have cached complexity estimates\n", len(beadList))
		result.AllCached = true
		return result, nil
	}

	// Create estimate task under the work
	if e.workID == "" {
		return nil, fmt.Errorf("workID is required for creating estimate tasks")
	}
	taskID := fmt.Sprintf("%s.estimate-%d", e.workID, time.Now().Unix())
	if err := e.database.CreateTask(ctx, taskID, "estimate", result.UncachedIDs, 0, e.workID); err != nil {
		return nil, fmt.Errorf("failed to create estimate task: %w", err)
	}
	fmt.Printf("Created estimate task %s for %d bead(s): %s\n",
		taskID, len(result.UncachedIDs), strings.Join(result.UncachedIDs, ", "))

	result.TaskID = taskID

	// Check if we're already in a worktree (work context)
	worktreePath := e.workDir

	// Only create a new worktree if we're not already in one (e.g., not in a work's worktree)
	if !worktree.ExistsPath(e.workDir) {
		// Create worktree for estimation task
		branchName := fmt.Sprintf("task/%s", taskID)
		worktreePath = filepath.Join(filepath.Dir(e.workDir), taskID)

		fmt.Printf("Creating worktree for estimation task at %s...\n", worktreePath)
		if err := worktree.Create(e.workDir, worktreePath, branchName); err != nil {
			return nil, fmt.Errorf("failed to create worktree for estimation: %w", err)
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
	if err := e.database.StartTask(ctx, taskID, sessionName, ""); err != nil {
		return nil, fmt.Errorf("failed to start estimation task in database: %w", err)
	}

	// Build prompt
	prompt := claude.BuildEstimatePrompt(taskID, uncachedBeads)

	// Spawn Claude to perform estimation in the worktree (non-blocking)
	fmt.Println("Spawning estimation task...")
	// Never auto-close estimation task tabs - they're system tasks
	// Note: hooks.env is applied by co claude itself
	_, err := claude.Run(ctx, taskID, uncachedBeads, prompt, worktreePath, e.projectName, false)
	if err != nil {
		fmt.Printf("Failed to spawn estimation: %v\n", err)
		return nil, fmt.Errorf("failed to spawn estimation: %w", err)
	}

	result.TaskSpawned = true
	fmt.Printf("Estimation task %s spawned successfully\n", taskID)
	fmt.Println("Estimation is running in background. Re-run 'co plan --auto-group' after it completes.")

	return result, nil
}
