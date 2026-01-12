package cmd

import (
	"fmt"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var (
	flagEstimateScore  int
	flagEstimateTokens int
	flagEstimateTask   string
)

var estimateCmd = &cobra.Command{
	Use:   "estimate <bead-id>",
	Short: "Report complexity estimate for a bead (used by Claude during estimation)",
	Long:  `Report complexity estimate for a bead. This command is called by Claude Code during estimation tasks.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runEstimate,
}

func init() {
	estimateCmd.Flags().IntVar(&flagEstimateScore, "score", 0, "Complexity score (1-10)")
	estimateCmd.Flags().IntVar(&flagEstimateTokens, "tokens", 0, "Estimated tokens needed")
	estimateCmd.Flags().StringVar(&flagEstimateTask, "task", "", "Task ID (optional, helps with multiple estimation runs)")
	estimateCmd.MarkFlagRequired("score")
	estimateCmd.MarkFlagRequired("tokens")
	rootCmd.AddCommand(estimateCmd)
}

func runEstimate(cmd *cobra.Command, args []string) error {
	beadID := args[0]

	// Validate score range
	if flagEstimateScore < 1 || flagEstimateScore > 10 {
		return fmt.Errorf("score must be between 1 and 10, got %d", flagEstimateScore)
	}

	// Validate tokens range
	if flagEstimateTokens < 5000 || flagEstimateTokens > 50000 {
		return fmt.Errorf("tokens must be between 5000 and 50000, got %d", flagEstimateTokens)
	}

	// Find project
	proj, err := project.Find("")
	if err != nil {
		return fmt.Errorf("failed to find project: %w", err)
	}
	defer proj.Close()

	// Get bead from beads DB to compute description hash
	bead, err := beads.GetBeadInDir(beadID, proj.MainRepoPath())
	if err != nil {
		return fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}

	// Compute description hash
	// Combine title and description as that's what affects complexity
	fullDescription := bead.Title + "\n" + bead.Description
	descHash := db.HashDescription(fullDescription)

	// Store estimate in complexity cache
	if err := proj.DB.CacheComplexity(beadID, descHash, flagEstimateScore, flagEstimateTokens); err != nil {
		return fmt.Errorf("failed to cache complexity: %w", err)
	}

	// Use provided task ID or find which task contains this bead
	taskID := flagEstimateTask
	if taskID == "" {
		taskID, err = proj.DB.GetTaskForBead(GetContext(), beadID)
		if err != nil {
			return fmt.Errorf("failed to find task for bead: %w", err)
		}
	}

	if taskID == "" {
		// Not part of a task, just print confirmation
		fmt.Printf("✓ Estimated %s: complexity=%d, tokens=%d\n", beadID, flagEstimateScore, flagEstimateTokens)
		return nil
	}

	// Mark this bead as completed in the task
	if err := proj.DB.CompleteTaskBead(GetContext(), taskID, beadID); err != nil {
		// Non-fatal: bead might not be in a task or already completed
		fmt.Printf("Note: could not mark bead complete in task: %v\n", err)
	}

	// Check if this is an estimate task
	task, err := proj.DB.GetTask(GetContext(), taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	if task != nil && task.TaskType == "estimate" {
		// Get all beads in the task
		taskBeadIDs, err := proj.DB.GetTaskBeads(GetContext(), taskID)
		if err != nil {
			return fmt.Errorf("failed to get task beads: %w", err)
		}

		// Check if all beads have estimates
		allEstimated, err := proj.DB.AreAllBeadsEstimated(taskBeadIDs)
		if err != nil {
			return fmt.Errorf("failed to check estimates: %w", err)
		}

		if allEstimated {
			// Auto-complete the estimation task
			if err := proj.DB.CompleteTask(GetContext(), taskID, ""); err != nil {
				return fmt.Errorf("failed to complete task: %w", err)
			}
			fmt.Printf("✓ Estimated %s: complexity=%d, tokens=%d\n", beadID, flagEstimateScore, flagEstimateTokens)
			fmt.Printf("✅ All %d beads estimated. Task %s complete!\n", len(taskBeadIDs), taskID)

			// Print summary of estimates
			fmt.Println("\nEstimation Summary:")
			for _, id := range taskBeadIDs {
				// Get bead info for display
				b, err := beads.GetBeadInDir(id, proj.MainRepoPath())
				if err != nil {
					continue
				}
				// Get cached complexity
				fullDesc := b.Title + "\n" + b.Description
				hash := db.HashDescription(fullDesc)
				score, tokens, found, _ := proj.DB.GetCachedComplexity(id, hash)
				if found {
					// Truncate title if too long
					title := b.Title
					if len(title) > 50 {
						title = title[:47] + "..."
					}
					fmt.Printf("  %s: %s (complexity=%d, tokens=%d)\n", id, title, score, tokens)
				}
			}
		} else {
			// Count remaining
			var remaining []string
			for _, id := range taskBeadIDs {
				b, _ := beads.GetBeadInDir(id, proj.MainRepoPath())
				if b != nil {
					fullDesc := b.Title + "\n" + b.Description
					hash := db.HashDescription(fullDesc)
					_, _, found, _ := proj.DB.GetCachedComplexity(id, hash)
					if !found {
						remaining = append(remaining, id)
					}
				}
			}
			fmt.Printf("✓ Estimated %s: complexity=%d, tokens=%d\n", beadID, flagEstimateScore, flagEstimateTokens)
			fmt.Printf("Progress: %d/%d estimated. Remaining: %s\n",
				len(taskBeadIDs)-len(remaining), len(taskBeadIDs), strings.Join(remaining, ", "))
		}
	} else {
		// Regular implement task, just print confirmation
		fmt.Printf("✓ Estimated %s: complexity=%d, tokens=%d\n", beadID, flagEstimateScore, flagEstimateTokens)
	}

	return nil
}