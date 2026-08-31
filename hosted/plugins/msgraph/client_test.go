package msgraph

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

func TestAuthenticate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" {
				t.Errorf("bad grant_type: %s", r.Form.Get("grant_type"))
			}
			if r.Form.Get("scope") != graphScope {
				t.Errorf("bad scope: %s", r.Form.Get("scope"))
			}
			json.MarshalWrite(w, AuthToken{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
		}))
		defer server.Close()

		c := NewClient("https://graph.example.com", server.URL, "tenant", "id", "secret", server.Client())
		if err := c.authenticate(t.Context()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			json.MarshalWrite(w, AuthErrorResponse{Error: "invalid_client", ErrorDescription: "bad secret"}, jsoncompat.Options)
		}))
		defer server.Close()

		c := NewClient("https://graph.example.com", server.URL, "tenant", "id", "secret", server.Client())
		err := c.authenticate(t.Context())
		if !errors.Is(err, ErrAuthentication) {
			t.Errorf("expected ErrAuthentication, got %v", err)
		}
	})

	t.Run("caches token", func(t *testing.T) {
		count := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			json.MarshalWrite(w, AuthToken{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
		}))
		defer server.Close()

		c := NewClient("https://graph.example.com", server.URL, "tenant", "id", "secret", server.Client())
		c.authenticate(t.Context())
		c.authenticate(t.Context())
		if count != 1 {
			t.Errorf("expected 1 auth call, got %d", count)
		}
	})
}

func TestList(t *testing.T) {
	t.Run("returns values", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/tenant/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
			json.MarshalWrite(w, AuthToken{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
		})
		mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer tok" {
				t.Errorf("bad auth: %s", r.Header.Get("Authorization"))
			}
			json.MarshalWrite(w, ODataResponse{
				Value: []jsontext.Value{jsontext.Value(`{"id":"a1"}`)},
			}, jsoncompat.Options)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		c := NewClient(server.URL, server.URL, "tenant", "id", "secret", server.Client())
		resp, err := c.List(t.Context(), AlertsEndpoint, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Value) != 1 {
			t.Fatalf("expected 1, got %d", len(resp.Value))
		}
	})

	t.Run("uses nextLink", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/tenant/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
			json.MarshalWrite(w, AuthToken{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
		})
		mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("$skiptoken") != "page2" {
				t.Errorf("expected skiptoken=page2, got %s", r.URL.RawQuery)
			}
			json.MarshalWrite(w, ODataResponse{Value: []jsontext.Value{jsontext.Value(`{"id":"a2"}`)}}, jsoncompat.Options)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		c := NewClient(server.URL, server.URL, "tenant", "id", "secret", server.Client())
		resp, err := c.List(t.Context(), AlertsEndpoint, nil, server.URL+"/v1.0/security/alerts_v2?$skiptoken=page2")
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Value) != 1 {
			t.Fatalf("expected 1, got %d", len(resp.Value))
		}
	})

	t.Run("error response", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/tenant/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
			json.MarshalWrite(w, AuthToken{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
		})
		mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			json.MarshalWrite(w, GraphErrorResponse{Error: struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}{Code: "Forbidden", Message: "no access"}}, jsoncompat.Options)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		c := NewClient(server.URL, server.URL, "tenant", "id", "secret", server.Client())
		_, err := c.List(t.Context(), AlertsEndpoint, nil, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestListAll(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tenant/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, AuthToken{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
	})
	pages := 0
	mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
		pages++
		if pages == 1 {
			json.MarshalWrite(w, ODataResponse{
				Value:    []jsontext.Value{jsontext.Value(`{"id":"a1"}`)},
				NextLink: "http://" + r.Host + "/v1.0/security/alerts_v2?$skiptoken=p2",
			}, jsoncompat.Options)
		} else {
			json.MarshalWrite(w, ODataResponse{Value: []jsontext.Value{jsontext.Value(`{"id":"a2"}`)}}, jsoncompat.Options)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewClient(server.URL, server.URL, "tenant", "id", "secret", server.Client())
	results, err := c.ListAll(t.Context(), AlertsEndpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 across pages, got %d", len(results))
	}
}
