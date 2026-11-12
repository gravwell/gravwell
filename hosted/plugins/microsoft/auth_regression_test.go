package microsoft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type failingTransport struct{}

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic-sensitive-transport-detail")
}

func TestTransportErrorsDoNotExposeRequestOrUpstreamDetail(t *testing.T) {
	c, err := NewClient(testClientConfig(t, "https://fixture.invalid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.http = &http.Client{Transport: failingTransport{}}
	_, err = c.Fetch(context.Background(), datasets["entra-directory-audits"], time.Now(), time.Now(), "")
	if err == nil {
		t.Fatal("transport failure accepted")
	}
	for _, s := range []string{"fixture.invalid", "11111111-1111", "synthetic-sensitive-transport-detail"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("diagnostic leaked %q", s)
		}
	}
}

func TestOAuthAndAPIRequestsBothUsePacing(t *testing.T) {
	times := make(chan time.Time, 2)
	// Observe dispatch, not server arrival: TLS setup on the first request
	// can consume the pacing interval before that handler receives anything.
	transport := testRoundTripper(func(r *http.Request) (*http.Response, error) {
		times <- time.Now()
		body := `{"value":[]}`
		if strings.HasSuffix(r.URL.Path, "/token") {
			body = `{"access_token":"synthetic","expires_in":3600}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	conf := testClientConfig(t, "https://fixture.invalid")
	conf.Requests_Per_Minute = 600
	c, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.limiter = rate.NewLimiter(rate.Every(100*time.Millisecond), 1)
	c.http = &http.Client{Transport: transport}
	if _, err = c.Fetch(context.Background(), datasets["entra-directory-audits"], time.Now(), time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	first, second := <-times, <-times
	if second.Sub(first) < 75*time.Millisecond {
		t.Fatalf("token and API attempts only %s apart", second.Sub(first))
	}
}

func TestOAuthExpiryOverflowRejected(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"access_token":"synthetic","expires_in":9223372036854775807}`)
	}))
	defer server.Close()
	c, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.http = server.Client()
	if _, err = c.token(context.Background(), "synthetic-scope"); err == nil {
		t.Fatal("overflowing token expiry accepted")
	}
}
