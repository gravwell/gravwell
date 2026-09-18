# Hosted Ingester (AKA "Fetchers")

## The "What"

The `hosted` package is a runtime framework for running **poll-based** Gravwell ingesters. These ingesters periodically fetch data from third-party APIs rather than receive it over a persistent connection. Internally, these are also sometimes referred to as **fetchers**. This package ships as a single binary, `gravwell_hosted_runner`, which can run multiple plugins simultaneously while maintaining a single connection to a Gravwell indexer.

## The "Why"

Hosted ingesters go out across the Internet and ask an API on a schedule for data, handle pagination, ensure it survives restarts without losing its position, and deal with rate limits. This framework abstracts all of that plumbing so individual plugins only have to implement the actual logic it takes to make the actual API call.

## Package Overview

- [`hosted`](https://github.com/gravwell/gravwell/tree/main/hosted): the core layer
- [`hosted/storage`](https://github.com/gravwell/gravwell/tree/main/hosted/storage): BoltDB-backed implementation of the [`Storage`](https://github.com/gravwell/gravwell/blob/main/hosted/interface.go) interface.
- [`hosted/plugins`](https://github.com/gravwell/gravwell/tree/main/hosted/plugins): the registration layer
- `hosted/plugins/<name>`: the actual plugin implementation (e.g. `okta`, `mimecast`, `msgraph`, `sqs`, `tester`)
- [`hosted/runner`](https://github.com/gravwell/gravwell/tree/main/hosted/runner): the `main` package for the `gravwell_hosted_runner` binary. Reads the config, opens the BoltDB state, builds a [`NativeRuntime`](https://github.com/gravwell/gravwell/blob/main/hosted/native.go) for each plugin, creates [`NativeRunner`](https://github.com/gravwell/gravwell/blob/main/hosted/native.go) wrappers, and starts up everything. On `SIGINT`/`SIGQUIT`/`SIGTERM` it shuts down cleanly: closing each runner, closing the BoltDB state handler (which syncs it to disk), then syncing and closing the ingest muxer.

### Runtime Connection

```
   hosted_runner.conf
          │
          ▼
     runner/config.go        ← gcfg loads [Global], [State], [Mimecast "x"], [Okta "y"], [SQS "z"], etc.
          │
          ▼
     plugins/config.go       ← Configs.Builders() iterates every configured plugin instance
          │
          ▼
     plugins/builder.go      ← MyBuilder.Build() constructs the plugin, wraps Job → Ingester
          │
          ▼
     hosted/native.go        ← NativeRunner wraps the Ingester, NativeRuntime provides Storage/Logger/Writer
          │
          ▼
     plugins/<name>/         ← Handle() fetches one page, returns Continuation
          │
          ▼
     hosted/job.go           ← jobIngesterAdapter loop: Handle → Sync (if built with WrapJobWithSync) → Sleep(Delay) → repeat
          │
          ▼
     ingest muxer + BoltDB   ← entries go to indexer, state persists to disk
```

## Adding a New Plugin

In order to create a new plugin, follow these steps:

1. Create `hosted/plugins/<name>`

- `config.go` Defines the plugin's `Config`, embeds `hosted.BaseConfig` (and `MultiTagConfig` or `PollingConfig` as needed), implements `Verify()`. If the plugin emits to multiple/configurable tags via `MultiTagConfig`, also implement a `Tags() []string` method (see `mimecast`, `msgraph`, `sqs`); plugins with a single fixed tag can instead expose a package-level `Tags`/`Tag` var/const (see `okta`, `tester`)
- `<name>.go` Defines the `Job` (or `Ingester` if you need concurrent internal streams). Export `Name`, `ID`, and `Version` constants

2. Register it in `hosted/plugins/config.go` and map the config field into [`Configs`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go) like so:

```go
type Configs struct {
  // existing config fields...
  MyPlugin map[string]*myplugin.Config
}
```

Add: 

- validation into [`Verify()`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go)
- tag collection in [`Tags()`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go)
- count in [`IngesterCount()`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go)
- a loop in [`Builders`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go)

3. Add a `Builder` in [`hosted/plugins/builder.go`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/builder.go). `Build` must satisfy the `IngesterBuilder` interface, which takes a `TagNegotiator` and a `syncFn` that flushes state after each successful `Handle`:

```go
type MyPluginBuilder struct {
  Builder[*myplugin.Config]
}

func (b *MyPluginBuilder) Build(tn hosted.TagNegotiator, syncFn func() error) (hosted.Ingester, error) {
  return hosted.WrapJobWithSync(myplugin.New(b.config), syncFn), nil
}

func NewMyPluginBuilder(config *myplugin.Config, kind, id, version string) *MyPluginBuilder { ... }
```

If your plugin has no meaningful state to force-sync after each `Handle`, discard the second parameter and use `hosted.WrapJob` instead, as `SQSBuilder` does:

```go
func (b *MyPluginBuilder) Build(tn hosted.TagNegotiator, _ func() error) (hosted.Ingester, error) {
  return hosted.WrapJob(myplugin.New(b.config)), nil
}
```

4. Add an `example.conf` into your plugin package

See [`hosted/plugins/mimecast/example.conf`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/mimecast/example.conf) as an example.

5. **Write tests!**

`hosted/testutil_test.go` defines an in-memory `testRuntime` used by the core package's own tests, but it's unexported and lives in a `_test.go` file inside `package hosted`, so a plugin package cannot import it directly. Instead, write a small local mock that implements `hosted.Runtime` — see `mockRuntime` in [`hosted/plugins/sqs/sqs_test.go`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/sqs/sqs_test.go) or [`hosted/plugins/msgraph/msgraph_test.go`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/msgraph/msgraph_test.go) as a model to copy. Call `Handle()` (or `Run()`) directly against it, and assert on written entries and stored state.

## Design Considerations

- `Handle` is called **sequentially per plugin**. The adapter calls it, sleeps, and then calls it again.
  - If you need concurrent internal streams, either handle the fan out logic inside of `Handle` itself (like the `mimecast` plugin does with `errgroup`) or keep `Ingester.Run` (like `okta`). 
  - **Do not block `Handle` indefinitely.**
- State lives in BoltDB per plugin instance
  - Keyed by `kind/name/<ingester UUID>` (see `createNativeRuntime` in `hosted/runner/manager.go`)
  - The UUID in your config (`Ingester-UUID`) is what separates two different instances of the same plugin type.
  - If a plugin instance has no `Ingester-UUID` configured, `Config.Verify()` applies a fixed, plugin-specific default UUID (the `defaultIngesterUUIDStr` constant in that plugin's `config.go`). That default is stable across restarts, so state is **not** lost — but it also means only one un-configured instance of a given plugin type can run at a time; a second one will collide with the first and be skipped at startup.
- `Sync()` semantics depend on how your `Builder.Build` wraps the plugin:
  - `hosted.WrapJobWithSync` (used by `mimecast`, `tester`) calls your `syncFn` (typically `BucketWriter.Sync`) after every successful `Handle()`. This narrows the at-least-once duplicate window on restart to whatever is still sitting in the muxer's in-memory channel. **Don't skip it if you use this pattern.**
  - `hosted.WrapJob` (used by `sqs`) runs the same `Handle`/sleep loop but never calls a sync function; state is only as durable as the underlying BoltDB writes.
  - Plugins that implement `hosted.Ingester` directly instead of `hosted.Job` (`okta`, `msgraph`) bypass the adapter entirely and are responsible for their own state syncing inside `Run`.
- `rt.Alive()` returns false when the ingest muxer is backed up and writes will block
  - The adapter **already checks this** before calling `Handle()` — if the connection is unhealthy it backs off for `JobAliveDelay` (10s) and retries. Plugins do not need to check `rt.Alive()` themselves.
