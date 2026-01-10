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

// Dependency represents a dependency relationship.
type Dependency struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	DependencyType string `json:"dependency_type"`
}

// BeadWithDeps represents a bead with its dependency information.
type BeadWithDeps struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Status       string       `json:"status"`
	Dependencies []Dependency `json:"dependencies"`
}

// GetReadyBeadsInDir queries beads in a specific directory.
func GetReadyBeadsInDir(dir string) ([]Bead, error) {
	cmd := exec.Command("bd", "ready", "--json")
	if dir != "" {
		cmd.Dir = dir
	}
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

// GetBeadInDir retrieves a single bead by ID in a specific directory.
func GetBeadInDir(id, dir string) (*Bead, error) {
	cmd := exec.Command("bd", "show", id, "--json")
	if dir != "" {
		cmd.Dir = dir
	}
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

// GetBeadWithDepsInDir retrieves a single bead by ID including its dependencies in a specific directory.
func GetBeadWithDepsInDir(id, dir string) (*BeadWithDeps, error) {
	cmd := exec.Command("bd", "show", id, "--json")
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get bead %s: %w", id, err)
	}

	var beads []BeadWithDeps
	if err := json.Unmarshal(output, &beads); err != nil {
		return nil, fmt.Errorf("failed to parse bead %s: %w", id, err)
	}

	if len(beads) == 0 {
		return nil, fmt.Errorf("bead %s not found", id)
	}

	return &beads[0], nil
}

// InitInDir initializes beads in the specified directory.
// Runs: bd init
func InitInDir(dir string) error {
	cmd := exec.Command("bd", "init")
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd init failed: %w\n%s", err, output)
	}
	return nil
}

// InstallHooksInDir installs beads hooks in the specified directory.
// Runs: bd hooks install
func InstallHooksInDir(dir string) error {
	cmd := exec.Command("bd", "hooks", "install")
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd hooks install failed: %w\n%s", err, output)
	}
	return nil
}
