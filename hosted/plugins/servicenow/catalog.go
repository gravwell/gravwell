package servicenow

import (
	"fmt"
	"sort"
	"strings"
)

// Dataset is one explicit ServiceNow Table API source. RequiredRole is an
// operator-facing least-privilege hint; the instance's table/field ACLs remain
// authoritative and may omit fields the caller cannot read.
type Dataset struct {
	Name, Product, Table, Tag, Fields, Timestamp, Query, RequiredRole, Documentation string
	RequiresTableOverride                                                            bool
	REST                                                                             *RESTSpec
}

// RESTSpec describes a read-only JSON endpoint whose response is split into
// one compact Gravwell entry per object. Table API datasets leave REST nil.
type RESTSpec struct {
	Path, ResultPath, IDField, StaticID, LimitParameter, OffsetParameter string
	Parameters                                                           map[string]string
}

var catalog = map[string]Dataset{
	"audit":                      {"audit", "Audit", "sys_audit", "servicenow-audit", "sys_id,sys_created_on,sys_created_by,tablename,fieldname,documentkey,user,oldvalue,newvalue,reason,record_checkpoint", "sys_created_on", "", "read ACL for sys_audit (commonly admin/auditor)", auditDocs, false, nil},
	"audit-relations":            {"audit-relations", "Audit", "sys_audit_relation", "servicenow-audit", "sys_id,sys_created_on,documentkey,tablename,fieldname,oldvalue,newvalue", "sys_created_on", "", "read ACL for sys_audit_relation", auditDocs, false, nil},
	"system":                     {"system", "Platform", "syslog", "servicenow-platform", "sys_id,sys_created_on,sys_created_by,level,message,source", "sys_created_on", "", "read ACL for syslog", systemTableDocs, false, nil},
	"transactions":               {"transactions", "Platform", "syslog_transaction", "servicenow-platform", "sys_id,sys_created_on,sys_created_by,type,origin_application,response_time,network_time,output_length,sql_time,sql_count,business_rule_time,business_rule_count,url,client_ip,response_status,protocol,gzipped", "sys_created_on", "", "read ACL for syslog_transaction", systemTableDocs, false, nil},
	"email":                      {"email", "Platform", "sys_email", "servicenow-platform", "sys_id,sys_created_on,sys_created_by,type,state,subject,recipients,importance,target_table,instance", "sys_created_on", "", "read ACL for sys_email", systemTableDocs, false, nil},
	"users":                      {"users", "Platform", "sys_user", "servicenow-platform", "sys_id,sys_created_on,sys_updated_on,sys_updated_by,user_name,name,active,locked_out,password_needs_reset,web_service_access_only,internal_integration_user,last_login_time", "sys_updated_on", "", "read ACL for sys_user", identityAuditDocs, false, nil},
	"groups":                     {"groups", "Platform", "sys_user_group", "servicenow-platform", "sys_id,sys_created_on,sys_updated_on,sys_updated_by,name,description,active,manager,parent,type", "sys_updated_on", "", "read ACL for sys_user_group", identityAuditDocs, false, nil},
	"group-members":              {"group-members", "Platform", "sys_user_grmember", "servicenow-platform", "sys_id,sys_created_on,sys_updated_on,user,group", "sys_updated_on", "", "read ACL for sys_user_grmember", identityAuditDocs, false, nil},
	"user-roles":                 {"user-roles", "Platform", "sys_user_has_role", "servicenow-platform", "sys_id,sys_created_on,sys_updated_on,user,role,inherited,inh_count", "sys_updated_on", "", "read ACL for sys_user_has_role", identityAuditDocs, false, nil},
	"roles":                      {"roles", "Platform", "sys_user_role", "servicenow-platform", "sys_id,sys_created_on,sys_updated_on,name,description,elevated_privilege,assignable_by", "sys_updated_on", "", "read ACL for sys_user_role", identityAuditDocs, false, nil},
	"properties":                 {"properties", "Platform", "sys_properties", "servicenow-platform", "sys_id,sys_created_on,sys_updated_on,name,description,type,value,choices,ignore_cache,is_private", "sys_updated_on", "", "read ACL for sys_properties; sensitive fields may be omitted", tableDocs, false, nil},
	"acls":                       {"acls", "Platform", "sys_security_acl", "servicenow-platform", "sys_id,sys_created_on,sys_updated_on,name,operation,type,active,admin_overrides,advanced,condition,description", "sys_updated_on", "", "read ACL for sys_security_acl (commonly security_admin)", identityAuditDocs, false, nil},
	"incidents":                  {"incidents", "ITSM", "incident", "servicenow-itsm", taskFields, "sys_updated_on", "", "itil plus table/field read ACLs", taskTableDocs, false, nil},
	"problems":                   {"problems", "ITSM", "problem", "servicenow-itsm", taskFields, "sys_updated_on", "", "itil plus table/field read ACLs", problemTableDocs, false, nil},
	"changes":                    {"changes", "ITSM", "change_request", "servicenow-itsm", taskFields, "sys_updated_on", "", "itil plus table/field read ACLs", changeTableDocs, false, nil},
	"requests":                   {"requests", "ITSM", "sc_request", "servicenow-itsm", taskFields, "sys_updated_on", "", "itil plus table/field read ACLs", requestTableDocs, false, nil},
	"request-items":              {"request-items", "ITSM", "sc_req_item", "servicenow-itsm", taskFields, "sys_updated_on", "", "itil plus table/field read ACLs", requestTableDocs, false, nil},
	"slas":                       {"slas", "ITSM", "task_sla", "servicenow-itsm", "sys_id,task,sla,stage,has_breached,start_time,end_time,planned_end_time,original_breach_time,business_percentage,percentage,time_left,duration,sys_created_on,sys_updated_on", "sys_updated_on", "", "itil plus table/field read ACLs", taskSLADocs, false, nil},
	"approvals":                  {"approvals", "ITSM", "sysapproval_approver", "servicenow-itsm", "sys_id,sysapproval,document_id,approver,state,source_table,due_date,comments,sys_created_on,sys_updated_on", "sys_updated_on", "", "itil plus table/field read ACLs", approvalTableDocs, false, nil},
	"cmdb-cis":                   {"cmdb-cis", "CMDB", "cmdb_ci", "servicenow-cmdb-assets", "sys_id,name,sys_class_name,install_status,operational_status,company,location,assigned_to,managed_by,sys_created_on,sys_updated_on", "sys_updated_on", "", "read ACL for cmdb_ci (commonly itil)", cmdbDocs, false, nil},
	"cmdb-hardware":              {"cmdb-hardware", "CMDB", "cmdb_ci_hardware", "servicenow-cmdb-assets", "sys_id,name,asset,manufacturer,model_id,serial_number,install_status,operational_status,company,location,assigned_to,sys_created_on,sys_updated_on", "sys_updated_on", "", "read ACL for cmdb_ci_hardware", cmdbDocs, false, nil},
	"cmdb-relations":             {"cmdb-relations", "CMDB", "cmdb_rel_ci", "servicenow-cmdb-assets", "sys_id,parent,child,type,connection_strength,sys_created_on,sys_updated_on", "sys_updated_on", "", "read ACL for cmdb_rel_ci", cmdbDocs, false, nil},
	"csm-cases":                  {"csm-cases", "CSM", "sn_customerservice_case", "servicenow-csm", taskFields, "sys_updated_on", "", "CSM plugin plus csm_ws_integration and table/field ACLs", csmCaseDocs, false, nil},
	"csm-contacts":               {"csm-contacts", "CSM", "customer_contact", "servicenow-csm", "sys_id,name,first_name,last_name,email,phone,account,active,sys_created_on,sys_updated_on", "sys_updated_on", "", "CSM plugin plus contact table/field read ACLs", csmTableDocs, false, nil},
	"km-articles":                {"km-articles", "Knowledge Management", "kb_knowledge", "servicenow-km", "sys_id,number,short_description,text,workflow_state,kb_knowledge_base,kb_category,author,active,valid_to,sys_created_on,sys_updated_on", "sys_updated_on", "", "knowledge table read ACL and user criteria", knowledgeTableDocs, false, nil},
	"km-categories":              {"km-categories", "Knowledge Management", "kb_category", "servicenow-km", "sys_id,label,value,parent_id,active,sys_created_on,sys_updated_on", "sys_updated_on", "", "knowledge category read ACL", knowledgeCategoryDocs, false, nil},
	"app-engine-apps":            {"app-engine-apps", "App Engine", "sys_app", "servicenow-app-engine", "sys_id,name,scope,version,active,sys_created_on,sys_updated_on", "sys_updated_on", "", "read ACL for sys_app", appEngineApplicationDocs, false, nil},
	"app-engine-custom":          {"app-engine-custom", "App Engine", "CUSTOM_TABLE_NAME", "servicenow-app-engine", "", "sys_updated_on", "", "read ACL and web-service access for configured custom table", appEngineTableDocs, true, nil},
	"itom-events":                {"itom-events", "IT Operations Management", "em_event", "servicenow-itom", "sys_id,time_of_event,source,node,type,resource,metric_name,severity,description,additional_info,resolution_state,message_key,sys_created_on,sys_updated_on", "sys_updated_on", "", "evt_mgmt_user, evt_mgmt_operator, evt_mgmt_admin, or equivalent em_event read ACL", itomEventDocs, false, nil},
	"itom-alerts":                {"itom-alerts", "IT Operations Management", "em_alert", "servicenow-itom", "sys_id,number,source,node,type,resource,metric_name,severity,state,description,additional_info,opened_at,last_event_time,closed_at,sys_created_on,sys_updated_on", "sys_updated_on", "", "evt_mgmt_user or equivalent em_alert read ACL", itomAlertDocs, false, nil},
	"itam-assets":                {"itam-assets", "IT Asset Management", "alm_asset", "servicenow-itam", "sys_id,asset_tag,display_name,model,model_category,serial_number,install_status,substatus,assigned_to,managed_by,company,location,purchase_date,warranty_expiration,sys_created_on,sys_updated_on", "sys_updated_on", "", "asset or equivalent alm_asset read ACL", itamAssetDocs, false, nil},
	"itam-hardware":              {"itam-hardware", "IT Asset Management", "alm_hardware", "servicenow-itam", "sys_id,asset_tag,display_name,model,model_category,serial_number,install_status,substatus,assigned_to,managed_by,company,location,purchase_date,warranty_expiration,sys_created_on,sys_updated_on", "sys_updated_on", "", "asset or equivalent alm_hardware read ACL", itamHardwareDocs, false, nil},
	"secops-incidents":           {"secops-incidents", "Security Operations", "sn_si_incident", "servicenow-secops", taskFields + ",severity,risk_score,category,subcategory,source,affected_users,affected_cis", "sys_updated_on", "", "sn_si.read or an equivalent Security Incident read role", secopsDocs, false, nil},
	"secops-tasks":               {"secops-tasks", "Security Operations", "sn_si_task", "servicenow-secops", taskFields, "sys_updated_on", "", "sn_si.read or an equivalent Security Incident task read role", secopsDocs, false, nil},
	"secops-audit":               {"secops-audit", "Security Operations", "sn_si_audit_log", "servicenow-secops", "sys_id,security_incident,action,source,description,state,sys_created_on,sys_created_by,sys_updated_on,sys_updated_by", "sys_updated_on", "", "Security Incident audit-log read ACL", secopsDocs, false, nil},
	"vulnerability-items":        {"vulnerability-items", "Vulnerability Response", "sn_vul_vulnerable_item", "servicenow-vulnerability-response", "sys_id,number,active,state,priority,risk_score,risk_rating,severity,source,configuration_item,vulnerability,assignment_group,assigned_to,first_found,last_found,closed_at,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_vul.read_all or an equivalent vulnerable-item read role", vulnerabilityItemDocs, false, nil},
	"vulnerability-groups":       {"vulnerability-groups", "Vulnerability Response", "sn_vul_vulnerability", "servicenow-vulnerability-response", "sys_id,number,active,state,priority,risk_score,risk_rating,severity,source,assignment_group,assigned_to,opened_at,closed_at,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_vul.read_all or an equivalent vulnerability-group read role", vulnerabilityDashboardDocs, false, nil},
	"irm-risks":                  {"irm-risks", "Integrated Risk Management", "sn_risk_risk", "servicenow-irm", "sys_id,number,name,description,state,risk_rating,risk_score,likelihood,impact,owner,profile,assignment_group,assigned_to,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_risk.reader or equivalent sn_risk_risk read ACL", irmDashboardDocs, false, nil},
	"irm-issues":                 {"irm-issues", "Integrated Risk Management", "sn_grc_issue", "servicenow-irm", "sys_id,number,short_description,description,state,priority,risk_rating,source,profile,assignment_group,assigned_to,due_date,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_grc.reader or equivalent sn_grc_issue read ACL", irmDashboardDocs, false, nil},
	"irm-controls":               {"irm-controls", "Integrated Risk Management", "sn_compliance_control", "servicenow-irm", "sys_id,number,name,description,state,status,compliance,owner,profile,policy,assignment_group,assigned_to,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_compliance.reader or equivalent control read ACL", irmComplianceDocs, false, nil},
	"irm-policies":               {"irm-policies", "Integrated Risk Management", "sn_compliance_policy", "servicenow-irm", "sys_id,number,name,short_description,description,state,status,owner,valid_from,valid_to,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_compliance.reader or equivalent policy read ACL", irmComplianceDocs, false, nil},
	"hrsd-cases":                 {"hrsd-cases", "HR Service Delivery", "sn_hr_core_case", "servicenow-hrsd", "sys_id,number,active,state,priority,assignment_group,assigned_to,hr_service,opened_at,closed_at,sys_created_on,sys_updated_on", "sys_updated_on", "", "a bounded HR case reader role and field ACLs; HR data is sensitive", hrsdDocs, false, nil},
	"hrsd-tasks":                 {"hrsd-tasks", "HR Service Delivery", "sn_hr_core_task", "servicenow-hrsd", "sys_id,number,active,state,priority,assignment_group,assigned_to,parent,opened_at,closed_at,sys_created_on,sys_updated_on", "sys_updated_on", "", "a bounded HR task reader role and field ACLs; HR data is sensitive", hrsdDocs, false, nil},
	"spm-projects":               {"spm-projects", "Strategic Portfolio Management", "pm_project", "servicenow-spm", "sys_id,number,short_description,description,active,state,status,priority,percent_complete,manager,portfolio,program,start_date,end_date,sys_created_on,sys_updated_on", "sys_updated_on", "", "project reader or equivalent pm_project read ACL", spmDocs, false, nil},
	"spm-project-tasks":          {"spm-project-tasks", "Strategic Portfolio Management", "pm_project_task", "servicenow-spm", "sys_id,number,short_description,description,active,state,status,priority,percent_complete,project,assigned_to,start_date,end_date,sys_created_on,sys_updated_on", "sys_updated_on", "", "project reader or equivalent pm_project_task read ACL", spmDocs, false, nil},
	"spm-demands":                {"spm-demands", "Strategic Portfolio Management", "dmn_demand", "servicenow-spm", "sys_id,number,short_description,description,active,state,stage,priority,risk,submitter,portfolio,program,start_date,due_date,sys_created_on,sys_updated_on", "sys_updated_on", "", "demand reader or equivalent dmn_demand read ACL", spmDocs, false, nil},
	"spm-programs":               {"spm-programs", "Strategic Portfolio Management", "pm_program", "servicenow-spm", "sys_id,number,name,short_description,description,active,state,status,manager,portfolio,start_date,end_date,sys_created_on,sys_updated_on", "sys_updated_on", "", "program reader or equivalent pm_program read ACL", spmDocs, false, nil},
	"spm-portfolios":             {"spm-portfolios", "Strategic Portfolio Management", "pm_portfolio", "servicenow-spm", "sys_id,number,name,short_description,description,active,state,status,manager,start_date,end_date,sys_created_on,sys_updated_on", "sys_updated_on", "", "portfolio reader or equivalent pm_portfolio read ACL", spmDocs, false, nil},
	"fsm-work-orders":            {"fsm-work-orders", "Field Service Management", "wm_order", "servicenow-fsm", taskFields + ",work_type,location,company,customer_account", "sys_updated_on", "", "wm_read or equivalent wm_order read ACL", fsmDocs, false, nil},
	"fsm-work-order-tasks":       {"fsm-work-order-tasks", "Field Service Management", "wm_task", "servicenow-fsm", taskFields + ",parent,work_type,location,company,customer_account", "sys_updated_on", "", "wm_read or equivalent wm_task read ACL", fsmDocs, false, nil},
	"devops-events":              {"devops-events", "DevOps", "sn_devops_event", "servicenow-devops", "sys_id,tool,type,source,native_id,state,payload,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_devops.viewer or equivalent event read ACL", devopsComponentsDocs, false, nil},
	"devops-inbound-events":      {"devops-inbound-events", "DevOps", "sn_devops_inbound_event", "servicenow-devops", "sys_id,tool,capability,state,payload,error_message,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_devops.viewer or equivalent inbound-event read ACL", devopsComponentsDocs, false, nil},
	"devops-pipeline-executions": {"devops-pipeline-executions", "DevOps", "sn_devops_pipeline_execution", "servicenow-devops", "sys_id,pipeline,number,state,status,result,start_time,end_time,duration,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_devops.viewer or equivalent pipeline-execution read ACL", devopsComponentsDocs, false, nil},
	"devops-step-executions":     {"devops-step-executions", "DevOps", "sn_devops_step_execution", "servicenow-devops", "sys_id,step,pipeline_execution,state,status,result,start_time,end_time,duration,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_devops.viewer or equivalent step-execution read ACL", devopsComponentsDocs, false, nil},
	"devops-tool-connectivity":   {"devops-tool-connectivity", "DevOps", "sn_devops_tool_connectivity_history", "servicenow-devops", "sys_id,tool,status,message,last_successful_connection,sys_created_on,sys_updated_on", "sys_updated_on", "", "sn_devops.viewer or equivalent tool-connectivity read ACL", devopsCloneDocs, false, nil},
}

