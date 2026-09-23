package microsoft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/processors"
)

func TestRetryStateKeysPreserveStrongestEligibility(t *testing.T) {
	for _, mode := range []string{"disabled", "enabled"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state")
			rt, closeDB := auditRuntime(t, path)
			early, late := time.Now().Add(time.Hour).UTC(), time.Now().Add(2*time.Hour).UTC()
			old := map[string]map[string]retryEligibility{
				retryStateKey:           {"https://raw.invalid": {Until: late}, "https://normalized.invalid": {Until: early}, "https://blocked.invalid": {Blocked: true}},
				normalizedRetryStateKey: {"https://raw.invalid": {Until: early}, "https://normalized.invalid": {Until: late}, "https://blocked.invalid": {Until: late}, "https://normalized-blocked.invalid": {Blocked: true}},
			}
			for key, value := range old {
				b, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := rt.Put(key, b); err != nil {
					t.Fatal(err)
				}
			}
			if err := rt.Put("microsoft/untouched-source", []byte("source-history")); err != nil {
				t.Fatal(err)
			}
			conf := testClientConfig(t, "https://raw.invalid")
			conf.Normalization = mode
			c, err := NewClient(conf, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.bindRetryState(rt, rt.bucket.Sync); err != nil {
				t.Fatal(err)
			}
			closeDB()
			rt, closeDB = auditRuntime(t, path)
			defer closeDB()
			b, err := rt.Get(retryStateKey)
			if err != nil {
				t.Fatal(err)
			}
			var merged map[string]retryEligibility
			if err := json.Unmarshal(b, &merged); err != nil {
				t.Fatal(err)
			}
			if !merged["https://raw.invalid"].Until.Equal(late) || !merged["https://normalized.invalid"].Until.Equal(late) || !merged["https://blocked.invalid"].Blocked || !merged["https://normalized-blocked.invalid"].Blocked {
				t.Fatalf("eligibility weakened: %#v", merged)
			}
			b, err = rt.Get(normalizedRetryStateKey)
			if err != nil {
				t.Fatal(err)
			}
			var normalized map[string]retryEligibility
			if err := json.Unmarshal(b, &normalized); err != nil {
				t.Fatal(err)
			}
			if len(normalized) != len(old[normalizedRetryStateKey]) {
				t.Fatal("predecessor removed")
			}
			b, err = rt.Get("microsoft/untouched-source")
			if err != nil || string(b) != "source-history" {
				t.Fatalf("source state changed: %q %v", b, err)
			}
		})
	}
}

func TestRetryStateKeyMergeFailsClosed(t *testing.T) {
	for _, failure := range []string{"decode", "put", "sync"} {
		t.Run(failure, func(t *testing.T) {
			rt := &retryFaultStorage{microsoftTestRuntime: newMicrosoftTestRuntime()}
			b := []byte(`{"https://fixture.invalid":{"blocked":true}}`)
			if failure == "decode" {
				b = []byte(`{"truncated":`)
			}
			if err := rt.Put(normalizedRetryStateKey, b); err != nil {
				t.Fatal(err)
			}
			rt.fail = failure == "put"
			c, err := NewClient(testClientConfig(t, "https://fixture.invalid"), nil)
			if err != nil {
				t.Fatal(err)
			}
			err = c.bindRetryState(rt, func() error {
				if failure == "sync" {
					return errors.New("synthetic state sync failure")
				}
				return nil
			})
			var stateErr *retryStateError
			if !errors.As(err, &stateErr) {
				t.Fatalf("state merge failure not surfaced: %v", err)
			}
		})
	}
}

func TestDeferredRetryOriginsSurviveHandleAndBoltRestart(t *testing.T) {
	for _, auth := range []bool{false, true} {
		for _, header := range []string{"600", "601", "18446744073709551616", "Wed, 01 Jan 9999 00:00:00 GMT"} {
			t.Run(fmt.Sprintf("auth=%v/header=%s", auth, header), func(t *testing.T) {
				calls := map[string]int{}
				transport := testRoundTripper(func(r *http.Request) (*http.Response, error) {
					calls[r.URL.Host]++
					status := 200
					body := `{"value":[]}`
					headers := make(http.Header)
					if r.URL.Host == "auth.invalid" {
						body = `{"access_token":"synthetic","expires_in":3600}`
					}
					if (auth && r.URL.Host == "auth.invalid") || (!auth && r.URL.Host == "graph.invalid") {
						status = 429
						headers.Set("Retry-After", header)
						body = `{}`
					}
					return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
				})
				conf := testClientConfig(t, "https://graph.invalid")
				conf.Auth_Host = "https://auth.invalid"
				conf.ARM_Host = "https://arm.invalid"
				conf.Defender_Host = "https://defender.invalid"
				conf.Api = []string{"entra-users", "azure-activity", "defender-alerts"}
				conf.Subscription_ID = []string{"33333333-3333-4333-8333-333333333333"}
				conf.Ingester_UUID = "550e8400-e29b-41d4-a716-446655440000"
				// Terminal attempts must persist eligibility and return without sleeping.
				conf.Max_Retries = 0
				statePath := filepath.Join(t.TempDir(), "state")
				for cycle := 0; cycle < 2; cycle++ {
					rt, closeDB := auditRuntime(t, statePath)
					c, err := NewClient(conf, &http.Client{Transport: transport})
					if err != nil {
						t.Fatal(err)
					}
					p := New(conf, processors.NewProcessorSet(rt))
					p.client = c
					p.syncState = rt.bucket.Sync
					// A depleted in-cycle wait budget exercises the no-block deferral path
					// even for a valid 600-second hint, without waiting ten real minutes.
					ctx := context.WithValue(context.Background(), retryWaitDeadlineKey{}, time.Now())
					start := time.Now()
					if _, err := p.Handle(ctx, rt); err != nil {
						t.Fatal(err)
					}
					if time.Since(start) > time.Second {
						t.Fatal("deferred origin blocked the Handle cycle")
					}
					if len(rt.errorLogs) == 0 {
						t.Fatal("deferred origin was not reported")
					}
					closeDB()
				}
				if auth {
					if calls["auth.invalid"] != 1 || calls["graph.invalid"] != 0 || calls["arm.invalid"] != 0 || calls["defender.invalid"] != 0 {
						t.Fatalf("early request after auth throttle: %v", calls)
					}
				} else {
					if calls["graph.invalid"] != 1 || calls["arm.invalid"] != 2 || calls["defender.invalid"] != 2 {
						t.Fatalf("origin isolation/recreated retry: %v", calls)
					}
				}
			})
		}
	}
}

