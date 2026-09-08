/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

// newTestClient spins up a mock HTTP server backed by mux and returns a Client on it.
func newTestClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	srv := &http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	c, err := NewOpts(Opts{Server: l.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPost(t *testing.T) {
	t.Run("success round trips request and response", func(t *testing.T) {
		var gotMethod, gotPath string
		mux := http.NewServeMux()
		mux.HandleFunc("/files", func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			req, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("failed to read request: %v", err.Error())
			}
			in := &types.File{}
			if err := json.Unmarshal(req, in); err != nil {
				t.Errorf("failed to unmarshal request: %v", err.Error())
			}
			in.ID = "file-id-one"
			w.Header().Set("Content-Type", "application/json")
			b, err := json.Marshal(in)
			if err != nil {
				t.Errorf("failed to marshal response: %v", err.Error())
				return
			}
			w.Write(b)
		})
		c := newTestClient(t, mux)

		var reqF types.File
		reqF.Name = "created"
		respF, err := c.post[types.File, types.File]("/files", &reqF)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		if gotMethod != http.MethodPost {
			t.Errorf("method = %q, want %q", gotMethod, http.MethodPost)
		}
		if gotPath != "/files" {
			t.Errorf("path = %q, want /files", gotPath)
		}
		if respF.Name != reqF.Name {
			t.Errorf("Bad name in response file. Expected '%v' | Actual '%v'", reqF.Name, respF.Name)
		}
		if respF.ID != "file-id-one" {
			t.Errorf("Bad ID in response file. Expected 'file-id-one' | Actual '%v'", respF.ID)
		}
	})

	t.Run("non-200 status surfaces as an error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/files", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		c := newTestClient(t, mux)

		if _, err := c.post[types.File, types.File]("/files", &types.File{}); err == nil {
			t.Fatal("expected error on 500 response, got nil")
		}
	})

	t.Run("malformed response body surfaces a decode error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/files", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`not json`))
		})
		c := newTestClient(t, mux)

		if _, err := c.post[types.File, types.File]("/files", &types.File{}); err == nil {
			t.Fatal("expected a decode error, got nil")
		}
	})
}

func TestPatch(t *testing.T) {
	t.Run("empty patch is short-circuited before any request is sent", func(t *testing.T) {
		called := false
		mux := http.NewServeMux()
		mux.HandleFunc("/files/abc", func(w http.ResponseWriter, r *http.Request) {
			called = true
		})
		c := newTestClient(t, mux)

		if _, err := c.patch[types.FilePatch, types.File]("/files/abc", types.FilePatch{}); !errors.Is(err, ErrEmptyPatch) {
			t.Fatalf("err = %v, want ErrEmptyPatch", err)
		}
		if called {
			t.Fatal("handler should not have been invoked for an empty patch")
		}
	})

	t.Run("non-empty patch round trips request and response", func(t *testing.T) {
		var gotMethod string
		var gotBody []byte
		mux := http.NewServeMux()
		mux.HandleFunc("/files/abc", func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotBody, _ = io.ReadAll(r.Body)
			w.Write([]byte(`{"Name":"renamed"}`))
		})
		c := newTestClient(t, mux)

		p := types.FilePatch{}
		p.Name = types.NewOptional("renamed")
		resp, err := c.patch[types.FilePatch, types.File]("/files/abc", p)
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if gotMethod != http.MethodPatch {
			t.Errorf("method = %q, want %q", gotMethod, http.MethodPatch)
		}
		if !strings.Contains(string(gotBody), `"Name":"renamed"`) {
			t.Errorf("request body missing marshaled Name field: %s", gotBody)
		}
		if resp.Name != "renamed" {
			t.Errorf("resp.Name = %q, want renamed", resp.Name)
		}
	})

	t.Run("non-200 status surfaces as an error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/files/abc", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		c := newTestClient(t, mux)

		p := types.FilePatch{}
		p.Name = types.NewOptional("renamed")
		if _, err := c.patch[types.FilePatch, types.File]("/files/abc", p); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("success sends a DELETE with no body", func(t *testing.T) {
		var gotMethod string
		var gotBody []byte
		mux := http.NewServeMux()
		mux.HandleFunc("/things/1", func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotBody, _ = io.ReadAll(r.Body)
		})
		c := newTestClient(t, mux)

		if err := c.delete("/things/1", false); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if gotMethod != http.MethodDelete {
			t.Errorf("method = %q, want %q", gotMethod, http.MethodDelete)
		}
		if len(gotBody) != 0 {
			t.Errorf("expected empty body, got %q", gotBody)
		}
	})

	t.Run("non-200 status surfaces as an error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/things/1", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		c := newTestClient(t, mux)

		if err := c.delete("/things/1", false); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestGetOptionsParams(t *testing.T) {
	if got := (GetOptions{}).params(); len(got) != 0 {
		t.Errorf("params() with defaults = %+v, want empty", got)
	}
	want := []urlParam{{"include_deleted", "true"}}
	if got := (GetOptions{IncludeDeleted: true}).params(); !reflect.DeepEqual(got, want) {
		t.Errorf("params() with IncludeDeleted = %+v, want %+v", got, want)
	}
}

func TestGet(t *testing.T) {
	t.Run("success round trips response and passes through params", func(t *testing.T) {
		var gotMethod string
		var gotQuery string
		mux := http.NewServeMux()
		mux.HandleFunc("/files/abc", func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotQuery = r.URL.Query().Get("include_deleted")
			w.Write([]byte(`{"Name":"a-file"}`))
		})
		c := newTestClient(t, mux)

		resp, err := c.get[types.File]("/files/abc", GetOptions{IncludeDeleted: true}.params()...)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if gotMethod != http.MethodGet {
			t.Errorf("method = %q, want %q", gotMethod, http.MethodGet)
		}
		if gotQuery != "true" {
			t.Errorf("include_deleted query param = %q, want true", gotQuery)
		}
		if resp.Name != "a-file" {
			t.Errorf("resp.Name = %q, want a-file", resp.Name)
		}
	})

	t.Run("non-200 status surfaces as an error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/files/abc", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
		c := newTestClient(t, mux)

		if _, err := c.get[types.File]("/files/abc"); !errors.Is(err, ErrNotAuthed) {
			t.Fatalf("err = %v, want ErrNotAuthed", err)
		}
	})
}