var productAliases = map[string]string{
	"all":                            "all",
	"platform":                       "Platform",
	"now-platform":                   "Platform",
	"audit":                          "Audit",
	"itsm":                           "ITSM",
	"it-service-management":          "ITSM",
	"cmdb":                           "CMDB",
	"cmdb-assets":                    "CMDB",
	"csm":                            "CSM",
	"customer-service-management":    "CSM",
	"knowledge-management":           "Knowledge Management",
	"km":                             "Knowledge Management",
	"app-engine":                     "App Engine",
	"itom":                           "IT Operations Management",
	"it-operations-management":       "IT Operations Management",
	"itam":                           "IT Asset Management",
	"it-asset-management":            "IT Asset Management",
	"secops":                         "Security Operations",
	"security-operations":            "Security Operations",
	"vulnerability-response":         "Vulnerability Response",
	"vr":                             "Vulnerability Response",
	"irm":                            "Integrated Risk Management",
	"grc":                            "Integrated Risk Management",
	"integrated-risk-management":     "Integrated Risk Management",
	"hrsd":                           "HR Service Delivery",
	"hr-service-delivery":            "HR Service Delivery",
	"spm":                            "Strategic Portfolio Management",
	"strategic-portfolio-management": "Strategic Portfolio Management",
	"fsm":                            "Field Service Management",
	"field-service-management":       "Field Service Management",
	"devops":                         "DevOps",
}

