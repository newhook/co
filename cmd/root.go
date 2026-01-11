package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "co",
	Short: "Claude Orchestrator - orchestrates Claude Code to process issues",
	Long:  `Claude Orchestrator (co) is a CLI tool that orchestrates Claude Code to process issues, creating PRs for each.`,
}

func Execute() error {
	return rootCmd.Execute()
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
}
