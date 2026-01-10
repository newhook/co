package db

import (
	"crypto/sha256"
	"fmt"
)

// CacheComplexity stores a complexity estimate for a bead in the cache.
func (db *DB) CacheComplexity(beadID, descHash string, score, tokens int) error {
	// Use REPLACE to handle both insert and update cases
	// SQLite doesn't have ON CONFLICT ... DO UPDATE, but REPLACE works similarly
	_, err := db.Exec(`
		REPLACE INTO complexity_cache (bead_id, description_hash, complexity_score, estimated_tokens)
		VALUES (?, ?, ?, ?)
	`, beadID, descHash, score, tokens)
	if err != nil {
		return fmt.Errorf("failed to cache complexity for %s: %w", beadID, err)
	}
	return nil
}

// GetCachedComplexity retrieves cached complexity for a bead if it exists and the description hash matches.
func (db *DB) GetCachedComplexity(beadID, descHash string) (score, tokens int, found bool, err error) {
	err = db.QueryRow(`
		SELECT complexity_score, estimated_tokens
		FROM complexity_cache
		WHERE bead_id = ? AND description_hash = ?
	`, beadID, descHash).Scan(&score, &tokens)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return 0, 0, false, nil
		}
		return 0, 0, false, fmt.Errorf("failed to get cached complexity: %w", err)
	}

	return score, tokens, true, nil
}

// GetAllCachedComplexity returns all cached complexity estimates.
func (db *DB) GetAllCachedComplexity() (map[string]struct{ Score, Tokens int }, error) {
	rows, err := db.Query(`
		SELECT bead_id, complexity_score, estimated_tokens
		FROM complexity_cache
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query complexity cache: %w", err)
	}
	defer rows.Close()

	result := make(map[string]struct{ Score, Tokens int })
	for rows.Next() {
		var beadID string
		var score, tokens int
		if err := rows.Scan(&beadID, &score, &tokens); err != nil {
			return nil, fmt.Errorf("failed to scan complexity row: %w", err)
		}
		result[beadID] = struct{ Score, Tokens int }{score, tokens}
	}
	return result, rows.Err()
}

// HashDescription creates a SHA256 hash of a description string.
func HashDescription(description string) string {
	h := sha256.Sum256([]byte(description))
	return fmt.Sprintf("%x", h)
}

// AreAllBeadsEstimated checks if all beads in the list have complexity estimates.
func (db *DB) AreAllBeadsEstimated(beadIDs []string) (bool, error) {
	if len(beadIDs) == 0 {
		return true, nil
	}

	// Count how many of the beads have estimates
	query := `SELECT COUNT(DISTINCT bead_id) FROM complexity_cache WHERE bead_id IN (`
	args := make([]interface{}, len(beadIDs))
	for i, id := range beadIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	var count int
	err := db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to count estimated beads: %w", err)
	}

	return count == len(beadIDs), nil
}