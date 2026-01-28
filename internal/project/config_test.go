package project

import (
	"testing"
	"time"
	"github.com/BurntSushi/toml"
)

func TestGeneratedConfigIsValidTOML(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{
			Name:      "test-project",
			CreatedAt: time.Date(2026, 1, 26, 10, 30, 0, 0, time.UTC),
		},
		Repo: RepoConfig{
			Type:   "github",
			Source: "https://github.com/example/repo",
			Path:   "main",
		},
	}
	content := cfg.GenerateDocumentedConfig()

	// Try to parse the generated content as valid TOML
	var parsed map[string]interface{}
	if _, err := toml.Decode(content, &parsed); err != nil {
		t.Errorf("Generated config is not valid TOML: %v\n\nContent:\n%s", err, content)
	}

	// Verify key fields are present
	project := parsed["project"].(map[string]interface{})
	if project["name"] != "test-project" {
		t.Errorf("Expected project.name to be 'test-project', got %v", project["name"])
	}

	repo := parsed["repo"].(map[string]interface{})
	if repo["type"] != "github" {
		t.Errorf("Expected repo.type to be 'github', got %v", repo["type"])
	}
}

func TestGeneratedConfigRoundTrip(t *testing.T) {
	original := &Config{
		Project: ProjectConfig{
			Name:      "test-project",
			CreatedAt: time.Date(2026, 1, 26, 10, 30, 0, 0, time.UTC),
		},
		Repo: RepoConfig{
			Type:   "github",
			Source: "https://github.com/example/repo",
			Path:   "main",
		},
	}

	content := original.GenerateDocumentedConfig()

	// Parse back into a Config struct
	var loaded Config
	if _, err := toml.Decode(content, &loaded); err != nil {
		t.Fatalf("Failed to decode generated config: %v", err)
	}

	// Verify fields match
	if loaded.Project.Name != original.Project.Name {
		t.Errorf("Project.Name: expected %q, got %q", original.Project.Name, loaded.Project.Name)
	}
	if !loaded.Project.CreatedAt.Equal(original.Project.CreatedAt) {
		t.Errorf("Project.CreatedAt: expected %v, got %v", original.Project.CreatedAt, loaded.Project.CreatedAt)
	}
	if loaded.Repo.Type != original.Repo.Type {
		t.Errorf("Repo.Type: expected %q, got %q", original.Repo.Type, loaded.Repo.Type)
	}
	if loaded.Repo.Source != original.Repo.Source {
		t.Errorf("Repo.Source: expected %q, got %q", original.Repo.Source, loaded.Repo.Source)
	}
	if loaded.Repo.Path != original.Repo.Path {
		t.Errorf("Repo.Path: expected %q, got %q", original.Repo.Path, loaded.Repo.Path)
	}
}

func TestGeneratedConfigWithSpecialCharacters(t *testing.T) {
	// Test with special characters that could break TOML
	cfg := &Config{
		Project: ProjectConfig{
			Name:      "test-project-with\"quotes",
			CreatedAt: time.Date(2026, 1, 26, 10, 30, 0, 0, time.UTC),
		},
		Repo: RepoConfig{
			Type:   "github",
			Source: "https://github.com/user/repo with spaces",
			Path:   "main",
		},
	}

	content := cfg.GenerateDocumentedConfig()

	// This should parse successfully even with special characters
	var parsed Config
	if _, err := toml.Decode(content, &parsed); err != nil {
		t.Errorf("Failed to parse config with special characters: %v\n\nContent:\n%s", err, content)
	}

	// Verify values
	if parsed.Project.Name != cfg.Project.Name {
		t.Errorf("Project.Name: expected %q, got %q", cfg.Project.Name, parsed.Project.Name)
	}
	if parsed.Repo.Source != cfg.Repo.Source {
		t.Errorf("Repo.Source: expected %q, got %q", cfg.Repo.Source, parsed.Repo.Source)
	}
}

func TestShouldSkipPermissionsDefault(t *testing.T) {
	// When Claude config is not specified in TOML, ShouldSkipPermissions should default to true
	tomlContent := `
[project]
  name = "test"
`
	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if !cfg.Claude.ShouldSkipPermissions() {
		t.Error("Expected ShouldSkipPermissions() to return true by default, got false")
	}
}

