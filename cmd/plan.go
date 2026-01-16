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

var planCmd = &cobra.Command{
	Use:   "plan <bead-id>",
	Short: "Launch Claude for planning a specific issue",
	Long: `Plan launches Claude Code for planning work on a specific issue.

This command is typically invoked by the TUI's Plan mode, which creates a
zellij tab for each issue and runs 'co plan <id>' within it.

Claude can then be used to:
- Investigate the issue (bd show <id>)
- Break down the issue into subtasks
- Plan implementation strategies
- Create related issues

Each issue gets its own dedicated planning session in a separate tab.`,
	Args: cobra.ExactArgs(1),
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
	defer proj.Close()

	beadID := args[0]
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

	// Build initial prompt for Claude
	initialPrompt := fmt.Sprintf(`You are planning for issue %s.

First, use the beads skill to investigate this issue:
/beads show %s

After reviewing the issue details, help plan the implementation by:
- Breaking down the work into subtasks if needed
- Identifying dependencies or blockers
- Suggesting implementation approaches
- Creating related issues with /beads create if needed

When breaking down into subtasks, create them as children of this issue:

  bd create "<subtask title>" --parent %s --type task \
    --description "<description of the subtask>"

This establishes %s as the parent, making the new issues its children.
The orchestrator will then process these subtasks as part of this work.

Use /beads commands throughout to manage the issue tracker.`, beadID, beadID, beadID, beadID)

	// Launch Claude in the main repo with the initial prompt
	claudeCmd := exec.Command("claude", "--dangerously-skip-permissions", initialPrompt)
	claudeCmd.Dir = mainRepoPath
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr

	if err := claudeCmd.Run(); err != nil {
		return fmt.Errorf("claude exited with error: %w", err)
	}

	return nil
}
