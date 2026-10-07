# Claude Compliance

Polls the Claude Enterprise Compliance API using the standard Hosted Runner.
One plugin supports the 26 JSON operations in `catalog.go`; it does not download
binary files, delete vendor content, or acknowledge vendor records.

## Configuration

Start with `example.conf`. Supply the Compliance API key either inline as
`Credential` or, preferably, as a `Credential-File` readable only by the runner
account. Use a unique persistent `Ingester-UUID` for every added
`[ClaudeCompliance "name"]` stanza.

A stanza lists as many `Dataset` selectors as you want. Each is collected
against its own checkpoint and lands on its family's tag, so one stanza can
cover a whole family without a config stanza per endpoint. `Page-Size` is
clamped to each endpoint's documented maximum rather than forcing one value
to fit them all.

`Host` defaults to `https://api.anthropic.com` and exists so the plugin can be
pointed at a mock, a proxy, or a future region-specific endpoint. Each stanza
owns one API rate limiter shared by all of its datasets and discovered children.
When several stanzas use keys from the same parent organization, divide that
organization's request budget among their `Requests-Per-Minute` values. Each
stanza gets its own checkpoint namespace from the runner, so no scope label is
needed.

`Lookback` is the standard polling lookback in whole hours and applies on
the first poll of a time-windowed dataset. An existing checkpoint always
takes precedence, so a long-idle ingester resumes from its stored cursor
rather than attempting to walk back to the beginning of time. Overlap
defaults to
300 seconds. A windowed request's upper bound trails the current time by one
minute, the vendor's documented indexing delay, so a late-indexed record is not
excluded whatever the overlap is. With `Follow-Children="enabled"`, the
`projects` and `local-sessions` inventories are listed without their time
filter so an unchanged parent cannot hide changed child records. The `chats`
root stays an incremental `order_by=updated_at` poll, because the vendor
returns a chat again whenever it receives a new message, changes project, or is
deleted. Inventory and transcript endpoints without a time filter are bounded
full scans, not lookback-filtered. A newly discovered chat collects its
complete message history before later refreshes use the stored message
checkpoint and overlap window.

The named stanza owns all polling settings. Repeat settings in each stanza as
needed. Defaults:
lookback 24 hours, 60 requests/minute, 300-second poll interval, page size 100,
1,000 pages, four retries, 100 children per cycle, and 10,000 remembered child
work items. The example sets 30 requests/minute. Response and entry sizes are
bounded internally rather than configured, so no operator choice can discard
data the API actually returned.
Configuration reloads apply both rate increases and decreases. `Max-Pages` and
the 100,000-record per-call bound chunk large traversals into immediately
rescheduled calls; a stored opaque cursor resumes the same frozen traversal.
A stored walk older than 23 hours, or whose cursor the API rejects with HTTP
400, restarts from its first page, because vendor cursors expire or are
re-evaluated after 24 hours. The restarted walk keeps its manifest, so records
already written are deduplicated rather than replayed.

Pagination follows the documented contract: `after_id` endpoints continue while
`has_more` is true (an absent `has_more` means false); page-token endpoints stop
when `has_more` is false or, on session endpoints without `has_more`, when
`next_page` is null. HTTP 429 and transient 5xx responses, and connection or
read failures, are retried up to `Max-Retries` with one-second exponential
backoff capped at 60 seconds and a longer `retry-after` honored up to the
configured poll interval; a value beyond that bound ends the current cycle;
`x-should-retry: false` is never retried. Waiting goes through `Runtime.Sleep`,
so a shutdown interrupts a backoff immediately.

## Datasets and tags

| Family / default tag suffix | Dataset selectors |
| --- | --- |
| `activities` | `activities` |
| `directory` | `organizations`, `organization-users`, `organization-roles`, `organization-role`, `role-permissions`, `organization-settings`, `groups`, `group`, `group-members` |
| `conversations` | `chats`, `chat-messages`, `file-metadata`, `generated-file-metadata` |
| `projects` | `projects`, `project`, `project-attachments`, `project-collaborators`, `project-document`, `project-document-metadata` |
| `artifacts` | `artifact-metadata` |
| `sessions` | `local-sessions`, `local-session`, `local-session-messages`, `remote-sessions`, `remote-session-messages` |

Tags use the standard `hosted.MultiTagConfig`: `Tag-Prefix` (default
`claude-compliance`) yields exactly six semantic tags, one per family, not one
tag per endpoint. Set `Tag-Name="claude"` in a stanza to pin a single tag.
Because many datasets share one tag, the intrinsic `_source` field is what
distinguishes them downstream: nine directory datasets land on
`claude-compliance-directory`, and two of them can return byte-identical
payloads.
Discovered children inherit the parent's tag. Native JSON stays compact and
unwrapped; `_source`, `_recordType`, and `_endpoint` intrinsic fields distinguish
datasets. `_endpoint` is the endpoint template, such as
`/v1/compliance/groups/{group_id}/members`; the resolved IDs are in `_parent`. Other intrinsic context is `_vendor`, `_product`, `_apiVersion`,
`_parent`, and `_session` when provided. These are not JSON properties.
If a response's session envelope exceeds Gravwell's enumerated-value limit, the
native message is retained and `_session` is omitted with a warning.

