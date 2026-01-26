package logparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFailures_GoTestOutput(t *testing.T) {
	input := `--- FAIL: TestExample (0.01s)
    example_test.go:42:
        Error Trace:	/path/example_test.go:42
        Error:      	expected 1, got 2
FAIL
FAIL	github.com/pkg/example	0.015s`

	failures, err := ParseFailures(input)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(failures), 1)

	// Should have identified this as a Go test failure
	found := false
	for _, f := range failures {
		if f.Name == "TestExample" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should find the test failure")
}

func TestParseFailures_LintOutput(t *testing.T) {
	input := `##[error]file.go:10:5: unchecked error (errcheck)`

	failures, err := ParseFailures(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	// Should have identified this as a lint error
	assert.Equal(t, "errcheck", failures[0].Name)
	assert.Equal(t, "file.go", failures[0].File)
}

func TestParseFailures_NoMatch(t *testing.T) {
	input := `PASS
ok      github.com/pkg/example  0.015s`

	failures, err := ParseFailures(input)
	require.NoError(t, err)
	assert.Empty(t, failures)
}

func TestParseFailures_MixedOutput(t *testing.T) {
	// Both test failures and lint errors in the same log
	input := `--- FAIL: TestMixed (0.01s)
    mixed_test.go:42:
        Error:      	test failed
FAIL
FAIL	github.com/pkg/example	0.015s
##[error]other.go:10:5: unchecked error (errcheck)`

	failures, err := ParseFailures(input)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(failures), 2)

	// Should have both test failure and lint error
	testFound := false
	lintFound := false
	for _, f := range failures {
		if f.Name == "TestMixed" {
			testFound = true
		}
		if f.Name == "errcheck" {
			lintFound = true
		}
	}
	assert.True(t, testFound, "Should find the test failure")
	assert.True(t, lintFound, "Should find the lint error")
}

func TestParseFailures_EmptyInput(t *testing.T) {
	failures, err := ParseFailures("")
	require.NoError(t, err)
	assert.Empty(t, failures)
}

func TestParseFailures_OnlyWhitespace(t *testing.T) {
	failures, err := ParseFailures("   \n\t\n   ")
	require.NoError(t, err)
	assert.Empty(t, failures)
}

func TestGoTestParser_ImplementsParser(t *testing.T) {
	var _ Parser = &GoTestParser{}
}

func TestLintParser_ImplementsParser(t *testing.T) {
	var _ Parser = &LintParser{}
}

func TestParserRegistry_GoParserRegistered(t *testing.T) {
	// Verify GoTestParser is registered and can parse Go test output
	goTestInput := `--- FAIL: TestRegistered (0.01s)
FAIL
FAIL	github.com/pkg/example	0.015s`

	found := false
	for _, p := range parsers {
		if p.CanParse(goTestInput) {
			found = true
			break
		}
	}
	assert.True(t, found, "GoTestParser should be registered and recognize Go test output")
}

func TestParserRegistry_LintParserRegistered(t *testing.T) {
	// Verify LintParser is registered and can parse lint output
	lintInput := `##[error]file.go:10:5: error (errcheck)`

	found := false
	for _, p := range parsers {
		if p.CanParse(lintInput) {
			found = true
			break
		}
	}
	assert.True(t, found, "LintParser should be registered and recognize lint output")
}

func TestParserOrdering_SpecificBeforeGeneric(t *testing.T) {
	// Test that specific parsers match before any generic fallback would
	goTestInput := `--- FAIL: TestOrdering (0.01s)
FAIL
FAIL	github.com/pkg/example	0.015s`

	// GoTestParser should recognize this
	goParser := &GoTestParser{}
	assert.True(t, goParser.CanParse(goTestInput))

	// LintParser should NOT recognize this
	lintParser := &LintParser{}
	assert.False(t, lintParser.CanParse(goTestInput))
}

func TestFailure_Fields(t *testing.T) {
	f := Failure{
		Name:      "TestExample",
		Context:   "github.com/pkg/example",
		File:      "example_test.go",
		Line:      42,
		Column:    0,
		Message:   "assertion failed",
		RawOutput: "raw log output",
	}

	// Failure is a struct, verify fields are accessible
	assert.Equal(t, "TestExample", f.Name)
	assert.Equal(t, "github.com/pkg/example", f.Context)
	assert.Equal(t, "example_test.go", f.File)
	assert.Equal(t, 42, f.Line)
	assert.Equal(t, 0, f.Column)
	assert.Equal(t, "assertion failed", f.Message)
	assert.Equal(t, "raw log output", f.RawOutput)
}
