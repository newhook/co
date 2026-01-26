package logparser

import (
	"regexp"
	"strconv"
	"strings"
)

func init() {
	RegisterParser(&LintParser{})
}

// LintError represents a parsed linter error.
type LintError struct {
	File       string // e.g., "core/segments/serial/condition_test.go"
	Line       int    // e.g., 124
	Column     int    // e.g., 15
	Message    string // e.g., "Error return value of `jobs.Backfill` is not checked"
	LinterName string // e.g., "errcheck", "gofmt", "unused"
}

// LintParser parses golangci-lint output from GitHub Actions logs.
type LintParser struct{}

var (
	// GitHub Actions annotation format: ##[error]file:line:col: message (linter)
	lintErrorPattern = regexp.MustCompile(`##\[error\]([^:]+):(\d+):(\d+):\s*(.+)\s*\(([^)]+)\)`)

	// Alternative format without column: ##[error]file:line: message (linter)
	lintErrorNoColPattern = regexp.MustCompile(`##\[error\]([^:]+):(\d+):\s*(.+)\s*\(([^)]+)\)`)
)

// CanParse returns true if the log contains golangci-lint output.
func (p *LintParser) CanParse(logContent string) bool {
	return strings.Contains(logContent, "##[error]") &&
		(strings.Contains(logContent, "(errcheck)") ||
			strings.Contains(logContent, "(gofmt)") ||
			strings.Contains(logContent, "(unused)") ||
			strings.Contains(logContent, "(staticcheck)") ||
			strings.Contains(logContent, "(govet)") ||
			strings.Contains(logContent, "(gosimple)") ||
			strings.Contains(logContent, "(ineffassign)") ||
			strings.Contains(logContent, "(typecheck)") ||
			strings.Contains(logContent, "(thelper)"))
}

// Parse extracts lint errors from the log content.
func (p *LintParser) Parse(logContent string) ([]TestFailure, error) {
	errors := p.ParseLintErrors(logContent)

	// Convert LintErrors to TestFailures for the common interface
	failures := make([]TestFailure, 0, len(errors))
	for _, e := range errors {
		failures = append(failures, TestFailure{
			TestName:  e.LinterName,
			Package:   "", // Lint errors don't have a package in this context
			File:      e.File,
			Line:      e.Line,
			Error:     e.Message,
			RawOutput: e.String(),
		})
	}

	return failures, nil
}

// ParseLintErrors extracts LintError structs from the log content.
func (p *LintParser) ParseLintErrors(logContent string) []LintError {
	cleaned := CleanLog(logContent)
	lines := strings.Split(cleaned, "\n")

	var errors []LintError
	seen := make(map[string]bool)

	for _, line := range lines {
		// Try full format with column
		if matches := lintErrorPattern.FindStringSubmatch(line); len(matches) == 6 {
			file := matches[1]
			lineNum, _ := strconv.Atoi(matches[2])
			col, _ := strconv.Atoi(matches[3])
			message := strings.TrimSpace(matches[4])
			linter := matches[5]

			key := file + ":" + matches[2] + ":" + matches[3]
			if !seen[key] {
				seen[key] = true
				errors = append(errors, LintError{
					File:       file,
					Line:       lineNum,
					Column:     col,
					Message:    message,
					LinterName: linter,
				})
			}
			continue
		}

		// Try format without column
		if matches := lintErrorNoColPattern.FindStringSubmatch(line); len(matches) == 5 {
			file := matches[1]
			lineNum, _ := strconv.Atoi(matches[2])
			message := strings.TrimSpace(matches[3])
			linter := matches[4]

			key := file + ":" + matches[2]
			if !seen[key] {
				seen[key] = true
				errors = append(errors, LintError{
					File:       file,
					Line:       lineNum,
					Column:     0,
					Message:    message,
					LinterName: linter,
				})
			}
		}
	}

	return errors
}

// String returns a human-readable representation of the lint error.
func (e LintError) String() string {
	if e.Column > 0 {
		return e.File + ":" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Column) + ": " + e.Message + " (" + e.LinterName + ")"
	}
	return e.File + ":" + strconv.Itoa(e.Line) + ": " + e.Message + " (" + e.LinterName + ")"
}

// GroupByLinter groups lint errors by linter name.
func GroupByLinter(errors []LintError) map[string][]LintError {
	groups := make(map[string][]LintError)
	for _, e := range errors {
		groups[e.LinterName] = append(groups[e.LinterName], e)
	}
	return groups
}
