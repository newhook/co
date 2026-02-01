package claude

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/newhook/co/internal/beads"
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

//go:embed templates/log_analysis.tmpl
var logAnalysisTemplateText string

var (
	estimateTmpl            = template.Must(template.New("estimate").Parse(estimateTemplateText))
	taskTmpl                = template.Must(template.New("task").Parse(taskTemplateText))
	prTmpl                  = template.Must(template.New("pr").Parse(prTemplateText))
	reviewTmpl              = template.Must(template.New("review").Parse(reviewTemplateText))
	updatePRDescriptionTmpl = template.Must(template.New("update-pr-description").Parse(updatePRDescriptionTemplateText))
	planTmpl                = template.Must(template.New("plan").Parse(planTemplateText))
	logAnalysisTmpl         = template.Must(template.New("log_analysis").Parse(logAnalysisTemplateText))
)

// SessionNameForProject returns the zellij session name for a specific project.
// Deprecated: Use project.SessionNameForProject instead.
func SessionNameForProject(projectName string) string {
	return project.SessionNameForProject(projectName)
}

// FormatTabName formats a tab name with an optional friendly name.
// Deprecated: Use project.FormatTabName instead.
func FormatTabName(prefix, workID, friendlyName string) string {
	return project.FormatTabName(prefix, workID, friendlyName)
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

// LogAnalysisParams contains parameters for building a log analysis prompt.
type LogAnalysisParams struct {
	TaskID       string
	WorkID       string
	BranchName   string
	RootIssueID  string
	WorkflowName string
	JobName      string
	LogContent   string
}

// BuildLogAnalysisPrompt builds a prompt for Claude-based CI log analysis.
func BuildLogAnalysisPrompt(params LogAnalysisParams) string {
	var buf bytes.Buffer
	if err := logAnalysisTmpl.Execute(&buf, params); err != nil {
		// Fallback to simple string if template execution fails
		return fmt.Sprintf("Log analysis task %s for work %s", params.TaskID, params.WorkID)
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

// OpenConsole creates a zellij tab with a shell in the work's worktree.
// The tab is named "console-<work-id>" or "console-<work-id> (friendlyName)" for easy identification.
// The hooksEnv parameter contains environment variables to export (format: "KEY=value").
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
//
// IMPORTANT: The zellij session must already exist before calling this function.
// Callers should use control.InitializeSession or control.EnsureControlPlane to ensure
// the session exists with the control plane running.
func OpenConsole(ctx context.Context, workID string, projectName string, workDir string, friendlyName string, hooksEnv []string, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := FormatTabName("console", workID, friendlyName)
	zc := zellij.New()

	// Verify session exists - callers must initialize it with control plane
	exists, err := zc.SessionExists(ctx, sessionName)
	if err != nil {
		return fmt.Errorf("failed to check session existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("zellij session %s does not exist - call control.InitializeSession first", sessionName)
	}

	// Check if tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, tabName)
	if tabExists {
		fmt.Fprintf(w, "Console tab %s already exists\n", tabName)
		return nil
	}

	// Build shell command with exports if needed
	// Use user's preferred shell from $SHELL, default to bash
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	shellName := filepath.Base(shell)

	var command string
	var args []string
	if len(hooksEnv) > 0 {
		var exports []string
		for _, env := range hooksEnv {
			exports = append(exports, fmt.Sprintf("export %s", env))
		}
		// Use shell -c to export vars and then exec shell for interactive shell
		shellCmd := fmt.Sprintf("%s && exec %s", strings.Join(exports, " && "), shell)
		command = shell
		args = []string{"-c", shellCmd}
	} else {
		command = shell
		args = nil
	}

	// Create tab with shell using layout approach
	fmt.Fprintf(w, "Creating console tab: %s in session %s\n", tabName, sessionName)
	if err := zc.CreateTabWithCommand(ctx, sessionName, tabName, workDir, command, args, shellName); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	fmt.Fprintf(w, "Console opened in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}

// OpenClaudeSession creates a zellij tab with an interactive Claude Code session in the work's worktree.
// The tab is named "claude-<work-id>" or "claude-<work-id> (friendlyName)" for easy identification.
// The hooksEnv parameter contains environment variables to export (format: "KEY=value").
// The config parameter controls Claude settings like --dangerously-skip-permissions.
// Progress messages are written to the provided writer. Pass io.Discard to suppress output.
//
// IMPORTANT: The zellij session must already exist before calling this function.
// Callers should use control.InitializeSession or control.EnsureControlPlane to ensure
// the session exists with the control plane running.
func OpenClaudeSession(ctx context.Context, workID string, projectName string, workDir string, friendlyName string, hooksEnv []string, cfg *project.Config, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := FormatTabName("claude", workID, friendlyName)
	zc := zellij.New()

	// Verify session exists - callers must initialize it with control plane
	exists, err := zc.SessionExists(ctx, sessionName)
	if err != nil {
		return fmt.Errorf("failed to check session existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("zellij session %s does not exist - call control.InitializeSession first", sessionName)
	}

	// Check if tab already exists
	tabExists, _ := zc.TabExists(ctx, sessionName, tabName)
	if tabExists {
		fmt.Fprintf(w, "Claude session tab %s already exists\n", tabName)
		return nil
	}

	// Build the claude command with exports if needed
	var claudeArgs []string
	if cfg != nil && cfg.Claude.ShouldSkipPermissions() {
		claudeArgs = []string{"--dangerously-skip-permissions"}
	}

	// If we have environment variables, use bash -c to export them
	var command string
	var args []string
	if len(hooksEnv) > 0 {
		var exports []string
		for _, env := range hooksEnv {
			exports = append(exports, fmt.Sprintf("export %s", env))
		}
		claudeCmd := "claude"
		if len(claudeArgs) > 0 {
			claudeCmd = "claude " + strings.Join(claudeArgs, " ")
		}
		shellCmd := fmt.Sprintf("%s && %s", strings.Join(exports, " && "), claudeCmd)
		command = "bash"
		args = []string{"-c", shellCmd}
	} else {
		command = "claude"
		args = claudeArgs
	}

	// Create tab with command using layout approach
	fmt.Fprintf(w, "Creating Claude session tab: %s in session %s\n", tabName, sessionName)
	if err := zc.CreateTabWithCommand(ctx, sessionName, tabName, workDir, command, args, "claude"); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
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
//
// IMPORTANT: The zellij session must already exist before calling this function.
// Callers should use control.InitializeSession or control.EnsureControlPlane to ensure
// the session exists with the control plane running.
func SpawnPlanSession(ctx context.Context, beadID string, projectName string, mainRepoPath string, w io.Writer) error {
	sessionName := SessionNameForProject(projectName)
	tabName := PlanTabName(beadID)
	zc := zellij.New()

	// Verify session exists - callers must initialize it with control plane
	exists, err := zc.SessionExists(ctx, sessionName)
	if err != nil {
		return fmt.Errorf("failed to check session existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("zellij session %s does not exist - call control.InitializeSession first", sessionName)
	}

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

	// Create a new tab with the plan command using a layout
	fmt.Fprintf(w, "Creating tab: %s in session %s\n", tabName, sessionName)
	if err := zc.CreateTabWithCommand(ctx, sessionName, tabName, mainRepoPath, "co", []string{"plan", beadID}, "planning"); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	fmt.Fprintf(w, "Plan session spawned in zellij session %s, tab %s\n", sessionName, tabName)
	return nil
}
