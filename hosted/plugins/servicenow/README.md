# ServiceNow Hosted Runner plugin

One `[ServiceNow "name"]` stanza selects any combination of sixteen ServiceNow
product domains, 55 explicit Table API datasets, and eight individually
validated non-Table endpoint profiles while sharing an instance and file-backed
credential. Raw mode preserves vendor keys (including duplicates), values, and
numeric representation while removing insignificant whitespace to produce one
compact object per entry; it is not byte-for-byte preservation. Optional
normalization preserves every vendor field and adds stable lowerCamel aliases
for documented snake_case or dotted response fields. It does not wrap or nest
the vendor object. Raw and normalized modes use the same semantic product tags
and separate durable state namespaces.

## Product and table coverage

| `Product=` value | ServiceNow tables | Current PDI evidence |
|---|---|---|
| `audit` | `sys_audit`, `sys_audit_relation` | Raw and normalized live delivery |
| `platform` | `syslog`, `syslog_transaction`, `sys_email`, `sys_user`, `sys_user_group`, `sys_user_grmember`, `sys_user_has_role`, `sys_user_role`, `sys_properties`, `sys_security_acl` | Raw and normalized live delivery |
| `itsm` | `incident`, `problem`, `change_request`, `sc_request`, `sc_req_item`, `task_sla`, `sysapproval_approver` | Raw and normalized live delivery |
| `cmdb` | `cmdb_ci`, `cmdb_ci_hardware`, `cmdb_rel_ci` | Raw and normalized live delivery |
| `csm` | `sn_customerservice_case`, `customer_contact` | Conditional; the current PDI returns `Invalid table` because CSM is not installed |
| `knowledge-management` | `kb_knowledge`, `kb_category` | Raw and normalized live delivery |
| `app-engine` | `sys_app`, tenant-defined custom table | Raw and normalized live delivery; custom requires `Table-Override` |
| `itom` | `em_event`, `em_alert` | Common Table API transport qualified; product entitlement may be absent in the validation PDI |
| `itam` | `alm_asset`, `alm_hardware` | Raw and normalized live delivery |
| `secops` | `sn_si_incident`, `sn_si_task`, `sn_si_audit_log` | Common Table API transport qualified; product entitlement may be absent in the validation PDI |
| `vulnerability-response` | `sn_vul_vulnerable_item`, `sn_vul_vulnerability` | Common Table API transport qualified; product entitlement may be absent in the validation PDI |
| `irm` | `sn_risk_risk`, `sn_grc_issue`, `sn_compliance_control`, `sn_compliance_policy` | Common Table API transport qualified; product entitlement may be absent in the validation PDI |
| `hrsd` | `sn_hr_core_case`, `sn_hr_core_task` | Common Table API transport qualified; sensitive-data ACLs and retention are tenant-owned |
| `spm` | `pm_project`, `pm_project_task`, `dmn_demand`, `pm_program`, `pm_portfolio` | Common Table API transport qualified; product entitlement may be absent in the validation PDI |
| `fsm` | `wm_order`, `wm_task` | Common Table API transport qualified; product entitlement may be absent in the validation PDI |
| `devops` | `sn_devops_event`, `sn_devops_inbound_event`, `sn_devops_pipeline_execution`, `sn_devops_step_execution`, `sn_devops_tool_connectivity_history` | Common Table API transport qualified; product entitlement may be absent in the validation PDI |

Use `API="all"` to expand all 54 portable catalog datasets in one stanza. The
tenant-defined App Engine custom table is intentionally opt-in because no
portable table name exists. `Product="all"` expands the full 55-dataset catalog
and therefore requires its `Table-Override`. Repeatable `API=` values select
exact catalog keys such as `itom-events`. `Table="incident"` and other real
ServiceNow table names provide exact-table selection and can be combined with
`Product=` or `API=`.

