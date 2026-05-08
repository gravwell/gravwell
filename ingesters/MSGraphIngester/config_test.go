package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReachbackPeriod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{
			name:     "unset defaults to 48h",
			input:    "",
			expected: 48 * time.Hour,
		},
		{
			name:     "valid 24h",
			input:    "24h",
			expected: 24 * time.Hour,
		},
		{
			name:     "valid 7 days",
			input:    "168h",
			expected: 168 * time.Hour,
		},
		{
			name:     "valid with surrounding whitespace",
			input:    "  24h  ",
			expected: 24 * time.Hour,
		},
		{
			name:     "invalid string defaults to 48h",
			input:    "not-a-duration",
			expected: 48 * time.Hour,
		},
		{
			name:     "zero defaults to 48h",
			input:    "0s",
			expected: 48 * time.Hour,
		},
		{
			name:     "negative defaults to 48h",
			input:    "-1h",
			expected: 48 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &cfgType{Global: global{Reachback_Period: tt.input}}
			assert.Equal(t, tt.expected, cfg.lookbackPeriod())
		})
	}
}

func TestAlertFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		period    string
		wantEmpty bool
		wantErr   bool
	}{
		{name: "unset returns empty", period: "", wantEmpty: true},
		{name: "invalid returns err", period: "bogus", wantErr: true},
		{name: "zero returns empty", period: "0s", wantEmpty: true},
		{name: "negative returns err", period: "-1h", wantErr: true},
		{name: "valid 24h returns filter", period: "24h", wantEmpty: false},
		{name: "valid 48h returns filter", period: "48h", wantEmpty: false},
		{name: "valid 7d returns filter", period: "168h", wantEmpty: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &cfgType{Global: global{Reachback_Period: tt.period}}
			filter, err := cfg.alertFilter()

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			if tt.wantEmpty {
				assert.Empty(t, filter)
				return
			}

			require.NotEmpty(t, filter)
			assert.True(t, strings.HasPrefix(filter, "createdDateTime ge"), "filter should start with OData datetime prefix, got %q", filter)

			ts, err := time.Parse(time.RFC3339, strings.TrimPrefix(filter, "createdDateTime ge "))
			require.NoError(t, err, "timestamp in filter should be valid RFC3339")
			assert.True(t, ts.Before(time.Now()), "filter timestamp should be in the past")
			assert.True(t, time.Since(ts) < 200*time.Hour, "filter timestamp should be within a reasonable range of the configured period")
		})
	}
}

func TestTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType map[string]*contentType
		wantErr     bool
		wantTags    []string
	}{
		{
			name:        "empty content type map returns error",
			contentType: map[string]*contentType{},
			wantErr:     true,
		},
		{
			name: "nil tag name is skipped",
			contentType: map[string]*contentType{
				"a": {Tag_Name: ""},
			},
			wantErr: true,
		},
		{
			name: "single tag",
			contentType: map[string]*contentType{
				"alerts": {Tag_Name: "graph-alerts"},
			},
			wantTags: []string{"graph-alerts"},
		},
		{
			name: "multiple distinct tags",
			contentType: map[string]*contentType{
				"alerts": {Tag_Name: "graph-alerts"},
				"scores": {Tag_Name: "graph-scores"},
			},
			wantTags: []string{"graph-alerts", "graph-scores"},
		},
		{
			name: "duplicate tags are deduplicated",
			contentType: map[string]*contentType{
				"alerts":   {Tag_Name: "graph-data"},
				"scores":   {Tag_Name: "graph-data"},
				"profiles": {Tag_Name: "graph-data"},
			},
			wantTags: []string{"graph-data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &cfgType{ContentType: tt.contentType}
			tags, err := cfg.Tags()

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantTags, tags)
		})
	}
}
