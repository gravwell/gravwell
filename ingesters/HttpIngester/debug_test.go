/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crewjam/rfc5424"
)

const testSecret = `s3cr3t-token-value`

type captureDebugLogger struct {
	lines []string
}

func (c *captureDebugLogger) Debug(msg string, sds ...rfc5424.SDParam) error {
	var sb strings.Builder
	sb.WriteString(msg)
	for _, sd := range sds {
		sb.WriteString(" " + sd.Name + "=" + sd.Value)
	}
	c.lines = append(c.lines, sb.String())
	return nil
}

func (c *captureDebugLogger) String() string {
	return strings.Join(c.lines, "\n")
}

func TestRedactedRequestURI(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"/ingest", "/ingest"},
		{"/ingest?token=" + testSecret, "/ingest?token=REDACTED"},
		{"/ingest?tag=foo&token=" + testSecret + "&token=other", "/ingest?tag=REDACTED&token=REDACTED&token=REDACTED"},
		{"/services/collector/health?token=" + testSecret, "/services/collector/health?token=REDACTED"},
		{"/a%20b?x=" + testSecret, "/a%20b?x=REDACTED"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodPost, tt.uri, nil)
		if got := redactedRequestURI(r); got != tt.want {
			t.Errorf("redactedRequestURI(%q) = %q, want %q", tt.uri, got, tt.want)
		}
		// the request itself must be left untouched for later handlers
		if r.URL.RequestURI() != tt.uri {
			t.Errorf("request URI modified: %q", r.URL.RequestURI())
		}
	}
}

func TestRequestKVRedactsQueryToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/ingest?mytoken="+testSecret, nil)
	for _, kv := range requestKV(httptest.NewRecorder(), r) {
		if strings.Contains(kv.Value, testSecret) {
			t.Fatalf("requestKV %s=%q contains the token", kv.Name, kv.Value)
		}
		if kv.Name == "url" && kv.Value != "/ingest?mytoken=REDACTED" {
			t.Fatalf("unexpected url value %q", kv.Value)
		}
	}
}

func TestLogRequestHeadersRedactsCredentials(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/ingest", nil)
	r.Header.Set("Authorization", "Bearer "+testSecret)
	r.Header.Set("Proxy-Authorization", "Basic "+testSecret)
	r.Header.Set("Cookie", "session="+testSecret)
	r.Header.Set("X-Custom-Token-Header", testSecret)
	r.Header.Set("X-Amz-Firehose-Access-Key", testSecret)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", "test-agent")

	lgr := &captureDebugLogger{}
	logRequestHeaders(lgr, r)
	out := lgr.String()
	if strings.Contains(out, testSecret) {
		t.Fatalf("header log contains the secret:\n%s", out)
	}
	for _, want := range []string{
		"Authorization: [REDACTED]",
		"X-Custom-Token-Header: [REDACTED]",
		"Content-Type: [application/json]",
		"User-Agent: [test-agent]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("header log missing %q:\n%s", want, out)
		}
	}
}

type failingAuther struct {
	authHandler
}

func (failingAuther) AuthRequest(*http.Request) error {
	return errors.New("bad token")
}

func TestDebugWrappersRedact(t *testing.T) {
	newReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/ingest?token="+testSecret, nil)
		r.Header.Set("Authorization", "Bearer "+testSecret)
		return r
	}
	check := func(name string, lgr *captureDebugLogger) {
		t.Helper()
		out := lgr.String()
		if len(lgr.lines) == 0 {
			t.Fatalf("%s: nothing logged", name)
		}
		if strings.Contains(out, testSecret) {
			t.Fatalf("%s: debug log contains the secret:\n%s", name, out)
		}
	}

	// middleware, logging every request
	lgr := &captureDebugLogger{}
	mw := newDebugLoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), lgr, true)
	mw.ServeHTTP(httptest.NewRecorder(), newReq())
	check("middleware", lgr)

	// auth wrapper, logging failed authentication
	lgr = &captureDebugLogger{}
	if err := newDebugLoggingAuther(failingAuther{}, lgr).AuthRequest(newReq()); err == nil {
		t.Fatal("expected an auth error")
	}
	check("auther", lgr)
}
