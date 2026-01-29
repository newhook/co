package github

import (
	"fmt"
	"strings"
)

// BeadCreateOptions represents options for creating a bead from a PR.
type BeadCreateOptions struct {
	Title       string
	Description string
	Type        string   // task, bug, feature
	Priority    string   // P0-P4
	Status      string   // open, in_progress, closed
	Labels      []string // label names
	Metadata    map[string]string
}

// MapPRToBeadCreate converts PR metadata to bead creation options.
func MapPRToBeadCreate(pr *PRMetadata) *BeadCreateOptions {
	opts := &BeadCreateOptions{
		Title:       pr.Title,
		Description: pr.Body,
		Type:        mapPRType(pr),
		Priority:    mapPRPriority(pr),
		Status:      mapPRStatus(pr),
		Labels:      pr.Labels,
		Metadata:    make(map[string]string),
	}

	// Store PR metadata
	opts.Metadata["pr_url"] = pr.URL
	opts.Metadata["pr_number"] = fmt.Sprintf("%d", pr.Number)
	opts.Metadata["pr_branch"] = pr.HeadRefName
	opts.Metadata["pr_base_branch"] = pr.BaseRefName
	opts.Metadata["pr_author"] = pr.Author
	opts.Metadata["pr_repo"] = pr.Repo

	return opts
}

// mapPRType infers a bead issue type from PR labels and title.
// Returns: "task", "bug", or "feature"
func mapPRType(pr *PRMetadata) string {
	// Check labels for type hints
	for _, label := range pr.Labels {
		labelLower := strings.ToLower(label)
		if strings.Contains(labelLower, "bug") || strings.Contains(labelLower, "fix") {
			return "bug"
		}
		if strings.Contains(labelLower, "feature") || strings.Contains(labelLower, "enhancement") {
			return "feature"
		}
	}

	// Check title for type hints
	titleLower := strings.ToLower(pr.Title)
	if strings.Contains(titleLower, "bug") || strings.Contains(titleLower, "fix") {
		return "bug"
	}
	if strings.Contains(titleLower, "feat") || strings.Contains(titleLower, "add") {
		return "feature"
	}

	// Default to task
	return "task"
}

// mapPRPriority infers priority from PR labels.
// Returns: "P0", "P1", "P2", "P3", or "P4"
func mapPRPriority(pr *PRMetadata) string {
	for _, label := range pr.Labels {
		labelLower := strings.ToLower(label)
		// Check for explicit priority labels
		if strings.Contains(labelLower, "critical") || strings.Contains(labelLower, "urgent") || strings.Contains(labelLower, "p0") {
			return "P0"
		}
		if strings.Contains(labelLower, "high") || strings.Contains(labelLower, "p1") {
			return "P1"
		}
		if strings.Contains(labelLower, "medium") || strings.Contains(labelLower, "p2") {
			return "P2"
		}
		if strings.Contains(labelLower, "low") || strings.Contains(labelLower, "p3") {
			return "P3"
		}
	}
	// Default to medium priority
	return "P2"
}

// mapPRStatus converts PR state to bead status.
func mapPRStatus(pr *PRMetadata) string {
	if pr.Merged {
		return "closed"
	}
	switch strings.ToUpper(pr.State) {
	case "OPEN":
		if pr.IsDraft {
			return "open"
		}
		return "in_progress"
	case "CLOSED":
		return "closed"
	case "MERGED":
		return "closed"
	default:
		return "open"
	}
}

// FormatBeadDescription formats a bead description with PR metadata.
func FormatBeadDescription(pr *PRMetadata) string {
	var builder strings.Builder

	// Add the original PR body
	if pr.Body != "" {
		builder.WriteString(pr.Body)
		builder.WriteString("\n\n")
	}

	// Add PR metadata section
	builder.WriteString("---\n")
	builder.WriteString("**Imported from GitHub PR**\n")
	builder.WriteString(fmt.Sprintf("- PR: #%d\n", pr.Number))
	builder.WriteString(fmt.Sprintf("- URL: %s\n", pr.URL))
	builder.WriteString(fmt.Sprintf("- Branch: %s → %s\n", pr.HeadRefName, pr.BaseRefName))
	builder.WriteString(fmt.Sprintf("- Author: %s\n", pr.Author))
	builder.WriteString(fmt.Sprintf("- State: %s\n", pr.State))

	if pr.IsDraft {
		builder.WriteString("- Draft: yes\n")
	}
	if pr.Merged {
		builder.WriteString(fmt.Sprintf("- Merged: %s\n", pr.MergedAt.Format("2006-01-02")))
	}
	if len(pr.Labels) > 0 {
		builder.WriteString(fmt.Sprintf("- Labels: %s\n", strings.Join(pr.Labels, ", ")))
	}

	return builder.String()
}
