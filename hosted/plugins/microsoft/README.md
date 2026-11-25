# Microsoft Hosted Runner plugin

The Microsoft plugin polls supported Microsoft Graph, Azure Resource Manager,
Defender, Defender XDR, Intune, Microsoft 365 Management Activity, and Fabric APIs from the Gravwell
Hosted Runner. One `[Microsoft "name"]` stanza can repeat `Api` to collect several
endpoints with one tenant credential and polling schedule.

The plugin follows the standard Hosted Runner job contract. Registration lives
in `hosted/plugins/config.go`, construction lives in
`hosted/plugins/builder.go`, and `MicrosoftBuilder` uses
`hosted.WrapJobWithSync`, like the Mimecast plugin. Entries use the standard
`hosted.Runtime.Write` contract, and state is synchronized through the standard
Hosted Runner job wrapper.

## Configuration

```ini
[Microsoft "entra-security"]
Tenant-ID="11111111-1111-1111-1111-111111111111"
Client-ID="22222222-2222-2222-2222-222222222222"
Client-Secret="REPLACE_WITH_CLIENT_SECRET_VALUE"
Api="entra-directory-audits"
Api="entra-signins"
Lookback=24
Requests-Per-Minute=10
Request-Interval=3600
```

| Option | Required | Default | Description |
| --- | --- | --- | --- |
| `Tenant-ID` | yes | — | Microsoft Entra tenant UUID. |
| `Client-ID` | yes | — | App registration client UUID. |
| `Client-Secret` | yes | — | App registration client-secret value. Keep it in the protected Hosted Runner configuration, consistent with the established hosted plugins. |
| `Publisher-Identifier` | for Management Activity selectors | — | Non-zero tenant UUID of the vendor publishing the integration. Microsoft requires it on every production Management Activity request for a dedicated quota. It is not the customer tenant, app, or object UUID. |
| `Api` | yes | — | Repeatable selector or selector group. `all` enables the complete catalog. |
| `Subscription-ID` | for subscription-scoped Azure selectors | — | Repeatable Azure subscription UUID. |
| `Lookback` | no | `24` | Standard `PollingConfig` initial lookback, in hours; valid range 1–672, or 1–168 when a Management Activity selector is enabled. |
| `Requests-Per-Minute` | no | `10` | Standard `PollingConfig` request pace; valid range 1–6000. |
| `Request-Interval` | no | `3600` | Standard `PollingConfig` interval, in seconds; minimum 60. |
| `Tag-Name` | no | — | Exact destination tag; valid only when all selected APIs resolve to exactly one consolidated destination. |
| `Tag-Prefix` | no | — | Prefix applied to every resolved consolidated destination tag. |
| `Tag-Schema` | no | `consolidated` | Compatibility spelling for the only supported schema. Omit it or set `consolidated`; other values are rejected. |
| `Ingester-UUID` | no | stable Microsoft UUID | Override only when running another independent Microsoft stanza. |
| `Graph-Host` | no | `https://graph.microsoft.com` | Graph origin override for regional endpoints, proxies, and tests. |
| `Graph-Scope` | no | `https://graph.microsoft.com/.default` | OAuth resource override paired with `Graph-Host` for sovereign clouds or proxies. |
| `ARM-Host` | no | `https://management.azure.com` | Azure Resource Manager origin override. |
| `ARM-Scope` | no | `https://management.azure.com/.default` | OAuth resource override paired with `ARM-Host`. |
| `Defender-Host` | no | `https://api.securitycenter.microsoft.com` | Defender for Endpoint / Defender Vulnerability Management origin override. |
| `Defender-Scope` | no | `https://api.securitycenter.microsoft.com/.default` | OAuth resource override paired with `Defender-Host`. |
| `Defender-XDR-Host` | no | `https://api.security.microsoft.com` | Unified Microsoft Defender XDR origin override. It is a separate Entra ID resource from `Defender-Host`, so each origin is paired with its own token audience. |
| `Defender-XDR-Scope` | no | `https://api.security.microsoft.com/.default` | OAuth resource override paired with `Defender-XDR-Host`. |
| `PowerBI-Host` | no | `https://api.powerbi.com` | Power BI/Fabric origin override. |
| `PowerBI-Scope` | no | `https://analysis.windows.net/powerbi/api/.default` | OAuth resource override paired with `PowerBI-Host`; Power BI's default resource differs from its API origin. |
| `Management-Host` | no | `https://manage.office.com` | Office 365 Management Activity origin override. |
| `Management-Scope` | no | configured Management origin plus `/.default` | OAuth scope override for a proxy whose API origin differs from the Microsoft token resource. |
| `Auth-Host` | no | `https://login.microsoftonline.com` | OAuth origin override. |

