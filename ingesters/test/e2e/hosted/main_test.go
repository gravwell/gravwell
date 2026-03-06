package hosted

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gravwell/gravwell/v3/ingesters/test/e2e"
	tc "github.com/testcontainers/testcontainers-go"
)

var (
	ingester   = "hosted_ingester_runner"
	source     = "./ingesters/hosted/runner"
	dockerfile = tc.FromDockerfile{
		Context:    "../../../..",
		Dockerfile: "ingesters/test/e2e/Dockerfile",
		BuildArgs: map[string]*string{
			"INGESTER": &ingester,
			"SOURCE":   &source,
		},
	}
)

func TestMain(m *testing.M) {
	err := e2e.EnsureGravwell(context.Background())
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	m.Run()

	e2e.Cleanup()
}
