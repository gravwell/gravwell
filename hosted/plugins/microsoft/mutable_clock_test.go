package microsoft

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMutableCatalogClocksDoNotFilterCurrentObjects(t *testing.T) {
	for _, name := range []string{"entra-access-reviews", "intune-managed-devices"} {
		t.Run(name, func(t *testing.T) {
			d := datasets[name]
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("$filter") {
					t.Errorf("mutable list filtered by non-mutation clock: %s", r.URL.Query().Get("$filter"))
				}
				rows := []any{matrixRecord(d, "current", "Changed without clock update")}
				if d.ParentPath != "" && r.URL.Path == d.ParentPath {
					rows = []any{map[string]any{"id": "parent"}}
				}
				if e := json.NewEncoder(w).Encode(map[string]any{"value": rows}); e != nil {
					t.Error(e)
				}
			})
			c, e := NewClient(auditConfig(t, s.URL, name), nil)
			if e != nil {
				t.Fatal(e)
			}
			records, e := c.Fetch(context.Background(), d, time.Now().Add(-time.Hour), time.Now(), "")
			if e != nil {
				t.Fatal(e)
			}
			if len(records) != 1 || !strings.Contains(string(records[0].Raw), "Changed without clock update") {
				t.Fatal("mutable source record missing")
			}
			if d.Mode != ModeSnapshot {
				t.Fatal("mutable source without a proven change clock must be snapshot")
			}
		})
	}
}
