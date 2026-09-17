# Claude Compliance

Polls the Claude Enterprise Compliance API using the standard Hosted Runner.
One plugin supports the 26 JSON operations in `catalog.go`; it does not download
binary files, delete vendor content, or acknowledge vendor records.

## Configuration

Start with `example.conf`. Supply a separate Compliance credential file and
Gravwell ingest-secret file, readable only by the runner account. Replace both
zero UUID placeholders with distinct persistent UUIDs. Use a unique instance
UUID for every added `[ClaudeCompliance "name"]` stanza.

`Scope-Identity` is a label you choose, not an Anthropic header, credential, or
API permission. For example, `company-production-all-orgs` identifies one key's
visible organization/scope boundary. Instances sharing that credential boundary
should share the label: it keys the shared API rate limiter and participates in
checkpoint identity. Different access boundaries need different labels. Keep
the label stable across key rotation when the access boundary remains the same.
Changing it deliberately starts a new checkpoint namespace and can replay data.

`Lookback=24`, `Lookback=24h`, and `Lookback=1d` all mean 24 hours on the first
poll of a time-windowed dataset. Whole-day and whole-hour components may be
combined, such as `Lookback=1d12h`; legacy bare integers remain hours. An
existing checkpoint takes precedence;
`Start-Time` (RFC3339 with
offset) overrides lookback when that checkpoint is absent. Overlap defaults to
300 seconds. Discovery scans parent inventories without their time filter so
unchanged parents cannot hide changed child records. Inventory and transcript
endpoints without a time filter are bounded full scans, not lookback-filtered.
A newly discovered chat collects its complete message history before later
refreshes use the stored message checkpoint and overlap window.

The named stanza owns all polling settings. Repeat settings in each stanza as
needed. Defaults:
lookback 24 hours, 60 requests/minute, 300-second poll interval, page size 100,
1,000 pages, four retries, 16 MiB responses, 4 MiB entries, 100 children per
cycle, and 10,000 remembered child work items. The example sets 30 requests/minute.
The strictest configured rate is shared within a scope label. `Max-Pages` and
the 100,000-record per-call bound chunk large traversals into immediately
rescheduled calls; a stored opaque cursor resumes the same frozen traversal.

## Datasets and tags

| Family / default tag suffix | Dataset selectors |
| --- | --- |
| `activities` | `activities` |
| `directory` | `organizations`, `organization-users`, `organization-roles`, `organization-role`, `role-permissions`, `organization-settings`, `groups`, `group`, `group-members` |
| `conversations` | `chats`, `chat-messages`, `file-metadata`, `generated-file-metadata` |
| `projects` | `projects`, `project`, `project-attachments`, `project-collaborators`, `project-document`, `project-document-metadata` |
| `artifacts` | `artifact-metadata` |
| `sessions` | `local-sessions`, `local-session`, `local-session-messages`, `remote-sessions`, `remote-session-messages` |

The default prefix is `claude-compliance-`: exactly six semantic tags, not one
tag per endpoint. Set `Tag-Name="claude"` in every stanza to combine all data.
Discovered children inherit the parent's tag. Native JSON stays compact and
unwrapped; `_source`, `_recordType`, and `_endpoint` intrinsic fields distinguish
datasets. Other intrinsic context is `_vendor`, `_product`, `_apiVersion`,
`_parent`, and `_session` when provided. These are not JSON properties.
If a response's session envelope exceeds Gravwell's enumerated-value limit, the
native message is retained and `_session` is omitted with a warning.

Seven roots require no `Parameter`: `activities`, `organizations`, `groups`,
`chats`, `projects`, `local-sessions`, and `remote-sessions`. With
`Follow-Children="enabled"`, organizations discover users/roles/permissions,
groups discover members, chats and sessions discover messages, and projects
discover attachments/collaborators. Failed children remain queued with bounded
backoff. A full pending queue defers the current root page until existing work
can complete; it does not partially ingest or silently drop that page. Completed
work evicted from the bounded queue has its large manifest compacted to a small
retired checkpoint. Membership/content is revisited hourly; remote sessions every poll.

Other selectors need explicit IDs matching placeholders in `catalog.go`, e.g.
`Parameter="artifact_version_id:the-id"` for `artifact-metadata`. Binary content,
file/document ID discovery, and a global artifact inventory are not implemented.
Only configure operations your Compliance access key is authorized to read.

## State and delivery contract

The builder obtains the existing muxer's `SyncContext` method through its
standard `TagNegotiator` argument. No shared runtime, runner, storage, or ingest
SDK extension is required. After every completed dataset traversal:

1. A complete response page is validated before any record from that page is written.
2. Every new record must be accepted by `Runtime.Write`.
3. The existing muxer's `SyncContext` must return successfully, within two minutes.
4. Cancellation is checked, then the page cursor and partial manifest or final
   dataset checkpoint is stored.
5. The standard `WrapJobWithSync` adapter synchronizes state after a successful
   complete `Handle` cycle. `State.Sync=true` also flushes each state transaction.

A validation or discovery failure prevents that page from being written. A later
page, synchronization, or state-write failure retains the last completed page
cursor, so a retry does not restart the whole traversal. A write or synchronization
failure can still replay the affected page because ingest and state are not one
transaction. Discovery work is persisted once per page rather than once per
record; one child failure does not roll back other completed datasets.

**This is not an all-cache-drained backend-acknowledgement guarantee.** Upstream
`SyncContext` drains exposed channels and synchronizes current connections, but
does not expose a complete disk-cache backlog barrier. This plugin cannot prove
every cached entry reached a backend. Retain both ingest cache and state on
durable storage; do not delete a cache while retaining its later checkpoint.
State/cache are not one atomic transaction and exactly-once delivery is not
claimed. The upstream adapter logs state-sync errors without rolling back state.

## Operator validation

Execution host: your runner host. Target: a separate test state/cache and
authorized Claude organization. Action: configure the example and start the
upstream Hosted Runner using its normal service configuration. No test in this
directory contacts Claude or Gravwell; unit tests use synthetic responses.

Success requires observing useful native records, correct intrinsic fields and
tags, followed by a restart using the same UUIDs, scope label, cache, and state.
An HTTP 200, compile, or nonempty tag alone is not proof of complete collection.
If authentication fails, check key access and file permissions without printing
the key; if writes or synchronization fail, repair connectivity/cache capacity,
then repeat the same test without deleting state. Preserve configuration, cache,
and state together for persistence and a stopped-service backup. To disable
collection, stop the runner and remove the added Claude configuration stanzas;
preserve state/cache for recovery. Revalidate after restoring those stanzas.
Record sanitized counts, time windows, source and
binary hashes, errors, and restart results in your deployment evidence directory.

API references: [overview](https://platform.claude.com/docs/en/manage-claude/compliance-api),
[access](https://platform.claude.com/docs/en/manage-claude/compliance-api-access),
[activities](https://platform.claude.com/docs/en/manage-claude/compliance-activity-feed),
[sessions](https://platform.claude.com/docs/en/manage-claude/compliance-sessions).
