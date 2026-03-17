package hosted

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/build"
	. "github.com/gravwell/gravwell/v3/ingesters/test/e2e"
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
	mockDockerfile.Context = filepath.Join(RepoRoot(), "tools/mock/mimecast")

	mock, err := tc.Run(t.Context(), "",
		WithDefaults(t, "mimecast-mock",
			tc.WithDockerfile(mockDockerfile),
			tc.WithWaitStrategy(wait.ForLog("starting server")),
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

	time.Sleep(5 * time.Second)

	c := GetClient(t)
	// run for the artifact, help debugging
	_ = RunSearch(t, c, "tag=gravwell syslog Appname==mimecast", time.Hour)
	ent := RunSearch(t, c, "tag=mimecast-audit", time.Hour)

	if len(ent) == 0 {
		t.Fatal("No entries found")
	}
}