func TestGetDownload(t *testing.T) {
	t.Run("success returns the raw body reader", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/files/abc/download", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("hello world"))
		})
		c := newTestClient(t, mux)

		rc, err := c.getDownload("/files/abc/download")
		if err != nil {
			t.Fatalf("getDownload: %v", err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}
		if string(data) != "hello world" {
			t.Errorf("body = %q, want %q", data, "hello world")
		}
	})

	t.Run("non-200 status surfaces as an error with no reader", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/files/abc/download", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		c := newTestClient(t, mux)

		rc, err := c.getDownload("/files/abc/download")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if rc != nil {
			t.Errorf("expected nil reader on error, got %v", rc)
		}
	})
}

// tests that the underlying driver (reqDriver)  is operated as expected
func TestReqDriver(t *testing.T) {
	t.Run("headers, client query params, and call params all reach the request", func(t *testing.T) {
		var gotHeader string
		var gotQuery url.Values
		mux := http.NewServeMux()
		mux.HandleFunc("/req", func(w http.ResponseWriter, r *http.Request) {
			gotHeader = r.Header.Get("X-Test-Header")
			gotQuery = r.URL.Query()
		})
		c := newTestClient(t, mux)
		c.hm.add("X-Test-Header", "hdrval")
		c.qm.add("clientparam", "clientval")

		resp, err := c.reqDriver(http.MethodGet, "/req", nil, nil, urlParam{"extra", "extraval"})
		if err != nil {
			t.Fatalf("reqDriver: %v", err)
		}
		defer drainResponse(resp)

		if gotHeader != "hdrval" {
			t.Errorf("header X-Test-Header = %q, want hdrval", gotHeader)
		}
		if v := gotQuery.Get("clientparam"); v != "clientval" {
			t.Errorf("clientparam = %q, want clientval", v)
		}
		if v := gotQuery.Get("extra"); v != "extraval" {
			t.Errorf("extra = %q, want extraval", v)
		}
	})

	t.Run("non-200 status drains the body and returns an aliased error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/req", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("not found body"))
		})
		c := newTestClient(t, mux)

		resp, err := c.reqDriver(http.MethodGet, "/req", nil, nil)
		if resp != nil {
			t.Errorf("expected nil response on error, got %v", resp)
		}
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("request body is sent as-is", func(t *testing.T) {
		var gotBody []byte
		mux := http.NewServeMux()
		mux.HandleFunc("/req", func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
		})
		c := newTestClient(t, mux)

		resp, err := c.reqDriver(http.MethodPost, "/req", []byte(`{"a":1}`), nil)
		if err != nil {
			t.Fatalf("reqDriver: %v", err)
		}
		defer drainResponse(resp)
		if string(gotBody) != `{"a":1}` {
			t.Errorf("body = %s, want {\"a\":1}", gotBody)
		}
	})
}
