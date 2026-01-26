package logparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoTestParser_CanParse(t *testing.T) {
	parser := &GoTestParser{}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "Standard go test failure",
			input:    `--- FAIL: TestExample (0.01s)`,
			expected: true,
		},
		{
			name:     "Gotestsum format",
			input:    `=== FAIL: github.com/pkg/example TestExample (0.01s)`,
			expected: true,
		},
		{
			name:     "Package failure line",
			input:    `FAIL	github.com/pkg/example	0.015s`,
			expected: true,
		},
		{
			name:     "No test failures",
			input:    `PASS
ok      github.com/pkg/example  0.015s`,
			expected: false,
		},
		{
			name:     "Build error not test",
			input:    `# github.com/pkg/example
./file.go:10:5: undefined: foo`,
			expected: false,
		},
		{
			name:     "Lint error not test",
			input:    `##[error]file.go:10:5: unchecked error (errcheck)`,
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

func TestGoTestParser_SingleTestFailure(t *testing.T) {
	parser := &GoTestParser{}

	input := `--- FAIL: TestExample (0.01s)
    example_test.go:42:
        Error Trace:	/path/example_test.go:42
        Error:      	expected 1, got 2
        Test:       	TestExample
FAIL
FAIL	github.com/pkg/example	0.015s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "TestExample", failures[0].TestName)
	assert.Equal(t, "github.com/pkg/example", failures[0].Package)
	assert.Equal(t, "example_test.go", failures[0].File)
	assert.Equal(t, 42, failures[0].Line)
	assert.Contains(t, failures[0].Error, "expected 1, got 2")
}

func TestGoTestParser_MultipleTestFailures(t *testing.T) {
	parser := &GoTestParser{}

	input := `--- FAIL: TestFirst (0.01s)
    first_test.go:10:
        Error Trace:	/path/first_test.go:10
        Error:      	first error
--- FAIL: TestSecond (0.02s)
    second_test.go:20:
        Error Trace:	/path/second_test.go:20
        Error:      	second error
FAIL
FAIL	github.com/pkg/example	0.030s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 2)

	// Verify both failures are captured
	testNames := []string{failures[0].TestName, failures[1].TestName}
	assert.Contains(t, testNames, "TestFirst")
	assert.Contains(t, testNames, "TestSecond")
}

func TestGoTestParser_SubtestFailure(t *testing.T) {
	parser := &GoTestParser{}

	input := `--- FAIL: TestParent (0.00s)
    --- FAIL: TestParent/SubTest (0.00s)
        parent_test.go:25:
            Error Trace:	/path/parent_test.go:25
            Error:      	subtest failed
FAIL
FAIL	github.com/pkg/example	0.010s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(failures), 1)

	// Should capture the subtest failure
	found := false
	for _, f := range failures {
		if f.TestName == "TestParent/SubTest" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should find the subtest failure")
}

func TestGoTestParser_TableTestFailure(t *testing.T) {
	parser := &GoTestParser{}

	input := `--- FAIL: TestTable (0.00s)
    --- FAIL: TestTable/case_name (0.00s)
        table_test.go:30:
            Error Trace:	/path/table_test.go:30
            Error:      	case failed
FAIL
FAIL	github.com/pkg/example	0.010s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(failures), 1)

	// Should capture the table test case failure
	found := false
	for _, f := range failures {
		if f.TestName == "TestTable/case_name" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should find the table test case failure")
}

func TestGoTestParser_StandardGoTestFormat(t *testing.T) {
	parser := &GoTestParser{}

	// Standard Go test format without testify
	input := `--- FAIL: TestSimple (0.00s)
    simple_test.go:15: got 1, want 2
FAIL
FAIL	github.com/pkg/example	0.010s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "TestSimple", failures[0].TestName)
	assert.Equal(t, "simple_test.go", failures[0].File)
	assert.Equal(t, 15, failures[0].Line)
}

func TestGoTestParser_RealCILogFormat(t *testing.T) {
	parser := &GoTestParser{}

	// Real CI log format with timestamps and job/step prefixes
	input := `Test	Run tests	2026-01-26T14:49:40.7760945Z --- FAIL: TestWatcher_DebounceWithPubsub (0.34s)
Test	Run tests	2026-01-26T14:49:40.7761234Z     watcher_test.go:340:
Test	Run tests	2026-01-26T14:49:40.7761567Z         Error Trace:	watcher_test.go:340
Test	Run tests	2026-01-26T14:49:40.7761890Z         Error:      	received unexpected second event
Test	Run tests	2026-01-26T14:49:40.7762123Z         Test:       	TestWatcher_DebounceWithPubsub
Test	Run tests	2026-01-26T14:49:40.7762456Z FAIL
Test	Run tests	2026-01-26T14:49:40.7762789Z FAIL	github.com/example/watcher	0.350s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "TestWatcher_DebounceWithPubsub", failures[0].TestName)
	assert.Equal(t, "watcher_test.go", failures[0].File)
	assert.Equal(t, 340, failures[0].Line)
	assert.Contains(t, failures[0].Error, "received unexpected second event")
}

func TestGoTestParser_TruncatedLogs(t *testing.T) {
	parser := &GoTestParser{}

	// Incomplete failure block (truncated)
	input := `--- FAIL: TestTruncated (0.01s)
    truncated_test.go:50:`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	// Should still capture the test name even without full error details
	assert.Equal(t, "TestTruncated", failures[0].TestName)
}

