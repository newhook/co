package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/newhook/co/internal/github"
)

// PRFeedback represents a piece of feedback from a PR.
type PRFeedback struct {
	ID           string
	WorkID       string
	PRURL        string
	FeedbackType string
	Title        string
	Description  string
	Source       string
	SourceURL    string
	SourceID     *string    // GitHub comment/check ID for resolution tracking
	Priority     int
	BeadID       *string
	Metadata     map[string]string
	CreatedAt    time.Time
	ProcessedAt  *time.Time
	ResolvedAt   *time.Time // When the GitHub comment was resolved
}

// CreatePRFeedback creates a new PR feedback record.
func (db *DB) CreatePRFeedback(ctx context.Context, workID, prURL string, item github.FeedbackItem) (*PRFeedback, error) {
	id := uuid.New().String()

	// Convert metadata to JSON
	metadataJSON, err := json.Marshal(item.Context)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Get source_id from metadata if present
	var sourceID *string
	if sid, ok := item.Context["source_id"]; ok && sid != "" {
		sourceID = &sid
	}

	query := `
		INSERT INTO pr_feedback (
			id, work_id, pr_url, feedback_type, title, description,
			source, source_url, source_id, priority, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = db.DB.ExecContext(ctx, query,
		id, workID, prURL, string(item.Type), item.Title, item.Description,
		item.Source, item.SourceURL, sourceID, item.Priority, string(metadataJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR feedback: %w", err)
	}

	return &PRFeedback{
		ID:           id,
		WorkID:       workID,
		PRURL:        prURL,
		FeedbackType: string(item.Type),
		Title:        item.Title,
		Description:  item.Description,
		Source:       item.Source,
		SourceURL:    item.SourceURL,
		Priority:     item.Priority,
		Metadata:     item.Context,
		CreatedAt:    time.Now(),
	}, nil
}

// GetUnprocessedFeedback returns all unprocessed feedback for a work.
func (db *DB) GetUnprocessedFeedback(ctx context.Context, workID string) ([]PRFeedback, error) {
	query := `
		SELECT id, work_id, pr_url, feedback_type, title, description,
		       source, source_url, priority, bead_id, metadata, created_at, processed_at
		FROM pr_feedback
		WHERE work_id = ? AND processed_at IS NULL
		ORDER BY priority ASC, created_at ASC
	`

	rows, err := db.DB.QueryContext(ctx, query, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to query unprocessed feedback: %w", err)
	}
	defer rows.Close()

	var feedbacks []PRFeedback
	for rows.Next() {
		var f PRFeedback
		var beadID sql.NullString
		var processedAt sql.NullTime
		var metadataJSON string

		err := rows.Scan(&f.ID, &f.WorkID, &f.PRURL, &f.FeedbackType,
			&f.Title, &f.Description, &f.Source, &f.SourceURL,
			&f.Priority, &beadID, &metadataJSON, &f.CreatedAt, &processedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan feedback row: %w", err)
		}

		if beadID.Valid {
			f.BeadID = &beadID.String
		}
		if processedAt.Valid {
			f.ProcessedAt = &processedAt.Time
		}

		// Parse metadata JSON
		if metadataJSON != "" && metadataJSON != "{}" {
			if err := json.Unmarshal([]byte(metadataJSON), &f.Metadata); err != nil {
				// Log but don't fail on metadata parse errors
				f.Metadata = make(map[string]string)
			}
		} else {
			f.Metadata = make(map[string]string)
		}

		feedbacks = append(feedbacks, f)
	}

	return feedbacks, rows.Err()
}

// MarkFeedbackProcessed marks feedback as processed and associates it with a bead.
func (db *DB) MarkFeedbackProcessed(ctx context.Context, feedbackID, beadID string) error {
	query := `
		UPDATE pr_feedback
		SET bead_id = ?, processed_at = ?
		WHERE id = ?
	`

	result, err := db.DB.ExecContext(ctx, query, beadID, time.Now(), feedbackID)
	if err != nil {
		return fmt.Errorf("failed to mark feedback as processed: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("feedback %s not found", feedbackID)
	}

	return nil
}

// CountUnresolvedFeedbackForWork returns the count of unresolved PR feedback items for a work.
func (db *DB) CountUnresolvedFeedbackForWork(ctx context.Context, workID string) (int, error) {
	query := `SELECT COUNT(*) FROM pr_feedback WHERE work_id = ? AND bead_id IS NULL`
	var count int
	err := db.DB.QueryRowContext(ctx, query, workID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count unresolved feedback: %w", err)
	}
	return count, nil
}

// GetFeedbackByBeadID returns the feedback associated with a bead.
func (db *DB) GetFeedbackByBeadID(ctx context.Context, beadID string) (*PRFeedback, error) {
	query := `
		SELECT id, work_id, pr_url, feedback_type, title, description,
		       source, source_url, priority, bead_id, metadata, created_at, processed_at
		FROM pr_feedback
		WHERE bead_id = ?
	`

	var f PRFeedback
	var beadIDNull sql.NullString
	var processedAt sql.NullTime
	var metadataJSON string

	err := db.DB.QueryRowContext(ctx, query, beadID).Scan(
		&f.ID, &f.WorkID, &f.PRURL, &f.FeedbackType,
		&f.Title, &f.Description, &f.Source, &f.SourceURL,
		&f.Priority, &beadIDNull, &metadataJSON, &f.CreatedAt, &processedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query feedback by bead ID: %w", err)
	}

	if beadIDNull.Valid {
		f.BeadID = &beadIDNull.String
	}
	if processedAt.Valid {
		f.ProcessedAt = &processedAt.Time
	}

	// Parse metadata JSON
	if metadataJSON != "" && metadataJSON != "{}" {
		if err := json.Unmarshal([]byte(metadataJSON), &f.Metadata); err != nil {
			f.Metadata = make(map[string]string)
		}
	} else {
		f.Metadata = make(map[string]string)
	}

	return &f, nil
}

// HasExistingFeedback checks if feedback already exists for a specific source.
func (db *DB) HasExistingFeedback(ctx context.Context, workID, title, source string) (bool, error) {
	query := `
		SELECT COUNT(*) FROM pr_feedback
		WHERE work_id = ? AND title = ? AND source = ?
	`

	var count int
	err := db.DB.QueryRowContext(ctx, query, workID, title, source).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check existing feedback: %w", err)
	}

	return count > 0, nil
}

// GetUnresolvedFeedbackForClosedBeads returns feedback items where the associated bead is closed but not resolved on GitHub.
func (db *DB) GetUnresolvedFeedbackForClosedBeads(ctx context.Context, workID string) ([]PRFeedback, error) {
	query := `
		SELECT pf.id, pf.work_id, pf.pr_url, pf.feedback_type, pf.title, pf.description,
		       pf.source, pf.source_url, pf.source_id, pf.priority, pf.bead_id,
		       pf.metadata, pf.created_at, pf.processed_at, pf.resolved_at
		FROM pr_feedback pf
		WHERE pf.work_id = ?
		  AND pf.bead_id IS NOT NULL
		  AND pf.resolved_at IS NULL
		  AND pf.source_id IS NOT NULL
		ORDER BY pf.created_at ASC
	`

	rows, err := db.DB.QueryContext(ctx, query, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to query unresolved feedback: %w", err)
	}
	defer rows.Close()

	var feedbacks []PRFeedback
	for rows.Next() {
		var f PRFeedback
		var sourceID, beadID sql.NullString
		var processedAt, resolvedAt sql.NullTime
		var metadataJSON string

		err := rows.Scan(&f.ID, &f.WorkID, &f.PRURL, &f.FeedbackType,
			&f.Title, &f.Description, &f.Source, &f.SourceURL, &sourceID,
			&f.Priority, &beadID, &metadataJSON, &f.CreatedAt, &processedAt, &resolvedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan feedback row: %w", err)
		}

		if sourceID.Valid {
			f.SourceID = &sourceID.String
		}
		if beadID.Valid {
			f.BeadID = &beadID.String
		}
		if processedAt.Valid {
			f.ProcessedAt = &processedAt.Time
		}
		if resolvedAt.Valid {
			f.ResolvedAt = &resolvedAt.Time
		}

		// Parse metadata JSON
		if metadataJSON != "" && metadataJSON != "{}" {
			if err := json.Unmarshal([]byte(metadataJSON), &f.Metadata); err != nil {
				f.Metadata = make(map[string]string)
			}
		} else {
			f.Metadata = make(map[string]string)
		}

		feedbacks = append(feedbacks, f)
	}

	return feedbacks, rows.Err()
}

// MarkFeedbackResolved marks feedback as resolved on GitHub.
func (db *DB) MarkFeedbackResolved(ctx context.Context, feedbackID string) error {
	query := `
		UPDATE pr_feedback
		SET resolved_at = ?
		WHERE id = ?
	`

	result, err := db.DB.ExecContext(ctx, query, time.Now(), feedbackID)
	if err != nil {
		return fmt.Errorf("failed to mark feedback as resolved: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("feedback %s not found", feedbackID)
	}

	return nil
}