package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/newhook/co/internal/beads/queries"
	"github.com/newhook/co/internal/db"
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

// Estimate returns a complexity score (1-10) and estimated context tokens for an issue.
// Results are cached based on the description hash.
// Returns (0, 0, nil) if the issue needs estimation but an estimation task was spawned.
func (e *LLMEstimator) Estimate(ctx context.Context, issue queries.Issue) (score int, tokens int, err error) {
	// Calculate description hash for caching
	fullDescription := issue.Title + "\n" + issue.Description
	descHash := db.HashDescription(fullDescription)

	// Check cache first
	if e.database != nil {
		score, tokens, found, err := e.database.GetCachedComplexity(ctx, issue.ID, descHash)
		if err == nil && found {
			return score, tokens, nil
		}
	}

	// For single estimates, run a batch of one (never force)
	result, err := e.EstimateBatch(ctx, []queries.Issue{issue}, false)
	if err != nil {
		return 0, 0, err
	}

	// If a task was spawned, return zeros (estimation in progress)
	if result.TaskSpawned {
		return 0, 0, nil
	}

	// Retrieve the cached result
	score, tokens, found, err := e.database.GetCachedComplexity(ctx, issue.ID, descHash)
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

// EstimateBatch spawns an estimation task for issues without cached complexity.
// This function is non-blocking - it spawns the task and returns immediately.
// Returns EstimationResult indicating whether all issues are cached or if a task was spawned.
func (e *LLMEstimator) EstimateBatch(ctx context.Context, issues []queries.Issue, forceEstimate bool) (*EstimationResult, error) {
	result := &EstimationResult{}

	if len(issues) == 0 {
		result.AllCached = true
		return result, nil
	}

	// Filter out already cached issues (unless forcing re-estimation)
	var uncachedIssues []queries.Issue

	if forceEstimate {
		// Force re-estimation of all issues
		fmt.Println("Force re-estimation enabled, ignoring cached estimates")
		uncachedIssues = issues
		for _, issue := range issues {
			result.UncachedIDs = append(result.UncachedIDs, issue.ID)
		}
	} else {
		// Normal flow: filter out cached issues
		for _, issue := range issues {
			fullDescription := issue.Title + "\n" + issue.Description
			descHash := db.HashDescription(fullDescription)
			_, _, found, _ := e.database.GetCachedComplexity(ctx, issue.ID, descHash)
			if !found {
				uncachedIssues = append(uncachedIssues, issue)
				result.UncachedIDs = append(result.UncachedIDs, issue.ID)
			}
		}
	}

	if len(uncachedIssues) == 0 {
		// All issues already cached
		fmt.Printf("All %d issue(s) already have cached complexity estimates\n", len(issues))
		result.AllCached = true
		return result, nil
	}

	// Cannot estimate here - estimation must happen through tasks run by orchestrator
	return nil, fmt.Errorf("missing complexity estimates for %d issue(s): %s. Use 'co run --auto' to run estimation through the orchestrator",
		len(result.UncachedIDs), strings.Join(result.UncachedIDs, ", "))
}
