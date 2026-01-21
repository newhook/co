package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/newhook/co/internal/db/sqlc"
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
func (db *DB) CreatePRFeedback(ctx context.Context, workID, prURL, feedbackType, title, description, source, sourceURL string, priority int, metadata map[string]string) (*PRFeedback, error) {
	id := uuid.New().String()

	// Convert metadata to JSON
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Get source_id from metadata if present
	var sourceID *string
	if sid, ok := metadata["source_id"]; ok && sid != "" {
		sourceID = &sid
	}

	query := `
		INSERT INTO pr_feedback (
			id, work_id, pr_url, feedback_type, title, description,
			source, source_url, source_id, priority, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = db.DB.ExecContext(ctx, query,
		id, workID, prURL, feedbackType, title, description,
		source, sourceURL, sourceID, priority, string(metadataJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR feedback: %w", err)
	}

	return &PRFeedback{
		ID:           id,
		WorkID:       workID,
		PRURL:        prURL,
		FeedbackType: feedbackType,
		Title:        title,
		Description:  description,
		Source:       source,
		SourceURL:    sourceURL,
		SourceID:     sourceID,
		Priority:     priority,
		Metadata:     metadata,
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
// If sourceID is provided, it uses that as the unique identifier (e.g., GitHub comment ID).
// Otherwise falls back to checking by title and source.
func (db *DB) HasExistingFeedback(ctx context.Context, workID, title, source string) (bool, error) {
	// This method signature is kept for backward compatibility,
	// but we should check if the source_id already exists in the database
	// by looking at the metadata or source_id column
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

// HasExistingFeedbackBySourceID checks if feedback already exists for a specific source ID.
// This is the preferred method for checking duplicates when a unique source ID is available.
func (db *DB) HasExistingFeedbackBySourceID(ctx context.Context, workID, sourceID string) (bool, error) {
	query := `
		SELECT COUNT(*) FROM pr_feedback
		WHERE work_id = ? AND source_id = ?
	`

	var count int
	err := db.DB.QueryRowContext(ctx, query, workID, sourceID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check existing feedback by source ID: %w", err)
	}

	return count > 0, nil
}

// GetFeedbackBySourceID returns the feedback associated with a source ID (e.g., GitHub comment ID).
// Returns nil if no feedback exists for this source ID.
func (db *DB) GetFeedbackBySourceID(ctx context.Context, workID, sourceID string) (*PRFeedback, error) {
	f, err := db.queries.GetPRFeedbackBySourceID(ctx, sqlc.GetPRFeedbackBySourceIDParams{
		WorkID:   workID,
		SourceID: sql.NullString{String: sourceID, Valid: true},
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query feedback by source ID: %w", err)
	}

	result := &PRFeedback{
		ID:           f.ID,
		WorkID:       f.WorkID,
		PRURL:        f.PrUrl,
		FeedbackType: f.FeedbackType,
		Title:        f.Title,
		Description:  f.Description,
		Source:       f.Source,
		SourceURL:    f.SourceUrl.String,
		Priority:     int(f.Priority),
		CreatedAt:    f.CreatedAt,
	}

	if f.SourceID.Valid {
		result.SourceID = &f.SourceID.String
	}
	if f.BeadID.Valid {
		result.BeadID = &f.BeadID.String
	}
	if f.ProcessedAt.Valid {
		result.ProcessedAt = &f.ProcessedAt.Time
	}

	// Parse metadata JSON
	if f.Metadata != "" && f.Metadata != "{}" {
		if err := json.Unmarshal([]byte(f.Metadata), &result.Metadata); err != nil {
			result.Metadata = make(map[string]string)
		}
	} else {
		result.Metadata = make(map[string]string)
	}

	return result, nil
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

// GetUnresolvedFeedbackForBeads returns feedback items for specific beads that are not yet resolved on GitHub.
func (db *DB) GetUnresolvedFeedbackForBeads(ctx context.Context, beadIDs []string) ([]PRFeedback, error) {
	if len(beadIDs) == 0 {
		return nil, nil
	}

	// Convert string slice to sql.NullString slice for sqlc
	nullBeadIDs := make([]sql.NullString, len(beadIDs))
	for i, beadID := range beadIDs {
		nullBeadIDs[i] = sql.NullString{String: beadID, Valid: true}
	}

	// Use sqlc generated query
	feedbacks, err := db.queries.GetUnresolvedFeedbackForBeads(ctx, nullBeadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query unresolved feedback for beads: %w", err)
	}

	// Convert sqlc PrFeedback to our PRFeedback struct
	result := make([]PRFeedback, len(feedbacks))
	for i, f := range feedbacks {
		result[i] = PRFeedback{
			ID:           f.ID,
			WorkID:       f.WorkID,
			PRURL:        f.PrUrl,
			FeedbackType: f.FeedbackType,
			Title:        f.Title,
			Description:  f.Description,
			Source:       f.Source,
			SourceURL:    f.SourceUrl.String,
			Priority:     int(f.Priority),
			CreatedAt:    f.CreatedAt,
		}

		if f.SourceID.Valid {
			result[i].SourceID = &f.SourceID.String
		}
		if f.BeadID.Valid {
			result[i].BeadID = &f.BeadID.String
		}
		if f.ProcessedAt.Valid {
			result[i].ProcessedAt = &f.ProcessedAt.Time
		}
		if f.ResolvedAt.Valid {
			result[i].ResolvedAt = &f.ResolvedAt.Time
		}

		// Parse metadata JSON
		if f.Metadata != "" && f.Metadata != "{}" {
			if err := json.Unmarshal([]byte(f.Metadata), &result[i].Metadata); err != nil {
				result[i].Metadata = make(map[string]string)
			}
		} else {
			result[i].Metadata = make(map[string]string)
		}
	}

	return result, nil
}

// MarkFeedbackResolved marks feedback as resolved on GitHub.
func (db *DB) MarkFeedbackResolved(ctx context.Context, feedbackID string) error {
	// Use sqlc generated method
	err := db.queries.MarkPRFeedbackResolved(ctx, feedbackID)
	if err != nil {
		return fmt.Errorf("failed to mark feedback as resolved: %w", err)
	}

	return nil
}

// ScheduledTaskParams contains the parameters for scheduling a task.
type ScheduledTaskParams struct {
	WorkID         string
	TaskType       string
	ScheduledAt    time.Time
	Metadata       map[string]string
	IdempotencyKey string
	MaxAttempts    int
}

// MarkFeedbackResolvedAndScheduleTasks atomically marks feedback as resolved
// and schedules the associated GitHub tasks in a single transaction.
// This implements the transactional outbox pattern correctly.
func (db *DB) MarkFeedbackResolvedAndScheduleTasks(ctx context.Context, feedbackID string, tasks []ScheduledTaskParams) error {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.queries.WithTx(tx)

	// Mark feedback as resolved
	if err := qtx.MarkPRFeedbackResolved(ctx, feedbackID); err != nil {
		return fmt.Errorf("failed to mark feedback as resolved: %w", err)
	}

	// Schedule all tasks
	for _, task := range tasks {
		// Check if task with this idempotency key already exists
		if task.IdempotencyKey != "" {
			existing, err := qtx.GetTaskByIdempotencyKey(ctx, sql.NullString{String: task.IdempotencyKey, Valid: true})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to check existing task: %w", err)
			}
			if existing.ID != "" {
				// Task already exists, skip
				continue
			}
		}

		id := uuid.New().String()

		metadataJSON := "{}"
		if task.Metadata != nil {
			data, err := json.Marshal(task.Metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata: %w", err)
			}
			metadataJSON = string(data)
		}

		maxAttempts := task.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = DefaultMaxAttempts
		}

		err := qtx.CreateScheduledTaskWithRetry(ctx, sqlc.CreateScheduledTaskWithRetryParams{
			ID:             id,
			WorkID:         task.WorkID,
			TaskType:       task.TaskType,
			ScheduledAt:    task.ScheduledAt,
			Status:         TaskStatusPending,
			Metadata:       metadataJSON,
			AttemptCount:   0,
			MaxAttempts:    int64(maxAttempts),
			IdempotencyKey: sql.NullString{String: task.IdempotencyKey, Valid: task.IdempotencyKey != ""},
		})
		if err != nil {
			return fmt.Errorf("failed to schedule task: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}