The live state above is bounded to the Zurich PDI and Gravwell 5.9.2. Twenty-eight
datasets have direct PDI-to-Gravwell evidence, exceeding the agreed seven-dataset
common-transport threshold. The remaining Table API datasets use the identical
request, pagination, cursor, framing, normalization, and tag code path and are
qualified by equivalence. This does not assert that another tenant has the same
plugins, roles, ACLs, fields, or licensed products.

## Validated non-Table REST collections

ServiceNow also publishes product-specific REST APIs. Their endpoint shapes are
not uniform: some expose GET collections, some are mixed read/write APIs, and
some are actions or inbound receivers rather than log sources. The following
named profiles each passed an exact response-shape test and live PDI GET:

| `API-Endpoint=` | Product | Exact path | Paging/identity contract |
|---|---|---|---|
| `aggregate-incidents` | ITSM | `/api/now/stats/incident` | Singleton object; stable ingester ID |
| `attachment-metadata` | Platform | `/api/now/attachment` | `sysparm_limit`/`sysparm_offset`; `sys_id` |
| `service-catalog-items` | ITSM | `/api/sn_sc/servicecatalog/items` | `sysparm_limit`/`sysparm_offset`; `sys_id` |
| `service-catalogs` | ITSM | `/api/sn_sc/servicecatalog/catalogs` | `sysparm_limit`/`sysparm_offset`; `sys_id` |
| `change-models` | ITSM | `/api/sn_chg_rest/v1/change/model` | Bounded collection; `sys_id` |
| `cmdb-instances` | CMDB | `/api/now/cmdb/instance/cmdb_ci` | `sysparm_limit`/`sysparm_offset`; `sys_id` |
| `scripted-rest-services` | App Engine | `/api/now/table/sys_ws_definition` | Bounded safe fields; Table API paging; `sys_id` |
| `scripted-rest-resources` | App Engine | `/api/now/table/sys_ws_operation` | Active GET resources; Table API paging; `sys_id` |

Use `API-Endpoint="all"` for all eight profiles, or repeat a specific name. This
is separate from `API="all"`, which retains the 54 portable Table API datasets.
For a tenant-specific documented GET collection, use the explicit JSON form:

```ini
API-Endpoint="custom-events={\"Product\":\"Custom Application\",\"Path\":\"/api/x_example/events\",\"Tag\":\"servicenow-custom-events\",\"Result_Path\":\"result\",\"ID_Field\":\"sys_id\",\"Timestamp\":\"sys_updated_on\",\"Limit_Parameter\":\"sysparm_limit\",\"Offset_Parameter\":\"sysparm_offset\",\"Required_Role\":\"documented read role\",\"Documentation\":\"https://www.servicenow.com/docs/r/api-reference/rest-api-explorer/c_CustomWebServices.html\"}"
```

The endpoint must be instance-relative beneath `/api/`; absolute URLs, query
fragments, and non-ServiceNow documentation URLs are rejected. The ingester
performs only GET requests, splits the configured result array into one compact
JSON object per Gravwell entry, follows same-origin endpoint-bound pagination,
and stores payload hashes so changed objects sharing an identifier are emitted
again. Fixed query parameters may be supplied in `Parameters`.

Unlike the common Table API catalog, every new custom `API-Endpoint` definition
has a unique result shape and pagination contract. It therefore requires its
own vendor response fixture and endpoint-specific test or live receipt before
that endpoint can be called production-ready. Write/action endpoints and
inbound webhooks are explicitly outside this polling form.

## Compatible Gravwell forms

- Hosted Runner: implemented and live-sanitized validated against Gravwell 5.9.2.
- Standalone Fetcher: implemented with the same catalog/client and statically validated; it is not enabled beside the Hosted Runner because that would duplicate collection.
- Shared Fetcher: one registration exists but is not enabled beside the Hosted Runner for the same reason.
- Stock Gravwell ingester: none is listed in the public Gravwell ingester documentation as of 2026-09-03.
- Documented REST GET collections: eight named profiles are endpoint-validated;
  additional custom profiles require their own schema and pagination proof.
