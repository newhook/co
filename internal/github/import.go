package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/worktree"
)

// PRImporter handles importing PRs into work units.
type PRImporter struct {
	client     ClientInterface
	gitOps     git.Operations
	worktreeOps worktree.Operations
}

// NewPRImporter creates a new PR importer with default operations.
func NewPRImporter(client ClientInterface) *PRImporter {
	return &PRImporter{
		client:      client,
		gitOps:      git.NewOperations(),
		worktreeOps: worktree.NewOperations(),
	}
}

// NewPRImporterWithOps creates a new PR importer with custom operations (for testing).
func NewPRImporterWithOps(client ClientInterface, gitOps git.Operations, worktreeOps worktree.Operations) *PRImporter {
	return &PRImporter{
		client:      client,
		gitOps:      gitOps,
		worktreeOps: worktreeOps,
	}
}

// SetupWorktreeFromPR fetches a PR's branch and creates a worktree for it.
// It returns the created worktree path and the PR metadata.
//
// Parameters:
//   - repoPath: Path to the main repository
//   - prURLOrNumber: PR URL or number
//   - repo: Repository in owner/repo format (only needed if prURLOrNumber is a number)
//   - workDir: Directory where the worktree should be created (worktree will be at workDir/tree)
//   - branchName: Name to use for the local branch (if empty, uses the PR's branch name)
//
// The function:
// 1. Fetches PR metadata to get branch information
// 2. Fetches the PR's head ref using GitHub's refs/pull/<n>/head
// 3. Creates a worktree at workDir/tree from the fetched branch
func (p *PRImporter) SetupWorktreeFromPR(ctx context.Context, repoPath, prURLOrNumber, repo, workDir, branchName string) (*PRMetadata, string, error) {
	logging.Info("setting up worktree from PR",
		"repoPath", repoPath,
		"prURLOrNumber", prURLOrNumber,
		"repo", repo,
		"workDir", workDir,
		"branchName", branchName)

	// Fetch PR metadata
	metadata, err := p.client.GetPRMetadata(ctx, prURLOrNumber, repo)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get PR metadata: %w", err)
	}

	// Determine the local branch name
	localBranch := branchName
	if localBranch == "" {
		localBranch = metadata.HeadRefName
	}

	// Fetch the PR's head ref
	logging.Debug("fetching PR ref", "prNumber", metadata.Number, "localBranch", localBranch)
	if err := p.gitOps.FetchPRRef(ctx, repoPath, metadata.Number, localBranch); err != nil {
		return metadata, "", fmt.Errorf("failed to fetch PR ref: %w", err)
	}

	// Create the worktree directory path
	worktreePath := filepath.Join(workDir, "tree")

	// Create worktree from the fetched branch
	logging.Debug("creating worktree", "worktreePath", worktreePath, "branch", localBranch)
	if err := p.worktreeOps.CreateFromExisting(ctx, repoPath, worktreePath, localBranch); err != nil {
		return metadata, "", fmt.Errorf("failed to create worktree: %w", err)
	}

	logging.Info("successfully set up worktree from PR",
		"prNumber", metadata.Number,
		"worktreePath", worktreePath,
		"branch", localBranch)

	return metadata, worktreePath, nil
}

// FetchPRMetadata is a convenience method that just fetches PR metadata without creating a worktree.
func (p *PRImporter) FetchPRMetadata(ctx context.Context, prURLOrNumber, repo string) (*PRMetadata, error) {
	return p.client.GetPRMetadata(ctx, prURLOrNumber, repo)
}

// CreateBeadOptions contains options for creating a bead from a PR.
type CreateBeadOptions struct {
	// BeadsDir is the directory containing the beads database.
	BeadsDir string
	// SkipIfExists skips creation if a bead with the same PR URL already exists.
	SkipIfExists bool
	// OverrideTitle allows overriding the PR title.
	OverrideTitle string
	// OverrideType allows overriding the inferred type.
	OverrideType string
	// OverridePriority allows overriding the inferred priority.
	OverridePriority string
}

