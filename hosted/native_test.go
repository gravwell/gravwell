package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNativeRunnerCloseStopsOneIngester covers stopping a single ingester while the runtime
// it was handed stays alive, which is exactly what a config reload does. The process wide
// runtime context is never cancelled here, so a runner that waits on the runtime instead of
// its own context hangs in Close and the reload never finishes.
func TestNativeRunnerCloseStopsOneIngester(t *testing.T) {
	// this context stands in for the runtime shared by every ingester in the process
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := newTestRuntime(ctx, cancel)

	// an ingester that only ever waits on the runtime, the way the job adapter does
	var once sync.Once
	started := make(chan struct{})
	ig := ingesterFunc(func(_ context.Context, rt Runtime) error {
		for {
			once.Do(func() { close(started) })
			if rt.Sleep(10 * time.Millisecond) {
				return nil
			}
		}
	})

	nr, err := NewNativeRunner("test.ingesters.gravwell.io", "test", "1.0.0", uuid.New(), &struct{}{}, ig, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err = nr.Start(); err != nil {
		t.Fatal(err)
	}
	// closing before the ingester is actually up would exit on the run loop guard and
	// prove nothing, wait until it is really in its poll loop
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("ingester never started")
	}

	done := make(chan error, 1)
	go func() { done <- nr.Close() }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("close returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close did not return, the ingester cannot be stopped on its own")
	}
	if nr.Running() {
		t.Error("runner still reports running after close")
	}
	if ctx.Err() != nil {
		t.Error("closing one runner cancelled the shared runtime context")
	}
}

type ingesterFunc func(context.Context, Runtime) error

func (f ingesterFunc) Run(ctx context.Context, rt Runtime) error { return f(ctx, rt) }

// TestNativeRunnerChildState covers the state a runner hands up for registration as a child on
// the ingest muxer, the identifiers have to come from the runner and the ingest counters have to
// come from the runtime the ingester was actually writing through.
func TestNativeRunnerChildState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &stateRuntime{
		testRuntime: newTestRuntime(ctx, cancel),
		state:       State{Entries: 12, Size: 480, Tags: []string{`gravwell`, `test`}},
	}

	guid := uuid.New()
	started := make(chan struct{})
	var once sync.Once
	ig := ingesterFunc(func(_ context.Context, rt Runtime) error {
		for {
			once.Do(func() { close(started) })
			if rt.Sleep(10 * time.Millisecond) {
				return nil
			}
		}
	})
	nr, err := NewNativeRunner("test.ingesters.gravwell.io", "instance", "1.2.3", guid, &struct{}{}, ig, rt)
	if err != nil {
		t.Fatal(err)
	}

	// an ingester that has never been started reports no uptime
	if s := nr.ChildState(); s.Uptime != 0 {
		t.Fatalf("stopped ingester reported uptime %v", s.Uptime)
	}

	if err = nr.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("ingester never started")
	}

	s := nr.ChildState()
	if s.UUID != guid.String() {
		t.Errorf("bad child UUID %q", s.UUID)
	}
	if s.Label != `instance` {
		t.Errorf("bad child label %q", s.Label)
	}
	if s.Version != `1.2.3` {
		t.Errorf("bad child version %q", s.Version)
	}
	if s.Entries != 12 || s.Size != 480 {
		t.Errorf("child counters did not come from the runtime: %d entries %d bytes", s.Entries, s.Size)
	}
	if len(s.Tags) != 2 {
		t.Errorf("child tags did not come from the runtime: %v", s.Tags)
	}
	if s.Uptime <= 0 {
		t.Errorf("running ingester reported no uptime")
	}

	if err = nr.Close(); err != nil {
		t.Fatal(err)
	}
	if s = nr.ChildState(); s.Uptime != 0 {
		t.Fatalf("closed ingester reported uptime %v", s.Uptime)
	}
}

// stateRuntime is a testRuntime that also tracks ingest counters, the way NativeRuntime does
type stateRuntime struct {
	*testRuntime
	state State
}

func (sr *stateRuntime) State() State { return sr.state }

// TestNativeRunnerChildStateStatus covers the transient error and warning conditions riding
// along with the child state.  A healthy ingester carries no status block at all.
func TestNativeRunnerChildStateStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &stateRuntime{testRuntime: newTestRuntime(ctx, cancel)}

	nr, err := NewNativeRunner("test.ingesters.gravwell.io", "instance", "1.0.0", uuid.New(),
		&struct{}{}, ingesterFunc(func(context.Context, Runtime) error { return nil }), rt)
	if err != nil {
		t.Fatal(err)
	}

	if s := nr.ChildState(); s.Metadata != nil {
		t.Fatalf("healthy ingester reported a status block: %s", s.Metadata)
	}

	rt.state = State{Error: errors.New("authentication rejected"), Warn: "degraded"}
	var status ChildStatus
	if err = json.Unmarshal(nr.ChildState().Metadata, &status); err != nil {
		t.Fatal(err)
	}
	if status.Error != `authentication rejected` {
		t.Errorf("bad error in child status %q", status.Error)
	}
	if status.Warn != `degraded` {
		t.Errorf("bad warning in child status %q", status.Warn)
	}

	// clearing the condition drops the block again
	rt.state = State{}
	if s := nr.ChildState(); s.Metadata != nil {
		t.Fatalf("cleared status still reported: %s", s.Metadata)
	}
}

// TestNativeRunnerChildStateConfig covers the config opt in.  A config that does not implement
// ConfigSanitizer is never reported, we do not trust a plugin to have scrubbed its own secrets.
func TestNativeRunnerChildStateConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ig := ingesterFunc(func(context.Context, Runtime) error { return nil })

	// a config that has not opted in
	nr, err := NewNativeRunner("test.ingesters.gravwell.io", "instance", "1.0.0", uuid.New(),
		&secretConfig{Token: `super-secret`}, ig, newTestRuntime(ctx, cancel))
	if err != nil {
		t.Fatal(err)
	}
	if s := nr.ChildState(); s.Configuration != nil {
		t.Fatalf("a config that did not opt in was reported: %s", s.Configuration)
	}

	// the same config, opted in and scrubbed
	if nr, err = NewNativeRunner("test.ingesters.gravwell.io", "instance", "1.0.0", uuid.New(),
		&sanitizedConfig{secretConfig{Token: `super-secret`, Endpoint: `https://example.com`}},
		ig, newTestRuntime(ctx, cancel)); err != nil {
		t.Fatal(err)
	}
	cfg := nr.ChildState().Configuration
	if cfg == nil {
		t.Fatal("an opted in config was not reported")
	}
	if bytes.Contains(cfg, []byte(`super-secret`)) {
		t.Fatalf("the sanitized config leaked a secret: %s", cfg)
	}
	if !bytes.Contains(cfg, []byte(`example.com`)) {
		t.Fatalf("the sanitized config dropped the endpoint: %s", cfg)
	}
}

type secretConfig struct {
	Token    string
	Endpoint string
}

func (sc *secretConfig) Equal(any) bool { return false }

type sanitizedConfig struct {
	secretConfig
}

func (sc *sanitizedConfig) SanitizedConfig() any {
	return struct{ Endpoint string }{Endpoint: sc.Endpoint}
}
