# Ingester End-to-End Tests

An end-to-end testing framework for Gravwell ingesters. Tests are written with standard `go test` and use [testcontainers-go](https://golang.testcontainers.org/) to spin up a Gravwell instance and ingester containers in Docker. The framework handles building ingesters, managing the Gravwell instance, networking, config templating, and artifact collection.

## Prerequisites

- **Docker**
- **Go**

No other dependencies are required — everything else runs inside containers.

## Running Tests

From anywhere within the repository:

```sh
# Run all e2e tests
go test ./ingesters/test/e2e/...

# Run a specific test package
go test ./ingesters/test/e2e/HttpIngester/

# Run a single test
go test ./ingesters/test/e2e/HttpIngester/ -run TestHttp

# Preserve artifacts (logs, configs, search results) after the run
go test -artifacts ./ingesters/test/e2e/HttpIngester/
```

### Flags

These flags are passed directly to `go test`:

| Flag | Default | Description |
|---|---|---|
| `-version` | `latest` | Tag of the `gravwell/gravwell` Docker image to test against |
| `-license` | *(empty — runs unlicensed)* | Path to a license file to mount into the Gravwell container |
| `-instance-platform` | `linux/amd64` | Platform for the Gravwell instance container |
| `-ingester-platform` | *(current architecture)* | Platform used to build and run ingester containers |
| `-endpoint` | *(empty — spins up a new instance)* | Gravwell ingest endpoint to use; providing this skips starting a local instance |

Example:

```sh
go test ./ingesters/test/e2e/HttpIngester/ -version v5.7.0 -license ./license.key
```

## Project Layout

```
e2e/
├── ingester/       # matching the name in ../../ingesters
│   ├── main_test.go
│   ├── ingester_test.go
│   └── testdata/
│       └── ingester.conf   # Ingester config (Go template)
└── .../
```

## Scaffolding a New Test

Follow these steps to add e2e tests for an ingester.

### 1. Register the ingester for building

Add your ingester's path (relative to the repo root `ingesters/` directory) to the `include` file:

```
HttpIngester
hosted/runner
MyNewIngester        # add a new line
```

This tells the Dockerfile to `go build` your ingester into the shared image.

### 2. Create the test package directory

```
e2e/
└── MyNewIngester/
    ├── main_test.go
    ├── myingester_test.go
    └── testdata/
        └── myingester.conf
```

### 3. Write `main_test.go`

Every test package **must** have a `TestMain` that calls `e2e.Start()` and `e2e.Cleanup()`:

```go
package MyNewIngester

import (
    "testing"

    "github.com/gravwell/gravwell/v3/ingesters/test/e2e"
)

func TestMain(m *testing.M) {
    e2e.Start()
	// Add custom setup if needed (e.g. start a kafka cluster)
    m.Run()
	// Teardown custom setup
    e2e.Cleanup()
}
```

### 4. Write a config template

Config files are Go [`text/template`](https://pkg.go.dev/text/template) files. The template data is the struct you pass to `WithConfig` — typically `e2e.DefaultConfig` which provides a `config.IngestConfig`

Place the template in `testdata/`. For example, `testdata/myingester.conf`:

```
[Global]
    Ingest-Secret = "{{ .Ingest_Secret }}"
    Connection-Timeout = "{{ .Connection_Timeout }}"
    Cleartext-Backend-Target={{ index .Cleartext_Backend_Target 0 }}
    Log-Level=DEBUG
    Log-File=/opt/gravwell/log/myingester.log

[SomeListener "default"]
    Tag-Name=mytag
```

### 5. Write a test

A typical test starts the ingester, sends data, then queries Gravwell to verify it arrived:

```go
package MyNewIngester

import (
    "testing"
    "time"

    "github.com/gravwell/gravwell/v3/ingesters/test/e2e"
    tc "github.com/testcontainers/testcontainers-go"
)

func TestIngest(t *testing.T) {
    // Start the ingester container
    ingester, err := tc.Run(t.Context(), "",
        e2e.Ingester(t, "myingester", "MyNewIngester",
            e2e.WithConfig(t, "testdata/myingester.conf", "myingester.conf", e2e.DefaultConfig),
            // add tc.WithExposedPorts(), tc.WithWaitStrategy(), etc. as needed
        )...,
    )
    t.Cleanup(func() {
        e2e.SaveTestFiles(t, ingester, e2e.Log, []string{
            "/opt/gravwell/log/myingester.log",
        })
        e2e.Terminate(t, ingester)
    })
	// We check for errors after calling t.Cleanup so startup logs can be captured. Helps debugging.
    if err != nil {
        e2e.Fatal(t, err)
    }

    // ... send data to the ingester ...

    // Allow time for ingestion
    time.Sleep(5 * time.Second)

    // Query Gravwell and assert
    c := e2e.GetClient(t)
    ent := e2e.RunSearch(t, c, "tag=mytag", time.Minute)
    if len(ent) != 1 {
        e2e.Fatalf(t, "got %d entries, want 1", len(ent))
    }
}
```

## Key Helpers

| Function | Purpose |
|---|---|
| `e2e.Ingester(t, name, binary, ...)` | Returns `[]tc.ContainerCustomizer` that configures an ingester container with sensible defaults (image, network, platform, wait strategy). |
| `e2e.WithConfig(t, src, target, data)` | Renders a Go template config and mounts it at `/opt/gravwell/etc/<target>`. |
| `e2e.WithDefaults(t, name, ...)` | Like `Ingester` but without the ingester-specific image/env — useful for auxiliary containers (mocks, etc.). |
| `e2e.GetClient(t)` | Returns an authenticated `*client.Client` connected to the running Gravwell instance. |
| `e2e.RunSearch(t, c, query, duration)` | Executes a search query and returns the entries. Also writes results as artifacts. |
| `e2e.SaveTestFiles(t, container, type, paths)` | Copies files out of a container and saves them as test artifacts. |
| `e2e.Fatal(t, ...)` / `e2e.Fatalf(t, ...)` | Like `t.Fatal` / `t.Fatalf`, but also saves Gravwell instance logs before failing. |
| `e2e.Terminate(t, container)` | Safely stops a container — use in `t.Cleanup`. |

> You can also run `pkgsite -open` locally to view all helper docs

## Best Practices

- **One `TestMain` per package.** Each test package (directory) needs its own `main_test.go` with `e2e.Start()` / `e2e.Cleanup()`. The Gravwell instance is shared across tests within a package.

- **Use `t.Cleanup` for teardown.** Always register cleanup immediately after starting a container. This ensures logs are captured and containers are stopped even when tests fail.

- **Use `e2e.Fatal` / `e2e.Fatalf` instead of `t.Fatal`.** The e2e wrappers save Gravwell instance logs before failing, which makes debugging much easier.

- **Give ingestion time to propagate.** After sending data, add a `time.Sleep` before querying. Ingestion is asynchronous — entries may take a few seconds to become searchable.

- **Keep config templates in `testdata/`.** This is the standard Go convention and keeps test data co-located with the tests that use it.

- **Use unique tag names.** Each test (or listener) should use a distinct tag to avoid cross-contamination between tests running in the same package.

- **Save relevant logs in `t.Cleanup`.** Use `e2e.SaveTestFiles` to pull ingester logs out of the container. When run with `-artifacts`, these files are preserved.

- **Use `e2e.WithDefaults` for non-ingester containers.** If your test needs a mock server or other auxiliary container, use `WithDefaults` instead of `Ingester` to get networking and logging without the ingester-specific setup.