Every host override must be an absolute HTTPS URL without credentials, query,
or fragment. Vendor pagination links must remain on the originating configured
service.

## Selectors and tags

The catalog contains 105 public selector names. `Api="all"` resolves 80
collection contracts after equivalent product aliases and filtered routes that
share one destination are collapsed.
Product groups such as `microsoft-entra-id-kit`, `microsoft-azure-kit`,
`microsoft-defender-kit`, and `microsoft-defender-xdr-kit` expand to catalog
selectors. `ResolveDatasets` and the catalog tests are the executable selector
inventory. `Config.Tags` applies the consolidated routing table and returns the
effective runtime destinations.

Each entry contains one compact, single-line vendor JSON record. The entry uses
the source timestamp when the selected API supplies one. The exact selector is
preserved as the `_source` intrinsic field without changing the vendor payload.

The catalog always routes into 22 stable destinations. `Tag-Schema` may be
omitted or explicitly set to `consolidated`; other values are rejected. Every
destination starts with `ms-`: Azure activity, inventory,
and policy; Defender for Cloud and Endpoint; Defender XDR alerts, incidents,
hunting, and posture; Entra audit, sign-in, risk, directory, and governance;
Microsoft 365 inventory; the five Office 365 Management Activity workloads;
Intune; and Fabric. In consolidated mode, the `microsoft-azure-kit` group
excludes its historical Azure-named Microsoft Graph directory aliases; select
`microsoft-entra-id-kit` or explicitly select an alias to collect those
contracts. Explicit aliases and `Api="all"` route those contracts to their Entra
destinations and collapse duplicate requests. Selector-based state keys do not
change.

The `microsoft365-management-activity` group enables all five documented
content types. Individual selectors and default routes are:

| Selector | Content type | Destination |
| --- | --- | --- |
| `microsoft365-audit-azure-active-directory` | `Audit.AzureActiveDirectory` | `ms-o365-azure` |
| `microsoft365-audit-exchange` | `Audit.Exchange` | `ms-o365-exchange` |
| `microsoft365-audit-sharepoint` | `Audit.SharePoint` | `ms-o365-sharepoint` |
| `microsoft365-audit-general` | `Audit.General` | `ms-o365-general` |
| `microsoft365-dlp` | `DLP.All` | `ms-o365-dlp` |

The first four content types require the documented `ActivityFeed.Read`
application permission. `DLP.All` also requires `ActivityFeed.ReadDlp` and an
applicable DLP entitlement. Unified audit logging must be enabled in the tenant.
Management Activity stanzas also require Gravwell's vendor tenant UUID as
`Publisher-Identifier`; a customer tenant, application, or object UUID is not a
valid substitute.

## Polling and state

Time-bounded event sources use half-open wire windows `[start,end)`. The
finalized `end` is five minutes behind wall clock. A first poll starts one
standard `Lookback` before that end. Each later poll starts five minutes before
the prior completed end, replaying a bounded sliding event-time overlap.

The overlap has a bounded receipt document scoped only to those replayed five
minutes. Records already written from the overlap are suppressed by source
identity plus timestamp, while a record which becomes visible on the next poll
is admitted when its source timestamp is at or after the prior completed end
minus five minutes. Equal timestamps with distinct source identities remain
distinct. Records timestamped before that documented overlap require vendor
replay; the plugin intentionally keeps no all-history identity/digest map.

