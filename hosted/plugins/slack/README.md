# Slack Audit hosted plugin

The Slack plugin polls the Slack Enterprise Audit Logs API and writes each
native audit entry as one compact JSON Gravwell entry. Slack Events API
callbacks use the separate Slack Events receiver; they are not handled by this
polling plugin.

## Required configuration

Configure one `[Slack "name"]` stanza with a persistent, unique
`Ingester-UUID`. Supply the organization-scoped OAuth token in exactly one of
these forms:

- `API-Token="..."` for a direct value; or
- `API-Token-File="/opt/gravwell/secrets/slack-audit-api.token"` for a protected
  regular file readable only by its owner.

When both are present, `API-Token-File` wins. The token needs the Slack
`auditlogs:read` scope and an Enterprise organization-level installation.
`Host` defaults to `https://api.slack.com`; `Tag-Name` defaults to
`slack-audit`.

`Lookback` is expressed in whole hours and defaults to 168 (seven days). It
bounds initial history only. After the first completed window, collection
resumes from five minutes before the durable completed-through checkpoint so
late-visible events receive a bounded replay without requesting the full
lookback every cycle. `Request-Interval` defaults to 300 seconds and
`Requests-Per-Minute` defaults to 30.

## Persistence and delivery

Persist the Hosted Runner `[State]` path and ingest cache. The plugin fixes a
collection window before pagination, resumes its cursor after restart, and
advances the completed-through checkpoint only after each accepted entry uses
the Hosted Runner's per-entry acknowledged delivery path. One bounded receipt
ring protects active traversal and retry progress, while a separate 10,000-slot
ring retains records from the newest five minutes so a large initial-history
walk cannot evict the receipts required by the next overlap. Pagination uses a
separate persisted cursor-marker ledger, so record volume cannot hide a cursor
loop. Each request asks Slack for 1,000 records. The 10,000-marker ledger
therefore covers approximately 10 million requested records in one traversal;
Slack may return additional records that share a timestamp. Past that marker
bound, the runner returns a visible terminal capacity error before making
another Slack request. An operator must select a shorter initial lookback and
reset that plugin instance's active state before collection can resume. A
window receives at most three cursor traversal cycles; another repeated or
invalid cursor returns an error so the runner reports the stall and applies its
error delay. Records rejected by the configured global ingest entry-size limit
are warned, receipted, and skipped so one record cannot permanently block the
window.

Slack collection remains at-least-once if a process or state device fails
between backend acknowledgement and receipt persistence. More than 10,000
distinct records inside one five-minute overlap can exceed the overlap receipt
capacity and allow repeats. Separately, if one active traversal delivers more
than 10,000 distinct non-overlap records and its cursor must reset, records
evicted from the traversal receipt ring may repeat during that reset. Events
that become visible more than five minutes after their timestamp are outside
the bounded late-visibility replay contract.

## References

- [Slack Audit Logs API](https://docs.slack.dev/admins/audit-logs-api/)
- [Slack Audit Logs API actions](https://docs.slack.dev/reference/audit-logs-api/methods-actions-reference/)
- [Gravwell Hosted Runner configuration](https://docs.gravwell.io/configuration/configuration.html#hosted-runner-configuration)
- [Gravwell storage configuration](https://docs.gravwell.io/configuration/storage.html)
