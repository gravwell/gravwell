package microsoft

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"golang.org/x/time/rate"
)

type failingReader struct {
	read bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, errors.New("synthetic partial read failure")
	}
	r.read = true
	copy(p, "partial")
	return len("partial"), nil
}

func TestManagementURLPolicyRejectsOriginTenantAndSensitiveQueryChanges(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	prefix := server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/"
	if err := client.validateManagementURL(prefix + "subscriptions/list"); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(server.URL, "https://")
	for name, endpoint := range map[string]string{
		"downgrade":       "http://" + host + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/list",
		"userinfo":        "https://user:pass@" + host + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/list",
		"wrong-tenant":    server.URL + "/api/v1.0/other/activity/feed/subscriptions/list",
		"wrong-api-path":  server.URL + "/api/v2.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/list",
		"sensitive-query": prefix + "subscriptions/list?access_token=forbidden",
		"fragment":        prefix + "subscriptions/list#secret",
	} {
		t.Run(name, func(t *testing.T) {
			if err := client.validateManagementURL(endpoint); err == nil {
				t.Fatalf("accepted %s", endpoint)
			}
		})
	}
}

func TestManagementPublisherIdentifierIsMandatoryAndPinned(t *testing.T) {
	conf := testClientConfig(t, "https://manage.office.com")
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "https://manage.office.com/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/list"
	got, err := client.withManagementPublisherIdentifier(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(got)
	if err != nil || parsed.Query().Get("PublisherIdentifier") != managementTestPublisher {
		t.Fatalf("publisher URL=%q error=%v", got, err)
	}
	if _, err := client.withManagementPublisherIdentifier(endpoint + "?PublisherIdentifier=55555555-5555-4555-8555-555555555555"); err == nil {
		t.Fatal("changed PublisherIdentifier accepted")
	}
	client.conf.Publisher_Identifier = ""
	if _, err := client.withManagementPublisherIdentifier(endpoint); err == nil {
		t.Fatal("missing Publisher-Identifier accepted")
	}
}

func TestManagementCrossOriginRedirectDispatchesZeroTargetRequests(t *testing.T) {
	var sinkCalls atomic.Int64
	sink := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sinkCalls.Add(1) }))
	defer sink.Close()
	var sourceCalls atomic.Int64
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		http.Redirect(w, r, sink.URL+"/capture", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	conf := testClientConfig(t, source.URL)
	conf.Management_Host = source.URL
	client, err := NewClient(conf, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.tokens[client.conf.Management_Scope] = cachedToken{Value: "synthetic-bearer", Expires: time.Now().Add(time.Hour)}
	endpoint := source.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content"
	if _, err := client.managementRequest(t.Context(), http.MethodGet, endpoint); err == nil {
		t.Fatal("cross-origin redirect accepted")
	}
	if sourceCalls.Load() != 1 || sinkCalls.Load() != 0 {
		t.Fatalf("source calls=%d sink calls=%d", sourceCalls.Load(), sinkCalls.Load())
	}
}

func TestManagementRedirectLoopIsBoundedAndSafe(t *testing.T) {
	var calls atomic.Int64
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		next := "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/a"
		if strings.HasSuffix(r.URL.Path, "/a") {
			next = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/b"
		}
		http.Redirect(w, r, server.URL+next, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.tokens[client.conf.Management_Scope] = cachedToken{Value: "synthetic-bearer", Expires: time.Now().Add(time.Hour)}
	endpoint := server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/a"
	_, err = client.managementRequest(t.Context(), http.MethodGet, endpoint)
	if err == nil || calls.Load() > 10 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
	for _, forbidden := range []string{endpoint, conf.Tenant_ID, conf.Client_ID, "synthetic-bearer"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("unsafe error=%q", err)
		}
	}
}

func TestManagementOffOriginNextPageIsRejectedBeforeDispatch(t *testing.T) {
	const contentType = "Audit.Exchange"
	var sinkCalls atomic.Int64
	sink := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sinkCalls.Add(1) }))
	defer sink.Close()
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			w.Header().Set("NextPageUri", sink.URL+"/capture?contentType="+contentType)
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err == nil {
		t.Fatal("off-origin NextPageUri accepted")
	}
	if sinkCalls.Load() != 0 || len(runtime.Entries()) != 0 {
		t.Fatalf("sink calls=%d entries=%d", sinkCalls.Load(), len(runtime.Entries()))
	}
}

func TestManagementRetryAfterParsingAndDeadlineDeferral(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		raw  string
		want time.Duration
	}{
		{"12", 12 * time.Second},
		{now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second},
		{"malformed", 7 * time.Second},
	} {
		got, err := managementRetryDelay(tc.raw, now, 7*time.Second)
		if err != nil || got != tc.want {
			t.Fatalf("raw=%q got=%s error=%v", tc.raw, got, err)
		}
	}
	if _, err := managementRetryDelay("999999999999999999999", now, time.Second); err == nil {
		t.Fatal("overflowing Retry-After accepted")
	}

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":{"code":"AF429"}}`)
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.tokens[client.conf.Management_Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	endpoint := server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/list"
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err = client.managementRequest(ctx, http.MethodGet, endpoint)
	if err == nil || !strings.Contains(err.Error(), "deferred") || calls.Load() != 1 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
}

func TestManagementResponseLimitUsesLimitPlusOne(t *testing.T) {
	payload := strings.Repeat("x", managementMaximumResponseBody+1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.tokens[client.conf.Management_Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	endpoint := server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content"
	if _, err := client.managementRequest(t.Context(), http.MethodGet, endpoint); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error=%v", err)
	}
}

func TestManagementDeclaredChunkedDecompressedAndPartialResponseBounds(t *testing.T) {
	t.Run("declared-over-limit", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(managementMaximumResponseBody+1))
			_, _ = io.WriteString(w, "small")
		}))
		defer server.Close()
		client := managementSecurityClient(t, server)
		if _, err := client.managementRequest(t.Context(), http.MethodGet, managementSecurityEndpoint(server.URL)); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("chunked-over-limit", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher := w.(http.Flusher)
			for remaining := managementMaximumResponseBody + 1; remaining > 0; {
				chunk := 32 << 10
				if remaining < chunk {
					chunk = remaining
				}
				_, _ = w.Write(bytes.Repeat([]byte{'x'}, chunk))
				flusher.Flush()
				remaining -= chunk
			}
		}))
		defer server.Close()
		client := managementSecurityClient(t, server)
		if _, err := client.managementRequest(t.Context(), http.MethodGet, managementSecurityEndpoint(server.URL)); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("decompressed-over-limit", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			writer := gzip.NewWriter(w)
			_, _ = writer.Write(bytes.Repeat([]byte{'x'}, managementMaximumResponseBody+1))
			_ = writer.Close()
		}))
		defer server.Close()
		client := managementSecurityClient(t, server)
		if _, err := client.managementRequest(t.Context(), http.MethodGet, managementSecurityEndpoint(server.URL)); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("partial-read", func(t *testing.T) {
		if _, err := readLimitedResponse(&failingReader{}, 64); err == nil {
			t.Fatal("partial read failure accepted")
		}
	})
}

