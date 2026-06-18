package HttpIngester

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"
)

func TestHec(t *testing.T) {
	_, endpoint := setup(t, "hec")

	t.Run("event ingest", func(t *testing.T) {
		data := `{"event": "hec blah"}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		ent := e2e.WaitForEntries(t, c, "tag=hec-testing", time.Minute, 1, 30*time.Second)
		if len(ent) != 1 {
			e2e.Fatalf(t, "got %d entries, want 1", len(ent))
		}
		if string(ent[0].Data) != "hec blah" {
			e2e.Fatalf(t, "got %s, want %s", string(ent[0].Data), "hec blah")
		}
	})

	t.Run("raw ingest", func(t *testing.T) {
		data := `raw hec`
		SendHecRaw(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		ent := e2e.WaitForEntries(t, c, "tag=hec-testing words -e DATA raw hec", time.Minute, 1, 30*time.Second)
		if len(ent) != 1 {
			e2e.Fatalf(t, "got %d entries, want 1", len(ent))
		}
		if string(ent[0].Data) != data {
			e2e.Fatalf(t, "got %s, want %s", string(ent[0].Data), data)
		}
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
