/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

func TestOtelLogsListenerValidation(t *testing.T) {
	tests := []struct {
		name        string
		listener    otelLogsListener
		expectError bool
		expectedURL string
	}{
		{
			name: "valid listener with custom URL",
			listener: otelLogsListener{
				URL:      "/custom/logs",
				Tag_Name: "otel-logs",
			},
			expectError: false,
			expectedURL: "/custom/logs",
		},
		{
			name: "valid listener with default URL",
			listener: otelLogsListener{
				Tag_Name: "otel-logs",
			},
			expectError: false,
			expectedURL: "/v1/logs",
		},
		{
			name: "valid listener with default tag",
			listener: otelLogsListener{
				URL: "/v1/logs",
			},
			expectError: false,
			expectedURL: "/v1/logs",
		},
		{
			name: "invalid URL with scheme",
			listener: otelLogsListener{
				URL:      "http://example.com/logs",
				Tag_Name: "otel-logs",
			},
			expectError: true,
		},
		{
			name: "invalid URL with host",
			listener: otelLogsListener{
				URL:      "//example.com/logs",
				Tag_Name: "otel-logs",
			},
			expectError: true,
		},
		{
			name: "invalid tag name with special chars",
			listener: otelLogsListener{
				URL:      "/v1/logs",
				Tag_Name: "otel logs!",
			},
			expectError: true,
		},
		{
			name: "valid complex path",
			listener: otelLogsListener{
				URL:      "/api/v2/opentelemetry/logs",
				Tag_Name: "otel-logs",
			},
			expectError: false,
			expectedURL: "/api/v2/opentelemetry/logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initialTag := tt.listener.Tag_Name
			pth, err := tt.listener.validate("test")
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if pth != tt.expectedURL {
					t.Errorf("expected URL %q, got %q", tt.expectedURL, pth)
				}
				if initialTag == "" && tt.listener.Tag_Name != entry.DefaultTagName {
					t.Errorf("expected default tag name to be set, got %q", tt.listener.Tag_Name)
				}
			}
		})
	}
}

func TestOtelLogsListenerTags(t *testing.T) {
	tests := []struct {
		name        string
		listener    otelLogsListener
		expectedTag string
		expectError bool
	}{
		{
			name: "single tag",
			listener: otelLogsListener{
				Tag_Name: "otel-logs",
			},
			expectedTag: "otel-logs",
			expectError: false,
		},
		{
			name: "empty tag should error",
			listener: otelLogsListener{
				Tag_Name: "",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags, err := tt.listener.tags()
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(tags) != 1 {
					t.Errorf("expected 1 tag, got %d", len(tags))
				}
				if tags[0] != tt.expectedTag {
					t.Errorf("expected tag %q, got %q", tt.expectedTag, tags[0])
				}
			}
		})
	}
}

func TestOtelLogsListenerConfigOptions(t *testing.T) {
	tests := []struct {
		name     string
		listener otelLogsListener
		check    func(*testing.T, otelLogsListener)
	}{
		{
			name: "ignore timestamps enabled",
			listener: otelLogsListener{
				URL:               "/v1/logs",
				Tag_Name:          "otel-logs",
				Ignore_Timestamps: true,
			},
			check: func(t *testing.T, l otelLogsListener) {
				if !l.Ignore_Timestamps {
					t.Error("Ignore_Timestamps should be true")
				}
			},
		},
		{
			name: "debug posts enabled",
			listener: otelLogsListener{
				URL:         "/v1/logs",
				Tag_Name:    "otel-logs",
				Debug_Posts: true,
			},
			check: func(t *testing.T, l otelLogsListener) {
				if !l.Debug_Posts {
					t.Error("Debug_Posts should be true")
				}
			},
		},
		{
			name: "disable EVs enabled",
			listener: otelLogsListener{
				URL:         "/v1/logs",
				Tag_Name:    "otel-logs",
				Disable_EVs: true,
			},
			check: func(t *testing.T, l otelLogsListener) {
				if !l.Disable_EVs {
					t.Error("Disable_EVs should be true")
				}
			},
		},
		{
			name: "all options configured",
			listener: otelLogsListener{
				URL:               "/v1/logs",
				Tag_Name:          "otel-logs",
				Ignore_Timestamps: true,
				Debug_Posts:       true,
				Disable_EVs:       true,
			},
			check: func(t *testing.T, l otelLogsListener) {
				if !l.Ignore_Timestamps {
					t.Error("Ignore_Timestamps should be true")
				}
				if !l.Debug_Posts {
					t.Error("Debug_Posts should be true")
				}
				if !l.Disable_EVs {
					t.Error("Disable_EVs should be true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.listener.validate("test")
			if err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
			tt.check(t, tt.listener)
		})
	}
}

func TestOtelLogsListenerURLNormalization(t *testing.T) {
	tests := []struct {
		name        string
		inputURL    string
		expectedURL string
	}{
		{
			name:        "simple path",
			inputURL:    "/v1/logs",
			expectedURL: "/v1/logs",
		},
		{
			name:        "path with trailing slash",
			inputURL:    "/v1/logs/",
			expectedURL: "/v1/logs",
		},
		{
			name:        "path with double slashes in middle",
			inputURL:    "/v1//logs",
			expectedURL: "/v1/logs",
		},
		{
			name:        "relative path",
			inputURL:    "./v1/logs",
			expectedURL: "v1/logs",
		},
		{
			name:        "path with query ignored",
			inputURL:    "/v1/logs?foo=bar",
			expectedURL: "/v1/logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener := otelLogsListener{
				URL:      tt.inputURL,
				Tag_Name: "test",
			}
			pth, err := listener.validate("test")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if pth != tt.expectedURL {
				t.Errorf("expected URL %q, got %q", tt.expectedURL, pth)
			}
			if listener.URL != tt.expectedURL {
				t.Errorf("listener URL should be normalized to %q, got %q", tt.expectedURL, listener.URL)
			}
		})
	}
}
