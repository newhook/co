package claude

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/logging"
	"github.com/newhook/co/internal/project"
	"github.com/newhook/co/internal/zellij"
)

//go:embed templates/estimate.tmpl
var estimateTemplateText string

//go:embed templates/task.tmpl
var taskTemplateText string

//go:embed templates/pr.tmpl
var prTemplateText string

//go:embed templates/review.tmpl
var reviewTemplateText string

//go:embed templates/update-pr-description.tmpl
var updatePRDescriptionTemplateText string

//go:embed templates/plan.tmpl
var planTemplateText string

var (
	estimateTmpl            = template.Must(template.New("estimate").Parse(estimateTemplateText))
	taskTmpl                = template.Must(template.New("task").Parse(taskTemplateText))
	prTmpl                  = template.Must(template.New("pr").Parse(prTemplateText))
	reviewTmpl              = template.Must(template.New("review").Parse(reviewTemplateText))
	updatePRDescriptionTmpl = template.Must(template.New("update-pr-description").Parse(updatePRDescriptionTemplateText))
	planTmpl                = template.Must(template.New("plan").Parse(planTemplateText))
)

// SessionNameForProject returns the zellij session name for a specific project.
func SessionNameForProject(projectName string) string {
	return fmt.Sprintf("co-%s", projectName)
}

// FormatTabName formats a tab name with an optional friendly name.
// If friendlyName is not empty, formats as "prefix-workID (friendlyName)", otherwise just "prefix-workID".
func FormatTabName(prefix, workID, friendlyName string) string {
	baseName := fmt.Sprintf("%s-%s", prefix, workID)
	if friendlyName != "" {
		return fmt.Sprintf("%s (%s)", baseName, friendlyName)
	}
	return baseName
}

// BuildTaskPrompt builds a prompt for a task with multiple beads.
func BuildTaskPrompt(taskID string, beadList []beads.Bead, branchName, baseBranch string) string {
	data := struct {
		TaskID     string
		BeadIDs    []string
		BranchName string
		BaseBranch string
	}{
		TaskID:     taskID,
		BeadIDs:    getBeadIDs(beadList),
		BranchName: branchName,
		BaseBranch: baseBranch,
	}

	var buf bytes.Buffer
	if err := taskTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Task %s on branch %s for beads: %v", taskID, branchName, getBeadIDs(beadList))
	}

	return buf.String()
}

// getBeadIDs extracts bead IDs from a slice of beads.
func getBeadIDs(beadList []beads.Bead) []string {
	ids := make([]string, len(beadList))
	for i, b := range beadList {
		ids[i] = b.ID
	}
	return ids
}

// TabExists checks if a tab with the given name exists in the session.
func TabExists(ctx context.Context, sessionName, tabName string) bool {
	zc := zellij.New()
	exists, _ := zc.TabExists(ctx, sessionName, tabName)
	return exists
}

// TerminateWorkTabs terminates all zellij tabs associated with a work unit.
// This includes the work orchestrator tab (work-<workID>) and all task tabs (task-<workID>.*).
// Each tab's running process is terminated with Ctrl+C before the tab is closed.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func TerminateWorkTabs(ctx context.Context, workID string, projectName string, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	zc := zellij.New()

	// Check if session exists
	exists, err := zc.SessionExists(ctx, sessionName)
	if err != nil || !exists {
		// Session doesn't exist, nothing to terminate
		return nil
	}

	// Get list of all tab names
	tabNames, err := zc.QueryTabNames(ctx, sessionName)
	if err != nil {
		// Can't query tabs, maybe session is dead
		return nil
	}

	// Find tabs to terminate
	workTabName := fmt.Sprintf("work-%s", workID)
	taskTabPrefix := fmt.Sprintf("task-%s.", workID)

	var tabsToClose []string
	for _, tabName := range tabNames {
		tabName = strings.TrimSpace(tabName)
		if tabName == "" {
			continue
		}
		// Match work orchestrator tab or task tabs for this work
		if tabName == workTabName || strings.HasPrefix(tabName, taskTabPrefix) {
			tabsToClose = append(tabsToClose, tabName)
		}
	}

	if len(tabsToClose) == 0 {
		return nil
	}

	fmt.Fprintf(w, "Terminating %d zellij tab(s) for work %s...\n", len(tabsToClose), workID)

	for _, tabName := range tabsToClose {
		if err := zc.TerminateAndCloseTab(ctx, sessionName, tabName); err != nil {
			fmt.Fprintf(w, "Warning: failed to terminate tab %s: %v\n", tabName, err)
			// Continue with other tabs
		} else {
			fmt.Fprintf(w, "  Terminated tab: %s\n", tabName)
		}
	}

	return nil
}