Graph OData requests use inclusive `ge` and exclusive `lt` filters. Azure
Activity requests use the API's required time-only `eventTimestamp ge ... and
eventTimestamp le ...` filter; category selectors match `category.value`
locally after each page is decoded. Advanced Hunting uses an explicit
half-open timestamp predicate in KQL. Fabric's inclusive millisecond API
receives `[start,end)` as an inclusive wire range ending at `end-1ms`. Records
returned outside the current finalized wire window, including future
timestamps, are skipped without advancing beyond the finalized end.

Fabric serves only the last 28 days, so a window that starts before that floor
is clamped forward to it. The interval the clamp drops cannot be refetched by
any later reconfiguration: it is logged as an error and published as a status
warning naming the selector and the skipped interval, for the same reason the
Management retention losses are, because the poll continues and returns no
error. The clamp itself does not change the window end or the checkpoint.

A record whose own time field is absent, null, or in a layout the plugin does
not parse is still delivered, timestamped at the inside edge of the window being
finalized, and reported in its own warning separate from the skip above. Its
overlap receipt is keyed on source identity alone, because the substituted
timestamp moves with the window.

Snapshot sources emit the current vendor records on each poll, consistent with
existing Hosted Runner inventory plugins. Parent/child fetches scope child
identities by their parent while preserving the original child JSON.

Each selector/subscription uses discrete Hosted Runner keys: a timestamp, an
atomic pagination cursor document, a bounded atomic overlap-receipt document,
and, for Fabric only, an atomic scan-window document. The receipt document is
pruned at every completed poll and cannot exceed 200,000 identities. Reaching
that capacity is bounded at-least-once recovery rather than a terminal error:
the newest receipts are retained, the oldest inside the replay horizon are
dropped, and the records they covered may be delivered again by the next
overlapping window. The drop is logged and published as a status warning.
Nothing is skipped, and the selector keeps collecting -- the error this
replaced was never persisted, so no later poll could produce a different
answer and a dense selector stopped collecting for good.
The job sends each selected entry through the standard Hosted Runner write
path before storing a continuation or completed checkpoint. A write failure
leaves the prior checkpoint unchanged; entries already accepted from that
partial page can replay on retry, preserving at-least-once delivery.
`WrapJobWithSync` syncs the updated plugin state before the next poll.

A cursor, overlap, or Fabric document that cannot be decoded at all, including
an overlap document written by a newer build after a rollback, is replaced
rather than returned as an error on every later poll. The durable timestamp is
kept, so the selector restarts the current window from its last completed
boundary and reports the reset as a warning.

A poll that leaves pending work runs again immediately and visits only the
targets that have some, so one selector's pagination does not wait a full
interval behind unrelated ones. That sequence is bounded: once it has been
running for one `Request-Interval` the next poll visits every target again, so
a long Management Activity catch-up cannot keep its sibling selectors from being
collected. The catch-up itself is never paced back to the interval.

Management Activity selectors use a separate versioned state document per
content type. The plugin lists and starts subscriptions, suppresses a repeated
successful start for 15 minutes, and records a start as requested rather than
as confirmed -- the next list is what observes the status. While the source
still reports a content type as anything other than enabled inside that
suppression interval the poll collects nothing, so it names the selector, the
content type and the observed status in a warning and publishes a status
warning; a clean poll alone would clear the previous error and leave the
selector silently idle. It lists half-open content-created windows no
larger than 24 hours and no older than seven days, follows every `NextPageUri`,
and retrieves every `contentUri`. It records the active content ID, content-body
digest, event ordinal, bounded content receipts, and completed window. Each
event is accepted and each state mutation is synchronized before collection
continues. A partial content array resumes at its durable ordinal after restart.

That ordinal is validated on load against the absolute bound one content blob
can hold -- the 100,000 events the decoder itself enforces -- in the head
document and in the delivery checkpoint that may be adopted in its place. A
larger stored ordinal is structurally impossible, so the document is replaced
from the last completed boundary, which keeps `CompletedThrough`, discards every
in-flight listing and delivery field, and delivers nothing until that
replacement is synchronized. Bounding only the sign of the field let such a
value load and then fail against the fetched event count with no state mutation
at all, so every later poll fetched the same blob and failed identically until
the window aged out of retention. A legitimately maximal ordinal, and a document
written before the bound existed, both still resume. An ordinal inside that
bound and past the blob the source actually serves -- a length only the fetched
body states -- replays that blob from its own beginning under the at-least-once
contract, with the corrected ordinal durable and reported before any event is
written.

That document is stored across three keys so the cost of one synchronized write
does not grow with the state it happens to be carrying. The head document holds
the window, the listing cursor and the delivery position and is small and
fixed-size; the content descriptors live on their own key, written by the
listing loop and at window boundaries; the delivery receipts live on theirs,
flushed whenever a pass yields or a window closes. Finishing one content blob
therefore costs the head document, the small per-event delivery checkpoint and
one synchronization, not another copy of sets bounded only at 10,000
descriptors and 200,000 receipts. Receipts may lag inside a window because the
durably synchronized content index is what prevents redelivery there; they are
only consulted across windows, and every path that hands state to the next
window flushes them first. Both membership checks are answered from an index
built once per pass rather than by scanning. A document written by an earlier
build that carries both sets inline is migrated on its first write.

Each of those documents is measured against its own ceiling before it is
decoded, and the two arrays inside them are then read one member at a time
under the count bound and the aggregate stored-byte budget, so the member past
either bound is refused before it is retained. The ceiling bounds the bytes
that are read and says nothing about what retaining them costs: a dense JSON
member is two bytes while the element it decodes to is forty or ninety-six, so
decoding the array whole and measuring it afterwards let a compact document
inside its ceiling expand to hundreds of megabytes of retained elements, with
an element count an order of magnitude past the bound that then refused the
whole document. That applies to the descriptor key, the receipt key, both sets
an earlier build's head carries inline, and the non-Management overlap receipt
document. A set carried inline is still held only to its count bound, because
the aggregate budget postdates it and the maintenance pass is what brings it
under budget before its first migration write. The replacement a reset
publishes reads only the completed boundary out of the old head, skipping both
sets at the scanner, so the bound that refuses a document can never also cost
the checkpoint the reset restarts from.

An expired content URL is re-listed by stable `contentId`. The replacement is
individually valid -- same origin, same feed subtree, inside the 2,048-byte URL
bound -- and can still be longer than the descriptor it replaces, so the
aggregate descriptor growth is preflighted before anything is mutated. A
replacement that fits is retained and persisted; one that would push the set
past its 8 MiB budget is used for that fetch and not written down, so delivery
proceeds against the descriptor already held and a later pass that has to fetch
the same blob re-lists it again. Applying the replacement first instead left the
persist refusing the whole transaction on the aggregate bound, with the expired
descriptor still on disk and every later poll reaching the same refusal until
the window crossed the retention floor. Content the source can no longer serve
at all -- the identifier is absent from the re-list, or the
re-list of a window now outside retention is refused -- is permanently skipped:
the descriptor is receipted, the content index advances, and the exact count and
`contentCreated` range are recorded in the state document, logged, and published
as a status warning. A window whose own end has crossed the retention floor is
dropped whole and its checkpoint advanced past it, since none of its blobs can
be fetched and re-listing it is refused. Those are bounded, reported losses of
an interval the source no longer holds; they are not terminal, because a
terminal error there could never clear and stopped collection for the content
type entirely.

Both Management state bounds are recoverable rather than terminal. A window that
publishes more content descriptors than the document holds (10,000), or more
descriptor bytes than it holds (8 MiB), is halved
and listed again, down to a one-minute floor, and the remainder is collected by
the following window; nothing is listed twice because the bound is only reached
before delivery of that window starts. The byte budget matters because a
descriptor is mostly a source-supplied `contentUri`, so the count bound alone
did not bound the document the listing loop rewrites on every page. A single
listing page carrying more descriptors than a whole window may hold narrows the
same way, and is refused by the decoder before the descriptor that exceeds the
bound is retained: the subscription, descriptor and content arrays are all
streamed under their own count bound rather than decoded whole and measured
afterwards. `contentUri` is bounded at 2,048 bytes by the URL policy and the
descriptor timestamps at 64 bytes each. Halved spans are truncated to whole
seconds, which is the precision the `startTime` and `endTime` query parameters
can express, so a stored window boundary and the request that produced it never
disagree. A listed descriptor is matched against the range the query actually
asked for, and one genuinely outside it is reported and skipped rather than
failing the listing: the window that does cover it starts exactly where this one
ends. A descriptor already held or already accepted is skipped before that
check, so it can never be fatal. Delivery receipts are pruned to the
seven-day source retention and then trimmed to 200,000 by dropping the oldest
`contentCreated` first, which the content index has already advanced past. That
maintenance runs once per pass, immediately before the checkpoint that persists
the set, because running it per recorded receipt is what made a set growing
toward the bound quadratic. A set already at 200,000 is the one case the
once-per-pass schedule cannot answer on its own: the next completed content
appends receipt 200,001 and the per-content persist, which does not maintain
anything, refuses the whole transaction on the count bound, so the next poll
re-fetched the same blob and was refused identically. Room for one more receipt
is therefore made before it is appended, oldest-first and with a little
headroom so the whole-set pass amortizes across the contents that follow; the
identity whose delivery the transaction is acknowledging is never a candidate,
because it does not exist yet. Both cases are reported as warnings, one per
pass naming the whole eviction however the receipts went. An interval the source can no longer serve is
logged as an error and published as a status warning, because the poll loop
clears the status error channel after every poll that returns no error.

Each fetch chunk is capped at 1,000 requests and at what it retains: 32 MiB of
compacted records or 100,000 records, checked between pages, so a chunk always
makes at least one page of progress and then yields the same durable
continuation it returns at the request budget. Pages are decoded by streaming
their array element by element rather than into a whole slice, and each element
is charged against a 50,000-element per-page bound before it is decoded or
retained: a response inside the 16 MiB body bound can still hold millions of
elements, and materializing them all defeats the byte bound the body limit
exists to provide. A page past that bound is rejected whole with its cursor
untouched, so the next pass re-reads the same page and nothing is skipped or
delivered twice. Cycle detection retains only the
most recent 128 continuation fingerprints, so durable cursor state stays
bounded; terminal pages do not extend that history. Because that history is
bounded, the cursor also carries a cumulative page count for the window it
belongs to, validated on load and defaulting to zero for a document written
before the field existed. One `Handle` still reads at most 100 pages, but a
source that cycles through more distinct URLs or skip tokens than the history
holds no longer resumes forever: at 4,096 cumulative pages the window is closed
against its own end after everything already read has been delivered, and the
truncation is logged and published as a status warning rather than resumed or
silently restarted.

A parent-child fan-out charges only its parent-list pages against that
cumulative count. Spending one cumulative page per child made the ordinary
width of a tenant the thing that exhausted the budget -- `defender-users`
issues at least one request per device, so a tenant with more devices than the
budget closed the window partway down its device list on every pass and never
reached the rest of it. The child pages of one parent are bounded separately,
by a per-parent counter that is persisted with the cursor, validated on load,
reset when the fan-out advances to another parent, and defaulted to zero for a
document written before the field existed. One parent may supply at most 1,024
child pages. The parent that reaches that is truncated and the truncation is
logged and published as a status warning, naming the parent; the loss is bounded
to the rest of that parent's child list, and the fan-out continues at the next
parent with the parent-list cursor intact, so the window still completes and
every later parent is still delivered. Closing the whole window there instead
discarded every parent behind the offending one, advanced the watermark past
them anyway, and repeated that permanent loss on every window, since the next
window meets the same parent again. The cumulative parent-list page bound is
unchanged and still closes the window it truncates.

A stored Fabric cursor is checked against the whole invariant it is written
under: a page in flight names both that page and the exclusive end of the UTC
day it belongs to, with the day end after the day cursor, and a cursor between
days names neither. A pending page with no day end passed every other bound and
then advanced the day cursor to the zero time, so the window restarted at year 1
and failed on a plain vendor error that no later poll could answer differently.
Such a document is refused before any request is dispatched and routed through
the same reset path, which keeps the durable timestamp. A document written
before the day-end field existed carries no pending page, so it still resumes.

A cursor the source has already served is refused at every ordinary
non-Management site -- envelope pages, Resource Graph skip tokens, Fabric
continuation URIs, and both sides of a parent-child fan-out. A refusal clears
the stored cursor and restarts the window from the durable watermark on the
next scheduled poll, keeping the operator-facing reason, because nothing else
in the plugin rewrites that key: returning the refusal instead left the bad
cursor on disk to fail identically forever. Four of the five sites compare the
cursor before using it, so the pass that reads a persisted repeat issues no
request at all; Resource Graph publishes its skip token in the response body,
so it costs one.

Pagination continuation URLs must remain on the original
configured origin. Malformed records are skipped with bounded diagnostics so one bad row
does not starve later valid rows.

Advanced Hunting snapshots preflight an unpartitioned query and then every
candidate deterministic hash partition before publishing partition records. If
a partition overflows, planning widens before delivery. If source growth makes
a planned data partition overflow later, the current checkpoint is retained and
the scan fails rather than repartitioning already published partitions.

OAuth and data requests use `ingesters/utils.RetryHttpClient` through
`NewRetryHttpClientWithClient`, the same shared rate-limited retry implementation
used by the Mimecast Hosted Runner plugin.
Authentication, rate waits, retries, and pages for one selector/subscription
chunk share a two-minute total deadline; each HTTP attempt has a 30-second
timeout. A timeout returns an error without advancing that selector's state, and
later selectors and scheduled polls proceed normally. Caller cancellation stops
the same shared helper. POST bodies are replayable so a retry sends the complete
OAuth form or API request again. HTTP redirects are followed only when the
scheme, hostname, and effective port exactly match the initial request. A
redirect that changes any part of that origin is rejected before a bearer
header or OAuth form body is dispatched. Every response body is bounded by
bytes, not only by the attempt timeout: 16 MiB for an ordinary API response,
64 KiB for an OAuth token response, and 64 KiB for the body of a failed request,
which is discarded without being decoded. An oversized declared length is
refused before any of the body is read.

Management Activity calls use a dedicated bounded policy because that API
publishes `Retry-After`: at most five attempts, with 429 and 5xx as the only
retryable statuses. Every attempt is rate-limited, and an eligibility time
beyond the remaining deadline is deferred without an early request. Each
Management request owns a two-minute deadline, and one poll of one selector may
spend two minutes in total waiting on the source; beyond that the remaining
requests yield a durable continuation and the next poll starts with a fresh
span. Entry delivery is deliberately outside both bounds and runs on the job
context because the deadline belongs to the source: sharing it with the delivery
loop made a slow ingest link fail every pass on a write. Response bodies carry the same
16 MiB bound as every other API response, and individual native events are
limited to 1 MiB. One URL policy guards every Management Activity URL -- the
feed URL the plugin builds, a listed `contentUri`, a `NextPageUri`
continuation, and every redirect target. A URL must be at most 2,048 bytes both
as the source wrote it and as this plugin dispatches it, with the mandatory
`PublisherIdentifier` applied, so acceptance and dispatch can never disagree
about the same URL. It
must retain the exact configured HTTPS origin, carry no userinfo or fragment,
and address the exact `/api/v1.0/<tenant>/activity/feed/` subtree with a
non-sensitive query surface. Matching that prefix as written is not enough: a
path segment that is a literal or percent-decoded `.` or `..`, that decodes to
a `/` or `\` separator, that is double-encoded, or that contains a backslash is
refused outright, because such a path carries the prefix yet addresses
something else once a server canonicalizes it.

A descriptor or a continuation that policy refuses is bounded loss of that one
item, not of the selector. The refused descriptor is dropped and the refused
continuation ends the listing for that window; the listing progress already
made is persisted, the counts and the first reason are logged and published as
a status warning, and the pass reports an error. The next poll therefore
delivers what was listed and moves on instead of failing on the same page
forever. A stored `contentUri` the current policy refuses -- a descriptor
persisted by an earlier build -- is receipted and permanently skipped the same
way an expired blob is, with the exact loss recorded in the state document.

Each refused descriptor is counted exactly once. The count is charged by the
same state transaction that advances the listing past the page that published
it, which is exactly-once only while the listing moves forward; narrowing does
not, because it restarts the listing from the same window start and the whole
already-charged prefix is published and refused again. The head document
therefore carries a listing-start snapshot of the refusal totals and the first
reason, keyed to that window start, and narrowing restores it in the same
transaction that discards the listing -- so a window that narrows repeatedly,
across pass boundaries and restarts, still reports the loss it actually took.

The stored reason follows one presentation contract. It is written canonically
and bounded, and every operator-visible insertion of it -- the structured log
parameter and the error the pass returns -- is escaped once, where it is
inserted. Escaping it where it was written instead left a reason restored from
the state store, which load validation bounds the length of and nothing else,
reaching the sink with a CR, an LF, a Unicode `Cf` format character or a
`Zl`/`Zp` separator intact, which ends the physical record mid-value and lets
the remainder be read as further complete records the plugin never wrote.

A `NextPageUri` the same window has already followed ends that window's listing
the same way, and costs no request because the repeat is refused before
dispatch. Both sides of that comparison are durable -- the page history and the
continuation itself are persisted by the page that published them -- so
returning the repeat as the pass's error left every later poll reading the same
pair back and failing identically until the window crossed the retention floor,
at which point the interval it covers was lost with it. The loss is bounded to
whatever sat behind the repeat, it is logged and published as a status warning
naming the repeat, and the window that follows starts exactly at the truncated
window's end.

## Validation

From the repository root:

```sh
go test ./hosted/plugins/microsoft ./hosted/plugins
go test -tags upstream_registration ./hosted/plugins/microsoft ./hosted/plugins
go test -race ./hosted/plugins/microsoft ./hosted/plugins
go vet ./hosted/...
```
