package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/newhook/co/internal/db/sqlc"
)

// WorkBead represents a bead assigned to a work with grouping info.
type WorkBead struct {
	WorkID    string
	BeadID    string
	GroupID   int64
	Position  int64
	CreatedAt time.Time
}

// workBeadToLocal converts an sqlc.WorkBead to local WorkBead.
func workBeadToLocal(wb *sqlc.WorkBead) *WorkBead {
	return &WorkBead{
		WorkID:    wb.WorkID,
		BeadID:    wb.BeadID,
		GroupID:   wb.GroupID,
		Position:  wb.Position,
		CreatedAt: wb.CreatedAt,
	}
}

// AddWorkBead adds a bead to a work with the specified group and position.
func (db *DB) AddWorkBead(ctx context.Context, workID, beadID string, groupID, position int64) error {
	err := db.queries.AddWorkBead(ctx, sqlc.AddWorkBeadParams{
		WorkID:   workID,
		BeadID:   beadID,
		GroupID:  groupID,
		Position: position,
	})
	if err != nil {
		return fmt.Errorf("failed to add bead %s to work %s: %w", beadID, workID, err)
	}
	return nil
}

// AddWorkBeads adds multiple beads to a work with the same group ID.
// Beads are positioned sequentially starting from the next available position.
// Returns an error if any bead already exists in the work.
func (db *DB) AddWorkBeads(ctx context.Context, workID string, beadIDs []string, groupID int64) error {
	if len(beadIDs) == 0 {
		return nil
	}

	// Check for existing beads before adding
	existingBeads, err := db.queries.GetWorkBeads(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to check existing beads: %w", err)
	}

	existingSet := make(map[string]bool)
	for _, b := range existingBeads {
		existingSet[b.BeadID] = true
	}

	// Check for duplicates
	var duplicates []string
	for _, beadID := range beadIDs {
		if existingSet[beadID] {
			duplicates = append(duplicates, beadID)
		}
	}
	if len(duplicates) > 0 {
		return fmt.Errorf("beads already exist in work %s: %s", workID, strings.Join(duplicates, ", "))
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.queries.WithTx(tx)

	// Get current max position
	maxPos, err := qtx.GetMaxWorkBeadPosition(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to get max position: %w", err)
	}

	position := maxPos + 1
	for _, beadID := range beadIDs {
		err := qtx.AddWorkBead(ctx, sqlc.AddWorkBeadParams{
			WorkID:   workID,
			BeadID:   beadID,
			GroupID:  groupID,
			Position: position,
		})
		if err != nil {
			return fmt.Errorf("failed to add bead %s: %w", beadID, err)
		}
		position++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// RemoveWorkBead removes a bead from a work.
func (db *DB) RemoveWorkBead(ctx context.Context, workID, beadID string) error {
	rows, err := db.queries.RemoveWorkBead(ctx, sqlc.RemoveWorkBeadParams{
		WorkID: workID,
		BeadID: beadID,
	})
	if err != nil {
		return fmt.Errorf("failed to remove bead %s from work %s: %w", beadID, workID, err)
	}
	if rows == 0 {
		return fmt.Errorf("bead %s not found in work %s", beadID, workID)
	}
	return nil
}

// GetWorkBeads returns all beads assigned to a work.
func (db *DB) GetWorkBeads(ctx context.Context, workID string) ([]*WorkBead, error) {
	beads, err := db.queries.GetWorkBeads(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work beads: %w", err)
	}

	result := make([]*WorkBead, len(beads))
	for i, b := range beads {
		result[i] = workBeadToLocal(&b)
	}
	return result, nil
}

// GetUnassignedWorkBeads returns beads in a work that are not yet in any task.
func (db *DB) GetUnassignedWorkBeads(ctx context.Context, workID string) ([]*WorkBead, error) {
	beads, err := db.queries.GetUnassignedWorkBeads(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get unassigned work beads: %w", err)
	}

	result := make([]*WorkBead, len(beads))
	for i, b := range beads {
		result[i] = workBeadToLocal(&b)
	}
	return result, nil
}

// IsBeadInTask checks if a bead is already assigned to a task in the work.
func (db *DB) IsBeadInTask(ctx context.Context, workID, beadID string) (bool, error) {
	inTask, err := db.queries.IsBeadInTask(ctx, sqlc.IsBeadInTaskParams{
		WorkID: workID,
		BeadID: beadID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check if bead in task: %w", err)
	}
	return inTask, nil
}

// GetNextBeadGroupID returns the next available group ID for a work.
func (db *DB) GetNextBeadGroupID(ctx context.Context, workID string) (int64, error) {
	// First ensure the counter exists
	err := db.queries.InitializeBeadGroupCounter(ctx, workID)
	if err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		return 0, fmt.Errorf("failed to initialize bead group counter: %w", err)
	}

	// Get and increment the counter atomically
	groupID, err := db.queries.GetAndIncrementBeadGroupCounter(ctx, workID)
	if err != nil {
		return 0, fmt.Errorf("failed to get next bead group ID: %w", err)
	}
	return groupID, nil
}

// UpdateWorkBeadGroup updates the group ID for a bead in a work.
func (db *DB) UpdateWorkBeadGroup(ctx context.Context, workID, beadID string, groupID int64) error {
	rows, err := db.queries.UpdateWorkBeadGroup(ctx, sqlc.UpdateWorkBeadGroupParams{
		GroupID: groupID,
		WorkID:  workID,
		BeadID:  beadID,
	})
	if err != nil {
		return fmt.Errorf("failed to update bead group: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("bead %s not found in work %s", beadID, workID)
	}
	return nil
}

// GetWorkBeadGroups returns all distinct group IDs for a work (excluding 0).
func (db *DB) GetWorkBeadGroups(ctx context.Context, workID string) ([]int64, error) {
	groups, err := db.queries.GetWorkBeadGroups(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work bead groups: %w", err)
	}
	return groups, nil
}

// GetWorkBeadsByGroup returns all beads in a specific group.
func (db *DB) GetWorkBeadsByGroup(ctx context.Context, workID string, groupID int64) ([]*WorkBead, error) {
	beads, err := db.queries.GetWorkBeadsByGroup(ctx, sqlc.GetWorkBeadsByGroupParams{
		WorkID:  workID,
		GroupID: groupID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get beads by group: %w", err)
	}

	result := make([]*WorkBead, len(beads))
	for i, b := range beads {
		result[i] = workBeadToLocal(&b)
	}
	return result, nil
}

// DeleteWorkBeads removes all beads from a work.
func (db *DB) DeleteWorkBeads(ctx context.Context, workID string) error {
	_, err := db.queries.DeleteWorkBeads(ctx, workID)
	if err != nil {
		return fmt.Errorf("failed to delete work beads: %w", err)
	}
	return nil
}

// InitializeBeadGroupCounter initializes the group counter for a work.
func (db *DB) InitializeBeadGroupCounter(ctx context.Context, workID string) error {
	err := db.queries.InitializeBeadGroupCounter(ctx, workID)
	if err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("failed to initialize bead group counter: %w", err)
	}
	return nil
}

// GetAllAssignedBeads returns a map of bead IDs to work IDs for all beads
// that are assigned to any work. This is used by plan mode to show which
// beads are already assigned.
func (db *DB) GetAllAssignedBeads(ctx context.Context) (map[string]string, error) {
	rows, err := db.queries.GetAllAssignedBeads(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get assigned beads: %w", err)
	}

	result := make(map[string]string, len(rows))
	for _, row := range rows {
		result[row.BeadID] = row.WorkID
	}
	return result, nil
}
