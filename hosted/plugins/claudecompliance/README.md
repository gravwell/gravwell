# Claude Compliance

Polls the Claude Enterprise Compliance API using the standard Hosted Runner.
One plugin supports the 26 JSON operations in `catalog.go`; it does not download
binary files, delete vendor content, or acknowledge vendor records.

## Configuration

Start with `example.conf`. Supply the Compliance API key either inline as
`Credential` or, preferably, as a `Credential-File` owned by root or the runner
account with no group or world permissions (`0600` is the normal setting). Use
a unique persistent `Ingester-UUID` for every added
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
needed. A one-way fingerprint of the configured origin and current credential
is mapped to one of the stanza's finite `source/v4` state slots. State keys use
the slot number, not the fingerprint or credential. Changing either selects
the mapped slot before collection starts, including when another selected
dataset fails, so a different tenant can never inherit the prior tenant's lower
bound, cursor, manifest, or child worklist. A routine credential rotation
starts the new namespace at the configured lookback. A small stanza-level
record, holding only those fingerprints and never any credential material,
tracks the namespaces a rotation superseded so they are reclaimed rather than
retained forever. Reclamation takes two phases, because a mistyped or expired API key is
non-empty and selects a namespace of its own exactly as a valid one does:
observing the change records the new namespace and *retains* the previous one
intact, and deleting it is authorized only by a collection cycle that made a
successful request under the replacement, completed without error, and left none
of its own collection outstanding. One success proves only the operation that
returned it, so a replacement granted a subset of the prior key's scopes -- it
collects one selected dataset and is refused `401`/`403` on the next -- reclaims
nothing. Nor does an error-free cycle on its own, because collection is spread
over cycles by design: that refused dataset's discovered child is left pending
with a future retry time, so the next cycle can list everything it is permitted,
never reach the sleeping child, and still be an incomplete replacement cycle. A
traversal that stopped at `Max-Pages` mid-cursor, a page deferred for
child-work capacity, and a cycle that skipped its root to drain healthy
discovered children while that root's own walk is still stored mid-cursor leave
the same gap. An operator who writes a
wrong or under-scoped key and reverts it therefore resumes the original
checkpoint instead of restarting at the lookback and skipping everything older,
and a reclamation that is unauthorized or fails stays recorded for a later
complete cycle. The plugin uses a bounded set of reusable source slots and
clears only the exact state keys it owns through the standard Hosted Runner
storage interface. If every slot is occupied by a replacement that has not
completed successfully, another rotation fails closed instead of destroying a
checkpoint that may still be needed. Restoring a previously used credential
selects its retained slot and resumes collection.

`Lookback` is the standard polling lookback in whole hours and applies on
the first poll of a time-windowed dataset. `Lookback=0`, which is also the
default when the field is omitted, starts at the first poll without a historical
backfill. A positive value backfills that many hours. An existing checkpoint
always takes precedence, so a long-idle ingester resumes from its stored cursor
rather than applying the initial lookback again. Overlap defaults to 300
seconds. A windowed request's upper bound trails the current time by one
minute, the vendor's documented indexing delay, so a late-indexed record is not
excluded whatever the overlap is. With `Follow-Children="enabled"`, the
`projects` and `local-sessions` inventories are listed without their time
filter so an unchanged parent cannot hide changed child records. The `chats`
root stays an incremental `order_by=updated_at` poll, because the vendor
returns a chat again whenever it receives a new message, changes project, or is
deleted. Inventory and transcript endpoints without a time filter are bounded
full scans, not lookback-filtered. A newly discovered chat collects its
complete message history before later refreshes use the chat-message
`created_at` filter, stored checkpoint, and overlap window.

