package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	version = flag.String("version", "5.8.10", "gravwell version to test against, must be a tag of gravwell/gravwell")
	license = flag.String("license", "/opt/gravwell/etc/license", "path to license file to mount into container")
)

var gravwell *testcontainers.DockerContainer

func TestMain(m *testing.M) {
	ctx := context.Background()
	flag.Parse()
	// init gravwell instance
	var err error

	testnet, err := network.New(ctx)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	image := "gravwell/gravwell:" + *version
	fmt.Printf("Starting gravwell instance with image: %s\n", image)
	gravwell, err = testcontainers.Run(
		context.Background(),
		"gravwell/gravwell:"+*version,
		testcontainers.WithName("gravwell-e2e"),
		network.WithNetwork([]string{"gravnet", "testnet"}, testnet),
		testcontainers.WithExposedPorts("80/tcp", "4023/tcp"),
		testcontainers.WithImagePlatform("linux/amd64"),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{
				HostFilePath:      *license,
				ContainerFilePath: "/opt/gravwell/etc/license",
				FileMode:          0o644,
			}),
		testcontainers.WithWaitStrategyAndDeadline(
			20*time.Second,
			wait.ForListeningPort("80/tcp"),
			wait.ForListeningPort("4023/tcp"),
		),
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// confirm it's up

	// run tests
	result := m.Run()
	// clean up

	file, err := gravwell.CopyFileFromContainer(context.Background(), "/opt/gravwell/log/error.log")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else {
		fmt.Println(file)
	}
	os.Exit(result)
}
