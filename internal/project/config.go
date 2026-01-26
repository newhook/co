package project

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/BurntSushi/toml"
)

// Config represents the project configuration stored in .co/config.toml.
type Config struct {
	Project   ProjectConfig   `toml:"project"`
	Repo      RepoConfig      `toml:"repo"`
	Hooks     HooksConfig     `toml:"hooks"`
	Linear    LinearConfig    `toml:"linear"`
	Claude    ClaudeConfig    `toml:"claude"`
	Workflow  WorkflowConfig  `toml:"workflow"`
	Scheduler SchedulerConfig `toml:"scheduler"`
}

// ClaudeConfig contains Claude Code configuration.
type ClaudeConfig struct {
	// SkipPermissions controls whether to run Claude with --dangerously-skip-permissions.
	// Defaults to true when not specified in config.
	SkipPermissions *bool `toml:"skip_permissions"`

	// TimeLimitMinutes is the maximum duration in minutes for a Claude session.
	// When set to 0 or omitted, there is no time limit.
	TimeLimitMinutes int `toml:"time_limit"`

	// TaskTimeoutMinutes controls the maximum execution time for a task in minutes.
	// Defaults to 60 minutes when not specified.
	TaskTimeoutMinutes *int `toml:"task_timeout_minutes"`
}

// ShouldSkipPermissions returns true if Claude should run with --dangerously-skip-permissions.
// Defaults to true when not explicitly configured.
func (c *ClaudeConfig) ShouldSkipPermissions() bool {
	if c.SkipPermissions == nil {
		return true // default to true
	}
	return *c.SkipPermissions
}

// TimeLimit returns the maximum duration for a Claude session.
// Returns 0 if no time limit is configured.
func (c *ClaudeConfig) TimeLimit() time.Duration {
	if c.TimeLimitMinutes <= 0 {
		return 0
	}
	return time.Duration(c.TimeLimitMinutes) * time.Minute
}

// GetTaskTimeout returns the task timeout duration.
// Defaults to 60 minutes when not explicitly configured.
// If time_limit is set and is less than the default/configured task_timeout_minutes,
// time_limit takes precedence.
func (c *ClaudeConfig) GetTaskTimeout() time.Duration {
	// Calculate the task timeout
	var taskTimeout time.Duration
	if c.TaskTimeoutMinutes == nil || *c.TaskTimeoutMinutes <= 0 {
		taskTimeout = 60 * time.Minute // default to 60 minutes
	} else {
		taskTimeout = time.Duration(*c.TaskTimeoutMinutes) * time.Minute
	}

	// If time_limit is set and is less than task timeout, use time_limit
	if c.TimeLimitMinutes > 0 {
		timeLimit := time.Duration(c.TimeLimitMinutes) * time.Minute
		if timeLimit < taskTimeout {
			return timeLimit
		}
	}

	return taskTimeout
}

// ProjectConfig contains project metadata.
type ProjectConfig struct {
	Name      string    `toml:"name"`
	CreatedAt time.Time `toml:"created_at"`
}

// RepoConfig contains repository configuration.
type RepoConfig struct {
	Type   string `toml:"type"`   // "local" or "github"
	Source string `toml:"source"` // Original path or URL
	Path   string `toml:"path"`   // Always "main"
}

// HooksConfig contains hook configuration.
type HooksConfig struct {
	// Env is a list of environment variables to set before running commands.
	// Format: ["KEY=value", "ANOTHER_KEY=value"]
	// These are applied when spawning Claude in zellij tabs.
	Env []string `toml:"env"`
}

// LinearConfig contains Linear integration configuration.
type LinearConfig struct {
	// APIKey is the Linear API key for authentication.
	// Can also be set via LINEAR_API_KEY environment variable.
	APIKey string `toml:"api_key"`
}

// WorkflowConfig contains workflow configuration.
type WorkflowConfig struct {
	// MaxReviewIterations limits the number of review/fix cycles.
	// Defaults to 2 when not specified.
	MaxReviewIterations *int `toml:"max_review_iterations"`
}

