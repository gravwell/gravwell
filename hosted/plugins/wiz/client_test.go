/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wiz

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

// fakeWiz is an httptest server that speaks just enough of the Wiz OAuth and
// GraphQL protocol to exercise the client.
type fakeWiz struct {
	server       *httptest.Server
	authCalls    atomic.Int64
	graphqlCalls atomic.Int64

	// tokenToAccept, when non-empty, is the only bearer token graphql accepts;
	// anything else gets an UNAUTHENTICATED graphql error. Updated on each auth.
	current atomic.Value // string
}

func newFakeWiz(t *testing.T) *fakeWiz {
	t.Helper()
	f := &fakeWiz{}
	f.current.Store("")
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		f.authCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("audience") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// mint a fresh token each time so we can detect refreshes.
		token := "tok-" + r.Form.Get("client_id")
		if n := f.authCalls.Load(); n > 1 {
			token = token + "-refreshed"
		}
		f.current.Store(token)
		_ = json.MarshalWrite(w, AuthToken{AccessToken: token, ExpireIn: 3600}, jsoncompat.Options)
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		f.graphqlCalls.Add(1)
		want := f.current.Load().(string)
		got := r.Header.Get("Authorization")
		if got != "Bearer "+want {
			_ = json.MarshalWrite(w, graphQLResponse{
				Errors: []GraphQLError{{Message: "nope", Extensions: struct {
					Code string `json:"code"`
				}{Code: "UNAUTHENTICATED"}}},
			}, jsoncompat.Options)
			return
		}
		_ = json.MarshalWrite(w, graphQLResponse{Data: jsontext.Value(`{"ok":true}`)}, jsoncompat.Options)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeWiz) client() *Client {
	return NewClient(f.server.URL+"/graphql", f.server.URL+"/oauth/token", defaultAudience,
		"cid", "csecret", f.server.Client())
}

func TestAuthenticate(t *testing.T) {
	f := newFakeWiz(t)
	c := f.client()

	if err := c.authenticate(t.Context(), false); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	// a second call should not hit the server because the token is still valid.
	if err := c.authenticate(t.Context(), false); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if n := f.authCalls.Load(); n != 1 {
		t.Fatalf("auth calls = %d, want 1", n)
	}

	// forcing a refresh should hit the server again.
	if err := c.authenticate(t.Context(), true); err != nil {
		t.Fatalf("authenticate force: %v", err)
	}
	if n := f.authCalls.Load(); n != 2 {
		t.Fatalf("auth calls = %d, want 2", n)
	}
}

func TestAuthenticateFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	c := NewClient(server.URL+"/graphql", server.URL+"/oauth/token", defaultAudience, "id", "secret", server.Client())
	err := c.authenticate(t.Context(), false)
	if !errors.Is(err, ErrAuthenticationFailure) {
		t.Fatalf("got %v, want ErrAuthenticationFailure", err)
	}
}

func TestQuery(t *testing.T) {
	f := newFakeWiz(t)
	c := f.client()

	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.Query(t.Context(), "query { ok }", nil, &out); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !out.OK {
		t.Fatal("expected ok=true")
	}
}

// TestQueryErrorClassification verifies that access-denied and internal errors
// are collapsed to sentinel errors that do NOT leak the upstream payload.
func TestQueryErrorClassification(t *testing.T) {
	tests := []struct {
		name    string
		errs    []GraphQLError
		want    error
		payload string
	}{
		{
			name: "access denied by code",
			errs: []GraphQLError{{Message: "access denied: secret detail here", Extensions: struct {
				Code string `json:"code"`
			}{Code: "FORBIDDEN"}}},
			want:    ErrAccessDenied,
			payload: "secret detail",
		},
		{
			name:    "access denied by message",
			errs:    []GraphQLError{{Message: "Access Denied, resource X"}},
			want:    ErrAccessDenied,
			payload: "resource X",
		},
		{
			name:    "internal error by message",
			errs:    []GraphQLError{{Message: "oops! an internal error occurred ref 999"}},
			want:    ErrInternal,
			payload: "ref 999",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/token") {
					_ = json.MarshalWrite(w, AuthToken{AccessToken: "tok", ExpireIn: 3600}, jsoncompat.Options)
					return
				}
				_ = json.MarshalWrite(w, graphQLResponse{Errors: test.errs}, jsoncompat.Options)
			}))
			defer server.Close()

			c := NewClient(server.URL+"/graphql", server.URL+"/oauth/token", defaultAudience, "id", "secret", server.Client())
			err := c.Query(t.Context(), "query { x }", nil, nil)
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), test.payload) {
				t.Errorf("error %q leaked payload %q", err.Error(), test.payload)
			}
		})
	}
}

// TestQueryReauth simulates a stale token: the graphql handler rejects the
// initial bearer, the client must refresh and retry successfully.
func TestQueryReauth(t *testing.T) {
	f := newFakeWiz(t)
	c := f.client()

	// prime a valid token, then invalidate it server side so the first graphql
	// call comes back UNAUTHENTICATED and forces a refresh.
	if err := c.authenticate(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	f.current.Store("stale-token-nobody-has")

	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.Query(t.Context(), "query { ok }", nil, &out); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !out.OK {
		t.Fatal("expected ok=true after reauth")
	}
	// one initial auth + one forced refresh.
	if n := f.authCalls.Load(); n != 2 {
		t.Fatalf("auth calls = %d, want 2", n)
	}
}
