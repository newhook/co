package project

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config represents the project configuration stored in .co/config.toml.
type Config struct {
	Project ProjectConfig `toml:"project"`
	Repo    RepoConfig    `toml:"repo"`
	Hooks   HooksConfig   `toml:"hooks"`
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
