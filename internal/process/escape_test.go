package process

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEscapePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{
			name:    "simple pattern",
			pattern: "simple",
			want:    "'simple'",
		},
		{
			name:    "pattern with single quote",
			pattern: "test'pattern",
			want:    "'test'\\''pattern'",
		},
		{
			name:    "pattern with multiple single quotes",
			pattern: "test'pattern'here",
			want:    "'test'\\''pattern'\\''here'",
		},
		{
			name:    "pattern with special characters",
			pattern: "test$pattern*here",
			want:    "'test$pattern*here'",
		},
		{
			name:    "empty pattern",
			pattern: "",
			want:    "''",
		},
		{
			name:    "pattern with spaces",
			pattern: "test pattern",
			want:    "'test pattern'",
		},
		{
			name:    "pattern with newline",
			pattern: "test\npattern",
			want:    "'test\npattern'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapePattern(tt.pattern)
			assert.Equal(t, tt.want, got)
		})
	}
}
