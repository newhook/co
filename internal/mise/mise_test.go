package mise

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewOperations(t *testing.T) {
	ops := NewOperations("/some/dir")
	if ops == nil {
		t.Fatal("NewOperations returned nil")
	}

	// Verify it returns a cliOperations
	cli, ok := ops.(*cliOperations)
	if !ok {
		t.Error("NewOperations should return *cliOperations")
	}
	if cli.dir != "/some/dir" {
		t.Errorf("expected dir '/some/dir', got %s", cli.dir)
	}
}

func TestCLIOperationsImplementsInterface(t *testing.T) {
	// Compile-time check that cliOperations implements Operations
	var _ Operations = (*cliOperations)(nil)
}

func TestFindConfigFile(t *testing.T) {
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "mise-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
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
				os.MkdirAll(filepath.Join(tempDir, ".mise"), 0755)
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
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFindConfigFile_Priority(t *testing.T) {
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "mise-test-priority-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create multiple config files
	os.WriteFile(filepath.Join(tempDir, ".mise.toml"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tempDir, "mise.toml"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tempDir, ".tool-versions"), []byte(""), 0644)

	// Should return first in order: .mise.toml
	result := findConfigFile(tempDir)
	if result != ".mise.toml" {
		t.Errorf("expected '.mise.toml' (first in order), got %q", result)
	}
}

func TestIsManaged_PackageLevel(t *testing.T) {
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "mise-test-managed-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test with no config file
	if IsManaged(tempDir) {
		t.Error("expected IsManaged to return false when no config file exists")
	}

	// Create a config file
	os.WriteFile(filepath.Join(tempDir, ".mise.toml"), []byte(""), 0644)

	// Test with config file
	if !IsManaged(tempDir) {
		t.Error("expected IsManaged to return true when config file exists")
	}
}

func TestOperations_IsManaged(t *testing.T) {
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "mise-test-ops-managed-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ops := NewOperations(tempDir)

	// Test with no config file
	if ops.IsManaged() {
		t.Error("expected IsManaged to return false when no config file exists")
	}

	// Create a config file
	os.WriteFile(filepath.Join(tempDir, ".mise.toml"), []byte(""), 0644)

	// Test with config file
	if !ops.IsManaged() {
		t.Error("expected IsManaged to return true when config file exists")
	}
}

func TestConfigFiles_AllVariants(t *testing.T) {
	// Verify configFiles contains expected entries
	expected := []string{
		".mise.toml",
		"mise.toml",
		".mise/config.toml",
		".tool-versions",
	}

	if len(configFiles) != len(expected) {
		t.Errorf("expected %d config files, got %d", len(expected), len(configFiles))
	}

	for _, exp := range expected {
		found := false
		for _, cf := range configFiles {
			if cf == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected configFiles to contain %q", exp)
		}
	}
}
