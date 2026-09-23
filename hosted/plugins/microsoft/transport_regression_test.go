package microsoft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type retryBodyProbe struct {
	reader io.Reader
	closed bool
}

func (b *retryBodyProbe) Read(p []byte) (int, error) {
	if b.reader == nil {
		return 0, io.ErrUnexpectedEOF
	}
	return b.reader.Read(p)
}
func (b *retryBodyProbe) Close() error { b.closed = true; return nil }

func TestRetryEligibilitySurvivesResponseBodyFailure(t *testing.T) {
	for _, auth := range []bool{false, true} {
		for _, oversized := range []bool{false, true} {
			for _, status := range []int{429, 503} {
				t.Run(fmt.Sprintf("auth=%v/large=%v/status=%d", auth, oversized, status), func(t *testing.T) {
					rt := newMicrosoftTestRuntime()
					calls := 0
					var bodies []*retryBodyProbe
					for cycle := 0; cycle < 2; cycle++ {
						c, err := NewClient(testClientConfig(t, "https://source.invalid"), nil)
						if err != nil {
							t.Fatal(err)
						}
						if err := c.bindRetryState(rt, nil); err != nil {
							t.Fatal(err)
						}
						c.http.Transport = testRoundTripper(func(r *http.Request) (*http.Response, error) {
							calls++
							body := &retryBodyProbe{}
							if oversized {
								n := 9
								if auth {
									n = (1 << 20) + 1
								}
								body.reader = strings.NewReader(strings.Repeat("x", n))
							}
							bodies = append(bodies, body)
							return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"3600"}}, Body: body}, nil
						})
						ctx, cancel := context.WithTimeout(context.Background(), time.Second)
						if auth {
							_, err = c.tokenResponse(ctx, "https://source.invalid/token", "synthetic")
						} else {
							d := datasets["entra-users"]
							c.tokens[d.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
							ctx = context.WithValue(ctx, fetchBudgetKey{}, &fetchBudget{bytesRemaining: 8, requestsRemaining: 10})
							_, err = c.request(ctx, http.MethodGet, "https://source.invalid/v1.0/users", d.Scope, nil)
						}
						cancel()
						if err == nil {
							t.Fatal("failed response accepted")
						}
					}
					if calls != 1 {
						t.Fatalf("discarded eligibility caused %d requests across recreation", calls)
					}
					for _, b := range bodies {
						if !b.closed {
							t.Fatal("response body not closed")
						}
					}
				})
			}
		}
	}
}

func TestRetryResponseBodyClosesOnStateFailure(t *testing.T) {
	for _, auth := range []bool{false, true} {
		c, err := NewClient(testClientConfig(t, "https://source.invalid"), nil)
		if err != nil {
			t.Fatal(err)
		}
		rt := &retryFaultStorage{newMicrosoftTestRuntime(), true}
		if err := c.bindRetryState(rt, nil); err != nil {
			t.Fatal(err)
		}
		body := &retryBodyProbe{reader: strings.NewReader(`{}`)}
		c.http.Transport = testRoundTripper(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"3600"}}, Body: body}, nil
		})
		if auth {
			_, err = c.tokenResponse(context.Background(), "https://source.invalid/token", "synthetic")
		} else {
			d := datasets["entra-users"]
			c.tokens[d.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
			_, err = c.request(context.Background(), http.MethodGet, "https://source.invalid/v1.0/users", d.Scope, nil)
		}
		var stateErr *retryStateError
		if !errors.As(err, &stateErr) {
			t.Fatalf("state failure lost: %v", err)
		}
		if !body.closed {
			t.Fatal("body leaked on retry-state failure")
		}
	}
}

