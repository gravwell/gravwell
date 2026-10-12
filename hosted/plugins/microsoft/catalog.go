package microsoft

import (
	"fmt"
	"sort"
	"strings"
)

type Mode string

const (
	ModeAppend    Mode = "append"
	ModeLifecycle Mode = "lifecycle"
	ModeSnapshot  Mode = "snapshot"
)

type Kind string

const (
	KindODataGET        Kind = "odata-get"
	KindObjectGET       Kind = "object-get"
	KindAzureActivity   Kind = "azure-activity"
	KindResourceGraph   Kind = "resource-graph"
	KindPolicyStates    Kind = "policy-states"
	KindFabricActivity  Kind = "fabric-activity"
	KindAdvancedHunting Kind = "advanced-hunting"
	KindParentChild     Kind = "parent-child"
)

type Service string

const (
	ServiceGraph    Service = "graph"
	ServiceARM      Service = "arm"
	ServiceDefender Service = "defender"
	ServicePowerBI  Service = "powerbi"
)

type Dataset struct {
	Name      string
	Product   string
	TagGroup  string
	Service   Service
	Kind      Kind
	Mode      Mode
	Path      string
	Scope     string
	TimeField string
	IDField   string
	IDFields  []string
	Query     string
	Filter    string
	// ServerPageSize leaves page sizing to endpoints which do not support $top.
	ServerPageSize bool
	ParentPath     string
	ChildPath      string
	Permission     string
	Description    string
}

func (d Dataset) Method() string {
	switch d.Kind {
	case KindResourceGraph, KindPolicyStates, KindAdvancedHunting:
		return "POST"
	default:
		return "GET"
	}
}

func (d Dataset) APIVersion() string {
	if marker := "api-version="; strings.Contains(d.Path, marker) {
		value := strings.SplitN(d.Path, marker, 2)[1]
		return strings.SplitN(value, "&", 2)[0]
	}
	switch d.Service {
	case ServiceGraph:
		return "v1.0"
	case ServiceDefender:
		return "v1"
	case ServicePowerBI:
		return "v1.0"
	default:
		return "documented-endpoint"
	}
}

