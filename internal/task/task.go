package task

import (
	"context"

	"github.com/newhook/co/internal/beads/queries"
)

// Status constants for task tracking.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

// Task represents a virtual task - a group of beads to be processed together.
type Task struct {
	ID         string          // Unique task identifier
	BeadIDs    []string        // IDs of beads in this task
	Beads      []queries.Issue // Full bead information
	Complexity int             // Sum of bead complexity scores
	Status     string          // pending, processing, completed, failed
}

// Planner creates task groupings from a list of beads.
type Planner interface {
	// Plan analyzes beads and creates task assignments based on complexity budget.
	// The budget represents the target complexity per task (e.g., 70% of context window).
	// Returns a list of tasks with beads grouped to respect dependencies and fit within budget.
	Plan(
		issues []queries.Issue,
		dependencies map[string][]queries.GetDependenciesForIssuesRow,
		budget int,
	) ([]Task, error)
}

// ComplexityEstimator estimates the complexity of a bead.
type ComplexityEstimator interface {
	// Estimate returns a complexity score (1-10) and estimated context tokens for a bead.
	Estimate(ctx context.Context, issue queries.Issue) (score int, tokens int, err error)
}

// BeadComplexity holds complexity information for a single bead.
type BeadComplexity struct {
	BeadID          string
	ComplexityScore int // 1-10 scale
	EstimatedTokens int
}
