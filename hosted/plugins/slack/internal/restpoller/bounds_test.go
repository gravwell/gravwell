package restpoller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmptyCursorPageContinues(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Write([]byte("{\"data\":[],\"nextCursor\":\"next\"}"))
		} else {
			w.Write([]byte("{\"data\":[{\"id\":\"visible\"}]}"))
		}
	}))
	defer s.Close()
	c, _ := NewClient(s.URL, "synthetic", 10000, 10, 3, s.Client())
	got, e := c.Collect(context.Background(), catalog["n8n"]["users"], nil, "", "")
	if e != nil || len(got.Records) != 1 || calls != 2 {
		t.Fatalf("empty page terminated cursor traversal: %v %d %d", e, len(got.Records), calls)
	}
}
func TestOversizedLogicalRecordFailsWithoutPartialCollection(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{\"data\":[{\"id\":\"large\",\"value\":\"" + strings.Repeat("x", 8<<20) + "\"}]}"))
	}))
	defer s.Close()
	c, _ := NewClient(s.URL, "synthetic", 10000, 10, 3, s.Client())
	got, e := c.Collect(t.Context(), catalog["n8n"]["users"], nil, "", "")
	if e == nil || len(got.Records) != 0 {
		t.Fatal("oversized record accepted")
	}
}
func TestNonSlackStateChangesWithCollectionScope(t *testing.T) {
	c := &Config{Base_URL: "https://one.example", Scope_Identity: "scope"}
	c.SetProduct("n8n")
	d := catalog["n8n"]["users"]
	key := c.stateKey(d)
	c.Base_URL = "https://two.example"
	if c.stateKey(d) == key {
		t.Fatal("base URL shares checkpoint")
	}
}
