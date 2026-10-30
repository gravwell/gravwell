package microsoft

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestSnapshotSuppressesRepeatedRecordWithinPoll(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"value":[{"id":"synthetic-user","displayName":"Example User","userPrincipalName":"example@example.invalid"},{"id":"synthetic-user","displayName":"Example User","userPrincipalName":"example@example.invalid"}]}`)
	}))
	defer server.Close()
	runtime := newMicrosoftTestRuntime()
	plugin := newMicrosoftTestPlugin(t, server, runtime)
	plugin.conf.Api = []string{"entra-users"}
	if _, err := plugin.Handle(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("duplicate snapshot entries=%d", len(runtime.entries))
	}
}

func TestIngestUnchangedDuplicateOccurrencesAcrossPolls(t *testing.T) {
	for _, unchanged := range []bool{false, true} {
		t.Run(fmt.Sprint(unchanged), func(t *testing.T) {
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"value":[{"id":"same"},{"id":"same"}]}`) })
			rt := newMicrosoftTestRuntime()
			p := newMicrosoftTestPlugin(t, s, rt)
			p.conf.Api = []string{"entra-users"}
			p.conf.Ingest_Unchanged = unchanged
			for poll := 1; poll <= 2; poll++ {
				if _, err := p.Handle(context.Background(), rt); err != nil {
					t.Fatal(err)
				}
				want := 1
				if unchanged {
					want = 2 * poll
				}
				if len(rt.entries) != want {
					t.Fatalf("poll %d entries=%d want %d", poll, len(rt.entries), want)
				}
			}
		})
	}
}

type occurrenceFailureWriter struct {
	*persistentTestRuntime
	calls, failAt int
}

func (w *occurrenceFailureWriter) WriteEntryContext(ctx context.Context, e *entry.Entry) error {
	w.calls++
	if w.calls == w.failAt {
		return errors.New("synthetic occurrence failure")
	}
	return w.persistentTestRuntime.WriteEntryContext(ctx, e)
}

func TestIngestUnchangedOccurrenceReceiptsSurviveRestart(t *testing.T) {
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"value":[{"id":"same"},{"id":"same"}]}`) })
	c := auditConfig(t, s.URL, "entra-users")
	c.Ingest_Unchanged = true
	path := filepath.Join(t.TempDir(), "state")
	for attempt := 0; attempt < 3; attempt++ {
		rt, closeDB := auditRuntime(t, path)
		if attempt == 0 {
			auditRun(t, c, rt, &occurrenceFailureWriter{persistentTestRuntime: rt, failAt: 2})
		} else {
			auditRun(t, c, rt, rt)
		}
		want := 1
		if attempt == 2 {
			want = 2
		}
		if len(rt.entries) != want {
			t.Fatalf("attempt %d writes=%d want=%d", attempt, len(rt.entries), want)
		}
		if attempt == 0 && len(rt.errorLogs) == 0 {
			t.Fatal("second occurrence was never attempted")
		}
		closeDB()
	}
}

func TestIngestUnchangedLegacyReceiptAcknowledgesFirstOccurrence(t *testing.T) {
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"value":[{"id":"same"},{"id":"same"}]}`) })
	rt := newMicrosoftTestRuntime()
	p := newMicrosoftTestPlugin(t, s, rt)
	p.conf.Api = []string{"entra-users"}
	p.conf.Ingest_Unchanged = true
	digest, err := recordDigest([]byte(`{"id":"same"}`), datasets["entra-users"])
	if err != nil {
		t.Fatal(err)
	}
	state := streamState{Records: map[string]recordState{}, Pending: &deliveryProgress{Start: p.now().Add(-time.Hour), End: p.now(), Receipts: map[string]bool{"same:" + digest: true}}}
	if err := saveState(rt, stateKey(p.conf.StateNamespace(), "entra-users", ""), state); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(context.Background(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("legacy receipt replay wrote %d want only second occurrence", len(rt.entries))
	}
}