// BuildEstimatePrompt builds a prompt for complexity estimation of beads.
func BuildEstimatePrompt(taskID string, beadList []beads.Bead) string {
	data := struct {
		TaskID  string
		BeadIDs []string
	}{
		TaskID:  taskID,
		BeadIDs: getBeadIDs(beadList),
	}

	var buf bytes.Buffer
	if err := estimateTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Estimation task %s for beads: %v", taskID, getBeadIDs(beadList))
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
func BuildReviewPrompt(taskID string, workID string, branchName string, baseBranch string, rootIssueID string) string {
	data := struct {
		TaskID      string
		WorkID      string
		BranchName  string
		BaseBranch  string
		RootIssueID string
	}{
		TaskID:      taskID,
		WorkID:      workID,
		BranchName:  branchName,
		BaseBranch:  baseBranch,
		RootIssueID: rootIssueID,
	}

	var buf bytes.Buffer
	if err := reviewTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Review task %s for work %s on branch %s (base: %s)", taskID, workID, branchName, baseBranch)
	}

	return buf.String()
}

// BuildUpdatePRDescriptionPrompt builds a prompt for updating a PR description.
func BuildUpdatePRDescriptionPrompt(taskID string, workID string, prURL string, branchName string, baseBranch string) string {
	data := struct {
		TaskID     string
		WorkID     string
		PRURL      string
		BranchName string
		BaseBranch string
	}{
		TaskID:     taskID,
		WorkID:     workID,
		PRURL:      prURL,
		BranchName: branchName,
		BaseBranch: baseBranch,
	}

	var buf bytes.Buffer
	if err := updatePRDescriptionTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Update PR description task %s for work %s, PR %s on branch %s (base: %s)", taskID, workID, prURL, branchName, baseBranch)
	}

	return buf.String()
}

// BuildPlanPrompt builds a prompt for planning an issue.
func BuildPlanPrompt(beadID string) string {
	data := struct {
		BeadID string
	}{
		BeadID: beadID,
	}

	var buf bytes.Buffer
	if err := planTmpl.Execute(&buf, data); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Planning for issue %s", beadID)
	}

	return buf.String()
}

// RunPlanSession runs an interactive Claude session for planning an issue.
// This launches Claude with the plan prompt and connects stdin/stdout/stderr
// for interactive use. The config parameter controls Claude settings like --dangerously-skip-permissions.
func RunPlanSession(ctx context.Context, beadID string, workDir string, stdin io.Reader, stdout, stderr io.Writer, cfg *project.Config) error {
	prompt := BuildPlanPrompt(beadID)

	var args []string
	if cfg != nil && cfg.Claude.ShouldSkipPermissions() {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = workDir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude exited with error: %w", err)
	}

	return nil
}

