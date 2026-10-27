# n8n Hosted Runner

The n8n Hosted Runner polls metadata from the n8n public API and writes one
compact JSON record per Gravwell entry. Configure `Base-URL` with the instance's
`/api/v1` path and supply the API key with either `Credential` or a protected
file referenced by `Credential-File`. When both are configured,
`Credential-File` takes precedence. `Base-URL` must be an absolute origin whose
path ends with `/api/v1`; userinfo, query parameters, fragments, and
non-canonical path escaping (including percent-encoded separators) are rejected
so that a dataset request cannot be retargeted. HTTPS is required by default.
`Allow-Plaintext=true` permits HTTP and sends the API key in cleartext; use it
only for loopback or test instances, never over an untrusted network. An
isolated test instance is appropriate; do not enable this for production.

## Datasets

`Dataset` is required and has no default. Name every dataset the stanza is
allowed to collect. This makes access to potentially sensitive source data
explicit: `users` includes email addresses, `variables` preserves configured
values, and `workflows` preserves workflow bodies.

| Dataset | Method and path | API-key access | Dataset tag |
|---|---|---|---|
| `audit` | `POST /audit` | Security-audit access | `n8n-audit` |
| `credentials` | `GET /credentials` | `credential:list` | `n8n-creds` |
| `executions` | `GET /executions?includeData=false` | List access for the endpoint | `n8n-executions` |
| `projects` | `GET /projects` | List access for the endpoint | `n8n-projects` |
| `tags` | `GET /tags` | List access for the endpoint | `n8n-tags` |
| `users` | `GET /users` | List access for the endpoint | `n8n-users` |
| `variables` | `GET /variables` | List access for the endpoint | `n8n-variables` |
| `workflows` | `GET /workflows` | List access for the endpoint | `n8n-workflows` |

`audit` is an event-like activity dataset. `executions` is also activity data,
but an execution can change while it is running; a later snapshot of the same
execution is treated as a changed record. The other datasets are resource
records whose current API representation may change between snapshots. All
eight endpoints are polled as complete API snapshots; this distinction
describes the source data, not a different checkpoint or pagination protocol.
`executions` is always requested with `includeData=false`, so its entries carry
execution metadata only; node inputs and outputs are never fetched, and this is
not configurable.

Only the `credentials` dataset removes secret-shaped properties before framing
or checkpointing a record. Other datasets, including `variables`, `workflows`, and `audit`,
preserve their returned values apart from compact JSON normalization. Those
values may contain sensitive data; select only datasets approved for ingestion.
The configured API key is used for authentication only; the ingester does not
add it to entries, state, URLs, or log messages.

List endpoints use `limit` and `cursor` pagination, and every record in a list
response must be a JSON object. `Page-Size` is bounded to 250. A configured
`Max-Pages` limit that prevents a complete snapshot fails the poll without
advancing its state. Responses are decoded as streams; the ingester does not
reject a dataset solely because a response or complete snapshot crosses an
arbitrary byte threshold. To avoid retaining the full payload in memory,
records are normalized into a private mode-0600 temporary file, one record at a
time. That file and its 64 KiB write buffer are reused for one dataset collection,
rewound and truncated between records, and removed when collection ends, including
errors. Streaming retains a raw-record counter, not an unused metadata slice.
Strings, identity values, and recursive credential scrubbing are streamed;
no complete raw record or generic JSON object graph is retained in memory.
Object fingerprints sort fixed-size child hashes with a shared 4096-member
memory budget and sorted disk runs for wider objects. An unchanged record is
discarded without allocating entry data; a changed record is read once into the
plugin's entry buffer. `NativeRuntime.Write` then deep-copies that payload before
enqueueing it because runtimes cannot trust plugins to keep buffers immutable.
The plugin therefore limits each normalized record to 64 MiB, bounding the
plugin-owned payload plus that record's runtime copy to about 128 MiB. Entries
already queued by the ingest muxer can add memory according to its global queue
and cache settings; this plugin does not change those settings. Per-record
hash-sort runs are removed after use; all remaining scratch files are removed on
collection success or error.
The scratch location is `os.TempDir()`; on Unix, set the existing `TMPDIR`
environment variable before starting the runner to choose its directory
(otherwise the usual default is `/tmp`). Use disk-backed storage, not a tmpfs
or memory-backed temporary directory, to keep scratch payloads out of RAM.
Allow space for one normalized record up to 67,108,864 bytes plus its hash-sort
runs. Each member digest is 64 bytes, and one record is limited to 250,000
retained object members across all nested objects. Merging temporarily retains
both input and output runs, so the member-hash scratch bound is about 32 MiB.
This is not a whole-snapshot spool. A process crash may leave
files behind; apply the host's normal temporary-file cleanup policy.
The plugin does not set environment variables or override global runner options.
A request,
decoding, or later-page failure can therefore leave earlier entries enqueued
while the completed snapshot checkpoint remains unchanged. A separate bounded
in-flight cache is saved through the standard runtime storage API when a poll
fails, so repeated failures and a process restart do not continually re-enqueue
the same unchanged prefix. The cache is bound to the completed manifest and is
cleared when a full snapshot succeeds; source churn beyond its 500,000-pair
budget may evict an old observation before the current poll reaches it, but the
poll remembers that eviction and does not enqueue the observation again. This avoids an unbounded
whole-snapshot disk spool and preserves the existing write-before-checkpoint
ordering. One normalized record must fit the plugin's 64 MiB per-record safety
limit; the output is checked while streaming before entry data is allocated.
The dataset poll fails rather than silently dropping an oversized record, so its
completed manifest does not advance and the standard hard-error retry behavior
applies. Duplicate
retained object keys are rejected rather than choosing an ambiguous value;
credential-secret properties are removed before this check. JSON object
key order is preserved in entries but does not change their fingerprints;
array order remains significant. Pagination cursors are limited to 64 KiB and
only their hashes are retained for repeat detection. A separate 500,000 raw-record ceiling bounds
tiny-record and pagination-overlap workloads; the 250,000 tracked-record limit
bounds snapshot state and map allocation. After removal of unused retained
fields, measured in-poll heap is approximately 0.8 KiB of live process memory per tracked record,
or roughly 200 MiB at the 250,000-record limit. Datasets are collected
sequentially, so that estimate does not multiply across the configured dataset
list. HTTP 403, 404,
405, 410, and 501 responses mark only that dataset unavailable; the remaining
selected datasets still run. HTTP 401 and rate limits remain poll errors so they
are visible to the standard Job adapter. If a deployment returns 403 for a revoked
key, it is an unavailable warning, not a poll error; investigate widespread 403s
rather than assuming they indicate only dataset entitlement.

