package logparser

import (
	"regexp"
	"strings"
)

var (
	// timestampPattern matches GitHub Actions log timestamp prefixes.
	// Format: 2026-01-26T14:49:40.7760945Z
	timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s*`)

	// ansiPattern matches ANSI escape codes (color codes, etc).
	ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

	// jobPrefixPattern matches GitHub Actions job/step prefixes.
	// Format: "JobName\tStepName\t2026-01-26..."
	jobPrefixPattern = regexp.MustCompile(`^[^\t]+\t[^\t]+\t\d{4}-\d{2}-\d{2}T[^\s]+\s*`)
)

// StripTimestamps removes CI log timestamp prefixes from each line.
// Input:  "2026-01-26T14:49:40.7760945Z --- FAIL: TestName"
// Output: "--- FAIL: TestName"
func StripTimestamps(log string) string {
	lines := strings.Split(log, "\n")
	for i, line := range lines {
		lines[i] = timestampPattern.ReplaceAllString(line, "")
	}
	return strings.Join(lines, "\n")
}

// StripANSI removes ANSI color codes from the log.
// Input:  "=== [31mFAIL[0m: TestName"
// Output: "=== FAIL: TestName"
func StripANSI(log string) string {
	return ansiPattern.ReplaceAllString(log, "")
}

// StripJobPrefix removes GitHub Actions job/step prefix from each line.
// Input:  "Test\tRun tests\t2026-01-26... content"
// Output: "content"
func StripJobPrefix(log string) string {
	lines := strings.Split(log, "\n")
	for i, line := range lines {
		lines[i] = jobPrefixPattern.ReplaceAllString(line, "")
	}
	return strings.Join(lines, "\n")
}

// CleanLog applies all cleanup operations to a log.
func CleanLog(log string) string {
	log = StripJobPrefix(log)
	log = StripTimestamps(log)
	log = StripANSI(log)
	return log
}
