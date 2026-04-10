package hosted

import (
	"testing"
	"time"

	"gravwell/e2e"

	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestTesterPlugin(t *testing.T) {
	fetcher, err := tc.Run(t.Context(), "gravwell/hosted:e2e",
		e2e.WithDefaults(t, "hosted-tester",
			tc.WithWaitStrategyAndDeadline(
				10*time.Second,
				wait.ForLog("Successfully connected to ingesters"),
			),
			e2e.WithConfig(t, "testdata/tester.conf", "hosted_runner.conf", e2e.DefaultConfig),
		)...,
	)
	t.Cleanup(func() {
		e2e.SaveTestFiles(t, fetcher, e2e.Log, []string{
			"/opt/gravwell/log/hosted_runner.log",
		})
		e2e.Terminate(t, fetcher)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}

	time.Sleep(10 * time.Second)

	c := e2e.GetClient(t)
	ent := e2e.RunSearch(t, c, "tag=test", time.Hour)

	if len(ent) == 0 {
		t.Fatal("No entries found")
	}
}
