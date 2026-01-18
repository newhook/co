package project

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config represents the project configuration stored in .co/config.toml.
type Config struct {
	Project  ProjectConfig  `toml:"project"`
	Repo     RepoConfig     `toml:"repo"`
	Hooks    HooksConfig    `toml:"hooks"`
	Linear   LinearConfig   `toml:"linear"`
	Claude   ClaudeConfig   `toml:"claude"`
	Workflow WorkflowConfig `toml:"workflow"`
}

// ClaudeConfig contains Claude Code configuration.
type ClaudeConfig struct {
	// SkipPermissions controls whether to run Claude with --dangerously-skip-permissions.
	// Defaults to true when not specified in config.
	SkipPermissions *bool `toml:"skip_permissions"`
}

// ShouldSkipPermissions returns true if Claude should run with --dangerously-skip-permissions.
// Defaults to true when not explicitly configured.
func (c *ClaudeConfig) ShouldSkipPermissions() bool {
	if c.SkipPermissions == nil {
		return true // default to true
	}
	return *c.SkipPermissions
}

// ProjectConfig contains project metadata.
type ProjectConfig struct {
	Name      string    `toml:"name"`
	CreatedAt time.Time `toml:"created_at"`
}

// RepoConfig contains repository configuration.
type RepoConfig struct {
	Type   string `toml:"type"`   // "local" or "github"
	Source string `toml:"source"` // Original path or URL
	Path   string `toml:"path"`   // Always "main"
}

// HooksConfig contains hook configuration.
type HooksConfig struct {
	// Env is a list of environment variables to set before running commands.
	// Format: ["KEY=value", "ANOTHER_KEY=value"]
	// These are applied when spawning Claude in zellij tabs.
	Env []string `toml:"env"`
}

// LinearConfig contains Linear integration configuration.
type LinearConfig struct {
	// APIKey is the Linear API key for authentication.
	// Can also be set via LINEAR_API_KEY environment variable.
	APIKey string `toml:"api_key"`
}

// WorkflowConfig contains workflow configuration.
type WorkflowConfig struct {
	// MaxReviewIterations limits the number of review/fix cycles.
	// Defaults to 2 when not specified.
	MaxReviewIterations *int `toml:"max_review_iterations"`
}

// GetMaxReviewIterations returns the configured max review iterations or 2 if not specified.
func (w *WorkflowConfig) GetMaxReviewIterations() int {
	if w.MaxReviewIterations == nil {
		return 2
	}
	return *w.MaxReviewIterations
}

// LoadConfig reads and parses a config.toml file.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig writes the config to the specified path.
func (c *Config) SaveConfig(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	return nil
}