The named stanza owns all polling settings. Repeat settings in each stanza as
needed. Defaults:
no historical lookback, 60 requests/minute, 300-second poll interval, page size 100,
1,000 pages, four retries, 100 children per cycle, and 9,000 remembered child
work items. When child discovery is enabled, `Max-Pending` must hold one
maximum-size discovery page for every selected dataset (up to 2,000 for
organizations) so admission is atomic, and cannot exceed 9,000. That ceiling is
derived from the worst-case *serialized* work item, not from its worst-case
length: an identifier is stored three times over, in the worklist key, in the
parameter list, and percent-encoded inside the child's state key. Opaque
identifiers are therefore also rejected if they are not valid UTF-8 or contain
`<`, `>`, `&` or a Unicode line separator, which are the characters that would
otherwise make a nominally bounded identifier serialize several times larger.
An operator-supplied `Parameter` value is bound into the same request paths and
work items as a discovered one and is held to the same 256-byte ceiling at
validation time. The example sets 30 requests/minute.
Response and entry sizes are bounded internally at 33,554,432 bytes. Configure
the global `Max-Entry-Size` at or above that value so every record the plugin
accepts can also be accepted by the muxer. If the global limit is lower,
`Runtime.Write` returns an error and the affected checkpoint does not advance.
A multi-record response that exceeds the internal bound is retried with
successively smaller pages down to one record.
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
`next_page` is null. HTTP 429 and transient 5xx responses and connection or
read failures are retried up to `Max-Retries` with one-second exponential
backoff capped at 60 seconds and a longer `retry-after` honored up to the
configured poll interval; a value beyond that bound ends the current cycle.
Malformed or contradictory response envelopes fail the current page without
an in-request retry; the last completed checkpoint is retained so collection
can retry from there on a later cycle.
One collection cycle also has an aggregate waiting budget of one poll
interval, shared by every dataset and discovered child it visits, so a
multi-page traversal cannot chain one ceiling-sized wait per page and hold the
runner far longer than a whole poll. When the budget cannot cover the next
wait, the cycle stops at its last committed page. No page is skipped, and the
next cycle resumes the stored cursor. The server's requested delay remains a
plugin-local signal for fan-out scheduling; the shared Hosted Runner does not
accept per-error schedules and retries a failed `Handle` after its standard
30-second error delay. Back-pressure that must be honored before another
request is waited out inside the cycle through `Runtime.Sleep`;
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
unwrapped. `_source` identifies the configured dataset, `_parent` is a compact
JSON object containing only the discovery-derived parent parameters for child
requests, and `_session` contains a valid bounded session or chat envelope only
for `chat-messages`, `local-session-messages`, and `remote-session-messages`.
These are intrinsic fields, not JSON properties. Irrelevant, missing, null, or
invalid context is omitted rather than represented by a placeholder.
If a response's session envelope is not an object or exceeds Gravwell's
enumerated-value limit, the native message is retained and `_session` is
omitted with a warning.

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
`uuid` for organizations, `organization_id` for organization settings, `user_id`
for group members, `version_id` for artifact metadata, a composite key for role
permissions and collaborators, otherwise `id`.
For windowed datasets an identity last seen before the next window is dropped
from the manifest once the walk completes.

Seven roots require no `Parameter`: `activities`, `organizations`, `groups`,
`chats`, `projects`, `local-sessions`, and `remote-sessions`. With
`Follow-Children="enabled"`, organizations discover users/roles/permissions,
groups discover members, chats and sessions discover messages, and projects
discover attachments/collaborators. Deleted chats and projects receive one
final child drain before their work is compacted and tombstoned; a `pending`
remote session is not queued until it starts. Failed
children remain queued with bounded backoff. HTTP 404 is treated as removed
content only for a deleted parent's final drain, and only after that same cycle
listed the root successfully: such work is logged, completed, compacted and
tombstoned. An ordinary child's 404 stays a bounded-backoff failure even after a
clean root listing, because the vendor also returns a bare 404 for
unauthenticated access, so listing authorization does not prove authorization
for the child resource. A full pending queue defers the current root page until existing work
can complete; it does not partially ingest or silently drop that page. Completed
work evicted from the bounded queue has every known checkpoint key cleared.
Only a vendor-confirmed deletion retains one small revision tombstone; an
oversized stored parameter, a removed catalog entry, capacity eviction and
absence-based retirement all reclaim without one, because none of them is
evidence that the vendor deleted anything. For the same reason the drain path is
the sole owner of a deleted parent's child: absence-based retirement skips work
already committed to a drain, so a parent that is reported deleted and then
drops out of the listing entirely still ends with the tombstone its deletion
earned rather than the weaker retirement. Such work stays bounded by the drain
itself, by an authoritative 404, or by the stuck-pending capacity fallback. Root discovery still runs at least once per configured poll
interval while child work remains active. Membership and content are revisited
hourly and whenever a parent's revision changes. A selector that is also
reachable as a discovered child of another selected dataset -- `organizations`
plus `organization-roles` with `Parameter="organization_id:O"`, for instance --
is a redundant selection: the configured walk and the discovered one keep
separate checkpoints rather than corrupting one shared checkpoint, so those
records are collected twice. The same holds one level deeper: that pair of
selectors reaches each role's permissions from both roots, and each root keeps
its own checkpoint for it. Local-session transcripts are revisited only when
the session's `updated_at` (its last inference call) changes, and active remote
sessions every poll.