// SpawnWorkOrchestrator creates a zellij tab and runs the orchestrate command for a work unit.
// The tab is named "work-<work-id>" or "work-<work-id> (friendlyName)" for easy identification.
// The function returns immediately after spawning - the orchestrator runs in the tab.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func SpawnWorkOrchestrator(ctx context.Context, workID string, projectName string, workDir string, friendlyName string, w io.Writer) error {
	logging.Debug("SpawnWorkOrchestrator called", "workID", workID, "projectName", projectName, "workDir", workDir)
	sessionName := SessionNameForProject(projectName)
	tabName := FormatTabName("work", workID, friendlyName)
	zc := zellij.New()

	// Ensure session exists
	logging.Debug("SpawnWorkOrchestrator ensuring session exists", "sessionName", sessionName)
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		logging.Error("SpawnWorkOrchestrator EnsureSession failed", "sessionName", sessionName, "error", err)
		return err
	}

	// Build the orchestrate command with --work flag
	orchestrateCommand := fmt.Sprintf("co orchestrate --work %s", workID)

	// Check if tab already exists
	tabExists, err := zc.TabExists(ctx, sessionName, tabName)
	if err != nil {
		return fmt.Errorf("failed to check if tab exists: %w", err)
	}
	if tabExists {
		fmt.Fprintf(w, "Tab %s already exists, reusing...\n", tabName)

		// Switch to the existing tab
		if err := zc.SwitchToTab(ctx, sessionName, tabName); err != nil {
			fmt.Fprintf(w, "Warning: failed to switch to existing tab: %v\n", err)
		}

		// Send Ctrl+C to terminate any running process
		zc.SendCtrlC(ctx, sessionName)
		time.Sleep(zc.CtrlCDelay)

		// Clear the line and execute the command
		if err := zc.ClearAndExecute(ctx, sessionName, orchestrateCommand); err != nil {
			return fmt.Errorf("failed to execute orchestrate command: %w", err)
		}
	} else {
		// Create a new tab
		fmt.Fprintf(w, "Creating tab: %s in session %s\n", tabName, sessionName)
		if err := zc.CreateTab(ctx, sessionName, tabName, workDir); err != nil {
			return fmt.Errorf("failed to create tab: %w", err)
		}

		// Switch to the new tab
		if err := zc.SwitchToTab(ctx, sessionName, tabName); err != nil {
			fmt.Fprintf(w, "Warning: failed to switch to tab: %v\n", err)
		}

		// Execute the orchestrate command
		fmt.Fprintf(w, "Executing: %s\n", orchestrateCommand)
		if err := zc.ExecuteCommand(ctx, sessionName, orchestrateCommand); err != nil {
			return fmt.Errorf("failed to execute orchestrate command: %w", err)
		}
	}

	logging.Debug("SpawnWorkOrchestrator completed successfully", "workID", workID, "sessionName", sessionName, "tabName", tabName)
	fmt.Fprintf(w, "Work orchestrator spawned in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}

// OpenConsole creates a zellij tab with a shell in the work's worktree.
// The tab is named "console-<work-id>" or "console-<work-id> (friendlyName)" for easy identification.
// The hooksEnv parameter contains environment variables to export (format: "KEY=value").
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func OpenConsole(ctx context.Context, workID string, projectName string, workDir string, friendlyName string, hooksEnv []string, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := FormatTabName("console", workID, friendlyName)
	zc := zellij.New()

	// Ensure session exists
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		return err
	}

	// Check if tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, tabName)
	if tabExists {
		fmt.Fprintf(w, "Console tab %s already exists, switching to it...\n", tabName)
		// Switch to the existing tab
		if err := zc.SwitchToTab(ctx, sessionName, tabName); err != nil {
			return fmt.Errorf("failed to switch to existing tab: %w", err)
		}
	} else {
		// Create a new tab
		fmt.Fprintf(w, "Creating console tab: %s in session %s with cwd=%s\n", tabName, sessionName, workDir)
		if err := zc.CreateTab(ctx, sessionName, tabName, workDir); err != nil {
			return fmt.Errorf("failed to create tab: %w", err)
		}

		// Send cd command and set env vars to ensure proper work context
		// (zellij --cwd may not always work as expected)
		if workDir != "" {
			// Build export commands for hooks env
			var exports []string
			for _, env := range hooksEnv {
				exports = append(exports, fmt.Sprintf("export %s", env))
			}
			var cmd string
			if len(exports) > 0 {
				cmd = fmt.Sprintf("cd %q && %s", workDir, strings.Join(exports, " && "))
			} else {
				cmd = fmt.Sprintf("cd %q", workDir)
			}
			if err := zc.ExecuteCommand(ctx, sessionName, cmd); err != nil {
				fmt.Fprintf(w, "Warning: failed to initialize console: %v\n", err)
			}
		}

		// Switch to the new tab
		if err := zc.SwitchToTab(ctx, sessionName, tabName); err != nil {
			fmt.Fprintf(w, "Warning: failed to switch to tab: %v\n", err)
		}
	}

	fmt.Fprintf(w, "Console opened in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}

// OpenClaudeSession creates a zellij tab with an interactive Claude Code session in the work's worktree.
// The tab is named "claude-<work-id>" or "claude-<work-id> (friendlyName)" for easy identification.
// The hooksEnv parameter contains environment variables to export (format: "KEY=value").
// The config parameter controls Claude settings like --dangerously-skip-permissions.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func OpenClaudeSession(ctx context.Context, workID string, projectName string, workDir string, friendlyName string, hooksEnv []string, cfg *project.Config, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := FormatTabName("claude", workID, friendlyName)
	zc := zellij.New()

	// Ensure session exists
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		return err
	}

	// Build the claude command based on config
	claudeCmd := "claude"
	if cfg != nil && cfg.Claude.ShouldSkipPermissions() {
		claudeCmd = "claude --dangerously-skip-permissions"
	}

	// Check if tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, tabName)
	if tabExists {
		fmt.Fprintf(w, "Claude session tab %s already exists, switching to it...\n", tabName)
		// Switch to the existing tab
		if err := zc.SwitchToTab(ctx, sessionName, tabName); err != nil {
			return fmt.Errorf("failed to switch to existing tab: %w", err)
		}
	} else {
		// Create a new tab
		fmt.Fprintf(w, "Creating Claude session tab: %s in session %s with cwd=%s\n", tabName, sessionName, workDir)
		if err := zc.CreateTab(ctx, sessionName, tabName, workDir); err != nil {
			return fmt.Errorf("failed to create tab: %w", err)
		}

		// Send cd command and set env vars to ensure proper work context
		// (zellij --cwd may not always work as expected)
		// Build export commands for hooks env
		var exports []string
		for _, env := range hooksEnv {
			exports = append(exports, fmt.Sprintf("export %s", env))
		}
		var cmd string
		if len(exports) > 0 {
			cmd = fmt.Sprintf("cd %q && %s && %s", workDir, strings.Join(exports, " && "), claudeCmd)
		} else {
			cmd = fmt.Sprintf("cd %q && %s", workDir, claudeCmd)
		}
		if err := zc.ExecuteCommand(ctx, sessionName, cmd); err != nil {
			fmt.Fprintf(w, "Warning: failed to initialize Claude session: %v\n", err)
		}

		// Switch to the new tab
		if err := zc.SwitchToTab(ctx, sessionName, tabName); err != nil {
			fmt.Fprintf(w, "Warning: failed to switch to tab: %v\n", err)
		}
	}

	fmt.Fprintf(w, "Claude session opened in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}

// PlanTabName returns the zellij tab name for a bead's planning session.
// This mirrors db.TabNameForBead but is in the claude package to avoid circular imports.
func PlanTabName(beadID string) string {
	return fmt.Sprintf("plan-%s", beadID)
}

// SpawnPlanSession creates a zellij tab and runs the plan command for a bead.
// The tab is named "plan-<bead-id>" for easy identification.
// The function returns immediately after spawning - the plan session runs in the tab.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func SpawnPlanSession(ctx context.Context, beadID string, projectName string, mainRepoPath string, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := PlanTabName(beadID)
	zc := zellij.New()

	// Ensure session exists
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		return err
	}

	// Build the plan command
	planCommand := fmt.Sprintf("co plan %s", beadID)

	// Check if tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, tabName)
	if tabExists {
		fmt.Fprintf(w, "Tab %s already exists, terminating and recreating...\n", tabName)

		// Terminate and close the existing tab
		if err := zc.TerminateAndCloseTab(ctx, sessionName, tabName); err != nil {
			fmt.Fprintf(w, "Warning: failed to terminate existing tab: %v\n", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Create a new tab
	fmt.Fprintf(w, "Creating tab: %s in session %s\n", tabName, sessionName)
	if err := zc.CreateTab(ctx, sessionName, tabName, mainRepoPath); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	// Switch to the new tab
	time.Sleep(200 * time.Millisecond)
	if err := zc.SwitchToTab(ctx, sessionName, tabName); err != nil {
		fmt.Fprintf(w, "Warning: failed to switch to tab: %v\n", err)
	}

	// Execute the plan command
	fmt.Fprintf(w, "Executing: %s\n", planCommand)
	time.Sleep(200 * time.Millisecond)
	if err := zc.ExecuteCommand(ctx, sessionName, planCommand); err != nil {
		return fmt.Errorf("failed to execute plan command: %w", err)
	}

	fmt.Fprintf(w, "Plan session spawned in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}

// EnsureWorkOrchestrator checks if a work orchestrator tab exists and spawns one if not.
// This is used for resilience - if the orchestrator crashes or is killed, it can be restarted.
// Returns true if the orchestrator was spawned, false if it was already running.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
// The database parameter is used to check orchestrator heartbeat status.
func EnsureWorkOrchestrator(ctx context.Context, database *db.DB, workID string, projectName string, workDir string, friendlyName string, w io.Writer) (bool, error) {
	sessionName := SessionNameForProject(projectName)
	tabName := FormatTabName("work", workID, friendlyName)

	// Check if the tab already exists
	if TabExists(ctx, sessionName, tabName) {
		// Tab exists, but we need to check if the orchestrator is actually running
		// Check for orchestrator heartbeat in database
		if alive, err := database.IsOrchestratorAlive(ctx, workID, db.DefaultStalenessThreshold); err == nil && alive {
			// Orchestrator is alive
			fmt.Fprintf(w, "Work orchestrator tab %s already exists and orchestrator is alive\n", tabName)
			return false, nil
		}

		// Tab exists but orchestrator is dead - restart
		fmt.Fprintf(w, "Work orchestrator tab %s exists but orchestrator is dead - restarting...\n", tabName)

		// Try to close the dead tab first
		zc := zellij.New()
		if err := zc.SwitchToTab(ctx, sessionName, tabName); err == nil {
			// Send Ctrl+C to ensure any hung process is terminated
			zc.SendCtrlC(ctx, sessionName)
			time.Sleep(zc.CtrlCDelay)
			// Close the tab
			zc.CloseTab(ctx, sessionName)
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Spawn the orchestrator
	if err := SpawnWorkOrchestrator(ctx, workID, projectName, workDir, friendlyName, w); err != nil {
		return false, err
	}

	return true, nil
}
