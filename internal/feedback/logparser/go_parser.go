package logparser

import (
	"regexp"
	"strconv"
	"strings"
)

func init() {
	RegisterParser(&GoTestParser{})
}

// GoTestParser parses Go test output, including testify and gotestsum formats.
type GoTestParser struct{}

var (
	// Standard go test failure: --- FAIL: TestName (duration)
	goTestFailPattern = regexp.MustCompile(`---\s*FAIL:\s*(\S+)\s*\([\d.]+s\)`)

	// Gotestsum format: === FAIL: package TestName (duration)
	gotestsumFailPattern = regexp.MustCompile(`===\s*FAIL:\s*(\S+)\s+(\S+)\s*\([\d.]+s\)`)

	// Package failure line: FAIL\tpackage/path\tduration
	packageFailPattern = regexp.MustCompile(`^FAIL\s+(\S+)\s+[\d.]+s`)

	// Testify error trace: Error Trace:\t/path/to/file.go:line
	testifyErrorTracePattern = regexp.MustCompile(`Error Trace:\s*(.+?):(\d+)`)

	// Testify error message: Error:\s+message
	testifyErrorPattern = regexp.MustCompile(`Error:\s+(.+)`)

	// Generic file:line pattern in error output
	fileLinePattern = regexp.MustCompile(`(\S+_test\.go):(\d+)`)
)

// CanParse returns true if the log contains Go test output.
func (p *GoTestParser) CanParse(logContent string) bool {
	cleaned := CleanLog(logContent)
	return strings.Contains(cleaned, "--- FAIL:") ||
		strings.Contains(cleaned, "=== FAIL:") ||
		strings.Contains(cleaned, "FAIL\t")
}

// Parse extracts test failures from Go test output.
func (p *GoTestParser) Parse(logContent string) ([]TestFailure, error) {
	cleaned := CleanLog(logContent)
	lines := strings.Split(cleaned, "\n")

	var failures []TestFailure
	failureMap := make(map[string]*TestFailure)

	for i, line := range lines {

		// Try gotestsum format first (has package info)
		if matches := gotestsumFailPattern.FindStringSubmatch(line); len(matches) == 3 {
			pkg := matches[1]
			testName := matches[2]
			key := pkg + "/" + testName

			if _, exists := failureMap[key]; !exists {
				failure := &TestFailure{
					TestName: testName,
					Package:  pkg,
				}
				failureMap[key] = failure

				// Extract context around this failure
				failure.RawOutput = p.extractContext(lines, i, 20)
				p.enrichFailure(failure, failure.RawOutput)
			}
			continue
		}

		// Try standard go test format
		if matches := goTestFailPattern.FindStringSubmatch(line); len(matches) == 2 {
			testName := matches[1]
			key := testName

			if _, exists := failureMap[key]; !exists {
				failure := &TestFailure{
					TestName: testName,
				}
				failureMap[key] = failure

				// Extract context around this failure
				failure.RawOutput = p.extractContext(lines, i, 20)
				p.enrichFailure(failure, failure.RawOutput)
			}
			continue
		}

		// Try package failure line to get package info
		if matches := packageFailPattern.FindStringSubmatch(line); len(matches) == 2 {
			pkg := matches[1]
			// Try to associate with existing failures without package
			for _, failure := range failureMap {
				if failure.Package == "" {
					failure.Package = pkg
				}
			}
		}
	}

	// Convert map to slice
	for _, failure := range failureMap {
		failures = append(failures, *failure)
	}

	return failures, nil
}

// extractContext extracts lines around the failure for context.
func (p *GoTestParser) extractContext(lines []string, index, contextLines int) string {
	start := max(0, index-contextLines)
	end := min(len(lines), index+contextLines)
	return strings.Join(lines[start:end], "\n")
}

// enrichFailure extracts additional details from the raw output.
func (p *GoTestParser) enrichFailure(failure *TestFailure, rawOutput string) {
	lines := strings.Split(rawOutput, "\n")

	for i, line := range lines {
		// Extract file and line from testify Error Trace
		if matches := testifyErrorTracePattern.FindStringSubmatch(line); len(matches) == 3 {
			path := matches[1]
			lineNum, _ := strconv.Atoi(matches[2])

			// Extract just the filename from the path
			parts := strings.Split(path, "/")
			failure.File = parts[len(parts)-1]
			failure.Line = lineNum
		}

		// Extract error message from testify Error
		if matches := testifyErrorPattern.FindStringSubmatch(line); len(matches) == 2 {
			var errParts []string
			errParts = append(errParts, strings.TrimSpace(matches[1]))
			// Collect multi-line error messages
			for j := i + 1; j < len(lines); j++ {
				nextLine := strings.TrimSpace(lines[j])
				if nextLine == "" || strings.HasPrefix(nextLine, "Error Trace:") ||
					strings.HasPrefix(nextLine, "Test:") || strings.HasPrefix(nextLine, "Messages:") {
					break
				}
				errParts = append(errParts, nextLine)
			}
			failure.Error = strings.Join(errParts, " ")
		}

		// Fallback: try to find file:line in the output
		if failure.File == "" {
			if matches := fileLinePattern.FindStringSubmatch(line); len(matches) == 3 {
				failure.File = matches[1]
				failure.Line, _ = strconv.Atoi(matches[2])
			}
		}
	}
}