Recognized timestamp fields accept RFC3339 strings or numeric Unix seconds only
when they are at or after the Unix epoch and no more than 24 hours ahead of
collection time. Out-of-range values in either representation are ignored in
favor of another recognized timestamp field or the current UTC time.

The client never follows redirects because a redirected request could disclose
the n8n API key to another origin. The plugin owns only its n8n API HTTP client;
it does not modify shared HTTP utilities, global configuration, ingest backends,
cache options, or the Hosted Runner lifecycle. Rate-limit waiting happens
before the 90-second request context, which covers the HTTP request and response
consumption. Transport
errors and HTTP status responses are returned directly to the standard Job
adapter, including 429. On a fully successful
poll, the adapter sleeps for `Request-Interval` after collection finishes. A
hard error instead causes the adapter to retry after a fixed 30 seconds,
regardless of `Request-Interval`; each retry fetches every selected dataset's
complete snapshot again. The plugin retains one rate limiter across successful
and failed polls, so retries remain inside the configured
`Requests-Per-Minute` budget even when the 30-second error delay is shorter
than the normal request interval. A valid `Retry-After` delta or HTTP date on
HTTP 429 or 503 extends that shared gate across polls, capped at 15 minutes; a
shorter later value cannot reduce an existing server-requested delay. Malformed
and expired values are ignored.
Dataset-unavailable responses (403, 404, 405, 410, and 501) are warnings and
do not trigger this error retry path.

## Tags

`Tag-Prefix` replaces the `n8n` prefix. `Tag-Name` is allowed only when exactly
one dataset is selected.

## Snapshot behavior

Each dataset is collected as a complete bounded snapshot, but emission is per
record. The ingester stores a completed manifest per dataset and a separate
in-flight runtime key/value entry when needed. A completed manifest
is sorted binary data containing one fixed-width state pair per observed record version:
a SHA-256 digest of the record identity and a SHA-256 fingerprint of its
normalized body. A poll emits only the records whose identity and fingerprint
pair is not already in that manifest or in-flight replay cache, so adding one record to a dataset emits
one entry rather than republishing the snapshot. This matters for
append-only datasets such as `executions`, where a whole-snapshot rule would
re-emit the entire retained history on every poll.

Identity uses the first non-empty top-level field in this order: `id`, `uuid`,
`eventId`, `executionId`, `workflowId`, `email`, then `name`. Numeric and string
identity values remain distinct. If none is present, the normalized record
fingerprint becomes the identity; byte-identical no-identity records therefore
collapse to one observation. Entry time uses the first valid top-level field in
this order: `timestamp`, `updatedAt`, `createdAt`, `startedAt`, `stoppedAt`,
`updated_at`, then `created_at`, subject to the validity window above, and falls
back to collection time. Conflicting records with one identity on the same page
fail that dataset rather than choosing one arbitrarily.

