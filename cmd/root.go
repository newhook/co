package cmd

import (
	"context"

	cosignal "github.com/newhook/co/internal/signal"
	"github.com/spf13/cobra"
)

var (
	// rootCtx holds the signal-cancellable context for the application
	rootCtx    context.Context
	rootCancel context.CancelFunc
)

var rootCmd = &cobra.Command{
	Use:   "co",
	Short: "Claude Orchestrator - orchestrates Claude Code to process issues",
	Long:  `Claude Orchestrator (co) is a CLI tool that orchestrates Claude Code to process issues, creating PRs for each.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Create a cancellable context with signal handling
		rootCtx, rootCancel = cosignal.WithSignalCancel(context.Background())
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		// Clean up the signal handler
		if rootCancel != nil {
			rootCancel()
		}
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// GetContext returns the root context that is cancelled on SIGINT/SIGTERM.
// This should be used by all subcommands instead of context.Background().
func GetContext() context.Context {
	if rootCtx == nil {
		// Fallback if called before PersistentPreRun (shouldn't happen in normal use)
		return context.Background()
	}
	return rootCtx
}

func init() {
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(completeCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(projCmd)
	rootCmd.AddCommand(workCmd)
	rootCmd.AddCommand(claudeCmd)
	rootCmd.AddCommand(syncCmd)
}
