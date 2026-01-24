package cmd

import (
	"context"
	"fmt"

	"github.com/newhook/co/internal/project"
	cosignal "github.com/newhook/co/internal/signal"
	"github.com/newhook/co/internal/tui"
	"github.com/spf13/cobra"
)

var (
	// rootCtx holds the signal-cancellable context for the application
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// flagNoMouse disables mouse support in the TUI
	flagNoMouse bool
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
	// Default to TUI when no subcommand is provided
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := GetContext()
		proj, err := project.Find(ctx, "")
		if err != nil {
			return fmt.Errorf("not in a project directory: %w", err)
		}
		defer proj.Close()

		if err := tui.RunRootTUI(ctx, proj, !flagNoMouse); err != nil {
			return fmt.Errorf("error running TUI: %w", err)
		}
		return nil
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
	// Add TUI flags to root command (when run without subcommand)
	rootCmd.Flags().BoolVar(&flagNoMouse, "no-mouse", false, "disable mouse support in the TUI")

	// Add subcommands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(completeCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(projCmd)
	rootCmd.AddCommand(workCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(orchestrateCmd)
}
