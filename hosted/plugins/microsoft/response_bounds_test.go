package microsoft

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

// oversizeBody returns a syntactically valid JSON envelope larger than limit,
// so a client without a byte bound would decode and retain all of it.
func oversizeBody(limit int) []byte {
	var payload bytes.Buffer
	payload.WriteString(`{"value":[{"id":"`)
	payload.Write(bytes.Repeat([]byte("a"), limit))
	payload.WriteString(`"}]}`)
	return payload.Bytes()
}

func newBoundsTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.retryBackoff = 0
	return client
}

// TestOrdinaryAPIResponsesAreByteBounded pins the resident-set bound on the
// non-Management transport. http.Client.Timeout bounds how long one attempt may
// take, not how many bytes it buffers, so before the repair a misbehaving or
// proxied Graph/ARM/Defender/PowerBI origin -- including any configured *-Host
// override -- could grow the Hosted Runner's heap by whatever fit in a
// thirty-second transfer, affecting every other plugin in the process.
func TestOrdinaryAPIResponsesAreByteBounded(t *testing.T) {
	if maximumAPIResponseBody != managementMaximumResponseBody {
		t.Fatalf("ordinary bound %d and Management bound %d have diverged",
			maximumAPIResponseBody, managementMaximumResponseBody)
	}

	t.Run("declared-length-is-refused-before-reading", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
				_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600}`)
				return
			}
			// Declared far larger than the bound, so the body is never read.
			w.Header().Set("Content-Length", strconv.Itoa(4*maximumAPIResponseBody))
			_, _ = w.Write(oversizeBody(1 << 10))
		}))
		defer server.Close()
		client := newBoundsTestClient(t, server)
		_, err := client.request(t.Context(), http.MethodGet, server.URL+"/v1.0/users", "https://graph.microsoft.com/.default", nil)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeds %d bytes", maximumAPIResponseBody)) {
			t.Fatalf("error=%v, want the declared length refused against the %d byte bound", err, maximumAPIResponseBody)
		}
	})

	t.Run("undeclared-body-is-truncated-and-refused", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
				_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600}`)
				return
			}
			// No Content-Length: chunked, so only the read bound can stop it.
			_, _ = w.Write(oversizeBody(maximumAPIResponseBody))
		}))
		defer server.Close()
		client := newBoundsTestClient(t, server)
		_, err := client.request(t.Context(), http.MethodGet, server.URL+"/v1.0/users", "https://graph.microsoft.com/.default", nil)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeds %d bytes", maximumAPIResponseBody)) {
			t.Fatalf("error=%v, want an unbounded chunked body refused at the %d byte bound", err, maximumAPIResponseBody)
		}
	})

	t.Run("response-at-the-bound-is-accepted", func(t *testing.T) {
		body := oversizeBody(1 << 10)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
				_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600}`)
				return
			}
			_, _ = w.Write(body)
		}))
		defer server.Close()
		client := newBoundsTestClient(t, server)
		got, err := client.request(t.Context(), http.MethodGet, server.URL+"/v1.0/users", "https://graph.microsoft.com/.default", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("ordinary response was altered: %d bytes, want %d", len(got), len(body))
		}
	})

	t.Run("failed-request-still-reports-its-status", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
				_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600}`)
				return
			}
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write(oversizeBody(maximumAPIErrorBody))
		}))
		defer server.Close()
		client := newBoundsTestClient(t, server)
		_, err := client.request(t.Context(), http.MethodGet, server.URL+"/v1.0/users", "https://graph.microsoft.com/.default", nil)
		if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
			t.Fatalf("error=%v, want the HTTP status of the failed request", err)
		}
	})
}

// TestTokenResponsesAreByteBounded pins the same bound on the OAuth exchange,
// which holds a handful of short fields and has no reason to buffer more.
func TestTokenResponsesAreByteBounded(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(append([]byte(`{"access_token":"token","expires_in":3600,"padding":"`),
			append(bytes.Repeat([]byte("a"), maximumTokenResponseBody), []byte(`"}`)...)...))
	}))
	defer server.Close()
	client := newBoundsTestClient(t, server)
	_, err := client.token(t.Context(), "https://graph.microsoft.com/.default")
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeds %d bytes", maximumTokenResponseBody)) {
		t.Fatalf("error=%v, want the token response refused at the %d byte bound", err, maximumTokenResponseBody)
	}
}

// TestReadCappedBodyBoundary pins the exact inclusive boundary, so the bound
// cannot be made off-by-one without a failure here.
func TestReadCappedBodyBoundary(t *testing.T) {
	for _, tc := range []struct {
		name          string
		size          int
		contentLength int64
		reject        bool
	}{
		{name: "under-the-bound", size: 7},
		{name: "exactly-at-the-bound", size: 16},
		{name: "one-byte-over", size: 17, reject: true},
		{name: "declared-over", size: 1, contentLength: 17, reject: true},
		{name: "undeclared-length", size: 16, contentLength: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte("x"), tc.size)
			resp := &http.Response{
				Body:          io.NopCloser(bytes.NewReader(payload)),
				ContentLength: int64(len(payload)),
			}
			if tc.contentLength != 0 {
				resp.ContentLength = tc.contentLength
			}
			got, err := readCappedBody(resp, 16)
			if tc.reject {
				if err == nil || !strings.Contains(err.Error(), "exceeds 16 bytes") {
					t.Fatalf("error=%v, want the bound enforced", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("payload=%q, want %q", got, payload)
			}
		})
	}
}
