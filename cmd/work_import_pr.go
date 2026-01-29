package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/newhook/co/internal/control"
	"github.com/newhook/co/internal/git"
	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/names"
	"github.com/newhook/co/internal/project"
	cosignal "github.com/newhook/co/internal/signal"
	"github.com/newhook/co/internal/work"
	"github.com/newhook/co/internal/worktree"
	"github.com/spf13/cobra"
)

var workImportPRCmd = &cobra.Command{
	Use:   "import-pr <pr-url>",
	Short: "Import a PR into a work unit",
	Long: `Create a work unit from an existing GitHub pull request.

This command fetches the PR's branch, creates a worktree, and sets up the work
for further development or review. The PR's branch becomes the work's feature branch.

Optionally creates a bead from the PR metadata to track the work in the beads system.

Examples:
  co work import-pr https://github.com/owner/repo/pull/123
  co work import-pr https://github.com/owner/repo/pull/123 --create-bead
  co work import-pr https://github.com/owner/repo/pull/123 --branch custom-branch-name`,
	Args: cobra.ExactArgs(1),
	RunE: runWorkImportPR,
}

var (
	flagImportPRCreateBead bool
	flagImportPRBranch     string
	flagImportPRAuto       bool
)

func init() {
	workImportPRCmd.Flags().BoolVar(&flagImportPRCreateBead, "create-bead", false, "create a bead from PR metadata")
	workImportPRCmd.Flags().StringVar(&flagImportPRBranch, "branch", "", "override the local branch name (default: use PR's branch name)")
	workImportPRCmd.Flags().BoolVar(&flagImportPRAuto, "auto", false, "run full automated workflow after import")
	workCmd.AddCommand(workImportPRCmd)
}

