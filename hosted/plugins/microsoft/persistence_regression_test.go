package microsoft

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted/storage"
)

// The writer is a fixture; checkpoint storage and the production Builder are
// real. Backend delivery acceptance is a separate live gate.
type persistentTestRuntime struct {
	*microsoftTestRuntime
	bucket *storage.BucketWriter
}

func (r *persistentTestRuntime) Get(key string) ([]byte, error)                   { return r.bucket.Get(key) }
func (r *persistentTestRuntime) Put(key string, value []byte) error               { return r.bucket.Put(key, value) }
func (r *persistentTestRuntime) SyncContext(context.Context, time.Duration) error { return nil }

func TestProductionBuilderDedupSurvivesBoltRestart(t *testing.T) {
	server := microsoftTestServer()
	defer server.Close()
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	path := filepath.Join(t.TempDir(), "hosted_runner.state")
	const identity = "550e8400-e29b-41d4-a716-446655440000"
	for run := 0; run < 2; run++ {
		db, err := storage.OpenBoltHandler(path, true)
		if err != nil {
			t.Fatal(err)
		}
		bucket, err := db.GetBucketWriter(identity)
		if err != nil {
			db.Close()
			t.Fatal(err)
		}
		runtime := &persistentTestRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(), bucket: bucket}
		runtime.stopOnSleep = true
		conf := testClientConfig(t, server.URL)
		conf.Api = []string{"entra-directory-audits"}
		conf.Ingester_UUID = identity
		if run == 1 {
			conf.Lookback = 168
		}
		if err := conf.Verify(); err != nil {
			db.Close()
			t.Fatal(err)
		}
		ingester, err := NewBuilder("restart-regression", conf).Build(runtime, func() error { return nil })
		if err != nil {
			db.Close()
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = ingester.Run(ctx, runtime)
		cancel()
		closeErr := db.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		want := 1
		if run == 1 {
			want = 0
		}
		if len(runtime.entries) != want {
			t.Fatalf("run %d: entries=%d, want=%d", run, len(runtime.entries), want)
		}
		if len(runtime.errorLogs) != 0 {
			t.Fatalf("run %d had unexpected errors: %v", run, runtime.errorLogs)
		}
	}
}
