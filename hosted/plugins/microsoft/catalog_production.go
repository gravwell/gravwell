package microsoft

// This file binds the exact public-kit tag contract to the existing shared
// Microsoft Hosted Runner. Selectors remain independently configurable so an
// operator can enable only the bounded sources required by an installed kit.
func init() {
	addActivityCategories()
	addAzureInventory()
	addDefenderEndpointInventory()
	addDefenderXDR()
	addEntraGovernance()

	selectorGroups["azure-activity-kit"] = []string{
		"azure-activity", "azure-activity-administrative", "azure-activity-security",
		"azure-activity-service-health", "azure-activity-resource-health", "azure-activity-alert",
		"azure-activity-recommendation", "azure-activity-policy", "azure-activity-autoscale",
	}
	selectorGroups["microsoft-azure-kit"] = []string{
		"azure-kit-activity", "azure-app-registrations", "azure-conditional-access", "azure-defender-alerts",
		"azure-defender-assessments", "azure-groups", "azure-key-vaults", "azure-managed-identities",
		"azure-management-groups", "azure-network-security-groups", "azure-policy-assignments",
		"azure-policy-definitions", "azure-policy-exemptions", "azure-public-ips", "azure-resource-groups",
		"azure-kit-resources", "azure-role-assignments", "azure-role-definitions", "azure-security-recommendations",
		"azure-service-principals", "azure-storage-accounts", "azure-subscriptions", "azure-users",
		"azure-virtual-machines", "azure-virtual-networks",
	}
	selectorGroups["microsoft-defender-kit"] = []string{
		"defender-actions", "defender-alerts", "defender-devices", "defender-exposure", "defender-incidents",
		"defender-indicators", "defender-investigations", "defender-machine-groups", "defender-recommendations",
		"defender-secure-score-kit", "defender-software", "defender-users", "defender-vulnerabilities",
	}
	selectorGroups["microsoft-defender-xdr-kit"] = []string{
		"defender-xdr-kit-incidents", "defender-xdr-kit-alerts", "defender-xdr-alert-evidence",
		"defender-xdr-device-events", "defender-xdr-device-process-events", "defender-xdr-device-network-events",
		"defender-xdr-device-file-events", "defender-xdr-device-registry-events", "defender-xdr-device-logon-events",
		"defender-xdr-email-events", "defender-xdr-email-attachment-info", "defender-xdr-email-url-info",
		"defender-xdr-url-click-events", "defender-xdr-cloud-app-events", "defender-xdr-identity-logon-events",
		"defender-xdr-identity-query-events", "defender-xdr-vulnerability",
	}
	selectorGroups["microsoft-entra-id-kit"] = []string{
		"entra-access-reviews", "entra-applications", "entra-directory-audits", "entra-conditional-access-policies",
		"entra-devices", "entra-directory-roles", "entra-groups", "entra-pim-assignments", "entra-risk-detections",
		"entra-risky-users", "entra-service-principals", "entra-signins", "entra-users",
	}
}

func addActivityCategories() {
	for selector, category := range map[string]string{
		"azure-activity-administrative": "Administrative", "azure-activity-security": "Security",
		"azure-activity-service-health": "ServiceHealth", "azure-activity-resource-health": "ResourceHealth",
		"azure-activity-alert": "Alert", "azure-activity-recommendation": "Recommendation",
		"azure-activity-policy": "Policy", "azure-activity-autoscale": "Autoscale",
	} {
		datasets[selector] = Dataset{Name: selector, Product: "Microsoft Azure", TagGroup: selector,
			Service: ServiceARM, Kind: KindAzureActivity, Mode: ModeAppend,
			Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Insights/eventtypes/management/values?api-version=2015-04-01",
			Scope: "https://management.azure.com/.default", TimeField: "eventTimestamp", IDField: "eventDataId",
			Filter: "category eq '" + category + "'", Permission: "Azure RBAC Reader",
			Description: "Azure Activity Log " + category + " records"}
	}
}

