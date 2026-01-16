package linear

import (
	"context"
	"fmt"
	"strings"

	"github.com/newhook/co/internal/beads"
)

// Fetcher orchestrates fetching Linear issues and importing them into Beads
type Fetcher struct {
	client     *Client
	beadsDir   string
	beadsCache map[string]string // linearID -> beadID cache
}

// NewFetcher creates a new fetcher with the given API key and beads directory
func NewFetcher(apiKey string, beadsDir string) (*Fetcher, error) {
	client, err := NewClient(apiKey)
	if err != nil {
		return nil, err
	}

	return &Fetcher{
		client:     client,
		beadsDir:   beadsDir,
		beadsCache: make(map[string]string),
	}, nil
}

// FetchAndImport fetches a Linear issue and imports it into Beads
// Returns the created bead ID and any error
func (f *Fetcher) FetchAndImport(ctx context.Context, linearIDOrURL string, opts *ImportOptions) (*ImportResult, error) {
	result := &ImportResult{
		Success: false,
	}

	// Parse and normalize the Linear ID
	linearID, err := ParseIssueIDOrURL(linearIDOrURL)
	if err != nil {
		result.Error = fmt.Errorf("invalid Linear ID or URL: %w", err)
		return result, result.Error
	}
	result.LinearID = linearID

	// Check if already imported (cached)
	if beadID, exists := f.beadsCache[linearID]; exists {
		result.BeadID = beadID
		result.Success = true
		result.SkipReason = "already imported (cached)"
		return result, nil
	}

	// Fetch the issue from Linear
	issue, err := f.client.GetIssue(ctx, linearID)
	if err != nil {
		result.Error = fmt.Errorf("failed to fetch issue from Linear: %w", err)
		return result, result.Error
	}
	result.LinearURL = issue.URL

	// Check if already imported by querying beads
	beadID, err := f.findExistingBead(ctx, linearID)
	if err != nil {
		result.Error = fmt.Errorf("failed to check for existing bead: %w", err)
		return result, result.Error
	}
	if beadID != "" {
		result.BeadID = beadID
		result.Success = true
		result.SkipReason = "already imported"
		f.beadsCache[linearID] = beadID
		return result, nil
	}

	// Apply filters if specified
	if opts != nil {
		if opts.StatusFilter != "" && MapStatus(issue.State) != opts.StatusFilter {
			result.SkipReason = fmt.Sprintf("filtered out by status (wanted: %s, got: %s)", opts.StatusFilter, MapStatus(issue.State))
			return result, nil
		}
		if opts.PriorityFilter != "" && MapPriority(issue.Priority) != opts.PriorityFilter {
			result.SkipReason = fmt.Sprintf("filtered out by priority (wanted: %s, got: %s)", opts.PriorityFilter, MapPriority(issue.Priority))
			return result, nil
		}
		if opts.AssigneeFilter != "" && (issue.Assignee == nil || issue.Assignee.Email != opts.AssigneeFilter) {
			result.SkipReason = "filtered out by assignee"
			return result, nil
		}
	}

	// Map Linear issue to Beads creation options
	beadOpts := MapIssueToBeadCreate(issue)

	// Override type if specified in options
	if opts != nil && opts.TypeFilter != "" {
		beadOpts.Type = opts.TypeFilter
	}

	// Format description with Linear metadata
	beadOpts.Description = FormatBeadDescription(issue)

	// Dry run: skip actual creation
	if opts != nil && opts.DryRun {
		result.Success = true
		result.SkipReason = "dry run"
		return result, nil
	}

	// Create the bead
	createdBeadID, err := f.createBead(ctx, beadOpts)
	if err != nil {
		result.Error = fmt.Errorf("failed to create bead: %w", err)
		return result, result.Error
	}

	result.BeadID = createdBeadID
	result.Success = true
	f.beadsCache[linearID] = createdBeadID

	// Handle dependencies if requested
	if opts != nil && opts.CreateDeps && len(issue.BlockedBy) > 0 {
		if err := f.createDependencies(ctx, createdBeadID, issue.BlockedBy, opts); err != nil {
			// Log but don't fail - the main import succeeded
			result.Error = fmt.Errorf("warning: failed to create dependencies: %w", err)
		}
	}

	return result, nil
}

// FetchBatch fetches and imports multiple Linear issues
func (f *Fetcher) FetchBatch(ctx context.Context, linearIDsOrURLs []string, opts *ImportOptions) ([]*ImportResult, error) {
	results := make([]*ImportResult, 0, len(linearIDsOrURLs))

	for _, idOrURL := range linearIDsOrURLs {
		result, err := f.FetchAndImport(ctx, idOrURL, opts)
		if err != nil {
			// Continue with other imports even if one fails
			result.Error = err
		}
		results = append(results, result)

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}
	}

	return results, nil
}

// findExistingBead checks if a bead already exists for the given Linear ID
func (f *Fetcher) findExistingBead(ctx context.Context, linearID string) (string, error) {
	// List all beads and search for linear_id in metadata or description
	// This is a simple implementation; a more efficient approach would use
	// a metadata index in beads if available
	beadsList, err := beads.ListBeads(ctx, f.beadsDir, beads.ListFilters{})
	if err != nil {
		return "", err
	}

	for _, bead := range beadsList {
		// Check if the bead's description or metadata contains the Linear ID
		if strings.Contains(bead.Description, linearID) ||
			strings.Contains(bead.Description, "linear.app/") && strings.Contains(bead.Description, linearID) {
			return bead.ID, nil
		}
	}

	return "", nil
}

// createBead creates a bead using the beads client
func (f *Fetcher) createBead(ctx context.Context, opts *BeadCreateOptions) (string, error) {
	// Convert priority string (P0-P4) to int (0-4)
	priority := 2 // default to medium
	if len(opts.Priority) >= 2 && opts.Priority[0] == 'P' {
		switch opts.Priority[1] {
		case '0':
			priority = 0
		case '1':
			priority = 1
		case '2':
			priority = 2
		case '3':
			priority = 3
		case '4':
			priority = 4
		}
	}

	createOpts := beads.CreateOptions{
		Title:       opts.Title,
		Description: opts.Description,
		Type:        opts.Type,
		Priority:    priority,
	}

	beadID, err := beads.Create(ctx, f.beadsDir, createOpts)
	if err != nil {
		return "", err
	}

	// Note: Assignee, Labels, and custom metadata are not yet supported in beads.CreateOptions
	// These would need to be added via separate update commands in a future enhancement

	return beadID, nil
}

// createDependencies creates dependency relationships for imported beads
func (f *Fetcher) createDependencies(ctx context.Context, beadID string, blockedByIDs []string, opts *ImportOptions) error {
	depth := 1
	if opts.MaxDepDepth > 0 && depth > opts.MaxDepDepth {
		return nil
	}

	for _, blockedByID := range blockedByIDs {
		// Fetch and import the blocking issue if not already imported
		result, err := f.FetchAndImport(ctx, blockedByID, &ImportOptions{
			DryRun:      opts.DryRun,
			CreateDeps:  true,
			MaxDepDepth: opts.MaxDepDepth,
		})
		if err != nil {
			return fmt.Errorf("failed to import blocking issue %s: %w", blockedByID, err)
		}

		if result.BeadID == "" {
			continue
		}

		// Create the dependency relationship
		// beadID depends on result.BeadID
		if err := beads.AddDependency(ctx, beadID, result.BeadID, f.beadsDir); err != nil {
			return fmt.Errorf("failed to add dependency %s -> %s: %w", beadID, result.BeadID, err)
		}
	}

	return nil
}
