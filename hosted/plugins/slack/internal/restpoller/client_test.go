package restpoller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestN8NCursorPaginationAndHeaderAuthentication(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get("X-N8N-API-KEY"); got != "test-key" {
			t.Errorf("X-N8N-API-KEY=%q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `{"data":[{"id":"1"},{"id":"2"}],"nextCursor":"next-1"}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"3"}],"nextCursor":null}`)
	}))
	defer server.Close()

	definition := catalog["n8n"]["users"]
	client, err := NewClient(server.URL, "test-key", 10000, 2, 5, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	collection, err := client.Collect(context.Background(), definition, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(collection.Records) != 3 || requests.Load() != 2 {
		t.Fatalf("records=%d requests=%d", len(collection.Records), requests.Load())
	}
}

func TestFreshdeskLinkPaginationStaysOnConfiguredOrigin(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "token" || password != "X" {
			t.Errorf("BasicAuth user=%q password=%q ok=%v", user, password, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			w.Header().Set("Link", `<`+server.URL+`/api/v2/tickets?after=abc>; rel="next"`)
			fmt.Fprint(w, `[{"id":1}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", 10000, 100, 5, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	definition := catalog["freshworks"]["tickets"]
	definition.PersistCursor = true
	collection, err := client.Collect(context.Background(), definition, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(collection.Records) != 1 || !strings.Contains(collection.PersistCursor, "after=abc") {
		t.Fatalf("collection=%+v", collection)
	}
}

func TestPaginationContinuationCannotEscapeConfiguredOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<https://attacker.invalid/logs>; rel="next"`)
		fmt.Fprint(w, `[{"id":"one"}]`)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "token", 10000, 100, 5, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Collect(context.Background(), catalog["freshworks"]["tickets"], nil, "", ""); err == nil {
		t.Fatal("cross-origin continuation was accepted")
	}
}

func TestNetSuiteSuiteQLHeadersBodyAndOffsetPagination(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Prefer") != "transient" || r.Header.Get("Authorization") != "Bearer oauth-token" {
			t.Errorf("method=%s prefer=%q auth=%q", r.Method, r.Header.Get("Prefer"), r.Header.Get("Authorization"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["q"] != "SELECT id FROM employee ORDER BY id" {
			t.Errorf("query=%q", body["q"])
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("offset") == "0" {
			fmt.Fprint(w, `{"items":[{"id":"1"},{"id":"2"}],"hasMore":true}`)
			return
		}
		if got := r.URL.Query().Get("offset"); got != "2" {
			t.Errorf("offset=%q", got)
		}
		fmt.Fprint(w, `{"items":[{"id":"3"}],"hasMore":false}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "oauth-token", 10000, 2, 5, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	collection, err := client.Collect(context.Background(), catalog["netsuite"]["suiteql"], make(url.Values), "", "SELECT id FROM employee ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if len(collection.Records) != 3 || requests.Load() != 2 {
		t.Fatalf("records=%d requests=%d", len(collection.Records), requests.Load())
	}
}

func TestParseNextLink(t *testing.T) {
	header := `<https://example.test/previous>; rel="prev", <https://example.test/next>; rel="next"`
	if got := parseNextLink(header); got != "https://example.test/next" {
		t.Fatalf("next=%q", got)
	}
}
