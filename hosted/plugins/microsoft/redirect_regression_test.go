package microsoft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestMicrosoftRedirectPolicyAllowsSameOriginReplayablePosts(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprintf("data-%d", status), func(t *testing.T) {
			const payload = `{"query":"DeviceEvents | take 1"}`
			var finalCalls atomic.Int64
			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					http.Redirect(w, r, server.URL+"/final", status)
					return
				}
				finalCalls.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodPost || string(body) != payload || r.Header.Get("Authorization") != "Bearer synthetic-token" {
					t.Errorf("method=%s body=%q authorization=%q", r.Method, body, r.Header.Get("Authorization"))
				}
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()

			client := newProductionPathTestClient(t, testClientConfig(t, server.URL), server.Client().Transport)
			const scope = "https://graph.microsoft.com/.default"
			client.tokens[scope] = cachedToken{Value: "synthetic-token", Expires: time.Now().Add(time.Hour)}
			if _, err := client.request(t.Context(), http.MethodPost, server.URL+"/start", scope, []byte(payload)); err != nil {
				t.Fatal(err)
			}
			if finalCalls.Load() != 1 {
				t.Fatalf("final calls=%d", finalCalls.Load())
			}
		})

		t.Run(fmt.Sprintf("oauth-%d", status), func(t *testing.T) {
			var finalCalls atomic.Int64
			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					http.Redirect(w, r, server.URL+"/token-final", status)
					return
				}
				finalCalls.Add(1)
				body, _ := io.ReadAll(r.Body)
				values, err := url.ParseQuery(string(body))
				if err != nil || values.Get("client_secret") != "synthetic-client-secret" || values.Get("grant_type") != "client_credentials" {
					t.Errorf("form=%q error=%v", body, err)
				}
				_, _ = io.WriteString(w, `{"access_token":"redirected-token","expires_in":3600}`)
			}))
			defer server.Close()

			client := newProductionPathTestClient(t, testClientConfig(t, server.URL), server.Client().Transport)
			token, err := client.token(t.Context(), "synthetic-scope")
			if err != nil || token != "redirected-token" {
				t.Fatalf("token=%q error=%v", token, err)
			}
			if finalCalls.Load() != 1 {
				t.Fatalf("final calls=%d", finalCalls.Load())
			}
		})
	}
}

func TestMicrosoftRedirectPolicyBlocksBearerAcrossOrigin(t *testing.T) {
	tests := []struct {
		name     string
		location func(source, sink string) string
		plain    bool
	}{
		{name: "effective-port", location: func(_, sink string) string { return sink + "/capture" }},
		{name: "hostname", location: func(source, _ string) string {
			return strings.Replace(source, "127.0.0.1", "localhost", 1) + "/capture"
		}},
		{name: "scheme", plain: true, location: func(_, sink string) string { return sink + "/capture" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sinkCalls atomic.Int64
			var sink *httptest.Server
			if tc.plain {
				sink = httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sinkCalls.Add(1) }))
			} else {
				sink = httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sinkCalls.Add(1) }))
			}
			defer sink.Close()

			var sourceCalls atomic.Int64
			var source *httptest.Server
			source = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceCalls.Add(1)
				http.Redirect(w, r, tc.location(source.URL, sink.URL), http.StatusTemporaryRedirect)
			}))
			defer source.Close()

			client := newProductionPathTestClient(t, testClientConfig(t, source.URL), source.Client().Transport)
			const scope = "https://graph.microsoft.com/.default"
			client.tokens[scope] = cachedToken{Value: "synthetic-bearer", Expires: time.Now().Add(time.Hour)}
			if _, err := client.request(t.Context(), http.MethodPost, source.URL+"/start", scope, []byte(`{"secret":"request-body"}`)); err == nil {
				t.Fatal("cross-origin redirect accepted")
			}
			if sourceCalls.Load() != 1 || sinkCalls.Load() != 0 {
				t.Fatalf("source calls=%d sink calls=%d", sourceCalls.Load(), sinkCalls.Load())
			}
		})
	}
}

func TestMicrosoftRedirectPolicyBlocksOAuthSecretAcrossOrigin(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			var sinkCalls atomic.Int64
			sink := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sinkCalls.Add(1) }))
			defer sink.Close()
			var sourceCalls atomic.Int64
			source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceCalls.Add(1)
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "client_secret=synthetic-client-secret") {
					t.Errorf("initial OAuth body=%q", body)
				}
				http.Redirect(w, r, sink.URL+"/capture", status)
			}))
			defer source.Close()

			client := newProductionPathTestClient(t, testClientConfig(t, source.URL), source.Client().Transport)
			if _, err := client.token(t.Context(), "synthetic-scope"); err == nil {
				t.Fatal("cross-origin OAuth redirect accepted")
			}
			if sourceCalls.Load() != 1 || sinkCalls.Load() != 0 {
				t.Fatalf("source calls=%d sink calls=%d", sourceCalls.Load(), sinkCalls.Load())
			}
		})
	}
}

func TestMicrosoftRedirectPolicyPreservesCancellation(t *testing.T) {
	slowStarted := make(chan struct{}, 1)
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, server.URL+"/slow", http.StatusTemporaryRedirect)
			return
		}
		slowStarted <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()

	client := newProductionPathTestClient(t, testClientConfig(t, server.URL), server.Client().Transport)
	const scope = "https://graph.microsoft.com/.default"
	client.tokens[scope] = cachedToken{Value: "synthetic-token", Expires: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := client.request(ctx, http.MethodGet, server.URL+"/start", scope, nil)
		done <- err
	}()
	<-slowStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func newProductionPathTestClient(t *testing.T, conf *Config, transport http.RoundTripper) *Client {
	t.Helper()
	prior := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	return client
}
