// Package zellij provides a client for interacting with the zellij terminal multiplexer.
// It abstracts session, tab, and pane management operations into a type-safe API.
package zellij

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// CurrentSessionName returns the name of the zellij session we're currently inside,
// or empty string if not inside a zellij session.
func CurrentSessionName() string {
	return os.Getenv("ZELLIJ_SESSION_NAME")
}

// IsInsideSession returns true if we're running inside a zellij session.
func IsInsideSession() bool {
	return CurrentSessionName() != ""
}

// IsInsideTargetSession returns true if we're inside the specified session.
func IsInsideTargetSession(session string) bool {
	return CurrentSessionName() == session
}

// Client provides methods for interacting with zellij sessions, tabs, and panes.
type Client struct {
	// Timeouts for various operations
	TabCreateDelay   time.Duration
	CtrlCDelay       time.Duration
	CommandDelay     time.Duration
	SessionStartWait time.Duration
}

// New creates a new zellij client with default configuration.
func New() *Client {
	return &Client{
		TabCreateDelay:   500 * time.Millisecond,
		CtrlCDelay:       500 * time.Millisecond,
		CommandDelay:     100 * time.Millisecond,
		SessionStartWait: 1 * time.Second,
	}
}

// sessionArgs returns the appropriate session arguments for zellij commands.
// If we're inside the target session, returns empty slice (use local actions).
// Otherwise returns ["-s", session] to target the specific session.
func sessionArgs(session string) []string {
	if IsInsideTargetSession(session) {
		return nil
	}
	return []string{"-s", session}
}

// ASCII codes for special keys
const (
	ASCIICtrlC = 3  // Ctrl+C (interrupt)
	ASCIIEnter = 13 // Enter key
)

// Session management

// ListSessions returns a list of all zellij session names.
func (c *Client) ListSessions(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "zellij", "list-sessions", "-s")
	output, err := cmd.Output()
	if err != nil {
		// No sessions or zellij not running
		return nil, nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var sessions []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			// Session names may have additional info, take first field
			parts := strings.Fields(line)
			if len(parts) > 0 {
				sessions = append(sessions, parts[0])
			}
		}
	}
	return sessions, nil
}

// SessionExists checks if a session with the given name exists.
func (c *Client) SessionExists(ctx context.Context, name string) (bool, error) {
	sessions, err := c.ListSessions(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(sessions, name), nil
}

// CreateSession creates a new zellij session with the given name.
// The session is started in the background (detached) using zellij attach -b.
func (c *Client) CreateSession(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "zellij", "attach", "-b", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}
	// Give it time to start
	time.Sleep(c.SessionStartWait)
	return nil
}

// EnsureSession creates a session if it doesn't already exist.
func (c *Client) EnsureSession(ctx context.Context, name string) error {
	exists, err := c.SessionExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return c.CreateSession(ctx, name)
}

// Tab management

// CreateTab creates a new tab in the specified session.
func (c *Client) CreateTab(ctx context.Context, session, name, cwd string) error {
	args := append(sessionArgs(session), "action", "new-tab", "--name", name)
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}
	time.Sleep(c.TabCreateDelay)
	return nil
}

// SwitchToTab switches to a tab by name in the specified session.
func (c *Client) SwitchToTab(ctx context.Context, session, name string) error {
	args := append(sessionArgs(session), "action", "go-to-tab-name", name)
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to switch to tab: %w", err)
	}
	return nil
}

// QueryTabNames returns a list of all tab names in the specified session.
func (c *Client) QueryTabNames(ctx context.Context, session string) ([]string, error) {
	args := append(sessionArgs(session), "action", "query-tab-names")
	cmd := exec.CommandContext(ctx, "zellij", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query tab names: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var tabs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			tabs = append(tabs, line)
		}
	}
	return tabs, nil
}

// TabExists checks if a tab with the given name exists in the session.
func (c *Client) TabExists(ctx context.Context, session, name string) (bool, error) {
	tabs, err := c.QueryTabNames(ctx, session)
	if err != nil {
		// If we can't query tabs, assume it doesn't exist
		return false, nil
	}
	return slices.Contains(tabs, name), nil
}

// CloseTab closes the current tab in the specified session.
func (c *Client) CloseTab(ctx context.Context, session string) error {
	args := append(sessionArgs(session), "action", "close-tab")
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to close tab: %w", err)
	}
	return nil
}

