package github

import (
	"context"
	"fmt"
	"path/filepath"

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
