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

// ASCII codes for special keys
const (
	ASCIICtrlC = 3  // Ctrl+C (interrupt)
	ASCIIEnter = 13 // Enter key
)

// client implements SessionManager and creates session instances.
type client struct {
	TabCreateDelay   time.Duration
	CtrlCDelay       time.Duration
	CommandDelay     time.Duration
	SessionStartWait time.Duration
}

// session implements the Session interface for a specific zellij session.
type session struct {
	name             string
	TabCreateDelay   time.Duration
	CtrlCDelay       time.Duration
	CommandDelay     time.Duration
	SessionStartWait time.Duration
}

// Compile-time checks.
var (
	_ SessionManager = (*client)(nil)
	_ Session        = (*session)(nil)
)

// New creates a new zellij client with default configuration.
func New() SessionManager {
	return &client{
		TabCreateDelay:   500 * time.Millisecond,
		CtrlCDelay:       500 * time.Millisecond,
		CommandDelay:     100 * time.Millisecond,
		SessionStartWait: 1 * time.Second,
	}
}

// Session returns a Session interface bound to the specified session name.
func (c *client) Session(name string) Session {
	return &session{
		name:             name,
		TabCreateDelay:   c.TabCreateDelay,
		CtrlCDelay:       c.CtrlCDelay,
		CommandDelay:     c.CommandDelay,
		SessionStartWait: c.SessionStartWait,
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

// =============================================================================
// SessionManager implementation (client)
// =============================================================================

// ListSessions returns a list of all zellij session names.
func (c *client) ListSessions(ctx context.Context) ([]string, error) {
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
func (c *client) SessionExists(ctx context.Context, name string) (bool, error) {
	sessions, err := c.ListSessions(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(sessions, name), nil
}

// IsSessionActive checks if a session exists and is active (not exited).
func (c *client) IsSessionActive(ctx context.Context, name string) (bool, error) {
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
func (c *client) createSessionWithCommand(ctx context.Context, sessionName, tabName, cwd, command string, args []string) error {
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
func (c *client) DeleteSession(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "zellij", "delete-session", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete zellij session: %w", err)
	}
	return nil
}

// EnsureSessionWithCommand creates a session with an initial tab running a command if it doesn't exist,
// or deletes and recreates it if exited.
// Returns true if a new session was created, false if an existing session was reused.
func (c *client) EnsureSessionWithCommand(ctx context.Context, sessionName, tabName, cwd, command string, args []string) (bool, error) {
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

// =============================================================================
// Session implementation (session)
// =============================================================================

// CreateTab creates a new tab in the session.
func (s *session) CreateTab(ctx context.Context, name, cwd string) error {
	args := append(sessionArgs(s.name), "action", "new-tab", "--name", name)
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}
	time.Sleep(s.TabCreateDelay)
	return nil
}

// CreateTabWithCommand creates a new tab that runs a specific command.
// This uses a layout file to ensure the command runs correctly even when
// called from outside the zellij session.
// The paneName parameter sets the pane title (optional, empty string uses command as title).
func (s *session) CreateTabWithCommand(ctx context.Context, name, cwd, command string, args []string, paneName string) error {
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
	cmdArgs := append(sessionArgs(s.name), "action", "new-tab", "--layout", tmpFile.Name())
	cmd := exec.CommandContext(ctx, "zellij", cmdArgs...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create tab with command: %w", err)
	}
	time.Sleep(s.TabCreateDelay)
	return nil
}

// SwitchToTab switches to a tab by name.
func (s *session) SwitchToTab(ctx context.Context, name string) error {
	args := append(sessionArgs(s.name), "action", "go-to-tab-name", name)
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to switch to tab: %w", err)
	}
	return nil
}

// QueryTabNames returns a list of all tab names in the session.
func (s *session) QueryTabNames(ctx context.Context) ([]string, error) {
	args := append(sessionArgs(s.name), "action", "query-tab-names")
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
func (s *session) TabExists(ctx context.Context, name string) (bool, error) {
	tabs, err := s.QueryTabNames(ctx)
	if err != nil {
		// If we can't query tabs, assume it doesn't exist
		return false, nil
	}
	return slices.Contains(tabs, name), nil
}

// CloseTab closes the current tab.
func (s *session) CloseTab(ctx context.Context) error {
	args := append(sessionArgs(s.name), "action", "close-tab")
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to close tab: %w", err)
	}
	return nil
}

// writeASCII writes an ASCII code to the current pane.
// Use this for control characters like Ctrl+C (3), Enter (13), etc.
func (s *session) writeASCII(ctx context.Context, code int) error {
	args := append(sessionArgs(s.name), "action", "write", fmt.Sprintf("%d", code))
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write ASCII code: %w", err)
	}
	return nil
}

// writeChars writes text to the current pane.
func (s *session) writeChars(ctx context.Context, text string) error {
	args := append(sessionArgs(s.name), "action", "write-chars", text)
	cmd := exec.CommandContext(ctx, "zellij", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write chars: %w", err)
	}
	return nil
}

// sendCtrlC sends Ctrl+C (interrupt signal) to the current pane.
func (s *session) sendCtrlC(ctx context.Context) error {
	return s.writeASCII(ctx, ASCIICtrlC)
}

// sendEnter sends the Enter key to the current pane.
func (s *session) sendEnter(ctx context.Context) error {
	return s.writeASCII(ctx, ASCIIEnter)
}

// executeCommand writes a command and sends Enter to execute it.
func (s *session) executeCommand(ctx context.Context, cmd string) error {
	if err := s.writeChars(ctx, cmd); err != nil {
		return fmt.Errorf("failed to write command: %w", err)
	}
	if err := s.sendEnter(ctx); err != nil {
		return fmt.Errorf("failed to send enter: %w", err)
	}
	return nil
}

// TerminateProcess sends Ctrl+C twice with a delay to ensure process termination.
// This handles cases where the first Ctrl+C might be caught by a signal handler.
func (s *session) TerminateProcess(ctx context.Context) error {
	if err := s.sendCtrlC(ctx); err != nil {
		return err
	}
	time.Sleep(s.CtrlCDelay)
	if err := s.sendCtrlC(ctx); err != nil {
		return err
	}
	time.Sleep(s.CtrlCDelay)
	return nil
}

// ClearAndExecute clears the current line and executes a command.
func (s *session) ClearAndExecute(ctx context.Context, cmd string) error {
	if err := s.writeChars(ctx, "clear"); err != nil {
		return err
	}
	time.Sleep(s.CommandDelay)
	if err := s.sendEnter(ctx); err != nil {
		return err
	}
	time.Sleep(s.CommandDelay)
	return s.executeCommand(ctx, cmd)
}

// TerminateAndCloseTab terminates any running process in a tab and closes it.
// It first switches to the tab, sends Ctrl+C to terminate, then closes the tab.
func (s *session) TerminateAndCloseTab(ctx context.Context, tabName string) error {
	// Switch to the tab
	if err := s.SwitchToTab(ctx, tabName); err != nil {
		return fmt.Errorf("failed to switch to tab: %w", err)
	}

	// Terminate any running process
	if err := s.TerminateProcess(ctx); err != nil {
		return fmt.Errorf("failed to terminate process: %w", err)
	}

	// Close the tab
	if err := s.CloseTab(ctx); err != nil {
		return fmt.Errorf("failed to close tab: %w", err)
	}

	return nil
}
