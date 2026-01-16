package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/newhook/co/internal/beads/cachemanager"
	"github.com/newhook/co/internal/beads/queries"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
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
	Dependents   []Dependency `json:"dependents"`
}

// BeadFull represents a bead with all available fields from bd list/show.
type BeadFull struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Status          string `json:"status"`
	Priority        int    `json:"priority"`
	Type            string `json:"issue_type"`
	DependencyCount int    `json:"dependency_count"`
	DependentCount  int    `json:"dependent_count"`
}

// ListFilters specifies filters for listing beads.
type ListFilters struct {
	Status string // "open", "closed", or empty for all
	Label  string // Filter by label
}

// GetReadyBeads queries ready beads in a specific directory.
func GetReadyBeads(ctx context.Context, dir string) ([]Bead, error) {
	cmd := exec.CommandContext(ctx, "bd", "ready", "--json")
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

// GetReadyBeadsFull queries ready beads with full details in a specific directory.
func GetReadyBeadsFull(ctx context.Context, dir string) ([]BeadFull, error) {
	cmd := exec.CommandContext(ctx, "bd", "ready", "--json")
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run bd ready: %w", err)
	}

	var beads []BeadFull
	if err := json.Unmarshal(output, &beads); err != nil {
		return nil, fmt.Errorf("failed to parse bd ready output: %w", err)
	}

	return beads, nil
}

// ListBeads lists beads with optional filters.
func ListBeads(ctx context.Context, dir string, filters ListFilters) ([]BeadFull, error) {
	args := []string{"list", "--json"}
	if filters.Status == "open" || filters.Status == "closed" {
		args = append(args, "--status="+filters.Status)
	}
	if filters.Label != "" {
		args = append(args, "--label="+filters.Label)
	}

	cmd := exec.CommandContext(ctx, "bd", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run bd list: %w", err)
	}

	var beads []BeadFull
	if err := json.Unmarshal(output, &beads); err != nil {
		return nil, fmt.Errorf("failed to parse bd list output: %w", err)
	}

	return beads, nil
}

// GetBead retrieves a single bead by ID in a specific directory.
func GetBead(ctx context.Context, id, dir string) (*Bead, error) {
	cmd := exec.CommandContext(ctx, "bd", "show", id, "--json")
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

// GetBeadFull retrieves a single bead by ID with full details.
func GetBeadFull(ctx context.Context, id, dir string) (*BeadFull, error) {
	cmd := exec.CommandContext(ctx, "bd", "show", id, "--json")
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get bead %s: %w", id, err)
	}

	var beads []BeadFull
	if err := json.Unmarshal(output, &beads); err != nil {
		return nil, fmt.Errorf("failed to parse bead %s: %w", id, err)
	}

	if len(beads) == 0 {
		return nil, fmt.Errorf("bead %s not found", id)
	}

	return &beads[0], nil
}

// GetBeadWithDeps retrieves a single bead by ID including its dependencies.
func GetBeadWithDeps(ctx context.Context, id, dir string) (*BeadWithDeps, error) {
	cmd := exec.CommandContext(ctx, "bd", "show", id, "--json")
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

// GetDependencies gets the list of issues that block the given issue.
// Returns only dependencies of type "blocks".
func GetDependencies(ctx context.Context, beadID, dir string) ([]Dependency, error) {
	cmd := exec.CommandContext(ctx, "bd", "dep", "list", beadID, "--json")
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies for %s: %w", beadID, err)
	}

	var deps []Dependency
	if err := json.Unmarshal(output, &deps); err != nil {
		return nil, fmt.Errorf("failed to parse dependencies for %s: %w", beadID, err)
	}

	// Filter to only "blocks" type
	var result []Dependency
	for _, d := range deps {
		if d.DependencyType == "blocks" {
			result = append(result, d)
		}
	}
	return result, nil
}

// Init initializes beads in the specified directory.
func Init(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "bd", "init")
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd init failed: %w\n%s", err, output)
	}
	return nil
}

// InstallHooks installs beads hooks in the specified directory.
func InstallHooks(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "bd", "hooks", "install")
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd hooks install failed: %w\n%s", err, output)
	}
	return nil
}

// CloseEligibleEpicsInDir closes any epics where all children are complete.
// Runs: bd epic close-eligible
func CloseEligibleEpicsInDir(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "bd", "epic", "close-eligible")
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd epic close-eligible failed: %w\n%s", err, output)
	}
	return nil
}

// CreateOptions specifies options for creating a bead.
type CreateOptions struct {
	Title       string
	Type        string // "task", "bug", "feature"
	Priority    int
	IsEpic      bool
	Description string
}

