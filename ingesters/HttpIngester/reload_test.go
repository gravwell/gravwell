/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/stretchr/testify/require"
)

func TestHotReload(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "tag1"), "tag1 was not negotiated on initial load")

	err = hndlr.hotReload(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
		"l2": {URL: "/test2", Tag_Name: "tag2"},
	}))
	require.NoError(t, err)

	require.True(t, hasTag(muxer, "tag1"), "tag1 was not negotiated during hot reload")
	require.True(t, hasTag(muxer, "tag2"), "tag2 was not negotiated during hot reload")
	require.Contains(t, getMuxerCfgType(t, muxer).Listener, "l2", "muxer did not receive the updated configuration")
}

func TestHotReloadRemovesListener(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
		"l2": {URL: "/test2", Tag_Name: "tag2"},
	}))
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/test1"), "l1 route should exist after initial load")
	require.True(t, hasRoute(hndlr, "/test2"), "l2 route should exist after initial load")

	err = hndlr.hotReload(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)

	require.True(t, hasRoute(hndlr, "/test1"), "l1 route should still exist after hot reload")
	require.False(t, hasRoute(hndlr, "/test2"), "l2 route should have been removed after hot reload")
}

func TestHotReloadNilConfig(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)

	hndlr.RLock()
	routeCountBefore := len(hndlr.mp)
	hndlr.RUnlock()

	err = hndlr.hotReload(nil)
	require.ErrorIs(t, err, ErrInvalidParameter)

	hndlr.RLock()
	routeCountAfter := len(hndlr.mp)
	hndlr.RUnlock()
	require.Equal(t, routeCountBefore, routeCountAfter, "handler state should not change after nil hot reload")
}

func TestHotReloadInvalidConfig(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)

	hndlr.RLock()
	routeCountBefore := len(hndlr.mp)
	hndlr.RUnlock()

	// Duplicate URLs should cause the config to fail validation.
	err = hndlr.hotReload(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
		"l2": {URL: "/test1", Tag_Name: "tag1"}, // duplicate URL
	}))
	require.Error(t, err, "expected error when reloading with duplicate URLs")

	hndlr.RLock()
	routeCountAfter := len(hndlr.mp)
	hndlr.RUnlock()
	require.Equal(t, routeCountBefore, routeCountAfter, "handler state should not change after failed hot reload")
}

func TestHotReloadPreservesExistingTags(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "tag1"), "tag1 should exist after initial load")

	err = hndlr.hotReload(newTestConfig(t, "", map[string]*lst{
		"l2": {URL: "/test2", Tag_Name: "tag2"},
	}))
	require.NoError(t, err)

	// "tag1" should still be known to the muxer even though its been removed from the config.
	require.True(t, hasTag(muxer, "tag1"), "tag1 should still be known to the muxer after hot reload")
	require.True(t, hasTag(muxer, "tag2"), "tag2 should be known to the muxer after hot reload")
}

func TestLoadConfigSetsRawConfiguration(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)

	muxCfgJson := getMuxerConfig(t, muxer)
	require.NotEmpty(t, muxCfgJson, "muxer configuration should be set after loadConfig")
	require.Contains(t, getMuxerCfgType(t, muxer).Listener, "l1", "muxer should have received the initial configuration")
}

func TestHotReloadHealthCheckURL(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(newTestConfig(t, `/health`, map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)
	require.Equal(t, "/health", hndlr.healthCheckURL, "health check URL should be set after initial reload")

	// If we reload without a health check URL, it should be cleared.
	err = hndlr.hotReload(newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)
	require.Empty(t, hndlr.healthCheckURL, "health check URL should have been cleared after hot reloading without providing one")

	err = hndlr.hotReload(newTestConfig(t, "/healthz", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	}))
	require.NoError(t, err)
	require.Equal(t, "/healthz", hndlr.healthCheckURL, "health check URL should have been updated after hot reload")
}

