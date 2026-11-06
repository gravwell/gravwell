// Coverage for request-level safety: credentials never follow a redirect to
// another origin, pagination trusts only the authoritative cursor signals,
// and one failing dataset in a stanza does not stop its healthy siblings.
package claudecompliance

import (
	"net/http"
	"strings"
	"testing"
)

// A redirect must never carry the API key to a different origin. The client
// stops at the 3xx instead of following it, so no second request is made and
// the error carries neither the credential nor the target.
func TestRedirectNeverCarriesCredentialsToAnotherOrigin(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		p, rt := setup(t, "activities")
		var origins []string
		leaked := false
		p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
			origins = append(origins, r.URL.Scheme+"://"+r.URL.Host)
			if r.URL.Host != "api.anthropic.com" && r.Header.Get("x-api-key") != "" {
				leaked = true
			}
			resp := reply(``, code)
			resp.Header.Set("Location", "https://redirect.example.invalid/collect")
			return resp, nil
		})
		_, err := p.Handle(t.Context(), rt)
		if leaked {
			t.Errorf("%d: credential sent to a foreign origin", code)
		}
		if len(origins) != 1 || origins[0] != "https://api.anthropic.com" {
			t.Errorf("%d: requests reached %v", code, origins)
		}
		if err == nil {
			t.Errorf("%d: redirect was treated as success", code)
		} else if strings.Contains(err.Error(), "synthetic-test-key") ||
			strings.Contains(err.Error(), "redirect.example.invalid") {
			t.Errorf("%d: error text leaked the credential or target: %v", code, err)
		}
		if rt.committed(p) {
			t.Errorf("%d: redirect advanced the checkpoint", code)
		}
	}
}

// Pagination follows only the documented cursor signals. Any extra counter
// the vendor includes -- absent, zero, stale, or contradicting the cursor --
// must not end or extend a traversal.
func TestPaginationIgnoresNonAuthoritativeCounters(t *testing.T) {
	for _, tc := range []struct {
		name       string
		firstExtra string
		wantPages  int
	}{
		{name: "no counter", firstExtra: ``, wantPages: 2},
		{name: "zero total", firstExtra: `,"total":0`, wantPages: 2},
		{name: "stale total", firstExtra: `,"total":1`, wantPages: 2},
		{name: "total contradicts cursor", firstExtra: `,"total":99,"remaining":0`, wantPages: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, rt := setup(t, "activities")
			pages := 0
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				pages++
				if r.URL.Query().Get("after_id") == "" {
					return reply(`{"data":[{"id":"a1"}],"has_more":true,"last_id":"a1"`+tc.firstExtra+`}`, 200), nil
				}
				return reply(`{"data":[{"id":"a2"}],"has_more":false,"total":0}`, 200), nil
			})
			if _, e := p.Handle(t.Context(), rt); e != nil {
				t.Fatal(e)
			}
			if pages != tc.wantPages {
				t.Errorf("requested %d pages, want %d", pages, tc.wantPages)
			}
			if len(rt.entries) != 2 {
				t.Errorf("collected %d entries, want 2", len(rt.entries))
			}
		})
	}
}

// One dataset failing in a multi-dataset stanza must not stop the healthy
// ones, and the healthy progress must still be checkpointed.
func TestOneFailingDatasetDoesNotStopItsSiblings(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Dataset = []string{"activities", "organizations", "groups"}
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/organizations") {
			return reply(`{}`, 503), nil
		}
		return reply(`{"data":[{"id":"r","uuid":"r"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e == nil {
		t.Fatal("a failing dataset was reported as success")
	} else if !strings.Contains(e.Error(), "organizations") {
		t.Errorf("error does not name the failing dataset: %v", e)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("healthy datasets collected %d entries, want 2", len(rt.entries))
	}
	for _, name := range []string{"activities", "groups"} {
		b, e := p.conf.bind(name)
		if e != nil {
			t.Fatal(e)
		}
		if cp := readCheckpoint(t, rt, b.key()); cp.Since.IsZero() {
			t.Errorf("healthy dataset %s did not checkpoint", name)
		}
	}
	bad, e := p.conf.bind("organizations")
	if e != nil {
		t.Fatal(e)
	}
	if cp := readCheckpoint(t, rt, bad.key()); !cp.Since.IsZero() {
		t.Error("failing dataset advanced its checkpoint")
	}
}
