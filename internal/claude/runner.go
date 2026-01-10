package claude

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
)

// SessionNameForProject returns the zellij session name for a specific project.
func SessionNameForProject(projectName string) string {
	return fmt.Sprintf("co-%s", projectName)
}

// TaskResult contains the result of a task execution.
type TaskResult struct {
	TaskID         string
	Completed      bool
	PartialFailure bool
	CompletedBeads []string
	FailedBeads    []string
	Error          error
}

// Run invokes Claude Code for a task (group of beads) using project-specific session naming.
// Returns a TaskResult indicating which beads completed and which failed.
func Run(ctx context.Context, database *db.DB, taskID string, taskBeads []beads.Bead, prompt string, workDir, projectName string) (*TaskResult, error) {
	sessionName := SessionNameForProject(projectName)
	paneName := fmt.Sprintf("task-%s", taskID)

	result := &TaskResult{
		TaskID: taskID,
	}

	// Ensure session exists
	if err := ensureSession(ctx, sessionName); err != nil {
		return nil, err
	}

	// Check if pane with this task name already exists
	if paneExists(ctx, sessionName, paneName) {
		fmt.Printf("Pane %s already exists, skipping claude launch\n", paneName)
	} else {
		// Run Claude in a new pane in the session
		runArgs := []string{"-s", sessionName, "run", "--name", paneName, "--cwd", workDir, "--", "claude", "--dangerously-skip-permissions"}
		fmt.Printf("Running: zellij %s\n", strings.Join(runArgs, " "))
		runCmd := exec.CommandContext(ctx, "zellij", runArgs...)
		if err := runCmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to run claude in zellij pane: %w", err)
		}

		// Wait for Claude to initialize
		fmt.Println("Waiting 3s for Claude to initialize...")
		time.Sleep(3 * time.Second)
	}

	// Send text to the pane
	writeArgs := []string{"-s", sessionName, "action", "write-chars", prompt}
	fmt.Printf("Running: zellij -s %s action write-chars <prompt>\n", sessionName)
	writeCmd := exec.CommandContext(ctx, "zellij", writeArgs...)
	if err := writeCmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to send prompt: %w", err)
	}

	// Wait a moment for the text to be received
	time.Sleep(500 * time.Millisecond)

	// Send Enter to submit
	enterArgs := []string{"-s", sessionName, "action", "write", "13"}
	fmt.Printf("Running: zellij %s\n", strings.Join(enterArgs, " "))
	enterCmd := exec.CommandContext(ctx, "zellij", enterArgs...)
	if err := enterCmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to send enter: %w", err)
	}

	// Send another Enter just to be sure
	time.Sleep(100 * time.Millisecond)
	exec.CommandContext(ctx, "zellij", enterArgs...).Run()

	fmt.Println("Prompt sent to Claude")

	// Monitor for task completion via database polling
	fmt.Printf("Polling database for completion of task: %s (%d beads)\n", taskID, len(taskBeads))
	paneExitCount := 0
	for {
		time.Sleep(2 * time.Second)

		// Check if task is completed (all beads done)
		task, err := database.GetTask(taskID)
		if err != nil {
			fmt.Printf("Warning: failed to check task status: %v\n", err)
			continue
		}

		if task != nil && (task.Status == db.StatusCompleted || task.Status == db.StatusFailed) {
			if task.Status == db.StatusCompleted {
				fmt.Println("Task marked as completed!")
				result.Completed = true
			} else {
				fmt.Printf("Task marked as failed: %s\n", task.ErrorMessage)
				result.Error = fmt.Errorf("task failed: %s", task.ErrorMessage)
			}

			// Send /exit to close Claude
			time.Sleep(500 * time.Millisecond)
			exitArgs := []string{"-s", sessionName, "action", "write-chars", "/exit"}
			exec.CommandContext(ctx, "zellij", exitArgs...).Run()
			time.Sleep(100 * time.Millisecond)
			exec.CommandContext(ctx, "zellij", "-s", sessionName, "action", "write", "13").Run()

			fmt.Println("Sent /exit to Claude")
			break
		}

		// Check if pane has exited (Claude crashed or finished without marking complete)
		if !paneExists(ctx, sessionName, paneName) {
			paneExitCount++
			// Wait a few cycles to confirm it's really gone (not just a transient state)
			if paneExitCount >= 3 {
				fmt.Println("Claude pane exited without completing task - checking for partial completion")

				// Determine which beads completed and which failed
				result.CompletedBeads, result.FailedBeads = getTaskBeadStatus(database, taskID, taskBeads)

				if len(result.CompletedBeads) > 0 && len(result.FailedBeads) > 0 {
					result.PartialFailure = true
					result.Error = fmt.Errorf("partial failure: %d beads completed, %d beads failed",
						len(result.CompletedBeads), len(result.FailedBeads))

					// Mark remaining beads as failed in database
					for _, beadID := range result.FailedBeads {
						database.FailTaskBead(taskID, beadID)
					}
				} else if len(result.CompletedBeads) == len(taskBeads) {
					// All completed but task not marked - auto-complete
					result.Completed = true
				} else {
					// All failed
					result.Error = fmt.Errorf("task failed: no beads completed")
				}
				break
			}
		} else {
			paneExitCount = 0
		}
	}

	// Populate completed/failed beads if not already set
	if len(result.CompletedBeads) == 0 && len(result.FailedBeads) == 0 {
		result.CompletedBeads, result.FailedBeads = getTaskBeadStatus(database, taskID, taskBeads)
	}

	return result, nil
}

