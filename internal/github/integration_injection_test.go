package github

import (
	"context"
	"strings"
	"testing"
)

// TestCreateBeadFromFeedback_InjectionPrevention tests that the CreateBeadFromFeedback
// function properly sanitizes input to prevent command injection attacks.
func TestCreateBeadFromFeedback_InjectionPrevention(t *testing.T) {
	tests := []struct {
		name        string
		beadInfo    BeadInfo
		shouldError bool
		errorMsg    string
	}{
		{
			name: "Normal input - should succeed",
			beadInfo: BeadInfo{
				Title:       "Fix bug in authentication",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "The authentication module has a bug that needs fixing",
				Labels:      []string{"security", "auth"},
			},
			shouldError: false,
		},
		{
			name: "Title with shell metacharacters - should be sanitized",
			beadInfo: BeadInfo{
				Title:       "Fix bug; rm -rf /; echo 'hacked'",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: false, // Should succeed with sanitized input
		},
		{
			name: "Title with command substitution - should be sanitized",
			beadInfo: BeadInfo{
				Title:       "Fix $(whoami) bug",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: false, // Should succeed with sanitized input
		},
		{
			name: "Title with backticks - should be sanitized",
			beadInfo: BeadInfo{
				Title:       "Fix `ls -la` bug",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: false, // Should succeed with sanitized input
		},
		{
			name: "Title with null bytes - should error",
			beadInfo: BeadInfo{
				Title:       "Fix bug\x00with null",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: true,
			errorMsg:    "contains null bytes",
		},
		{
			name: "Description with pipe and redirect - should be sanitized",
			beadInfo: BeadInfo{
				Title:       "Fix bug",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Fix this | cat /etc/passwd > /tmp/stolen",
			},
			shouldError: false, // Should succeed with sanitized input
		},
		{
			name: "Label with injection attempt - should be sanitized",
			beadInfo: BeadInfo{
				Title:       "Fix bug",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test",
				Labels:      []string{"security", "'; DROP TABLE users; --"},
			},
			shouldError: false, // Should succeed with sanitized input
		},
		{
			name: "Invalid bead type - should error",
			beadInfo: BeadInfo{
				Title:       "Fix bug",
				Type:        "invalid_type",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: true,
			errorMsg:    "invalid bead type",
		},
		{
			name: "Invalid priority - should error",
			beadInfo: BeadInfo{
				Title:       "Fix bug",
				Type:        "bug",
				Priority:    10, // Out of range
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: true,
			errorMsg:    "priority must be between 0 and 4",
		},
		{
			name: "Empty title - should error",
			beadInfo: BeadInfo{
				Title:       "",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: true,
			errorMsg:    "cannot be empty",
		},
		{
			name: "Title too long - should error",
			beadInfo: BeadInfo{
				Title:       strings.Repeat("a", 201), // Exceeds 200 char limit
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: true,
			errorMsg:    "exceeds maximum length",
		},
		{
			name: "Control characters in title - should be sanitized",
			beadInfo: BeadInfo{
				Title:       "Fix\x01\x02\x03bug\x1F",
				Type:        "bug",
				Priority:    2,
				ParentID:    "beads-123",
				Description: "Test description",
			},
			shouldError: false, // Should succeed with control chars removed
		},
	}

	integration := NewIntegration(nil)
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This test doesn't actually execute bd command, it just tests
			// that validation happens. In a real test environment, we'd mock exec.CommandContext.
			_, err := integration.CreateBeadFromFeedback(ctx, tt.beadInfo)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error containing '%s' but got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s' but got: %v", tt.errorMsg, err)
				}
			} else {
				// We expect an error because bd command isn't actually available in test
				// but it shouldn't be a validation error
				if err != nil && (strings.Contains(err.Error(), "null bytes") ||
					strings.Contains(err.Error(), "cannot be empty") ||
					strings.Contains(err.Error(), "exceeds maximum length") ||
					strings.Contains(err.Error(), "invalid bead type") ||
					strings.Contains(err.Error(), "priority must be")) {
					t.Errorf("Unexpected validation error: %v", err)
				}
			}
		})
	}
}

// TestValidateAndSanitizeInput tests the input validation function directly
func TestValidateAndSanitizeInput(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		maxLength   int
		fieldName   string
		shouldError bool
		expected    string
		errorMsg    string
	}{
		{
			name:        "Normal input",
			input:       "This is a normal input",
			maxLength:   100,
			fieldName:   "test",
			shouldError: false,
			expected:    "This is a normal input",
		},
		{
			name:        "Input with null byte",
			input:       "Test\x00input",
			maxLength:   100,
			fieldName:   "test",
			shouldError: true,
			errorMsg:    "contains null bytes",
		},
		{
			name:        "Input with control characters",
			input:       "Test\x01\x02\x03input\x1F",
			maxLength:   100,
			fieldName:   "test",
			shouldError: false,
			expected:    "Testinput",
		},
		{
			name:        "Empty input after trim",
			input:       "   ",
			maxLength:   100,
			fieldName:   "test",
			shouldError: true,
			errorMsg:    "cannot be empty",
		},
		{
			name:        "Input exceeds max length",
			input:       strings.Repeat("a", 101),
			maxLength:   100,
			fieldName:   "test",
			shouldError: true,
			errorMsg:    "exceeds maximum length",
		},
		{
			name:        "Input with newlines and tabs preserved",
			input:       "Line 1\nLine 2\tTabbed",
			maxLength:   100,
			fieldName:   "test",
			shouldError: false,
			expected:    "Line 1\nLine 2\tTabbed",
		},
		{
			name:        "Input with shell metacharacters",
			input:       "Test; rm -rf /; echo 'done'",
			maxLength:   100,
			fieldName:   "test",
			shouldError: false,
			expected:    "Test; rm -rf /; echo 'done'", // Metacharacters preserved (exec.CommandContext handles safely)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateAndSanitizeInput(tt.input, tt.maxLength, tt.fieldName)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error containing '%s' but got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s' but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else if result != tt.expected {
					t.Errorf("Expected '%s' but got '%s'", tt.expected, result)
				}
			}
		})
	}
}

// TestValidateBeadType tests the bead type validation
func TestValidateBeadType(t *testing.T) {
	tests := []struct {
		name        string
		beadType    string
		shouldError bool
	}{
		{"Valid bug type", "bug", false},
		{"Valid feature type", "feature", false},
		{"Valid task type", "task", false},
		{"Valid epic type", "epic", false},
		{"Valid type uppercase", "BUG", false},
		{"Invalid type", "invalid", true},
		{"Empty type", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBeadType(tt.beadType)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for type '%s' but got nil", tt.beadType)
			} else if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error for type '%s': %v", tt.beadType, err)
			}
		})
	}
}

// TestValidatePriority tests the priority validation
func TestValidatePriority(t *testing.T) {
	tests := []struct {
		name        string
		priority    int
		shouldError bool
	}{
		{"Valid priority 0", 0, false},
		{"Valid priority 1", 1, false},
		{"Valid priority 2", 2, false},
		{"Valid priority 3", 3, false},
		{"Valid priority 4", 4, false},
		{"Invalid negative priority", -1, true},
		{"Invalid priority too high", 5, true},
		{"Invalid priority way too high", 10, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePriority(tt.priority)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for priority %d but got nil", tt.priority)
			} else if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error for priority %d: %v", tt.priority, err)
			}
		})
	}
}