package happ

import (
	"encoding/json"
	"testing"
)

func TestExtractCountry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		remarks  string
		want     string
	}{
		{
			name:    "Netherlands flag",
			remarks: "\U0001F1F3\U0001F1F1 | WiFi",
			want:    "NL",
		},
		{
			name:    "US flag",
			remarks: "\U0001F1FA\U0001F1F8 | Server",
			want:    "US",
		},
		{
			name:    "Russia flag",
			remarks: "\U0001F1F7\U0001F1FA Moscow",
			want:    "RU",
		},
		{
			name:    "no flag plain text",
			remarks: "Plain text",
			want:    "",
		},
		{
			name:    "single RI symbol",
			remarks: "\U0001F1F3", // only one regional indicator character
			want:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractCountry(tc.remarks)
			if got != tc.want {
				t.Errorf("extractCountry(%q) = %q, want %q", tc.remarks, got, tc.want)
			}
		})
	}
}

func TestConvertServer_Nil(t *testing.T) {
	t.Parallel()
	result := ConvertServer(nil)
	if result != nil {
		t.Errorf("ConvertServer(nil) = %v, want nil", result)
	}
}

func TestConvertServer_EmptyOutbounds(t *testing.T) {
	t.Parallel()
	// Nil outbounds field triggers the len check early return.
	result := ConvertServer(&Server{Outbounds: nil})
	if result != nil {
		t.Errorf("ConvertServer(nil outbounds) = %v, want nil", result)
	}

	// Empty JSON array outbounds yields an empty result (non-nil slice).
	result = ConvertServer(&Server{Outbounds: json.RawMessage(`[]`)})
	if len(result) != 0 {
		t.Errorf("ConvertServer([]) = %v (len=%d), want empty", result, len(result))
	}
}

func TestConvertServer_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv := &Server{Outbounds: json.RawMessage(`not json`)}
	result := ConvertServer(srv)
	if result != nil {
		t.Errorf("ConvertServer(invalid json) = %v, want nil", result)
	}
}

func TestConvertAllServers_Empty(t *testing.T) {
	t.Parallel()
	result := ConvertAllServers([]Server{})
	if result == nil {
		// nil is acceptable, but an empty non-nil slice is also valid.
		// Just verify no entries were returned.
		return
	}
	if len(result) != 0 {
		t.Errorf("ConvertAllServers(empty) = %v (len=%d), want empty", result, len(result))
	}
}
