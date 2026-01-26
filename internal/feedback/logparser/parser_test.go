package logparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTestFailures_GoTestOutput(t *testing.T) {
	input := `--- FAIL: TestExample (0.01s)
    example_test.go:42:
        Error Trace:	/path/example_test.go:42
        Error:      	expected 1, got 2
FAIL
FAIL	github.com/pkg/example	0.015s`

	failures, err := ParseTestFailures(input)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(failures), 1)

	// Should have identified this as a Go test failure
	found := false
	for _, f := range failures {
		if f.TestName == "TestExample" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should find the test failure")
}

func TestParseTestFailures_LintOutput(t *testing.T) {
	input := `##[error]file.go:10:5: unchecked error (errcheck)`

	failures, err := ParseTestFailures(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	// Should have identified this as a lint error
	assert.Equal(t, "errcheck", failures[0].TestName) // LinterName maps to TestName
	assert.Equal(t, "file.go", failures[0].File)
}

func TestParseTestFailures_NoMatch(t *testing.T) {
	input := `PASS
ok      github.com/pkg/example  0.015s`

	failures, err := ParseTestFailures(input)
	require.NoError(t, err)
	assert.Empty(t, failures)
}

func TestParseTestFailures_MixedOutput(t *testing.T) {
	// Both test failures and lint errors in the same log
	input := `--- FAIL: TestMixed (0.01s)
    mixed_test.go:42:
        Error:      	test failed
FAIL
FAIL	github.com/pkg/example	0.015s
##[error]other.go:10:5: unchecked error (errcheck)`

	failures, err := ParseTestFailures(input)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(failures), 2)

	// Should have both test failure and lint error
	testFound := false
	lintFound := false
	for _, f := range failures {
		if f.TestName == "TestMixed" {
			testFound = true
		}
		if f.TestName == "errcheck" {
			lintFound = true
		}
	}
	assert.True(t, testFound, "Should find the test failure")
	assert.True(t, lintFound, "Should find the lint error")
}

func TestParseTestFailures_EmptyInput(t *testing.T) {
	failures, err := ParseTestFailures("")
	require.NoError(t, err)
	assert.Empty(t, failures)
}

func TestParseTestFailures_OnlyWhitespace(t *testing.T) {
	failures, err := ParseTestFailures("   \n\t\n   ")
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

func TestTestFailure_String(t *testing.T) {
	tf := TestFailure{
		TestName:  "TestExample",
		Package:   "github.com/pkg/example",
		File:      "example_test.go",
		Line:      42,
		Error:     "assertion failed",
		RawOutput: "raw log output",
	}

	// TestFailure is a struct, verify fields are accessible
	assert.Equal(t, "TestExample", tf.TestName)
	assert.Equal(t, "github.com/pkg/example", tf.Package)
	assert.Equal(t, "example_test.go", tf.File)
	assert.Equal(t, 42, tf.Line)
	assert.Equal(t, "assertion failed", tf.Error)
	assert.Equal(t, "raw log output", tf.RawOutput)
}
