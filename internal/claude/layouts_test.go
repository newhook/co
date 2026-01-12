package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWorkflowLayout(t *testing.T) {
	params := LayoutParams{
		WorkID:      "w-test123",
		WorkDir:     "/tmp/test/work",
		ProjectRoot: "/tmp/test/project",
		SessionName: "co-test-project",
		CoPath:      "/usr/local/bin/co",
	}

	layout, err := GenerateWorkflowLayout(params)
	if err != nil {
		t.Fatalf("GenerateWorkflowLayout failed: %v", err)
	}

	// Verify layout contains expected elements
	if !strings.Contains(layout, "w-test123") {
		t.Error("Layout should contain work ID")
	}
	if !strings.Contains(layout, "/tmp/test/work") {
		t.Error("Layout should contain work directory")
	}
	if !strings.Contains(layout, "/tmp/test/project") {
		t.Error("Layout should contain project root")
	}
	if !strings.Contains(layout, "/usr/local/bin/co") {
		t.Error("Layout should contain co path")
	}
	if !strings.Contains(layout, "orchestrate") {
		t.Error("Layout should contain orchestrate command")
	}
	if !strings.Contains(layout, "poll") {
		t.Error("Layout should contain poll command")
	}
	if !strings.Contains(layout, "orchestrator") {
		t.Error("Layout should have orchestrator tab")
	}
	if !strings.Contains(layout, "work") {
		t.Error("Layout should have work tab")
	}
	if !strings.Contains(layout, "monitor") {
		t.Error("Layout should have monitor tab")
	}
}

func TestGenerateWorkflowLayoutDefaultCoPath(t *testing.T) {
	params := LayoutParams{
		WorkID:      "w-test",
		WorkDir:     "/tmp/work",
		ProjectRoot: "/tmp/project",
		SessionName: "test-session",
		// CoPath intentionally left empty
	}

	layout, err := GenerateWorkflowLayout(params)
	if err != nil {
		t.Fatalf("GenerateWorkflowLayout failed: %v", err)
	}

	// Should still generate a valid layout
	if layout == "" {
		t.Error("Layout should not be empty")
	}
}

func TestWriteWorkflowLayout(t *testing.T) {
	params := LayoutParams{
		WorkID:      "w-test456",
		WorkDir:     "/tmp/test/work",
		ProjectRoot: "/tmp/test/project",
		SessionName: "co-test",
		CoPath:      "/usr/bin/co",
	}

	layoutPath, err := WriteWorkflowLayout(params)
	if err != nil {
		t.Fatalf("WriteWorkflowLayout failed: %v", err)
	}
	defer os.Remove(layoutPath)

	// Verify file was created
	if _, err := os.Stat(layoutPath); os.IsNotExist(err) {
		t.Error("Layout file should exist")
	}

	// Verify file has correct extension
	if !strings.HasSuffix(layoutPath, ".kdl") {
		t.Errorf("Layout file should have .kdl extension, got %s", layoutPath)
	}

	// Verify file contains expected content
	content, err := os.ReadFile(layoutPath)
	if err != nil {
		t.Fatalf("Failed to read layout file: %v", err)
	}
	if !strings.Contains(string(content), "w-test456") {
		t.Error("Layout file should contain work ID")
	}
}

func TestEnsureLayoutDir(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "layout-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the .co directory first
	coDir := filepath.Join(tmpDir, ".co")
	if err := os.MkdirAll(coDir, 0755); err != nil {
		t.Fatalf("Failed to create .co dir: %v", err)
	}

	// Test EnsureLayoutDir
	layoutDir, err := EnsureLayoutDir(tmpDir)
	if err != nil {
		t.Fatalf("EnsureLayoutDir failed: %v", err)
	}

	// Verify directory was created
	expectedDir := filepath.Join(tmpDir, ".co", "layouts")
	if layoutDir != expectedDir {
		t.Errorf("Layout dir mismatch: got %s, want %s", layoutDir, expectedDir)
	}

	if _, err := os.Stat(layoutDir); os.IsNotExist(err) {
		t.Error("Layouts directory should exist")
	}
}

func TestWriteProjectLayout(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "layout-project-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the .co directory first
	coDir := filepath.Join(tmpDir, ".co")
	if err := os.MkdirAll(coDir, 0755); err != nil {
		t.Fatalf("Failed to create .co dir: %v", err)
	}

	content := "layout { tab name=\"test\" {} }"
	layoutPath, err := WriteProjectLayout(tmpDir, "test-layout", content)
	if err != nil {
		t.Fatalf("WriteProjectLayout failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(layoutPath); os.IsNotExist(err) {
		t.Error("Layout file should exist")
	}

	// Verify path is correct
	expectedPath := filepath.Join(tmpDir, ".co", "layouts", "test-layout.kdl")
	if layoutPath != expectedPath {
		t.Errorf("Layout path mismatch: got %s, want %s", layoutPath, expectedPath)
	}

	// Verify content was written
	readContent, err := os.ReadFile(layoutPath)
	if err != nil {
		t.Fatalf("Failed to read layout file: %v", err)
	}
	if string(readContent) != content {
		t.Errorf("Content mismatch: got %q, want %q", string(readContent), content)
	}
}

func TestGetEmbeddedLayout(t *testing.T) {
	layout := GetEmbeddedLayout()

	// Verify embedded layout is not empty
	if layout == "" {
		t.Error("Embedded layout should not be empty")
	}

	// Verify it contains expected kdl structure
	if !strings.Contains(layout, "layout") {
		t.Error("Embedded layout should contain 'layout' keyword")
	}
	if !strings.Contains(layout, "tab") {
		t.Error("Embedded layout should contain tabs")
	}
}
