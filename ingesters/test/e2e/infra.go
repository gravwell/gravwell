package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	Version  = flag.String("version", "latest", "gravwell version to test against, must be a tag of gravwell/gravwell")
	License  = flag.String("license", "/opt/gravwell/etc/license", "path to license file to mount into container")
	platform = flag.String("instance-platform", "linux/amd64", "platform to use for built containers")
)

var net *tc.DockerNetwork
var instance *tc.DockerContainer

var mtx sync.RWMutex
var started bool

func EnsureGravwell(ctx context.Context) (err error) {
	mtx.Lock()
	defer mtx.Unlock()
	if started {
		return
	}
	if !flag.Parsed() {
		flag.Parse()
	}

	net, err = network.New(ctx)
	if err != nil {
		return err
	}

	image := "gravwell/gravwell:latest"
	fmt.Printf("Starting gravwell instance with image: %s\n", image)
	instance, err = tc.Run(
		context.Background(),
		"gravwell/gravwell:"+*Version,
		tc.WithReuseByName("gravwell-e2e"),
		network.WithNetwork([]string{"gravwell"}, net),
		tc.WithExposedPorts("80/tcp", "4023/tcp"),
		tc.WithImagePlatform(*platform),
		tc.WithFiles(
			tc.ContainerFile{
				HostFilePath:      *License,
				ContainerFilePath: "/opt/gravwell/etc/license",
				FileMode:          0o644,
			},
		),
		tc.WithEnv(map[string]string{
			"GRAVWELL_INGEST_AUTH":   DefaultConfig.Ingest_Secret,
			"GRAVWELL_INGEST_SECRET": DefaultConfig.Ingest_Secret,
			"DISABLE_simple_relay":   "TRUE",
		}),
		tc.WithWaitStrategyAndDeadline(
			5*time.Second,
			wait.ForListeningPort("80/tcp"),
			wait.ForListeningPort("4023/tcp"),
		),
	)
	if err != nil {
		return err
	}
	return nil
}

func Cleanup() {
	mtx.Lock()
	defer mtx.Unlock()
	contents, err := files(context.Background(), instance, []string{
		"/opt/gravwell/etc/gravwell.conf",
		"/opt/gravwell/log/info.log",
		"/opt/gravwell/log/warn.log",
		"/opt/gravwell/log/error.log",
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	for p, file := range contents {
		fmt.Println(p)
		fmt.Println(string(file))
	}

	fmt.Println("Tearing down gravwell instance")
	err = instance.Terminate(context.Background())
	if err != nil {
		fmt.Println(err)
	}
}

func Network() *tc.DockerNetwork {
	mtx.RLock()
	defer mtx.RUnlock()
	return net
}

func Platform() string {
	mtx.RLock()
	defer mtx.RUnlock()
	return *platform
}
