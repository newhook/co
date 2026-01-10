package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	sessionName = "autoclaude"
)

// Run invokes Claude Code with the given prompt in the specified working directory.
// Uses zellij to run Claude in a proper terminal environment.
func Run(ctx context.Context, beadID, prompt string, workDir string) error {
	// Ensure autoclaude session exists
	if err := ensureSession(ctx); err != nil {
		return err
	}

	// Check if pane with this bead name already exists
	if paneExists(ctx, beadID) {
		fmt.Printf("Pane %s already exists, skipping claude launch\n", beadID)
	} else {
		// Run Claude in a new pane in the autoclaude session
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

	// Create completion file path
	completionFile := filepath.Join(os.TempDir(), fmt.Sprintf("ac-done-%s", beadID))
	os.Remove(completionFile) // Remove if exists from previous run

	// Send the prompt with completion file instruction
	fullPrompt := prompt + fmt.Sprintf("\n\nWhen you have fully completed this task, create the file: %s", completionFile)

	// Send text to the pane
	writeArgs := []string{"-s", sessionName, "action", "write-chars", fullPrompt}
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

	// Monitor for completion file
	fmt.Printf("Waiting for completion file: %s\n", completionFile)
	for {
		time.Sleep(2 * time.Second)

		if _, err := os.Stat(completionFile); err == nil {
			fmt.Println("Completion file found!")

			// Clean up the file
			os.Remove(completionFile)

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

func paneExists(ctx context.Context, paneName string) bool {
	// Use zellij action to list panes and check if one with this name exists
	cmd := exec.CommandContext(ctx, "zellij", "-s", sessionName, "action", "query-tab-names")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), paneName)
}

func ensureSession(ctx context.Context) error {
	// Check if session exists
	listCmd := exec.CommandContext(ctx, "zellij", "list-sessions")
	output, err := listCmd.Output()
	if err != nil {
		// No sessions, create one
		return createSession(ctx)
	}

	// Check if autoclaude session exists
	if strings.Contains(string(output), sessionName) {
		return nil
	}

	return createSession(ctx)
}

func createSession(ctx context.Context) error {
	// Start session detached
	cmd := exec.CommandContext(ctx, "zellij", "-s", sessionName)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}
	// Give it time to start
	time.Sleep(1 * time.Second)
	return nil
}
