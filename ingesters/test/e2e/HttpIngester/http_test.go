package HttpIngester

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/test/e2e"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestHttp(t *testing.T) {
	ingester, err := tc.Run(t.Context(), "",
		e2e.Ingester(t, "http", "HttpIngester",
			e2e.WithConfig(t, "testdata/http.conf", "gravwell_http_ingester.conf", e2e.DefaultConfig),
			tc.WithExposedPorts("80/tcp"),
			tc.WithWaitStrategy(wait.NewHTTPStrategy("/health/check")),
		)...,
	)
	if err != nil {
		e2e.Fatal(t, err)
	}

	t.Cleanup(func() {
		e2e.SaveTestFiles(t, ingester, e2e.Log, []string{
			"/opt/gravwell/log/http_ingester.log",
		})
		_ = ingester.Terminate(t.Context())
	})

	endpoint, err := ingester.PortEndpoint(t.Context(), "80", "http")
	if err != nil {
		t.Fatal(err)
	}
	data := `{"data": "passed"}`
	resp, err := http.Post(endpoint+"/ingest", "application/json", strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got bad http status %d", resp.StatusCode)
	}

	time.Sleep(5 * time.Second)
	c := e2e.GetClient(t)
	ent := e2e.RunSearch(t, c, "tag=http", time.Minute)
	if len(ent) != 1 {
		e2e.Fatalf(t, "got %d entries, want 1", len(ent))
	}
	if string(ent[0].Data) != data {
		e2e.Fatalf(t, "got %s, want %s", string(ent[0].Data), data)
	}
}
