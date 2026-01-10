package claude

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/newhook/co/internal/db"
)

// SessionNameForProject returns the zellij session name for a specific project.
func SessionNameForProject(projectName string) string {
	return fmt.Sprintf("co-%s", projectName)
}

// RunInProject invokes Claude Code with the given prompt using project-specific session naming.
func RunInProject(ctx context.Context, database *db.DB, beadID, prompt string, workDir, projectName string) error {
	sessionName := SessionNameForProject(projectName)

	// Ensure session exists
	if err := ensureSession(ctx, sessionName); err != nil {
		return err
	}

	// Check if pane with this bead name already exists
	if paneExists(ctx, sessionName, beadID) {
		fmt.Printf("Pane %s already exists, skipping claude launch\n", beadID)
	} else {
		// Run Claude in a new pane in the session
		runArgs := []string{"-s", sessionName, "run", "--name", beadID, "--cwd", workDir, "--", "claude", "--dangerously-skip-permissions"}
		fmt.Printf("Running: zellij %s\n", strings.Join(runArgs, " "))
		runCmd := exec.CommandContext(ctx, "zellij", runArgs...)
		if err := runCmd.Run(); err != nil {
			return fmt.Errorf("failed to run claude in zellij pane: %w", err)
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
		return fmt.Errorf("failed to send prompt: %w", err)
	}

	// Wait a moment for the text to be received
	time.Sleep(500 * time.Millisecond)

	// Send Enter to submit
	enterArgs := []string{"-s", sessionName, "action", "write", "13"}
	fmt.Printf("Running: zellij %s\n", strings.Join(enterArgs, " "))
	enterCmd := exec.CommandContext(ctx, "zellij", enterArgs...)
	if err := enterCmd.Run(); err != nil {
		return fmt.Errorf("failed to send enter: %w", err)
	}

	// Send another Enter just to be sure
	time.Sleep(100 * time.Millisecond)
	exec.CommandContext(ctx, "zellij", enterArgs...).Run()

	fmt.Println("Prompt sent to Claude")

	// Monitor for completion via database polling
	fmt.Printf("Polling database for completion of bead: %s\n", beadID)
	for {
		time.Sleep(2 * time.Second)

		completed, err := database.IsCompleted(beadID)
		if err != nil {
			fmt.Printf("Warning: failed to check completion status: %v\n", err)
			continue
		}

		if completed {
			fmt.Println("Bead marked as completed!")

			// Send /exit to close Claude
			time.Sleep(500 * time.Millisecond)
			exitArgs := []string{"-s", sessionName, "action", "write-chars", "/exit"}
			exec.CommandContext(ctx, "zellij", exitArgs...).Run()
			time.Sleep(100 * time.Millisecond)
			exec.CommandContext(ctx, "zellij", "-s", sessionName, "action", "write", "13").Run()

			fmt.Println("Sent /exit to Claude")
			break
		}
	}

	return nil
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