Each dataset declares a `Kind` in `catalog.go`, and the two kinds are
collected on separate rules. An **Event** happens once and is never revised --
the activity feed and every message endpoint -- so having seen its identity at
all is proof it was already written, whatever bytes the vendor later replays.
A **Record** describes something that exists and can be edited -- users,
groups, projects, chats -- so it is re-listed on every scan and only an
identical content digest proves nothing new arrived.

Each dataset declares its timestamp and identity in `catalog.go`. Events and
messages use their `created_at`; mutable objects use `updated_at`; snapshots
without a change time, such as users, organizations, role permissions,
collaborators, and settings, use collection time instead of an unrelated
creation time. Records are deduplicated by their documented stable identity:
`uuid` for organizations, `user_id` for group members, `version_id` for artifact
metadata, a composite key for role permissions and collaborators, otherwise `id`.
For windowed datasets an identity last seen before the next window is dropped
from the manifest once the walk completes.

Seven roots require no `Parameter`: `activities`, `organizations`, `groups`,
`chats`, `projects`, `local-sessions`, and `remote-sessions`. With
`Follow-Children="enabled"`, organizations discover users/roles/permissions,
groups discover members, chats and sessions discover messages, and projects
discover attachments/collaborators. Deleted chats and projects tombstone their
child work; a `pending` remote session is not queued until it starts. Failed
children remain queued with bounded backoff. A child that returns HTTP 404 after
its root listed successfully is logged and completed, as the vendor documents a
404 on a known ID as removed content. A full pending queue defers the current root page until existing work
can complete; it does not partially ingest or silently drop that page. Completed
work evicted from the bounded queue has its large manifest compacted to a small
retired checkpoint. Membership and content are revisited hourly and whenever a
parent's revision changes. Local-session transcripts are revisited only when
the session's `updated_at` (its last inference call) changes, and active remote
sessions every poll.

Other selectors need explicit IDs matching placeholders in `catalog.go`, e.g.
`Parameter="artifact_version_id:the-id"` for `artifact-metadata`. Binary content,
file/document ID discovery, a global artifact inventory, and the Claude Code
Artifacts list (`GET /v1/compliance/apps/code/artifacts`) are not implemented.
Only configure operations your Compliance access key is authorized to read.

## State

Progress lives in the runtime's own key/value store, one typed key per
independently meaningful value: `/since`, `/history`, `/retired`, and the page
walk's `/walk/cursor`, `/walk/since`, `/walk/until`, `/walk/started`. Two
values stay whole, as a single serialized value each: the dedup manifest and
the child worklist. Both are maps that must be enumerated and selectively
shrunk -- the manifest to evict its least recently updated identity when full,
the worklist to order pending work by last attempt and pick a capacity victim.
`hosted.Storage` offers only `Get`/`Put` on an exact key, with no delete, no
prefix listing and no batch, so one key per element could be written but never
enumerated, bounded or shrunk, and every evicted identity would leak a key
forever.

Because there is no batch, a commit is several writes. Their order is chosen
so that every prefix of it is safe: a page commit writes its cursor last, so
an interrupted write leaves no walk rather than half of one, and a dataset
commit clears the walk, then writes the manifest, then the lower bound, then
the history marker. No prefix of either can advance the lower bound past data
that was not written, so a torn commit costs duplicates and never records.

## Delivery contract

The muxer handed to the plugin must provide an ingest delivery barrier --
`SyncContext(context.Context, time.Duration) error`, which the shared muxer
already implements. It is **required, not optional**: construction fails
without it, because `Runtime.Write` only queues an entry and a state-store
sync says nothing about whether that entry was delivered. A checkpoint that
advanced on a queued-but-undelivered write would lose those records on
restart. After every completed dataset traversal:

1. A complete response page is validated before any record from that page is written.
2. Every new record must be accepted by `Runtime.Write`.
3. The muxer's `SyncContext` must return successfully within two minutes.
4. Cancellation is checked, then the page cursor and partial manifest or final
   dataset checkpoint is stored.
5. The standard `WrapJobWithSync` adapter additionally synchronizes *state*
   after a successful complete `Handle` cycle, and `State.Sync=true` flushes
   each state transaction. That is state durability, not an ingest barrier,
   and it is never a substitute for step 3.

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
tags, followed by a restart using the same UUIDs, cache, and state.
An HTTP 200, compile, or nonempty tag alone is not proof of complete collection.
If authentication fails, check key access and file permissions without printing
the key; if writes or synchronization fail, restore connectivity or cache capacity,
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
