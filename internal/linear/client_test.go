package linear

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseIssueIDOrURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "valid issue ID uppercase",
			input:   "ENG-123",
			want:    "ENG-123",
			wantErr: false,
		},
		{
			name:    "valid issue ID lowercase",
			input:   "eng-456",
			want:    "ENG-456",
			wantErr: false,
		},
		{
			name:    "valid URL",
			input:   "https://linear.app/myteam/issue/ENG-789/some-title",
			want:    "ENG-789",
			wantErr: false,
		},
		{
			name:    "valid URL with http",
			input:   "http://linear.app/company/issue/PROD-42/feature-request",
			want:    "PROD-42",
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format - missing dash",
			input:   "ENG123",
			wantErr: true,
		},
		{
			name:    "invalid format - no number",
			input:   "ENG-",
			wantErr: true,
		},
		{
			name:    "invalid URL - no identifier",
			input:   "https://linear.app/myteam/issue/",
			wantErr: true,
		},
		{
			name:    "whitespace trimmed",
			input:   "  ENG-100  ",
			want:    "ENG-100",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIssueIDOrURL(tt.input)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.want, got)
		})
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantErr bool
	}{
		{
			name:    "API key provided",
			apiKey:  "test-key",
			wantErr: false,
		},
		{
			name:    "no API key",
			apiKey:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.apiKey)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, client, "NewClient() returned nil client without error")
			}
		})
	}
}
