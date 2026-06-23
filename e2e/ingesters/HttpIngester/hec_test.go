package HttpIngester

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"

	"github.com/gravwell/gravwell/v3/client/types"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestHec(t *testing.T) {
	_, endpoint := setup(t, "hec")

	t.Run("event ingest", func(t *testing.T) {
		data := `{"event": "hec blah"}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=hec-testing words -e DATA hec blah", time.Minute, 1, 30*time.Second), 1, "hec blah")
	})

	t.Run("raw ingest", func(t *testing.T) {
		data := `raw hec`
		SendHecRaw(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=hec-testing words -e DATA raw hec", time.Minute, 1, 30*time.Second), 1, data)
	})

	t.Run("debug posts", func(t *testing.T) {
		data := `{"event": "hec blah blah"}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		ent := e2e.WaitForEntries(t, c, fmt.Sprintf(`tag=gravwell syslog Appname==httpingester Message=="HEC request" bytes==%d`, len(data)), time.Minute, 1, 30*time.Second)
		if len(ent) != 1 {
			e2e.Fatalf(t, "got %d entries, want 1", len(ent))
		}
	})

	t.Run("implicit evs", func(t *testing.T) {
		data := `{"event": "hec ev", "host": "test-host", "source": "test-source", "fields": {"custom-field": "field-value"}}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		ents := e2e.WaitForEntries(t, c, "tag=hec-testing intrinsic host==test-host", time.Minute, 1, 30*time.Second)
		assert(t, ents, 1, "hec ev")
		assertEV(t, ents[0], "source", "test-source")
		assertEV(t, ents[0], "custom-field", "field-value")
	})
}

func assert(t *testing.T, ent []types.StringTagEntry, count int, data string) {
	t.Helper()
	if len(ent) != count {
		e2e.Fatalf(t, "got %d entries, want %d", len(ent), count)
	}
	if string(ent[0].Data) != data {
		e2e.Fatalf(t, "got %q, want %q", string(ent[0].Data), data)
	}
}

func assertEV(t *testing.T, ent types.StringTagEntry, key string, value string) {
	t.Helper()
	ev, ok := ent.GetEnumerated(key)
	if !ok {
		e2e.Fatalf(t, "got no ev, want %q", key)
	}
	if ev != value {
		e2e.Fatalf(t, "for %q, got %q, want %q", key, ev, value)
	}
}

func TestHecNoDebug(t *testing.T) {
	_, endpoint := setup(t, "hec-no-debug",
		// we want to run without -v to ensure basics work
		tc.WithCmd("/opt/gravwell/bin/HttpIngester"),
		// since we run without -v we need to remove the wait strategy that relies on the verbose output
		tc.WithWaitStrategyAndDeadline(10*time.Second, wait.NewHTTPStrategy("/health/check").WithPollInterval(time.Second)),
	)

	t.Run("event ingest", func(t *testing.T) {
		data := `{"event": "hec no debug"}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		ent := e2e.WaitForEntries(t, c, "tag=hec-no-debug words -e DATA hec no debug", time.Minute, 1, 30*time.Second)
		if len(ent) != 1 {
			e2e.Fatalf(t, "got %d entries, want 1", len(ent))
		}
		if string(ent[0].Data) != "hec no debug" {
			e2e.Fatalf(t, "got %s, want %s", string(ent[0].Data), "hec no debug")
		}
	})

	t.Run("raw ingest", func(t *testing.T) {
		data := `raw hec no debug`
		SendHecRaw(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		ent := e2e.WaitForEntries(t, c, "tag=hec-no-debug words -e DATA raw hec no debug", time.Minute, 1, 30*time.Second)
		if len(ent) != 1 {
			e2e.Fatalf(t, "got %d entries, want 1", len(ent))
		}
		if string(ent[0].Data) != data {
			e2e.Fatalf(t, "got %s, want %s", string(ent[0].Data), data)
		}
	})
}

func SendHecEvent(t *testing.T, endpoint string, data io.Reader) {
	send(t, endpoint+"/services/collector/event", data)
}

func SendHecRaw(t *testing.T, endpoint string, data io.Reader) {
	send(t, endpoint+"/services/collector/raw", data)
}

func send(t *testing.T, endpoint string, data io.Reader) {
	req, err := http.NewRequest("POST", endpoint, data)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Splunk token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got bad http status %d", resp.StatusCode)
	}
}
