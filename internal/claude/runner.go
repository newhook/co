package claude

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"

	"github.com/newhook/co/internal/beads"
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

var (
	estimateTmpl            = template.Must(template.New("estimate").Parse(estimateTemplateText))
	taskTmpl                = template.Must(template.New("task").Parse(taskTemplateText))
	prTmpl                  = template.Must(template.New("pr").Parse(prTemplateText))
	reviewTmpl              = template.Must(template.New("review").Parse(reviewTemplateText))
	updatePRDescriptionTmpl = template.Must(template.New("update-pr-description").Parse(updatePRDescriptionTemplateText))
)

// SessionNameForProject returns the zellij session name for a specific project.
func SessionNameForProject(projectName string) string {
	return fmt.Sprintf("co-%s", projectName)
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

// SpawnWorkOrchestrator creates a zellij tab and runs the orchestrate command for a work unit.
// The tab is named "work-<work-id>" for easy identification.
// The function returns immediately after spawning - the orchestrator runs in the tab.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func SpawnWorkOrchestrator(ctx context.Context, workID string, projectName string, workDir string, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := fmt.Sprintf("work-%s", workID)
	zc := zellij.New()

	// Ensure session exists
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		return err
	}

	// Build the orchestrate command with --work flag
	orchestrateCommand := fmt.Sprintf("co orchestrate --work %s", workID)

	// Check if tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, tabName)
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

	fmt.Fprintf(w, "Work orchestrator spawned in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}

// OpenConsole creates a zellij tab with a shell in the work's worktree.
// The tab is named "console-<work-id>" for easy identification.
// The hooksEnv parameter contains environment variables to export (format: "KEY=value").
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func OpenConsole(ctx context.Context, workID string, projectName string, workDir string, hooksEnv []string, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := fmt.Sprintf("console-%s", workID)
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
// The tab is named "claude-<work-id>" for easy identification.
// The hooksEnv parameter contains environment variables to export (format: "KEY=value").
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func OpenClaudeSession(ctx context.Context, workID string, projectName string, workDir string, hooksEnv []string, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := fmt.Sprintf("claude-%s", workID)
	zc := zellij.New()

	// Ensure session exists
	if err := zc.EnsureSession(ctx, sessionName); err != nil {
		return err
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
		if workDir != "" {
			// Build export commands for hooks env
			var exports []string
			for _, env := range hooksEnv {
				exports = append(exports, fmt.Sprintf("export %s", env))
			}
			var cmd string
			if len(exports) > 0 {
				cmd = fmt.Sprintf("cd %q && %s && claude", workDir, strings.Join(exports, " && "))
			} else {
				cmd = fmt.Sprintf("cd %q && claude", workDir)
			}
			if err := zc.ExecuteCommand(ctx, sessionName, cmd); err != nil {
				fmt.Fprintf(w, "Warning: failed to initialize Claude session: %v\n", err)
			}
		} else {
			// No workDir specified, just launch claude
			if err := zc.ExecuteCommand(ctx, sessionName, "claude"); err != nil {
				fmt.Fprintf(w, "Warning: failed to launch Claude: %v\n", err)
			}
		}

		// Switch to the new tab
		if err := zc.SwitchToTab(ctx, sessionName, tabName); err != nil {
			fmt.Fprintf(w, "Warning: failed to switch to tab: %v\n", err)
		}
	}

	fmt.Fprintf(w, "Claude session opened in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}

// EnsureWorkOrchestrator checks if a work orchestrator tab exists and spawns one if not.
// This is used for resilience - if the orchestrator crashes or is killed, it can be restarted.
// Returns true if the orchestrator was spawned, false if it was already running.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
func EnsureWorkOrchestrator(ctx context.Context, workID string, projectName string, workDir string, w io.Writer) (bool, error) {
	sessionName := SessionNameForProject(projectName)
	tabName := fmt.Sprintf("work-%s", workID)

	// Check if the tab already exists
	if TabExists(ctx, sessionName, tabName) {
		fmt.Fprintf(w, "Work orchestrator tab %s already exists\n", tabName)
		return false, nil
	}

	// Spawn the orchestrator
	if err := SpawnWorkOrchestrator(ctx, workID, projectName, workDir, w); err != nil {
		return false, err
	}

	return true, nil
}
