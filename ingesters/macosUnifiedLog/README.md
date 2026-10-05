# Gravwell macOS Unified Log Ingester

The macOS Unified Log ingester streams events from the macOS [unified logging system](https://developer.apple.com/documentation/os/logging) into Gravwell. It runs `/usr/bin/log stream --style json` and ingests each event as a single-line JSON entry.

The ingester only runs on macOS. `log stream` needs administrator privileges, so run the ingester as root, for example from a LaunchDaemon.

## Building

```
GOOS=darwin GOARCH=arm64 go build   # Apple silicon
GOOS=darwin GOARCH=amd64 go build   # Intel
```

## Configuration

The default configuration file is `/opt/gravwell/etc/macos_unified_log.conf`, with overlays in `/opt/gravwell/etc/macos_unified_log.conf.d`. The `[Global]` section takes the standard Gravwell ingester options. See `macos_unified_log.conf` for an example.

Each `[Stream "name"]` stanza runs its own `log stream` process. Use several stanzas to send differently filtered events to different tags. At least one stanza is required.

| Parameter | Description |
|-----------|-------------|
| `Tag-Name` | Tag to ingest into. Defaults to `default`. |
| `Level` | `default`, `info`, or `debug`, passed as `--level`. Each level includes the ones before it, and `info` and `debug` are much more verbose. |
| `Predicate` | An [NSPredicate](https://developer.apple.com/documentation/foundation/nspredicate) filter, passed as `--predicate`. The value goes straight to `log` without a shell, so it needs no extra quoting. |
| `Process` | A process name or PID, passed as `--process`. Can be repeated. |
| `Type` | `activity`, `log`, or `trace`, passed as `--type`. Can be repeated. |
| `Include-Source` | Include symbol names and source line numbers where available (`--source`). |
| `Ignore-Timestamps` | Use the time an event is read instead of its own `timestamp` field. |
| `Source-Override` | Override the entry source IP for this stream. |
| `Preprocessor` | Preprocessors to apply to this stream's entries. Can be repeated. |

```
[Stream "auth"]
	Tag-Name=macos-auth
	Predicate="subsystem == 'com.apple.Authorization' OR process == 'sudo'"
	Type=log
	Type=activity
```

## Behavior

* Entry timestamps come from each event's `timestamp` field. If the ingester can't parse it, it uses the current time and logs one warning per stream.
* The ingester relays anything `log` writes to stderr, such as an invalid predicate or a permissions error, to its own log.
* When `log` exits, the ingester restarts it. The delay starts at 1 second and backs off to 30 seconds while `log` keeps failing without producing events.
* Events larger than 16MB, or output the ingester can't parse, cause it to restart the `log` process.
* Events emitted while `log` is restarting, or while the ingester is stopped, are not backfilled.