// getTaskBeadStatus returns lists of completed and failed bead IDs for a task.
func getTaskBeadStatus(database *db.DB, taskID string, taskBeads []beads.Bead) ([]string, []string) {
	var completed, failed []string

	for _, b := range taskBeads {
		// Check task_beads table for status
		var status string
		err := database.QueryRow(`
			SELECT status FROM task_beads WHERE task_id = ? AND bead_id = ?
		`, taskID, b.ID).Scan(&status)

		if err != nil || status != db.StatusCompleted {
			failed = append(failed, b.ID)
		} else {
			completed = append(completed, b.ID)
		}
	}

	return completed, failed
}

// BuildTaskPrompt builds a prompt for a task with multiple beads.
func BuildTaskPrompt(taskID string, taskBeads []beads.Bead, branchName, baseBranch string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("You are working on Task %s which includes the following issues:\n\n", taskID))

	for i, bead := range taskBeads {
		sb.WriteString(fmt.Sprintf("## Issue %d: %s - %s\n", i+1, bead.ID, bead.Title))
		if bead.Description != "" {
			sb.WriteString(bead.Description)
		}
		sb.WriteString("\n\n")
	}

	sb.WriteString(fmt.Sprintf(`Branch: %s
Base Branch: %s

Instructions:
1. First, check git log and git status to see if there is existing work on this branch from a previous session
2. If there is existing work, review it and continue from where it left off
3. Implement ALL issues in this task
4. Make logical commits grouping related changes
5. For EACH issue as you complete it:
   - Close the bead: bd close <id> --reason "<brief summary>"
   - Mark complete: co complete <id>
6. When ALL issues are complete:
   - Push the branch and create a PR targeting %s
   - Merge the PR using: gh pr merge --squash --delete-branch
   - Mark the final bead complete with PR: co complete <last-id> --pr <PR_URL>

Note: co complete auto-detects your task. When all beads in the task are marked complete, the task itself is marked complete.

Focus on implementing all tasks correctly and completely.`, branchName, baseBranch, baseBranch))

	return sb.String()
}

func paneExists(ctx context.Context, sessionName, paneName string) bool {
	// Use zellij action to list panes and check if one with this name exists
	cmd := exec.CommandContext(ctx, "zellij", "-s", sessionName, "action", "query-tab-names")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), paneName)
}

func ensureSession(ctx context.Context, sessionName string) error {
	// Check if session exists
	listCmd := exec.CommandContext(ctx, "zellij", "list-sessions")
	output, err := listCmd.Output()
	if err != nil {
		// No sessions, create one
		return createSession(ctx, sessionName)
	}

	// Check if requested session exists
	if strings.Contains(string(output), sessionName) {
		return nil
	}

	return createSession(ctx, sessionName)
}

func createSession(ctx context.Context, sessionName string) error {
	// Start session detached
	cmd := exec.CommandContext(ctx, "zellij", "-s", sessionName)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}
	// Give it time to start
	time.Sleep(1 * time.Second)
	return nil
}

// BuildEstimatePrompt builds a prompt for complexity estimation of beads.
func BuildEstimatePrompt(taskID string, taskBeads []beads.Bead) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("You are working on Estimation Task %s to estimate complexity for the following issues:\n\n", taskID))

	for i, bead := range taskBeads {
		sb.WriteString(fmt.Sprintf("## Issue %d: %s - %s\n", i+1, bead.ID, bead.Title))
		if bead.Description != "" {
			sb.WriteString(bead.Description)
		}
		sb.WriteString("\n\n")
	}

	sb.WriteString(`Instructions:
1. For each issue above, estimate its complexity and token usage
2. Run the following command for EACH issue:
   co estimate <id> --score <complexity> --tokens <estimated-tokens>

Complexity Scoring Guide:
- 1 = Trivial change (typo fix, one-liner, config change)
- 2-3 = Simple change (small function, straightforward bug fix)
- 4-5 = Medium change (new feature, multiple file changes)
- 6-7 = Complex change (significant feature, architectural changes)
- 8-9 = Very complex (major refactor, cross-cutting concerns)
- 10 = Massive change (complete rewrite, major architectural overhaul)

Token Estimation Guide:
- 5000-10000 = Very simple changes
- 10000-20000 = Simple to medium changes
- 20000-35000 = Medium to complex changes
- 35000-50000 = Complex changes requiring deep analysis

Base your estimates on:
- Number of files likely to be modified
- Complexity of the logic involved
- Amount of context needed to understand the task
- Testing requirements
- Potential for unexpected complications

After estimating all issues, the task will auto-complete. Do not use /exit.`)

	return sb.String()
}
