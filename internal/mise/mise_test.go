package mise

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewOperations(t *testing.T) {
	ops := NewOperations("/some/dir")
	require.NotNil(t, ops, "NewOperations returned nil")

	// Verify it returns a cliOperations
	cli, ok := ops.(*cliOperations)
	require.True(t, ok, "NewOperations should return *cliOperations")
	require.Equal(t, "/some/dir", cli.dir)
}

func TestCLIOperationsImplementsInterface(t *testing.T) {
	// Compile-time check that cliOperations implements Operations
	var _ Operations = (*cliOperations)(nil)
}

func TestFindConfigFile(t *testing.T) {
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "mise-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name     string
		setup    func() // Create files before test
		expected string
	}{
		{
			name:     "no config file",
			setup:    func() {},
			expected: "",
		},
		{
			name: "mise.toml exists",
			setup: func() {
				os.WriteFile(filepath.Join(tempDir, "mise.toml"), []byte(""), 0644)
			},
			expected: "mise.toml",
		},
		{
			name: ".mise.toml exists",
			setup: func() {
				os.WriteFile(filepath.Join(tempDir, ".mise.toml"), []byte(""), 0644)
			},
			expected: ".mise.toml",
		},
		{
			name: ".tool-versions exists",
			setup: func() {
				os.WriteFile(filepath.Join(tempDir, ".tool-versions"), []byte(""), 0644)
			},
			expected: ".tool-versions",
		},
		{
			name: ".mise/config.toml exists",
			setup: func() {
				os.MkdirAll(filepath.Join(tempDir, ".mise"), 0750)
				os.WriteFile(filepath.Join(tempDir, ".mise", "config.toml"), []byte(""), 0644)
			},
			expected: ".mise/config.toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any files from previous tests
			os.RemoveAll(filepath.Join(tempDir, ".mise.toml"))
			os.RemoveAll(filepath.Join(tempDir, "mise.toml"))
			os.RemoveAll(filepath.Join(tempDir, ".mise"))
			os.RemoveAll(filepath.Join(tempDir, ".tool-versions"))

			tt.setup()

			result := findConfigFile(tempDir)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFindConfigFile_Priority(t *testing.T) {
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "mise-test-priority-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create multiple config files
	os.WriteFile(filepath.Join(tempDir, ".mise.toml"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tempDir, "mise.toml"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tempDir, ".tool-versions"), []byte(""), 0644)

	// Should return first in order: .mise.toml
	result := findConfigFile(tempDir)
	require.Equal(t, ".mise.toml", result, "expected '.mise.toml' (first in order)")
}

func TestIsManaged_PackageLevel(t *testing.T) {
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "mise-test-managed-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test with no config file
	require.False(t, IsManaged(tempDir), "expected IsManaged to return false when no config file exists")

	// Create a config file
	os.WriteFile(filepath.Join(tempDir, ".mise.toml"), []byte(""), 0644)

	// Test with config file
	require.True(t, IsManaged(tempDir), "expected IsManaged to return true when config file exists")
}

func TestOperations_IsManaged(t *testing.T) {
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "mise-test-ops-managed-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ops := NewOperations(tempDir)

	// Test with no config file
	require.False(t, ops.IsManaged(), "expected IsManaged to return false when no config file exists")

	// Create a config file
	os.WriteFile(filepath.Join(tempDir, ".mise.toml"), []byte(""), 0644)

	// Test with config file
	require.True(t, ops.IsManaged(), "expected IsManaged to return true when config file exists")
}

func TestConfigFiles_AllVariants(t *testing.T) {
	// Verify configFiles contains expected entries
	expected := []string{
		".mise.toml",
		"mise.toml",
		".mise/config.toml",
		".tool-versions",
	}

	require.Len(t, configFiles, len(expected))

	for _, exp := range expected {
		found := false
		for _, cf := range configFiles {
			if cf == exp {
				found = true
				break
			}
		}
		require.True(t, found, "expected configFiles to contain %q", exp)
	}
}
