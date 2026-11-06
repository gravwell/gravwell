/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"fmt"
	"sort"
	"strings"
)

const (
	TagSchemaConsolidated = "consolidated"
)

func validateTagSchema(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", TagSchemaConsolidated:
		return TagSchemaConsolidated, nil
	default:
		return "", fmt.Errorf("Tag-Schema must be omitted or %q", TagSchemaConsolidated)
	}
}

func destinationTag(dataset Dataset, schema string) string {
	_ = schema
	// These historical Azure selectors call Microsoft Graph directory APIs.
	// Route them with their Entra equivalents so the Azure and Entra product
	// boundaries remain stable even when an operator selects an old alias.
	switch dataset.Name {
	case "defender-incidents":
		return "ms-defender-xdr-incidents"
	case "azure-app-registrations", "azure-groups", "azure-service-principals", "azure-users":
		return "ms-entra-directory"
	case "azure-conditional-access":
		return "ms-entra-governance"
	}
	switch dataset.Product {
	case "Microsoft Azure":
		switch {
		case dataset.Kind == KindAzureActivity:
			return "ms-azure-activity"
		case strings.Contains(dataset.Name, "policy"):
			return "ms-azure-policy"
		case strings.Contains(dataset.Path, "/providers/Microsoft.Security/"):
			return "ms-defender-cloud"
		default:
			return "ms-azure-inventory"
		}
	case "Microsoft Azure Policy":
		return "ms-azure-policy"
	case "Microsoft Defender for Cloud":
		return "ms-defender-cloud"
	case "Microsoft Defender for Endpoint":
		return "ms-defender-endpoint"
	case "Microsoft Defender XDR":
		switch {
		case dataset.Kind == KindAdvancedHunting:
			return "ms-defender-xdr-hunting"
		case strings.Contains(dataset.Name, "incident"):
			return "ms-defender-xdr-incidents"
		case strings.Contains(dataset.Name, "alert"):
			return "ms-defender-xdr-alerts"
		default:
			return "ms-defender-xdr-posture"
		}
	case "Microsoft Entra ID Protection":
		return "ms-entra-risk"
	case "Microsoft Entra ID":
		switch {
		case strings.Contains(dataset.Name, "access-review"),
			strings.Contains(dataset.Name, "directory-role"),
			strings.Contains(dataset.Name, "pim-"),
			strings.Contains(dataset.Name, "role-assignment"),
			strings.Contains(dataset.Name, "conditional-access"):
			return "ms-entra-governance"
		case strings.Contains(dataset.Name, "audit"):
			return "ms-entra-audit"
		case strings.Contains(dataset.Name, "signin"):
			return "ms-entra-signin"
		default:
			return "ms-entra-directory"
		}
	case "Microsoft 365":
		switch dataset.Name {
		case "microsoft365-audit-azure-active-directory":
			return "ms-o365-azure"
		case "microsoft365-audit-exchange":
			return "ms-o365-exchange"
		case "microsoft365-audit-sharepoint":
			return "ms-o365-sharepoint"
		case "microsoft365-audit-general":
			return "ms-o365-general"
		case "microsoft365-dlp":
			return "ms-o365-dlp"
		default:
			return "ms-m365-inventory"
		}
	case "Microsoft Exchange Online":
		return "ms-m365-inventory"
	case "Microsoft Intune":
		return "ms-intune"
	case "Microsoft Fabric":
		return "ms-fabric"
	default:
		return ""
	}
}

func resolveDatasetsForSchema(selectors []string, schema string) ([]Dataset, error) {
	resolved, err := ResolveDatasets(selectors)
	if err != nil {
		return resolved, err
	}
	_ = schema
	excludeAzureDirectoryAliases := false
	explicitAzureDirectoryAliases := make(map[string]struct{})
	for _, rawSelector := range selectors {
		selector := strings.ToLower(strings.TrimSpace(rawSelector))
		if selector == "microsoft-azure-kit" {
			excludeAzureDirectoryAliases = true
		}
		if isAzureDirectoryAlias(selector) {
			explicitAzureDirectoryAliases[selector] = struct{}{}
		}
	}
	selected := make(map[string]struct{}, len(resolved))
	unfiltered := make(map[string]struct{})
	for _, dataset := range resolved {
		selected[dataset.Name] = struct{}{}
		if dataset.ActivityCategory == "" {
			unfiltered[consolidatedWireDestinationKey(dataset)] = struct{}{}
		}
	}
	result := make([]Dataset, 0, len(resolved))
	seen := make(map[string]struct{}, len(resolved))
	for _, dataset := range resolved {
		if canonical := consolidatedCanonicalSelector(dataset.Name); canonical != "" {
			if excludeAzureDirectoryAliases && isAzureDirectoryAlias(dataset.Name) {
				if _, explicitlySelected := explicitAzureDirectoryAliases[dataset.Name]; !explicitlySelected {
					continue
				}
			}
			if _, ok := selected[canonical]; ok {
				continue
			}
		}
		if dataset.ActivityCategory != "" {
			if _, ok := unfiltered[consolidatedWireDestinationKey(dataset)]; ok {
				continue
			}
		}
		effective := dataset
		effective.Product = ""
		effective.TagGroup = destinationTag(dataset, schema)
		effective.RouteTags = nil
		key := CollectionContractKey(effective)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, dataset)
	}
	return result, nil
}

func isAzureDirectoryAlias(selector string) bool {
	switch selector {
	case "azure-app-registrations", "azure-conditional-access", "azure-groups", "azure-service-principals", "azure-users":
		return true
	default:
		return false
	}
}

func consolidatedWireDestinationKey(dataset Dataset) string {
	return strings.Join([]string{
		string(dataset.Service), dataset.Method(), dataset.Path, dataset.Scope,
		dataset.Query, dataset.Filter, dataset.ContentType,
		destinationTag(dataset, TagSchemaConsolidated),
	}, "|")
}

func consolidatedCanonicalSelector(selector string) string {
	switch selector {
	case "azure-app-registrations":
		return "entra-applications"
	case "azure-conditional-access":
		return "entra-conditional-access-policies"
	case "azure-groups":
		return "entra-groups"
	case "azure-service-principals":
		return "entra-service-principals"
	case "azure-users":
		return "entra-users"
	case "azure-defender-alerts":
		return "defender-cloud-alerts"
	case "azure-defender-assessments", "azure-security-recommendations":
		return "defender-cloud-assessments"
	default:
		return ""
	}
}

func destinationTags(datasets []Dataset, schema string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, dataset := range datasets {
		tag := destinationTag(dataset, schema)
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}
