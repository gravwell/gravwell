package hosted

import (
	"path/filepath"
	"testing"
	"time"

	"gravwell/e2e"

	"github.com/docker/docker/api/types/build"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	mockDockerfile = tc.FromDockerfile{
		Dockerfile: "Dockerfile",
		Repo:       "mimecast-mock",
		BuildOptionsModifier: func(options *build.ImageBuildOptions) {
			options.Version = build.BuilderBuildKit
		},
	}
)

func TestMimecast(t *testing.T) {
	mockDockerfile.Context = filepath.Join(e2e.RepoRoot(), "tools/mock/mimecast")

	mock, err := tc.Run(t.Context(), "",
		e2e.WithDefaults(t, "mimecast-mock",
			tc.WithDockerfile(mockDockerfile),
			tc.WithWaitStrategy(wait.ForLog("starting server")),
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}

	fetcher, err := tc.Run(t.Context(), "",
		e2e.Ingester(t, "hosted-mimecast", "hosted/runner",
			e2e.WithConfig(t, "testdata/mimecast.conf", "hosted_ingester.conf", e2e.DefaultConfig),
		)...,
	)
	t.Cleanup(func() {
		e2e.SaveTestFiles(t, fetcher, e2e.Log, []string{
			"/opt/gravwell/log/hosted_ingesters.log",
			"/opt/gravwell/log/error.log",
		})
		e2e.Terminate(t, fetcher)
		e2e.Terminate(t, mock)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}

	time.Sleep(5 * time.Second)

	c := e2e.GetClient(t)
	// run for the artifact, help debugging
	_ = e2e.RunSearch(t, c, "tag=gravwell syslog Appname==mimecast", time.Hour)
	ent := e2e.RunSearch(t, c, "tag=mimecast-audit", time.Hour)

	if len(ent) == 0 {
		t.Fatal("No entries found")
	}
}