var datasets = map[string]Dataset{
	"azure-activity": {
		Name: "azure-activity", Product: "Microsoft Azure", TagGroup: "azure-activity",
		Service: ServiceARM, Kind: KindAzureActivity, Mode: ModeAppend,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Insights/eventtypes/management/values?api-version=2015-04-01",
		Scope: "https://management.azure.com/.default", TimeField: "eventTimestamp", IDField: "eventDataId",
		Permission: "Azure RBAC Reader", Description: "Azure Monitor Activity Log records",
	},
	"azure-resource-inventory": {
		Name: "azure-resource-inventory", Product: "Microsoft Azure", TagGroup: "azure-resources",
		Service: ServiceARM, Kind: KindResourceGraph, Mode: ModeSnapshot,
		Path:  "/providers/Microsoft.ResourceGraph/resources?api-version=2024-04-01",
		Scope: "https://management.azure.com/.default", IDField: "id",
		Query:      "Resources | order by id asc",
		Permission: "Azure RBAC Reader", Description: "Azure Resource Graph resource inventory",
	},
	"azure-policy-states": {
		Name: "azure-policy-states", Product: "Microsoft Azure Policy", TagGroup: "azure-policy-states",
		Service: ServiceARM, Kind: KindPolicyStates, Mode: ModeSnapshot,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.PolicyInsights/policyStates/latest/queryResults?api-version=2024-10-01",
		Scope: "https://management.azure.com/.default", TimeField: "timestamp",
		IDFields:    []string{"policyAssignmentId", "policyDefinitionReferenceId", "resourceId"},
		Permission:  "Azure RBAC role granting Microsoft.PolicyInsights/policyStates/queryResults/action",
		Description: "Latest Azure Policy state records",
	},
	"defender-cloud-alerts": {
		Name: "defender-cloud-alerts", Product: "Microsoft Defender for Cloud", TagGroup: "microsoft-defender-cloud-alerts",
		Service: ServiceARM, Kind: KindODataGET, Mode: ModeLifecycle,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Security/alerts?api-version=2022-01-01",
		Scope: "https://management.azure.com/.default", TimeField: "properties.timeGeneratedUtc", IDField: "id",
		Permission: "Security Reader", Description: "Defender for Cloud subscription alerts",
	},
	"defender-cloud-assessments": {
		Name: "defender-cloud-assessments", Product: "Microsoft Defender for Cloud", TagGroup: "microsoft-defender-cloud-assessments",
		Service: ServiceARM, Kind: KindODataGET, Mode: ModeSnapshot,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Security/assessments?api-version=2020-01-01",
		Scope: "https://management.azure.com/.default", TimeField: "properties.status.firstEvaluationDate", IDField: "id",
		Permission: "Security Reader", Description: "Defender for Cloud security assessments",
	},
	"defender-cloud-assessment-metadata": {
		Name: "defender-cloud-assessment-metadata", Product: "Microsoft Defender for Cloud", TagGroup: "microsoft-defender-cloud-assessment-metadata",
		Service: ServiceARM, Kind: KindODataGET, Mode: ModeSnapshot,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Security/assessmentMetadata?api-version=2020-01-01",
		Scope: "https://management.azure.com/.default", IDField: "id",
		Permission: "Security Reader", Description: "Defender for Cloud assessment definitions and remediation metadata",
	},
	"defender-cloud-subassessments": {
		Name: "defender-cloud-subassessments", Product: "Microsoft Defender for Cloud", TagGroup: "microsoft-defender-cloud-subassessments",
		Service: ServiceARM, Kind: KindResourceGraph, Mode: ModeSnapshot,
		Path:  "/providers/Microsoft.ResourceGraph/resources?api-version=2024-04-01",
		Scope: "https://management.azure.com/.default", TimeField: "properties.timeGenerated", IDField: "id",
		Query:      "SecurityResources | where type =~ 'microsoft.security/assessments/subassessments' | order by id asc",
		Permission: "Security Reader", Description: "Defender for Cloud resource-level vulnerability and security findings from Azure Resource Graph",
	},
	"defender-cloud-secure-scores": {
		Name: "defender-cloud-secure-scores", Product: "Microsoft Defender for Cloud", TagGroup: "microsoft-defender-cloud-secure-scores",
		Service: ServiceARM, Kind: KindODataGET, Mode: ModeLifecycle,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Security/secureScores?api-version=2020-01-01",
		Scope: "https://management.azure.com/.default", IDField: "id",
		Permission: "Security Reader", Description: "Defender for Cloud subscription secure-score snapshots",
	},
	"defender-cloud-secure-score-controls": {
		Name: "defender-cloud-secure-score-controls", Product: "Microsoft Defender for Cloud", TagGroup: "microsoft-defender-cloud-secure-score-controls",
		Service: ServiceARM, Kind: KindODataGET, Mode: ModeLifecycle,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Security/secureScoreControls?api-version=2020-01-01&$expand=definition",
		Scope: "https://management.azure.com/.default", IDField: "id",
		Permission: "Security Reader", Description: "Defender for Cloud secure-score control health and score details",
	},
	"defender-cloud-regulatory-standards": {
		Name: "defender-cloud-regulatory-standards", Product: "Microsoft Defender for Cloud", TagGroup: "microsoft-defender-cloud-regulatory-standards",
		Service: ServiceARM, Kind: KindODataGET, Mode: ModeLifecycle,
		Path:  "/subscriptions/{subscriptionId}/providers/Microsoft.Security/regulatoryComplianceStandards?api-version=2019-01-01-preview",
		Scope: "https://management.azure.com/.default", IDField: "id",
		Permission: "Security Reader", Description: "Defender for Cloud regulatory-compliance standard state",
	},
	"entra-directory-audits": {
		Name: "entra-directory-audits", Product: "Microsoft Entra ID", TagGroup: "entra-audit-logs",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeAppend,
		Path: "/v1.0/auditLogs/directoryAudits", Scope: "https://graph.microsoft.com/.default",
		TimeField: "activityDateTime", IDField: "id", Permission: "AuditLog.Read.All",
		Description: "Microsoft Entra directory audit events",
	},
	"entra-signins": {
		Name: "entra-signins", Product: "Microsoft Entra ID", TagGroup: "entra-signins",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeAppend,
		Path: "/v1.0/auditLogs/signIns", Scope: "https://graph.microsoft.com/.default",
		TimeField: "createdDateTime", IDField: "id", Permission: "AuditLog.Read.All",
		Description: "Microsoft Entra sign-in events",
	},
	"entra-risk-detections": {
		Name: "entra-risk-detections", Product: "Microsoft Entra ID Protection", TagGroup: "entra-risk-detections",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeAppend,
		Path: "/v1.0/identityProtection/riskDetections", Scope: "https://graph.microsoft.com/.default",
		TimeField: "detectedDateTime", IDField: "id", Permission: "IdentityRiskEvent.Read.All",
		Description: "Microsoft Entra user and sign-in risk detections",
	},
	"entra-risky-users": {
		Name: "entra-risky-users", Product: "Microsoft Entra ID Protection", TagGroup: "entra-risky-users",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeLifecycle,
		Path: "/v1.0/identityProtection/riskyUsers", Scope: "https://graph.microsoft.com/.default",
		TimeField: "riskLastUpdatedDateTime", IDField: "id", Permission: "IdentityRiskyUser.Read.All",
		Description: "Microsoft Entra risky-user state",
	},
	"entra-users": {
		Name: "entra-users", Product: "Microsoft Entra ID", TagGroup: "entra-users",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/users", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "User.Read.All", Description: "Microsoft Entra user inventory",
	},
	"entra-groups": {
		Name: "entra-groups", Product: "Microsoft Entra ID", TagGroup: "entra-groups",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/groups", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "Group.Read.All", Description: "Microsoft Entra group inventory",
	},
	"entra-applications": {
		Name: "entra-applications", Product: "Microsoft Entra ID", TagGroup: "entra-applications",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/applications", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "Application.Read.All", Description: "Microsoft Entra application registrations",
	},
	"entra-service-principals": {
		Name: "entra-service-principals", Product: "Microsoft Entra ID", TagGroup: "entra-service-principals",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/servicePrincipals", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "Application.Read.All", Description: "Microsoft Entra service-principal inventory",
	},
	"entra-devices": {
		Name: "entra-devices", Product: "Microsoft Entra ID", TagGroup: "entra-devices",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/devices", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "Device.Read.All", Description: "Microsoft Entra registered-device inventory",
	},
	"entra-conditional-access-policies": {
		Name: "entra-conditional-access-policies", Product: "Microsoft Entra ID", TagGroup: "entra-conditional-access-policies",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/identity/conditionalAccess/policies", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "Policy.Read.All", Description: "Microsoft Entra Conditional Access policy inventory",
	},
	"entra-role-assignments": {
		Name: "entra-role-assignments", Product: "Microsoft Entra ID", TagGroup: "m365-roles",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/roleManagement/directory/roleAssignments", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "RoleManagement.Read.Directory", Description: "Microsoft Entra directory role assignments",
	},
	"microsoft365-exchange-mailboxes": {
		Name: "microsoft365-exchange-mailboxes", Product: "Microsoft Exchange Online", TagGroup: "m365-exchange-mailboxes",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/users?$select=id,displayName,userPrincipalName,mail,proxyAddresses,accountEnabled", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "User.Read.All", Description: "Exchange Online mail-enabled user directory projection",
	},
	"microsoft365-license-assignments": {
		Name: "microsoft365-license-assignments", Product: "Microsoft 365", TagGroup: "m365-license-assignment",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/users?$select=id,userPrincipalName,assignedLicenses,assignedPlans", Scope: "https://graph.microsoft.com/.default", IDField: "id",
		Permission: "User.Read.All", Description: "Microsoft 365 per-user license and service-plan assignments",
	},
	"defender-xdr-incidents": {
		Name: "defender-xdr-incidents", Product: "Microsoft Defender XDR", TagGroup: "microsoft-defender-incidents",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeLifecycle,
		Path: "/v1.0/security/incidents", Scope: "https://graph.microsoft.com/.default",
		TimeField: "lastUpdateDateTime", IDField: "id", Permission: "SecurityIncident.Read.All",
		Description: "Microsoft Defender XDR incidents",
	},
	"defender-xdr-alerts": {
		Name: "defender-xdr-alerts", Product: "Microsoft Defender XDR", TagGroup: "microsoft-defender-alerts",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeLifecycle,
		Path: "/v1.0/security/alerts_v2", Scope: "https://graph.microsoft.com/.default",
		TimeField: "lastUpdateDateTime", IDField: "id", Permission: "SecurityAlert.Read.All",
		Description: "Microsoft Defender XDR alerts v2",
	},
	"defender-secure-scores": {
		Name: "defender-secure-scores", Product: "Microsoft Defender XDR", TagGroup: "microsoft-defender-secure-score",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeLifecycle,
		Path: "/v1.0/security/secureScores", Scope: "https://graph.microsoft.com/.default",
		TimeField: "createdDateTime", IDField: "id", Permission: "SecurityEvents.Read.All",
		Description: "Microsoft secure-score snapshots",
	},
	"defender-endpoint-devices": {
		Name: "defender-endpoint-devices", Product: "Microsoft Defender for Endpoint", TagGroup: "microsoft-defender-devices",
		Service: ServiceDefender, Kind: KindODataGET, Mode: ModeLifecycle,
		Path: "/api/machines", Scope: "https://api.securitycenter.microsoft.com/.default",
		TimeField: "lastSeen", IDField: "id", Permission: "Machine.Read.All",
		Description: "Microsoft Defender for Endpoint device inventory",
	},
	"defender-endpoint-vulnerabilities": {
		Name: "defender-endpoint-vulnerabilities", Product: "Microsoft Defender for Endpoint", TagGroup: "microsoft-defender-vulnerabilities",
		Service: ServiceDefender, Kind: KindODataGET, Mode: ModeLifecycle,
		Path: "/api/vulnerabilities", Scope: "https://api.securitycenter.microsoft.com/.default",
		TimeField: "updatedOn", IDField: "id", Permission: "Vulnerability.Read.All",
		Description: "Microsoft Defender Vulnerability Management vulnerability catalog",
	},
	"defender-endpoint-software": {
		Name: "defender-endpoint-software", Product: "Microsoft Defender for Endpoint", TagGroup: "microsoft-defender-software",
		Service: ServiceDefender, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/api/Software", Scope: "https://api.securitycenter.microsoft.com/.default",
		IDField: "id", Permission: "Software.Read.All",
		Description: "Microsoft Defender Vulnerability Management software inventory",
	},
	"defender-endpoint-recommendations": {
		Name: "defender-endpoint-recommendations", Product: "Microsoft Defender for Endpoint", TagGroup: "microsoft-defender-recommendations",
		Service: ServiceDefender, Kind: KindODataGET, Mode: ModeLifecycle,
		Path: "/api/recommendations", Scope: "https://api.securitycenter.microsoft.com/.default",
		IDField: "id", Permission: "SecurityRecommendation.Read.All",
		Description: "Microsoft Defender Vulnerability Management recommendations",
	},
	"defender-endpoint-exposure-score": {
		Name: "defender-endpoint-exposure-score", Product: "Microsoft Defender for Endpoint", TagGroup: "microsoft-defender-exposure",
		Service: ServiceDefender, Kind: KindObjectGET, Mode: ModeLifecycle,
		Path: "/api/exposureScore", Scope: "https://api.securitycenter.microsoft.com/.default",
		TimeField: "time", IDField: "time", Permission: "Score.Read.All",
		Description: "Microsoft Defender Vulnerability Management organization exposure score",
	},
	"intune-audit-events": {
		Name: "intune-audit-events", Product: "Microsoft Intune", TagGroup: "intune-graph-audit-events",
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeAppend,
		Path: "/v1.0/deviceManagement/auditEvents", Scope: "https://graph.microsoft.com/.default",
		TimeField: "activityDateTime", IDField: "id", Permission: "DeviceManagementApps.Read.All",
		Description: "Microsoft Intune audit events",
	},
	"intune-managed-devices": {
		Name: "intune-managed-devices", Product: "Microsoft Intune", TagGroup: "intune-graph-managed-devices",
		// lastSyncDateTime is a device-sync clock, not an admin mutation clock.
		Service: ServiceGraph, Kind: KindODataGET, Mode: ModeSnapshot,
		Path: "/v1.0/deviceManagement/managedDevices", Scope: "https://graph.microsoft.com/.default",
		TimeField: "lastSyncDateTime", IDField: "id", Permission: "DeviceManagementManagedDevices.Read.All",
		Description: "Microsoft Intune managed-device state",
	},
	"fabric-activity": {
		Name: "fabric-activity", Product: "Microsoft Fabric", TagGroup: "microsoft-fabric-activity",
		Service: ServicePowerBI, Kind: KindFabricActivity, Mode: ModeAppend,
		Path: "/v1.0/myorg/admin/activityevents", Scope: "https://analysis.windows.net/powerbi/api/.default",
		TimeField: "CreationTime", IDField: "Id", Permission: "Fabric administrator or supported service principal",
		Description: "Microsoft Fabric tenant activity events",
	},
}