func TestGoTestParser_NoTestFailures(t *testing.T) {
	parser := &GoTestParser{}

	input := `=== RUN   TestExample
--- PASS: TestExample (0.00s)
PASS
ok      github.com/pkg/example  0.010s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	assert.Empty(t, failures)
}

func TestGoTestParser_BuildErrors(t *testing.T) {
	parser := &GoTestParser{}

	// Build errors should not be parsed as test failures
	input := `# github.com/pkg/example
./file.go:10:5: undefined: someFunc
./file.go:15:3: cannot use x (type int) as type string
FAIL	github.com/pkg/example [build failed]`

	// This should not parse as test failures (no --- FAIL: pattern)
	// but may trigger on FAIL\t pattern
	failures, err := parser.Parse(input)
	require.NoError(t, err)
	// Build errors should be empty or have minimal info
	// The key is they don't have TestName from --- FAIL: pattern
	for _, f := range failures {
		assert.NotContains(t, f.TestName, "undefined")
	}
}

func TestGoTestParser_UnicodeInErrorMessages(t *testing.T) {
	parser := &GoTestParser{}

	input := `--- FAIL: TestUnicode (0.01s)
    unicode_test.go:42:
        Error Trace:	/path/unicode_test.go:42
        Error:      	expected "日本語", got "中文"
FAIL
FAIL	github.com/pkg/example	0.015s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "TestUnicode", failures[0].TestName)
	assert.Contains(t, failures[0].Error, "日本語")
	assert.Contains(t, failures[0].Error, "中文")
}

func TestGoTestParser_VeryLongErrorMessage(t *testing.T) {
	parser := &GoTestParser{}

	longMessage := ""
	for i := 0; i < 100; i++ {
		longMessage += "this is a very long error message that keeps repeating "
	}

	input := `--- FAIL: TestLongError (0.01s)
    long_test.go:42:
        Error Trace:	/path/long_test.go:42
        Error:      	` + longMessage + `
FAIL
FAIL	github.com/pkg/example	0.015s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "TestLongError", failures[0].TestName)
	// Should capture at least part of the error
	assert.NotEmpty(t, failures[0].Error)
}

func TestGoTestParser_GotestsumFormat(t *testing.T) {
	parser := &GoTestParser{}

	input := `=== FAIL: github.com/pkg/example TestGotestsum (0.01s)
    gotestsum_test.go:42:
        Error Trace:	/path/gotestsum_test.go:42
        Error:      	gotestsum format error
FAIL	github.com/pkg/example	0.015s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(failures), 1)

	// Should capture the test from gotestsum format
	found := false
	for _, f := range failures {
		if f.TestName == "TestGotestsum" {
			found = true
			assert.Equal(t, "github.com/pkg/example", f.Package)
			break
		}
	}
	assert.True(t, found, "Should find the gotestsum format failure")
}

func TestGoTestParser_MultiLineErrorMessage(t *testing.T) {
	parser := &GoTestParser{}

	input := `--- FAIL: TestMultiLine (0.01s)
    multiline_test.go:42:
        Error Trace:	/path/multiline_test.go:42
        Error:      	Not equal:
                    	expected: "foo"
                    	actual  : "bar"

                    	Diff:
                    	--- Expected
                    	+++ Actual
                    	@@ -1 +1 @@
                    	-foo
                    	+bar
FAIL
FAIL	github.com/pkg/example	0.015s`

	failures, err := parser.Parse(input)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "TestMultiLine", failures[0].TestName)
	assert.Contains(t, failures[0].Error, "Not equal")
}

func TestGoTestParser_ExtractContext(t *testing.T) {
	parser := &GoTestParser{}

	lines := []string{
		"line 0",
		"line 1",
		"line 2",
		"--- FAIL: TestExample (0.01s)",
		"line 4",
		"line 5",
		"line 6",
	}

	// Extract context around index 3 with context of 2 lines
	// extractContext returns lines[index-contextLines:index+contextLines]
	// So for index=3, contextLines=2: lines[1:5] = line 1, line 2, --FAIL--, line 4
	context := parser.extractContext(lines, 3, 2)

	assert.Contains(t, context, "line 1")
	assert.Contains(t, context, "line 2")
	assert.Contains(t, context, "--- FAIL: TestExample")
	assert.Contains(t, context, "line 4")

	// With a larger context, we get more lines
	context = parser.extractContext(lines, 3, 3)
	assert.Contains(t, context, "line 0")
	assert.Contains(t, context, "line 5")
}

func TestGoTestParser_EnrichFailure(t *testing.T) {
	parser := &GoTestParser{}

	failure := &TestFailure{
		TestName: "TestEnrich",
	}

	rawOutput := `        Error Trace:	/some/path/enrich_test.go:42
        Error:      	expected something
        Test:       	TestEnrich`

	parser.enrichFailure(failure, rawOutput)

	assert.Equal(t, "enrich_test.go", failure.File)
	assert.Equal(t, 42, failure.Line)
	assert.Contains(t, failure.Error, "expected something")
}

func TestGoTestParser_EnrichFailureWithFallback(t *testing.T) {
	parser := &GoTestParser{}

	failure := &TestFailure{
		TestName: "TestFallback",
	}

	// Raw output without Error Trace but with file:line pattern
	rawOutput := `    fallback_test.go:25: got 1, want 2`

	parser.enrichFailure(failure, rawOutput)

	assert.Equal(t, "fallback_test.go", failure.File)
	assert.Equal(t, 25, failure.Line)
}
