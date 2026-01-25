package process

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProcessLister is a mock implementation of ProcessLister for testing.
type mockProcessLister struct {
	processes []string
	err       error
}

func (m *mockProcessLister) GetProcessList(ctx context.Context) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.processes, nil
}

// mockProcessKiller is a mock implementation of ProcessKiller for testing.
type mockProcessKiller struct {
	killedPatterns []string
	err            error
}

func (m *mockProcessKiller) KillByPattern(ctx context.Context, pattern string) error {
	m.killedPatterns = append(m.killedPatterns, pattern)
	return m.err
}

func TestIsProcessRunningWith(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		pattern   string
		processes []string
		want      bool
		wantErr   bool
	}{
		{
			name:      "process found",
			pattern:   "myapp",
			processes: []string{"/usr/bin/myapp --flag", "/bin/bash", "/usr/bin/other"},
			want:      true,
			wantErr:   false,
		},
		{
			name:      "process not found",
			pattern:   "nonexistent",
			processes: []string{"/usr/bin/myapp", "/bin/bash"},
			want:      false,
			wantErr:   false,
		},
		{
			name:      "empty process list",
			pattern:   "myapp",
			processes: []string{},
			want:      false,
			wantErr:   false,
		},
		{
			name:      "empty pattern matches all",
			pattern:   "",
			processes: []string{"/usr/bin/myapp"},
			want:      true,
			wantErr:   false,
		},
		{
			name:      "partial match",
			pattern:   "app",
			processes: []string{"/usr/bin/myapp", "/bin/bash"},
			want:      true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := &mockProcessLister{processes: tt.processes}
			got, err := IsProcessRunningWith(ctx, tt.pattern, lister)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsProcessRunningWith_ListerError(t *testing.T) {
	ctx := context.Background()
	lister := &mockProcessLister{err: errors.New("ps command failed")}

	_, err := IsProcessRunningWith(ctx, "myapp", lister)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get process list")
}

func TestKillProcessWith(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		pattern        string
		processes      []string
		killerErr      error
		wantErr        bool
		wantKillCalled bool
	}{
		{
			name:           "kills matching process",
			pattern:        "myapp",
			processes:      []string{"/usr/bin/myapp --flag", "/bin/bash"},
			wantErr:        false,
			wantKillCalled: true,
		},
		{
			name:           "no matching process - no kill called",
			pattern:        "nonexistent",
			processes:      []string{"/usr/bin/myapp", "/bin/bash"},
			wantErr:        false,
			wantKillCalled: false,
		},
		{
			name:           "empty pattern returns error",
			pattern:        "",
			processes:      []string{"/usr/bin/myapp"},
			wantErr:        true,
			wantKillCalled: false,
		},
		{
			name:           "killer error propagates",
			pattern:        "myapp",
			processes:      []string{"/usr/bin/myapp"},
			killerErr:      errors.New("pkill failed"),
			wantErr:        true,
			wantKillCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := &mockProcessLister{processes: tt.processes}
			killer := &mockProcessKiller{err: tt.killerErr}

			err := KillProcessWith(ctx, tt.pattern, lister, killer)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.wantKillCalled {
				assert.Contains(t, killer.killedPatterns, tt.pattern)
			} else {
				assert.Empty(t, killer.killedPatterns)
			}
		})
	}
}

func TestKillProcessWith_ListerError(t *testing.T) {
	ctx := context.Background()
	lister := &mockProcessLister{err: errors.New("ps command failed")}
	killer := &mockProcessKiller{}

	err := KillProcessWith(ctx, "myapp", lister, killer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get process list")
	assert.Empty(t, killer.killedPatterns)
}

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
