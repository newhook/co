package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/mise"
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
	Root   string        // Project directory path
	Config *Config       // Parsed config.toml
	DB     *db.DB        // Tracking database (lazy loaded)
	Beads  *beads.Client // Beads database client (for issue tracking)
}

// Find finds a project from a flag value or current directory.
// If flagValue is non-empty, uses that path; otherwise uses cwd.
func Find(ctx context.Context, flagValue string) (*Project, error) {
	if flagValue != "" {
		return find(ctx, flagValue)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return find(ctx, cwd)
}

// find walks up from startDir looking for a .co/ directory.
// Returns the project if found, or an error if not found.
func find(ctx context.Context, startDir string) (*Project, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	for {
		configPath := filepath.Join(dir, ConfigDir, ConfigFile)
		if _, err := os.Stat(configPath); err == nil {
			return load(ctx, dir)
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
func load(ctx context.Context, root string) (*Project, error) {
	configPath := filepath.Join(root, ConfigDir, ConfigFile)
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
	}

	proj := &Project{
		Root:   root,
		Config: cfg,
	}

	// Open the database automatically
	dbPath := filepath.Join(root, ConfigDir, TrackingDB)
	database, err := db.OpenPath(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open tracking database: %w", err)
	}
	proj.DB = database

	// Open the beads client automatically
	beadsDBPath := filepath.Join(root, MainDir, ".beads", "beads.db")
	beadsClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
	if err != nil {
		database.Close() // Clean up the already-opened DB
		return nil, fmt.Errorf("failed to open beads database: %w", err)
	}
	proj.Beads = beadsClient

	// Initialize logging to .co/debug.log
	if err := logging.Init(root); err != nil {
		// Log initialization failure is non-fatal, but log it if we can
		logging.Warn("failed to initialize logging", "error", err)
	}

	return proj, nil
}

// Create initializes a new project at the given directory.
// repoSource can be a local path (symlinked) or GitHub URL (cloned).
func Create(ctx context.Context, dir, repoSource string) (*Project, error) {
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
	repoType, err := setupRepo(ctx, repoSource, absDir, mainPath)
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

	// Save config with comprehensive documentation
	configPath := filepath.Join(configDir, ConfigFile)
	if err := cfg.SaveDocumentedConfig(configPath); err != nil {
		os.RemoveAll(absDir)
		return nil, err
	}

	// Initialize tracking database
	dbPath := filepath.Join(configDir, TrackingDB)
	database, err := db.OpenPath(ctx, dbPath)
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
func setupRepo(ctx context.Context, source, projectRoot, mainPath string) (string, error) {
	var repoType string

	if isGitHubURL(source) {
		// Clone from GitHub
		if err := git.Clone(ctx, source, mainPath); err != nil {
			return "", err
		}
		repoType = RepoTypeGitHub
	} else {
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
		repoType = RepoTypeLocal
	}

	// Initialize beads (required - fail on error)
	fmt.Printf("Initializing beads in %s...\n", mainPath)
	if err := beads.Init(ctx, mainPath); err != nil {
		return "", fmt.Errorf("failed to initialize beads: %w", err)
	}
	if err := beads.InstallHooks(ctx, mainPath); err != nil {
		return "", fmt.Errorf("failed to install beads hooks: %w", err)
	}

	// Generate mise config in project root with co's required tools
	if err := mise.GenerateConfig(projectRoot); err != nil {
		fmt.Printf("Warning: failed to generate mise config: %v\n", err)
	} else {
		fmt.Printf("Mise: generated .mise.toml with co requirements\n")
	}

	// Initialize mise in project root (optional - warn on error)
	if err := mise.Initialize(projectRoot); err != nil {
		fmt.Printf("Warning: mise initialization failed: %v\n", err)
	}

	return repoType, nil
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

// Close closes any open resources (database and beads client).
func (p *Project) Close() error {
	var errs []error
	if p.Beads != nil {
		if err := p.Beads.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing beads client: %w", err))
		}
	}
	if p.DB != nil {
		if err := p.DB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing database: %w", err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