func TestManagementRetryWaitHonorsCancellation(t *testing.T) {
	requested := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested <- struct{}{}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := managementSecurityClient(t, server)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := client.managementRequest(ctx, http.MethodGet, managementSecurityEndpoint(server.URL))
		done <- err
	}()
	<-requested
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestManagementEveryRetryAttemptIsRateLimited(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()
	client := managementSecurityClient(t, server)
	client.limiter = rate.NewLimiter(rate.Every(40*time.Millisecond), 1)
	started := time.Now()
	if _, err := client.managementRequest(t.Context(), http.MethodGet, managementSecurityEndpoint(server.URL)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond || calls.Load() != 2 {
		t.Fatalf("elapsed=%s calls=%d", elapsed, calls.Load())
	}
}

func managementSecurityClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.tokens[client.conf.Management_Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	return client
}

func managementSecurityEndpoint(serverURL string) string {
	return serverURL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/list"
}

func TestManagementHTTPErrorDoesNotEchoBodyOrIdentifiers(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"bad","message":"body-secret 11111111-1111-1111-1111-111111111111"}}`)
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.tokens[client.conf.Management_Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	endpoint := server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content"
	_, err = client.managementRequest(t.Context(), http.MethodGet, endpoint)
	if err == nil {
		t.Fatal("expected HTTP failure")
	}
	for _, forbidden := range []string{"body-secret", conf.Tenant_ID, server.URL, "token"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("unsafe error=%q", err)
		}
	}
	var httpErr *managementHTTPError
	if !errors.As(err, &httpErr) || httpErr.status != http.StatusBadRequest {
		t.Fatalf("error=%T %v", err, err)
	}
}

// The URL policy tested only that the raw EscapedPath started with the tenant's
// feed prefix. A path may carry that prefix as written and still address
// something else entirely once a server resolves dot segments or percent-decodes
// before routing, so every ambiguous form is refused outright.
func TestManagementURLPolicyRejectsAmbiguousPathForms(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	prefix := server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/"
	host := strings.TrimPrefix(server.URL, "https://")

	for name, endpoint := range map[string]string{
		"literal-parent":          prefix + "../../../evil",
		"literal-parent-inside":   prefix + "audit/../../../../evil",
		"literal-current":         prefix + "./audit/content",
		"encoded-parent-lower":    prefix + "%2e%2e/evil",
		"encoded-parent-upper":    prefix + "%2E%2E/evil",
		"half-encoded-parent":     prefix + ".%2e/evil",
		"encoded-current":         prefix + "%2e/audit/content",
		"encoded-parent-and-sep":  prefix + "%2e%2e%2fevil",
		"encoded-forward-slash":   prefix + "audit%2fcontent%2f..%2fevil",
		"encoded-back-slash":      prefix + "audit%5c..%5cevil",
		"double-encoded-parent":   prefix + "%252e%252e/evil",
		"literal-backslash":       prefix + `audit\..\evil`,
		"backslash-in-query":      prefix + `audit/content?contentType=Audit.Exchange\evil`,
		"userinfo":                "https://user:pass@" + host + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content",
		"userinfo-no-credentials": "https://@" + host + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content",
		"over-length":             prefix + "audit/" + strings.Repeat("u", managementMaximumURLLength),
	} {
		t.Run(name, func(t *testing.T) {
			if err := client.validateManagementURL(endpoint); err == nil {
				t.Fatalf("accepted %s", endpoint)
			}
		})
	}

	// Valid same-origin forms keep working, including the continuation shape the
	// source actually publishes.
	for name, endpoint := range map[string]string{
		"feed-root":    prefix,
		"subscription": prefix + "subscriptions/list?PublisherIdentifier=" + managementTestPublisher,
		"content-list": prefix + "subscriptions/content?contentType=Audit.Exchange&startTime=2026-10-08T00:00:00Z&endTime=2026-10-08T12:00:00Z",
		"content-blob": prefix + "audit/20231002080000000041234$20231002080000000041234$audit_exchange$Audit_Exchange$na0012",
		"next-page":    prefix + "subscriptions/content?contentType=Audit.Exchange&nextPage=20231002080000000041234",
		"dot-in-name":  prefix + "audit/content.1",
		"double-slash": prefix + "audit//content",
	} {
		t.Run("accepts-"+name, func(t *testing.T) {
			if err := client.validateManagementURL(endpoint); err != nil {
				t.Fatalf("rejected a valid URL %s: %v", endpoint, err)
			}
		})
	}
}

// The same validator has to guard every URL class, not just the one the plugin
// builds for itself: a listed contentUri, a NextPageUri continuation and a
// redirect target are all source-controlled.
func TestManagementURLPolicyAppliesToEveryURLClass(t *testing.T) {
	const contentType = "Audit.Exchange"
	const feed = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/"
	traversal := feed + "%2e%2e%2f%2e%2e%2fevil"

	t.Run("content-uri", func(t *testing.T) {
		var escaped atomic.Int64
		var server *httptest.Server
		server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
				writeEnabledSubscription(w, contentType)
			case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
				_, _ = fmt.Fprintf(w, `[{"contentType":%q,"contentId":"content-1","contentUri":%q,"contentCreated":"2026-10-08T11:30:00Z"}]`,
					contentType, server.URL+traversal)
			case strings.Contains(r.URL.Path, "evil"):
				escaped.Add(1)
				_, _ = io.WriteString(w, `[]`)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
		if _, err := plugin.Handle(t.Context(), rt); err == nil {
			t.Fatal("a traversal contentUri was accepted")
		}
		if escaped.Load() != 0 || len(rt.Entries()) != 0 {
			t.Fatalf("dispatched to the traversal path %d times, entries=%d", escaped.Load(), len(rt.Entries()))
		}
	})

	t.Run("next-page-uri", func(t *testing.T) {
		var escaped atomic.Int64
		var server *httptest.Server
		server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
				writeEnabledSubscription(w, contentType)
			case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
				w.Header().Set("NextPageUri", server.URL+traversal+"?contentType="+contentType)
				_, _ = io.WriteString(w, `[]`)
			case strings.Contains(r.URL.Path, "evil"):
				escaped.Add(1)
				_, _ = io.WriteString(w, `[]`)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
		if _, err := plugin.Handle(t.Context(), rt); err == nil {
			t.Fatal("a traversal NextPageUri was accepted")
		}
		if escaped.Load() != 0 {
			t.Fatalf("dispatched to the traversal continuation %d times", escaped.Load())
		}
	})

	t.Run("redirect", func(t *testing.T) {
		var escaped atomic.Int64
		var server *httptest.Server
		server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "evil") {
				escaped.Add(1)
				_, _ = io.WriteString(w, `[]`)
				return
			}
			http.Redirect(w, r, server.URL+traversal, http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		client := managementSecurityClient(t, server)
		if _, err := client.managementRequest(t.Context(), http.MethodGet, server.URL+feed+"audit/content-1"); err == nil {
			t.Fatal("a same-origin traversal redirect was followed")
		}
		if escaped.Load() != 0 {
			t.Fatalf("followed the traversal redirect %d times", escaped.Load())
		}
	})
}

// managementInBandURL builds a Management Activity URL whose unmodified form
// fits managementMaximumURLLength and whose dispatched form -- the same URL
// with the mandatory PublisherIdentifier applied -- does not. Acceptance used
// to measure the first form and dispatch the second, so a URL in this band was
// persisted by the listing and then refused by every request that followed.
func managementInBandURL(t *testing.T, serverURL string) string {
	t.Helper()
	base := serverURL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"
	filler := managementMaximumURLLength - len(base) - 8
	if filler < 1 {
		t.Fatalf("server URL %q leaves no room below the %d-byte URL bound", serverURL, managementMaximumURLLength)
	}
	return base + strings.Repeat("u", filler)
}

// Acceptance and dispatch have to answer the same question about the same
// bytes. validateManagementURL measured the URL as the source wrote it while
// managementRequest dispatched the URL with PublisherIdentifier applied, so a
// descriptor or continuation in the band just below the bound was accepted,
// persisted, and then refused by every dispatch until the window it belonged to
// crossed the source's retention floor.
func TestManagementURLAcceptanceAndDispatchShareOneEffectivePolicy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)

	raw := managementInBandURL(t, server.URL)
	if len(raw) > managementMaximumURLLength {
		t.Fatalf("fixture URL is %d bytes, want at most %d", len(raw), managementMaximumURLLength)
	}
	effective, err := client.withManagementPublisherIdentifier(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(effective) <= managementMaximumURLLength {
		t.Fatalf("fixture dispatch URL is %d bytes, want more than %d", len(effective), managementMaximumURLLength)
	}
	if err := client.validateManagementURL(raw); err == nil {
		t.Fatalf("acceptance admitted a %d-byte URL whose %d-byte dispatch form is over the bound", len(raw), len(effective))
	}
	if _, err := client.managementRequest(t.Context(), http.MethodGet, raw); err == nil {
		t.Fatal("dispatch admitted the same URL acceptance must refuse")
	}

	// A URL that still fits once the PublisherIdentifier is applied is accepted,
	// and the enforcement that pins the identifier is unchanged.
	fits := raw[:len(raw)-128]
	if err := client.validateManagementURL(fits); err != nil {
		t.Fatalf("rejected a URL whose dispatch form fits the bound: %v", err)
	}
	if err := client.validateManagementURL(fits + "?PublisherIdentifier=55555555-5555-4555-8555-555555555555"); err == nil {
		t.Fatal("acceptance admitted a changed PublisherIdentifier")
	}
}

// A descriptor or continuation the URL policy refuses is bounded loss of that
// one item. It used to be unbounded loss of the whole selector: the listing
// failed on the same page on every poll and the delivery loop failed on the
// same stored contentUri on every poll, so neither could ever get past it.
func TestManagementRefusedURLRecoversWithoutAStall(t *testing.T) {
	const contentType = "Audit.Exchange"
	const selector = "microsoft365-audit-exchange"

	t.Run("listed-descriptor", func(t *testing.T) {
		var server *httptest.Server
		var contentCalls atomic.Int64
		server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
				writeEnabledSubscription(w, contentType)
			case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
				_, _ = fmt.Fprintf(w, `[{"contentType":%q,"contentId":"content-1","contentUri":%q,"contentCreated":"2026-10-08T11:30:00Z"}]`,
					contentType, managementInBandURL(t, server.URL))
			case strings.Contains(r.URL.Path, "/audit/"):
				contentCalls.Add(1)
				_, _ = io.WriteString(w, `[]`)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
		if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "refused") {
			t.Fatalf("first poll error=%v, want a reported refusal", err)
		}
		if got := rt.logsContaining("WARN", "refused content descriptors"); len(got) != 1 {
			t.Fatalf("refusal warnings=%d, want exactly 1 (%+v)", len(got), got)
		}
		if contentCalls.Load() != 0 || len(rt.Entries()) != 0 {
			t.Fatalf("dispatched %d content requests and wrote %d entries for a refused descriptor", contentCalls.Load(), len(rt.Entries()))
		}

		// The refusal is durable progress, not a stall: the next poll completes
		// the window instead of re-listing the same refused page forever.
		plugin = newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("second poll error=%v, want the window to complete", err)
		}
		state, err := loadManagementState(rt, managementStateKey("microsoft", selector))
		if err != nil {
			t.Fatal(err)
		}
		if !state.CompletedThrough.Equal(managementTestNow) {
			t.Fatalf("completed_through=%s, want %s", state.CompletedThrough, managementTestNow)
		}
	})

	t.Run("stored-descriptor", func(t *testing.T) {
		var server *httptest.Server
		var contentCalls atomic.Int64
		server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
				writeEnabledSubscription(w, contentType)
			case strings.Contains(r.URL.Path, "/audit/"):
				contentCalls.Add(1)
				_, _ = io.WriteString(w, `[]`)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
		key := managementStateKey("microsoft", selector)
		stored := managementState{
			Version:     managementStateVersion,
			WindowStart: managementTestNow.Add(-time.Hour), WindowEnd: managementTestNow,
			TargetThrough: managementTestNow, ListingStarted: true, ListingComplete: true,
		}
		stored.appendContent(managementContent{
			ContentType: contentType, ContentID: "stored-1",
			ContentURI:     managementInBandURL(t, server.URL),
			ContentCreated: managementTestNow.Add(-30 * time.Minute),
		})
		if err := plugin.persistManagementCheckpoint(rt, key, &stored); err != nil {
			t.Fatal(err)
		}
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("stored refused contentUri error=%v, want bounded reported loss", err)
		}
		if got := rt.logsContaining("ERROR", "refused by the Microsoft Management Activity URL policy"); len(got) != 1 {
			t.Fatalf("loss reports=%d, want exactly 1 (%+v)", len(got), got)
		}
		if contentCalls.Load() != 0 {
			t.Fatalf("dispatched %d requests for a stored URL the policy refuses", contentCalls.Load())
		}
		state, err := loadManagementState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if !state.CompletedThrough.Equal(managementTestNow) || state.SkippedContents != 1 {
			t.Fatalf("completed_through=%s skipped=%d, want %s and 1", state.CompletedThrough, state.SkippedContents, managementTestNow)
		}
	})
}

// managementRefusedDescriptor publishes a descriptor the policy refuses for a
// reason that names nothing but the shape of the field: its contentCreated is
// not a timestamp. The contentUri is valid, because decodeManagementContent
// checks the URI first.
func managementRefusedDescriptor(serverURL, contentType, contentID string) string {
	return fmt.Sprintf(`{"contentType":%q,"contentId":%q,"contentUri":%q,"contentCreated":"not-a-timestamp"}`,
		contentType, contentID, serverURL+managementCapacityContentPath+contentID)
}

// Refused listing descriptors were counted only in a pass-local variable, and
// the report that published them ran after the listing loop. A page's
// continuation is durable before the next page is requested, so a later page
// that failed returned before the report ever ran -- and the refusals of every
// page already past that durable continuation were permanent loss that was
// never reported, never counted anywhere, and never seen again, because the next
// poll resumed from the continuation.
//
// TestManagementRefusedDescriptorsAreCountedDurablyAcrossALaterPageFailure is
// exactly that sequence: page one refuses descriptors and publishes its
// continuation, page two fails, the runner restarts, and the loss has to still
// be counted, still be reported once, and still let the window finish.
func TestManagementRefusedDescriptorsAreCountedDurablyAcrossALaterPageFailure(t *testing.T) {
	const contentType = "Audit.Exchange"
	const selector = "microsoft365-audit-exchange"
	const refused = 2

	var server *httptest.Server
	var firstPage, secondPage, contentCalls atomic.Int64
	secondPageWorks := &atomic.Bool{}
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			if r.URL.Query().Get("page") == "2" {
				secondPage.Add(1)
				if !secondPageWorks.Load() {
					// An undecodable page body. The listing fails here, a page
					// after the one whose continuation is already durable, which
					// is the condition the accounting has to survive.
					_, _ = io.WriteString(w, `{`)
					return
				}
				_, _ = io.WriteString(w, `[]`)
				return
			}
			firstPage.Add(1)
			w.Header().Set("NextPageUri", server.URL+r.URL.Path+"?contentType="+url.QueryEscape(contentType)+"&page=2")
			_, _ = fmt.Fprintf(w, `[%s,%s,%s]`,
				managementRefusedDescriptor(server.URL, contentType, "refused-1"),
				managementRefusedDescriptor(server.URL, contentType, "refused-2"),
				managementDescriptor(server.URL, contentType, "accepted-1", managementCapacityContentPath+"accepted-1"))
		case strings.Contains(r.URL.Path, managementCapacityContentPath+"accepted-1"):
			contentCalls.Add(1)
			_, _ = io.WriteString(w, `[{"Id":"event-1","CreationTime":"2026-10-08T11:50:00Z","Operation":"MailItemsAccessed"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", selector)

	// Pass one: page one's refusals and its continuation become durable, then
	// page two fails and the pass returns that failure.
	plugin := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err == nil {
		t.Fatal("the undecodable second page was not reported as this pass's error")
	}
	afterFailure, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.RefusedContents != refused || afterFailure.RefusedUnreported != refused {
		t.Fatalf("durable refusal accounting=%d/%d, want %d counted and %d still to report",
			afterFailure.RefusedContents, afterFailure.RefusedUnreported, refused, refused)
	}
	if !strings.Contains(afterFailure.RefusedReason, "contentCreated") {
		t.Fatalf("durable reason=%q, want the refusal named for the operator", afterFailure.RefusedReason)
	}
	if len(afterFailure.RefusedReason) > managementMaximumRefusedReasonLength {
		t.Fatalf("durable reason is %d bytes, over the %d-byte bound", len(afterFailure.RefusedReason), managementMaximumRefusedReasonLength)
	}
	// Non-vacuous: the pass genuinely never got as far as reporting, which is
	// where the loss used to disappear.
	if got := rt.logsContaining("WARN", "refused content descriptors"); len(got) != 0 {
		t.Fatalf("refusal warnings after the failed pass=%d, want none (%+v)", len(got), got)
	}
	if got := rt.warnStateContaining("refused"); len(got) != 0 {
		t.Fatalf("published refusal statuses after the failed pass=%d, want none (%q)", len(got), got)
	}
	// The continuation is exactly the page the pass failed on, and the accepted
	// descriptor it listed beside the refusals is retained.
	if afterFailure.Pages != 1 || afterFailure.NextPage == "" || !strings.Contains(afterFailure.NextPage, "page=2") {
		t.Fatalf("continuation state pages=%d next=%q, want page one durable and page two outstanding", afterFailure.Pages, afterFailure.NextPage)
	}
	if len(afterFailure.Contents) != 1 || afterFailure.Contents[0].ContentID != "accepted-1" {
		t.Fatalf("descriptors=%+v, want only the accepted one", afterFailure.Contents)
	}
	if got := contentCalls.Load(); got != 0 {
		t.Fatalf("content requests=%d before the listing completed", got)
	}

	// Pass two is a restart on the same state store with page two still broken.
	// The count may not move: page one is behind the durable continuation and is
	// never requested again.
	plugin = newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err == nil {
		t.Fatal("the restarted pass did not report the still-broken second page")
	}
	afterRestart, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart.RefusedContents != refused || afterRestart.RefusedUnreported != refused {
		t.Fatalf("durable refusal accounting after a restart=%d/%d, want %d/%d unchanged",
			afterRestart.RefusedContents, afterRestart.RefusedUnreported, refused, refused)
	}
	if got := firstPage.Load(); got != 1 {
		t.Fatalf("the first page was requested %d times, so the refusals could be counted more than once", got)
	}
	if got := secondPage.Load(); got != 2 {
		t.Fatalf("second-page requests=%d, want the continuation followed on both passes", got)
	}

	// Clearing the log and status channels loses nothing: the count is in the
	// state document, not in the channel it is reported on.
	rt.clearCapturedReports()
	cleared, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.RefusedContents != refused || cleared.RefusedUnreported != refused {
		t.Fatalf("durable refusal accounting after the channels were cleared=%d/%d, want %d/%d",
			cleared.RefusedContents, cleared.RefusedUnreported, refused, refused)
	}

	// Pass three completes the listing, so the refusals that have been durable
	// across two failures and a restart are finally reported -- once.
	secondPageWorks.Store(true)
	plugin = newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error=%v, want the refusal reported as this pass's error", err)
	}
	warnings := rt.logsContaining("WARN", "refused content descriptors")
	if len(warnings) != 1 {
		t.Fatalf("refusal warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["count"]; got != fmt.Sprint(refused) {
		t.Fatalf("reported count=%q, want %d", got, refused)
	}
	if got := warnings[0].Params["refused_total"]; got != fmt.Sprint(refused) {
		t.Fatalf("reported cumulative total=%q, want %d", got, refused)
	}
	if got := warnings[0].Params["reason"]; !strings.Contains(got, "contentCreated") {
		t.Fatalf("reported reason=%q, want the refusal named for the operator", got)
	}
	published := rt.warnStateContaining("refused 2 content descriptor(s)")
	if len(published) != 1 {
		t.Fatalf("published refusal statuses=%d, want exactly 1 (%q)", len(published), published)
	}
	if !strings.Contains(published[0], "2 descriptor(s) have been permanently refused") {
		t.Fatalf("published status=%q, want the cumulative total named", published[0])
	}
	reported, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if reported.RefusedContents != refused || reported.RefusedUnreported != 0 || reported.RefusedReason != "" {
		t.Fatalf("accounting after the report=%+v, want the count kept and the report acknowledged",
			[]any{reported.RefusedContents, reported.RefusedUnreported, reported.RefusedReason})
	}

	// Pass four delivers what the window did list and completes the checkpoint.
	// The refusal is not reported again and the count does not move.
	plugin = newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("delivery pass error=%v, want the window completed", err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want the one accepted descriptor delivered", got)
	}
	if got := contentCalls.Load(); got != 1 {
		t.Fatalf("content requests=%d, want exactly one", got)
	}
	if got := rt.logsContaining("WARN", "refused content descriptors"); len(got) != 1 {
		t.Fatalf("refusal warnings after the delivery pass=%d, want the one already published (%+v)", len(got), got)
	}
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if final.RefusedContents != refused || final.RefusedUnreported != 0 {
		t.Fatalf("final refusal accounting=%d/%d, want %d/0", final.RefusedContents, final.RefusedUnreported, refused)
	}
	if final.hasPending() || final.CompletedThrough.IsZero() {
		t.Fatalf("final state=%+v, want the checkpoint advanced past the window", final)
	}
	// The refusal itself stands: the invalid descriptors were never admitted and
	// nothing was ever dispatched for them.
	if final.hasContent("refused-1") || final.hasReceipt("refused-1") {
		t.Fatal("a refused descriptor was re-admitted")
	}
}

// TestManagementRefusedAccountingIsNotDoubleCountedWhenTheReportCannotBeAcknowledged
// pins the other retry boundary. The report is acknowledged in the state
// document, so a failed acknowledgement has to leave the durable count exactly
// where it was: the report may repeat, the count may not double.
func TestManagementRefusedAccountingIsNotDoubleCountedWhenTheReportCannotBeAcknowledged(t *testing.T) {
	const selector = "microsoft365-audit-exchange"
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", selector)
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, selector, syncer.Sync)

	state := &managementState{
		Version:           managementStateVersion,
		WindowStart:       managementTestNow.Add(-time.Hour),
		WindowEnd:         managementTestNow,
		RefusedContents:   2,
		RefusedUnreported: 2,
		RefusedReason:     "Microsoft Management Activity content descriptor has an invalid contentCreated",
	}
	if err := plugin.persistManagementState(rt, key, state); err != nil {
		t.Fatal(err)
	}
	syncer.failAt.Store(syncer.calls.Load() + 1)
	if err := plugin.reportManagementRefusedListing(rt, key, datasets[selector], state, false, ""); err == nil {
		t.Fatal("a failed acknowledgement was reported as success")
	} else if !strings.Contains(err.Error(), "synchronize") {
		t.Fatalf("error=%v, want the failed synchronization reported", err)
	}
	// The operator was still told, and the durable accounting is untouched, so
	// the next pass reports the same refusals once more rather than counting
	// them again.
	if got := rt.logsContaining("WARN", "refused content descriptors"); len(got) != 1 {
		t.Fatalf("refusal warnings=%d, want exactly 1 (%+v)", len(got), got)
	}
	stored, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefusedContents != 2 || stored.RefusedUnreported != 2 {
		t.Fatalf("durable accounting=%d/%d, want 2/2 after the failed acknowledgement",
			stored.RefusedContents, stored.RefusedUnreported)
	}
}

// TestManagementRefusedAccountingIsStructurallyValidated pins the bounds on the
// new durable fields, so a stored document cannot carry accounting that is
// internally impossible or a reason of unbounded length.
func TestManagementRefusedAccountingIsStructurallyValidated(t *testing.T) {
	// The bound has to admit the widest reason the presentation can produce, or
	// the persist would refuse a reason the listing legitimately recorded.
	widest := displayVendorValue(strings.Repeat("\r", maximumDisplayedVendorValue*4), maximumDisplayedVendorValue)
	if len(widest) > managementMaximumRefusedReasonLength {
		t.Fatalf("the widest escaped reason is %d bytes, over the %d-byte durable bound", len(widest), managementMaximumRefusedReasonLength)
	}

	for _, tc := range []struct {
		name       string
		contents   int
		unreported int
		reason     string
		valid      bool
	}{
		{"none", 0, 0, "", true},
		{"all-reported", 4, 0, "", true},
		{"all-outstanding", 4, 4, "a reason", true},
		{"reason-at-the-bound", 1, 1, strings.Repeat("r", managementMaximumRefusedReasonLength), true},
		{"negative-total", -1, 0, "", false},
		{"negative-outstanding", 4, -1, "", false},
		{"more-outstanding-than-counted", 1, 2, "", false},
		{"reason-one-byte-over-the-bound", 1, 1, strings.Repeat("r", managementMaximumRefusedReasonLength+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := managementState{
				Version:           managementStateVersion,
				RefusedContents:   tc.contents,
				RefusedUnreported: tc.unreported,
				RefusedReason:     tc.reason,
			}
			err := validateManagementProgressFields(state)
			if tc.valid && err != nil {
				t.Fatalf("valid refusal accounting was refused: %v", err)
			}
			if !tc.valid {
				if err == nil {
					t.Fatal("impossible refusal accounting was accepted")
				}
				if !strings.Contains(err.Error(), "permanent-loss accounting") {
					t.Fatalf("error=%v, want the accounting named", err)
				}
			}
		})
	}
}

// A Management subscription the source reports as disabled inside the bounded
// start-suppression interval collects nothing. The poll returned no error, so
// hosted/job.go cleared the previous one microseconds later and the selector
// went quiet with nothing an operator could see.
func TestManagementDisabledSubscriptionInsideSuppressionIsVisible(t *testing.T) {
	const contentType = "Audit.Exchange"
	const selector = "microsoft365-audit-exchange"
	var starts atomic.Int64
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			_, _ = fmt.Fprintf(w, `[{"contentType":%q,"status":"disabled"}]`, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/start"):
			starts.Add(1)
			_, _ = io.WriteString(w, `{"contentType":"Audit.Exchange","status":"enabled"}`)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	now := managementTestNow
	plugin := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	plugin.now = func() time.Time { return now }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatalf("subscription starts=%d, want 1", starts.Load())
	}
	key := managementStateKey("microsoft", selector)
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	// A start call is a request, not a confirmation: the next list is what
	// observes the status, and nothing in this plugin requires the optimistic
	// value the start path used to record.
	if state.SubscriptionStatus == "enabled" {
		t.Fatal("an unconfirmed start recorded the subscription as enabled")
	}

	// Inside the suppression interval the source still reports the subscription
	// as disabled. The start is correctly suppressed, and the operator has to be
	// told why the selector is collecting nothing.
	now = managementTestNow.Add(managementStartSuppression / 2)
	plugin = newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	plugin.now = func() time.Time { return now }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatalf("subscription starts=%d inside the suppression interval, want 1", starts.Load())
	}
	warnings := rt.logsContaining("WARN", "subscription is not enabled")
	if len(warnings) != 1 {
		t.Fatalf("suppression warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
	}
	if warnings[0].Params["selector"] != selector || warnings[0].Params["status"] != "disabled" {
		t.Fatalf("suppression warning params=%+v", warnings[0].Params)
	}
	if got := rt.warnStateContaining("subscription"); len(got) == 0 {
		t.Fatal("no operator-visible warning state was published for a disabled subscription")
	}

	// The suppression stays bounded: once the interval has elapsed the plugin
	// asks the source to start the subscription again.
	now = managementTestNow.Add(managementStartSuppression + time.Minute)
	plugin = newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	plugin.now = func() time.Time { return now }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 2 {
		t.Fatalf("subscription starts=%d after the suppression interval, want 2", starts.Load())
	}
}

// A source-supplied contentId is decoded verbatim: decodeManagementContent
// constrains its length and nothing else, so it can carry CR, LF or any other
// control character. The default RFC5424 file sink escapes '\', '"' and ']' in
// a parameter value and writes every other byte through unchanged, so every
// operator-visible insertion of that identifier used to be a CWE-117 log
// injection: the embedded CR LF ended the physical line and the remainder of
// the identifier was read as a further complete record this plugin never wrote.
const managementInjectionSelector = "microsoft365-audit-exchange"

// managementInjectionContentID is such an identifier. After the forged record
// it carries the remaining shapes a terminal or a log parser can be driven
// with: a tab, a NUL, an ESC, a NEL, and a backslash that must not be able to
// imitate an escape this presentation produces.
const managementInjectionContentID = "content-1\r\n<12>1 2026-10-08T12:00:00Z runner microsoft - - - forged record\r\n" +
	"\ttab\x00nul\x1besc\u0085nel\\slash"

// managementInjectionDisplayed is the only presentation of that identifier an
// operator may be shown: one physical line, every control character visible.
const managementInjectionDisplayed = `content-1\r\n<12>1 2026-10-08T12:00:00Z runner microsoft - - - forged record\r\n\ttab\x00nul\x1besc\x85nel\\slash`

// managementInjectionEvent is the one well-formed event the hostile blob holds.
const managementInjectionEvent = `{"Id":"event-1","CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}`

const (
	managementInjectionStalePath = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/stale-blob"
	managementInjectionFreshPath = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/fresh-blob"
)

// managementInjectionSource publishes one Audit.Exchange descriptor whose
// contentId is operator-hostile. The hostile text can only live in contentId: a
// contentUri carrying it is refused by validateManagementURL long before any
// log line, which is why the identifier is the field the Management API lets
// through to the operator's log. When expire is set the stale URI answers the
// way a blob past its URI's lifetime does, and relist decides whether the
// re-list that follows still names the identifier -- a successful refresh --
// or omits it, which is bounded permanent loss.
// contentID selects which hostile identifier the descriptor carries, so the
// same source serves the Cc control-character case and the Cf/Zl/Zp case.
type managementInjectionSource struct {
	created   time.Time
	body      string
	contentID string
	expire    bool
	relist    bool

	server *httptest.Server
	lists  atomic.Int64
	stale  atomic.Int64
	fresh  atomic.Int64
}

func (s *managementInjectionSource) start(t *testing.T) *httptest.Server {
	t.Helper()
	if s.created.IsZero() {
		s.created = managementTestNow.Add(-30 * time.Minute)
	}
	if s.body == "" {
		s.body = "[" + managementInjectionEvent + "]"
	}
	if s.contentID == "" {
		s.contentID = managementInjectionContentID
	}
	descriptorID, err := json.Marshal(s.contentID)
	if err != nil {
		t.Fatal(err)
	}
	s.server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			path := managementInjectionStalePath
			if s.lists.Add(1) > 1 {
				if !s.relist {
					_, _ = io.WriteString(w, "[]")
					return
				}
				path = managementInjectionFreshPath
			}
			_, _ = fmt.Fprintf(w, `[{"contentType":"Audit.Exchange","contentId":%s,"contentUri":%q,"contentCreated":%q}]`,
				descriptorID, s.server.URL+path, s.created.UTC().Format(time.RFC3339Nano))
		case strings.HasSuffix(r.URL.Path, managementInjectionStalePath):
			s.stale.Add(1)
			if s.expire {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":{"code":"AF20050","message":"expired"}}`)
				return
			}
			_, _ = io.WriteString(w, s.body)
		case strings.HasSuffix(r.URL.Path, managementInjectionFreshPath):
			s.fresh.Add(1)
			_, _ = io.WriteString(w, s.body)
		default:
			http.NotFound(w, r)
		}
	})
	t.Cleanup(s.server.Close)
	return s.server
}

