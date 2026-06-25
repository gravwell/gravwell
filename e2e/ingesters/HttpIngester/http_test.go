package HttpIngester

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"
)

func TestHttp(t *testing.T) {
	_, endpoint := setup(t, "http")
	data := `{"data": "passed"}`
	resp, err := http.Post(endpoint+"/ingest", "application/json", strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got bad http status %d", resp.StatusCode)
	}

	c := e2e.GetClient(t)
	ent := e2e.WaitForEntries(t, c, "tag=http", time.Minute, 1, 30*time.Second)
	if len(ent) != 1 {
		e2e.Fatalf(t, "got %d entries, want 1", len(ent))
	}
	if string(ent[0].Data) != data {
		e2e.Fatalf(t, "got %s, want %s", string(ent[0].Data), data)
	}
}
