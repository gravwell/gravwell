// Coverage for the request contract each dataset declares: what temporal
// filter an endpoint accepts, which inventories must be bounded full scans,
// and that no dataset is singled out by name in a way that leaves a
// semantically equivalent sibling sending parameters its endpoint rejects.
package claudecompliance

import (
	"net/http"
	"net/url"
	"sort"
	"testing"
	"time"
)

// documentedParams are the only query parameters any dataset may send.
var documentedParams = map[string]bool{
	"limit": true, "page": true, "after_id": true,
	"order": true, "order_by": true,
}

// TestEveryRequestSendsOnlyDocumentedParameters walks every dataset on its
// root-only path and proves the request carries nothing beyond pagination,
// ordering, and the endpoint's own declared time filter.
func TestEveryRequestSendsOnlyDocumentedParameters(t *testing.T) {
	for name, d := range Datasets {
		t.Run(name, func(t *testing.T) {
			p, rt := setup(t, name)
			p.conf.Follow_Children = "disabled"
			if e := p.conf.Verify(); e != nil {
				t.Fatal(e)
			}
			var q url.Values
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				q = r.URL.Query()
				raw := `{"id":"x"}`
				if d.Rows != "" {
					raw = `{"` + d.Rows + `":[{"id":"x"}],"has_more":false,"next_page":null}`
				}
				return reply(raw, 200), nil
			})
			if _, e := p.Handle(t.Context(), rt); e != nil {
				t.Fatal(e)
			}
			for key := range q {
				if documentedParams[key] {
					continue
				}
				// The only other admissible parameters are this dataset's own
				// declared window bounds.
				if d.Window != "" && (key == d.Window+".gte" || key == d.Window+".lte") {
					continue
				}
				t.Errorf("%s sent undocumented parameter %q=%q", name, key, q.Get(key))
			}
			if d.Window == "" {
				for key := range q {
					if len(key) > 4 && (key[len(key)-4:] == ".gte" || key[len(key)-4:] == ".lte") {
						t.Errorf("%s declares no time filter but sent %q", name, key)
					}
				}
			}
		})
	}
}

// TestProjectsRootInventoryIsABoundedFullScan pins the projects contract:
// the endpoint documents creation-time filters while the record is mutable,
// so neither time may be used to narrow the inventory. Filtering on either
// would silently drop a project edited outside the window.
func TestProjectsRootInventoryIsABoundedFullScan(t *testing.T) {
	if w := Datasets["projects"].Window; w != "" {
		t.Fatalf("projects declares time filter %q; the endpoint supports none that is safe here", w)
	}
	for _, follow := range []string{"disabled", "enabled"} {
		t.Run(follow, func(t *testing.T) {
			p, rt := setup(t, "projects")
			p.conf.Follow_Children = follow
			if e := p.conf.Verify(); e != nil {
				t.Fatal(e)
			}
			var queries []url.Values
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				queries = append(queries, r.URL.Query())
				if r.URL.Path == "/v1/compliance/apps/projects" {
					return reply(`{"data":[{"id":"p1","updated_at":"2026-09-07T23:00:00Z"}],"has_more":false}`, 200), nil
				}
				return reply(`{"data":[],"has_more":false}`, 200), nil
			})
			if _, e := p.Handle(t.Context(), rt); e != nil {
				t.Fatalf("valid projects request failed: %v", e)
			}
			for _, q := range queries {
				for key := range q {
					if !documentedParams[key] {
						t.Errorf("follow=%s: undocumented parameter %q=%q", follow, key, q.Get(key))
					}
				}
			}
			if len(rt.entries) != 1 {
				t.Fatalf("follow=%s: expected the project record, got %d entries", follow, len(rt.entries))
			}
			// The record keeps its own update time, and the full scan
			// checkpoints with its manifest retained.
			want, _ := timeParse("2026-09-07T23:00:00Z")
			if got := rt.entries[0].TS.StandardTime().UTC(); !got.Equal(want) {
				t.Errorf("follow=%s: record timestamp %v, want the record's updated_at %v", follow, got, want)
			}
			cp := readCheckpoint(t, rt, p.conf.key())
			if cp.Since.IsZero() {
				t.Errorf("follow=%s: full scan did not checkpoint", follow)
			}
			if len(cp.Manifest) != 1 {
				t.Errorf("follow=%s: manifest holds %d entries, want the scanned record", follow, len(cp.Manifest))
			}
			// A second cycle must deduplicate rather than rewrite.
			before := len(rt.entries)
			if _, e := p.Handle(t.Context(), rt); e != nil {
				t.Fatal(e)
			}
			if len(rt.entries) != before {
				t.Errorf("follow=%s: unchanged project was rewritten", follow)
			}
		})
	}
}

// TestDatasetsDeclaringAWindowCanActuallyUseIt proves the catalog's time
// filters and the request builder's one dataset-name exception agree, so no
// sibling silently inherits a filter shape its endpoint does not accept.
func TestDatasetsDeclaringAWindowCanActuallyUseIt(t *testing.T) {
	windowed := map[string]bool{}
	for name, d := range Datasets {
		if d.Window != "" {
			windowed[name] = true
			if d.Rows == "" {
				t.Errorf("%s declares a time filter on a single-object endpoint", name)
			}
		}
	}
	// Only local-sessions is treated specially, and only to omit the upper
	// bound its endpoint does not document.
	for name := range windowed {
		p, rt := setup(t, name)
		p.conf.Follow_Children = "disabled"
		if e := p.conf.Verify(); e != nil {
			t.Fatal(e)
		}
		d := Datasets[name]
		var q url.Values
		p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
			q = r.URL.Query()
			return reply(`{"`+d.Rows+`":[],"has_more":false,"next_page":null}`, 200), nil
		})
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
		if !q.Has(d.Window + ".gte") {
			t.Errorf("%s declares window %q but sent no lower bound", name, d.Window)
		}
		hasUpper := q.Has(d.Window + ".lte")
		if name == "local-sessions" && hasUpper {
			t.Errorf("%s sent an upper bound its endpoint does not document", name)
		}
		if name != "local-sessions" && !hasUpper {
			t.Errorf("%s sent no upper bound", name)
		}
	}
	if len(windowed) == 0 {
		t.Fatal("no dataset declares a time filter; the catalog contract is not being exercised")
	}
	t.Logf("datasets with a documented time filter: %v", sortedKeys(windowed))
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func timeParse(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }
