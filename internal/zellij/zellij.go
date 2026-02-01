// Package zellij provides a client for interacting with the zellij terminal multiplexer.
// It abstracts session, tab, and pane management operations into a type-safe API.
package zellij

//go:generate moq -stub -out zellij_mock.go . SessionManager Session

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"text/template"
	"time"
)

//go:embed tab.kdl.tmpl
var tabLayoutTemplate string

// TabLayoutData contains the data for rendering a tab layout template.
type TabLayoutData struct {
	TabName  string
	PaneName string
	Command  string
	Args     []string
	Cwd      string
}

// CurrentSessionName returns the name of the zellij session we're currently inside,
// or empty string if not inside a zellij session.
func CurrentSessionName() string {
	return os.Getenv("ZELLIJ_SESSION_NAME")
}

// IsInsideTargetSession returns true if we're inside the specified session.
func IsInsideTargetSession(session string) bool {
	return CurrentSessionName() == session
}

// SessionManager defines the interface for managing zellij sessions.
// This abstraction enables testing without actual zellij commands.
//
// All session creation should go through EnsureSessionWithCommand to ensure
// every session starts with an initial tab. Direct session creation methods
// are intentionally excluded from this interface.
type SessionManager interface {
	SessionExists(ctx context.Context, name string) (bool, error)
	EnsureSessionWithCommand(ctx context.Context, sessionName, tabName, cwd, command string, args []string) (bool, error)

	// Session returns a Session interface bound to the specified session name.
	Session(name string) Session
}

// Session defines the interface for operations within a specific zellij session.
// Each Session instance is bound to a specific session name.
type Session interface {
	// Tab management
	CreateTab(ctx context.Context, name, cwd string) error
	CreateTabWithCommand(ctx context.Context, name, cwd, command string, args []string, paneName string) error
	SwitchToTab(ctx context.Context, name string) error
	QueryTabNames(ctx context.Context) ([]string, error)
	TabExists(ctx context.Context, name string) (bool, error)
	CloseTab(ctx context.Context) error

	// High-level operations
	TerminateProcess(ctx context.Context) error
	ClearAndExecute(ctx context.Context, cmd string) error
	TerminateAndCloseTab(ctx context.Context, tabName string) error

}

// Client provides methods for interacting with zellij sessions, tabs, and panes.
type Client struct {
	// Timeouts for various operations
	TabCreateDelay   time.Duration
	CtrlCDelay       time.Duration
	CommandDelay     time.Duration
	SessionStartWait time.Duration
}

// Compile-time checks.
var (
	_ SessionManager = (*Client)(nil)
	_ Session        = (*session)(nil)
)

// New creates a new zellij client with default configuration.
func New() *Client {
	return &Client{
		TabCreateDelay:   500 * time.Millisecond,
		CtrlCDelay:       500 * time.Millisecond,
		CommandDelay:     100 * time.Millisecond,
		SessionStartWait: 1 * time.Second,
	}
}

// session implements the Session interface for a specific zellij session.
type session struct {
	client *Client
	name   string
}

// Session returns a Session interface bound to the specified session name.
func (c *Client) Session(name string) Session {
	return &session{client: c, name: name}
}

// Session interface implementations for session struct

func (s *session) CreateTab(ctx context.Context, name, cwd string) error {
	return s.client.CreateTab(ctx, s.name, name, cwd)
}

func (s *session) CreateTabWithCommand(ctx context.Context, name, cwd, command string, args []string, paneName string) error {
	return s.client.CreateTabWithCommand(ctx, s.name, name, cwd, command, args, paneName)
}

func (s *session) SwitchToTab(ctx context.Context, name string) error {
	return s.client.SwitchToTab(ctx, s.name, name)
}

func (s *session) QueryTabNames(ctx context.Context) ([]string, error) {
	return s.client.QueryTabNames(ctx, s.name)
}

func (s *session) TabExists(ctx context.Context, name string) (bool, error) {
	return s.client.TabExists(ctx, s.name, name)
}

func (s *session) CloseTab(ctx context.Context) error {
	return s.client.CloseTab(ctx, s.name)
}

func (s *session) TerminateProcess(ctx context.Context) error {
	return s.client.TerminateProcess(ctx, s.name)
}

func (s *session) ClearAndExecute(ctx context.Context, cmd string) error {
	return s.client.ClearAndExecute(ctx, s.name, cmd)
}

