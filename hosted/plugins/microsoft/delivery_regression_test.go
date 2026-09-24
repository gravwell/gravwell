package microsoft

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type syncFailureWriter struct {
	*microsoftTestRuntime
	calls int
}

func (w *syncFailureWriter) SyncContext(context.Context, time.Duration) error {
	w.calls++
	return errors.New("synthetic sync failure")
}

func TestProductionBuilderSyncFailureRetainsCheckpoint(t *testing.T) {
	server := microsoftTestServer()
	defer server.Close()
	priorTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = priorTransport })
	rt := newMicrosoftTestRuntime()
	rt.stopOnSleep = true
	writer := &syncFailureWriter{microsoftTestRuntime: rt}
	conf := testClientConfig(t, server.URL)
	conf.Api = []string{"entra-directory-audits"}
	conf.Ingester_UUID = "550e8400-e29b-41d4-a716-446655440000"
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	ingester, err := NewBuilder("sync-regression", conf).Build(writer, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ingester.Run(ctx, rt); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 {
		t.Errorf("delivery sync calls=%d", writer.calls)
	}
	if len(rt.values) != 0 {
		t.Fatal("checkpoint persisted despite failed delivery sync")
	}
}
