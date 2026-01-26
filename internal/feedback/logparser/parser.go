package logparser

// TestFailure represents a parsed test failure.
type TestFailure struct {
	TestName  string // e.g., "TestTriggeredBroadcast"
	Package   string // e.g., "campaigns/campaign_runtime"
	File      string // e.g., "triggered_broadcast_test.go"
	Line      int    // e.g., 203
	Error     string // The error message
	RawOutput string // Relevant log snippet for context
}

// Parser interface for language-specific implementations.
type Parser interface {
	// CanParse returns true if this parser can handle the given log content.
	CanParse(logContent string) bool
	// Parse extracts test failures from the log content.
	Parse(logContent string) ([]TestFailure, error)
}

// parsers is the registry of available parsers.
var parsers []Parser

// RegisterParser adds a parser to the registry.
func RegisterParser(p Parser) {
	parsers = append(parsers, p)
}

// ParseTestFailures attempts to parse test failures using all registered parsers.
func ParseTestFailures(logContent string) ([]TestFailure, error) {
	var allFailures []TestFailure

	for _, p := range parsers {
		if p.CanParse(logContent) {
			failures, err := p.Parse(logContent)
			if err != nil {
				continue // Try next parser
			}
			allFailures = append(allFailures, failures...)
		}
	}

	return allFailures, nil
}
