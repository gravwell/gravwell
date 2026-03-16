package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

var DefaultConfig = config.IngestConfig{
	Cleartext_Backend_Target: []string{"gravwell:4023"},
	Ingest_Secret:            "IngestSecrets",
}

// Ingester will add the default list of container configs for consistency and as an easy way to adjust how we handle the various running containers.
// This should be used with discrete ContainerCustomizers as needed for each container.
//
// container, err := tc.Run(t.Context(), "",
//
//		Ingester(t, "ingester",
//			tc.WithDockerfile(dockerfile),
//			WithConfig(t, "testdata/ingester.conf", "ingester.conf", DefaultConfig),
//		)...,
//	)
func Ingester(t *testing.T, name, ingester string, extras ...tc.ContainerCustomizer) []tc.ContainerCustomizer {
	defaults := []tc.ContainerCustomizer{
		tc.WithImage("gravwell/ingesters:e2e"),
		tc.WithEnv(map[string]string{
			"INGESTER": ingester,
		}),
		tc.WithWaitStrategyAndDeadline(
			10*time.Second,
			wait.ForLog("Successfully connected to ingesters"),
		),
	}
	return WithDefaults(t, name, append(defaults, extras...)...)
}

// WithDefaults will add the default list of container configs for consistency and as an easy way to adjust how we handle the various running containers.
// This should be used with discrete ContainerCustomizers as needed for each container.
//
// container, err := tc.Run(t.Context(), "",
//
//		WithDefaults(t, "mock",
//			tc.WithDockerfile(dockerfile),
//			WithConfig(t, "testdata/mock.conf", "mock.conf", DefaultConfig),
//		)...,
//	)
func WithDefaults(t *testing.T, name string, extras ...tc.ContainerCustomizer) []tc.ContainerCustomizer {
	defaults := []tc.ContainerCustomizer{
		tc.WithLogger(log.TestLogger(t)),
		tc.WithName(name),
		tc.WithImagePlatform(IngestPlatform()),
		network.WithNetwork([]string{name}, Network()),
		tc.WithHostPortAccess(80, 4023),
	}
	return append(defaults, extras...)
}

// WithConfig will take a source file parse it as a go template (via text/template) and mount the output to `/opt/gravwell/etc/[target]`
// data is passed to the template unmodified.
func WithConfig(t *testing.T, source, target string, data any) tc.ContainerCustomizer {
	return tc.WithFiles(tc.ContainerFile{
		Reader:            NewConfigReader(t, source, data),
		ContainerFilePath: filepath.Clean("/opt/gravwell/etc/" + target),
		FileMode:          0o644,
	})
}

// Terminate will safely stop a container, useful in a Cleanup call.
func Terminate(t *testing.T, con *tc.DockerContainer) {
	t.Helper()
	if con == nil {
		return
	}
	_ = con.Terminate(context.Background())
}
