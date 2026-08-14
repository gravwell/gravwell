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

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/stretchr/testify/require"
)

func TestOtelListenerValidation(t *testing.T) {
	tests := []struct {
		name        string
		listener    *otelMetricsListener
		expectError bool
		expectedURL string
	}{
		{
			name: "valid listener with custom URL",
			listener: &otelMetricsListener{
				URL:      "/custom/metrics",
				Tag_Name: "otel-custom",
			},
			expectError: false,
			expectedURL: "/custom/metrics",
		},
		{
			name: "valid listener with default URL",
			listener: &otelMetricsListener{
				URL:      "",
				Tag_Name: "otel-metrics",
			},
			expectError: false,
			expectedURL: defaultOtelMetricsURL,
		},
		{
			name: "valid listener with default tag",
			listener: &otelMetricsListener{
				URL:      "/v1/metrics",
				Tag_Name: "",
			},
			expectError: false,
			expectedURL: "/v1/metrics",
		},
		{
			name: "invalid URL with scheme",
			listener: &otelMetricsListener{
				URL:      "http://localhost/metrics",
				Tag_Name: "otel",
			},
			expectError: true,
		},
		{
			name: "invalid URL with host",
			listener: &otelMetricsListener{
				URL:      "//localhost/metrics",
				Tag_Name: "otel",
			},
			expectError: true,
		},
		{
			name: "invalid tag name with special chars",
			listener: &otelMetricsListener{
				URL:      "/v1/metrics",
				Tag_Name: "otel@metrics!",
			},
			expectError: true,
		},
		{
			name: "valid complex path",
			listener: &otelMetricsListener{
				URL:      "/otel/v1/metrics",
				Tag_Name: "otel",
			},
			expectError: false,
			expectedURL: "/otel/v1/metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := tt.listener.validate("test-listener")
			if tt.expectError {
				require.Error(t, err, "expected validation error")
			} else {
				require.NoError(t, err, "expected no validation error")
				require.Equal(t, tt.expectedURL, url, "URL should match expected")
				require.Equal(t, tt.expectedURL, tt.listener.URL, "listener URL should be normalized")
			}
		})
	}
}

func TestOtelListenerTags(t *testing.T) {
	tests := []struct {
		name         string
		listener     *otelMetricsListener
		expectedTags []string
		expectError  bool
	}{
		{
			name: "single tag",
			listener: &otelMetricsListener{
				Tag_Name: "otel-metrics",
			},
			expectedTags: []string{"otel-metrics"},
			expectError:  false,
		},
		{
			name: "empty tag should error",
			listener: &otelMetricsListener{
				Tag_Name: "",
			},
			expectedTags: nil,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags, err := tt.listener.tags()
			if tt.expectError {
				require.Error(t, err, "expected error when getting tags")
			} else {
				require.NoError(t, err, "expected no error when getting tags")
				require.Equal(t, tt.expectedTags, tags, "tags should match expected")
			}
		})
	}
}

func TestOtelListenerInConfig(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	cfg := &cfgType{
		gbl: gbl{
			Bind:             "127.0.0.1:8080",
			Health_Check_URL: "",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
			},
		},
	}

	err := hndlr.loadConfig(cfg)
	require.NoError(t, err, "config should load successfully")
	require.True(t, hasTag(muxer, "otel-metrics"), "otel-metrics tag should be negotiated")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "otel route should exist")
}

func TestOtelListenerMultipleInConfig(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	cfg := &cfgType{
		gbl: gbl{
			Bind:             "127.0.0.1:8080",
			Health_Check_URL: "",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"prod": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-prod",
			},
			"dev": {
				URL:      "/dev/v1/metrics",
				Tag_Name: "otel-dev",
			},
		},
	}

	err := hndlr.loadConfig(cfg)
	require.NoError(t, err, "config should load successfully")
	require.True(t, hasTag(muxer, "otel-prod"), "otel-prod tag should be negotiated")
	require.True(t, hasTag(muxer, "otel-dev"), "otel-dev tag should be negotiated")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "prod otel route should exist")
	require.True(t, hasRoute(hndlr, "/dev/v1/metrics"), "dev otel route should exist")
}

func TestOtelListenerHotReload(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	// Initial load with one listener
	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "initial route should exist")

	// Hot reload with additional listener
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
			},
			"otel2": {
				URL:      "/v2/metrics",
				Tag_Name: "otel-metrics-v2",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "original route should still exist")
	require.True(t, hasRoute(hndlr, "/v2/metrics"), "new route should exist")
	require.True(t, hasTag(muxer, "otel-metrics-v2"), "new tag should be negotiated")
}

func TestOtelListenerHotReloadRemoval(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	// Initial load with two listeners
	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
			},
			"otel2": {
				URL:      "/v2/metrics",
				Tag_Name: "otel-metrics-v2",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "first route should exist")
	require.True(t, hasRoute(hndlr, "/v2/metrics"), "second route should exist")

	// Hot reload removing one listener
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "remaining route should still exist")
	require.False(t, hasRoute(hndlr, "/v2/metrics"), "removed route should not exist")
}

func TestOtelListenerConflictWithOtherListeners(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	// Try to load config with conflicting URLs between standard listener and otel listener
	cfg := &cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		Listener: map[string]*lst{
			"std": {
				URL:      "/v1/metrics",
				Tag_Name: "std-tag",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-tag",
			},
		},
	}

	err := hndlr.loadConfig(cfg)
	require.Error(t, err, "should fail with duplicate URL between listener types")
	require.Contains(t, err.Error(), "duplicated", "error should mention duplication")
}

