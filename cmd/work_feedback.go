package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/github"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var workFeedbackCmd = &cobra.Command{
	Use:   "feedback [<work-id>]",
	Short: "Process PR feedback and create beads",
	Long: `Process PR feedback for a work unit and create beads from actionable items.

Fetches PR status checks, workflow runs, comments, and review comments,
then creates beads for failures and requested changes.

If no work ID is provided, detects from current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkFeedback,
}

var (
	feedbackDryRun      bool
	feedbackAutoAdd     bool
	feedbackMinPriority int
)

func init() {
	workFeedbackCmd.Flags().BoolVar(&feedbackDryRun, "dry-run", false, "Show what beads would be created without creating them")
	workFeedbackCmd.Flags().BoolVar(&feedbackAutoAdd, "auto-add", false, "Automatically add created beads to the work")
	workFeedbackCmd.Flags().IntVar(&feedbackMinPriority, "min-priority", 2, "Minimum priority for created beads (0-4, 0=critical)")
}

// ProcessPRFeedbackQuiet processes PR feedback without outputting to stdout
// This is used by the scheduler to avoid interfering with the TUI
// Returns the number of beads created and any error
func ProcessPRFeedbackQuiet(ctx context.Context, proj *project.Project, database *db.DB, workID string, autoAdd bool, minPriority int) (int, error) {
	return processPRFeedbackInternal(ctx, proj, database, workID, autoAdd, minPriority, true)
}

// ProcessPRFeedback processes PR feedback for a work and creates beads
// This is an internal function that can be called directly
// Returns the number of beads created and any error
func ProcessPRFeedback(ctx context.Context, proj *project.Project, database *db.DB, workID string, autoAdd bool, minPriority int) (int, error) {
	return processPRFeedbackInternal(ctx, proj, database, workID, autoAdd, minPriority, false)
}

// processPRFeedbackInternal is the actual implementation with output control
func processPRFeedbackInternal(ctx context.Context, proj *project.Project, database *db.DB, workID string, autoAdd bool, minPriority int, quiet bool) (int, error) {
	// Get work details
	work, err := database.GetWork(ctx, workID)
	if err != nil {
		return 0, fmt.Errorf("failed to get work %s: %w", workID, err)
	}

	if work.PRURL == "" {
		return 0, fmt.Errorf("work %s does not have an associated PR URL", workID)
	}

	if work.RootIssueID == "" {
		return 0, fmt.Errorf("work %s does not have a root issue ID", workID)
	}

	if !quiet {
		fmt.Printf("Processing PR feedback for work %s\n", workID)
		fmt.Printf("PR URL: %s\n", work.PRURL)
		fmt.Printf("Root issue: %s\n", work.RootIssueID)
	}

	// Create GitHub integration with custom rules
	rules := &github.FeedbackRules{
		CreateBeadForFailedChecks:   true,
		CreateBeadForTestFailures:   true,
		CreateBeadForLintErrors:     true,
		CreateBeadForReviewComments: true,
		IgnoreDraftPRs:              false,
		MinimumPriority:             minPriority,
	}

	integration := github.NewIntegration(rules)

	// Fetch and process PR feedback
	if !quiet {
		fmt.Println("\nFetching PR feedback...")
	}
	feedbackItems, err := integration.FetchAndStoreFeedback(ctx, work.PRURL)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch PR feedback: %w", err)
	}

	if len(feedbackItems) == 0 {
		if !quiet {
			fmt.Println("No actionable feedback found.")
		}
		return 0, nil
	}

	if !quiet {
		fmt.Printf("Found %d actionable feedback items:\n\n", len(feedbackItems))
	}

	// Store feedback in database and create beads
	createdBeads := []string{}

	for i, item := range feedbackItems {
		// Check if feedback already exists
		// Prefer checking by source_id (unique comment ID) if available
		var exists bool
		var err error

		if sourceID, ok := item.Context["source_id"]; ok && sourceID != "" {
			// Use the unique source ID for deduplication
			exists, err = database.HasExistingFeedbackBySourceID(ctx, workID, sourceID)
		} else {
			// Fallback to title + source check (less reliable)
			exists, err = database.HasExistingFeedback(ctx, workID, item.Title, item.Source)
		}

		if err != nil {
			if !quiet {
				fmt.Printf("Error checking existing feedback: %v\n", err)
			}
			continue
		}

		if exists {
			if !quiet {
				fmt.Printf("%d. [SKIP - Already processed] %s\n", i+1, item.Title)
			}
			continue
		}

		if !quiet {
			fmt.Printf("%d. %s\n", i+1, item.Title)
			fmt.Printf("   Type: %s | Priority: P%d | Source: %s\n", item.Type, item.Priority, item.Source)
		}

		// Store feedback in database
		prFeedback, err := database.CreatePRFeedback(ctx, workID, work.PRURL, string(item.Type), item.Title,
			item.Description, item.Source, item.SourceURL, item.Priority, item.Context)
		if err != nil {
			if !quiet {
				fmt.Printf("   Error storing feedback: %v\n", err)
			}
			continue
		}

		// Create bead info with metadata for external-ref
		metadata := map[string]string{
			"source_url": item.SourceURL,
		}
		// Add source_id if available
		if prFeedback.SourceID != nil && *prFeedback.SourceID != "" {
			metadata["source_id"] = *prFeedback.SourceID
		}
		// Add other context from item
		for k, v := range item.Context {
			metadata[k] = v
		}

		beadInfo := github.BeadInfo{
			Title:       item.Title,
			Description: item.Description,
			Type:        getBeadType(item.Type),
			Priority:    item.Priority,
			ParentID:    work.RootIssueID,
			Labels:      []string{"from-pr-feedback"},
			Metadata:    metadata,
		}

		// Create bead using bd CLI
		beadID, err := integration.CreateBeadFromFeedback(ctx, beadInfo)
		if err != nil {
			if !quiet {
				fmt.Printf("   Error creating bead: %v\n", err)
			}
			continue
		}

		if !quiet {
			fmt.Printf("   Created bead: %s\n", beadID)
		}
		createdBeads = append(createdBeads, beadID)

		// Post back to GitHub comment if this feedback came from a comment
		if sourceID, ok := metadata["source_id"]; ok && sourceID != "" {
			// Check if it's from a review comment or regular comment
			isReviewComment := false
			if _, ok := metadata["reviewer"]; ok {
				// This is from a review comment
				isReviewComment = true
			}

			// Create the acknowledgment message
			ackMessage := fmt.Sprintf("✅ Created tracking issue **%s** for this feedback.\n\nTitle: %s\nPriority: P%d",
				beadID, item.Title, item.Priority)

			// Post the acknowledgment
			if commentIDStr, ok := metadata["comment_id"]; ok && commentIDStr != "" {
				commentID, parseErr := strconv.Atoi(commentIDStr)
				if parseErr == nil {
					client := github.NewClient()
					var postErr error
					if isReviewComment {
						postErr = client.PostReviewReply(ctx, work.PRURL, commentID, ackMessage)
					} else {
						postErr = client.PostReplyToComment(ctx, work.PRURL, commentID, ackMessage)
					}

					if postErr != nil {
						if !quiet {
							fmt.Printf("   Warning: Failed to post acknowledgment to GitHub: %v\n", postErr)
						}
					} else {
						if !quiet {
							fmt.Printf("   Posted acknowledgment to GitHub comment\n")
						}
					}
				}
			}
		}

		// Mark feedback as processed
		if err := database.MarkFeedbackProcessed(ctx, prFeedback.ID, beadID); err != nil {
			if !quiet {
				fmt.Printf("   Warning: Failed to mark feedback as processed: %v\n", err)
			}
		}

		// Add bead to work if auto-add is enabled
		if autoAdd {
			// Add to work_beads table - need to get next group ID
			groups, err := database.GetWorkBeadGroups(ctx, workID)
			if err != nil {
				if !quiet {
					fmt.Printf("   Warning: Failed to get work groups: %v\n", err)
				}
				continue
			}

			nextGroupID := int64(1)
			if len(groups) > 0 {
				nextGroupID = groups[len(groups)-1] + 1
			}

			if err := database.AddWorkBead(ctx, workID, beadID, nextGroupID, 0); err != nil {
				if !quiet {
					fmt.Printf("   Warning: Failed to add bead to work: %v\n", err)
				}
			} else {
				if !quiet {
					fmt.Printf("   Added bead to work\n")
				}
			}
		}
	}

	// Summary
	if !quiet {
		fmt.Printf("\n=== Summary ===\n")
		fmt.Printf("Total feedback items: %d\n", len(feedbackItems))
		fmt.Printf("Beads created: %d\n", len(createdBeads))

		if len(createdBeads) > 0 && !autoAdd {
			fmt.Println("\nTo add these beads to the work, run:")
			fmt.Printf("  co work add %s\n", strings.Join(createdBeads, " "))
		}

		if len(createdBeads) > 0 && autoAdd {
			fmt.Println("\nBeads have been added to the work. Run the following to execute them:")
			fmt.Printf("  co run --work %s\n", workID)
		}
	}

	return len(createdBeads), nil
}

func runWorkFeedback(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	// Find project
	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to find project: %w", err)
	}
	defer proj.Close()

	// Get work ID
	var workID string
	if len(args) > 0 {
		workID = args[0]
	} else {
		// Try to detect from current directory
		work, err := detectWork(ctx, proj, proj.DB)
		if err != nil {
			return fmt.Errorf("failed to detect work from current directory: %w", err)
		}
		workID = work.ID
	}

	// Skip dry-run as it's not needed for internal calls
	if feedbackDryRun {
		fmt.Println("[DRY RUN MODE - Not creating beads]")
	}

	// Call the internal function
	_, err = ProcessPRFeedback(ctx, proj, proj.DB, workID, feedbackAutoAdd, feedbackMinPriority)
	return err
}

func getBeadType(feedbackType github.FeedbackType) string {
	switch feedbackType {
	case github.FeedbackTypeTest, github.FeedbackTypeBuild, github.FeedbackTypeCI:
		return "bug"
	case github.FeedbackTypeLint, github.FeedbackTypeSecurity:
		return "task"
	case github.FeedbackTypeReview:
		return "task"
	default:
		return "task"
	}
}

// detectWork tries to detect the work from the current directory
func detectWork(ctx context.Context, proj *project.Project, database *db.DB) (*db.Work, error) {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if we're in a work directory
	if !strings.Contains(cwd, proj.Root) {
		return nil, fmt.Errorf("not in a project directory")
	}

	// Extract work ID from path
	rel, err := filepath.Rel(proj.Root, cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to get relative path: %w", err)
	}

	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) == 0 {
		return nil, fmt.Errorf("could not detect work ID from path")
	}

	// The work ID should be the first directory component (e.g., "w-abc")
	workID := parts[0]
	if !strings.HasPrefix(workID, "w-") {
		return nil, fmt.Errorf("not in a work directory (expected w-* prefix)")
	}

	// Verify work exists
	work, err := database.GetWork(ctx, workID)
	if err != nil {
		return nil, fmt.Errorf("work %s not found: %w", workID, err)
	}

	return work, nil
}
