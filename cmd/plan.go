package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Launch Claude for planning and issue management",
	Long: `Plan launches Claude Code in the main repository for planning work.

This command is typically invoked by the TUI's Plan mode, which creates a
zellij tab and runs 'co plan' within it. Claude can then be used to:

- Investigate existing issues (bd show <id>)
- Create new issues (bd create)
- Update issue status and priorities
- Plan implementation strategies

The TUI's Plan mode can send issues to Claude by typing 'bd show <id>'
into this session.`,
	RunE: runPlan,
}

func init() {
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Find project
	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to find project: %w", err)
	}

	mainRepoPath := proj.MainRepoPath()

	// Launch Claude in the main repo
	claudeCmd := exec.Command("claude")
	claudeCmd.Dir = mainRepoPath
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr

	if err := claudeCmd.Run(); err != nil {
		return fmt.Errorf("claude exited with error: %w", err)
	}

	return nil
}
