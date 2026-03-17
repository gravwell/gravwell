package e2e

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	version        = flag.String("version", "latest", "gravwell version to test against, must be a tag of gravwell/gravwell")
	license        = flag.String("license", "", "path to license file to mount into container")
	platform       = flag.String("instance-platform", "linux/amd64", "platform to use for gravwell instance")
	ingestPlatform = flag.String("ingest-platform", "linux/"+runtime.GOARCH, "platform to use for ingestion containers")
	endpoint       = flag.String("endpoint", "", "gravwell ingest endpoint to use")
)

var net *tc.DockerNetwork
var instance *tc.DockerContainer

var mtx sync.RWMutex
var started bool

func buildIngesters() {
	var stdout, stderr bytes.Buffer
	docker := exec.Command("docker", "buildx", "build", "-t", "gravwell/ingesters:e2e", "-f", "./ingesters/test/e2e/Dockerfile", "--platform", *ingestPlatform, ".")
	docker.Dir = RepoRoot()
	docker.Stdout = &stdout
	docker.Stderr = &stderr
	if err := docker.Run(); err != nil {
		fmt.Println(stderr.String())
		fmt.Println(stdout.String())
		panic(err)
	}
}

func find(signal string) (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		path := filepath.Join(current, signal)
		if _, err := os.Stat(path); err == nil {
			return current, nil // Found the marker file, return this directory
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached the filesystem root without finding the marker
			return "", fmt.Errorf("project root marker '%s' not found", signal)
		}
		current = parent
	}
}

// RepoRoot will find the root path of the repo. Useful when declaring build contexts to avoid relative pathing.
func RepoRoot() string {
	r, err := find(".git")
	if err != nil {
		panic(err)
	}
	return r
}

// Start MUST be called within each package's TestMain before tests are run.
// Multiple concurrent calls are allowed and handled safely.
func Start() {
	ctx := context.Background()
	mtx.Lock()
	defer mtx.Unlock()
	if started {
		return
	}
	if !flag.Parsed() {
		flag.Parse()
	}

	buildIngesters()

	var err error

	net, err = network.New(ctx)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if endpoint != nil && *endpoint != "" {
		DefaultConfig.Cleartext_Backend_Target = []string{*endpoint + ":4023"}
		return
	}

	licenseFile := tc.ContainerFile{
		HostFilePath:      *license,
		ContainerFilePath: "/opt/gravwell/etc/license",
		FileMode:          0o644,
	}

	if license == nil || *license == "" {
		licenseFile.Reader = strings.NewReader("UNLICENSED")
	}

	config := tc.ContainerFile{
		HostFilePath:      RepoRoot() + "/ingesters/test/e2e/testdata/gravwell.conf",
		ContainerFilePath: "/opt/gravwell/etc/gravwell.conf",
		FileMode:          0o644,
	}

	image := "gravwell/gravwell:" + *version
	instance, err = tc.Run(
		ctx,
		image,
		network.WithNetwork([]string{"gravwell"}, net),
		tc.WithExposedPorts("80/tcp"),
		tc.WithImagePlatform(*platform),
		tc.WithFiles(licenseFile, config),
		tc.WithEnv(map[string]string{
			"GRAVWELL_INGEST_AUTH":   DefaultConfig.Ingest_Secret,
			"GRAVWELL_INGEST_SECRET": DefaultConfig.Ingest_Secret,
			"DISABLE_simple_relay":   "TRUE",
		}),
		tc.WithWaitStrategyAndDeadline(
			5*time.Second,
			wait.ForListeningPort("80/tcp"),
		),
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// Cleanup is currently a noop, mostly due to testing.M being quite limited in functionality.
func Cleanup() {}

// Network returns the ephemeral docker network for this test. Used by WithDefaults and Ingester to attach containers to the network.
// If running additional containers they MUST be in this network to communicate with Ingesters and the Gravwell instance.
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

func IngestPlatform() string {
	mtx.RLock()
	defer mtx.RUnlock()
	return *ingestPlatform
}
