package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Run invokes Claude Code with the given prompt in the specified working directory.
// Output is streamed to stdout/stderr. Returns an error on non-zero exit.
func Run(ctx context.Context, prompt string, workDir string) error {
	cmd := exec.CommandContext(ctx, "claude", "--dangerously-skip-permissions", "-p", prompt)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude invocation failed: %w", err)
	}
	return nil
}
