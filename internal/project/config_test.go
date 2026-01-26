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
