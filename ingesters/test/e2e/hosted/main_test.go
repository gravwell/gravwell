package hosted

import (
	"os/exec"
	"testing"

	"github.com/gravwell/gravwell/v3/ingesters/test/e2e"
)

func TestMain(m *testing.M) {
	e2e.Start()

	// test containers doesn't pull well with buildkit
	if err := exec.Command("docker", "pull", "golang:latest").Run(); err != nil {
		panic(err)
	}
	if err := exec.Command("docker", "pull", "busybox:latest").Run(); err != nil {
		panic(err)
	}

	m.Run()

	e2e.Cleanup()
}