func TestShouldSkipPermissionsExplicitFalse(t *testing.T) {
	// When explicitly set to false, ShouldSkipPermissions should return false
	tomlContent := `
[project]
  name = "test"

[claude]
  skip_permissions = false
`
	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if cfg.Claude.ShouldSkipPermissions() {
		t.Error("Expected ShouldSkipPermissions() to return false when explicitly set, got true")
	}
}

func TestGeneratedConfigWithUTF8(t *testing.T) {
	// Test with UTF-8 characters
	cfg := &Config{
		Project: ProjectConfig{
			Name:      "проект-名前-مشروع",
			CreatedAt: time.Date(2026, 1, 26, 10, 30, 0, 0, time.UTC),
		},
		Repo: RepoConfig{
			Type:   "github",
			Source: "https://github.com/日本/リポジトリ",
			Path:   "main",
		},
	}

	content := cfg.GenerateDocumentedConfig()

	// This should parse successfully
	var parsed Config
	if _, err := toml.Decode(content, &parsed); err != nil {
		t.Errorf("Failed to parse config with UTF-8: %v", err)
	}

	// Verify values
	if parsed.Project.Name != cfg.Project.Name {
		t.Errorf("Project.Name: expected %q, got %q", cfg.Project.Name, parsed.Project.Name)
	}
}

func TestLogParserConfig_ShouldUseClaude(t *testing.T) {
	tests := []struct {
		name     string
		config   LogParserConfig
		expected bool
	}{
		{
			name:     "Default (false)",
			config:   LogParserConfig{},
			expected: false,
		},
		{
			name:     "Explicitly enabled",
			config:   LogParserConfig{UseClaude: true},
			expected: true,
		},
		{
			name:     "Explicitly disabled",
			config:   LogParserConfig{UseClaude: false},
			expected: false,
		},
		{
			name:     "Enabled with model",
			config:   LogParserConfig{UseClaude: true, Model: "sonnet"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ShouldUseClaude()
			if result != tt.expected {
				t.Errorf("ShouldUseClaude() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLogParserConfig_GetModel(t *testing.T) {
	tests := []struct {
		name     string
		config   LogParserConfig
		expected string
	}{
		{
			name:     "Default (empty) returns haiku",
			config:   LogParserConfig{},
			expected: "haiku",
		},
		{
			name:     "Haiku model",
			config:   LogParserConfig{Model: "haiku"},
			expected: "haiku",
		},
		{
			name:     "Sonnet model",
			config:   LogParserConfig{Model: "sonnet"},
			expected: "sonnet",
		},
		{
			name:     "Opus model",
			config:   LogParserConfig{Model: "opus"},
			expected: "opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetModel()
			if result != tt.expected {
				t.Errorf("GetModel() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLogParserConfigFromTOML(t *testing.T) {
	tests := []struct {
		name              string
		tomlContent       string
		wantUseClaude     bool
		wantModel         string
	}{
		{
			name: "Not specified defaults",
			tomlContent: `
[project]
name = "test"
`,
			wantUseClaude: false,
			wantModel:     "haiku",
		},
		{
			name: "Enabled with haiku",
			tomlContent: `
[project]
name = "test"

[log_parser]
use_claude = true
model = "haiku"
`,
			wantUseClaude: true,
			wantModel:     "haiku",
		},
		{
			name: "Enabled with sonnet",
			tomlContent: `
[project]
name = "test"

[log_parser]
use_claude = true
model = "sonnet"
`,
			wantUseClaude: true,
			wantModel:     "sonnet",
		},
		{
			name: "Enabled without model (defaults to haiku)",
			tomlContent: `
[project]
name = "test"

[log_parser]
use_claude = true
`,
			wantUseClaude: true,
			wantModel:     "haiku",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			if _, err := toml.Decode(tt.tomlContent, &cfg); err != nil {
				t.Fatalf("Failed to decode TOML: %v", err)
			}

			if cfg.LogParser.ShouldUseClaude() != tt.wantUseClaude {
				t.Errorf("ShouldUseClaude() = %v, want %v", cfg.LogParser.ShouldUseClaude(), tt.wantUseClaude)
			}

			if cfg.LogParser.GetModel() != tt.wantModel {
				t.Errorf("GetModel() = %q, want %q", cfg.LogParser.GetModel(), tt.wantModel)
			}
		})
	}
}
