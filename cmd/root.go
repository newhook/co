package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ac",
	Short: "Auto Claude - orchestrates Claude Code to process beads",
	Long: `Auto Claude (ac) is a CLI tool that orchestrates Claude Code to process beads,
creating PRs for each task.

It queries ready beads, invokes Claude Code to implement changes,
and manages the PR workflow automatically.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(runCmd)
}
