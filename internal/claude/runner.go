package claude

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
)

//go:embed templates/estimate.tmpl
var estimateTemplateText string

//go:embed templates/task.tmpl
var taskTemplateText string

//go:embed templates/pr.tmpl
var prTemplateText string

//go:embed templates/review.tmpl
var reviewTemplateText string

var (
	estimateTmpl = template.Must(template.New("estimate").Parse(estimateTemplateText))
	taskTmpl     = template.Must(template.New("task").Parse(taskTemplateText))
	prTmpl       = template.Must(template.New("pr").Parse(prTemplateText))
	reviewTmpl   = template.Must(template.New("review").Parse(reviewTemplateText))
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
func Run(ctx context.Context, database *db.DB, taskID string, taskBeads []beads.Bead, prompt string, workDir, projectName string, autoClose bool) (*TaskResult, error) {
	sessionName := SessionNameForProject(projectName)

	// Always use the full task ID as the tab name for clear task isolation
	// This ensures each task gets its own tab that can be independently managed
	tabName := fmt.Sprintf("task-%s", taskID)

	result := &TaskResult{
		TaskID: taskID,
	}

	// Ensure session exists
	if err := ensureSession(ctx, sessionName); err != nil {
		return nil, err
	}

	// Write prompt to a temporary file
	tmpDir := os.TempDir()
	promptFile := filepath.Join(tmpDir, fmt.Sprintf("co-prompt-%s.txt", taskID))
	if err := os.WriteFile(promptFile, []byte(prompt), 0600); err != nil {
		return nil, fmt.Errorf("failed to write prompt file: %w", err)
	}
	// Clean up the prompt file when done
	defer os.Remove(promptFile)

	// Build the wrapper command - assume co is in PATH since user is running it
	claudeCommand := fmt.Sprintf("co claude %s --prompt-file %s", taskID, promptFile)
	if autoClose {
		claudeCommand += " --auto-close"
	}

	// Check if tab with this task name already exists
	// Since each task gets its own tab, this shouldn't normally happen
	// But handle it gracefully by terminating and restarting
	if TabExists(ctx, sessionName, tabName) {
		fmt.Printf("Tab %s already exists, terminating any running process and restarting...\n", tabName)

		// Switch to the existing tab
		switchArgs := []string{"-s", sessionName, "action", "go-to-tab-name", tabName}
		switchCmd := exec.CommandContext(ctx, "zellij", switchArgs...)
		if err := switchCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to switch to existing tab: %v\n", err)
		}

		// Send Ctrl+C to terminate any running process
		fmt.Println("Terminating any existing process...")
		ctrlCArgs := []string{"-s", sessionName, "action", "write", "3"} // Ctrl+C
		exec.CommandContext(ctx, "zellij", ctrlCArgs...).Run()
		time.Sleep(500 * time.Millisecond)

		// Clear the line for a clean start
		clearArgs := []string{"-s", sessionName, "action", "write-chars", "clear"}
		exec.CommandContext(ctx, "zellij", clearArgs...).Run()
		time.Sleep(100 * time.Millisecond)
		exec.CommandContext(ctx, "zellij", "-s", sessionName, "action", "write", "13").Run()
		time.Sleep(100 * time.Millisecond)

		// Now launch Claude wrapper
		fmt.Println("Starting Claude wrapper...")
		runArgs := []string{"-s", sessionName, "action", "write-chars", claudeCommand}
		fmt.Printf("Running: zellij %s\n", strings.Join(runArgs, " "))
		runCmd := exec.CommandContext(ctx, "zellij", runArgs...)
		if err := runCmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to write claude wrapper command: %w", err)
		}

		// Send Enter to execute the command
		enterArgs := []string{"-s", sessionName, "action", "write", "13"}
		enterCmd := exec.CommandContext(ctx, "zellij", enterArgs...)
		if err := enterCmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to execute claude wrapper command: %w", err)
		}

		// Wait for Claude to initialize
		fmt.Println("Waiting 3s for Claude to initialize...")
		time.Sleep(3 * time.Second)
	} else {
		// Create a new tab with the task name
		tabArgs := []string{
			"-s", sessionName, "action", "new-tab",
			"--cwd", workDir,
			"--name", tabName,
		}
		fmt.Printf("Running: zellij %s\n", strings.Join(tabArgs, " "))
		tabCmd := exec.CommandContext(ctx, "zellij", tabArgs...)
		if err := tabCmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to create tab: %w", err)
		}

		// Wait a moment for tab to be created
		time.Sleep(500 * time.Millisecond)

		// Switch to the new tab
		switchArgs := []string{"-s", sessionName, "action", "go-to-tab-name", tabName}
		switchCmd := exec.CommandContext(ctx, "zellij", switchArgs...)
		if err := switchCmd.Run(); err != nil {
			// Non-fatal: just log it
			fmt.Printf("Warning: failed to switch to tab: %v\n", err)
		}

		// Run Claude wrapper in the new tab
		runArgs := []string{"-s", sessionName, "action", "write-chars", claudeCommand}
		fmt.Printf("Running: zellij %s\n", strings.Join(runArgs, " "))
		runCmd := exec.CommandContext(ctx, "zellij", runArgs...)
		if err := runCmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to write claude wrapper command: %w", err)
		}

		// Send Enter to execute the command
		enterArgs := []string{"-s", sessionName, "action", "write", "13"}
		enterCmd := exec.CommandContext(ctx, "zellij", enterArgs...)
		if err := enterCmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to execute claude wrapper command: %w", err)
		}

		// Wait for Claude to initialize
		fmt.Println("Waiting 3s for Claude to initialize...")
		time.Sleep(3 * time.Second)
	}

	fmt.Println("Claude started with task prompt")

	// Monitor for task completion via database polling
	fmt.Printf("Polling database for completion of task: %s (%d beads)\n", taskID, len(taskBeads))
	tabExitCount := 0
	for {
		time.Sleep(2 * time.Second)

		// Check if task is completed (all beads done)
		task, err := database.GetTask(ctx, taskID)
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

			// The wrapper (co claude) will detect the task completion and terminate Claude
			// Tab remains open for review
			fmt.Println("Task completed - wrapper will terminate Claude")
			break
		}

		// Check if tab has exited (Claude crashed or finished without marking complete)
		if !TabExists(ctx, sessionName, tabName) {
			tabExitCount++
			// Wait a few cycles to confirm it's really gone (not just a transient state)
			if tabExitCount >= 3 {
				fmt.Println("Claude tab exited without completing task - checking for partial completion")

				// Determine which beads completed and which failed
				result.CompletedBeads, result.FailedBeads = getTaskBeadStatus(database, taskID, taskBeads)

				if len(result.CompletedBeads) > 0 && len(result.FailedBeads) > 0 {
					result.PartialFailure = true
					result.Error = fmt.Errorf("partial failure: %d beads completed, %d beads failed",
						len(result.CompletedBeads), len(result.FailedBeads))

					// Mark remaining beads as failed in database
					for _, beadID := range result.FailedBeads {
						database.FailTaskBead(ctx, taskID, beadID)
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
			tabExitCount = 0
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
	data := struct {
		TaskID     string
		BeadIDs    []string
		BranchName string
		BaseBranch string
	}{
		TaskID:     taskID,
		BeadIDs:    getBeadIDs(taskBeads),
		BranchName: branchName,
		BaseBranch: baseBranch,
	}

	var buf bytes.Buffer
	if err := taskTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Task %s on branch %s for beads: %v", taskID, branchName, getBeadIDs(taskBeads))
	}

	return buf.String()
}

// getBeadIDs extracts bead IDs from a slice of beads.
func getBeadIDs(beads []beads.Bead) []string {
	ids := make([]string, len(beads))
	for i, b := range beads {
		ids[i] = b.ID
	}
	return ids
}

func TabExists(ctx context.Context, sessionName, tabName string) bool {
	// Use zellij action to list tabs and check if one with this name exists
	cmd := exec.CommandContext(ctx, "zellij", "-s", sessionName, "action", "query-tab-names")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), tabName)
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
	data := struct {
		TaskID  string
		BeadIDs []string
	}{
		TaskID:  taskID,
		BeadIDs: getBeadIDs(taskBeads),
	}

	var buf bytes.Buffer
	if err := estimateTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Estimation task %s for beads: %v", taskID, getBeadIDs(taskBeads))
	}

	return buf.String()
}

// BuildPRPrompt builds a prompt for PR creation.
func BuildPRPrompt(taskID string, workID string, branchName string, baseBranch string) string {
	data := struct {
		TaskID     string
		WorkID     string
		BranchName string
		BaseBranch string
	}{
		TaskID:     taskID,
		WorkID:     workID,
		BranchName: branchName,
		BaseBranch: baseBranch,
	}

	var buf bytes.Buffer
	if err := prTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("PR creation task %s for work %s on branch %s (base: %s)", taskID, workID, branchName, baseBranch)
	}

	return buf.String()
}

// BuildReviewPrompt builds a prompt for code review.
func BuildReviewPrompt(taskID string, workID string, branchName string, baseBranch string) string {
	data := struct {
		TaskID     string
		WorkID     string
		BranchName string
		BaseBranch string
	}{
		TaskID:     taskID,
		WorkID:     workID,
		BranchName: branchName,
		BaseBranch: baseBranch,
	}

	var buf bytes.Buffer
	if err := reviewTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Review task %s for work %s on branch %s (base: %s)", taskID, workID, branchName, baseBranch)
	}

	return buf.String()
}