func TestHotReloadIdempotent(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	cfg := newTestConfig(t, "", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
		"l2": {URL: "/test2", Tag_Name: "tag2"},
	})

	err := hndlr.loadConfig(cfg)
	require.NoError(t, err)

	err = hndlr.hotReload(cfg)
	require.NoError(t, err)

	hndlr.RLock()
	routeCountAfterFirst := len(hndlr.mp)
	hndlr.RUnlock()

	err = hndlr.hotReload(cfg)
	require.NoError(t, err)

	hndlr.RLock()
	routeCountAfterSecond := len(hndlr.mp)
	hndlr.RUnlock()

	require.Equal(t, routeCountAfterFirst, routeCountAfterSecond, "route count should be identical after repeated hot reloads using the same config")
	require.True(t, hasTag(muxer, "tag1"), "tag1 should still be known after idempotent hot reload")
	require.True(t, hasTag(muxer, "tag2"), "tag2 should still be known after idempotent hot reload")
}

func hasAuthRoute(h *handler, path string) bool {
	h.RLock()
	defer h.RUnlock()

	_, ok := h.auth[newRoute(http.MethodPost, path)]

	return ok
}

func TestHotReloadOtelMetrics(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-metrics"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "otel-metrics"), "otel-metrics tag should be negotiated on initial load")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "metrics route should exist after initial load")

	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-metrics"},
			"m2": {URL: "/v2/metrics", Tag_Name: "otel-metrics-v2"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "otel-metrics"), "otel-metrics tag should survive hot reload")
	require.True(t, hasTag(muxer, "otel-metrics-v2"), "otel-metrics-v2 tag should be negotiated after hot reload")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "original metrics route should still exist after hot reload")
	require.True(t, hasRoute(hndlr, "/v2/metrics"), "new metrics route should exist after hot reload")
}

func TestHotReloadOtelMetricsRemovesListener(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-m1"},
			"m2": {URL: "/v2/metrics", Tag_Name: "otel-m2"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "m1 route should exist after initial load")
	require.True(t, hasRoute(hndlr, "/v2/metrics"), "m2 route should exist after initial load")

	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-m1"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "m1 route should still exist after hot reload")
	require.False(t, hasRoute(hndlr, "/v2/metrics"), "m2 route should have been removed after hot reload")
}

func TestHotReloadOtelMetricsPreservesExistingTags(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-m1"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "otel-m1"), "otel-m1 should be known after initial load")

	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m2": {URL: "/v2/metrics", Tag_Name: "otel-m2"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "otel-m1"), "otel-m1 should still be known to the muxer after hot reload")
	require.True(t, hasTag(muxer, "otel-m2"), "otel-m2 should be known to the muxer after hot reload")
}

func TestHotReloadOtelMetricsIdempotent(t *testing.T) {
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
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-m1"},
			"m2": {URL: "/v2/metrics", Tag_Name: "otel-m2"},
		},
	}

	require.NoError(t, hndlr.loadConfig(cfg))
	require.NoError(t, hndlr.hotReload(cfg))

	hndlr.RLock()
	routeCountAfterFirst := len(hndlr.mp)
	hndlr.RUnlock()

	require.NoError(t, hndlr.hotReload(cfg))

	hndlr.RLock()
	routeCountAfterSecond := len(hndlr.mp)
	hndlr.RUnlock()

	require.Equal(t, routeCountAfterFirst, routeCountAfterSecond, "route count should be identical after repeated hot reloads")
	require.True(t, hasTag(muxer, "otel-m1"))
	require.True(t, hasTag(muxer, "otel-m2"))
}

func TestHotReloadOtelMetricsWithAuth(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	// Load with basic auth
	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
				auth:     auth{AuthType: basic, Username: "user", Password: "pass"},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "metrics route should exist")

	// Hot reload preserving auth
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
				auth:     auth{AuthType: basic, Username: "user", Password: "newpass"},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "metrics route should still exist after hot reload")

	// Hot reload dropping auth
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-metrics"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "metrics route should still exist after auth removal")
}

