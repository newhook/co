package logparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintParser_CanParse(t *testing.T) {
	parser := &LintParser{}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "errcheck lint error",
			input:    `##[error]file.go:10:5: Error return value is not checked (errcheck)`,
			expected: true,
		},
		{
			name:     "gofmt lint error",
			input:    `##[error]file.go:10:1: File is not properly formatted (gofmt)`,
			expected: true,
		},
		{
			name:     "unused lint error",
			input:    `##[error]file.go:5:6: func unused is unused (unused)`,
			expected: true,
		},
		{
			name:     "no lint errors",
			input:    `Some regular log output`,
			expected: false,
		},
		{
			name:     "test failure not lint",
			input:    `--- FAIL: TestSomething (0.01s)`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.CanParse(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLintParser_ParseSingleError(t *testing.T) {
	parser := &LintParser{}

	input := `##[error]file.go:10:5: Error return value of ` + "`" + `jobs.Backfill` + "`" + ` is not checked (errcheck)`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "file.go", failures[0].File)
	assert.Equal(t, 10, failures[0].Line)
	assert.Equal(t, 5, failures[0].Column)
	assert.Contains(t, failures[0].Message, "Error return value")
	assert.Equal(t, "errcheck", failures[0].Name)
}

func TestLintParser_ParseMultipleErrors(t *testing.T) {
	parser := &LintParser{}

	input := `##[error]file.go:10:5: unchecked error (errcheck)
##[error]file.go:20:1: not formatted (gofmt)
##[error]other.go:5:6: unused func (unused)`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 3)

	// First error
	assert.Equal(t, "file.go", failures[0].File)
	assert.Equal(t, 10, failures[0].Line)
	assert.Equal(t, "errcheck", failures[0].Name)

	// Second error
	assert.Equal(t, "file.go", failures[1].File)
	assert.Equal(t, 20, failures[1].Line)
	assert.Equal(t, "gofmt", failures[1].Name)

	// Third error
	assert.Equal(t, "other.go", failures[2].File)
	assert.Equal(t, 5, failures[2].Line)
	assert.Equal(t, "unused", failures[2].Name)
}

func TestLintParser_ParseWithCILogPrefixes(t *testing.T) {
	parser := &LintParser{}

	// CI log format: JobName\tStepName\ttimestamp content
	input := `ci / lint (go.mod)	UNKNOWN STEP	2026-01-22T23:36:37.3352343Z ##[error]file.go:10:5: msg (errcheck)`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "file.go", failures[0].File)
	assert.Equal(t, 10, failures[0].Line)
	assert.Equal(t, "errcheck", failures[0].Name)
}

func TestLintParser_ParseMixedContent(t *testing.T) {
	parser := &LintParser{}

	input := `Running golangci-lint...
level=info msg="running linters"
##[error]core/segments/serial/condition_test.go:124:15: Error return value is not checked (errcheck)
level=info msg="analysis complete"
##[error]core/segments/setup_test.go:17:1: File is not properly formatted (gofmt)
FAIL`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 2)

	assert.Equal(t, "core/segments/serial/condition_test.go", failures[0].File)
	assert.Equal(t, 124, failures[0].Line)
	assert.Equal(t, 15, failures[0].Column)

	assert.Equal(t, "core/segments/setup_test.go", failures[1].File)
	assert.Equal(t, 17, failures[1].Line)
}

func TestLintParser_ParseMessageWithParentheses(t *testing.T) {
	parser := &LintParser{}

	input := `##[error]file.go:10:5: call to function() should be checked (errcheck)`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Contains(t, failures[0].Message, "function()")
	assert.Equal(t, "errcheck", failures[0].Name)
}

func TestLintParser_ParseDeduplication(t *testing.T) {
	parser := &LintParser{}

	// Duplicate entries should be deduplicated
	input := `##[error]file.go:10:5: same error (errcheck)
##[error]file.go:10:5: same error (errcheck)`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)
}

func TestFormatFailure(t *testing.T) {
	f := Failure{
		File:    "file.go",
		Line:    10,
		Column:  5,
		Message: "error message",
		Name:    "errcheck",
	}

	assert.Equal(t, "file.go:10:5: error message (errcheck)", FormatFailure(f))

	// Without column
	f.Column = 0
	assert.Equal(t, "file.go:10: error message (errcheck)", FormatFailure(f))
}

func TestGroupByName(t *testing.T) {
	failures := []Failure{
		{File: "a.go", Line: 1, Name: "errcheck"},
		{File: "b.go", Line: 2, Name: "gofmt"},
		{File: "c.go", Line: 3, Name: "errcheck"},
	}

	groups := GroupByName(failures)

	require.Len(t, groups, 2)
	assert.Len(t, groups["errcheck"], 2)
	assert.Len(t, groups["gofmt"], 1)
}

func TestLintParser_Parse(t *testing.T) {
	parser := &LintParser{}

	input := `##[error]file.go:10:5: unchecked error (errcheck)`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	// Verify Failure fields
	assert.Equal(t, "errcheck", failures[0].Name)
	assert.Equal(t, "file.go", failures[0].File)
	assert.Equal(t, 10, failures[0].Line)
	assert.Contains(t, failures[0].Message, "unchecked error")
}

func TestLintParser_RealWorldSample(t *testing.T) {
	parser := &LintParser{}

	// Sample from GitHub Actions golangci-lint output
	input := `##[error]core/segments/serial/condition_test.go:124:15: Error return value of ` + "`" + `jobs.Backfill` + "`" + ` is not checked (errcheck)
##[error]core/segments/setup_test.go:17:1: File is not properly formatted (gofmt)
##[error]core/segments/serial/setup_test.go:98:6: func eventWithData is unused (unused)
##[error]core/segments/serial/condition_test.go:90:3: test helper function should call t.Helper() (thelper)`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 4)

	// Verify grouping
	groups := GroupByName(failures)
	require.Len(t, groups, 4) // errcheck, gofmt, unused, thelper

	assert.Len(t, groups["errcheck"], 1)
	assert.Len(t, groups["gofmt"], 1)
	assert.Len(t, groups["unused"], 1)
	assert.Len(t, groups["thelper"], 1)
}
