package claude

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

//go:embed layouts/automated-workflow.kdl
var automatedWorkflowLayoutTemplate string

// LayoutParams contains parameters for generating a workflow layout
type LayoutParams struct {
	WorkID       string
	WorkDir      string
	ProjectRoot  string
	SessionName  string
	CoPath       string // Path to co executable
}

// automatedWorkflowLayout is the template for generating the dynamic layout
const automatedWorkflowLayoutTmpl = `// Auto-generated layout for automated workflow
// Work ID: {{.WorkID}}
// Generated at runtime for zellij

layout {
    // Orchestrator tab - runs the workflow steps
    tab name="orchestrator" focus=true cwd="{{.ProjectRoot}}" {
        pane command="{{.CoPath}}" {
            args "orchestrate" "--work" "{{.WorkID}}"
        }
    }

    // Work tab - for Claude Code tasks
    tab name="work" cwd="{{.WorkDir}}" {
        pane split_direction="vertical" {
            // Main working pane
            pane size="70%"
            // Helper pane for git/shell
            pane size="30%"
        }
    }

    // Monitor tab - shows workflow progress
    tab name="monitor" cwd="{{.ProjectRoot}}" {
        pane split_direction="horizontal" {
            // Polling output
            pane size="60%" command="{{.CoPath}}" {
                args "poll"
            }
            // Manual commands
            pane size="40%"
        }
    }
}
`

var layoutTmpl = template.Must(template.New("layout").Parse(automatedWorkflowLayoutTmpl))

// GenerateWorkflowLayout generates a zellij layout file for the automated workflow
func GenerateWorkflowLayout(params LayoutParams) (string, error) {
	// Get the co executable path
	if params.CoPath == "" {
		coPath, err := os.Executable()
		if err != nil {
			params.CoPath = "co" // Fallback to PATH lookup
		} else {
			params.CoPath = coPath
		}
	}

	var buf bytes.Buffer
	if err := layoutTmpl.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("failed to generate layout: %w", err)
	}

	return buf.String(), nil
}

// WriteWorkflowLayout writes the layout to a temporary file and returns the path
func WriteWorkflowLayout(params LayoutParams) (string, error) {
	layout, err := GenerateWorkflowLayout(params)
	if err != nil {
		return "", err
	}

	// Create a temp file for the layout
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("co-layout-%s-*.kdl", params.WorkID))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := tmpFile.WriteString(layout); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write layout: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to close layout file: %w", err)
	}

	return tmpFile.Name(), nil
}

// EnsureLayoutDir ensures the .co/layouts directory exists in the project
func EnsureLayoutDir(projectRoot string) (string, error) {
	layoutDir := filepath.Join(projectRoot, ".co", "layouts")
	if err := os.MkdirAll(layoutDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create layouts directory: %w", err)
	}
	return layoutDir, nil
}

// WriteProjectLayout writes a layout file to the project's .co/layouts directory
func WriteProjectLayout(projectRoot string, name string, content string) (string, error) {
	layoutDir, err := EnsureLayoutDir(projectRoot)
	if err != nil {
		return "", err
	}

	layoutPath := filepath.Join(layoutDir, name+".kdl")
	if err := os.WriteFile(layoutPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write layout file: %w", err)
	}

	return layoutPath, nil
}

// StartWorkflowWithLayout starts a new zellij session with the workflow layout
func StartWorkflowWithLayout(ctx context.Context, params LayoutParams) error {
	// Generate and write the layout
	layoutPath, err := WriteWorkflowLayout(params)
	if err != nil {
		return err
	}
	defer os.Remove(layoutPath) // Clean up temp file

	// Start zellij with the layout
	cmd := exec.CommandContext(ctx, "zellij",
		"--session", params.SessionName,
		"--layout", layoutPath,
	)
	cmd.Dir = params.ProjectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start zellij with layout: %w", err)
	}

	return nil
}

// GetEmbeddedLayout returns the embedded layout template content
func GetEmbeddedLayout() string {
	return automatedWorkflowLayoutTemplate
}