func TestHotReloadOtelMetricsWithJWTAuth(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {
				URL:      "/v1/metrics",
				Tag_Name: "otel-metrics",
				auth:     auth{AuthType: jwtT, Username: "user", Password: "pass", LoginURL: "/v1/metrics/login"},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "metrics route should exist")
	require.True(t, hasAuthRoute(hndlr, "/v1/metrics/login"), "JWT login route should be registered")

	// Hot reload should update auth routes
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-metrics"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "metrics route should still exist after auth removal")
	require.False(t, hasAuthRoute(hndlr, "/v1/metrics/login"), "JWT login route should be removed after auth removal")
}

func TestHotReloadOtelLogs(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-logs"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "otel-logs"), "otel-logs tag should be negotiated on initial load")
	require.True(t, hasRoute(hndlr, "/v1/logs"), "logs route should exist after initial load")

	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-logs"},
			"l2": {URL: "/v2/logs", Tag_Name: "otel-logs-v2"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "otel-logs"), "otel-logs tag should survive hot reload")
	require.True(t, hasTag(muxer, "otel-logs-v2"), "otel-logs-v2 tag should be negotiated after hot reload")
	require.True(t, hasRoute(hndlr, "/v1/logs"), "original logs route should still exist after hot reload")
	require.True(t, hasRoute(hndlr, "/v2/logs"), "new logs route should exist after hot reload")
}

func TestHotReloadOtelLogsRemovesListener(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-l1"},
			"l2": {URL: "/v2/logs", Tag_Name: "otel-l2"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/logs"), "l1 route should exist after initial load")
	require.True(t, hasRoute(hndlr, "/v2/logs"), "l2 route should exist after initial load")

	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-l1"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/logs"), "l1 route should still exist after hot reload")
	require.False(t, hasRoute(hndlr, "/v2/logs"), "l2 route should have been removed after hot reload")
}

func TestHotReloadOtelLogsPreservesExistingTags(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-l1"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "otel-l1"), "otel-l1 should be known after initial load")

	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l2": {URL: "/v2/logs", Tag_Name: "otel-l2"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasTag(muxer, "otel-l1"), "otel-l1 should still be known to the muxer after hot reload")
	require.True(t, hasTag(muxer, "otel-l2"), "otel-l2 should be known to the muxer after hot reload")
}

func TestHotReloadOtelLogsIdempotent(t *testing.T) {
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
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-l1"},
			"l2": {URL: "/v2/logs", Tag_Name: "otel-l2"},
		},
	}

	require.NoError(t, hndlr.loadConfig(cfg))
	require.NoError(t, hndlr.hotReload(cfg))

	hndlr.RLock()
	routeCountAfterFirst := len(hndlr.mp)
	hndlr.RUnlock()

	require.NoError(t, hndlr.hotReload(cfg))

	hndlr.RLock()
	routeCountAfterSecond := len(hndlr.mp)
	hndlr.RUnlock()

	require.Equal(t, routeCountAfterFirst, routeCountAfterSecond, "route count should be identical after repeated hot reloads")
	require.True(t, hasTag(muxer, "otel-l1"))
	require.True(t, hasTag(muxer, "otel-l2"))
}

func TestHotReloadOtelLogsWithAuth(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {
				URL:      "/v1/logs",
				Tag_Name: "otel-logs",
				auth:     auth{AuthType: preToken, TokenName: "Bearer", TokenValue: "secret"},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/logs"), "logs route should exist")

	// Hot reload with updated token value
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {
				URL:      "/v1/logs",
				Tag_Name: "otel-logs",
				auth:     auth{AuthType: preToken, TokenName: "Bearer", TokenValue: "new-secret"},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/logs"), "logs route should still exist after hot reload")

	// Hot reload dropping auth
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-logs"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/logs"), "logs route should still exist after auth removal")
}

func TestHotReloadOtelLogsWithJWTAuth(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {
				URL:      "/v1/logs",
				Tag_Name: "otel-logs",
				auth:     auth{AuthType: jwtT, Username: "user", Password: "pass", LoginURL: "/v1/logs/login"},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/logs"), "logs route should exist")
	require.True(t, hasAuthRoute(hndlr, "/v1/logs/login"), "JWT login route should be registered")

	// Hot reload should clear the JWT login route when auth is removed
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-logs"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/v1/logs"), "logs route should still exist after auth removal")
	require.False(t, hasAuthRoute(hndlr, "/v1/logs/login"), "JWT login route should be removed after auth removal")
}

