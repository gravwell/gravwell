# Wiz Ingester

This ingester pulls the following event types from the [Wiz](https://www.wiz.io/)
cloud security platform via its GraphQL API and forwards each record to Gravwell
as JSON:

Each record is enriched with the related objects needed to stand on its own —
the affected resource/endpoint, the rule/control that fired, and the actor — so
a single entry is self-contained:

- **VulnerabilityFinding** (`vulnerabilityFindings`) — cursor `updatedAt`.
  Includes `vulnerableAsset` (affected endpoint: name, providerUniqueId,
  cloudPlatform, region, subscription, exposure), tying a CVE to its host.
- **Issues** (`issues`) — cursor `createdAt`. Includes `entitySnapshot` (affected
  resource), `sourceRule`, and `control`.
- **Detection** (`detections`) — cursor `updatedAt`. Includes `ruleMatch.rule`,
  `primaryActor`, and `primaryResource`.
- **ConfigurationFinding** (`configurationFindings`) — cursor `updatedAt`.
  Includes `rule` (cloud config rule), `resource`, and `subscription`.
- **Audit** (`auditLogEntries`) — cursor `timestamp`. Includes the acting `user`
  or `serviceAccount` (identity fields only — no secrets/tokens).

Each query filters server-side on its cursor field (via `$since`) and tracks a
per-source high-water-mark, so after the initial snapshot only newer records are
pulled.

## How it works

1. **Authentication.** At startup the `Client-Id` and `Client-Secret` are
   exchanged for an OAuth bearer token via the client-credentials grant against
   `auth.app.wiz.io`. Any request that comes back unauthenticated (HTTP 401 or a
   GraphQL `UNAUTHENTICATED` error) transparently refreshes the token and retries
   once.
2. **Paged pulls.** Each event type has a built-in GraphQL query for its
   Relay-style connection. The plugin pages through it with `first`/`after`,
   ingesting each node as its original JSON. `null` fields (and objects/arrays
   left empty once nulls are removed) are stripped first, since they are noise.
3. **Incremental tracking.** Each source has a cursor field (see above). The
   query filters server-side on that field via `$since`, and a per-source
   high-water-mark is kept in plugin storage. After the initial snapshot (which
   goes back `Lookback` hours) only records newer than the stored timestamp are
   pulled. Progress (cursor + timestamp) is persisted after every page so a
   restart resumes mid-scan.
4. **Tagging.** Every record goes to `Tag-Name`, overridable per source via
   `Tag-Override`. Each entry also carries a `type` enumerated value naming the
   Wiz query it came from (`vulnerabilityFindings`, `issues`, `detections`,
   `configurationFindings`, `auditLogEntries`).
5. **Error handling.** A source that returns `access denied` or a deterministic
   query error is logged once (cleanly, without the full payload) and skipped on
   future scans; transient `internal error`s are retried next cycle.

## Configuration

| Key | Required | Description |
| --- | --- | --- |
| `Endpoint` | yes | Tenant GraphQL endpoint from the Wiz console. Must be under `app.wiz.io`. |
| `Client-Id` | yes | Service account client id. |
| `Client-Secret` | yes | Service account client secret. |
| `Tag-Name` | yes | Default tag for all ingested events. |
| `Tag-Override` | no | Per-source routing, `source:tag`. Repeatable. Sources: `VulnerabilityFinding`, `Issues`, `Detection`, `ConfigurationFinding`, `Audit`. |
| `Lookback` | no | Hours of history to pull on the first scan of each source (default 24). |
| `Requests-Per-Minute` | no | Client-side rate limit (default 60). |
| `Request-Interval` | no | Seconds between poll cycles (default 300). |
| `Page-Size` | no | Nodes requested per GraphQL page (default 100). |
| `Max-Pages-Per-Type` | no | Pages drained per source per poll cycle (default 20). |
| `Query-Override` | no | Replace a source's query with a file, `source:/path/to/query.graphql`. Repeatable. |
| `Auth-URL` | no | Override the OAuth token endpoint (gov/fedramp tenants). |
| `Audience` | no | Override the OAuth audience (default `wiz-api`). |

See `example.conf` for a complete example.

## Customizing a query

The built-in queries use conservative, high-confidence field selections. If your
tenant exposes different fields, or you want richer records or server-side time
filtering, override a source's query with one you've verified in the Wiz API
Explorer:

```
Query-Override="Audit:/opt/gravwell/etc/wiz/audit.graphql"
```

The query must return a Relay-style connection (`nodes { … }` or
`edges { node { … } }` plus `pageInfo { hasNextPage endCursor }`). The plugin
supplies whichever of `$first`, `$after`, and `$since` the query declares;
`$since` receives the tracked high-water-mark timestamp (RFC3339) for
server-side incremental pulls. Example:

```graphql
query($first: Int, $after: String, $since: DateTime) {
  auditLogEntries(first: $first, after: $after, filterBy: {timestamp: {after: $since}}) {
    nodes { id action status timestamp }
    pageInfo { hasNextPage endCursor }
  }
}
```