func runWorkImportPR(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	// Find project
	proj, err := project.Find(ctx, "")
	if err != nil {
		return err
	}
	defer proj.Close()

	prURL := args[0]

	// Create GitHub client and PR importer
	ghClient := github.NewClient()
	importer := github.NewPRImporter(ghClient)

	// Fetch PR metadata first
	fmt.Printf("Fetching PR metadata from %s...\n", prURL)
	metadata, err := importer.FetchPRMetadata(ctx, prURL, "")
	if err != nil {
		return fmt.Errorf("failed to fetch PR metadata: %w", err)
	}

	fmt.Printf("PR #%d: %s\n", metadata.Number, metadata.Title)
	fmt.Printf("Author: %s\n", metadata.Author)
	fmt.Printf("State: %s\n", metadata.State)
	fmt.Printf("Branch: %s -> %s\n", metadata.HeadRefName, metadata.BaseRefName)

	// Check if PR is still open
	if metadata.State != "OPEN" {
		fmt.Printf("Warning: PR is %s\n", metadata.State)
	}

	// Determine branch name
	branchName := flagImportPRBranch
	if branchName == "" {
		branchName = metadata.HeadRefName
	}

	// Generate work ID
	workID, err := proj.DB.GenerateWorkID(ctx, branchName, proj.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("failed to generate work ID: %w", err)
	}
	fmt.Printf("\nWork ID: %s\n", workID)

	// Block signals during critical worktree creation
	cosignal.BlockSignals()
	defer cosignal.UnblockSignals()

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	mainRepoPath := proj.MainRepoPath()
	gitOps := git.NewOperations()
	wtOps := worktree.NewOperations()

	// Set up worktree from PR
	fmt.Printf("Setting up worktree from PR branch...\n")
	_, worktreePath, err := importer.SetupWorktreeFromPR(ctx, mainRepoPath, prURL, "", workDir, branchName)
	if err != nil {
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to set up worktree: %w", err)
	}

	// Set up upstream tracking
	if err := gitOps.PushSetUpstream(ctx, branchName, worktreePath); err != nil {
		_ = wtOps.RemoveForce(ctx, mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to set upstream: %w", err)
	}

	// Initialize mise in worktree if needed
	if err := mise.Initialize(worktreePath); err != nil {
		fmt.Printf("Warning: mise initialization failed: %v\n", err)
	}

	// Get a human-readable name for this worker
	workerName, err := names.GetNextAvailableName(ctx, proj.DB.DB)
	if err != nil {
		fmt.Printf("Warning: failed to get worker name: %v\n", err)
	}

	// Optionally create a bead from PR metadata
	var rootIssueID string
	if flagImportPRCreateBead {
		fmt.Printf("Creating bead from PR metadata...\n")
		result, err := importer.CreateBeadFromPR(ctx, metadata, &github.CreateBeadOptions{
			BeadsDir:     proj.BeadsPath(),
			SkipIfExists: true,
		})
		if err != nil {
			fmt.Printf("Warning: failed to create bead: %v\n", err)
		} else if result.Created {
			fmt.Printf("Created bead: %s\n", result.BeadID)
			rootIssueID = result.BeadID
		} else {
			fmt.Printf("Bead already exists: %s (%s)\n", result.BeadID, result.SkipReason)
			rootIssueID = result.BeadID
		}
	}

	// Get base branch from project config
	baseBranch := proj.Config.Repo.GetBaseBranch()

	// Create work record in database
	if err := proj.DB.CreateWork(ctx, workID, workerName, worktreePath, branchName, baseBranch, rootIssueID, flagImportPRAuto); err != nil {
		_ = wtOps.RemoveForce(ctx, mainRepoPath, worktreePath)
		os.RemoveAll(workDir)
		return fmt.Errorf("failed to create work record: %w", err)
	}

	// Set PR URL on the work and schedule feedback polling
	prFeedbackInterval := proj.Config.Scheduler.GetPRFeedbackInterval()
	commentResolutionInterval := proj.Config.Scheduler.GetCommentResolutionInterval()
	if err := proj.DB.SetWorkPRURLAndScheduleFeedback(ctx, workID, prURL, prFeedbackInterval, commentResolutionInterval); err != nil {
		fmt.Printf("Warning: failed to set PR URL on work: %v\n", err)
	}

	// Add bead to work_beads if created
	if rootIssueID != "" {
		if err := work.AddBeadsToWorkInternal(ctx, proj, workID, []string{rootIssueID}); err != nil {
			fmt.Printf("Warning: failed to add bead to work: %v\n", err)
		}
	}

	fmt.Printf("\nImported PR into work: %s\n", workID)
	if workerName != "" {
		fmt.Printf("Worker: %s\n", workerName)
	}
	fmt.Printf("Directory: %s\n", workDir)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("Branch: %s\n", branchName)
	fmt.Printf("Base Branch: %s\n", baseBranch)
	fmt.Printf("PR URL: %s\n", prURL)

	// Initialize zellij session and spawn control plane if new session
	sessionResult, err := control.InitializeSession(ctx, proj)
	if err != nil {
		fmt.Printf("Warning: failed to initialize zellij session: %v\n", err)
	} else if sessionResult.SessionCreated {
		// Display notification for new session
		printSessionCreatedNotification(sessionResult.SessionName)
	}

	// If --auto, run the full automated workflow
	if flagImportPRAuto {
		fmt.Println("\nRunning automated workflow...")
		result, err := work.RunWorkAuto(ctx, proj, workID, os.Stdout)
		if err != nil {
			return fmt.Errorf("failed to run automated workflow: %w", err)
		}
		if result.OrchestratorSpawned {
			fmt.Println("Orchestrator spawned in zellij tab.")
		}
		// Ensure control plane is running
		if err := control.EnsureControlPlane(ctx, proj); err != nil {
			fmt.Printf("Warning: failed to ensure control plane: %v\n", err)
		}
		fmt.Println("Switch to the zellij session to monitor progress.")
		return nil
	}

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  cd %s\n", workID)
	fmt.Printf("  co run               # Execute tasks\n")

	return nil
}