var selectorGroups = map[string][]string{
	"azure":             {"azure-activity", "azure-resource-inventory", "azure-policy-states"},
	"defender":          {"defender-cloud-alerts", "defender-cloud-assessments", "defender-cloud-assessment-metadata", "defender-cloud-subassessments", "defender-cloud-secure-scores", "defender-cloud-secure-score-controls", "defender-cloud-regulatory-standards", "defender-xdr-incidents", "defender-xdr-alerts", "defender-secure-scores", "defender-endpoint-devices", "defender-endpoint-vulnerabilities", "defender-endpoint-software", "defender-endpoint-recommendations", "defender-endpoint-exposure-score"},
	"defender-cloud":    {"defender-cloud-alerts", "defender-cloud-assessments", "defender-cloud-assessment-metadata", "defender-cloud-subassessments", "defender-cloud-secure-scores", "defender-cloud-secure-score-controls", "defender-cloud-regulatory-standards"},
	"defender-xdr":      {"defender-xdr-incidents", "defender-xdr-alerts", "defender-secure-scores"},
	"defender-endpoint": {"defender-endpoint-devices", "defender-endpoint-vulnerabilities", "defender-endpoint-software", "defender-endpoint-recommendations", "defender-endpoint-exposure-score"},
	"entra":             {"entra-directory-audits", "entra-signins", "entra-risk-detections", "entra-risky-users", "entra-users", "entra-groups", "entra-applications", "entra-service-principals", "entra-devices", "entra-conditional-access-policies"},
	"intune":            {"intune-audit-events", "intune-managed-devices"},
	"fabric":            {"fabric-activity"},
}