The checkpoint is still the whole snapshot, so a deleted record drops out of
state without re-emitting the records that survived, and a record that is
deleted and later reappears is emitted again. An unchanged snapshot produces
byte-identical state and writes nothing at all. A request, decode,
normalization, write, or pagination failure leaves the completed manifest unchanged;
successfully enqueued records are retained in the separate in-flight replay cache.
Storage failure or a process failure before progress can be recorded may still
replay entries.
If a mutable record is repeated with a changed body on a later cursor page, the
observed versions are retained as separate pairs for repeat suppression; conflicting versions within the same
page still fail the dataset rather than choosing arbitrarily.

Per-record deduplication reduces Gravwell writes, not n8n API requests. Every
poll fetches each selected dataset's complete snapshot again, including records
that have not changed; a list dataset with `N` retained records needs roughly
`ceil(N / Page-Size)` page requests. One client-side rate limiter is shared by
all selected datasets and retained across polls and hard-error retries, so
`Requests-Per-Minute` is a combined budget, not a separate allowance per
dataset or retry. Estimate the request time from the total pages across all
selected datasets (about one request every
`60 / Requests-Per-Minute` seconds after the first request), plus HTTP response
time. On a successful poll, the runner sleeps for `Request-Interval` after
collection finishes, so long polls add to that interval. A dataset that
exceeds `Max-Pages` fails without advancing its state and follows the fixed
30-second hard-error retry behavior described above.

Per-record state is bounded to 250,000 tracked records per dataset, counted
after pagination repeats are deduplicated. The limit is per dataset, so a
snapshot larger than it fails that dataset's poll and says so rather than
collapsing back to whole-snapshot re-emission; the remedies are to reduce how
many records n8n retains for that dataset or to stop collecting it. Dropping
other datasets does not help.

State that does not parse is discarded and rebuilt, which costs one duplicate
snapshot instead of wedging the dataset.

The runtime key/value interface has no delete or key-iteration operation, so
the plugin replaces each dataset's manifest as a whole instead of storing
individual record keys that could not be pruned after source-side deletion.
This makes state writes proportional to the current manifest size when it
changes. Each completed or in-flight manifest holds at most 500,000 observed
pairs (32,000,000 bytes plus the in-flight manifest's 32-byte binding).

Four costs are worth knowing before enabling a large dataset. First, full
snapshot fetching and the shared request budget mean poll duration scales with
all retained pages, even when no records are emitted; see the request-cost
note above. Second, completed state is one blob per dataset, 64 bytes per observed
record version, with a separate bounded in-flight blob for failed polls.
The completed blob is rewritten whenever the snapshot changes and the in-flight
blob is saved when a failed poll has new successful enqueues, so a dataset with many records that
change often writes a correspondingly large value to the shared state database
on most polls; this is still far less volume than re-emitting the snapshot,
but it is not free. Third, live map bookkeeping uses approximately 0.8 KiB per
tracked record while one dataset is collected, or roughly 200 MiB at the
250,000-record limit. Fourth, a different fingerprint format uses a separate state
namespace; the first successful snapshot in that namespace emits each selected
record once. Superseded state is left in place because the runtime key/value
contract has no delete. The `snapshot-v6` namespace binds streamed fingerprints
to identities that include each identity field's scalar kind, so numeric and
string IDs with the same spelling remain distinct. Earlier namespaces are not
read; moving from the immediately previous namespace intentionally causes one
complete snapshot replay, followed by normal unchanged-poll suppression.
Plugin release-version changes do not change the state identity; only an
explicit snapshot namespace migration does so.

The plugin uses only the public `hosted.Runtime` contract. `NativeRuntime.Write`
hands entries to the ingest muxer; it is not a backend acknowledgement. After a
successful `Handle`, the standard `WrapJobWithSync` adapter flushes the state
bucket before the next poll. This narrows the duplicate-on-restart window but
does not provide an at-least-once guarantee: a failed poll or process failure
can replay entries when state is not yet durable, or lose queued entries when
state becomes durable before those entries reach the ingest disk cache or
backend.

## Credential rotation

For file-backed configuration, the API key is re-read from `Credential-File` at
the start of every poll. Write a
replacement API key to a new mode-0600 file and atomically rename it over the
configured path; the next poll picks it up without restarting the runner. The
file must be a regular file rather than a symbolic link, must not be readable by
group or other users, and must hold between 1 byte and 64 KiB. An inline
`Credential` requires a runner restart to rotate and should be used only when
the deployment's configuration-secret controls are appropriate.