// managementInjectionDigest is the semantic digest of a blob body, so a seeded
// state document can agree with what the source serves instead of tripping the
// changed-content replay path.
func managementInjectionDigest(t *testing.T, events ...string) string {
	t.Helper()
	raw := make([]json.RawMessage, 0, len(events))
	for _, event := range events {
		raw = append(raw, json.RawMessage(event))
	}
	digest, err := managementContentDigest(raw)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// seedManagementInjectionWindow publishes the window the next poll has to list,
// so the listing under test does not depend on window planning.
func seedManagementInjectionWindow(t *testing.T, plugin *Microsoft, rt *microsoftTestRuntime, key string) {
	t.Helper()
	state := managementState{
		Version:          managementStateVersion,
		CompletedThrough: managementTestNow.Add(-time.Hour),
		TargetThrough:    managementTestNow,
		WindowStart:      managementTestNow.Add(-time.Hour),
		WindowEnd:        managementTestNow,
	}
	if err := plugin.persistManagementCheckpoint(rt, key, &state); err != nil {
		t.Fatal(err)
	}
}

// seedManagementInjectionDelivery publishes the durable documents of a selector
// that has listed the hostile descriptor and is part way through delivering it.
// digest, eventIndex and replays choose which operator-visible branch of the
// delivery loop the next poll takes.
func seedManagementInjectionDelivery(t *testing.T, plugin *Microsoft, rt *microsoftTestRuntime, key, contentURI, digest string, eventIndex, replays int) {
	t.Helper()
	seedManagementInjectionDeliveryFor(t, plugin, rt, key, managementInjectionContentID, contentURI, digest, eventIndex, replays)
}

// seedManagementInjectionDeliveryFor is the same for an arbitrary hostile
// identifier. The in-flight ActiveContentID and the descriptor at ContentIndex
// name the same content, which is the relationship load validation requires.
func seedManagementInjectionDeliveryFor(t *testing.T, plugin *Microsoft, rt *microsoftTestRuntime, key, contentID, contentURI, digest string, eventIndex, replays int) {
	t.Helper()
	state := managementState{
		Version:             managementStateVersion,
		CompletedThrough:    managementTestNow.Add(-time.Hour),
		TargetThrough:       managementTestNow,
		WindowStart:         managementTestNow.Add(-time.Hour),
		WindowEnd:           managementTestNow,
		ListingStarted:      true,
		ListingComplete:     true,
		Pages:               1,
		ActiveContentID:     contentID,
		ActiveDigest:        digest,
		ActiveDigestChanges: replays,
		EventIndex:          eventIndex,
	}
	state.appendContent(managementContent{
		ContentType: "Audit.Exchange", ContentID: contentID,
		ContentURI: contentURI, ContentCreated: managementTestNow.Add(-30 * time.Minute),
	})
	if err := plugin.persistManagementCheckpoint(rt, key, &state); err != nil {
		t.Fatal(err)
	}
}

// assertManagementDisplayedContentID fails unless value is exactly the escaped
// presentation: no control character survived, and the escaped text an operator
// has to be able to read is present in full.
func assertManagementDisplayedContentID(t *testing.T, where, value string) {
	t.Helper()
	if strings.ContainsFunc(value, unicode.IsControl) {
		t.Fatalf("%s=%q retains a control character an operator log cannot carry", where, value)
	}
	if value != managementInjectionDisplayed {
		t.Fatalf("%s=%q, want the escaped presentation %q", where, value, managementInjectionDisplayed)
	}
}

// assertManagementDisplayedInStatus fails unless the published status embeds the
// escaped presentation and nothing that can end a line.
func assertManagementDisplayedInStatus(t *testing.T, status string) {
	t.Helper()
	if strings.ContainsFunc(status, unicode.IsControl) {
		t.Fatalf("published status=%q retains a control character", status)
	}
	if !strings.Contains(status, managementInjectionDisplayed) {
		t.Fatalf("published status=%q, want it to name the identifier as %q", status, managementInjectionDisplayed)
	}
}

// assertManagementCanonicalReceipt proves the escaping is presentation only:
// the durable receipt that prevents redelivery, the membership index built from
// it and the bytes on the state key all name the identifier by its original
// bytes, and never by the escaped presentation.
func assertManagementCanonicalReceipt(t *testing.T, rt *microsoftTestRuntime, key string) {
	t.Helper()
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	for index, content := range state.Contents {
		if content.ContentID != managementInjectionContentID {
			t.Fatalf("descriptor %d content id=%q, want the canonical bytes", index, content.ContentID)
		}
	}
	if len(state.Receipts) != 1 || state.Receipts[0].ContentID != managementInjectionContentID {
		t.Fatalf("receipts=%+v, want exactly one naming the canonical identifier", state.Receipts)
	}
	if !state.hasReceipt(managementInjectionContentID) {
		t.Fatal("receipt membership does not answer for the canonical identifier")
	}
	if state.hasReceipt(managementInjectionDisplayed) {
		t.Fatal("receipt membership answers for the escaped presentation, so the canonical identifier was mutated")
	}
	encoded, err := json.Marshal(managementInjectionContentID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := rt.Get(managementReceiptsKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stored, encoded) {
		t.Fatalf("persisted receipts %s do not carry the canonical identifier", stored)
	}
}

func TestDisplayManagementContentIDEscapesEveryControlCharacter(t *testing.T) {
	if got := displayManagementContentID(managementInjectionContentID); got != managementInjectionDisplayed {
		t.Fatalf("displayed=%q, want %q", got, managementInjectionDisplayed)
	}
	// Unicode Cc is U+0000-U+001F and U+007F-U+009F. Every one of them has to
	// become visible text, because the sink writes all of them through.
	for r := rune(0); r <= 0x9f; r++ {
		if !unicode.IsControl(r) {
			continue
		}
		got := displayManagementContentID(string(r))
		if strings.ContainsFunc(got, unicode.IsControl) {
			t.Fatalf("U+%04X displayed as %q, which still holds a control character", r, got)
		}
		if len(got) < 2 || !strings.HasPrefix(got, `\`) {
			t.Fatalf("U+%04X displayed as %q, want a visible escape", r, got)
		}
	}
	// Nothing an operator can act on is rewritten.
	for _, clean := range []string{"content-1", "20230101123456789012345678901234", "a.b-c_d"} {
		if got := displayManagementContentID(clean); got != clean {
			t.Fatalf("clean identifier %q displayed as %q", clean, got)
		}
	}
	// The presentation is bounded on its own. A descriptor set read back from
	// the state document is bounded only in aggregate, so one identifier inside
	// it can be far longer than the listing path accepts.
	displayed := displayManagementContentID(strings.Repeat("\n", managementMaximumDisplayedContentID*4))
	if want := managementMaximumDisplayedContentID*2 + len("..."); len(displayed) != want {
		t.Fatalf("displayed length=%d, want the bounded %d", len(displayed), want)
	}
	if !strings.HasSuffix(displayed, "...") {
		t.Fatalf("displayed=%q, want the bound marked", displayed)
	}
}

// managementInjectionSinkRuntime forwards every operator-visible line to the
// default RFC5424 file sink, which is the sink the forged physical lines
// appeared in.
type managementInjectionSinkRuntime struct {
	*microsoftTestRuntime
	t         *testing.T
	sink      *log.Logger
	forwarded atomic.Int64
}

func (r *managementInjectionSinkRuntime) Warn(message string, params ...rfc5424.SDParam) {
	r.microsoftTestRuntime.Warn(message, params...)
	r.forward(r.sink.Warn(message, params...))
}

func (r *managementInjectionSinkRuntime) Error(message string, params ...rfc5424.SDParam) {
	r.microsoftTestRuntime.Error(message, params...)
	r.forward(r.sink.Error(message, params...))
}

func (r *managementInjectionSinkRuntime) forward(err error) {
	r.forwarded.Add(1)
	if err != nil {
		r.t.Errorf("sink write failed: %v", err)
	}
}

// TestManagementHostileContentIDIsOneRecordInTheRFC5424FileSink is the
// end-to-end proof. A JSON contentId carrying CR, LF and other control bytes
// reaches the real file sink through the real delivery path, and what lands on
// disk has to be exactly one physical record per reported line.
func TestManagementHostileContentIDIsOneRecordInTheRFC5424FileSink(t *testing.T) {
	// 123 is a well-formed JSON element and not an object, so delivery skips it
	// after a bounded decode failure and reports the blob it came from.
	source := &managementInjectionSource{body: "[123]"}
	server := source.start(t)

	path := t.TempDir() + "/runner.log"
	sink, err := log.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sink.SetAppname("microsoft")
	sink.SetHostname("runner")
	rt := &managementInjectionSinkRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), t: t, sink: sink}

	plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
	key := managementStateKey("microsoft", managementInjectionSelector)
	seedManagementInjectionWindow(t, plugin, rt.microsoftTestRuntime, key)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("poll error=%v, want the hostile descriptor collected and reported", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	warnings := rt.logsContaining("WARN", "skipped after a bounded decode failure")
	if len(warnings) != 1 {
		t.Fatalf("decode-failure warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("sink bytes %q do not end a record", raw)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if got, want := len(lines), int(rt.forwarded.Load()); got != want {
		t.Fatalf("the sink holds %d physical lines for %d reported lines:\n%s", got, want, raw)
	}
	carrying := ""
	for _, line := range lines {
		if !strings.HasPrefix(line, "<") {
			t.Fatalf("physical line %q is not a complete record; a reported value ended the line before it", line)
		}
		if strings.ContainsAny(line, "\r") {
			t.Fatalf("physical line %q carries a raw carriage return", line)
		}
		if !strings.Contains(line, "content_id=") {
			if strings.Contains(line, "forged record") {
				t.Fatalf("physical line %q is the forged record the identifier carried", line)
			}
			continue
		}
		if carrying != "" {
			t.Fatalf("the identifier was written across more than one physical record:\n%s", raw)
		}
		carrying = line
	}
	if carrying == "" {
		t.Fatalf("no record named the content identifier:\n%s", raw)
	}
	if got := strings.Count(string(raw), "forged record"); got != 1 {
		t.Fatalf("the forged record text appears %d times, want only inside the one escaped parameter:\n%s", got, raw)
	}
	// The sink escapes a backslash in a parameter value, so each escape this
	// presentation produced reaches disk with its backslash doubled; what
	// matters is that the escape is there and visible rather than acted on.
	for _, escape := range []string{`\r`, `\n`, `\t`, `\x00`, `\x1b`, `\x85`, `\\slash`} {
		if !strings.Contains(carrying, escape) {
			t.Fatalf("record %q is missing the visible escape %s", carrying, escape)
		}
	}
	assertManagementDisplayedContentID(t, "content_id", warnings[0].Params["content_id"])
	assertManagementCanonicalReceipt(t, rt.microsoftTestRuntime, key)
}

// TestManagementHostileContentIDIsEscapedAtEveryOperatorDisplay covers every
// operator-visible insertion of a contentId there is. Each case drives one
// insertion with a hostile identifier, so reverting the escaping at any single
// site fails exactly one of them.
func TestManagementHostileContentIDIsEscapedAtEveryOperatorDisplay(t *testing.T) {
	key := managementStateKey("microsoft", managementInjectionSelector)
	bodyDigest := managementInjectionDigest(t, managementInjectionEvent)

	// The changed-content replay warning.
	t.Run("changed-content-replay", func(t *testing.T) {
		source := &managementInjectionSource{}
		server := source.start(t)
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
		seedManagementInjectionDelivery(t, plugin, rt, key, server.URL+managementInjectionStalePath,
			managementInjectionDigest(t, `{"Id":"other"}`), 0, 0)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("poll error=%v, want the changed blob replayed", err)
		}
		warnings := rt.logsContaining("WARN", "content changed while partially delivered")
		if len(warnings) != 1 {
			t.Fatalf("replay warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
		}
		assertManagementDisplayedContentID(t, "content_id", warnings[0].Params["content_id"])
		assertManagementCanonicalReceipt(t, rt, key)
	})

	// The bounded-replay warning the same branch publishes once the replay
	// budget is spent.
	t.Run("repeatedly-changed-content", func(t *testing.T) {
		source := &managementInjectionSource{}
		server := source.start(t)
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
		seedManagementInjectionDelivery(t, plugin, rt, key, server.URL+managementInjectionStalePath,
			managementInjectionDigest(t, `{"Id":"other"}`), 0, managementMaximumContentReplays)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("poll error=%v, want the durable ordinal kept", err)
		}
		warnings := rt.logsContaining("WARN", "continuing from the durable ordinal")
		if len(warnings) != 1 {
			t.Fatalf("bounded-replay warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
		}
		assertManagementDisplayedContentID(t, "content_id", warnings[0].Params["content_id"])
		assertManagementCanonicalReceipt(t, rt, key)
	})

	// The corrupt-ordinal warning, and the status message the poll loop leaves
	// alone, which is the one status channel a contentId reaches.
	t.Run("corrupt-delivery-ordinal", func(t *testing.T) {
		source := &managementInjectionSource{}
		server := source.start(t)
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
		// Five is inside the absolute bound load validation can check and past
		// the single event this blob holds.
		seedManagementInjectionDelivery(t, plugin, rt, key, server.URL+managementInjectionStalePath, bodyDigest, 5, 0)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("poll error=%v, want the blob replayed from its own beginning", err)
		}
		warnings := rt.logsContaining("WARN", "exceeded the content it was recorded against")
		if len(warnings) != 1 {
			t.Fatalf("ordinal warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
		}
		assertManagementDisplayedContentID(t, "content_id", warnings[0].Params["content_id"])
		published := rt.warnStateContaining("reset a corrupt delivery ordinal")
		if len(published) != 1 {
			t.Fatalf("published ordinal statuses=%d, want exactly 1 (%q)", len(published), published)
		}
		assertManagementDisplayedInStatus(t, published[0])
		assertManagementCanonicalReceipt(t, rt, key)
	})

	// The skipped-event warning.
	t.Run("skipped-event", func(t *testing.T) {
		source := &managementInjectionSource{body: "[123]"}
		server := source.start(t)
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
		seedManagementInjectionDelivery(t, plugin, rt, key, server.URL+managementInjectionStalePath,
			managementInjectionDigest(t, "123"), 0, 0)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("poll error=%v, want the malformed event skipped", err)
		}
		warnings := rt.logsContaining("WARN", "skipped after a bounded decode failure")
		if len(warnings) != 1 {
			t.Fatalf("skip warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
		}
		assertManagementDisplayedContentID(t, "content_id", warnings[0].Params["content_id"])
		assertManagementCanonicalReceipt(t, rt, key)
	})

	// The permanent-loss report. A stored contentUri the URL policy refuses is
	// bounded loss of that one blob, and it names the blob.
	t.Run("permanent-content-loss", func(t *testing.T) {
		source := &managementInjectionSource{}
		server := source.start(t)
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
		seedManagementInjectionDelivery(t, plugin, rt, key, managementInBandURL(t, server.URL), "", 0, 0)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("poll error=%v, want bounded reported loss", err)
		}
		reports := rt.logsContaining("ERROR", "refused by the Microsoft Management Activity URL policy")
		if len(reports) != 1 {
			t.Fatalf("loss reports=%d, want exactly 1 (%+v)", len(reports), reports)
		}
		assertManagementDisplayedContentID(t, "content_id", reports[0].Params["content_id"])
		assertManagementCanonicalReceipt(t, rt, key)
	})

	// The outside-the-window warning, which names a descriptor the listing
	// refused before any delivery state existed for it.
	t.Run("descriptor-outside-the-window", func(t *testing.T) {
		source := &managementInjectionSource{created: managementTestNow}
		server := source.start(t)
		rt := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
		seedManagementInjectionWindow(t, plugin, rt, key)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("poll error=%v, want the boundary descriptor reported rather than fatal", err)
		}
		warnings := rt.logsContaining("WARN", "outside the requested window")
		if len(warnings) != 1 {
			t.Fatalf("boundary warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
		}
		assertManagementDisplayedContentID(t, "content_id", warnings[0].Params["content_id"])
		if got := len(rt.Entries()); got != 0 {
			t.Fatalf("entries=%d, want the descriptor left to the window that covers it", got)
		}
	})
}

// TestManagementHostileContentIDIsPersistedVerbatim proves the escaping never
// reaches the descriptor set or the delivery identity. The pass is stopped by a
// write failure part way through the blob, so the durable documents can be read
// while the identifier is still in flight.
func TestManagementHostileContentIDIsPersistedVerbatim(t *testing.T) {
	source := &managementInjectionSource{}
	server := source.start(t)
	rt := newMicrosoftTestRuntime(t)
	rt.failAt.Store(1)
	plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
	key := managementStateKey("microsoft", managementInjectionSelector)
	seedManagementInjectionWindow(t, plugin, rt, key)
	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "write failure") {
		t.Fatalf("poll error=%v, want the synthetic write failure", err)
	}
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Contents) != 1 || state.Contents[0].ContentID != managementInjectionContentID {
		t.Fatalf("persisted descriptors=%+v, want the canonical identifier verbatim", state.Contents)
	}
	if state.ActiveContentID != managementInjectionContentID {
		t.Fatalf("active content id=%q, want the canonical identifier verbatim", state.ActiveContentID)
	}
	if !state.hasContent(managementInjectionContentID) {
		t.Fatal("descriptor membership does not answer for the canonical identifier")
	}
	if state.hasContent(managementInjectionDisplayed) {
		t.Fatal("descriptor membership answers for the escaped presentation")
	}
	encoded, err := json.Marshal(managementInjectionContentID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{key, managementContentsKey(key), managementProgressKey(key)} {
		stored, err := rt.Get(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Contains(stored, encoded) {
			t.Fatalf("%s holds %s, which does not carry the canonical identifier", name, stored)
		}
	}
}

// TestManagementHostileContentIDRefreshesByItsCanonicalBytes proves the
// escaping never reaches the request path. The source answers the stale content
// URI the way a blob past its URI's lifetime does and re-lists the same
// identifier at a fresh URI. refreshManagementContent matches the re-listed
// descriptor by contentId, so an escaped identifier would not be found at all
// and the blob would be reported as permanent loss instead of collected.
func TestManagementHostileContentIDRefreshesByItsCanonicalBytes(t *testing.T) {
	source := &managementInjectionSource{expire: true, relist: true}
	server := source.start(t)
	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
	key := managementStateKey("microsoft", managementInjectionSelector)
	seedManagementInjectionWindow(t, plugin, rt, key)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("poll error=%v, want the refreshed descriptor collected", err)
	}
	if got := source.stale.Load(); got != 1 {
		t.Fatalf("stale content requests=%d, want 1", got)
	}
	if got := source.fresh.Load(); got != 1 {
		t.Fatalf("refreshed content requests=%d, want the re-listed URI fetched once", got)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want the refreshed blob delivered", got)
	}
	if got := rt.logsContaining("ERROR", "no longer available from the source"); len(got) != 0 {
		t.Fatalf("permanent-loss reports=%d, want none (%+v)", len(got), got)
	}
	assertManagementCanonicalReceipt(t, rt, key)
}

// A Management Activity subscription descriptor carries a status string the
// plugin bounds at 64 bytes, lowercases, and trims at its edges. Nothing
// touched its interior, so a status of "dis\r\n<13>1 ... forged" was one
// physical record on the wire and two in the operator's log: the suppression
// warning published it to both a structured log parameter and the status
// channel, and the embedded CR LF ended the record and let the remainder be
// read as a further complete entry this plugin never wrote (CWE-117).
//
// The status fits inside the 64-byte descriptor bound the listing path already
// enforces, so this is a status the source can really publish, and it is all
// lowercase with non-space edges so neither ToLower nor TrimSpace changes it:
// the escaping is the only thing between it and the sink.
const managementStatusInjectionStatus = "dis\r\n<13>1 t runner ms - - - forged\t\x00\x1b\u0085\\"

// managementStatusInjectionDisplayed is the only presentation of that status an
// operator may be shown.
const managementStatusInjectionDisplayed = `dis\r\n<13>1 t runner ms - - - forged\t\x00\x1b\x85\\`

// managementStatusInjectionSource answers subscriptions/list with the hostile
// status for the requested content type and accepts a start request, which is
// what seeds the bounded suppression interval the warning is published from.
func managementStatusInjectionSource(t *testing.T, contentType string, starts *atomic.Int64) *httptest.Server {
	t.Helper()
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			_, _ = fmt.Fprintf(w, `[{"contentType":%q,"status":%s}]`, contentType, mustJSONString(t, managementStatusInjectionStatus))
		case strings.HasSuffix(r.URL.Path, "/subscriptions/start"):
			starts.Add(1)
			_, _ = fmt.Fprintf(w, `{"contentType":%q,"status":"enabled"}`, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	})
	t.Cleanup(server.Close)
	return server
}

// TestManagementHostileSubscriptionStatusIsOneRecordInTheRFC5424FileSink is the
// end-to-end proof for the status insertions. A hostile status reaches the real
// suppression warning through the real subscription path and into the real file
// sink, and what lands on disk has to be exactly one physical record per
// reported line. SetWarn is published on the same pass, so a second escaped
// insertion is covered by the same measurement.
func TestManagementHostileSubscriptionStatusIsOneRecordInTheRFC5424FileSink(t *testing.T) {
	const contentType = "Audit.Exchange"
	var starts atomic.Int64
	server := managementStatusInjectionSource(t, contentType, &starts)

	path := t.TempDir() + "/runner.log"
	sink, err := log.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sink.SetAppname("microsoft")
	sink.SetHostname("runner")
	rt := &managementInjectionSinkRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), t: t, sink: sink}

	// The first poll observes the hostile status and asks the source to start
	// the subscription; the second runs inside the bounded suppression interval
	// and is the one that has to tell the operator why it collected nothing.
	now := managementTestNow
	plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
	plugin.now = func() time.Time { return now }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatalf("subscription starts=%d, want 1", starts.Load())
	}
	now = managementTestNow.Add(managementStartSuppression / 2)
	plugin = newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
	plugin.now = func() time.Time { return now }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	warnings := rt.logsContaining("WARN", "subscription is not enabled")
	if len(warnings) != 1 {
		t.Fatalf("suppression warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["status"]; got != managementStatusInjectionDisplayed {
		t.Fatalf("status=%q, want the escaped presentation %q", got, managementStatusInjectionDisplayed)
	}
	for name, value := range warnings[0].Params {
		if strings.ContainsFunc(value, unicode.IsControl) {
			t.Fatalf("reported %s=%q retains a control character an operator log cannot carry", name, value)
		}
	}
	published := rt.warnStateContaining("is collecting nothing")
	if len(published) != 1 {
		t.Fatalf("published status warnings=%d, want exactly 1 (%+v)", len(published), published)
	}
	if strings.ContainsFunc(published[0], unicode.IsControl) {
		t.Fatalf("published status=%q retains a control character", published[0])
	}
	if !strings.Contains(published[0], managementStatusInjectionDisplayed) {
		t.Fatalf("published status=%q, want it to name the status as %q", published[0], managementStatusInjectionDisplayed)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("sink bytes %q do not end a record", raw)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if got, want := len(lines), int(rt.forwarded.Load()); got != want {
		t.Fatalf("the sink holds %d physical lines for %d reported lines:\n%s", got, want, raw)
	}
	carrying := ""
	for _, line := range lines {
		if !strings.HasPrefix(line, "<") {
			t.Fatalf("physical line %q is not a complete record; a reported value ended the line before it", line)
		}
		if strings.ContainsAny(line, "\r") {
			t.Fatalf("physical line %q carries a raw carriage return", line)
		}
		if !strings.Contains(line, "status=") {
			if strings.Contains(line, "forged") {
				t.Fatalf("physical line %q is the forged record the status carried", line)
			}
			continue
		}
		if carrying != "" {
			t.Fatalf("the status was written across more than one physical record:\n%s", raw)
		}
		carrying = line
	}
	if carrying == "" {
		t.Fatalf("no record named the subscription status:\n%s", raw)
	}
	if got := strings.Count(string(raw), "forged"); got != 1 {
		t.Fatalf("the forged record text appears %d times, want only inside the one escaped parameter:\n%s", got, raw)
	}
	for _, escape := range []string{`\r`, `\n`, `\t`, `\x00`, `\x1b`, `\x85`, `\\`} {
		if !strings.Contains(carrying, escape) {
			t.Fatalf("record %q is missing the visible escape %s", carrying, escape)
		}
	}

	// Presentation only: the canonical status is what the state document keeps
	// and what the "enabled" comparison that gates collection reads.
	key := managementStateKey("microsoft", managementInjectionSelector)
	state, err := loadManagementState(rt.microsoftTestRuntime, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.SubscriptionStatus != managementStatusInjectionStatus {
		t.Fatalf("stored subscription status=%q, want the canonical bytes", state.SubscriptionStatus)
	}
	if state.SubscriptionStatus == managementStatusInjectionDisplayed {
		t.Fatal("the escaped presentation reached the state document")
	}
	if len(rt.Entries()) != 0 {
		t.Fatalf("entries=%d, want nothing collected while the subscription is not enabled", len(rt.Entries()))
	}
}

// TestRawSubscriptionStatusWouldForgeARecordInTheSameSink is the mutation proof
// that the sink the test above measures is the sink that was vulnerable: it
// writes the canonical status through the same default file sink with no
// presentation applied -- exactly what reverting either insertion would do --
// and requires that it really does forge a further physical record.
func TestRawSubscriptionStatusWouldForgeARecordInTheSameSink(t *testing.T) {
	path := t.TempDir() + "/raw.log"
	sink, err := log.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sink.SetAppname("microsoft")
	sink.SetHostname("runner")
	if err := sink.Warn("Microsoft Management Activity subscription is not enabled",
		log.KV("status", managementStatusInjectionStatus)); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("the unescaped status produced %d physical record(s); the sink neutralizes control characters, so the escaping test proves nothing:\n%s", len(lines), raw)
	}
	forged := false
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "<") && strings.Contains(line, "forged") {
			forged = true
		}
	}
	if !forged {
		t.Fatalf("the unescaped status did not forge a complete record, so the regression above is not measuring one:\n%s", raw)
	}

	escapedPath := t.TempDir() + "/escaped.log"
	escapedSink, err := log.NewFile(escapedPath)
	if err != nil {
		t.Fatal(err)
	}
	escapedSink.SetAppname("microsoft")
	escapedSink.SetHostname("runner")
	if err := escapedSink.Warn("Microsoft Management Activity subscription is not enabled",
		log.KV("status", displayManagementSubscriptionStatus(managementStatusInjectionStatus))); err != nil {
		t.Fatal(err)
	}
	if err := escapedSink.Close(); err != nil {
		t.Fatal(err)
	}
	escapedRaw, err := os.ReadFile(escapedPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(strings.TrimSuffix(string(escapedRaw), "\n"), "\n")); got != 1 {
		t.Fatalf("the escaped presentation produced %d physical records:\n%s", got, escapedRaw)
	}
}

// TestManagementSubscriptionStatusPresentationIsBoundedAndKeepsLegitimateWords
// pins the other half of the contract. The vocabulary is deliberately not
// validated -- the point of the report is to state a status the plugin did not
// expect, including one a later service revision introduces -- so a legitimate
// status has to survive the escape byte for byte while the presentation stays
// bounded on its own.
func TestManagementSubscriptionStatusPresentationIsBoundedAndKeepsLegitimateWords(t *testing.T) {
	for _, legitimate := range []string{"enabled", "disabled", "disabledbyuser", "start-requested", "absent"} {
		if got := displayManagementSubscriptionStatus(legitimate); got != legitimate {
			t.Fatalf("legitimate status %q displayed as %q", legitimate, got)
		}
	}
	// A status restored from a state document is not re-bounded by the 64-byte
	// descriptor check, so the presentation is bounded by itself.
	displayed := displayManagementSubscriptionStatus(strings.Repeat("\r", maximumDisplayedVendorValue*4))
	if want := maximumDisplayedVendorValue*2 + len(displayTruncationMarker); len(displayed) != want {
		t.Fatalf("displayed length=%d, want the bounded %d", len(displayed), want)
	}
	if strings.ContainsFunc(displayed, unicode.IsControl) {
		t.Fatal("the bounded presentation retains a control character")
	}
}

// displayVendorValue escaped Unicode Cc and nothing else. Cc is not the whole of
// what an operator-visible record cannot carry: Cf format characters reorder or
// erase what is displayed without ending a line, and Zl/Zp end a record for
// every reader that splits on Unicode line boundaries rather than on LF. All of
// them reached the RFC5424 file sink and the status channel unchanged.
//
// The constants below name one representative of each shape the repair has to
// neutralize. Every one is a character a Management contentId, a Defender parent
// identifier or a subscription status can really carry: the Management API
// constrains the length of a contentId and nothing else about its bytes.
//
// Each is written as an explicit codepoint conversion rather than as the
// character itself. Every one is invisible or reorders what follows it, so a
// source file holding them literally would be a file nobody could review -- and
// Go's own scanner refuses to read one of them, U+FEFF, anywhere but the first
// byte of a file.
const (
	// displayTestRightToLeftOverride (Cf) reorders everything after it, so
	// "gpj.exe" displays as "exe.jpg" and an identifier displays as one it is
	// not.
	displayTestRightToLeftOverride = string(rune(0x202E))
	// displayTestLeftToRightOverride (Cf) is the same in the other direction.
	displayTestLeftToRightOverride = string(rune(0x202D))
	// The isolates (Cf). An unterminated one reorders the record's own later
	// fields, not only the rest of the value.
	displayTestLeftToRightIsolate    = string(rune(0x2066))
	displayTestRightToLeftIsolate    = string(rune(0x2067))
	displayTestFirstStrongIsolate    = string(rune(0x2068))
	displayTestPopDirectionalIsolate = string(rune(0x2069))
	// Zero-width Cf characters. Each has no glyph, so a value holding them
	// displays as a shorter value and two different identifiers display as one.
	displayTestZeroWidthSpace     = string(rune(0x200B))
	displayTestZeroWidthNonJoiner = string(rune(0x200C))
	displayTestZeroWidthJoiner    = string(rune(0x200D))
	displayTestByteOrderMark      = string(rune(0xFEFF))
	displayTestSoftHyphen         = string(rune(0x00AD))
	// displayTestLineSeparator is Zl and displayTestParagraphSeparator is Zp.
	displayTestLineSeparator      = string(rune(0x2028))
	displayTestParagraphSeparator = string(rune(0x2029))
	// displayTestNonBreakingSpace is Zs, which is neither Zl nor Zp: it ends no
	// record and reorders nothing, so it is left alone.
	displayTestNonBreakingSpace = string(rune(0x00A0))
)

// displayUnicodeHostileValue is one vendor value carrying every shape at once,
// with a forged record behind the Unicode line separator and a literal
// backslash sequence that must not be able to imitate an escape this
// presentation produces.
const displayUnicodeHostileValue = "content-1" +
	displayTestLineSeparator + "<12>1 2026-10-08T12:00:00Z runner microsoft - - - forged record" +
	displayTestParagraphSeparator + "forged paragraph" +
	displayTestRightToLeftOverride + "gpj.exe" +
	displayTestLeftToRightOverride + "ltr" +
	displayTestLeftToRightIsolate + displayTestRightToLeftIsolate + displayTestFirstStrongIsolate + "iso" + displayTestPopDirectionalIsolate +
	displayTestZeroWidthSpace + displayTestZeroWidthNonJoiner + displayTestZeroWidthJoiner +
	displayTestByteOrderMark + displayTestSoftHyphen +
	`\u{2028}literal`

// displayUnicodeHostileDisplayed is the only presentation of that value an
// operator may be shown. Every escape is bounded and unambiguous, and the
// literal backslash is doubled, so the text "\u{2028}" the value itself carries
// can never be read as the escape of U+2028.
const displayUnicodeHostileDisplayed = `content-1` +
	`\u{2028}<12>1 2026-10-08T12:00:00Z runner microsoft - - - forged record` +
	`\u{2029}forged paragraph` +
	`\u{202e}gpj.exe` +
	`\u{202d}ltr` +
	`\u{2066}\u{2067}\u{2068}iso\u{2069}` +
	`\u{200b}\u{200c}\u{200d}` +
	`\u{feff}\u{00ad}` +
	`\\u{2028}literal`

// splitDisplayedRecords splits text the way the strictest reader of a log file
// does: on LF, and on the two Unicode separators a Unicode-aware splitter also
// ends a record at. The CWE-117 claim for Zl and Zp is exactly this -- the
// RFC5424 sink does not end a physical line at U+2028, but a reader that splits
// on Unicode line boundaries reads one record as two.
func splitDisplayedRecords(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
	})
}

// TestDisplayVendorValueEscapesEveryUnicodeSeparatorAndFormatCharacter is the
// exhaustive statement. Each category is asserted in its own subtest and each
// one is answered by its own branch in displayVendorValue, so deleting any
// single branch fails exactly one of these rather than all three.
func TestDisplayVendorValueEscapesEveryUnicodeSeparatorAndFormatCharacter(t *testing.T) {
	for _, category := range []struct {
		name  string
		table *unicode.RangeTable
	}{
		{"cf-format", unicode.Cf},
		{"zl-line-separator", unicode.Zl},
		{"zp-paragraph-separator", unicode.Zp},
	} {
		t.Run(category.name, func(t *testing.T) {
			members := 0
			for r := rune(0); r <= 0x10FFFF; r++ {
				if !unicode.Is(category.table, r) {
					continue
				}
				members++
				displayed := displayVendorValue(string(r), maximumDisplayedVendorValue)
				if strings.ContainsRune(displayed, r) {
					t.Fatalf("U+%04X displayed as %q, which still carries the character itself", r, displayed)
				}
				if !strings.HasPrefix(displayed, `\u{`) || !strings.HasSuffix(displayed, `}`) {
					t.Fatalf("U+%04X displayed as %q, want a bounded \\u{...} escape", r, displayed)
				}
				// Bounded on its own: the widest codepoint Unicode defines is
				// U+10FFFF, which is ten characters.
				if len(displayed) > len(`\u{10ffff}`) {
					t.Fatalf("U+%04X displayed as %q, which is %d characters", r, displayed, len(displayed))
				}
				// Injective: the escape names the codepoint exactly, padded to
				// the four digits a BMP character needs so two characters can
				// never share one presentation.
				if digits := len(displayed) - len(`\u{}`); digits < displayCodepointMinimumDigits {
					t.Fatalf("U+%04X displayed as %q, want at least %d hex digits", r, displayed, displayCodepointMinimumDigits)
				}
			}
			if members == 0 {
				t.Fatal("the category came back empty, so the subtest proves nothing")
			}
		})
	}
}

// TestDisplayVendorValueNeutralizesTheHostileValueExactly pins the whole
// presentation, so weakening any single escape is a diff rather than a quietly
// weaker property.
func TestDisplayVendorValueNeutralizesTheHostileValueExactly(t *testing.T) {
	displayed := displayVendorValue(displayUnicodeHostileValue, maximumDisplayedVendorValue)
	if displayed != displayUnicodeHostileDisplayed {
		t.Fatalf("displayed=%q,\n     want %q", displayed, displayUnicodeHostileDisplayed)
	}
	if got := len(splitDisplayedRecords(displayed)); got != 1 {
		t.Fatalf("the presentation reads as %d records, want exactly 1: %q", got, displayed)
	}
	// The raw value is what makes that non-vacuous: it reads as more than one
	// record, which is the condition the escape exists for.
	if got := len(splitDisplayedRecords(displayUnicodeHostileValue)); got < 3 {
		t.Fatalf("the raw value reads as %d records, so the Zl/Zp claim is vacuous", got)
	}
	// The escape is injective in both directions: the value's own literal
	// backslash sequence and a real U+2028 have different presentations.
	if displayVendorValue(`\u{2028}`, maximumDisplayedVendorValue) == displayVendorValue(displayTestLineSeparator, maximumDisplayedVendorValue) {
		t.Fatal("a literal backslash sequence and U+2028 share one presentation")
	}
	if got := displayVendorValue(`\u{2028}`, maximumDisplayedVendorValue); got != `\\u{2028}` {
		t.Fatalf("literal lookalike displayed as %q, want the backslash doubled", got)
	}
}

// TestDisplayVendorValueLeavesCanonicalValuesAlone is the other side of the
// repair. A value an operator can act on, including legitimate non-ASCII text,
// has to reach the log exactly as the source published it.
func TestDisplayVendorValueLeavesCanonicalValuesAlone(t *testing.T) {
	for _, clean := range []string{
		"content-1",
		"20230101123456789012345678901234",
		"a.b-c_d",
		"enabled",
		"disabled",
		"d8a4f2e1-1111-2222-3333-444455556666",
		// Letters, marks, digits, punctuation, symbols and even the non-breaking
		// space are untouched: the escape is keyed on Cc, Cf, Zl and Zp alone.
		"Zürich-Überwachung",
		"監査ログ",
		"a b" + displayTestNonBreakingSpace + "c",
		"emoji-\U0001F512",
	} {
		if got := displayVendorValue(clean, maximumDisplayedVendorValue); got != clean {
			t.Fatalf("canonical value %q displayed as %q", clean, got)
		}
	}
}

// TestDisplayVendorValueBoundsAndMarksTruncatedUnicodeEscapes pins that the
// bound is applied to the input and that reaching it is always stated, so a
// value an operator reads is never silently shortened and can never grow the
// parameter past the per-value ceiling.
func TestDisplayVendorValueBoundsAndMarksTruncatedUnicodeEscapes(t *testing.T) {
	for _, tc := range []struct{ name, character string }{
		{"line-separator", displayTestLineSeparator},
		{"paragraph-separator", displayTestParagraphSeparator},
		{"right-to-left-override", displayTestRightToLeftOverride},
		{"soft-hyphen", displayTestSoftHyphen},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// maximumDisplayedVendorValue copies of a multi-byte character is at
			// least the bound of input whatever its UTF-8 width is.
			displayed := displayVendorValue(strings.Repeat(tc.character, maximumDisplayedVendorValue), maximumDisplayedVendorValue)
			if !strings.HasSuffix(displayed, displayTruncationMarker) {
				t.Fatalf("displayed=%q, want the bound marked", displayed)
			}
			// Four characters per byte of input plus the marker is the bound the
			// list presentation is sized against.
			if limit := maximumDisplayedVendorValue*4 + len(displayTruncationMarker); len(displayed) > limit {
				t.Fatalf("displayed length=%d, want at most %d", len(displayed), limit)
			}
			if got := len(splitDisplayedRecords(displayed)); got != 1 {
				t.Fatalf("a truncated presentation reads as %d records", got)
			}
			// Truncation stops at a rune boundary, so the result holds only whole
			// escapes and the marker.
			if trimmed := strings.TrimSuffix(displayed, displayTruncationMarker); strings.Count(trimmed, `\u{`) != strings.Count(trimmed, `}`) {
				t.Fatalf("displayed=%q holds a partial escape", displayed)
			}
		})
	}
}

// TestDisplayVendorListKeepsItsBoundWithUnicodeEscapes pins that the list
// presentation's own ceiling still holds once one codepoint can escape to ten
// characters.
func TestDisplayVendorListKeepsItsBoundWithUnicodeEscapes(t *testing.T) {
	values := make([]string, 0, maximumDiagnostics)
	for range maximumDiagnostics {
		values = append(values, strings.Repeat(displayTestLineSeparator, maximumDisplayedVendorValue))
	}
	joined := displayVendorList(values)
	if limit := maximumDisplayedVendorList + len(displayTruncationMarker); len(joined) > limit {
		t.Fatalf("list presentation length=%d, want at most %d", len(joined), limit)
	}
	if got := len(splitDisplayedRecords(joined)); got != 1 {
		t.Fatalf("the list presentation reads as %d records", got)
	}
	// The first element always fits, so the presentation is never reduced to the
	// marker alone.
	if !strings.HasPrefix(joined, `\u{2028}`) {
		t.Fatalf("list presentation=%q, want the first element shown", joined)
	}
}

// TestDisplayedUnicodeControlsAreOneRecordInTheRealRFC5424Sink is the
// end-to-end proof against the sink the forged records appear in. The raw value
// goes through the same sink in the same run, which is what makes the claim
// load-bearing: the sink neutralizes none of these characters, so the escape is
// the only thing that does.
func TestDisplayedUnicodeControlsAreOneRecordInTheRealRFC5424Sink(t *testing.T) {
	escaped := managementUnicodeSinkRecord(t, displayVendorValue(displayUnicodeHostileValue, maximumDisplayedVendorValue))
	records := splitDisplayedRecords(strings.TrimSuffix(escaped, "\n"))
	if len(records) != 1 {
		t.Fatalf("the escaped record reads as %d records on disk:\n%q", len(records), escaped)
	}
	if !strings.HasPrefix(records[0], "<") {
		t.Fatalf("record %q is not a complete RFC5424 record", records[0])
	}
	// Nothing that can end or reorder a record survived.
	for _, r := range records[0] {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			t.Fatalf("record %q still carries U+%04X", records[0], r)
		}
	}
	// The escapes an operator has to be able to read are all there. The sink
	// doubles a backslash in a parameter value, so each escape reaches disk with
	// its own backslash doubled; what matters is that the escape is visible text
	// rather than something acted on.
	for _, escape := range []string{`u{2028}`, `u{2029}`, `u{202e}`, `u{202d}`, `u{2066}`, `u{2069}`, `u{200b}`, `u{feff}`, `u{00ad}`} {
		if !strings.Contains(records[0], escape) {
			t.Fatalf("record %q is missing the visible escape %s", records[0], escape)
		}
	}

	// The same sink writes the raw value through unchanged.
	raw := managementUnicodeSinkRecord(t, displayUnicodeHostileValue)
	rawRecords := splitDisplayedRecords(strings.TrimSuffix(raw, "\n"))
	if len(rawRecords) < 3 {
		t.Fatalf("the raw value produced %d records, so the sink claim is vacuous:\n%q", len(rawRecords), raw)
	}
	if !strings.Contains(raw, displayTestRightToLeftOverride) {
		t.Fatalf("the sink neutralized U+202E on its own, so the escape claim is vacuous:\n%q", raw)
	}
	forged := false
	for _, record := range rawRecords[1:] {
		if strings.Contains(record, "forged record") || strings.Contains(record, "forged paragraph") {
			forged = true
		}
	}
	if !forged {
		t.Fatalf("no forged record appeared from the raw value:\n%q", raw)
	}
}

// managementUnicodeSinkRecord writes one parameter through the default RFC5424
// file sink and returns the bytes that landed on disk.
func managementUnicodeSinkRecord(t *testing.T, value string) string {
	t.Helper()
	path := t.TempDir() + "/runner.log"
	sink, err := log.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sink.SetAppname("microsoft")
	sink.SetHostname("runner")
	if err := sink.Warn("Microsoft Management Activity reported a vendor value", log.KV("content_id", value)); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestManagementUnicodeHostileContentIDReachesTheRealStatusSinkEscaped drives
// the Unicode-hostile identifier through the real delivery path, so the escape
// is proved at the two operator-visible sinks a contentId actually reaches: the
// log parameter and the status channel the poll loop leaves alone. The canonical
// bytes have to survive in the descriptor set, the receipt and the persisted
// documents at the same time.
func TestManagementUnicodeHostileContentIDReachesTheRealStatusSinkEscaped(t *testing.T) {
	source := &managementInjectionSource{contentID: displayUnicodeHostileValue}
	server := source.start(t)
	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, managementInjectionSelector, (&managementSyncController{}).Sync)
	key := managementStateKey("microsoft", managementInjectionSelector)
	// Five is inside the absolute bound load validation can check and past the
	// one event this blob holds, so the corrupt-ordinal branch publishes both a
	// log parameter and a status message naming the identifier.
	seedManagementInjectionDeliveryFor(t, plugin, rt, key, displayUnicodeHostileValue,
		server.URL+managementInjectionStalePath, managementInjectionDigest(t, managementInjectionEvent), 5, 0)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("poll error=%v, want the blob replayed from its own beginning", err)
	}

	warnings := rt.logsContaining("WARN", "exceeded the content it was recorded against")
	if len(warnings) != 1 {
		t.Fatalf("ordinal warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["content_id"]; got != displayUnicodeHostileDisplayed {
		t.Fatalf("content_id=%q, want the escaped presentation %q", got, displayUnicodeHostileDisplayed)
	}
	published := rt.warnStateContaining("reset a corrupt delivery ordinal")
	if len(published) != 1 {
		t.Fatalf("published statuses=%d, want exactly 1 (%q)", len(published), published)
	}
	if !strings.Contains(published[0], displayUnicodeHostileDisplayed) {
		t.Fatalf("published status=%q, want it to name the identifier as %q", published[0], displayUnicodeHostileDisplayed)
	}
	for _, where := range []string{warnings[0].Params["content_id"], published[0]} {
		if got := len(splitDisplayedRecords(where)); got != 1 {
			t.Fatalf("reported value %q reads as %d records", where, got)
		}
	}

	// Presentation only. The descriptor set, the receipt, the membership index
	// and the bytes on the receipt key all carry the canonical identifier.
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Receipts) != 1 || state.Receipts[0].ContentID != displayUnicodeHostileValue {
		t.Fatalf("receipts=%+v, want exactly one naming the canonical identifier", state.Receipts)
	}
	if !state.hasReceipt(displayUnicodeHostileValue) || state.hasReceipt(displayUnicodeHostileDisplayed) {
		t.Fatal("receipt membership does not answer for the canonical identifier alone")
	}
	encoded, err := json.Marshal(displayUnicodeHostileValue)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := rt.Get(managementReceiptsKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stored, encoded) {
		t.Fatalf("persisted receipts %s do not carry the canonical identifier", stored)
	}
}

// managementNarrowingRefusalSource lists a window the way the source does and
// prepends two descriptors the policy permanently refuses to the first page of
// every request whose startTime is the start of the catch-up.
//
// The accepted descriptors are paginated, which is what makes the prefix a
// prefix: page one's refusals are charged and persisted, and the page after it
// is the one that overflows the descriptor capacity and narrows the window. The
// refusals belong only to the catch-up's own start, so the window that collects
// the remainder after a narrowing publishes none of them and the count under
// test is exactly what that one listing-start saw.
func managementNarrowingRefusalSource(t *testing.T, contentType string, accepted []managementCapacityContent,
	refusedPages, fetched map[string]*atomic.Int64) *httptest.Server {
	t.Helper()
	for _, content := range accepted {
		fetched[content.id] = &atomic.Int64{}
	}
	catchUpStart := managementTestNow.Add(-managementMaximumWindow)
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			values := r.URL.Query()
			start, err := time.Parse(time.RFC3339, values.Get("startTime"))
			if err != nil {
				t.Errorf("startTime=%q: %v", values.Get("startTime"), err)
				return
			}
			end, err := time.Parse(time.RFC3339, values.Get("endTime"))
			if err != nil {
				t.Errorf("endTime=%q: %v", values.Get("endTime"), err)
				return
			}
			inWindow := make([]managementCapacityContent, 0, len(accepted))
			for _, content := range accepted {
				if content.created.Before(start) || !content.created.Before(end) {
					continue
				}
				inWindow = append(inWindow, content)
			}
			listed := make([]string, 0, len(inWindow)+2)
			if values.Get("page") == "" {
				if start.Equal(catchUpStart) {
					refusedPages["start"].Add(1)
					listed = append(listed,
						managementRefusedDescriptor(server.URL, contentType, "refused-1"),
						managementRefusedDescriptor(server.URL, contentType, "refused-2"))
				}
				if len(inWindow) > 1 {
					values.Set("page", "2")
					w.Header().Set("NextPageUri", server.URL+r.URL.Path+"?"+values.Encode())
				}
				if len(inWindow) > 0 {
					inWindow = inWindow[:1]
				}
			} else {
				inWindow = inWindow[1:]
			}
			for _, content := range inWindow {
				listed = append(listed, managementDescriptorAt(server.URL, contentType, content))
			}
			_, _ = fmt.Fprintf(w, "[%s]", strings.Join(listed, ","))
		case strings.Contains(r.URL.Path, managementCapacityContentPath):
			id := strings.TrimPrefix(r.URL.Path, managementCapacityContentPath)
			counter, ok := fetched[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			counter.Add(1)
			_, _ = fmt.Fprintf(w, `[{"Id":%q,"CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}]`, id)
		default:
			http.NotFound(w, r)
		}
	})
	return server
}

// A refusal is charged by the state transaction that advances the listing past
// the page that published it, which is exactly-once while the listing moves
// forward. Narrowing does not move forward: it halves the window and restarts
// the listing from the same WindowStart, so every page of the already-charged
// prefix is requested again, published again and refused again -- and charged
// again on top of a charge that was already durable. The same permanent loss
// was therefore counted twice, and once more per further narrowing, across a
// Handle pass boundary and across a restart, because both sides of the
// accounting were on disk before the narrowing happened.
//
// TestManagementRefusedDescriptorsAreCountedOnceAcrossANarrowingRestart is that
// sequence: a catch-up whose first page refuses two descriptors and publishes a
// continuation, a second page that overflows the descriptor capacity and
// narrows, and a re-list of the identical refused prefix -- every pass a fresh
// plugin, which is the restart. The durable count has to name two refused
// descriptors, not four.
func TestManagementRefusedDescriptorsAreCountedOnceAcrossANarrowingRestart(t *testing.T) {
	const contentType = "Audit.Exchange"
	const selector = "microsoft365-audit-exchange"
	const refused = 2

	accepted := []managementCapacityContent{
		{id: "content-early", created: managementTestNow.Add(-20 * time.Hour)},
		{id: "content-middle", created: managementTestNow.Add(-10 * time.Hour)},
		{id: "content-late", created: managementTestNow.Add(-2 * time.Hour)},
	}
	refusedPages := map[string]*atomic.Int64{"start": {}}
	fetched := make(map[string]*atomic.Int64)
	server := managementNarrowingRefusalSource(t, contentType, accepted, refusedPages, fetched)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", selector)
	finished := false
	for pass := 1; pass <= 12 && !finished; pass++ {
		// A fresh plugin every pass: the accounting and the snapshot that keeps
		// it honest are both durable, so a restart must not change the answer.
		plugin := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
		plugin.managementDescriptorLimit = 2
		_, err := plugin.Handle(t.Context(), rt)
		if err != nil && !strings.Contains(err.Error(), "refused") {
			t.Fatalf("pass %d: %v", pass, err)
		}
		state, loadErr := loadManagementState(rt, key)
		if loadErr != nil {
			t.Fatalf("pass %d: %v", pass, loadErr)
		}
		finished = err == nil && !state.hasPending() && state.CompletedThrough.Equal(managementTestNow)
		if pass == 12 && !finished {
			t.Fatalf("the selector never finished its bounded catch-up: %+v", state)
		}
	}

	// Non-vacuous in both halves: the narrowing really happened, and the refused
	// prefix really was published to the listing more than once. Without the
	// listing-start snapshot the second publication is a second charge.
	if got := rt.logsContaining("WARN", "listing window narrowed at descriptor capacity"); len(got) == 0 {
		t.Fatal("the descriptor capacity never narrowed the window, so the double-count claim is vacuous")
	}
	if got := refusedPages["start"].Load(); got < 2 {
		t.Fatalf("the refused prefix was listed %d time(s); the re-list that would charge it twice never happened", got)
	}

	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.RefusedContents != refused {
		t.Fatalf("durable refused total=%d, want %d: the re-listed prefix was charged more than once",
			state.RefusedContents, refused)
	}
	if state.RefusedUnreported != 0 || state.RefusedReason != "" {
		t.Fatalf("durable refusal accounting=%d/%q, want the report acknowledged",
			state.RefusedUnreported, state.RefusedReason)
	}
	// The exact loss is what reaches the operator, so an inflated count would be
	// an inflated report as well.
	if got := rt.warnStateContaining(fmt.Sprintf("%d descriptor(s) have been permanently refused", refused)); len(got) == 0 {
		t.Fatalf("published status never named %d permanently refused descriptors: %+v",
			refused, rt.warnStateContaining("permanently refused"))
	}
	for _, warning := range rt.logsContaining("WARN", "refused content descriptors") {
		if got := warning.Params["refused_total"]; got != fmt.Sprint(refused) {
			t.Fatalf("reported refused_total=%q, want %d", got, refused)
		}
	}

	// Preservation: narrowing still collects the whole planned range exactly
	// once, which is what the snapshot must not disturb.
	if got := len(rt.Entries()); got != len(accepted) {
		t.Fatalf("entries=%d, want one per accepted content blob", got)
	}
	for id, counter := range fetched {
		if got := counter.Load(); got != 1 {
			t.Fatalf("content %s fetched %d times, want exactly 1", id, got)
		}
	}
	if !state.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the whole planned range %s", state.CompletedThrough, managementTestNow)
	}
}

// TestNarrowingRestoresTheListingStartRefusalSnapshotExactly pins the mechanism
// on its own, including the two cases where it must not fire: a snapshot taken
// for a different WindowStart cannot describe this listing's prefix, and a head
// written by an earlier build carries no snapshot at all.
func TestNarrowingRestoresTheListingStartRefusalSnapshotExactly(t *testing.T) {
	window := func() managementState {
		return managementState{
			Version:     managementStateVersion,
			WindowStart: managementTestNow.Add(-managementMaximumWindow), WindowEnd: managementTestNow,
			RefusedContents: 7, RefusedUnreported: 4, RefusedReason: "a reason",
		}
	}

	t.Run("snapshot-is-taken-once-and-restored", func(t *testing.T) {
		state := window()
		state.snapshotRefusalListingStart()
		// The listing then charges a prefix and is resumed by a later pass,
		// which must not re-snapshot what the first one recorded.
		state.RefusedContents, state.RefusedUnreported = 11, 8
		state.snapshotRefusalListingStart()
		if !narrowManagementWindow(&state) {
			t.Fatal("a full window would not narrow")
		}
		if state.RefusedContents != 7 || state.RefusedUnreported != 4 || state.RefusedReason != "a reason" {
			t.Fatalf("restored accounting=%d/%d/%q, want the listing-start 7/4",
				state.RefusedContents, state.RefusedUnreported, state.RefusedReason)
		}
		// The snapshot survives the narrowing, because the narrowed listing can
		// narrow again and has to restore to the same place.
		if state.RefusedListingStart == nil || state.RefusedListingStart.Contents != 7 {
			t.Fatalf("snapshot after narrowing=%+v, want it retained", state.RefusedListingStart)
		}
		state.RefusedContents, state.RefusedUnreported = 9, 6
		if !narrowManagementWindow(&state) {
			t.Fatal("a 12h window would not narrow")
		}
		if state.RefusedContents != 7 || state.RefusedUnreported != 4 {
			t.Fatalf("second narrowing restored %d/%d, want 7/4", state.RefusedContents, state.RefusedUnreported)
		}
		if err := validateManagementState(state); err != nil {
			t.Fatalf("a restored, narrowed window is not persistable: %v", err)
		}
	})

	// The snapshot has to be durable, not pass-local: a listing is resumed from
	// its persisted continuation across passes and restarts, so the narrowing
	// that restores it can be many passes after the prefix was charged.
	t.Run("it-survives-a-restart", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		server := managementEmptyListingSource(t, "Audit.Exchange")
		plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
		key := managementStateKey("microsoft", "snapshot")

		state := window()
		state.snapshotRefusalListingStart()
		state.RefusedContents, state.RefusedUnreported = 11, 8
		if err := plugin.persistManagementCheckpoint(rt, key, &state); err != nil {
			t.Fatal(err)
		}
		head, err := rt.Get(key)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(head), "refused_listing_start") {
			t.Fatalf("the persisted head does not carry the listing-start snapshot: %s", head)
		}

		restored, err := loadManagementState(rt, key)
		if err != nil {
			t.Fatalf("a head carrying a listing-start snapshot was refused: %v", err)
		}
		if got := restored.RefusedListingStart; got == nil || !got.WindowStart.Equal(state.WindowStart) ||
			got.Contents != 7 || got.Unreported != 4 || got.Reason != "a reason" {
			t.Fatalf("restored snapshot=%+v, want the one that was written", got)
		}
		if !narrowManagementWindow(&restored) {
			t.Fatal("a full window would not narrow after the restart")
		}
		if restored.RefusedContents != 7 || restored.RefusedUnreported != 4 {
			t.Fatalf("a restart restored %d/%d, want 7/4", restored.RefusedContents, restored.RefusedUnreported)
		}
		if err := plugin.persistManagementState(rt, key, &restored); err != nil {
			t.Fatalf("the narrowed, restored state is not persistable: %v", err)
		}
	})

	t.Run("a-new-window-start-replaces-it", func(t *testing.T) {
		state := window()
		state.snapshotRefusalListingStart()
		// The window that collects the remainder starts where the narrowed one
		// ended, so its listing starts from the accounting as it stands now.
		state.WindowStart, state.WindowEnd = managementTestNow.Add(-12*time.Hour), managementTestNow
		state.RefusedContents, state.RefusedUnreported = 11, 8
		state.snapshotRefusalListingStart()
		if got := state.RefusedListingStart; got == nil || got.Contents != 11 || got.Unreported != 8 {
			t.Fatalf("snapshot=%+v, want the new window's own listing start", got)
		}
	})

	t.Run("a-stale-snapshot-is-not-applied", func(t *testing.T) {
		state := window()
		state.RefusedListingStart = &managementRefusalSnapshot{
			WindowStart: managementTestNow.Add(-72 * time.Hour), Contents: 1, Unreported: 1,
		}
		if !narrowManagementWindow(&state) {
			t.Fatal("a full window would not narrow")
		}
		if state.RefusedContents != 7 || state.RefusedUnreported != 4 {
			t.Fatalf("a snapshot for another window was applied: %d/%d", state.RefusedContents, state.RefusedUnreported)
		}
	})

	t.Run("a-head-without-a-snapshot-is-unchanged", func(t *testing.T) {
		state := window()
		if !narrowManagementWindow(&state) {
			t.Fatal("a full window would not narrow")
		}
		if state.RefusedContents != 7 || state.RefusedUnreported != 4 {
			t.Fatalf("accounting=%d/%d, want an earlier build's head left alone", state.RefusedContents, state.RefusedUnreported)
		}
	})

	t.Run("a-hostile-snapshot-is-refused-on-load", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			snapshot managementRefusalSnapshot
			valid    bool
		}{
			{"canonical", managementRefusalSnapshot{Contents: 4, Unreported: 2}, true},
			{"all-reported", managementRefusalSnapshot{Contents: 4}, true},
			{"at-the-live-count", managementRefusalSnapshot{Contents: 7, Unreported: 7}, true},
			{"negative-total", managementRefusalSnapshot{Contents: -1}, false},
			{"negative-outstanding", managementRefusalSnapshot{Contents: 4, Unreported: -1}, false},
			{"more-outstanding-than-counted", managementRefusalSnapshot{Contents: 1, Unreported: 2}, false},
			{"past-the-live-count", managementRefusalSnapshot{Contents: 8, Unreported: 1}, false},
			{"reason-one-byte-over-the-bound", managementRefusalSnapshot{
				Contents: 1, Unreported: 1,
				Reason: strings.Repeat("r", managementMaximumRefusedReasonLength+1)}, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				state := window()
				snapshot := tc.snapshot
				snapshot.WindowStart = state.WindowStart
				state.RefusedListingStart = &snapshot
				err := validateManagementProgressFields(state)
				if tc.valid && err != nil {
					t.Fatalf("a valid snapshot was refused: %v", err)
				}
				if !tc.valid && err == nil {
					t.Fatal("an invalid snapshot was accepted, so a narrowing could restore an unpersistable document")
				}
			})
		}
	})
}

