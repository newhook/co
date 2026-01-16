package beads

import (
	"time"

	"github.com/newhook/co/internal/beads/queries"
)

// Bead is a clean wrapper around queries.Issue without SQL null types.
type Bead struct {
	ID                 string
	Title              string
	Description        string
	Design             string
	AcceptanceCriteria string
	Notes              string
	Status             string
	Priority           int
	Type               string // issue_type in the database
	Assignee           string
	EstimatedMinutes   int
	CreatedAt          time.Time
	CreatedBy          string
	Owner              string
	UpdatedAt          time.Time
	ClosedAt           time.Time
	CloseReason        string
	ExternalRef        string
	IsEpic             bool // derived from issue_type == "epic"
}

// BeadFromIssue converts a queries.Issue to a clean Bead.
func BeadFromIssue(issue queries.Issue) Bead {
	b := Bead{
		ID:                 issue.ID,
		Title:              issue.Title,
		Description:        issue.Description,
		Design:             issue.Design,
		AcceptanceCriteria: issue.AcceptanceCriteria,
		Notes:              issue.Notes,
		Status:             issue.Status,
		Priority:           int(issue.Priority),
		Type:               issue.IssueType,
		CreatedAt:          issue.CreatedAt,
		UpdatedAt:          issue.UpdatedAt,
		IsEpic:             issue.IssueType == "epic",
	}

	if issue.Assignee.Valid {
		b.Assignee = issue.Assignee.String
	}
	if issue.EstimatedMinutes.Valid {
		b.EstimatedMinutes = int(issue.EstimatedMinutes.Int64)
	}
	if issue.CreatedBy.Valid {
		b.CreatedBy = issue.CreatedBy.String
	}
	if issue.Owner.Valid {
		b.Owner = issue.Owner.String
	}
	if issue.ClosedAt.Valid {
		b.ClosedAt = issue.ClosedAt.Time
	}
	if issue.CloseReason.Valid {
		b.CloseReason = issue.CloseReason.String
	}
	if issue.ExternalRef.Valid {
		b.ExternalRef = issue.ExternalRef.String
	}

	return b
}

// BeadsFromIssues converts a slice of queries.Issue to a slice of Bead.
func BeadsFromIssues(issues []queries.Issue) []Bead {
	beads := make([]Bead, len(issues))
	for i, issue := range issues {
		beads[i] = BeadFromIssue(issue)
	}
	return beads
}

// BeadMap converts a map of issue ID to queries.Issue to a map of ID to Bead.
func BeadMap(issues map[string]queries.Issue) map[string]Bead {
	beads := make(map[string]Bead, len(issues))
	for id, issue := range issues {
		beads[id] = BeadFromIssue(issue)
	}
	return beads
}