// GetMaxReviewIterations returns the configured max review iterations or 2 if not specified.
func (w *WorkflowConfig) GetMaxReviewIterations() int {
	if w.MaxReviewIterations == nil {
		return 2
	}
	return *w.MaxReviewIterations
}

// SchedulerConfig contains scheduler timing configuration.
type SchedulerConfig struct {
	// PRFeedbackIntervalMinutes is the interval between PR feedback checks.
	// Defaults to 5 minutes when not specified.
	PRFeedbackIntervalMinutes *int `toml:"pr_feedback_interval_minutes"`

	// CommentResolutionIntervalMinutes is the interval between comment resolution checks.
	// Defaults to 5 minutes when not specified.
	CommentResolutionIntervalMinutes *int `toml:"comment_resolution_interval_minutes"`

	// SchedulerPollSeconds is the scheduler polling interval.
	// Defaults to 1 second when not specified.
	SchedulerPollSeconds *int `toml:"scheduler_poll_seconds"`

	// ActivityUpdateSeconds is the interval for updating task activity timestamps.
	// Defaults to 30 seconds when not specified.
	ActivityUpdateSeconds *int `toml:"activity_update_seconds"`
}

// GetPRFeedbackInterval returns the PR feedback check interval.
// Defaults to 5 minutes when not specified.
func (s *SchedulerConfig) GetPRFeedbackInterval() time.Duration {
	if s.PRFeedbackIntervalMinutes != nil && *s.PRFeedbackIntervalMinutes > 0 {
		return time.Duration(*s.PRFeedbackIntervalMinutes) * time.Minute
	}
	return 5 * time.Minute
}

// GetCommentResolutionInterval returns the comment resolution check interval.
// Defaults to 5 minutes when not specified.
func (s *SchedulerConfig) GetCommentResolutionInterval() time.Duration {
	if s.CommentResolutionIntervalMinutes != nil && *s.CommentResolutionIntervalMinutes > 0 {
		return time.Duration(*s.CommentResolutionIntervalMinutes) * time.Minute
	}
	return 5 * time.Minute
}

// GetSchedulerPollInterval returns the scheduler polling interval.
// Defaults to 1 second when not specified.
func (s *SchedulerConfig) GetSchedulerPollInterval() time.Duration {
	if s.SchedulerPollSeconds != nil && *s.SchedulerPollSeconds > 0 {
		return time.Duration(*s.SchedulerPollSeconds) * time.Second
	}
	return 1 * time.Second
}

// GetActivityUpdateInterval returns the activity update interval.
// Defaults to 30 seconds when not specified.
func (s *SchedulerConfig) GetActivityUpdateInterval() time.Duration {
	if s.ActivityUpdateSeconds != nil && *s.ActivityUpdateSeconds > 0 {
		return time.Duration(*s.ActivityUpdateSeconds) * time.Second
	}
	return 30 * time.Second
}

// LoadConfig reads and parses a config.toml file.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig writes the config to the specified path.
func (c *Config) SaveConfig(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	return nil
}

// SaveDocumentedConfig writes a fully documented config to the specified path.
// This creates a config file with inline comments explaining all available options.
func (c *Config) SaveDocumentedConfig(path string) error {
	content := c.GenerateDocumentedConfig()
	return os.WriteFile(path, []byte(content), 0600)
}