// managementRefusedReasonCase is one stored reason and the only presentation an
// operator may be shown for it.
type managementRefusedReasonCase struct {
	name     string
	stored   string
	reported string
}

// managementRefusedReasonCases covers every class the presentation contract
// exists for, the ordinary sentence it must leave alone, and the already
// escaped literal sequence that proves the reason is escaped exactly once.
func managementRefusedReasonCases() []managementRefusedReasonCase {
	return []managementRefusedReasonCase{
		{"carriage-return", "refused\rforged", `refused\rforged`},
		{"line-feed", "refused\nforged", `refused\nforged`},
		{"crlf-record-forgery", "refused\r\n<13>1 t runner ms - - - forged", `refused\r\n<13>1 t runner ms - - - forged`},
		// Cf: U+200E LEFT-TO-RIGHT MARK has no glyph at all and U+202E
		// RIGHT-TO-LEFT OVERRIDE reorders everything after it.
		{"format-character", "refused\u200e\u202eforged", `refused\u{200e}\u{202e}forged`},
		// Zl and Zp: a Unicode-aware record splitter ends the line and the
		// paragraph here.
		{"line-separator", "refused forged", `refused\u{2028}forged`},
		{"paragraph-separator", "refused forged", `refused\u{2029}forged`},
		{"ordinary-text", "Microsoft Management Activity content descriptor has an invalid contentCreated",
			"Microsoft Management Activity content descriptor has an invalid contentCreated"},
		// The two characters a backslash and an r, which is what an earlier
		// build's escaped presentation looks like when it is read back. Escaping
		// it once doubles the backslash, which is what keeps it distinguishable
		// from a real carriage return; escaping it at the write and again at the
		// display would show an operator a reason nothing ever held.
		{"already-escaped-literal", `refused\rforged`, `refused\\rforged`},
	}
}

