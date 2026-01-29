package mise

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Operations defines the interface for mise operations.
// This abstraction enables testing without actual mise commands.
type Operations interface {
	// IsManaged returns true if the directory has a mise config file.
	IsManaged(dir string) bool
	// Trust runs `mise trust` in the given directory.
	Trust(dir string) error
	// Install runs `mise install` in the given directory.
	Install(dir string) error
	// HasTask checks if a mise task exists in the given directory.
	HasTask(dir, taskName string) bool
	// RunTask runs a mise task in the given directory.
	RunTask(dir, taskName string) error
	// Exec runs a command with the mise environment in the given directory.
	Exec(dir, command string, args ...string) ([]byte, error)
	// Initialize runs mise trust, install, and setup task if available.
	Initialize(dir string) error
	// InitializeWithOutput runs mise trust, install, and setup task if available,
	// writing progress messages to the provided writer.
	InitializeWithOutput(dir string, w io.Writer) error
}

// cliOperations implements Operations using the mise CLI.
type cliOperations struct{}

// Compile-time check that cliOperations implements Operations.
var _ Operations = (*cliOperations)(nil)

// Default is the default Operations implementation using the mise CLI.
var Default Operations = &cliOperations{}

// configFiles lists all mise config file locations to check.
var configFiles = []string{
	".mise.toml",
	"mise.toml",
	".mise/config.toml",
	".tool-versions",
}

// findConfigFile returns the first mise config file found in dir, or empty string if none.
func findConfigFile(dir string) string {
	for _, file := range configFiles {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); err == nil {
			return file
		}
	}
	return ""
}

// IsManaged implements Operations.IsManaged.
func (c *cliOperations) IsManaged(dir string) bool {
	return findConfigFile(dir) != ""
}

// IsManaged returns true if the directory has a mise config file.
// Deprecated: Use Default.IsManaged instead.
func IsManaged(dir string) bool {
	return Default.IsManaged(dir)
}

// Trust implements Operations.Trust.
func (c *cliOperations) Trust(dir string) error {
	cmd := exec.Command("mise", "trust")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mise trust failed: %w\n%s", err, output)
	}
	return nil
}

// Trust runs `mise trust` in the given directory.
// Deprecated: Use Default.Trust instead.
func Trust(dir string) error {
	return Default.Trust(dir)
}

// Install implements Operations.Install.
func (c *cliOperations) Install(dir string) error {
	cmd := exec.Command("mise", "install")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mise install failed: %w\n%s", err, output)
	}
	return nil
}

// Install runs `mise install` in the given directory.
// Deprecated: Use Default.Install instead.
func Install(dir string) error {
	return Default.Install(dir)
}

// HasTask implements Operations.HasTask.
func (c *cliOperations) HasTask(dir, taskName string) bool {
	cmd := exec.Command("mise", "task", "ls")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// Check if task name appears in output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == taskName {
			return true
		}
	}
	return false
}

// HasTask checks if a mise task exists in the given directory.
// Deprecated: Use Default.HasTask instead.
func HasTask(dir, taskName string) bool {
	return Default.HasTask(dir, taskName)
}

// RunTask implements Operations.RunTask.
func (c *cliOperations) RunTask(dir, taskName string) error {
	cmd := exec.Command("mise", "run", taskName)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mise run %s failed: %w\n%s", taskName, err, output)
	}
	return nil
}

// RunTask runs a mise task in the given directory.
// Deprecated: Use Default.RunTask instead.
func RunTask(dir, taskName string) error {
	return Default.RunTask(dir, taskName)
}

// Exec implements Operations.Exec.
func (c *cliOperations) Exec(dir, command string, args ...string) ([]byte, error) {
	miseArgs := append([]string{"exec", "--"}, command)
	miseArgs = append(miseArgs, args...)
	// #nosec G204 -- command and args are from trusted internal callers
	cmd := exec.Command("mise", miseArgs...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("mise exec %s failed: %w\n%s", command, err, output)
	}
	return output, nil
}

// Exec runs a command with the mise environment in the given directory.
// Deprecated: Use Default.Exec instead.
func Exec(dir, command string, args ...string) ([]byte, error) {
	return Default.Exec(dir, command, args...)
}

// Initialize implements Operations.Initialize.
func (c *cliOperations) Initialize(dir string) error {
	return c.InitializeWithOutput(dir, os.Stdout)
}

// Initialize runs mise trust, install, and setup task if available.
// Deprecated: Use Default.Initialize instead.
func Initialize(dir string) error {
	return Default.Initialize(dir)
}

// InitializeWithOutput implements Operations.InitializeWithOutput.
func (c *cliOperations) InitializeWithOutput(dir string, w io.Writer) error {
	configFile := findConfigFile(dir)
	if configFile == "" {
		fmt.Fprintf(w, "  Mise: not enabled (no config file found)\n")
		return nil
	}

	fmt.Fprintf(w, "  Mise: found %s\n", configFile)

	fmt.Fprintf(w, "  Mise: running trust...\n")
	if err := c.Trust(dir); err != nil {
		return err
	}

	fmt.Fprintf(w, "  Mise: running install...\n")
	if err := c.Install(dir); err != nil {
		return err
	}

	// Run setup task if it exists
	if c.HasTask(dir, "setup") {
		fmt.Fprintf(w, "  Mise: running setup task...\n")
		if err := c.RunTask(dir, "setup"); err != nil {
			return err
		}
	}

	fmt.Fprintf(w, "  Mise: initialization complete\n")
	return nil
}

// InitializeWithOutput runs mise trust, install, and setup task if available.
// Deprecated: Use Default.InitializeWithOutput instead.
func InitializeWithOutput(dir string, w io.Writer) error {
	return Default.InitializeWithOutput(dir, w)
}
