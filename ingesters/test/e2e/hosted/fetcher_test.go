package hosted

import (
	"testing"
	"time"

	. "github.com/gravwell/gravwell/v3/ingesters/test/e2e"
	tc "github.com/testcontainers/testcontainers-go"
)

func TestTesterPlugin(t *testing.T) {
	fetcher, err := tc.Run(t.Context(), "",
		WithDefaults(t, "hosted-tester",
			tc.WithDockerfile(dockerfile),
			WithConfig(t, "testdata/tester.conf", "hosted_ingester_runner.conf", DefaultConfig),
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		SaveTestFiles(t, fetcher, Log, []string{
			"/opt/gravwell/log/hosted_ingesters.log",
		})
		_ = fetcher.Terminate(t.Context())
	})

	time.Sleep(5 * time.Second)

	c := GetClient(t)
	ent := RunSearch(t, c, "tag=test", time.Hour)

	if len(ent) == 0 {
		t.Fatal("No entries found")
	}
}