const tableDocs = "https://www.servicenow.com/docs/r/api-reference/rest-apis/c_TableAPI.html"
const auditDocs = "https://www.servicenow.com/docs/r/platform-security/c_UnderstandingTheSysAuditTable.html"
const systemTableDocs = "https://www.servicenow.com/docs/r/now-intelligence/reporting/c_ReportOnSystemTables.html"
const identityAuditDocs = "https://www.servicenow.com/docs/r/platform-security/identity/explore-identity-audit.html"
const taskTableDocs = "https://www.servicenow.com/docs/r/platform-administration/table-administration-and-data-management/c_TaskTable.html"
const problemTableDocs = "https://www.servicenow.com/docs/r/it-service-management/problem-management/installed-with-pm.html"
const changeTableDocs = "https://www.servicenow.com/docs/r/BgFKZnPHldZ62gGtzQ71Mw/NWFNVuJrJMHXqx~5CdLZxA"
const requestTableDocs = "https://www.servicenow.com/docs/r/it-service-management/request-management/request-management-architecture.html"
const taskSLADocs = "https://www.servicenow.com/docs/r/it-service-management/service-level-management/r_TaskSLATable.html"
const approvalTableDocs = "https://www.servicenow.com/docs/r/platform-user-interface/service-portal/approvals-widget.html"
const cmdbDocs = "https://www.servicenow.com/docs/r/servicenow-platform/configuration-management-database-cmdb/c_ConfigurationManagementDatabase.html"
const csmCaseDocs = "https://www.servicenow.com/docs/r/api-reference/rest-apis/case-api.html"
const csmTableDocs = "https://www.servicenow.com/docs/r/customer-service-management/r_TIWCustomerService.html"
const knowledgeTableDocs = "https://www.servicenow.com/docs/r/4AA65edJ2a~o5pASflQppQ/ASQNrPLnSf4smAUIFHyHDA"
const knowledgeCategoryDocs = "https://www.servicenow.com/docs/r/servicenow-platform/knowledge-management/t_DefineAKnowledgeCategory.html"
const appEngineApplicationDocs = "https://www.servicenow.com/docs/r/application-development/app-config-source-code.html"
const appEngineTableDocs = "https://www.servicenow.com/docs/r/application-development/app-engine-studio/app-tutorial-create-table.html"
const itomEventDocs = "https://www.servicenow.com/docs/r/it-operations-management/event-management/t_EMManageEvent.html"
const itomAlertDocs = "https://www.servicenow.com/docs/r/it-operations-management/event-management/r_InstalledWithEventManagement.html"
const itamAssetDocs = "https://www.servicenow.com/docs/r/it-asset-management/hardware-asset-management/read-only-fields-ham.html"
const itamHardwareDocs = "https://www.servicenow.com/docs/r/it-asset-management/asset-management/t_CreateAssetandCIFieldsMapping.html"
const secopsDocs = "https://www.servicenow.com/docs/r/security-management/security-incident-response/installed-with-sir.html"
const vulnerabilityItemDocs = "https://www.servicenow.com/docs/r/security-management/vulnerability-response/enable-auditing-selected-fields-vuln-items-table.html"
const vulnerabilityDashboardDocs = "https://www.servicenow.com/docs/r/security-management/vulnerability-response/vulnerability-mgmnt-pa-dashboard.html"
const irmDashboardDocs = "https://www.servicenow.com/docs/r/security-management/grc-ced-risk-compliance-db-reports.html"
const irmComplianceDocs = "https://www.servicenow.com/docs/r/governance-risk-compliance/policy-and-compliance-management/r_InstallWPolAndCompl.html"
const hrsdDocs = "https://www.servicenow.com/docs/r/employee-service-management/hr-service-delivery/components-installed-with-case-and-knowledge-management.html"
const spmDocs = "https://www.servicenow.com/docs/r/it-business-management/strategic-portfolio-management/supported-tables-for-partition-ewd.html"
const fsmDocs = "https://www.servicenow.com/docs/r/field-service-management/r_TableInstallWFieldServMgmnt.html"
const devopsComponentsDocs = "https://www.servicenow.com/docs/r/it-service-management/devops-change-velocity/installed-with-dev-ops.html"
const devopsCloneDocs = "https://www.servicenow.com/docs/r/it-service-management/devops-change-velocity/devops-cloning.html"
const taskFields = "sys_id,number,active,state,impact,urgency,priority,assignment_group,assigned_to,short_description,description,opened_at,resolved_at,closed_at,sys_created_on,sys_created_by,sys_updated_on,sys_updated_by,sys_mod_count"