func (s *session) TerminateAndCloseTab(ctx context.Context, tabName string) error {
	return s.client.TerminateAndCloseTab(ctx, s.name, tabName)
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

// IsSessionActive checks if a session exists and is active (not exited).
func (c *Client) IsSessionActive(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "zellij", "list-sessions", "-n")
	output, err := cmd.Output()
	if err != nil {
		return false, nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		if parts[0] == name {
			// Session found - check if it's exited
			return !strings.Contains(line, "EXITED"), nil
		}
	}
	return false, nil
}

// createSessionWithCommand creates a new zellij session with an initial tab running a command.
// Uses the same layout template as CreateTabWithCommand for consistency.
func (c *Client) createSessionWithCommand(ctx context.Context, sessionName, tabName, cwd, command string, args []string) error {
	// Render the tab layout template (same as CreateTabWithCommand)
	tmpl, err := template.New("tab").Parse(tabLayoutTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse tab layout template: %w", err)
	}

	data := TabLayoutData{
		TabName:  tabName,
		PaneName: tabName,
		Command:  command,
		Args:     args,
		Cwd:      cwd,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render tab layout template: %w", err)
	}

	// Write layout to temp file
	tmpFile, err := os.CreateTemp("", "zellij-session-*.kdl")
	if err != nil {
		return fmt.Errorf("failed to create temp layout file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.WriteString(buf.String()); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write session layout file: %w", err)
	}
	_ = tmpFile.Close()

	// Create session with the layout
	// Use --new-session-with-layout for detached session creation
	cmd := exec.CommandContext(ctx, "zellij", "-s", sessionName, "--new-session-with-layout", tmpFile.Name())
	// Detach immediately by not connecting stdin/stdout
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start zellij session: %w", err)
	}

	// Don't wait for it - let it run in background
	go func() { _ = cmd.Wait() }()

	// Give it time to start
	time.Sleep(c.SessionStartWait)
	return nil
}

// DeleteSession deletes a zellij session by name.
func (c *Client) DeleteSession(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "zellij", "delete-session", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete zellij session: %w", err)
	}
	return nil
}

// EnsureSessionWithCommand creates a session with an initial tab running a command if it doesn't exist,
// or deletes and recreates it if exited.
// Returns true if a new session was created, false if an existing session was reused.
func (c *Client) EnsureSessionWithCommand(ctx context.Context, sessionName, tabName, cwd, command string, args []string) (bool, error) {
	active, err := c.IsSessionActive(ctx, sessionName)
	if err != nil {
		return false, err
	}
	if active {
		return false, nil
	}

	// Check if session exists but is exited
	exists, err := c.SessionExists(ctx, sessionName)
	if err != nil {
		return false, err
	}
	if exists {
		// Session is exited - delete it first
		if err := c.DeleteSession(ctx, sessionName); err != nil {
			return false, fmt.Errorf("failed to delete exited session: %w", err)
		}
	}

	if err := c.createSessionWithCommand(ctx, sessionName, tabName, cwd, command, args); err != nil {
		return false, err
	}
	return true, nil
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

// CreateTabWithCommand creates a new tab that runs a specific command.
// This uses a layout file to ensure the command runs correctly even when
// called from outside the zellij session.
// The paneName parameter sets the pane title (optional, empty string uses command as title).
func (c *Client) CreateTabWithCommand(ctx context.Context, session, name, cwd, command string, args []string, paneName string) error {
	// Parse and render the tab layout template
	tmpl, err := template.New("tab").Parse(tabLayoutTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse tab layout template: %w", err)
	}

	data := TabLayoutData{
		TabName:  name,
		PaneName: paneName,
		Command:  command,
		Args:     args,
		Cwd:      cwd,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render tab layout template: %w", err)
	}

	// Write layout to temp file
	tmpFile, err := os.CreateTemp("", "zellij-tab-*.kdl")
	if err != nil {
		return fmt.Errorf("failed to create temp layout file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.WriteString(buf.String()); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write tab layout file: %w", err)
	}
	_ = tmpFile.Close()

	// Create the tab using the layout
	cmdArgs := append(sessionArgs(session), "action", "new-tab", "--layout", tmpFile.Name())
	cmd := exec.CommandContext(ctx, "zellij", cmdArgs...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create tab with command: %w", err)
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