// Pane input control

// WriteASCII writes an ASCII code to the current pane in the session.
// Use this for control characters like Ctrl+C (3), Enter (13), etc.
func (c *Client) WriteASCII(ctx context.Context, session string, code int) error {
	args := append(sessionArgs(session), "action", "write", fmt.Sprintf("%d", code))
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write ASCII code: %w", err)
	}
	return nil
}

// WriteChars writes text to the current pane in the session.
func (c *Client) WriteChars(ctx context.Context, session, text string) error {
	args := append(sessionArgs(session), "action", "write-chars", text)
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write chars: %w", err)
	}
	return nil
}

// SendCtrlC sends Ctrl+C (interrupt signal) to the current pane.
func (c *Client) SendCtrlC(ctx context.Context, session string) error {
	return c.WriteASCII(ctx, session, ASCIICtrlC)
}

// SendEnter sends the Enter key to the current pane.
func (c *Client) SendEnter(ctx context.Context, session string) error {
	return c.WriteASCII(ctx, session, ASCIIEnter)
}

// ExecuteCommand writes a command and sends Enter to execute it.
func (c *Client) ExecuteCommand(ctx context.Context, session, cmd string) error {
	if err := c.WriteChars(ctx, session, cmd); err != nil {
		return fmt.Errorf("failed to write command: %w", err)
	}
	if err := c.SendEnter(ctx, session); err != nil {
		return fmt.Errorf("failed to send enter: %w", err)
	}
	return nil
}

// High-level operations

// TerminateProcess sends Ctrl+C twice with a delay to ensure process termination.
// This handles cases where the first Ctrl+C might be caught by a signal handler.
func (c *Client) TerminateProcess(ctx context.Context, session string) error {
	if err := c.SendCtrlC(ctx, session); err != nil {
		return err
	}
	time.Sleep(c.CtrlCDelay)
	if err := c.SendCtrlC(ctx, session); err != nil {
		return err
	}
	time.Sleep(c.CtrlCDelay)
	return nil
}

// ClearAndExecute clears the current line and executes a command.
func (c *Client) ClearAndExecute(ctx context.Context, session, cmd string) error {
	if err := c.WriteChars(ctx, session, "clear"); err != nil {
		return err
	}
	time.Sleep(c.CommandDelay)
	if err := c.SendEnter(ctx, session); err != nil {
		return err
	}
	time.Sleep(c.CommandDelay)
	return c.ExecuteCommand(ctx, session, cmd)
}

// TerminateAndCloseTab terminates any running process in a tab and closes it.
// It first switches to the tab, sends Ctrl+C to terminate, then closes the tab.
func (c *Client) TerminateAndCloseTab(ctx context.Context, session, tabName string) error {
	// Switch to the tab
	if err := c.SwitchToTab(ctx, session, tabName); err != nil {
		return fmt.Errorf("failed to switch to tab: %w", err)
	}

	// Terminate any running process
	if err := c.TerminateProcess(ctx, session); err != nil {
		return fmt.Errorf("failed to terminate process: %w", err)
	}

	// Close the tab
	if err := c.CloseTab(ctx, session); err != nil {
		return fmt.Errorf("failed to close tab: %w", err)
	}

	return nil
}

// Floating pane operations

// RunFloating runs a command in a new floating pane in the specified session.
// The name parameter sets the pane name for identification.
// The cwd parameter sets the working directory.
func (c *Client) RunFloating(ctx context.Context, session, name, cwd string, command ...string) error {
	args := append(sessionArgs(session), "run", "--floating", "--name", name)
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	args = append(args, "--")
	args = append(args, command...)
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to run floating pane: %w", err)
	}
	return nil
}

// ToggleFloatingPanes toggles the visibility of floating panes in the session.
func (c *Client) ToggleFloatingPanes(ctx context.Context, session string) error {
	args := append(sessionArgs(session), "action", "toggle-floating-panes")
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to toggle floating panes: %w", err)
	}
	return nil
}

// Run runs a command in a new pane in the specified session.
// The name parameter sets the pane name for identification.
// The cwd parameter sets the working directory.
func (c *Client) Run(ctx context.Context, session, name, cwd string, command ...string) error {
	args := append(sessionArgs(session), "run", "--name", name)
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	args = append(args, "--")
	args = append(args, command...)
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to run pane: %w", err)
	}
	return nil
}