Other selectors need explicit IDs matching placeholders in `catalog.go`, e.g.
`Parameter="artifact_version_id:the-id"` for `artifact-metadata`. Binary content,
file/document ID discovery, a global artifact inventory, and the Claude Code
Artifacts list (`GET /v1/compliance/apps/code/artifacts`) are not implemented.
Only configure operations your Compliance access key is authorized to read.

## State

Progress lives in the runtime's own key/value store, one typed key per
independently meaningful value: `/since`, `/history`, and the page
walk's `/walk/cursor`, `/walk/since`, `/walk/until`, `/walk/started`, and
consecutive-empty-page count. Two values stay whole, as a single serialized
value each: the dedup manifest and
the child worklist. Both are maps that must be enumerated and selectively
shrunk -- the manifest to evict its least recently updated identity when full,
the worklist to order pending work by last attempt and pick a capacity victim.
`hosted.Storage` offers only `Get`/`Put` on an exact key, with no element
listing and no batch, so one key per element could be written but never
enumerated, bounded or shrunk, and every evicted identity would leak a key
forever.

Because a manifest stays whole, rewriting it costs the whole retained state, so
a page walk does not rewrite it per page. Each page appends one bounded journal
record describing only that page's own change, and both complete maps are
rewritten once per sixteen pages and once when the traversal completes. A
journal record is applied idempotently when state is loaded, so an interruption
can at worst re-apply or ignore one record: page progress stays loss-free, the
retained state stays bounded, and a torn commit still costs duplicates rather
than records.

The plugin uses only the standard Hosted Runner `Storage` surface: exact-key
`Get` and `Put`. It does not require a shared runtime extension. Physical state
is kept finite by reusable numbered slots rather than by creating a permanent
namespace from every credential fingerprint or vendor identifier.

Each stanza has four source slots under `source/v4/<slot>`. The durable
`sources/v2` record maps a one-way origin-and-credential fingerprint to its
slot and records the configured roots that slot owns; no credential material
appears in a state key. A changed credential receives a free slot before any
request is made. Only a successful, complete cycle under that replacement may
clear superseded slots. If all four slots remain occupied, another rotation is
rejected until a previously used credential is restored or a successful cycle
finishes the handover. The plugin therefore never destroys a potentially live
checkpoint merely to make room for an unproven credential.

A configured selector keeps its checkpoint under
`source/v4/<source-slot>/dataset/<dataset><resolved-path>#`. Its child
worklist, cleanup backlog and tombstone registry live with that configured
root. Discovered children receive numbered checkpoint slots under
`<root>/child/<slot>#`; the owning root's durable worklist records which child
owns each slot. `Max-Pending` bounds those slots. A slot is handed back only
after every known checkpoint key is cleared and the durable cleanup obligation
is removed, so vendor churn cannot grow the physical key space without bound
or pass stale dedup state to the next child.

Configured and discovered walks remain independent. If a stanza selects a
dataset directly and also reaches the same operation through
`Follow-Children="enabled"`, each owner has its own checkpoint and the records
are collected twice. Remove the redundant `Dataset` selector or disable child
following if that is not intended. Likewise, two configured roots that reach
the same descendant own separate child slots, so retiring one root cannot
clear state still used by the other.

The `source/v4` layout is pre-release state. It does not import checkpoints
from earlier development layouts. The first poll starts from the configured
`Lookback`; with `Lookback=0`, it starts at that poll. Previously ingested
records inside a positive lookback window can therefore arrive again during a
pre-release upgrade, but the new layout cannot skip records by carrying an
incompatible lower bound forward.

Releasing a child and reclaiming
its checkpoint cannot be one atomic write, so the obligation to reclaim is
itself durable: it is recorded before the worklist gives up ownership, acted on
only once the stored worklist no longer owns the child, and retried on a later
cycle or after a restart. A failed worklist write therefore cannot orphan an
owned checkpoint, and a failed reclamation cannot be forgotten. Retirement
cascades through the catalog relationship and matching parent parameters, so a
grandchild whose parent is gone is retired with it instead of backing off
against a 404 forever; an unrelated sibling under a different parent is
untouched. Deletion tombstones are held in one bounded registry per configured
root and the oldest are reclaimed, which costs at most one more final drain of
an already deleted child.