func TestHotReloadMixedOtelAndStdListeners(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	err := hndlr.loadConfig(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		Listener: map[string]*lst{
			"std": {URL: "/json", Tag_Name: "json-tag"},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-metrics"},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-logs"},
		},
	})
	require.NoError(t, err)
	require.True(t, hasRoute(hndlr, "/json"), "std route should exist")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "metrics route should exist")
	require.True(t, hasRoute(hndlr, "/v1/logs"), "logs route should exist")

	// Hot reload: remove std listener, keep OTEL listeners, add new metrics listener
	err = hndlr.hotReload(&cfgType{
		gbl: gbl{
			Bind: "127.0.0.1:8080",
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		OtelListener: map[string]*otelMetricsListener{
			"m1": {URL: "/v1/metrics", Tag_Name: "otel-metrics"},
			"m2": {URL: "/v2/metrics", Tag_Name: "otel-metrics-v2"},
		},
		OtelLogsListener: map[string]*otelLogsListener{
			"l1": {URL: "/v1/logs", Tag_Name: "otel-logs"},
		},
	})
	require.NoError(t, err)
	require.False(t, hasRoute(hndlr, "/json"), "std route should have been removed")
	require.True(t, hasRoute(hndlr, "/v1/metrics"), "original metrics route should remain")
	require.True(t, hasRoute(hndlr, "/v2/metrics"), "new metrics route should exist")
	require.True(t, hasRoute(hndlr, "/v1/logs"), "logs route should remain")
	require.True(t, hasTag(muxer, "json-tag"), "json-tag should still be known by the muxer")
	require.True(t, hasTag(muxer, "otel-metrics"))
	require.True(t, hasTag(muxer, "otel-metrics-v2"))
	require.True(t, hasTag(muxer, "otel-logs"))
}

func newTestMuxer(t *testing.T) *ingest.IngestMuxer {
	t.Helper()

	lgr := log.NewDiscardLogger()
	lg = lgr

	muxer, err := ingest.NewUniformMuxer(ingest.UniformMuxerConfig{
		Destinations: []string{"127.0.0.1:4023"},
		Tags:         []string{"default"},
		Auth:         "testing",
		Logger:       lgr,
	})
	require.NoError(t, err)

	return muxer
}

func newTestHandler(t *testing.T, muxer *ingest.IngestMuxer) *handler {
	t.Helper()

	hndlr, err := newHandler(muxer, lg, nil, nil, nil, 0)
	require.NoError(t, err)

	return hndlr
}

func newTestConfig(t *testing.T, healthCheckURL string, listeners map[string]*lst) *cfgType {
	t.Helper()

	return &cfgType{
		gbl: gbl{
			Bind:             "127.0.0.1:8080",
			Health_Check_URL: healthCheckURL,
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "testing",
			},
		},
		Listener: listeners,
	}
}

func hasTag(muxer *ingest.IngestMuxer, tag string) bool {
	return slices.Contains(muxer.KnownTags(), tag)
}

func hasRoute(h *handler, path string) bool {
	h.RLock()
	defer h.RUnlock()

	_, ok := h.mp[newRoute(defaultMethod, path)]

	return ok
}

func getMuxerConfig(t *testing.T, muxer *ingest.IngestMuxer) []byte {
	t.Helper()

	v := reflect.ValueOf(muxer).Elem()
	f := v.FieldByName("ingesterState")
	require.True(t, f.IsValid(), "could not find ingesterState field in muxer")

	confField := f.FieldByName("Configuration")
	require.True(t, confField.IsValid(), "could not find Configuration field in ingesterState")

	return confField.Bytes()
}

func getMuxerCfgType(t *testing.T, muxer *ingest.IngestMuxer) cfgType {
	t.Helper()

	var cfg cfgType
	err := json.Unmarshal(getMuxerConfig(t, muxer), &cfg)
	require.NoError(t, err)

	return cfg
}
