# Hosted Ingester (AKA "Fetchers")

## The "What"

The `hosted` package is a runtime framework for running **poll-based** Gravwell ingesters. These ingesters periodically fetch data from third-party APIs rather than receive it over a persistent connection. Internally, these are also sometimes referred to as **fetchers**. This package ships as a single binary, `gravwell_hosted_runner`, which can run multiple plugins simultaneously while maintaining a single connection to a Gravwell indexer.

## The "Why"

Hosted ingesters go out across the Internet and ask an API on a schedule for data, handle pagination, ensure it survives restarts without losing its position, and deal with rate limits. This framework abstracts all of that plumbing so individual plugins only have to implement the actual logic it takes to make the actual API call.

## Package Overview

- [`hosted`](https://github.com/gravwell/gravwell/tree/main/hosted): the core layer
- [`hosted/storage`](https://github.com/gravwell/gravwell/tree/main/hosted/storage): BoltDB-backed implementation of the [`Storage`](https://github.com/gravwell/gravwell/blob/main/hosted/interface.go#L63) interface. 
- [`hosted/plugins`](https://github.com/gravwell/gravwell/blob/main/plugins): the registration layer
- `hosted/plugins/<name>`: the actual plugin implementation
- [`hosted/runner`](https://github.com/gravwell/gravwell/blob/main/hosted/runner): the `main` package for the `gravwell_hosted_runner` binary. Reads the config, opens the BoltDB state, builds a [`NativeRuntime`](https://github.com/gravwell/gravwell/blob/main/hosted/native.go#L218) for each plugin, creates [`NativeRunner`](https://github.com/gravwell/gravwell/blob/main/hosted/native.go#L45) wrappers, and starts up everything. It shuts down cleanly on `SIGTERM`, syncs BoltDB, and flushes the ingest muxer.

### Runtime Connection

```
   hosted_runner.conf
          │
          ▼
     runner/config.go        ← gcfg loads [Global], [State], [Mimecast "x"], [Okta "y"], etc.
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
     hosted/job.go           ← jobIngesterAdapter loop: Handle → Sync → Sleep(Delay) → repeat
          │
          ▼
     ingest muxer + BoltDB   ← entries go to indexer, state persists to disk
```

## Adding a New Plugin

In order to create a new plugin, follow these steps:

1. Create `hosted/plugins/<name>`

- `config.go` Defines the plugins `Config`, embeds `hosted.BaseConfig` (and `MultiTagConfig` or `PollingConfig` as needed), implement `Verify()` and `Tags()` to satisfy the interface
- `<name>.go` Defines the `Job` (or `Ingester` if you need concurrent internal streams). Export `Name`, `ID`, and `Version` constants

2. Register it in `hosted/plugins/config.go` and map the config field into [`Configs`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go#L26) like so:

```go
type Configs struct {
  // existing config fields...
  MyPlugin map[string]*myplugin.Config
}
```

Add: 

- validation into [`Verify()`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go#L33)
- tag collection in [`Tags()`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go#L65)
- count in [`IngesterCount()`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go#L79)
- a loop in [`Builders`](http://github.com/gravwell/gravwell/blob/main/hosted/plugins/config.go#L96)

3. Add a `Builder` in [`hosted/plugins/builder.go`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/builder.go):

```go
type MyPluginBuilder struct {
  Builder[*myplugin.Config]
}

func (b *MyPluginBuilder) Build(tn hosted.TagNegotiator) (hosted.Ingester, error) {
  return hosted.WrapJob(myplugin.New(b.config)), nil
}

func NewMyPluginBuilder(config *myplugin.Config, kind, id, version string) *MyPluginBuilder { ... }
```

4. Add an `example.conf` into your plugin package

See [`hosted/plugins/mimecast/example.conf`](https://github.com/gravwell/gravwell/blob/main/hosted/plugins/mimecast/example.conf) as an example.

5. **Write tests!**

The `mockRuntime` / `testRuntime` pattern in `hosted/testutil_test.go` will give you a full in-memory `Runtime`. Call `Handle()` directly, and assert on written entries and stored state.

## Design Considerations

- `Handle` is called **sequentially per plugin**. The adapter calls it, sleeps, and then calls it again.
  - If you need concurrent internal streams, either handle the fan out logic inside of `Handle` itself (like the `mimecast` plugin does with `errgroup`) or keep `Ingester.Run` (like `okta`). 
  - **Do not block `Handle` indefinitely.**
- State lives in BoltDB per plugin instance
  - Keyed by `kind/name/version`
  - The UUID in your config is what separates two different instances of the same plugin type.
  - If a plugin has no UUID, one is generated for you. But this **changes on every restart**, so state will be lost.
- `Sync()` is called by the adapter after every successful `Handle()`
  - This is what guarantees at-least-once delivery. **Don't skip it.**
- `rt.Alive()` returns false when the ingest muxer is backed up and writes will block
  - The adapter **already checks this** before calling `Handle()` — if the connection is unhealthy it backs off for `JobAliveDelay` (10s) and retries. Plugins do not need to check `rt.Alive()` themselves.