All discovered children of one configured root share one aggregate
100,000-entry manifest budget; a stanza selecting several roots keeps one such
budget per root. Trimming is demand driven: while the aggregate fits, every
child keeps what it has. Under pressure each child is held to a fair
retained-identity share, while the capacity an active child reserves before its
request survives that reconciliation rather than being trimmed back before the
request it was made for runs. That reservation is at least one page, or the
equal share when that is larger, and a child already measured gets one page
beyond what it last retained while aggregate capacity is free -- so a child
with more identities than a page converges on remembering all of them over
successive refreshes instead of forgetting most of itself and re-delivering the
remainder forever. Each
completed run replaces the reservation with what the child actually retained,
which is what stops the reservation growing past the resource. Reconciliation
evicts old dedup identities without changing cursors or lower bounds, which can
cause replay but cannot skip records.

Because there is no batch, a commit is several writes. Their order is chosen
so that every prefix of it is safe: a page commit writes its cursor last, so an
interrupted first page leaves no valid walk while an interrupted later page
retains the preceding cursor. A dataset commit clears the walk and its journal,
then writes the manifest, the lower bound, and the history marker. No prefix of
either can advance the lower bound past data that was not written, so a torn
commit costs duplicates and never records.

## Delivery contract

The muxer handed to the plugin must provide an ingest delivery barrier --
`SyncContext(context.Context, time.Duration) error`. The plugin checks that
existing muxer capability through a private interface; no shared Hosted Runner
interface change is required. The barrier is **required, not optional**:
construction fails without it,
because `Runtime.Write` only queues an entry and a state-store sync says
nothing about whether that entry was delivered. `Runtime.Write` also remains
the authoritative global entry-size check; a rejected write cannot advance a
checkpoint. A checkpoint that advanced on a queued-but-undelivered write would
lose those records on restart. After every completed dataset traversal:

1. A complete response page is validated before any record from that page is written.
2. Every new record must be accepted by `Runtime.Write`.
3. The muxer's `SyncContext` must return successfully within two minutes.
4. Cancellation is checked, then the page cursor and partial manifest or final
   dataset checkpoint is stored.
5. The standard `WrapJobWithSync` adapter additionally synchronizes *state*
   after a successful complete `Handle` cycle, and `State.Sync=true` flushes
   each state transaction. That is state durability, not an ingest barrier,
   and it is never a substitute for step 3.

A page-validation failure, including a non-object row, prevents the entire page
from being written or checkpointed; malformed rows are never silently
discarded. A parent record that cannot safely supply its child-discovery
identity because the identity is missing, oversized, or contains an invalid
character is retained as valid telemetry with a warning, while child expansion
is skipped only for that parent and valid siblings continue. A repeated
top-level field is a page-validation failure: a record or envelope
that supplies its identity, timestamp, deletion marker, pagination signal or
error marker more than once would leave the decision depending on which copy a
decoder kept while the emitted bytes still carry every copy, so the page is
rejected without advancing its dataset checkpoint. Valid earlier rows can
already have supplied discovery decisions, so a deletion tombstone backed by
one of those rows may still advance after its child drains. A later
page, synchronization, or state-write failure retains the last completed page
cursor, so a retry does not restart the whole traversal. Within one running
process, successful writes from an uncommitted page are remembered and skipped
on the error retry; this prevents a backend outage from repeatedly filling the
ingest cache with the same page. A delivery-barrier failure stops the current
dataset fan-out immediately, bounding this in-process ledger to one failed
page rather than one page per discovered child. A process restart can still replay that page
because ingest and state are not one transaction. Discovery work is persisted
once per page rather than once per record; one child failure does not roll back
other completed datasets, and a ready sibling is drained on the next cycle
after the bounded short retry, before the failed child's backoff expires.

An empty page with a continuation token advances only the small cursor state;
it does not rewrite unchanged dedup manifests. One hundred consecutive empty
continuing pages are allowed for sparse session pagination. A longer empty walk
fails the cycle and returns to the runner's standard error delay instead of
consuming the API and state store in an immediate loop.

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
If authentication fails, check key ownership and file permissions without printing
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