// configTemplateText is the Go template for generating documented config.toml files.
const configTemplateText = `# Claude Orchestrator Project Configuration
# This file configures how 'co' manages your project.
# See README.md for full documentation.

# =============================================================================
# Project Metadata
# =============================================================================
[project]
# Project name (derived from directory name)
name = {{.ProjectName | tomlString}}

# When this project was created
created_at = {{.CreatedAt}}

# =============================================================================
# Repository Configuration
# =============================================================================
[repo]
# Repository type: "local" (symlinked) or "github" (cloned)
type = {{.RepoType | tomlString}}

# Original repository path or GitHub URL
source = {{.RepoSource | tomlString}}

# Directory containing the repository (always "main")
path = {{.RepoPath | tomlString}}

# =============================================================================
# Hooks Configuration (Optional)
# =============================================================================
# Environment variables set when spawning Claude in zellij tabs.
# Useful for configuring Claude Code to use Vertex AI, setting PATH, etc.
#
# [hooks]
# env = [
#   "CLAUDE_CODE_USE_VERTEX=1",
#   "CLOUD_ML_REGION=us-east5",
#   "MY_CUSTOM_VAR=value"
# ]

# =============================================================================
# Claude Configuration (Optional)
# =============================================================================
# Controls how Claude Code sessions are executed.
#
# [claude]
# # Whether to run Claude with --dangerously-skip-permissions flag.
# # When true, Claude can execute commands without prompting for confirmation.
# # Defaults to true when not specified.
# skip_permissions = true
#
# # Maximum duration in minutes for a Claude session.
# # Tasks exceeding this limit are terminated and marked as failed.
# # Set to 0 or omit to disable the time limit.
# time_limit = 30
#
# # Maximum execution time for a task in minutes.
# # Defaults to 60 minutes when not specified.
# # If time_limit is set and is less, time_limit takes precedence.
# task_timeout_minutes = 60

# =============================================================================
# Workflow Configuration (Optional)
# =============================================================================
# Controls automated workflow behavior.
#
# [workflow]
# # Maximum number of review/fix cycles before proceeding to PR.
# # Increase for more thorough reviews, decrease to limit iteration time.
# # Defaults to 2 when not specified.
# max_review_iterations = 2

# =============================================================================
# Scheduler Configuration (Optional)
# =============================================================================
# Controls timing for background operations during orchestration.
#
# [scheduler]
# # Interval between PR feedback checks in minutes.
# # How often to check for CI failures and review comments.
# # Defaults to 5 minutes when not specified.
# pr_feedback_interval_minutes = 5
#
# # Interval between comment resolution checks in minutes.
# # How often to check for resolved feedback that needs GitHub updates.
# # Defaults to 5 minutes when not specified.
# comment_resolution_interval_minutes = 5
#
# # Internal scheduler polling frequency in seconds.
# # Defaults to 1 second when not specified.
# scheduler_poll_seconds = 1
#
# # Interval for updating task activity timestamps in seconds.
# # Defaults to 30 seconds when not specified.
# activity_update_seconds = 30

# =============================================================================
# Linear Integration (Optional)
# =============================================================================
# Authentication for importing issues from Linear.
# Can also be set via LINEAR_API_KEY environment variable.
#
# [linear]
# api_key = "lin_api_..."
`

// configTemplateData holds the data used to render the config template.
type configTemplateData struct {
	ProjectName string
	CreatedAt   string
	RepoType    string
	RepoSource  string
	RepoPath    string
}

// tomlString formats a string for TOML output with proper escaping.
// It wraps the string in double quotes and escapes special characters.
func tomlString(s string) string {
	// Escape backslashes first, then quotes
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	escaped = strings.ReplaceAll(escaped, "\r", `\r`)
	escaped = strings.ReplaceAll(escaped, "\t", `\t`)
	return `"` + escaped + `"`
}

// configTemplate is the parsed template for generating documented config files.
var configTemplate = template.Must(template.New("config").Funcs(template.FuncMap{
	"tomlString": tomlString,
}).Parse(configTemplateText))

// GenerateDocumentedConfig generates a documented config.toml string with comments.
// This includes the actual project values plus commented-out examples for optional sections.
func (c *Config) GenerateDocumentedConfig() string {
	data := configTemplateData{
		ProjectName: c.Project.Name,
		CreatedAt:   c.Project.CreatedAt.Format(time.RFC3339),
		RepoType:    c.Repo.Type,
		RepoSource:  c.Repo.Source,
		RepoPath:    c.Repo.Path,
	}

	var buf bytes.Buffer
	if err := configTemplate.Execute(&buf, data); err != nil {
		// Fall back to a minimal valid TOML if template execution fails
		return fmt.Sprintf("[project]\nname = %q\ncreated_at = %s\n[repo]\ntype = %q\nsource = %q\npath = %q\n",
			c.Project.Name, data.CreatedAt, c.Repo.Type, c.Repo.Source, c.Repo.Path)
	}
	return buf.String()
}
