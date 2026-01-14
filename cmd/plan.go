package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var (
	flagPlanBead string
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Launch Claude for planning a specific issue",
	Long: `Plan launches Claude Code for planning work on a specific issue.

This command is typically invoked by the TUI's Plan mode, which creates a
zellij tab for each issue and runs 'co plan --bead=<id>' within it.

Claude can then be used to:
- Investigate the issue (bd show <id>)
- Break down the issue into subtasks
- Plan implementation strategies
- Create related issues

Each issue gets its own dedicated planning session in a separate tab.`,
	RunE: runPlan,
}

func init() {
	rootCmd.AddCommand(planCmd)
	planCmd.Flags().StringVar(&flagPlanBead, "bead", "", "bead ID to plan for (required)")
	_ = planCmd.MarkFlagRequired("bead")
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Find project
	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to find project: %w", err)
	}
	defer proj.Close()

	// Validate bead ID is provided
	if flagPlanBead == "" {
		return fmt.Errorf("--bead flag is required")
	}

	beadID := flagPlanBead
	zellijSession := fmt.Sprintf("co-%s", proj.Config.Project.Name)
	tabName := db.TabNameForBead(beadID)

	// Apply hooks.env to current process - inherited by child processes (Claude)
	applyHooksEnv(proj.Config.Hooks.Env)

	// Register the plan session in the database
	if err := proj.DB.RegisterPlanSession(ctx, beadID, zellijSession, tabName, os.Getpid()); err != nil {
		return fmt.Errorf("failed to register plan session: %w", err)
	}
	defer func() {
		// Unregister when done
		_ = proj.DB.UnregisterPlanSession(ctx, beadID)
	}()

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
