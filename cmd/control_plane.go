package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/newhook/co/internal/control"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

// ControlPlaneTabName is the name of the control plane tab in zellij
// Deprecated: Use control.TabName instead
const ControlPlaneTabName = control.TabName

var controlCmd = &cobra.Command{
	Use:   "control",
	Short: "[Agent] Run the control plane for background task execution",
	Long: `[Agent Command - Spawned automatically by the system, not for direct user invocation]

The control plane runs as a long-lived process that watches for scheduled tasks
across all works and executes them with retry support. It runs in a dedicated
zellij tab named "control" and is spawned automatically.`,
	Hidden: true,
	RunE:   runControlPlane,
}

var controlRoot string

func init() {
	rootCmd.AddCommand(controlCmd)
	controlCmd.Flags().StringVar(&controlRoot, "root", "", "Project root directory")
}

func runControlPlane(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	proj, err := project.Find(ctx, controlRoot)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	// Apply hooks.env to current process - inherited by child processes
	applyHooksEnv(proj.Config.Hooks.Env)

	fmt.Println("=== Control Plane Started ===")
	fmt.Printf("Project: %s\n", proj.Config.Project.Name)
	fmt.Println("Watching for scheduled tasks across all works...")

	// Start the control plane loop
	return control.RunControlPlaneLoop(ctx, proj)
}

// ScheduleDestroyWorktree schedules a worktree destruction task for the control plane.
// Delegates to internal/control.ScheduleDestroyWorktree.
func ScheduleDestroyWorktree(ctx context.Context, proj *project.Project, workID string) error {
	return control.ScheduleDestroyWorktree(ctx, proj, workID)
}

// SpawnControlPlane spawns the control plane in a zellij tab.
// Delegates to internal/control.SpawnControlPlane.
func SpawnControlPlane(ctx context.Context, projectName string, projectRoot string, w io.Writer) error {
	return control.SpawnControlPlane(ctx, projectName, projectRoot, w)
}

// EnsureControlPlane ensures the control plane is running, spawning it if needed.
// Delegates to internal/control.EnsureControlPlane.
func EnsureControlPlane(ctx context.Context, projectName string, projectRoot string, w io.Writer) (bool, error) {
	return control.EnsureControlPlane(ctx, projectName, projectRoot, w)
}

// IsControlPlaneRunning checks if the control plane is running for a specific project.
// Delegates to internal/control.IsControlPlaneRunning.
func IsControlPlaneRunning(ctx context.Context, projectRoot string) bool {
	return control.IsControlPlaneRunning(ctx, projectRoot)
}

// TriggerPRFeedbackCheck schedules an immediate PR feedback check.
// Delegates to internal/control.TriggerPRFeedbackCheck.
func TriggerPRFeedbackCheck(ctx context.Context, proj *project.Project, workID string) error {
	return control.TriggerPRFeedbackCheck(ctx, proj, workID)
}