// Create creates a new bead and returns its ID.
func Create(ctx context.Context, dir string, opts CreateOptions) (string, error) {
	args := []string{"create", "--title=" + opts.Title, "--type=" + opts.Type, fmt.Sprintf("--priority=%d", opts.Priority)}
	if opts.IsEpic {
		args = append(args, "--epic")
	}
	if opts.Description != "" {
		args = append(args, "--description="+opts.Description)
	}

	cmd := exec.CommandContext(ctx, "bd", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("failed to create bead: %w\n%s", err, exitErr.Stderr)
		}
		return "", fmt.Errorf("failed to create bead: %w", err)
	}

	// Parse the bead ID from output
	beadID := strings.TrimSpace(string(output))
	// Handle case where output might have extra text
	if strings.Contains(beadID, " ") {
		parts := strings.Fields(beadID)
		for _, p := range parts {
			if strings.HasPrefix(p, "ac-") || strings.HasPrefix(p, "bd-") {
				beadID = p
				break
			}
		}
	}

	if beadID == "" {
		return "", fmt.Errorf("failed to get created bead ID from output: %s", output)
	}

	return beadID, nil
}

// Close closes a bead.
func Close(ctx context.Context, beadID, dir string) error {
	cmd := exec.CommandContext(ctx, "bd", "close", beadID)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to close bead %s: %w\n%s", beadID, err, output)
	}
	return nil
}

// Reopen reopens a closed bead.
func Reopen(ctx context.Context, beadID, dir string) error {
	cmd := exec.CommandContext(ctx, "bd", "reopen", beadID)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reopen bead %s: %w\n%s", beadID, err, output)
	}
	return nil
}

// UpdateOptions specifies options for updating a bead.
type UpdateOptions struct {
	Title       string
	Type        string
	Description string
}

// Update updates a bead's fields.
func Update(ctx context.Context, beadID, dir string, opts UpdateOptions) error {
	args := []string{"update", beadID, "--title=" + opts.Title, "--type=" + opts.Type}
	if opts.Description != "" {
		args = append(args, "--description="+opts.Description)
	}

	cmd := exec.CommandContext(ctx, "bd", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to update bead %s: %w\n%s", beadID, err, output)
	}
	return nil
}

// AddDependency adds a dependency between two beads.
// The bead identified by beadID will depend on the bead identified by dependsOnID.
func AddDependency(ctx context.Context, beadID, dependsOnID, dir string) error {
	cmd := exec.CommandContext(ctx, "bd", "dep", "add", beadID, dependsOnID)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add dependency %s -> %s: %w\n%s", beadID, dependsOnID, err, output)
	}
	return nil
}

// EditCommand returns an exec.Cmd for opening a bead in an editor.
// This is meant to be used with tea.ExecProcess for interactive editing.
func EditCommand(beadID, dir string) *exec.Cmd {
	cmd := exec.Command("bd", "edit", beadID)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd
}

// GetTransitiveDependencies collects all transitive dependencies for a bead.
// It traverses the "blocked_by" dependency type to find all beads that must
// be completed before the given bead. Returns beads in dependency order
// (dependencies before dependents).
func GetTransitiveDependencies(ctx context.Context, id, dir string) ([]BeadWithDeps, error) {
	visited := make(map[string]bool)
	var result []BeadWithDeps

	// Use a recursive helper to collect dependencies
	var collect func(beadID string) error
	collect = func(beadID string) error {
		if visited[beadID] {
			return nil
		}
		visited[beadID] = true

		bead, err := GetBeadWithDeps(ctx, beadID, dir)
		if err != nil {
			return fmt.Errorf("failed to get bead %s: %w", beadID, err)
		}

		// First, recursively collect all dependencies
		for _, dep := range bead.Dependencies {
			if dep.DependencyType == "blocked_by" && !visited[dep.ID] {
				if err := collect(dep.ID); err != nil {
					return err
				}
			}
		}

		// Then add this bead (ensures dependencies come before dependents)
		result = append(result, *bead)
		return nil
	}

	if err := collect(id); err != nil {
		return nil, err
	}

	return result, nil
}

