package servicenow

import "sort"

const (
	aggregateDocs      = "https://www.servicenow.com/docs/r/api-reference/rest-apis/c_AggregateAPI.html"
	attachmentDocs     = "https://www.servicenow.com/docs/r/api-reference/rest-apis/c_AttachmentAPI.html"
	serviceCatalogDocs = "https://www.servicenow.com/docs/r/api-reference/rest-apis/c_ServiceCatalogAPI.html"
	changeAPIDocs      = "https://www.servicenow.com/docs/r/api-reference/rest-apis/change-management-api.html"
	cmdbInstanceDocs   = "https://www.servicenow.com/docs/r/api-reference/rest-apis/cmdb-instance-api.html"
	scriptedRESTDocs   = "https://www.servicenow.com/docs/r/api-reference/rest-api-explorer/c_CustomWebServices.html"
)

// endpointCatalog contains only exact read-only endpoint profiles whose path,
// result shape, identity, pagination, and authentication behavior have passed
// fixture tests and a live PDI request. Operators can select one by name or use
// API-Endpoint="all" without embedding JSON in the Hosted Runner config.
var endpointCatalog = map[string]APIEndpoint{
	"aggregate-incidents": {
		Product: "ITSM", Path: "/api/now/stats/incident", Tag: "servicenow-itsm",
		Result_Path: "result", Static_ID: "incident-aggregate", Timestamp: "sys_updated_on",
		Required_Role: "read ACL for incident (commonly itil)", Documentation: aggregateDocs,
		Parameters: map[string]string{"sysparm_count": "true"},
	},
	"attachment-metadata": {
		Product: "Platform", Path: "/api/now/attachment", Tag: "servicenow-platform",
		Result_Path: "result", ID_Field: "sys_id", Timestamp: "sys_updated_on",
		Limit_Parameter: "sysparm_limit", Offset_Parameter: "sysparm_offset",
		Required_Role: "read ACL for sys_attachment", Documentation: attachmentDocs,
	},
	"service-catalog-items": {
		Product: "ITSM", Path: "/api/sn_sc/servicecatalog/items", Tag: "servicenow-itsm",
		Result_Path: "result", ID_Field: "sys_id", Timestamp: "sys_updated_on",
		Limit_Parameter: "sysparm_limit", Offset_Parameter: "sysparm_offset",
		Required_Role: "catalog visibility and item read access", Documentation: serviceCatalogDocs,
	},
	"service-catalogs": {
		Product: "ITSM", Path: "/api/sn_sc/servicecatalog/catalogs", Tag: "servicenow-itsm",
		Result_Path: "result", ID_Field: "sys_id", Timestamp: "sys_updated_on",
		Limit_Parameter: "sysparm_limit", Offset_Parameter: "sysparm_offset",
		Required_Role: "catalog visibility and catalog read access", Documentation: serviceCatalogDocs,
	},
	"change-models": {
		Product: "ITSM", Path: "/api/sn_chg_rest/v1/change/model", Tag: "servicenow-itsm",
		Result_Path: "result", ID_Field: "sys_id", Timestamp: "sys_updated_on",
		Required_Role: "change_manager, itil, sn_change_read, or admin", Documentation: changeAPIDocs,
	},
	"cmdb-instances": {
		Product: "CMDB", Path: "/api/now/cmdb/instance/cmdb_ci", Tag: "servicenow-cmdb-assets",
		Result_Path: "result", ID_Field: "sys_id", Timestamp: "sys_updated_on",
		Limit_Parameter: "sysparm_limit", Offset_Parameter: "sysparm_offset",
		Required_Role: "itil plus CMDB class read ACL", Documentation: cmdbInstanceDocs,
	},
	"scripted-rest-services": {
		Product: "App Engine", Path: "/api/now/table/sys_ws_definition", Tag: "servicenow-app-engine",
		Result_Path: "result", ID_Field: "sys_id", Timestamp: "sys_updated_on",
		Limit_Parameter: "sysparm_limit", Offset_Parameter: "sysparm_offset",
		Required_Role: "web_service_admin plus table and field ACLs", Documentation: scriptedRESTDocs,
		Parameters: map[string]string{
			"sysparm_exclude_reference_link": "true",
			"sysparm_fields":                 "sys_id,sys_created_on,sys_updated_on,name,namespace,service_id,base_uri,active,is_versioned,default_version",
		},
	},
	"scripted-rest-resources": {
		Product: "App Engine", Path: "/api/now/table/sys_ws_operation", Tag: "servicenow-app-engine",
		Result_Path: "result", ID_Field: "sys_id", Timestamp: "sys_updated_on",
		Limit_Parameter: "sysparm_limit", Offset_Parameter: "sysparm_offset",
		Required_Role: "web_service_admin plus table and field ACLs", Documentation: scriptedRESTDocs,
		Parameters: map[string]string{
			"sysparm_exclude_reference_link": "true",
			"sysparm_fields":                 "sys_id,sys_created_on,sys_updated_on,name,http_method,operation_uri,relative_path,active,requires_authentication,requires_acl_authorization",
			"sysparm_query":                  "http_method=GET^active=true",
		},
	},
}

func endpointNames() []string {
	names := make([]string, 0, len(endpointCatalog))
	for name := range endpointCatalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// EndpointCatalog returns the named endpoint profiles in deterministic order.
func EndpointCatalog() []Dataset {
	names := endpointNames()
	result := make([]Dataset, 0, len(names))
	for _, name := range names {
		result = append(result, endpointDataset(name, endpointCatalog[name]))
	}
	return result
}
