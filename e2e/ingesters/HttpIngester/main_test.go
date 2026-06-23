package HttpIngester

import (
	"fmt"
	"gravwell/e2e"
	"testing"
	"time"

	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMain(m *testing.M) {
	e2e.Start()

	m.Run()

	e2e.Cleanup()
}

func setup(t *testing.T, name string, extras ...tc.ContainerCustomizer) (*tc.DockerContainer, string) {
	cc := append([]tc.ContainerCustomizer{
		e2e.WithConfig(t, fmt.Sprintf("testdata/%s.conf", name), "gravwell_http_ingester.conf", e2e.DefaultConfig),
		tc.WithExposedPorts("80/tcp"),
		tc.WithAdditionalWaitStrategyAndDeadline(10*time.Second, wait.NewHTTPStrategy("/health/check").WithPollInterval(time.Second)),
	}, extras...)
	ingester, err := tc.Run(t.Context(), "",
		e2e.Ingester(t, name, "HttpIngester", cc...)...,
	)
	t.Cleanup(func() {
		e2e.SaveTestFiles(t, ingester, e2e.Log, []string{
			"/opt/gravwell/log/http_ingester.log",
		})
		e2e.Terminate(t, ingester)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}

	endpoint, err := ingester.PortEndpoint(t.Context(), "80", "http")
	if err != nil {
		t.Fatal(err)
	}

	return ingester, endpoint
}
