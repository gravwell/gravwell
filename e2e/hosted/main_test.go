package hosted

import (
	"bytes"
	"fmt"
	"os/exec"
	"testing"

	"gravwell/e2e"
)

func TestMain(m *testing.M) {
	e2e.Start()
	buildHosted()
	m.Run()

	e2e.Cleanup()
}

func buildHosted() {
	var stdout, stderr bytes.Buffer
	docker := exec.Command("docker", "buildx", "build", "-t", "gravwell/hosted:e2e", "-f", "./e2e/hosted/Dockerfile", "--platform", e2e.IngestPlatform(), ".")
	docker.Dir = e2e.RepoRoot()
	docker.Stdout = &stdout
	docker.Stderr = &stderr
	if err := docker.Run(); err != nil {
		fmt.Println(stderr.String())
		fmt.Println(stdout.String())
		panic(err)
	}
}
