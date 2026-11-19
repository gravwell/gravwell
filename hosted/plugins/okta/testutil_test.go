/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// testKey is shared by every test, RSA key generation is slow
var testKey = sync.OnceValue(func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
})

func pkcs1PEM(k *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

func pkcs8PEM(t testing.TB, k any) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func b64url(i *big.Int) string { return base64.RawURLEncoding.EncodeToString(i.Bytes()) }

// jwkFields is the private JWK the Okta Admin Console hands out
func jwkFields(k *rsa.PrivateKey, kid string) map[string]string {
	return map[string]string{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
		"n": b64url(k.N), "e": b64url(big.NewInt(int64(k.E))), "d": b64url(k.D),
		"p": b64url(k.Primes[0]), "q": b64url(k.Primes[1]),
		"dp": b64url(k.Precomputed.Dp), "dq": b64url(k.Precomputed.Dq), "qi": b64url(k.Precomputed.Qinv),
	}
}

func oktaJWK(t testing.TB, fields map[string]string) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type logLine struct {
	level, msg string
	params     []rfc5424.SDParam
}

func (l logLine) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s", l.level, l.msg)
	for _, p := range l.params {
		fmt.Fprintf(&sb, " %s=%q", p.Name, p.Value)
	}
	return sb.String()
}

// testRuntime is a minimal in-memory hosted.Runtime. Sleep never waits, it cancels the
// context instead, so a polling loop stops at the first point it would back off.
type testRuntime struct {
	hosted.StatusTracker
	ctx    context.Context
	cancel context.CancelFunc
	onPut  func(key string) // optional hook, called after every Put

	mu      sync.Mutex
	store   map[string][]byte
	entries []entry.Entry
	tags    map[string]entry.EntryTag
	nextTag entry.EntryTag
	logs    []logLine
}

func newTestRuntime(t testing.TB) *testRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &testRuntime{
		ctx:    ctx,
		cancel: cancel,
		store:  make(map[string][]byte),
		tags:   make(map[string]entry.EntryTag),
	}
}

func (r *testRuntime) Alive() bool              { return r.ctx.Err() == nil }
func (r *testRuntime) Context() context.Context { return r.ctx }
func (r *testRuntime) Sleep(time.Duration) bool {
	r.cancel()
	return true
}

func (r *testRuntime) Get(key string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return append([]byte(nil), v...), nil
}

func (r *testRuntime) Put(key string, value []byte) error {
	r.mu.Lock()
	r.store[key] = append([]byte(nil), value...)
	hook := r.onPut
	r.mu.Unlock()
	if hook != nil {
		hook(key)
	}
	return nil
}

func (r *testRuntime) GetString(key string) (string, error) {
	v, err := r.Get(key)
	return string(v), err
}
func (r *testRuntime) PutString(key, value string) error { return r.Put(key, []byte(value)) }

func (r *testRuntime) GetInt64(key string) (int64, error) {
	v, err := r.Get(key)
	if err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(v)), nil
}

func (r *testRuntime) PutInt64(key string, value int64) error {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(value))
	return r.Put(key, b)
}

func (r *testRuntime) GetTime(key string) (time.Time, error) {
	v, err := r.Get(key)
	if err != nil {
		return time.Time{}, err
	}
	var t time.Time
	if err := t.UnmarshalBinary(v); err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func (r *testRuntime) PutTime(key string, value time.Time) error {
	b, err := value.MarshalBinary()
	if err != nil {
		return err
	}
	return r.Put(key, b)
}

func (r *testRuntime) Write(e entry.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	return nil
}

func (r *testRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tags[name]; ok {
		return t, nil
	}
	r.nextTag++
	r.tags[name] = r.nextTag
	return r.nextTag, nil
}

// data returns the raw data of every entry written with tag
func (r *testRuntime) data(tag entry.EntryTag) (out []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Tag == tag {
			out = append(out, string(e.Data))
		}
	}
	return
}

func (r *testRuntime) logged(level string) (out []logLine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.logs {
		if level == `` || l.level == level {
			out = append(out, l)
		}
	}
	return
}

func (r *testRuntime) log(level, msg string, sds []rfc5424.SDParam) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, logLine{level: level, msg: msg, params: sds})
}

func (r *testRuntime) Debug(msg string, sds ...rfc5424.SDParam)    { r.log("DEBUG", msg, sds) }
func (r *testRuntime) Info(msg string, sds ...rfc5424.SDParam)     { r.log("INFO", msg, sds) }
func (r *testRuntime) Warn(msg string, sds ...rfc5424.SDParam)     { r.log("WARN", msg, sds) }
func (r *testRuntime) Error(msg string, sds ...rfc5424.SDParam)    { r.log("ERROR", msg, sds) }
func (r *testRuntime) Critical(msg string, sds ...rfc5424.SDParam) { r.log("CRITICAL", msg, sds) }
