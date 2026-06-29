package hosted

import (
	"bufio"
	"strings"
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
				35*time.Second,
				wait.ForLog("Successfully connected to ingesters").WithPollInterval(time.Second),
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

	c := e2e.GetClient(t)
	ent := e2e.WaitForEntries(t, c, "tag=test", time.Hour, 1, 30*time.Second)

	if len(ent) == 0 {
		t.Fatal("No entries found")
	}

	t.Run("Check Logs", func(t *testing.T) {
		r, err := fetcher.CopyFileFromContainer(t.Context(), "/opt/gravwell/log/hosted_runner.log")
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "testing errors") {
				return
			}
		}
		t.Fatal("failed to find testing errors in log file")
	})
}
