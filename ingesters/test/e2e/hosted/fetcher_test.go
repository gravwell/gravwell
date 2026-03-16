package hosted

import (
	"testing"
	"time"

	. "github.com/gravwell/gravwell/v3/ingesters/test/e2e"
	tc "github.com/testcontainers/testcontainers-go"
)

func TestTesterPlugin(t *testing.T) {
	fetcher, err := tc.Run(t.Context(), "",
		Ingester(t, "hosted-tester", "hosted/runner",
			WithConfig(t, "testdata/tester.conf", "hosted_ingester.conf", DefaultConfig),
		)...,
	)
	t.Cleanup(func() {
		SaveTestFiles(t, fetcher, Log, []string{
			"/opt/gravwell/log/hosted_ingesters.log",
		})
		Terminate(t, fetcher)
	})
	if err != nil {
		Fatal(t, err)
	}

	time.Sleep(10 * time.Second)

	c := GetClient(t)
	ent := RunSearch(t, c, "tag=test", time.Hour)

	if len(ent) == 0 {
		t.Fatal("No entries found")
	}
}
