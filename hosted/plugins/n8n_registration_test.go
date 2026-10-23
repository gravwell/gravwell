/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"github.com/gravwell/gcfg"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// Operators copy example.conf verbatim, so its Global block has to load. A placeholder
// that is not a UUID fails IngestConfig.Verify before any n8n stanza is reached, and the
// operator is sent to the Global block instead of the values they actually have to fill in.
func TestN8nExampleGlobalBlockIsLoadable(t *testing.T) {
	body, err := os.ReadFile("n8n/example.conf")
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Global config.IngestConfig
		State  storage.BoltConfig
		Configs
	}
	if err := gcfg.ReadStringInto(&example, string(body)); err != nil {
		t.Fatal(err)
	}
	if id := example.Global.Ingester_UUID; id != "" {
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("example Global Ingester-UUID %q is not a UUID: %v", id, err)
		}
	}
	if example.N8n["primary"] == nil {
		t.Fatal("example did not register an N8n config")
	}
}

func TestN8nInlineCredentialAndDirectGlobalSecretAreLoadable(t *testing.T) {
	const source = `
[Global]
Ingest-Secret="synthetic-ingest-secret"
Cleartext-Backend-Target="127.0.0.1:4023"
Log-Level="INFO"

[N8n "inline"]
Base-URL="https://n8n.example.com/api/v1"
Credential="synthetic-n8n-key"
Dataset="users"
`
	var parsed struct {
		Global config.IngestConfig
		Configs
	}
	if err := gcfg.ReadStringInto(&parsed, source); err != nil {
		t.Fatalf("parse inline Credential and Global parameters: %v", err)
	}
	if err := parsed.Global.Verify(); err != nil {
		t.Fatalf("verify Global parameters: %v", err)
	}
	if got := parsed.Global.Secret(); got != "synthetic-ingest-secret" {
		t.Fatalf("Global Ingest-Secret was not retained: got length %d", len(got))
	}
	conf := parsed.N8n["inline"]
	if conf == nil {
		t.Fatal("inline N8n config was not registered")
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("verify inline N8n config: %v", err)
	}
	if conf.Credential != "synthetic-n8n-key" {
		t.Fatalf("inline credential was not retained: got length %d", len(conf.Credential))
	}
}

type n8nRegistrationRuntime struct {
	hosted.StatusTracker
	ctx     context.Context
	mu      sync.Mutex
	entries []entry.Entry
	store   map[string][]byte
	tags    map[string]entry.EntryTag
	nextTag entry.EntryTag
}

func newN8nRegistrationRuntime(ctx context.Context) *n8nRegistrationRuntime {
	return &n8nRegistrationRuntime{ctx: ctx, store: make(map[string][]byte), tags: make(map[string]entry.EntryTag)}
}

func (r *n8nRegistrationRuntime) Alive() bool                         { return true }
func (r *n8nRegistrationRuntime) Context() context.Context            { return r.ctx }
func (r *n8nRegistrationRuntime) Sleep(time.Duration) bool            { return true }
func (r *n8nRegistrationRuntime) Debug(string, ...rfc5424.SDParam)    {}
func (r *n8nRegistrationRuntime) Info(string, ...rfc5424.SDParam)     {}
func (r *n8nRegistrationRuntime) Warn(string, ...rfc5424.SDParam)     {}
func (r *n8nRegistrationRuntime) Error(string, ...rfc5424.SDParam)    {}
func (r *n8nRegistrationRuntime) Critical(string, ...rfc5424.SDParam) {}

func (r *n8nRegistrationRuntime) Write(value entry.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, value)
	return nil
}

func (r *n8nRegistrationRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tag, ok := r.tags[name]; ok {
		return tag, nil
	}
	r.nextTag++
	r.tags[name] = r.nextTag
	return r.nextTag, nil
}

func (r *n8nRegistrationRuntime) Get(key string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return append([]byte(nil), value...), nil
}

func (r *n8nRegistrationRuntime) Put(key string, value []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[key] = append([]byte(nil), value...)
	return nil
}

func (r *n8nRegistrationRuntime) GetString(key string) (string, error) {
	value, err := r.Get(key)
	return string(value), err
}
func (r *n8nRegistrationRuntime) PutString(key, value string) error {
	return r.Put(key, []byte(value))
}
func (r *n8nRegistrationRuntime) GetInt64(string) (int64, error) {
	return 0, storage.ErrStorageNotFound
}
func (r *n8nRegistrationRuntime) PutInt64(string, int64) error { return nil }
func (r *n8nRegistrationRuntime) GetTime(string) (time.Time, error) {
	return time.Time{}, storage.ErrStorageNotFound
}
func (r *n8nRegistrationRuntime) PutTime(string, time.Time) error { return nil }

func TestN8nRegistrationRunsThroughStandardJobAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/users" {
			http.NotFound(w, request)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"user-1"}],"nextCursor":null}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	secret := filepath.Join(dir, "n8n-api-key")
	if err := os.WriteFile(secret, []byte("synthetic-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("n8n/example.conf")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	start := strings.Index(source, "[N8n ")
	if start < 0 {
		t.Fatal("example configuration is missing the N8n stanza")
	}
	source = strings.ReplaceAll(source[start:], "/opt/gravwell/secrets/n8n-api-key", secret)

	var configs Configs
	if err := gcfg.ReadStringInto(&configs, source); err != nil {
		t.Fatal(err)
	}
	config := configs.N8n["primary"]
	if config == nil {
		t.Fatal("example did not register an N8n config")
	}
	config.Base_URL = server.URL + "/api/v1"
	config.Allow_Plaintext = true
	config.Dataset = []string{"users"}
	if err := configs.Verify(); err != nil {
		t.Fatal(err)
	}
	if got := configs.IngesterCount(); got != 1 {
		t.Fatalf("IngesterCount=%d", got)
	}
	tags, err := configs.Tags()
	if err != nil || !slices.Equal(tags, []string{"n8n-users"}) {
		t.Fatalf("Tags=%v error=%v", tags, err)
	}

	var found bool
	for name, builder := range configs.Builders() {
		if name != "primary" || builder.Kind() != "n8n" {
			continue
		}
		found = true
		var syncCalls atomic.Int32
		ingester, err := builder.Build(nil, func() error {
			syncCalls.Add(1)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		runtime := newN8nRegistrationRuntime(t.Context())
		if err := ingester.Run(t.Context(), runtime); err != nil {
			t.Fatal(err)
		}
		if syncCalls.Load() != 1 {
			t.Fatalf("state sync calls=%d want 1", syncCalls.Load())
		}
		if len(runtime.entries) != 1 || len(runtime.store) != 1 {
			t.Fatalf("entries=%d state keys=%d", len(runtime.entries), len(runtime.store))
		}
		if _, ok := runtime.tags["n8n-users"]; !ok {
			t.Fatalf("negotiated tags=%v", runtime.tags)
		}
	}
	if !found {
		t.Fatal("n8n builder was not registered")
	}
}
