// Package names provides human-readable name assignment for workers.
// Names are drawn from a predefined list and assigned to workers for easy identification.
package names

import (
	"context"
	"database/sql"
	"fmt"
)

// predefinedNames is the list of human names to assign to workers.
// These names are used in order of availability.
var predefinedNames = []string{
	"Alice",
	"Bob",
	"Charlie",
	"Diana",
	"Eve",
	"Frank",
	"Grace",
	"Henry",
	"Iris",
	"Jack",
	"Kate",
	"Leo",
	"Maya",
	"Nick",
	"Olivia",
	"Paul",
	"Quinn",
	"Rose",
	"Sam",
	"Tina",
	"Uma",
	"Victor",
	"Wendy",
	"Xavier",
	"Yara",
	"Zack",
}

// DB is the interface required for name operations.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// GetNextAvailableName returns the next available human name that is not currently in use.
// It queries the database for works that have names assigned and returns the first
// name from the predefined list that is not in use.
// Returns empty string if all names are in use.
func GetNextAvailableName(ctx context.Context, db DB) (string, error) {
	// Get all currently used names
	usedNames, err := getUsedNames(ctx, db)
	if err != nil {
		return "", fmt.Errorf("failed to get used names: %w", err)
	}

	// Create a set of used names for O(1) lookup
	usedSet := make(map[string]bool)
	for _, name := range usedNames {
		usedSet[name] = true
	}

	// Find the first available name
	for _, name := range predefinedNames {
		if !usedSet[name] {
			return name, nil
		}
	}

	// All names are in use
	return "", nil
}

// getUsedNames returns all names currently assigned to active works.
// A work is considered active if it's not completed or failed.
func getUsedNames(ctx context.Context, db DB) ([]string, error) {
	query := `
		SELECT name FROM works
		WHERE name != ''
		AND status NOT IN ('completed', 'failed')
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// ReleaseName is a no-op in the current implementation since names are automatically
// released when a work is completed or failed (they're excluded from the used names query).
// This function is provided for API completeness.
func ReleaseName(_ context.Context, _ DB, _ string) error {
	// Names are automatically released when work status changes to completed/failed
	return nil
}

// GetAllNames returns the full list of predefined names.
func GetAllNames() []string {
	return append([]string(nil), predefinedNames...)
}