// seedManagementRestoredRefusal publishes a head document carrying an
// outstanding refusal and the reason behind it. Load validation bounds that
// reason's length and says nothing at all about its contents, which is exactly
// the state a hostile or corrupted state store can present.
func seedManagementRestoredRefusal(t *testing.T, plugin *Microsoft, rt *microsoftTestRuntime, key, reason string) {
	t.Helper()
	state := managementState{
		Version:           managementStateVersion,
		CompletedThrough:  managementTestNow.Add(-time.Hour),
		TargetThrough:     managementTestNow,
		WindowStart:       managementTestNow.Add(-time.Hour),
		WindowEnd:         managementTestNow,
		RefusedContents:   1,
		RefusedUnreported: 1,
		RefusedReason:     reason,
	}
	if err := plugin.persistManagementCheckpoint(rt, key, &state); err != nil {
		t.Fatal(err)
	}
}

// managementEmptyListingSource completes the window's listing with nothing in
// it, so the pass that runs is the one that reports the refusal the state store
// handed it and nothing else.
func managementEmptyListingSource(t *testing.T, contentType string) *httptest.Server {
	t.Helper()
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = io.WriteString(w, "[]")
		default:
			http.NotFound(w, r)
		}
	})
	t.Cleanup(server.Close)
	return server
}