func ResolveDatasets(selectors []string) ([]Dataset, error) {
	selected := make(map[string]Dataset)
	for _, raw := range selectors {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "all" {
			for key, dataset := range datasets {
				selected[key] = dataset
			}
			continue
		}
		if members, ok := selectorGroups[name]; ok {
			for _, member := range members {
				selected[member] = datasets[member]
			}
			continue
		}
		dataset, ok := datasets[name]
		if !ok {
			return nil, fmt.Errorf("unsupported Api selector %q", raw)
		}
		selected[name] = dataset
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Dataset, 0, len(names))
	seenContracts := make(map[string]bool)
	for _, name := range names {
		dataset := selected[name]
		key := CollectionContractKey(dataset)
		if seenContracts[key] {
			continue
		}
		seenContracts[key] = true
		result = append(result, dataset)
	}
	return result, nil
}

// CollectionContractKey identifies equivalent requests and destination tags.
// Names and prose are deliberately excluded; all wire, identity, state-mode,
// authorization, and tag fields remain part of the key. Existing individual
// selectors keep their names and state keys when configured on their own.
func CollectionContractKey(d Dataset) string {
	d.Name, d.Description = "", ""
	return fmt.Sprintf("%#v", d)
}

func TagsForSelectors(selectors []string) []string {
	resolved, err := ResolveDatasets(selectors)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var result []string
	for _, dataset := range resolved {
		if _, ok := seen[dataset.TagGroup]; ok {
			continue
		}
		seen[dataset.TagGroup] = struct{}{}
		result = append(result, dataset.TagGroup)
	}
	return result
}

// Catalog returns a stable copy of the runtime dataset catalog. Generation and
// validation tools use this instead of maintaining a second static inventory.
func Catalog() []Dataset {
	names := make([]string, 0, len(datasets))
	for name := range datasets {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Dataset, 0, len(names))
	for _, name := range names {
		result = append(result, datasets[name])
	}
	return result
}