func addAzureInventory() {
	const armScope = "https://management.azure.com/.default"
	const graphScope = "https://graph.microsoft.com/.default"
	add := func(name, tag string, service Service, path, permission string) {
		datasets[name] = Dataset{Name: name, Product: "Microsoft Azure", TagGroup: tag, Service: service,
			Kind: KindODataGET, Mode: ModeSnapshot, Path: path, Scope: map[Service]string{ServiceARM: armScope, ServiceGraph: graphScope}[service],
			IDField: "id", Permission: permission, Description: "Microsoft Azure " + tag + " inventory"}
	}
	datasets["azure-kit-activity"] = Dataset{Name: "azure-kit-activity", Product: "Microsoft Azure", TagGroup: "azure-activity",
		Service: ServiceARM, Kind: KindAzureActivity, Mode: ModeAppend,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Insights/eventtypes/management/values?api-version=2015-04-01",
		Scope: armScope, TimeField: "eventTimestamp", IDField: "eventDataId", Permission: "Azure RBAC Reader", Description: "Azure Activity Log records"}
	add("azure-app-registrations", "azure-app-registrations", ServiceGraph, "/v1.0/applications?$select=id,appId,displayName,requiredResourceAccess,createdDateTime", "Application.Read.All")
	add("azure-conditional-access", "azure-conditional-access", ServiceGraph, "/v1.0/identity/conditionalAccess/policies", "Policy.Read.All")
	add("azure-defender-alerts", "azure-defender-alerts", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Security/alerts?api-version=2022-01-01", "Security Reader")
	add("azure-defender-assessments", "azure-defender-assessments", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Security/assessments?api-version=2020-01-01", "Security Reader")
	add("azure-groups", "azure-groups", ServiceGraph, "/v1.0/groups?$select=id,displayName,securityEnabled,mailEnabled,createdDateTime", "Group.Read.All")
	add("azure-key-vaults", "azure-key-vaults", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.KeyVault/vaults?api-version=2024-11-01", "Azure RBAC Reader")
	add("azure-managed-identities", "azure-managed-identities", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.ManagedIdentity/userAssignedIdentities?api-version=2023-01-31", "Azure RBAC Reader")
	add("azure-management-groups", "azure-management-groups", ServiceARM, "/providers/Microsoft.Management/managementGroups?api-version=2020-05-01", "Azure RBAC Reader")
	add("azure-network-security-groups", "azure-network-security-groups", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Network/networkSecurityGroups?api-version=2025-05-01", "Azure RBAC Reader")
	add("azure-policy-assignments", "azure-policy-assignments", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/policyAssignments?api-version=2025-03-01", "Azure RBAC Reader")
	add("azure-policy-definitions", "azure-policy-definitions", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/policyDefinitions?api-version=2025-01-01", "Azure RBAC Reader")
	add("azure-policy-exemptions", "azure-policy-exemptions", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/policyExemptions?api-version=2022-07-01-preview", "Azure RBAC Reader")
	add("azure-public-ips", "azure-public-ips", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Network/publicIPAddresses?api-version=2025-05-01", "Azure RBAC Reader")
	add("azure-resource-groups", "azure-resource-groups", ServiceARM, "/subscriptions/{subscriptionId}/resourcegroups?api-version=2021-04-01", "Azure RBAC Reader")
	add("azure-kit-resources", "azure-resources", ServiceARM, "/subscriptions/{subscriptionId}/resources?api-version=2021-04-01", "Azure RBAC Reader")
	add("azure-role-assignments", "azure-role-assignments", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/roleAssignments?api-version=2022-04-01", "Azure RBAC Reader")
	add("azure-role-definitions", "azure-role-definitions", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/roleDefinitions?api-version=2022-04-01", "Azure RBAC Reader")
	add("azure-security-recommendations", "azure-security-recommendations", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Security/assessments?api-version=2020-01-01", "Security Reader")
	add("azure-service-principals", "azure-service-principals", ServiceGraph, "/v1.0/servicePrincipals?$select=id,appId,displayName,accountEnabled,servicePrincipalType", "Application.Read.All")
	add("azure-storage-accounts", "azure-storage-accounts", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Storage/storageAccounts?api-version=2023-01-01", "Azure RBAC Reader")
	add("azure-subscriptions", "azure-subscriptions", ServiceARM, "/subscriptions?api-version=2022-12-01", "Azure RBAC Reader")
	add("azure-users", "azure-users", ServiceGraph, "/v1.0/users?$select=id,displayName,userPrincipalName,accountEnabled,createdDateTime", "User.Read.All")
	add("azure-virtual-machines", "azure-virtual-machines", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Compute/virtualMachines?api-version=2025-04-01", "Azure RBAC Reader")
	add("azure-virtual-networks", "azure-virtual-networks", ServiceARM, "/subscriptions/{subscriptionId}/providers/Microsoft.Network/virtualNetworks?api-version=2025-05-01", "Azure RBAC Reader")
}

func addDefenderEndpointInventory() {
	add := func(name, tag, path, permission, timeField string) {
		datasets[name] = Dataset{Name: name, Product: "Microsoft Defender for Endpoint", TagGroup: tag,
			Service: ServiceDefender, Kind: KindODataGET, Mode: ModeLifecycle, Path: path,
			Scope: "https://api.securitycenter.microsoft.com/.default", TimeField: timeField, IDField: "id",
			Permission: permission, Description: "Microsoft Defender for Endpoint " + tag}
	}
	add("defender-actions", "microsoft-defender-actions", "/api/machineactions", "Machine.ReadWrite.All", "lastUpdateTimeUtc")
	add("defender-alerts", "microsoft-defender-alerts", "/api/alerts", "Alert.Read.All", "lastUpdateTime")
	add("defender-devices", "microsoft-defender-devices", "/api/machines", "Machine.Read.All", "lastSeen")
	add("defender-incidents", "microsoft-defender-incidents", "/api/incidents", "Incident.Read.All", "lastUpdateTime")
	incident := datasets["defender-incidents"]
	incident.Scope = "https://api.security.microsoft.com/.default"
	datasets[incident.Name] = incident
	add("defender-indicators", "microsoft-defender-indicators", "/api/indicators", "Ti.Read.All", "lastUpdatedTime")
	add("defender-investigations", "microsoft-defender-investigations", "/api/investigations", "AdvancedQuery.Read.All", "lastUpdateTime")
	add("defender-machine-groups", "microsoft-defender-machine-groups", "/api/machinegroups", "Machine.Read.All", "")
	add("defender-recommendations", "microsoft-defender-recommendations", "/api/recommendations", "SecurityRecommendation.Read.All", "")
	add("defender-software", "microsoft-defender-software", "/api/Software", "Software.Read.All", "")
	add("defender-vulnerabilities", "microsoft-defender-vulnerabilities", "/api/vulnerabilities", "Vulnerability.Read.All", "updatedOn")
	datasets["defender-exposure"] = datasets["defender-endpoint-exposure-score"]
	d := datasets["defender-exposure"]
	d.Name, d.TagGroup = "defender-exposure", "microsoft-defender-exposure"
	datasets["defender-exposure"] = d
	datasets["defender-secure-score-kit"] = datasets["defender-secure-scores"]
	d = datasets["defender-secure-score-kit"]
	d.Name, d.TagGroup = "defender-secure-score-kit", "microsoft-defender-secure-score"
	datasets["defender-secure-score-kit"] = d
	datasets["defender-users"] = Dataset{Name: "defender-users", Product: "Microsoft Defender for Endpoint", TagGroup: "microsoft-defender-users",
		Service: ServiceDefender, Kind: KindParentChild, Mode: ModeLifecycle,
		ParentPath: "/api/machines", ChildPath: "/api/machines/{parentId}/logonusers",
		Scope: "https://api.securitycenter.microsoft.com/.default", IDField: "accountName", Permission: "User.Read.All",
		Description: "Users observed logged on to Microsoft Defender for Endpoint devices"}
}

func addDefenderXDR() {
	const graphScope = "https://graph.microsoft.com/.default"
	datasets["defender-xdr-kit-incidents"] = Dataset{Name: "defender-xdr-kit-incidents", Product: "Microsoft Defender XDR", TagGroup: "microsoft-defender-xdr-incident",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeLifecycle, Path: "/v1.0/security/incidents", Scope: graphScope,
		TimeField: "lastUpdateDateTime", IDField: "id", Permission: "SecurityIncident.Read.All", Description: "Microsoft Defender XDR incidents"}
	datasets["defender-xdr-kit-alerts"] = Dataset{Name: "defender-xdr-kit-alerts", Product: "Microsoft Defender XDR", TagGroup: "microsoft-defender-xdr-alert",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeLifecycle, Path: "/v1.0/security/alerts_v2", Scope: graphScope,
		TimeField: "lastUpdateDateTime", IDField: "id", Permission: "SecurityAlert.Read.All", Description: "Microsoft Defender XDR alerts"}
	tables := map[string]string{
		"defender-xdr-alert-evidence": "AlertEvidence", "defender-xdr-device-events": "DeviceEvents",
		"defender-xdr-device-process-events": "DeviceProcessEvents", "defender-xdr-device-network-events": "DeviceNetworkEvents",
		"defender-xdr-device-file-events": "DeviceFileEvents", "defender-xdr-device-registry-events": "DeviceRegistryEvents",
		"defender-xdr-device-logon-events": "DeviceLogonEvents", "defender-xdr-email-events": "EmailEvents",
		"defender-xdr-email-attachment-info": "EmailAttachmentInfo", "defender-xdr-email-url-info": "EmailUrlInfo",
		"defender-xdr-url-click-events": "UrlClickEvents", "defender-xdr-cloud-app-events": "CloudAppEvents",
		"defender-xdr-identity-logon-events": "IdentityLogonEvents", "defender-xdr-identity-query-events": "IdentityQueryEvents",
		"defender-xdr-vulnerability": "DeviceTvmSoftwareVulnerabilities",
	}
	for selector, table := range tables {
		tag := "microsoft-defender-" + selector[len("defender-xdr-"):]
		if selector == "defender-xdr-alert-evidence" {
			tag = "microsoft-defender-xdr-alert-evidence"
		}
		datasets[selector] = Dataset{Name: selector, Product: "Microsoft Defender XDR", TagGroup: tag,
			Service: ServiceGraph, Kind: KindAdvancedHunting, Mode: ModeAppend, Path: "/v1.0/security/runHuntingQuery",
			Scope: graphScope, Query: table, TimeField: "Timestamp", IDFields: []string{"ReportId", "Timestamp", "DeviceId"},
			Permission: "ThreatHunting.Read.All", Description: "Microsoft Defender XDR advanced hunting table " + table}
	}
	// TVM findings are a current device/software/CVE snapshot, not timestamped
	// events. Timestamp and ReportId are not columns in this table.
	d := datasets["defender-xdr-vulnerability"]
	d.Mode, d.TimeField = ModeSnapshot, ""
	d.IDFields = []string{"DeviceId", "SoftwareVendor", "SoftwareName", "SoftwareVersion", "CveId"}
	datasets[d.Name] = d
}

func addEntraGovernance() {
	const graphScope = "https://graph.microsoft.com/.default"
	datasets["entra-access-reviews"] = Dataset{Name: "entra-access-reviews", Product: "Microsoft Entra ID", TagGroup: "entra-access-reviews",
		// endDateTime is the scheduled end, not a reviewer/status mutation clock.
		Service: ServiceGraph, Kind: KindParentChild, Mode: ModeSnapshot,
		ParentPath: "/v1.0/identityGovernance/accessReviews/definitions", ChildPath: "/v1.0/identityGovernance/accessReviews/definitions/{parentId}/instances",
		Scope: graphScope, IDField: "id", TimeField: "endDateTime", Permission: "AccessReview.Read.All", Description: "Microsoft Entra access review instances"}
	datasets["entra-directory-roles"] = Dataset{Name: "entra-directory-roles", Product: "Microsoft Entra ID", TagGroup: "entra-directory-roles",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot, Path: "/v1.0/roleManagement/directory/roleDefinitions",
		Scope: graphScope, IDField: "id", Permission: "RoleManagement.Read.Directory", Description: "Microsoft Entra directory role definitions"}
	// The list API supports $select/$filter/$expand, not $top. A moving start
	// window omits valid current assignments whose startDateTime is old or null.
	datasets["entra-pim-assignments"] = Dataset{Name: "entra-pim-assignments", Product: "Microsoft Entra ID", TagGroup: "entra-pim-assignments",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot, ServerPageSize: true, Path: "/v1.0/roleManagement/directory/roleAssignmentScheduleInstances",
		Scope: graphScope, IDField: "id", TimeField: "startDateTime", Permission: "RoleManagement.Read.Directory", Description: "Microsoft Entra PIM active role assignment instances"}
}