// RefusedReason is a durable field. The listing wrote an escaped presentation
// into it and the report read it straight back out into log.KV and into the
// error the pass returns, so the only thing between a restored reason and the
// sink was a length check: a state store holding CR, LF, a Unicode Cf format
// character or a Zl/Zp separator in that field ended the physical record
// mid-value and let the remainder be read as further complete records this
// plugin never wrote (CWE-117).
//
// Escaping it on the way out instead is also what keeps it escaped exactly
// once. Escaping at the write and again at the display would double every
// backslash the presentation itself introduced, so a reason stored as the
// escape of a carriage return and one whose own bytes are a backslash and an r
// would become indistinguishable.
func TestManagementRestoredRefusedReasonIsNeutralizedExactlyOnce(t *testing.T) {
	const contentType = "Audit.Exchange"
	const selector = "microsoft365-audit-exchange"

	for _, tc := range managementRefusedReasonCases() {
		t.Run(tc.name, func(t *testing.T) {
			server := managementEmptyListingSource(t, contentType)
			rt := newMicrosoftTestRuntime(t)
			key := managementStateKey("microsoft", selector)
			plugin := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
			seedManagementRestoredRefusal(t, plugin, rt, key, tc.stored)

			// Non-vacuous: the hostile reason really is a document load
			// validation accepts, verbatim, which is what makes it reachable.
			restored, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatalf("the seeded reason was refused on load: %v", err)
			}
			if restored.RefusedReason != tc.stored {
				t.Fatalf("restored reason=%q, want the stored bytes verbatim", restored.RefusedReason)
			}

			_, err = plugin.Handle(t.Context(), rt)
			if err == nil || !strings.Contains(err.Error(), "refused 1 content descriptor") {
				t.Fatalf("poll error=%v, want the restored refusal reported", err)
			}
			assertManagementReasonIsPresentationSafe(t, "returned error", err.Error(), tc.reported)

			warnings := rt.logsContaining("WARN", "refused content descriptors")
			if len(warnings) != 1 {
				t.Fatalf("refusal warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
			}
			reported := warnings[0].Params["reason"]
			if reported != tc.reported {
				t.Fatalf("reported reason=%q, want the single escaped presentation %q", reported, tc.reported)
			}
			assertManagementReasonIsPresentationSafe(t, "log parameter", reported, tc.reported)

			// Escaped exactly once, in both directions: the reported value is
			// what one application of the contract produces, and a reason the
			// contract has nothing to do to is reported unchanged.
			if want := displayVendorValue(tc.stored, maximumDisplayedVendorValue); reported != want {
				t.Fatalf("reported reason=%q, want exactly one application of the contract, %q", reported, want)
			}
			if tc.stored == tc.reported && reported != tc.stored {
				t.Fatalf("an unescaped reason was altered: %q", reported)
			}

			// Presentation only: the durable field was never rewritten in
			// place, and the acknowledgement clears it the way it always did.
			acknowledged, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatal(err)
			}
			if acknowledged.RefusedReason != "" || acknowledged.RefusedUnreported != 0 || acknowledged.RefusedContents != 1 {
				t.Fatalf("accounting after the report=%d/%d/%q, want the count kept and the report acknowledged",
					acknowledged.RefusedContents, acknowledged.RefusedUnreported, acknowledged.RefusedReason)
			}
		})
	}

	// The two presentations above have to stay distinguishable, which is the
	// property a second escape would destroy and an idempotent escape -- the
	// other way to avoid double escaping -- would never have had.
	cases := managementRefusedReasonCases()
	if carriageReturn, literal := cases[0].reported, cases[len(cases)-1].reported; carriageReturn == literal {
		t.Fatalf("a real carriage return and the literal characters %q share one presentation %q", `\r`, carriageReturn)
	}
}

