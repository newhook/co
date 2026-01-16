package linear

import (
	"testing"
)

func TestMapStatus(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  string
	}{
		{
			name:  "unstarted to open",
			state: State{Type: "unstarted"},
			want:  "open",
		},
		{
			name:  "started to in_progress",
			state: State{Type: "started"},
			want:  "in_progress",
		},
		{
			name:  "completed to closed",
			state: State{Type: "completed"},
			want:  "closed",
		},
		{
			name:  "canceled to closed",
			state: State{Type: "canceled"},
			want:  "closed",
		},
		{
			name:  "unknown to open",
			state: State{Type: "unknown"},
			want:  "open",
		},
		{
			name:  "case insensitive",
			state: State{Type: "STARTED"},
			want:  "in_progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapStatus(tt.state)
			if got != tt.want {
				t.Errorf("MapStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapPriority(t *testing.T) {
	tests := []struct {
		name     string
		priority int
		want     string
	}{
		{
			name:     "0 to P0 (urgent to critical)",
			priority: 0,
			want:     "P0",
		},
		{
			name:     "1 to P1 (high to high)",
			priority: 1,
			want:     "P1",
		},
		{
			name:     "2 to P2 (medium to medium)",
			priority: 2,
			want:     "P2",
		},
		{
			name:     "3 to P3 (low to low)",
			priority: 3,
			want:     "P3",
		},
		{
			name:     "4 to P4 (no priority to backlog)",
			priority: 4,
			want:     "P4",
		},
		{
			name:     "unknown to P2 (default)",
			priority: 99,
			want:     "P2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapPriority(tt.priority)
			if got != tt.want {
				t.Errorf("MapPriority() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapType(t *testing.T) {
	tests := []struct {
		name  string
		issue *Issue
		want  string
	}{
		{
			name: "bug label",
			issue: &Issue{
				Title:  "Some issue",
				Labels: []Label{{Name: "bug"}},
			},
			want: "bug",
		},
		{
			name: "feature label",
			issue: &Issue{
				Title:  "Some issue",
				Labels: []Label{{Name: "feature"}},
			},
			want: "feature",
		},
		{
			name: "bug in title",
			issue: &Issue{
				Title: "Fix bug in authentication",
			},
			want: "bug",
		},
		{
			name: "feature in title",
			issue: &Issue{
				Title: "Add feature for user profile",
			},
			want: "feature",
		},
		{
			name: "default to task",
			issue: &Issue{
				Title: "Update documentation",
			},
			want: "task",
		},
		{
			name: "fix in label",
			issue: &Issue{
				Title:  "Something broken",
				Labels: []Label{{Name: "hotfix"}},
			},
			want: "bug",
		},
		{
			name: "enhancement in label",
			issue: &Issue{
				Title:  "Improve performance",
				Labels: []Label{{Name: "enhancement"}},
			},
			want: "feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapType(tt.issue)
			if got != tt.want {
				t.Errorf("MapType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapIssueToBeadCreate(t *testing.T) {
	estimate := 3.5
	issue := &Issue{
		Identifier:  "ENG-123",
		Title:       "Fix authentication bug",
		Description: "Users cannot log in",
		Priority:    1,
		State:       State{Type: "started"},
		URL:         "https://linear.app/team/issue/ENG-123",
		Assignee:    &User{Name: "John Doe", Email: "john@example.com"},
		Project:     &Project{Name: "Q1 Features"},
		Labels:      []Label{{Name: "bug"}, {Name: "security"}},
		Estimate:    &estimate,
	}

	opts := MapIssueToBeadCreate(issue)

	if opts.Title != "Fix authentication bug" {
		t.Errorf("Title = %v, want %v", opts.Title, "Fix authentication bug")
	}
	if opts.Type != "bug" {
		t.Errorf("Type = %v, want %v", opts.Type, "bug")
	}
	if opts.Priority != "P1" {
		t.Errorf("Priority = %v, want %v", opts.Priority, "P1")
	}
	if opts.Status != "in_progress" {
		t.Errorf("Status = %v, want %v", opts.Status, "in_progress")
	}
	if opts.Assignee != "john@example.com" {
		t.Errorf("Assignee = %v, want %v", opts.Assignee, "john@example.com")
	}
	if len(opts.Labels) != 2 {
		t.Errorf("Labels count = %v, want %v", len(opts.Labels), 2)
	}
	if opts.Metadata["linear_id"] != "ENG-123" {
		t.Errorf("Metadata linear_id = %v, want %v", opts.Metadata["linear_id"], "ENG-123")
	}
	if opts.Metadata["linear_url"] != "https://linear.app/team/issue/ENG-123" {
		t.Errorf("Metadata linear_url = %v, want %v", opts.Metadata["linear_url"], "https://linear.app/team/issue/ENG-123")
	}
	if opts.Metadata["linear_project"] != "Q1 Features" {
		t.Errorf("Metadata linear_project = %v, want %v", opts.Metadata["linear_project"], "Q1 Features")
	}
	if opts.Metadata["linear_estimate"] != "3.5" {
		t.Errorf("Metadata linear_estimate = %v, want %v", opts.Metadata["linear_estimate"], "3.5")
	}
}

func TestFormatBeadDescription(t *testing.T) {
	estimate := 2.0
	issue := &Issue{
		Identifier:  "ENG-456",
		Title:       "Add feature",
		Description: "Original description",
		State:       State{Name: "In Progress", Type: "started"},
		URL:         "https://linear.app/team/issue/ENG-456",
		Project:     &Project{Name: "Backend"},
		Estimate:    &estimate,
		Assignee:    &User{Name: "Jane Smith"},
	}

	desc := FormatBeadDescription(issue)

	// Check that it contains key elements
	if !contains(desc, "Original description") {
		t.Error("Description should contain original description")
	}
	if !contains(desc, "ENG-456") {
		t.Error("Description should contain Linear ID")
	}
	if !contains(desc, "https://linear.app/team/issue/ENG-456") {
		t.Error("Description should contain URL")
	}
	if !contains(desc, "In Progress") {
		t.Error("Description should contain state name")
	}
	if !contains(desc, "Backend") {
		t.Error("Description should contain project name")
	}
	if !contains(desc, "2.0") {
		t.Error("Description should contain estimate")
	}
	if !contains(desc, "Jane Smith") {
		t.Error("Description should contain assignee name")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && s != substr && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInMiddle(s, substr)))
}

func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
