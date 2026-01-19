package beads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/newhook/co/internal/beads/cachemanager"
	"github.com/newhook/co/internal/beads/queries"
	"github.com/newhook/co/internal/logging"
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
	Title        string
	Type         string   // "task", "bug", "feature"
	Priority     int
	IsEpic       bool
	Description  string
	Parent       string   // Parent bead ID for hierarchical child
	Labels       []string // Optional labels for the bead
	ExternalRef  string   // Optional external reference (e.g., GitHub comment ID)
}

// Create creates a new bead and returns its ID.
func Create(ctx context.Context, dir string, opts CreateOptions) (string, error) {
	// Determine the type - if IsEpic is set, override type to "epic"
	beadType := opts.Type
	if opts.IsEpic {
		beadType = "epic"
	}
	args := []string{"create", "--title=" + opts.Title, "--type=" + beadType, fmt.Sprintf("--priority=%d", opts.Priority)}
	if opts.Description != "" {
		args = append(args, "--description="+opts.Description)
	}
	if opts.Parent != "" {
		args = append(args, "--parent="+opts.Parent)
	}
	if opts.ExternalRef != "" {
		args = append(args, "--external-ref="+opts.ExternalRef)
	}
	for _, label := range opts.Labels {
		if label != "" {
			args = append(args, "--label="+label)
		}
	}

	logging.Debug("creating bead", "args", args, "dir", dir, "opts", opts)

	cmd := exec.CommandContext(ctx, "bd", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			logging.Error("bd create failed", "error", err, "stderr", string(exitErr.Stderr), "args", args)
			return "", fmt.Errorf("failed to create bead: %w\n%s", err, exitErr.Stderr)
		}
		logging.Error("bd create failed", "error", err, "args", args)
		return "", fmt.Errorf("failed to create bead: %w", err)
	}

	logging.Debug("bd create output", "output", string(output))

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
		logging.Error("failed to parse bead ID from output", "output", string(output), "args", args)
		return "", fmt.Errorf("failed to get created bead ID from output: %s", output)
	}

	logging.Debug("created bead", "beadID", beadID)
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

// AddComment adds a comment to a bead.
func AddComment(ctx context.Context, beadID, comment, dir string) error {
	cmd := exec.CommandContext(ctx, "bd", "comments", "add", beadID, comment)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add comment to bead %s: %w\n%s", beadID, err, output)
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
	Assignee    string
	Priority    *int // nil means don't update
	Status      string
}