func TestRetryEligibilityCannotBlockOrRetryEarly(t *testing.T) {
	for _, auth := range []bool{false, true} {
		for _, header := range []string{"600", "601", "9223372036854775807", "18446744073709551616", "Wed, 01 Jan 9999 00:00:00 GMT"} {
			t.Run(fmt.Sprintf("auth=%v/header=%s", auth, header), func(t *testing.T) {
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/token") && !auth {
						fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
						return
					}
					w.Header().Set("Retry-After", header)
					w.WriteHeader(http.StatusTooManyRequests)
				}))
				defer server.Close()
				c, err := NewClient(testClientConfig(t, server.URL), server.Client())
				if err != nil {
					t.Fatal(err)
				}
				c.conf.Max_Retries = 0
				rt := newMicrosoftTestRuntime()
				if err := c.bindRetryState(rt, nil); err != nil {
					t.Fatal(err)
				}
				before := time.Now()
				_, err = c.Fetch(context.Background(), datasets["entra-users"], before, before, "")
				if err == nil {
					t.Fatal("429 unexpectedly succeeded")
				}
				origin, _ := retryOrigin(server.URL)
				eligibility := c.cooldowns[origin]
				switch header {
				case "600", "601":
					seconds := 600
					if header == "601" {
						seconds = 601
					}
					if eligibility.Until.Before(before.Add(time.Duration(seconds) * time.Second)) {
						t.Fatal("vendor eligibility shortened")
					}
				case "Wed, 01 Jan 9999 00:00:00 GMT":
					if eligibility.Until.Year() != 9999 {
						t.Fatal("representable future date lost")
					}
				default:
					if !eligibility.Blocked {
						t.Fatal("unrepresentable hint did not defer fail-closed")
					}
				}
				// Recreated clients must load the same actual eligibility.
				next, err := NewClient(c.conf, server.Client())
				if err != nil {
					t.Fatal(err)
				}
				if err := next.bindRetryState(rt, nil); err != nil {
					t.Fatal(err)
				}
				if next.cooldowns[origin] != eligibility {
					t.Fatal("retry eligibility lost across client recreation")
				}
				short, cancelShort := context.WithTimeout(context.Background(), 20*time.Millisecond)
				if err := next.waitCooldown(short, server.URL); err == nil {
					t.Fatal("deferred origin became eligible")
				}
				cancelShort()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := c.waitCooldown(ctx, server.URL); err != context.Canceled {
					t.Fatalf("cancellation=%v", err)
				}
			})
		}
	}
}

func TestRetryEligibilityIsNotShortened(t *testing.T) {
	if got := retryDelay(0, "600"); got != 10*time.Minute {
		t.Errorf("600 seconds became %s", got)
	}
	future := time.Now().Add(10 * time.Minute).UTC().Format(http.TimeFormat)
	if got := retryDelay(0, future); got < 9*time.Minute {
		t.Errorf("HTTP-date became %s", got)
	}
	if got := retryDelay(0, "9223372036854775807"); got < 0 {
		t.Errorf("retry duration overflowed: %s", got)
	}
}

func TestProductionClientRejectsRedirects(t *testing.T) {
	for _, tokenRedirect := range []bool{false, true} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			t.Run(fmt.Sprintf("token=%v/status=%d", tokenRedirect, status), func(t *testing.T) {
				var targetCalls atomic.Int32
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/forbidden" {
						targetCalls.Add(1)
						fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600,"value":[]}`)
						return
					}
					if strings.HasSuffix(r.URL.Path, "/token") && !tokenRedirect {
						fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
						return
					}
					http.Redirect(w, r, "/forbidden", status)
				}))
				defer server.Close()
				client, err := NewClient(testClientConfig(t, server.URL), nil)
				if err != nil {
					t.Fatal(err)
				}
				client.http.Transport = server.Client().Transport
				_, err = client.Fetch(context.Background(), datasets["entra-directory-audits"], time.Now().Add(-time.Hour), time.Now(), "")
				if err == nil {
					t.Error("redirect accepted")
				}
				if targetCalls.Load() != 0 {
					t.Fatalf("forbidden redirected requests=%d", targetCalls.Load())
				}
			})
		}
	}
}

func TestRetryAfterSurvivesPollRetryExhaustion(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		calls.Add(1)
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = server.Client().Transport
	if _, err := client.Fetch(context.Background(), datasets["entra-signins"], time.Now(), time.Now(), ""); err == nil {
		t.Fatal("throttled poll accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := client.Fetch(ctx, datasets["entra-signins"], time.Now(), time.Now(), ""); err == nil {
		t.Fatal("cooldown ignored")
	}
	if calls.Load() != 1 {
		t.Fatalf("requests during Retry-After cooldown=%d", calls.Load()-1)
	}
}

func TestContinuationCannotSwitchToAnotherConfiguredService(t *testing.T) {
	conf := testClientConfig(t, "https://graph.example.invalid")
	conf.ARM_Host = "https://management.example.invalid"
	c, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.resolveContinuation("https://graph.example.invalid/v1.0/users", "https://management.example.invalid/next"); err == nil {
		t.Fatal("continuation can send the Graph token to another configured origin")
	}
}
