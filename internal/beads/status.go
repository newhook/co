package beads

// Bead status constants.
// These are the canonical status values used by the beads issue tracker.
const (
	StatusOpen       = "open"        // Default status for new issues
	StatusInProgress = "in_progress" // Issue is being actively worked on
	StatusBlocked    = "blocked"     // Issue cannot proceed due to dependencies
	StatusDeferred   = "deferred"    // Issue is postponed (not actively worked on)
	StatusClosed     = "closed"      // Issue is completed or resolved
)

// IsWorkableStatus returns true if the status indicates the bead can be worked on.
// This includes open and in_progress statuses (not blocked or deferred).
func IsWorkableStatus(status string) bool {
	switch status {
	case StatusOpen, StatusInProgress, "":
		return true
	default:
		return false
	}
}
