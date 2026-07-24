package jamf

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetToken exercises the OAuth client-credentials token exchange against
// /api/oauth/token: a successful grant, a rejected grant, a response missing
// access_token, and the caching/refresh behavior around tokenExpiryBuffer.
func TestGetToken(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/api/oauth/token" {
				t.Errorf("expected /api/oauth/token, got %s", r.URL.Path)
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
				t.Errorf("bad content-type: %s", ct)
			}
			r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" {
				t.Errorf("bad grant_type: %s", r.Form.Get("grant_type"))
			}
			if r.Form.Get("client_id") != "id" {
				t.Errorf("bad client_id: %s", r.Form.Get("client_id"))
			}
			if r.Form.Get("client_secret") != "secret" {
				t.Errorf("bad client_secret: %s", r.Form.Get("client_secret"))
			}
			json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: 3600})
		}))
		defer server.Close()

		c := NewClient(t.Context(), server.URL, "id", "secret", 60)
		tok, err := c.getToken(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if tok != "tok" {
			t.Errorf("expected %q, got %q", "tok", tok)
		}
	})

	t.Run("failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid_client"}`))
		}))
		defer server.Close()

		c := NewClient(t.Context(), server.URL, "id", "badsecret", 60)
		if _, err := c.getToken(t.Context()); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("missing access token in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(tokenResponse{ExpiresIn: 3600})
		}))
		defer server.Close()

		c := NewClient(t.Context(), server.URL, "id", "secret", 60)
		if _, err := c.getToken(t.Context()); err == nil {
			t.Fatal("expected error for empty access_token, got nil")
		}
	})

	t.Run("caches token across calls", func(t *testing.T) {
		count := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: 3600})
		}))
		defer server.Close()

		c := NewClient(t.Context(), server.URL, "id", "secret", 60)
		if _, err := c.getToken(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := c.getToken(t.Context()); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("expected 1 token request, got %d", count)
		}
	})

	t.Run("refreshes a token close to expiry", func(t *testing.T) {
		count := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			// expires_in shorter than tokenExpiryBuffer means the cached
			// token is treated as already expired on the very next call.
			json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: 1})
		}))
		defer server.Close()

		c := NewClient(t.Context(), server.URL, "id", "secret", 60)
		if _, err := c.getToken(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := c.getToken(t.Context()); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("expected 2 token requests, got %d", count)
		}
	})
}

// TestFetchInventoryPage covers a single call to the computers-inventory
// endpoint: that the bearer token, filter, pagination, and section query
// parameters are all sent correctly, and that both an auth failure and an
// error response from the inventory endpoint itself surface as errors.
func TestFetchInventoryPage(t *testing.T) {
	t.Run("returns values and sets request parameters", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: 3600})
		})
		mux.HandleFunc("/api/v1/computers-inventory", func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("bad auth header: %s", got)
			}
			if got := r.URL.Query().Get("filter"); got != "general.reportDate>2026-01-01T00:00:00Z;general.reportDate<2026-01-01T01:00:00Z" {
				t.Errorf("bad filter: %s", got)
			}
			if got := r.URL.Query().Get("page"); got != "2" {
				t.Errorf("bad page: %s", got)
			}
			if got := r.URL.Query().Get("page-size"); got != "50" {
				t.Errorf("bad page-size: %s", got)
			}
			sections := r.URL.Query()["section"]
			if len(sections) != 2 || sections[0] != "GENERAL" || sections[1] != "STORAGE" {
				t.Errorf("bad sections: %v", sections)
			}
			json.NewEncoder(w).Encode(Response{
				TotalCount: 1,
				Results:    []json.RawMessage{json.RawMessage(`{"id":1}`)},
			})
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		c := NewClient(t.Context(), server.URL, "id", "secret", 60)
		resp, err := c.FetchInventoryPage(t.Context(),
			"general.reportDate>2026-01-01T00:00:00Z;general.reportDate<2026-01-01T01:00:00Z",
			[]string{"GENERAL", "STORAGE"}, 2, 50)
		if err != nil {
			t.Fatal(err)
		}
		if resp.TotalCount != 1 {
			t.Errorf("expected totalCount 1, got %d", resp.TotalCount)
		}
		if len(resp.Results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(resp.Results))
		}
	})

	t.Run("propagates auth failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		c := NewClient(t.Context(), server.URL, "id", "secret", 60)
		if _, err := c.FetchInventoryPage(t.Context(), "x", []string{"GENERAL"}, 0, 10); err == nil {
			t.Fatal("expected error when token acquisition fails")
		}
	})

	t.Run("error response from inventory endpoint", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: 3600})
		})
		mux.HandleFunc("/api/v1/computers-inventory", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"httpStatus":403,"errors":[]}`))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		c := NewClient(t.Context(), server.URL, "id", "secret", 60)
		if _, err := c.FetchInventoryPage(t.Context(), "x", []string{"GENERAL"}, 0, 10); err == nil {
			t.Fatal("expected error")
		}
	})
}
