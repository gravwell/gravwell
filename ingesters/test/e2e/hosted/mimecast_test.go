package hosted

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
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

	// test containers doesn't pull well with buildkit
	if err := exec.Command("docker", "pull", "golang:latest").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("docker", "pull", "busybox:latest").Run(); err != nil {
		t.Fatal(err)
	}
	mock, err := tc.Run(t.Context(), "",
		e2e.WithDefaults(t, "mimecast-mock",
			tc.WithDockerfile(mockDockerfile),
			tc.WithExposedPorts("8080/tcp"),
			tc.WithWaitStrategy(wait.ForLog("starting server")),
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := mock.PortEndpoint(t.Context(), "8080/tcp", "http")
	body, _ := json.Marshal(map[string]any{
		"mta": map[string]string{
			"num_pages":       "20",
			"events_per_page": "1000",
		},
	})
	_, err = http.Post(endpoint, "application/json", bytes.NewBuffer(body))
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

	time.Sleep(10 * time.Second)

	c := e2e.GetClient(t)
	// run for the artifact, help debugging
	_ = e2e.RunSearch(t, c, "tag=gravwell syslog Appname==mimecast", time.Hour)
	if ent := e2e.RunSearch(t, c, "tag=mimecast-audit", time.Hour*24); len(ent) == 0 {
		e2e.Fatal(t, "no audit entries found")
	}

	if ent := e2e.RunSearch(t, c, "tag=mimecast-mta-delivery", time.Hour*24); len(ent) < 20 {
		e2e.Fatalf(t, "got %d entries, less than 20 mta entries found, expected at least one full page", len(ent))
	}

	errors := e2e.RunSearch(t, c, "tag=gravwell syslog Appname==mimecast Severity<=3", time.Hour)
	if len(errors) > 0 {
		e2e.Fatal(t, "found errors:", errors)
	}
}