// CreateBeadResult contains the result of creating a bead from a PR.
type CreateBeadResult struct {
	BeadID     string
	Created    bool
	SkipReason string
}

// CreateBeadFromPR creates a bead from PR metadata.
// This allows users to optionally track imported PRs in the beads system.
func (p *PRImporter) CreateBeadFromPR(ctx context.Context, metadata *PRMetadata, opts *CreateBeadOptions) (*CreateBeadResult, error) {
	logging.Info("creating bead from PR",
		"prNumber", metadata.Number,
		"prTitle", metadata.Title,
		"beadsDir", opts.BeadsDir)

	result := &CreateBeadResult{}

	// Check for existing bead if requested
	if opts.SkipIfExists {
		existingID, err := findExistingPRBead(ctx, opts.BeadsDir, metadata.URL)
		if err != nil {
			logging.Warn("failed to check for existing bead", "error", err)
			// Continue anyway - we'll try to create
		} else if existingID != "" {
			result.BeadID = existingID
			result.Created = false
			result.SkipReason = "bead already exists for this PR"
			logging.Info("found existing bead for PR", "beadID", existingID)
			return result, nil
		}
	}

	// Map PR to bead options
	beadOpts := MapPRToBeadCreate(metadata)

	// Apply overrides
	if opts.OverrideTitle != "" {
		beadOpts.Title = opts.OverrideTitle
	}
	if opts.OverrideType != "" {
		beadOpts.Type = opts.OverrideType
	}
	if opts.OverridePriority != "" {
		beadOpts.Priority = opts.OverridePriority
	}

	// Format description with PR metadata
	beadOpts.Description = FormatBeadDescription(metadata)

	// Convert priority string (P0-P4) to int (0-4)
	priority := parsePriority(beadOpts.Priority)

	// Create the bead
	createOpts := beads.CreateOptions{
		Title:       beadOpts.Title,
		Description: beadOpts.Description,
		Type:        beadOpts.Type,
		Priority:    priority,
	}

	beadID, err := beads.Create(ctx, opts.BeadsDir, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create bead: %w", err)
	}

	// Set external reference to PR URL for deduplication
	if err := beads.SetExternalRef(ctx, beadID, metadata.URL, opts.BeadsDir); err != nil {
		logging.Warn("failed to set external ref on bead", "error", err, "beadID", beadID)
		// Continue - bead was created successfully
	}

	// Add labels if present
	if len(beadOpts.Labels) > 0 {
		if err := beads.AddLabels(ctx, beadID, opts.BeadsDir, beadOpts.Labels); err != nil {
			logging.Warn("failed to add labels to bead", "error", err, "beadID", beadID)
			// Continue - bead was created successfully
		}
	}

	result.BeadID = beadID
	result.Created = true

	logging.Info("successfully created bead from PR",
		"beadID", beadID,
		"prNumber", metadata.Number)

	return result, nil
}

// findExistingPRBead checks if a bead already exists for the given PR URL.
// Uses the bd CLI to list beads and find one with matching external_ref.
func findExistingPRBead(ctx context.Context, beadsDir, prURL string) (string, error) {
	cmd := exec.CommandContext(ctx, "bd", "list", "--json")
	if beadsDir != "" {
		cmd.Env = append(os.Environ(), "BEADS_DIR="+beadsDir)
	}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list beads: %w", err)
	}

	var beadsList []struct {
		ID          string `json:"id"`
		ExternalRef string `json:"external_ref"`
	}
	if err := json.Unmarshal(output, &beadsList); err != nil {
		return "", fmt.Errorf("failed to parse beads list: %w", err)
	}

	for _, bead := range beadsList {
		if bead.ExternalRef == prURL {
			return bead.ID, nil
		}
	}

	return "", nil
}

// parsePriority converts priority string (P0-P4) to int (0-4).
func parsePriority(priority string) int {
	if len(priority) >= 2 && priority[0] == 'P' {
		switch priority[1] {
		case '0':
			return 0
		case '1':
			return 1
		case '2':
			return 2
		case '3':
			return 3
		case '4':
			return 4
		}
	}
	return 2 // default to medium
}
