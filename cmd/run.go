package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Process ready beads with Claude Code",
	Long: `Run processes all ready beads by invoking Claude Code to implement
each task and creating PRs for the changes.

The workflow for each bead:
1. Claude Code creates a branch and implements the changes
2. A PR is created and merged
3. The bead is closed with a summary`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("run called - not yet implemented")
	},
}
