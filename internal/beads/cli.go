package beads

import (
	"context"
)

// CLI defines the interface for bd command operations.
// This abstraction enables testing without actual bd CLI calls.
type CLI interface {
	// Init initializes beads in the specified directory.
	Init(ctx context.Context, beadsDir, prefix string) error
	// InstallHooks installs beads hooks in the specified directory.
	InstallHooks(ctx context.Context, repoDir string) error
	// Create creates a new bead and returns its ID.
	Create(ctx context.Context, beadsDir string, opts CreateOptions) (string, error)
	// Close closes a bead.
	Close(ctx context.Context, beadID, beadsDir string) error
	// Reopen reopens a closed bead.
	Reopen(ctx context.Context, beadID, beadsDir string) error
	// Update updates a bead's fields.
	Update(ctx context.Context, beadID, beadsDir string, opts UpdateOptions) error
	// AddComment adds a comment to a bead.
	AddComment(ctx context.Context, beadID, comment, beadsDir string) error
	// AddLabels adds labels to a bead.
	AddLabels(ctx context.Context, beadID, beadsDir string, labels []string) error
	// SetExternalRef sets the external reference for a bead.
	SetExternalRef(ctx context.Context, beadID, externalRef, beadsDir string) error
	// AddDependency adds a dependency between two beads.
	AddDependency(ctx context.Context, beadID, dependsOnID, beadsDir string) error
}

// Reader defines the interface for reading beads from the database.
// This abstraction enables testing without actual database access.
type Reader interface {
	// GetBead retrieves a single bead by ID with its dependencies/dependents.
	GetBead(ctx context.Context, id string) (*BeadWithDeps, error)
	// GetBeadsWithDeps retrieves beads and their dependencies/dependents.
	GetBeadsWithDeps(ctx context.Context, beadIDs []string) (*BeadsWithDepsResult, error)
	// ListBeads lists all beads with optional status filter.
	ListBeads(ctx context.Context, status string) ([]Bead, error)
	// GetReadyBeads returns all open beads where all dependencies are satisfied.
	GetReadyBeads(ctx context.Context) ([]Bead, error)
	// GetTransitiveDependencies collects all transitive dependencies for a bead.
	GetTransitiveDependencies(ctx context.Context, id string) ([]Bead, error)
	// GetBeadWithChildren retrieves a bead and all its child beads recursively.
	GetBeadWithChildren(ctx context.Context, id string) ([]Bead, error)
}

// cliImpl implements CLI using the bd command-line tool.
type cliImpl struct{}

// Compile-time check that cliImpl implements CLI.
var _ CLI = (*cliImpl)(nil)

// DefaultCLI is the default CLI implementation using the bd command.
var DefaultCLI CLI = &cliImpl{}

// Init implements CLI.Init.
func (c *cliImpl) Init(ctx context.Context, beadsDir, prefix string) error {
	return Init(ctx, beadsDir, prefix)
}

// InstallHooks implements CLI.InstallHooks.
func (c *cliImpl) InstallHooks(ctx context.Context, repoDir string) error {
	return InstallHooks(ctx, repoDir)
}

// Create implements CLI.Create.
func (c *cliImpl) Create(ctx context.Context, beadsDir string, opts CreateOptions) (string, error) {
	return Create(ctx, beadsDir, opts)
}

// Close implements CLI.Close.
func (c *cliImpl) Close(ctx context.Context, beadID, beadsDir string) error {
	return Close(ctx, beadID, beadsDir)
}

// Reopen implements CLI.Reopen.
func (c *cliImpl) Reopen(ctx context.Context, beadID, beadsDir string) error {
	return Reopen(ctx, beadID, beadsDir)
}

// Update implements CLI.Update.
func (c *cliImpl) Update(ctx context.Context, beadID, beadsDir string, opts UpdateOptions) error {
	return Update(ctx, beadID, beadsDir, opts)
}

// AddComment implements CLI.AddComment.
func (c *cliImpl) AddComment(ctx context.Context, beadID, comment, beadsDir string) error {
	return AddComment(ctx, beadID, comment, beadsDir)
}

// AddLabels implements CLI.AddLabels.
func (c *cliImpl) AddLabels(ctx context.Context, beadID, beadsDir string, labels []string) error {
	return AddLabels(ctx, beadID, beadsDir, labels)
}

// SetExternalRef implements CLI.SetExternalRef.
func (c *cliImpl) SetExternalRef(ctx context.Context, beadID, externalRef, beadsDir string) error {
	return SetExternalRef(ctx, beadID, externalRef, beadsDir)
}

// AddDependency implements CLI.AddDependency.
func (c *cliImpl) AddDependency(ctx context.Context, beadID, dependsOnID, beadsDir string) error {
	return AddDependency(ctx, beadID, dependsOnID, beadsDir)
}

// Compile-time check that Client implements Reader.
var _ Reader = (*Client)(nil)
