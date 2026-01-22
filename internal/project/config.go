package project

import (
	"fmt"
	"os"
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
// All intervals can be overridden via environment variables.
type SchedulerConfig struct {
	// PRFeedbackIntervalMinutes is the interval between PR feedback checks.
	// Can also be set via CO_PR_FEEDBACK_INTERVAL_MINUTES environment variable.
	// Defaults to 5 minutes when not specified.
	PRFeedbackIntervalMinutes *int `toml:"pr_feedback_interval_minutes"`

	// CommentResolutionIntervalMinutes is the interval between comment resolution checks.
	// Can also be set via CO_COMMENT_RESOLUTION_INTERVAL_MINUTES environment variable.
	// Defaults to 5 minutes when not specified.
	CommentResolutionIntervalMinutes *int `toml:"comment_resolution_interval_minutes"`

	// SchedulerPollSeconds is the scheduler polling interval.
	// Can also be set via CO_SCHEDULER_POLL_SECONDS environment variable.
	// Defaults to 1 second when not specified.
	SchedulerPollSeconds *int `toml:"scheduler_poll_seconds"`

	// ActivityUpdateSeconds is the interval for updating task activity timestamps.
	// Can also be set via CO_ACTIVITY_UPDATE_SECONDS environment variable.
	// Defaults to 30 seconds when not specified.
	ActivityUpdateSeconds *int `toml:"activity_update_seconds"`

	// ProcessingTimeoutMinutes is the maximum time a task can be in 'processing' state
	// without activity updates before it's considered stale and auto-failed.
	// Can also be set via CO_PROCESSING_TIMEOUT_MINUTES environment variable.
	// Defaults to 120 minutes (2 hours) when not specified.
	ProcessingTimeoutMinutes *int `toml:"processing_timeout_minutes"`

	// StaleCheckIntervalMinutes is the interval between checks for stale processing tasks.
	// Can also be set via CO_STALE_CHECK_INTERVAL_MINUTES environment variable.
	// Defaults to 5 minutes when not specified.
	StaleCheckIntervalMinutes *int `toml:"stale_check_interval_minutes"`
}

// GetPRFeedbackInterval returns the PR feedback check interval.
// Checks environment variable CO_PR_FEEDBACK_INTERVAL_MINUTES first,
// then config value, then defaults to 5 minutes.
func (s *SchedulerConfig) GetPRFeedbackInterval() time.Duration {
	if envVal := os.Getenv("CO_PR_FEEDBACK_INTERVAL_MINUTES"); envVal != "" {
		if minutes, err := parseMinutes(envVal); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	if s.PRFeedbackIntervalMinutes != nil && *s.PRFeedbackIntervalMinutes > 0 {
		return time.Duration(*s.PRFeedbackIntervalMinutes) * time.Minute
	}
	return 5 * time.Minute
}

// GetCommentResolutionInterval returns the comment resolution check interval.
// Checks environment variable CO_COMMENT_RESOLUTION_INTERVAL_MINUTES first,
// then config value, then defaults to 5 minutes.
func (s *SchedulerConfig) GetCommentResolutionInterval() time.Duration {
	if envVal := os.Getenv("CO_COMMENT_RESOLUTION_INTERVAL_MINUTES"); envVal != "" {
		if minutes, err := parseMinutes(envVal); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	if s.CommentResolutionIntervalMinutes != nil && *s.CommentResolutionIntervalMinutes > 0 {
		return time.Duration(*s.CommentResolutionIntervalMinutes) * time.Minute
	}
	return 5 * time.Minute
}

// GetSchedulerPollInterval returns the scheduler polling interval.
// Checks environment variable CO_SCHEDULER_POLL_SECONDS first,
// then config value, then defaults to 1 second.
func (s *SchedulerConfig) GetSchedulerPollInterval() time.Duration {
	if envVal := os.Getenv("CO_SCHEDULER_POLL_SECONDS"); envVal != "" {
		if seconds, err := parseSeconds(envVal); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	if s.SchedulerPollSeconds != nil && *s.SchedulerPollSeconds > 0 {
		return time.Duration(*s.SchedulerPollSeconds) * time.Second
	}
	return 1 * time.Second
}

// GetActivityUpdateInterval returns the activity update interval.
// Checks environment variable CO_ACTIVITY_UPDATE_SECONDS first,
// then config value, then defaults to 30 seconds.
func (s *SchedulerConfig) GetActivityUpdateInterval() time.Duration {
	if envVal := os.Getenv("CO_ACTIVITY_UPDATE_SECONDS"); envVal != "" {
		if seconds, err := parseSeconds(envVal); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	if s.ActivityUpdateSeconds != nil && *s.ActivityUpdateSeconds > 0 {
		return time.Duration(*s.ActivityUpdateSeconds) * time.Second
	}
	return 30 * time.Second
}

// GetProcessingTimeout returns the processing timeout duration.
// Checks environment variable CO_PROCESSING_TIMEOUT_MINUTES first,
// then config value, then defaults to 120 minutes (2 hours).
func (s *SchedulerConfig) GetProcessingTimeout() time.Duration {
	if envVal := os.Getenv("CO_PROCESSING_TIMEOUT_MINUTES"); envVal != "" {
		if minutes, err := parseMinutes(envVal); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	if s.ProcessingTimeoutMinutes != nil && *s.ProcessingTimeoutMinutes > 0 {
		return time.Duration(*s.ProcessingTimeoutMinutes) * time.Minute
	}
	return 120 * time.Minute
}

// GetStaleCheckInterval returns the stale task check interval.
// Checks environment variable CO_STALE_CHECK_INTERVAL_MINUTES first,
// then config value, then defaults to 5 minutes.
func (s *SchedulerConfig) GetStaleCheckInterval() time.Duration {
	if envVal := os.Getenv("CO_STALE_CHECK_INTERVAL_MINUTES"); envVal != "" {
		if minutes, err := parseMinutes(envVal); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	if s.StaleCheckIntervalMinutes != nil && *s.StaleCheckIntervalMinutes > 0 {
		return time.Duration(*s.StaleCheckIntervalMinutes) * time.Minute
	}
	return 5 * time.Minute
}

// parseMinutes parses a string as minutes.
func parseMinutes(s string) (int, error) {
	var minutes int
	_, err := fmt.Sscanf(s, "%d", &minutes)
	return minutes, err
}

// parseSeconds parses a string as seconds.
func parseSeconds(s string) (int, error) {
	var seconds int
	_, err := fmt.Sscanf(s, "%d", &seconds)
	return seconds, err
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
