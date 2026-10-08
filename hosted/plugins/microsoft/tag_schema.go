package microsoft

import (
	"fmt"
	"sort"
	"strings"
)

const (
	TagSchemaLegacy       = "legacy"
	TagSchemaConsolidated = "consolidated"
)

func validateTagSchema(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", TagSchemaLegacy:
		return TagSchemaLegacy, nil
	case TagSchemaConsolidated:
		return TagSchemaConsolidated, nil
	default:
		return "", fmt.Errorf("Tag-Schema must be %q or %q", TagSchemaLegacy, TagSchemaConsolidated)
	}
}

func destinationTag(dataset Dataset, schema string) string {
	if schema != TagSchemaConsolidated {
		return dataset.TagGroup
	}
	switch dataset.Product {
	case "Microsoft Azure":
		switch {
		case dataset.Kind == KindAzureActivity:
			return "microsoft-azure-activity"
		case strings.Contains(dataset.Name, "policy"):
			return "microsoft-azure-policy"
		case strings.Contains(dataset.Name, "defender") || strings.Contains(dataset.Name, "security"):
			return "microsoft-defender-cloud"
		default:
			return "microsoft-azure-inventory"
		}
	case "Microsoft Azure Policy":
		return "microsoft-azure-policy"
	case "Microsoft Defender for Cloud":
		return "microsoft-defender-cloud"
	case "Microsoft Defender for Endpoint":
		return "microsoft-defender-endpoint"
	case "Microsoft Defender XDR":
		switch {
		case strings.Contains(dataset.Name, "incident"):
			return "microsoft-defender-xdr-incidents"
		case strings.Contains(dataset.Name, "alert"):
			return "microsoft-defender-xdr-alerts"
		case dataset.Kind == KindAdvancedHunting:
			return "microsoft-defender-xdr-hunting"
		default:
			return "microsoft-defender-xdr-posture"
		}
	case "Microsoft Entra ID Protection":
		return "microsoft-entra-security"
	case "Microsoft Entra ID":
		switch {
		case strings.Contains(dataset.Name, "access-review"),
			strings.Contains(dataset.Name, "directory-role"),
			strings.Contains(dataset.Name, "pim-"),
			strings.Contains(dataset.Name, "role-assignment"):
			return "microsoft-entra-governance"
		case strings.Contains(dataset.Name, "audit"),
			strings.Contains(dataset.Name, "conditional-access"),
			strings.Contains(dataset.Name, "signin"):
			return "microsoft-entra-security"
		default:
			return "microsoft-entra-directory"
		}
	case "Microsoft 365", "Microsoft Exchange Online":
		return "microsoft-365"
	case "Microsoft Intune":
		return "microsoft-intune"
	case "Microsoft Fabric":
		return "microsoft-fabric"
	default:
		return dataset.TagGroup
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