// Update updates a bead's fields.
func Update(ctx context.Context, beadID, dir string, opts UpdateOptions) error {
	args := []string{"update", beadID}
	if opts.Title != "" {
		args = append(args, "--title="+opts.Title)
	}
	if opts.Type != "" {
		args = append(args, "--type="+opts.Type)
	}
	if opts.Description != "" {
		args = append(args, "--description="+opts.Description)
	}
	if opts.Assignee != "" {
		args = append(args, "--assignee="+opts.Assignee)
	}
	if opts.Priority != nil {
		args = append(args, fmt.Sprintf("--priority=%d", *opts.Priority))
	}
	if opts.Status != "" {
		args = append(args, "--status="+opts.Status)
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

// AddLabels adds labels to a bead.
func AddLabels(ctx context.Context, beadID, dir string, labels []string) error {
	if len(labels) == 0 {
		return nil
	}

	args := []string{"update", beadID}
	for _, label := range labels {
		args = append(args, "--add-label="+label)
	}

	cmd := exec.CommandContext(ctx, "bd", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add labels to bead %s: %w\n%s", beadID, err, output)
	}
	return nil
}

// SetExternalRef sets the external reference for a bead.
func SetExternalRef(ctx context.Context, beadID, externalRef, dir string) error {
	if externalRef == "" {
		return nil
	}

	args := []string{"update", beadID, "--external-ref=" + externalRef}

	cmd := exec.CommandContext(ctx, "bd", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set external ref for bead %s: %w\n%s", beadID, err, output)
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
func EditCommand(ctx context.Context, beadID, dir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "bd", "edit", beadID)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd
}

// Client provides database access to beads with caching.
type Client struct {
	db           *sql.DB
	queries      *queries.Queries
	cache        cachemanager.CacheManager[string, *BeadsWithDepsResult]
	dbPath       string
	cacheEnabled bool
}

// Dependency represents a dependency relationship between beads.
type Dependency struct {
	IssueID     string
	DependsOnID string
	Type        string // "blocks", "blocked_by", "parent-child", "relates-to"
	Status      string // status of the depended-on issue
	Title       string // title of the depended-on issue
}

// Dependent represents a bead that depends on another bead.
type Dependent struct {
	IssueID     string // the issue that depends on us
	DependsOnID string
	Type        string
	Status      string // status of the dependent issue
	Title       string // title of the dependent issue
}

// BeadWithDeps bundles a bead with its dependencies and dependents.
type BeadWithDeps struct {
	*Bead
	Dependencies []Dependency
	Dependents   []Dependent
}

// BeadsWithDepsResult holds the result of GetBeadsWithDeps.
type BeadsWithDepsResult struct {
	Beads        map[string]Bead
	Dependencies map[string][]Dependency
	Dependents   map[string][]Dependent
}

// GetBead returns a single BeadWithDeps from the result, or nil if not found.
func (r *BeadsWithDepsResult) GetBead(id string) *BeadWithDeps {
	bead, ok := r.Beads[id]
	if !ok {
		return nil
	}
	return &BeadWithDeps{
		Bead:         &bead,
		Dependencies: r.Dependencies[id],
		Dependents:   r.Dependents[id],
	}
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

	var cache cachemanager.CacheManager[string, *BeadsWithDepsResult]
	if cfg.CacheEnabled {
		cache = cachemanager.NewInMemoryCacheManager[string, *BeadsWithDepsResult](
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

// GetBeadsWithDeps retrieves beads and their dependencies/dependents.
// Results are cached based on sorted bead IDs.
func (c *Client) GetBeadsWithDeps(ctx context.Context, beadIDs []string) (*BeadsWithDepsResult, error) {
	if len(beadIDs) == 0 {
		return &BeadsWithDepsResult{
			Beads:        make(map[string]Bead),
			Dependencies: make(map[string][]Dependency),
			Dependents:   make(map[string][]Dependent),
		}, nil
	}

	// Create cache key from sorted bead IDs
	sortedIDs := make([]string, len(beadIDs))
	copy(sortedIDs, beadIDs)
	sort.Strings(sortedIDs)
	cacheKey := strings.Join(sortedIDs, ",")

	// Check cache
	if c.cacheEnabled && c.cache != nil {
		if cached, found := c.cache.Get(ctx, cacheKey); found {
			return cached, nil
		}
	}

	// Fetch issues from database
	issues, err := c.queries.GetIssuesByIDs(ctx, beadIDs)
	if err != nil {
		return nil, fmt.Errorf("fetching beads: %w", err)
	}

	// Build beads map
	beadsMap := make(map[string]Bead, len(issues))
	for _, issue := range issues {
		beadsMap[issue.ID] = BeadFromIssue(issue)
	}

	// Fetch dependencies
	deps, err := c.queries.GetDependenciesForIssues(ctx, beadIDs)
	if err != nil {
		return nil, fmt.Errorf("fetching dependencies: %w", err)
	}

	// Build dependencies map with clean types
	depsMap := make(map[string][]Dependency)
	for _, dep := range deps {
		depsMap[dep.IssueID] = append(depsMap[dep.IssueID], Dependency{
			IssueID:     dep.IssueID,
			DependsOnID: dep.DependsOnID,
			Type:        dep.Type,
			Status:      dep.Status,
			Title:       dep.Title,
		})
	}

	// Fetch dependents
	dependents, err := c.queries.GetDependentsForIssues(ctx, beadIDs)
	if err != nil {
		return nil, fmt.Errorf("fetching dependents: %w", err)
	}

	// Build dependents map with clean types
	dependentsMap := make(map[string][]Dependent)
	for _, dep := range dependents {
		dependentsMap[dep.DependsOnID] = append(dependentsMap[dep.DependsOnID], Dependent{
			IssueID:     dep.IssueID,
			DependsOnID: dep.DependsOnID,
			Type:        dep.Type,
			Status:      dep.Status,
			Title:       dep.Title,
		})
	}

	result := &BeadsWithDepsResult{
		Beads:        beadsMap,
		Dependencies: depsMap,
		Dependents:   dependentsMap,
	}

	// Cache result
	if c.cacheEnabled && c.cache != nil {
		c.cache.Set(ctx, cacheKey, result, cachemanager.DefaultExpiration)
	}

	return result, nil
}

// GetBead retrieves a single bead by ID with its dependencies/dependents.
// Returns nil if the bead is not found.
func (c *Client) GetBead(ctx context.Context, id string) (*BeadWithDeps, error) {
	result, err := c.GetBeadsWithDeps(ctx, []string{id})
	if err != nil {
		return nil, err
	}

	return result.GetBead(id), nil
}

// ListBeads lists all beads with optional status filter.
// Pass empty string for status to get all beads.
func (c *Client) ListBeads(ctx context.Context, status string) ([]Bead, error) {
	var ids []string
	var err error

	if status == "" {
		ids, err = c.queries.GetAllIssueIDs(ctx)
	} else {
		ids, err = c.queries.GetIssueIDsByStatus(ctx, status)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching bead IDs: %w", err)
	}

	if len(ids) == 0 {
		return []Bead{}, nil
	}

	result, err := c.GetBeadsWithDeps(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Convert map to slice
	beads := make([]Bead, 0, len(result.Beads))
	for _, bead := range result.Beads {
		beads = append(beads, bead)
	}

	return beads, nil
}

// GetReadyBeads returns all open beads where all dependencies are satisfied.
func (c *Client) GetReadyBeads(ctx context.Context) ([]Bead, error) {
	// Get all open beads
	ids, err := c.queries.GetIssueIDsByStatus(ctx, "open")
	if err != nil {
		return nil, fmt.Errorf("fetching open bead IDs: %w", err)
	}

	if len(ids) == 0 {
		return []Bead{}, nil
	}

	result, err := c.GetBeadsWithDeps(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Filter to beads where all dependencies are closed
	var ready []Bead
	for _, id := range ids {
		bead, ok := result.Beads[id]
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
			ready = append(ready, bead)
		}
	}

	return ready, nil
}

// GetTransitiveDependencies collects all transitive dependencies for a bead.
// Returns beads in dependency order (dependencies before dependents).
func (c *Client) GetTransitiveDependencies(ctx context.Context, id string) ([]Bead, error) {
	visited := make(map[string]bool)
	var orderedIDs []string

	// Recursive helper to collect dependency IDs
	var collect func(beadID string) error
	collect = func(beadID string) error {
		if visited[beadID] {
			return nil
		}
		visited[beadID] = true

		// Get this bead to find its dependencies
		result, err := c.GetBeadsWithDeps(ctx, []string{beadID})
		if err != nil {
			return err
		}

		// Recursively collect all blocked_by dependencies first
		for _, dep := range result.Dependencies[beadID] {
			if dep.Type == "blocked_by" && !visited[dep.DependsOnID] {
				if err := collect(dep.DependsOnID); err != nil {
					return err
				}
			}
		}

		// Add this bead after its dependencies
		orderedIDs = append(orderedIDs, beadID)
		return nil
	}

	if err := collect(id); err != nil {
		return nil, err
	}

	// Fetch all collected beads in one call
	result, err := c.GetBeadsWithDeps(ctx, orderedIDs)
	if err != nil {
		return nil, err
	}

	// Return in dependency order
	beads := make([]Bead, 0, len(orderedIDs))
	for _, beadID := range orderedIDs {
		if bead, ok := result.Beads[beadID]; ok {
			beads = append(beads, bead)
		}
	}

	return beads, nil
}

// GetBeadWithChildren retrieves a bead and all its child beads recursively.
// This is useful for epic beads that have sub-beads (parent-child relationship).
func (c *Client) GetBeadWithChildren(ctx context.Context, id string) ([]Bead, error) {
	visited := make(map[string]bool)
	var orderedIDs []string

	// Recursive helper to collect bead and children IDs
	var collect func(beadID string) error
	collect = func(beadID string) error {
		if visited[beadID] {
			return nil
		}
		visited[beadID] = true

		// Add this bead first
		orderedIDs = append(orderedIDs, beadID)

		// Get this bead to find its children
		result, err := c.GetBeadsWithDeps(ctx, []string{beadID})
		if err != nil {
			return err
		}

		// Recursively collect all parent-child dependents
		for _, dep := range result.Dependents[beadID] {
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

	// Fetch all collected beads in one call
	result, err := c.GetBeadsWithDeps(ctx, orderedIDs)
	if err != nil {
		return nil, err
	}

	// Return in order (parent before children)
	beads := make([]Bead, 0, len(orderedIDs))
	for _, beadID := range orderedIDs {
		if bead, ok := result.Beads[beadID]; ok {
			beads = append(beads, bead)
		}
	}

	return beads, nil
}
