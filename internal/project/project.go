package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/newhook/co/internal/db"
)

const (
	// ConfigDir is the directory name for project configuration.
	ConfigDir = ".co"
	// ConfigFile is the name of the project config file.
	ConfigFile = "config.toml"
	// TrackingDB is the name of the tracking database file.
	TrackingDB = "tracking.db"
	// MainDir is the directory name for the main repository.
	MainDir = "main"

	// RepoTypeLocal indicates a symlinked local repository.
	RepoTypeLocal = "local"
	// RepoTypeGitHub indicates a cloned GitHub repository.
	RepoTypeGitHub = "github"
)

// Project represents an orchestrator project.
type Project struct {
	Root   string  // Project directory path
	Config *Config // Parsed config.toml
	DB     *db.DB  // Tracking database (lazy loaded)
}

// FindWithFlag finds a project from a flag value or current directory.
// If flagValue is non-empty, uses that path; otherwise uses cwd.
func FindWithFlag(flagValue string) (*Project, error) {
	if flagValue != "" {
		return Find(flagValue)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return Find(cwd)
}

// Find walks up from startDir looking for a .co/ directory.
// Returns the project if found, or an error if not found.
func Find(startDir string) (*Project, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	for {
		configPath := filepath.Join(dir, ConfigDir, ConfigFile)
		if _, err := os.Stat(configPath); err == nil {
			return load(dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return nil, fmt.Errorf("no project found (no %s directory)", ConfigDir)
		}
		dir = parent
	}
}

// load loads a project from the given root directory.
func load(root string) (*Project, error) {
	configPath := filepath.Join(root, ConfigDir, ConfigFile)
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
	}

	return &Project{
		Root:   root,
		Config: cfg,
	}, nil
}

// Create initializes a new project at the given directory.
// repoSource can be a local path (symlinked) or GitHub URL (cloned).
func Create(dir, repoSource string) (*Project, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check if project already exists
	configDir := filepath.Join(absDir, ConfigDir)
	if _, err := os.Stat(configDir); err == nil {
		return nil, fmt.Errorf("project already exists at %s", absDir)
	}

	// Create project directory structure
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	mainPath := filepath.Join(absDir, MainDir)

	// Determine repo type and set up main/
	repoType, err := setupRepo(repoSource, mainPath)
	if err != nil {
		// Clean up on failure
		os.RemoveAll(absDir)
		return nil, err
	}

	// Create config
	cfg := &Config{
		Project: ProjectConfig{
			Name:      filepath.Base(absDir),
			CreatedAt: time.Now(),
		},
		Repo: RepoConfig{
			Type:   repoType,
			Source: repoSource,
			Path:   MainDir,
		},
	}

	// Save config
	configPath := filepath.Join(configDir, ConfigFile)
	if err := cfg.SaveConfig(configPath); err != nil {
		os.RemoveAll(absDir)
		return nil, err
	}

	// Initialize tracking database
	dbPath := filepath.Join(configDir, TrackingDB)
	database, err := db.OpenPath(dbPath)
	if err != nil {
		os.RemoveAll(absDir)
		return nil, fmt.Errorf("failed to initialize tracking database: %w", err)
	}
	database.Close()

	return &Project{
		Root:   absDir,
		Config: cfg,
	}, nil
}

// setupRepo sets up the main/ directory based on the repo source.
// Returns the repo type ("local" or "github").
func setupRepo(source, mainPath string) (string, error) {
	if isGitHubURL(source) {
		// Clone from GitHub
		cmd := exec.Command("git", "clone", source, mainPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("failed to clone repository: %w\n%s", err, output)
		}
		return RepoTypeGitHub, nil
	}

	// Local path - create symlink
	absSource, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("failed to resolve source path: %w", err)
	}

	// Verify source exists and is a directory
	info, err := os.Stat(absSource)
	if err != nil {
		return "", fmt.Errorf("source path does not exist: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source path is not a directory: %s", absSource)
	}

	// Create symlink
	if err := os.Symlink(absSource, mainPath); err != nil {
		return "", fmt.Errorf("failed to create symlink: %w", err)
	}

	return RepoTypeLocal, nil
}

// isGitHubURL returns true if the source looks like a GitHub URL.
func isGitHubURL(source string) bool {
	return strings.HasPrefix(source, "https://github.com/") ||
		strings.HasPrefix(source, "git@github.com:") ||
		strings.HasPrefix(source, "http://github.com/")
}

// MainRepoPath returns the path to the main repository.
func (p *Project) MainRepoPath() string {
	return filepath.Join(p.Root, MainDir)
}

// WorktreePath returns the path where a task's worktree should be created.
func (p *Project) WorktreePath(taskID string) string {
	return filepath.Join(p.Root, taskID)
}

// TrackingDBPath returns the path to the tracking database.
func (p *Project) TrackingDBPath() string {
	return filepath.Join(p.Root, ConfigDir, TrackingDB)
}

// OpenDB opens the project's tracking database.
func (p *Project) OpenDB() (*db.DB, error) {
	if p.DB != nil {
		return p.DB, nil
	}

	database, err := db.OpenPath(p.TrackingDBPath())
	if err != nil {
		return nil, fmt.Errorf("failed to open tracking database: %w", err)
	}
	p.DB = database
	return database, nil
}

// Close closes any open resources (like the database).
func (p *Project) Close() error {
	if p.DB != nil {
		return p.DB.Close()
	}
	return nil
}
