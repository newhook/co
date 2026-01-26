package logparser

// Failure represents a parsed CI failure (test, lint, build, etc.).
type Failure struct {
	Name      string // What failed (test name, lint rule, build target)
	Context   string // Where in codebase (package, module, directory)
	File      string // e.g., "triggered_broadcast_test.go"
	Line      int    // e.g., 203
	Column    int    // e.g., 15 (0 if not applicable)
	Message   string // The error/failure message
	RawOutput string // Relevant log snippet for context
}

// Parser interface for language-specific implementations.
type Parser interface {
	// CanParse returns true if this parser can handle the given log content.
	CanParse(logContent string) bool
	// Parse extracts failures from the log content.
	Parse(logContent string) ([]Failure, error)
}

// parsers is the registry of available parsers.
var parsers []Parser

// RegisterParser adds a parser to the registry.
func RegisterParser(p Parser) {
	parsers = append(parsers, p)
}

// ParseFailures attempts to parse failures using all registered parsers.
func ParseFailures(logContent string) ([]Failure, error) {
	var allFailures []Failure

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
