package beads

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// Bead represents a work item from the beads system.
type Bead struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// GetReadyBeads queries the beads system for work items that are ready to be processed.
func GetReadyBeads() ([]Bead, error) {
	cmd := exec.Command("bd", "ready", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run bd ready: %w", err)
	}

	var beads []Bead
	if err := json.Unmarshal(output, &beads); err != nil {
		return nil, fmt.Errorf("failed to parse bd ready output: %w", err)
	}

	return beads, nil
}

// GetBead retrieves a single bead by ID.
func GetBead(id string) (*Bead, error) {
	cmd := exec.Command("bd", "show", id, "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get bead %s: %w", id, err)
	}

	var beads []Bead
	if err := json.Unmarshal(output, &beads); err != nil {
		return nil, fmt.Errorf("failed to parse bead %s: %w", id, err)
	}

	if len(beads) == 0 {
		return nil, fmt.Errorf("bead %s not found", id)
	}

	return &beads[0], nil
}

// CloseBead marks a bead as complete with the given reason.
func CloseBead(id, reason string) error {
	cmd := exec.Command("bd", "close", id, "--reason", reason)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to close bead %s: %w", id, err)
	}
	return nil
}
