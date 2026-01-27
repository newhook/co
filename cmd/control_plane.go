package cmd

import (
	"fmt"
	"os"

	"github.com/newhook/co/internal/control"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/procmon"
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

	// Set BEADS_DIR so bd commands work in any spawned processes
	os.Setenv("BEADS_DIR", proj.BeadsPath())

	// Register this control plane process for heartbeat monitoring
	procManager := procmon.NewManager(proj.DB, db.DefaultHeartbeatInterval)
	if err := procManager.RegisterControlPlane(ctx); err != nil {
		return fmt.Errorf("failed to register control plane: %w", err)
	}
	defer procManager.Stop()

	fmt.Println("=== Control Plane Started ===")
	fmt.Printf("Project: %s\n", proj.Config.Project.Name)
	fmt.Println("Watching for scheduled tasks across all works...")

	// Start the control plane loop
	return control.RunControlPlaneLoop(ctx, proj, procManager)
}
