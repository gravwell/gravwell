package hosted

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"gravwell/e2e"

	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const mockImage = "mimecast-mock:e2e"

func TestMimecast(t *testing.T) {
	// The mock's Dockerfile needs BuildKit, and testcontainers cannot build with BuildKit
	// on Docker Desktop (testcontainers/testcontainers-go#3951), so the docker CLI builds it.
	if err := e2e.Build(mockImage, "./tools/mock/mimecast/Dockerfile", "."); err != nil {
		t.Fatal(err)
	}
	mock, err := tc.Run(t.Context(), mockImage,
		e2e.WithDefaults(t, "mimecast-mock",
			tc.WithExposedPorts("8080/tcp"),
			tc.WithWaitStrategy(wait.ForLog("starting server")),
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := mock.PortEndpoint(t.Context(), "8080/tcp", "http")
	pageConfig := map[string]int{
		"num_pages":       5,
		"events_per_page": 20,
	}
	body, _ := json.Marshal(map[string]any{
		"client_id": "mta",
		"config": map[string]any{
			"siem":       pageConfig,
			"siem_batch": pageConfig,
		},
	})
	_, err = http.Post(endpoint+"/config", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err)
	}

	fetcher, err := tc.Run(t.Context(), "gravwell/hosted:e2e",
		e2e.WithDefaults(t, "hosted-mimecast",
			tc.WithWaitStrategyAndDeadline(
				35*time.Second,
				wait.ForLog("Successfully connected to ingesters").WithPollInterval(time.Second),
			),
			e2e.WithConfig(t, "testdata/mimecast.conf", "hosted_runner.conf", e2e.DefaultConfig),
		)...,
	)
	t.Cleanup(func() {
		e2e.SaveTestFiles(t, fetcher, e2e.Log, []string{
			"/opt/gravwell/log/hosted_runner.log",
			"/opt/gravwell/log/error.log",
		})
		e2e.Terminate(t, fetcher)
		e2e.Terminate(t, mock)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}

	c := e2e.GetClient(t)
	if ent := e2e.WaitForEntries(t, c, "tag=mimecast-audit", time.Hour*24, 1, 30*time.Second); len(ent) == 0 {
		e2e.Fatal(t, "no audit entries found")
	}

	if ent := e2e.WaitForEntries(t, c, "tag=mimecast-mta-delivery", time.Hour*24, 100, 60*time.Second); len(ent) < 100 {
		e2e.Fatalf(t, "got %d entries, less than expected 100 mta entries ", len(ent))
	}

	errors := e2e.RunSearch(t, c, "tag=gravwell syslog Appname==mimecast Severity<=3", time.Hour)
	if len(errors) > 0 {
		e2e.Fatal(t, "found errors:", errors)
	}
}
