package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/newhook/co/internal/db/sqlc"
)

// RecoveryEventType defines the types of recovery events.
type RecoveryEventType string

const (
	// RecoveryEventTaskReset is logged when a stuck task is reset to pending.
	RecoveryEventTaskReset RecoveryEventType = "task_reset"
	// RecoveryEventTaskStaleFailed is logged when a stale task is auto-failed.
	RecoveryEventTaskStaleFailed RecoveryEventType = "task_stale_failed"
	// RecoveryEventBeadPreserved is logged when a bead's completed status is preserved during reset.
	RecoveryEventBeadPreserved RecoveryEventType = "bead_preserved"
	// RecoveryEventBeadReset is logged when a bead is reset to pending during task recovery.
	RecoveryEventBeadReset RecoveryEventType = "bead_reset"
)

// RecoveryEvent represents an audit log entry for recovery operations.
type RecoveryEvent struct {
	ID        int64
	EventType RecoveryEventType
	TaskID    string
	WorkID    string
	BeadID    string // Empty for task-level events
	Reason    string
	Details   map[string]interface{}
	CreatedAt time.Time
}

// LogRecoveryEvent records a recovery event to the audit table.
func (db *DB) LogRecoveryEvent(ctx context.Context, eventType RecoveryEventType, taskID, workID, beadID, reason string, details map[string]interface{}) error {
	var detailsJSON sql.NullString
	if details != nil {
		b, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("failed to marshal details: %w", err)
		}
		detailsJSON = sql.NullString{String: string(b), Valid: true}
	}

	var beadIDNull sql.NullString
	if beadID != "" {
		beadIDNull = sql.NullString{String: beadID, Valid: true}
	}

	return db.queries.InsertRecoveryEvent(ctx, sqlc.InsertRecoveryEventParams{
		EventType: string(eventType),
		TaskID:    taskID,
		WorkID:    workID,
		BeadID:    beadIDNull,
		Reason:    reason,
		Details:   detailsJSON,
	})
}

// GetRecoveryEventsForTask returns all recovery events for a specific task.
func (db *DB) GetRecoveryEventsForTask(ctx context.Context, taskID string) ([]RecoveryEvent, error) {
	rows, err := db.queries.GetRecoveryEventsForTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recovery events for task: %w", err)
	}

	events := make([]RecoveryEvent, len(rows))
	for i, row := range rows {
		events[i] = recoveryEventFromSQLCRow(row)
	}
	return events, nil
}

// GetRecoveryEventsForWork returns all recovery events for a specific work.
func (db *DB) GetRecoveryEventsForWork(ctx context.Context, workID string) ([]RecoveryEvent, error) {
	rows, err := db.queries.GetRecoveryEventsForWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recovery events for work: %w", err)
	}

	events := make([]RecoveryEvent, len(rows))
	for i, row := range rows {
		events[i] = recoveryEventFromSQLCRow(row)
	}
	return events, nil
}

// GetRecentRecoveryEvents returns the most recent recovery events.
func (db *DB) GetRecentRecoveryEvents(ctx context.Context, limit int) ([]RecoveryEvent, error) {
	rows, err := db.queries.GetRecentRecoveryEvents(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to get recent recovery events: %w", err)
	}

	events := make([]RecoveryEvent, len(rows))
	for i, row := range rows {
		events[i] = recoveryEventFromSQLCRow(row)
	}
	return events, nil
}

func recoveryEventFromSQLCRow(row sqlc.RecoveryEvent) RecoveryEvent {
	event := RecoveryEvent{
		ID:        row.ID,
		EventType: RecoveryEventType(row.EventType),
		TaskID:    row.TaskID,
		WorkID:    row.WorkID,
		Reason:    row.Reason,
		CreatedAt: row.CreatedAt,
	}

	if row.BeadID.Valid {
		event.BeadID = row.BeadID.String
	}

	if row.Details.Valid && row.Details.String != "" {
		var detailsMap map[string]interface{}
		if err := json.Unmarshal([]byte(row.Details.String), &detailsMap); err == nil {
			event.Details = detailsMap
		}
	}

	return event
}