- Push receiver: a separate ingestion process; not implemented by this polling
  ingester.

Authentication file formats:

```json
{"mode":"oauth-client-credentials","client_id":"REPLACE","client_secret":"REPLACE"}
```

Basic and pre-issued bearer modes are also supported. The secret file is read
again for each request so rotation does not require rebuilding the image.

Table and field ACLs on the instance are authoritative. `Skip-Unavailable`
isolates conditional product/table failures without disabling other tables.

Normalization adds canonical aliases but does not change the canonical
semantic product tag. The exact operation remains available through intrinsic
`_recordType`, `_source`, `_endpoint`, and `_apiVersion` values. Select one
representation per dataset in a deployment;
do not run raw and normalized stanzas for the same dataset concurrently because
that would mix two schemas on one tag and duplicate collection.

```ini
[ServiceNow "platform"]
Ingester-UUID="<unique UUID>"
Instance="https://<instance>.service-now.com"
Secret-File="/opt/gravwell/secrets/servicenow.json"
Product="platform"
Normalization="disabled"
Lookback=24
# Optional when normalization is enabled:
# Normalization-Field="users:userName=user.name|user_name"
```

`Normalization-Field` is repeatable and uses
`group:target=source1|source2`. The canonical target wins when it is already
populated. Otherwise, the first populated source wins. Divergent populated
sources add `_normalizationCollision` as intrinsic metadata without exposing
their values, and all original source fields remain intact. A target such as
`username` is distinct from the built-in lowerCamel `userName` target.

`Lookback` is an integer number of hours. It initializes a missing checkpoint only; durable state wins
after restart. Incremental Table API datasets retain their initial or
established lower-bound anchor on an empty poll and advance monotonically only
to the greatest fully accepted source ordering timestamp. Full-snapshot REST
profiles hash stable identities, emit changed content even when timestamps do
not change, and prune absent objects from local deduplication state without
emitting deletion tombstones. Per-item hashes and per-page continuation state
bound replay after partial failure; one entry accepted immediately before a
state-write failure may replay because the SDK does not expose an atomic
ingest-and-state transaction.

`Overlap` and `Max-Retries` default to 300 seconds and four retries when they
are omitted. An explicit zero disables overlap or retries. `Max-Retries` counts
attempts after the initial request.

Raw and normalized modes use separate state namespaces. Changing normalization
rules creates an intentionally separate state route and can replay records
within the configured lookback. Do not run the standalone or shared Fetcher
against the same tables while this stanza is enabled.

## Documentation authority

- Vendor transport and authentication: [ServiceNow Table API](https://www.servicenow.com/docs/r/api-reference/rest-apis/c_TableAPI.html), [Aggregate API](https://www.servicenow.com/docs/r/api-reference/rest-apis/c_AggregateAPI.html), [Attachment API](https://www.servicenow.com/docs/r/api-reference/rest-apis/c_AttachmentAPI.html), [Service Catalog API](https://www.servicenow.com/docs/r/api-reference/rest-apis/c_ServiceCatalogAPI.html), [Change Management API](https://www.servicenow.com/docs/r/api-reference/rest-apis/change-management-api.html), [CMDB Instance API](https://www.servicenow.com/docs/r/api-reference/rest-apis/cmdb-instance-api.html), [Scripted REST APIs](https://www.servicenow.com/docs/r/api-reference/rest-api-explorer/c_CustomWebServices.html), and [ServiceNow OAuth](https://www.servicenow.com/docs/bundle/yokohama-api-reference/page/integrate/inbound-rest/concept/c_OAuthAuthentication.html).
- Product/table sources: the exact primary URL for each catalog dataset is stored in `catalog.go`; the maintained operator ledger is in the product-specific ServiceNow integration guides.

These sources establish transport, authentication, table identity, state,
secret-file, framing, and backend delivery behavior. The target instance's
dictionary and sanitized live payload remain authoritative for optional fields.