// assertManagementReasonIsPresentationSafe fails unless value carries the
// expected presentation and nothing a log record cannot hold.
func assertManagementReasonIsPresentationSafe(t *testing.T, where, value, reported string) {
	t.Helper()
	if !strings.Contains(value, reported) {
		t.Fatalf("%s=%q, want it to carry the escaped presentation %q", where, value, reported)
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		t.Fatalf("%s=%q retains a control character an operator log cannot carry", where, value)
	}
	for _, table := range []*unicode.RangeTable{unicode.Cf, unicode.Zl, unicode.Zp} {
		if strings.ContainsFunc(value, func(r rune) bool { return unicode.Is(table, r) }) {
			t.Fatalf("%s=%q retains a Unicode format or separator character", where, value)
		}
	}
}

// TestManagementRestoredRefusedReasonIsOneRecordInTheRFC5424FileSink is the
// end-to-end half. A restored reason carrying CR LF and a forged record body
// reaches the real refusal report and the real file sink, and what lands on
// disk has to be exactly one physical record per reported line.
func TestManagementRestoredRefusedReasonIsOneRecordInTheRFC5424FileSink(t *testing.T) {
	const contentType = "Audit.Exchange"
	const selector = "microsoft365-audit-exchange"
	const hostile = "refused\r\n<13>1 2026-10-08T12:00:00Z runner ms - - - forged record\r\n"

	server := managementEmptyListingSource(t, contentType)
	path := t.TempDir() + "/runner.log"
	sink, err := log.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sink.SetAppname("microsoft")
	sink.SetHostname("runner")
	rt := &managementInjectionSinkRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), t: t, sink: sink}

	plugin := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	key := managementStateKey("microsoft", selector)
	seedManagementRestoredRefusal(t, plugin, rt.microsoftTestRuntime, key, hostile)
	if _, err := plugin.Handle(t.Context(), rt); err == nil {
		t.Fatal("poll error=nil, want the restored refusal reported")
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("sink bytes %q do not end a record", raw)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if got, want := len(lines), int(rt.forwarded.Load()); got != want {
		t.Fatalf("the sink holds %d physical lines for %d reported lines:\n%s", got, want, raw)
	}
	// The record the hostile reason tried to forge, exactly as it would start a
	// physical line of its own.
	const forged = "<13>1 2026-10-08T12:00:00Z runner ms"
	carrying := 0
	for _, line := range lines {
		if !strings.HasPrefix(line, "<") {
			t.Fatalf("physical line %q is not a complete record; the reported reason ended the line before it", line)
		}
		if strings.HasPrefix(line, forged) {
			t.Fatalf("physical line %q is the forged record the reason carried", line)
		}
		if strings.ContainsAny(line, "\r") {
			t.Fatalf("physical line %q carries a raw carriage return", line)
		}
		if strings.Contains(line, `refused\\r\\n`) {
			carrying++
		}
	}
	// Both insertions of the reason -- the refusal warning's reason parameter
	// and the error the pass returns -- are on the sink, and each is one whole
	// physical record.
	if carrying != 2 {
		t.Fatalf("the reason reached %d complete physical records, want the 2 insertions it has:\n%s", carrying, raw)
	}
}