// GetBeadWithChildren retrieves a bead and all its child beads recursively.
// This is useful for epic beads that have sub-beads.
func GetBeadWithChildren(ctx context.Context, id, dir string) ([]BeadWithDeps, error) {
	visited := make(map[string]bool)
	var result []BeadWithDeps

	var collect func(beadID string) error
	collect = func(beadID string) error {
		if visited[beadID] {
			return nil
		}
		visited[beadID] = true

		bead, err := GetBeadWithDeps(ctx, beadID, dir)
		if err != nil {
			return fmt.Errorf("failed to get bead %s: %w", beadID, err)
		}

		result = append(result, *bead)

		// Recursively collect children (parent-child relationship in dependents)
		for _, dep := range bead.Dependents {
			if dep.DependencyType == "parent-child" && !visited[dep.ID] {
				if err := collect(dep.ID); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := collect(id); err != nil {
		return nil, err
	}

	return result, nil
}

// Client provides database access to beads with caching.
type Client struct {
	db           *sql.DB
	queries      *queries.Queries
	cache        cachemanager.CacheManager[string, *IssuesWithDepsResult]
	dbPath       string
	cacheEnabled bool
}

// IssuesWithDepsResult holds the result of GetIssuesWithDeps.
type IssuesWithDepsResult struct {
	Issues       map[string]queries.Issue
	Dependencies map[string][]queries.GetDependenciesForIssuesRow
	Dependents   map[string][]queries.GetDependentsForIssuesRow
}

// ClientConfig holds configuration for the Client.
type ClientConfig struct {
	DBPath           string
	CacheEnabled     bool
	CacheExpiration  time.Duration
	CacheCleanupTime time.Duration
}

// DefaultClientConfig returns default configuration for the Client.
func DefaultClientConfig(dbPath string) ClientConfig {
	return ClientConfig{
		DBPath:           dbPath,
		CacheEnabled:     true,
		CacheExpiration:  10 * time.Minute,
		CacheCleanupTime: 30 * time.Minute,
	}
}

// NewClient creates a new beads database client.
func NewClient(ctx context.Context, cfg ClientConfig) (*Client, error) {
	// Open database in read-only mode
	db, err := sql.Open("sqlite3", "file:"+cfg.DBPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("opening beads database: %w", err)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging beads database: %w", err)
	}

	var cache cachemanager.CacheManager[string, *IssuesWithDepsResult]
	if cfg.CacheEnabled {
		cache = cachemanager.NewInMemoryCacheManager[string, *IssuesWithDepsResult](
			"beads-issues",
			cfg.CacheExpiration,
			cfg.CacheCleanupTime,
		)
	}

	return &Client{
		db:           db,
		queries:      queries.New(db),
		cache:        cache,
		dbPath:       cfg.DBPath,
		cacheEnabled: cfg.CacheEnabled,
	}, nil
}

// Close closes the database connection.
func (c *Client) Close() error {
	return c.db.Close()
}

// FlushCache flushes the cache.
func (c *Client) FlushCache(ctx context.Context) error {
	if c.cache == nil {
		return nil
	}
	return c.cache.Flush(ctx)
}

// GetIssuesWithDeps retrieves issues and their dependencies/dependents.
// Results are cached based on sorted issue IDs.
func (c *Client) GetIssuesWithDeps(ctx context.Context, issueIDs []string) (*IssuesWithDepsResult, error) {
	if len(issueIDs) == 0 {
		return &IssuesWithDepsResult{
			Issues:       make(map[string]queries.Issue),
			Dependencies: make(map[string][]queries.GetDependenciesForIssuesRow),
			Dependents:   make(map[string][]queries.GetDependentsForIssuesRow),
		}, nil
	}

	// Create cache key from sorted issue IDs
	sortedIDs := make([]string, len(issueIDs))
	copy(sortedIDs, issueIDs)
	sort.Strings(sortedIDs)
	cacheKey := strings.Join(sortedIDs, ",")

	// Check cache
	if c.cacheEnabled && c.cache != nil {
		if cached, found := c.cache.Get(ctx, cacheKey); found {
			return cached, nil
		}
	}

	// Fetch issues
	issues, err := c.queries.GetIssuesByIDs(ctx, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("fetching issues: %w", err)
	}

	// Build issues map
	issuesMap := make(map[string]queries.Issue, len(issues))
	for _, issue := range issues {
		issuesMap[issue.ID] = issue
	}

	// Fetch dependencies
	deps, err := c.queries.GetDependenciesForIssues(ctx, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("fetching dependencies: %w", err)
	}

	// Build dependencies map
	depsMap := make(map[string][]queries.GetDependenciesForIssuesRow)
	for _, dep := range deps {
		depsMap[dep.IssueID] = append(depsMap[dep.IssueID], dep)
	}

	// Fetch dependents
	dependents, err := c.queries.GetDependentsForIssues(ctx, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("fetching dependents: %w", err)
	}

	// Build dependents map
	dependentsMap := make(map[string][]queries.GetDependentsForIssuesRow)
	for _, dependent := range dependents {
		dependentsMap[dependent.DependsOnID] = append(dependentsMap[dependent.DependsOnID], dependent)
	}

	result := &IssuesWithDepsResult{
		Issues:       issuesMap,
		Dependencies: depsMap,
		Dependents:   dependentsMap,
	}

	// Cache result
	if c.cacheEnabled && c.cache != nil {
		c.cache.Set(ctx, cacheKey, result, cachemanager.DefaultExpiration)
	}

	return result, nil
}