func TestRetryBeyondWaitBudgetReturnsWithoutAnotherAttempt(t *testing.T) {
	for _, auth := range []bool{false, true} {
		calls := 0
		c, err := NewClient(testClientConfig(t, "https://fixture.invalid"), nil)
		if err != nil {
			t.Fatal(err)
		}
		c.conf.Max_Retries = 2
		c.http.Transport = testRoundTripper(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/token") && !auth {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"synthetic","expires_in":3600}`)), Header: make(http.Header)}, nil
			}
			calls++
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"601"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err = c.Fetch(ctx, datasets["entra-users"], time.Now(), time.Now(), "")
		cancel()
		if err == nil || !strings.Contains(err.Error(), "deferred") || calls != 1 {
			t.Fatalf("auth=%v calls=%d err=%v", auth, calls, err)
		}
	}
}

type retryFaultStorage struct {
	*microsoftTestRuntime
	fail bool
}

func (s *retryFaultStorage) Put(k string, b []byte) error {
	if s.fail {
		return errors.New("synthetic retry state failure")
	}
	return s.microsoftTestRuntime.Put(k, b)
}
func TestRetryStateFailureDoesNotPermitEarlyRequest(t *testing.T) {
	rt := &retryFaultStorage{newMicrosoftTestRuntime(), true}
	c, err := NewClient(testClientConfig(t, "https://fixture.invalid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.bindRetryState(rt, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Hour)
	err = c.retainCooldown("https://fixture.invalid", retryEligibility{Until: deadline})
	var stateErr *retryStateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("storage failure=%v", err)
	}
	if err := c.waitCooldown(context.Background(), "https://fixture.invalid"); err == nil {
		t.Fatal("storage failure cleared in-memory eligibility")
	}
	rt.fail = false
	if err := c.retainCooldown("https://fixture.invalid", retryEligibility{Until: deadline}); err != nil {
		t.Fatal(err)
	}
	next, err := NewClient(c.conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.bindRetryState(rt, nil); err != nil {
		t.Fatal(err)
	}
	if err := next.waitCooldown(context.Background(), "https://fixture.invalid"); err == nil {
		t.Fatal("eligibility absent after successful save and recreation")
	}
}

func TestDeferredAuthAllowsCachedTokenAndExpiredDataOrigin(t *testing.T) {
	c, err := NewClient(testClientConfig(t, "https://data.invalid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.conf.Auth_Host = "https://auth.invalid"
	if err := c.retainCooldown(c.conf.Auth_Host, retryEligibility{Until: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := c.retainCooldown(c.conf.Graph_Host, retryEligibility{Until: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	d := datasets["entra-users"]
	c.tokens[d.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	calls := 0
	c.http.Transport = testRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "data.invalid" {
			t.Fatal("unnecessary request to deferred auth origin")
		}
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"value":[]}`))}, nil
	})
	if _, err := c.Fetch(context.Background(), d, time.Now(), time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("eligible data requests=%d", calls)
	}
}

func TestNormalizationSwitchCannotRetryEarly(t *testing.T) {
	for _, auth := range []bool{false, true} {
		for _, first := range []string{"disabled", "enabled"} {
			t.Run(fmt.Sprintf("auth=%v/first=%s", auth, first), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "state")
				calls := 0
				conf := auditConfig(t, "https://data.invalid", "entra-users")
				conf.Auth_Host = "https://auth.invalid"
				conf.Max_Retries = 0
				started := time.Now()
				for cycle := 0; cycle < 2; cycle++ {
					conf.Normalization = first
					if cycle == 1 {
						if first == "disabled" {
							conf.Normalization = "enabled"
						} else {
							conf.Normalization = "disabled"
						}
					}
					rt, closeDB := auditRuntime(t, path)
					c, err := NewClient(conf, nil)
					if err != nil {
						t.Fatal(err)
					}
					c.http.Transport = testRoundTripper(func(r *http.Request) (*http.Response, error) {
						if !auth && r.URL.Host == "auth.invalid" {
							return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"synthetic","expires_in":3600}`))}, nil
						}
						calls++
						return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
					})
					p := New(conf, processors.NewProcessorSet(rt))
					p.client = c
					p.syncState = rt.bucket.Sync
					if _, err = p.Handle(context.Background(), rt); err != nil {
						t.Fatal(err)
					}
					closeDB()
				}
				if calls != 1 {
					t.Fatalf("normalization switch made %d throttled-origin calls in %s inside 3600-second eligibility", calls, time.Since(started))
				}
			})
		}
	}
}
