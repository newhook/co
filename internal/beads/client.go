package beads

import (
	"context"
	"database/sql"
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

// GetIssue retrieves a single issue by ID with its dependencies/dependents.
// Returns nil if the issue is not found.
func (c *Client) GetIssue(ctx context.Context, id string) (*queries.Issue, []queries.GetDependenciesForIssuesRow, []queries.GetDependentsForIssuesRow, error) {
	result, err := c.GetIssuesWithDeps(ctx, []string{id})
	if err != nil {
		return nil, nil, nil, err
	}

	issue, found := result.Issues[id]
	if !found {
		return nil, nil, nil, nil
	}

	return &issue, result.Dependencies[id], result.Dependents[id], nil
}

// ListIssues lists all issues with optional status filter.
// Pass empty string for status to get all issues.
func (c *Client) ListIssues(ctx context.Context, status string) ([]queries.Issue, error) {
	var ids []string
	var err error

	if status == "" {
		ids, err = c.queries.GetAllIssueIDs(ctx)
	} else {
		ids, err = c.queries.GetIssueIDsByStatus(ctx, status)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching issue IDs: %w", err)
	}

	if len(ids) == 0 {
		return []queries.Issue{}, nil
	}

	result, err := c.GetIssuesWithDeps(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Convert map to slice
	issues := make([]queries.Issue, 0, len(result.Issues))
	for _, issue := range result.Issues {
		issues = append(issues, issue)
	}

	return issues, nil
}

// GetReadyIssues returns all open issues where all dependencies are satisfied.
func (c *Client) GetReadyIssues(ctx context.Context) ([]queries.Issue, error) {
	// Get all open issues
	ids, err := c.queries.GetIssueIDsByStatus(ctx, "open")
	if err != nil {
		return nil, fmt.Errorf("fetching open issue IDs: %w", err)
	}

	if len(ids) == 0 {
		return []queries.Issue{}, nil
	}

	result, err := c.GetIssuesWithDeps(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Filter to issues where all dependencies are closed
	var ready []queries.Issue
	for _, id := range ids {
		issue, ok := result.Issues[id]
		if !ok {
			continue
		}

		// Check if all blocking dependencies are satisfied
		isReady := true
		for _, dep := range result.Dependencies[id] {
			if dep.Type == "blocks" && dep.Status != "closed" {
				isReady = false
				break
			}
		}

		if isReady {
			ready = append(ready, issue)
		}
	}

	return ready, nil
}

// GetTransitiveDependencies collects all transitive dependencies for an issue.
// Returns issues in dependency order (dependencies before dependents).
func (c *Client) GetTransitiveDependencies(ctx context.Context, id string) ([]queries.Issue, error) {
	visited := make(map[string]bool)
	var orderedIDs []string

	// Recursive helper to collect dependency IDs
	var collect func(issueID string) error
	collect = func(issueID string) error {
		if visited[issueID] {
			return nil
		}
		visited[issueID] = true

		// Get this issue to find its dependencies
		result, err := c.GetIssuesWithDeps(ctx, []string{issueID})
		if err != nil {
			return err
		}

		// Recursively collect all blocked_by dependencies first
		for _, dep := range result.Dependencies[issueID] {
			if dep.Type == "blocked_by" && !visited[dep.DependsOnID] {
				if err := collect(dep.DependsOnID); err != nil {
					return err
				}
			}
		}

		// Add this issue after its dependencies
		orderedIDs = append(orderedIDs, issueID)
		return nil
	}

	if err := collect(id); err != nil {
		return nil, err
	}

	// Fetch all collected issues in one call
	result, err := c.GetIssuesWithDeps(ctx, orderedIDs)
	if err != nil {
		return nil, err
	}

	// Return in dependency order
	issues := make([]queries.Issue, 0, len(orderedIDs))
	for _, issueID := range orderedIDs {
		if issue, ok := result.Issues[issueID]; ok {
			issues = append(issues, issue)
		}
	}

	return issues, nil
}

// GetIssueWithChildren retrieves an issue and all its child issues recursively.
// This is useful for epic issues that have sub-issues (parent-child relationship).
func (c *Client) GetIssueWithChildren(ctx context.Context, id string) ([]queries.Issue, error) {
	visited := make(map[string]bool)
	var orderedIDs []string

	// Recursive helper to collect issue and children IDs
	var collect func(issueID string) error
	collect = func(issueID string) error {
		if visited[issueID] {
			return nil
		}
		visited[issueID] = true

		// Add this issue first
		orderedIDs = append(orderedIDs, issueID)

		// Get this issue to find its children
		result, err := c.GetIssuesWithDeps(ctx, []string{issueID})
		if err != nil {
			return err
		}

		// Recursively collect all parent-child dependents
		for _, dep := range result.Dependents[issueID] {
			if dep.Type == "parent-child" && !visited[dep.IssueID] {
				if err := collect(dep.IssueID); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := collect(id); err != nil {
		return nil, err
	}

	// Fetch all collected issues in one call
	result, err := c.GetIssuesWithDeps(ctx, orderedIDs)
	if err != nil {
		return nil, err
	}

	// Return in order (parent before children)
	issues := make([]queries.Issue, 0, len(orderedIDs))
	for _, issueID := range orderedIDs {
		if issue, ok := result.Issues[issueID]; ok {
			issues = append(issues, issue)
		}
	}

	return issues, nil
}