func Catalog() []Dataset {
	result := make([]Dataset, 0, len(catalog))
	for _, dataset := range catalog {
		result = append(result, dataset)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func ResolveDatasets(names []string) ([]Dataset, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one dataset is required")
	}
	seen := map[string]bool{}
	var result []Dataset
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "all" {
			for _, d := range Catalog() {
				if !d.RequiresTableOverride && !seen[d.Name] {
					result = append(result, d)
					seen[d.Name] = true
				}
			}
			continue
		}
		d, ok := catalog[name]
		if !ok {
			return nil, fmt.Errorf("unknown ServiceNow dataset %q", raw)
		}
		if !seen[name] {
			result = append(result, d)
			seen[name] = true
		}
	}
	return result, nil
}

// ResolveProducts expands ServiceNow product names into the exact catalog
// datasets owned by each product. Product=all includes the tenant-specific App
// Engine custom-table route; Config.Datasets requires a matching table override
// before collection can start.
func ResolveProducts(names []string) ([]Dataset, error) {
	if len(names) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var result []Dataset
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		product, ok := productAliases[name]
		if !ok {
			return nil, fmt.Errorf("unknown ServiceNow Product %q", raw)
		}
		for _, d := range Catalog() {
			if product != "all" && d.Product != product {
				continue
			}
			if !seen[d.Name] {
				result = append(result, d)
				seen[d.Name] = true
			}
		}
	}
	return result, nil
}

// ResolveTables selects catalog datasets by their actual ServiceNow table
// names, such as incident, sys_user, or cmdb_ci. Tenant-defined tables use the
// App Engine product plus Table-Override because their names are not portable.
func ResolveTables(names []string) ([]Dataset, error) {
	if len(names) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var result []Dataset
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		var match *Dataset
		for _, d := range Catalog() {
			if strings.ToLower(d.Table) == name {
				candidate := d
				match = &candidate
				break
			}
		}
		if match == nil {
			return nil, fmt.Errorf("unknown or tenant-specific ServiceNow Table %q", raw)
		}
		if !seen[match.Name] {
			result = append(result, *match)
			seen[match.Name] = true
		}
	}
	return result, nil
}
