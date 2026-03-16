package hosted

import (
	"testing"
	"time"

	. "github.com/gravwell/gravwell/v3/ingesters/test/e2e"
	tc "github.com/testcontainers/testcontainers-go"
)

var (
	mockDockerfile = tc.FromDockerfile{
		Context:    "../../../..",
		Dockerfile: "tools/mock/mimecast/Dockerfile",
	}
)

func TestMimecast(t *testing.T) {
	mock, err := tc.Run(t.Context(), "",
		WithDefaults(t, "mimecast-mock",
			tc.WithDockerfile(mockDockerfile),
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}

	fetcher, err := tc.Run(t.Context(), "",
		Ingester(t, "hosted-mimecast", "hosted/runner",
			WithConfig(t, "testdata/mimecast.conf", "hosted_ingester.conf", DefaultConfig),
		)...,
	)
	t.Cleanup(func() {
		SaveTestFiles(t, fetcher, Log, []string{
			"/opt/gravwell/log/hosted_ingesters.log",
			"/opt/gravwell/log/error.log",
		})
		Terminate(t, fetcher)
		Terminate(t, mock)
	})
	if err != nil {
		Fatal(t, err)
	}

	time.Sleep(20 * time.Second)

	c := GetClient(t)
	// run for the artifact, help debugging
	_ = RunSearch(t, c, "tag=gravwell syslog Appname==mimecast", time.Hour)
	ent := RunSearch(t, c, "tag=mimecast-audit", time.Hour)

	if len(ent) == 0 {
		t.Fatal("No entries found")
	}
}