func TestOtelListenerDuplicateURL(t *testing.T) {
	cfg := &cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-tag1",
			},
			"otel2": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-tag2",
			},
		},
	}

	err := cfg.Verify()
	require.Error(t, err, "should fail with duplicate URLs in otel listeners")
	require.Contains(t, err.Error(), "duplicated", "error should mention duplication")
}

func TestOtelListenerWithPreprocessors(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	cfg := &cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel": {
				URL:          "/v1/metrics",
				Tag_Name:     "otel-metrics",
				Preprocessor: []string{"nonexistent-preprocessor"},
			},
		},
	}

	// This should fail because the preprocessor doesn't exist
	err := hndlr.loadConfig(cfg)
	require.Error(t, err, "should fail with nonexistent preprocessor")
}

func TestOtelListenerConfigOptions(t *testing.T) {
	tests := []struct {
		name     string
		listener *otelMetricsListener
		validate func(t *testing.T, l *otelMetricsListener)
	}{
		{
			name: "ignore timestamps enabled",
			listener: &otelMetricsListener{
				URL:               "/v1/metrics",
				Tag_Name:          "otel",
				Ignore_Timestamps: true,
			},
			validate: func(t *testing.T, l *otelMetricsListener) {
				require.True(t, l.Ignore_Timestamps, "Ignore_Timestamps should be true")
			},
		},
		{
			name: "debug posts enabled",
			listener: &otelMetricsListener{
				URL:         "/v1/metrics",
				Tag_Name:    "otel",
				Debug_Posts: true,
			},
			validate: func(t *testing.T, l *otelMetricsListener) {
				require.True(t, l.Debug_Posts, "Debug_Posts should be true")
			},
		},
		{
			name: "all options configured",
			listener: &otelMetricsListener{
				URL:               "/custom/metrics",
				Tag_Name:          "custom-otel",
				Ignore_Timestamps: true,
				Debug_Posts:       true,
				Preprocessor:      []string{"prep1", "prep2"},
			},
			validate: func(t *testing.T, l *otelMetricsListener) {
				require.True(t, l.Ignore_Timestamps)
				require.True(t, l.Debug_Posts)
				require.Len(t, l.Preprocessor, 2)
				require.Equal(t, "custom-otel", l.Tag_Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.listener.validate("test")
			require.NoError(t, err, "validation should succeed")
			tt.validate(t, tt.listener)
		})
	}
}

func TestOtelListenerURLNormalization(t *testing.T) {
	tests := []struct {
		name        string
		inputURL    string
		expectedURL string
	}{
		{
			name:        "simple path",
			inputURL:    "/v1/metrics",
			expectedURL: "/v1/metrics",
		},
		{
			name:        "path with trailing slash",
			inputURL:    "/v1/metrics/",
			expectedURL: "/v1/metrics",
		},
		{
			name:        "path with double slashes",
			inputURL:    "/v1//metrics",
			expectedURL: "/v1/metrics",
		},
		{
			name:        "relative path",
			inputURL:    "v1/metrics",
			expectedURL: "v1/metrics",
		},
		{
			name:        "path with query ignored",
			inputURL:    "/v1/metrics?test=1",
			expectedURL: "/v1/metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener := &otelMetricsListener{
				URL:      tt.inputURL,
				Tag_Name: "test",
			}
			url, err := listener.validate("test")
			require.NoError(t, err)
			require.Equal(t, tt.expectedURL, url, "URL should be normalized")
		})
	}
}

func TestOtelListenerEmptyConfig(t *testing.T) {
	cfg := &cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{},
	}

	err := cfg.Verify()
	require.Error(t, err, "should fail with no listeners configured")
	require.Contains(t, err.Error(), "No Listeners", "error should mention no listeners")
}

func TestOtelListenerWithBasicAuth(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	cfg := &cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
				auth: auth{
					AuthType: basic,
					Username: "user",
					Password: "pass",
				},
			},
		},
	}

	err := hndlr.loadConfig(cfg)
	require.NoError(t, err, "config with basic auth should load successfully")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "otel route should exist")
}

func TestOtelListenerWithPresharedToken(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	cfg := &cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
				auth: auth{
					AuthType:   preToken,
					TokenName:  "Bearer",
					TokenValue: "secret-token",
				},
			},
		},
	}

	err := hndlr.loadConfig(cfg)
	require.NoError(t, err, "config with preshared token auth should load successfully")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "otel route should exist")
}

func TestOtelListenerWithInvalidAuth(t *testing.T) {
	cfg := &cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
				auth: auth{
					AuthType: basic,
					Username: "user",
					// missing password
				},
			},
		},
	}

	err := cfg.Verify()
	require.Error(t, err, "invalid auth (missing password) should fail verification")
}

func TestOtelListenerMixedWithOtherListeners(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	cfg := &cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		Listener: map[string]*lst{
			"std": {
				URL:      "/json",
				Tag_Name: "json",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"otel": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
			},
		},
	}

	err := hndlr.loadConfig(cfg)
	require.NoError(t, err, "should load successfully with mixed listener types")
	require.True(t, hasRoute(hndlr, "/json"), "standard listener route should exist")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "otel listener route should exist")
	require.True(t, hasTag(muxer, "json"), "standard listener tag should be negotiated")
	require.True(t, hasTag(muxer, "otel-metrics"), "otel listener tag should be negotiated")
}
