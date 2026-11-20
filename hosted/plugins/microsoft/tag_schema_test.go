package microsoft

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestConsolidatedTagSchemaCoversCompleteCatalog(t *testing.T) {
	catalog := make([]Dataset, 0, len(datasets))
	for _, dataset := range datasets {
		catalog = append(catalog, dataset)
	}
	tags := destinationTags(catalog, TagSchemaConsolidated)
	if got, want := len(catalog), 105; got != want {
		t.Fatalf("catalog selectors=%d, want %d", got, want)
	}
	if got, want := len(tags), 22; got != want {
		t.Fatalf("consolidated tags=%d, want %d: %#v", got, want, tags)
	}
	wantTags := []string{
		"ms-azure-activity", "ms-azure-inventory", "ms-azure-policy",
		"ms-defender-cloud", "ms-defender-endpoint", "ms-defender-xdr-alerts",
		"ms-defender-xdr-hunting", "ms-defender-xdr-incidents", "ms-defender-xdr-posture",
		"ms-entra-audit", "ms-entra-directory", "ms-entra-governance", "ms-entra-risk", "ms-entra-signin",
		"ms-fabric", "ms-intune", "ms-m365-inventory",
		"ms-o365-azure", "ms-o365-dlp", "ms-o365-exchange", "ms-o365-general", "ms-o365-sharepoint",
	}
	if !reflect.DeepEqual(tags, wantTags) {
		t.Fatalf("consolidated tags=%#v, want %#v", tags, wantTags)
	}
	for _, dataset := range catalog {
		tag := destinationTag(dataset, TagSchemaConsolidated)
		if !strings.HasPrefix(tag, "ms-") {
			t.Fatalf("selector %q has non-consolidated tag %q", dataset.Name, tag)
		}
	}
}

func TestTagSchemaIsConsolidatedByDefaultAndRejectsLegacy(t *testing.T) {
	dataset := datasets["entra-users"]
	if got := destinationTag(dataset, ""); got != "ms-entra-directory" {
		t.Fatalf("omitted schema tag=%q, want ms-entra-directory", got)
	}
	if got := destinationTag(dataset, TagSchemaConsolidated); got != "ms-entra-directory" {
		t.Fatalf("consolidated tag=%q", got)
	}
	if _, err := validateTagSchema("legacy"); err == nil || !strings.Contains(err.Error(), "Tag-Schema") {
		t.Fatalf("legacy Tag-Schema error=%v", err)
	}
	if _, err := validateTagSchema("compact"); err == nil || !strings.Contains(err.Error(), "Tag-Schema") {
		t.Fatalf("invalid Tag-Schema error=%v", err)
	}
}

func TestConsolidatedSchemaPreservesSelectorStateAndEntryIdentity(t *testing.T) {
	dataset := datasets["defender-xdr-device-events"]
	if got, want := stateKey("microsoft", dataset.Name, ""), "microsoft/defender-xdr-device-events"; got != want {
		t.Fatalf("state key=%q want=%q", got, want)
	}
	item := entry.Entry{}
	if err := attachSourceIdentity(&item, dataset); err != nil {
		t.Fatal(err)
	}
	if got, ok := item.GetEnumeratedValue("_source"); !ok || got != dataset.Name {
		t.Fatalf("_source=%v present=%v", got, ok)
	}
	defaultSchema := entry.Entry{}
	if err := attachSourceIdentity(&defaultSchema, dataset); err != nil {
		t.Fatal(err)
	}
	if got, ok := defaultSchema.GetEnumeratedValue("_source"); !ok || got != dataset.Name {
		t.Fatalf("default _source=%v present=%v", got, ok)
	}
}

func TestConfigConsolidatedTagsAndManagementRoutes(t *testing.T) {
	conf := Config{Api: []string{"entra-users", "entra-groups"}, Tag_Schema: TagSchemaConsolidated}
	if got := conf.Tags(); !reflect.DeepEqual(got, []string{"ms-entra-directory"}) {
		t.Fatalf("tags=%#v", got)
	}
	management := datasets["microsoft365-audit-sharepoint"]
	if got := conf.TagForManagementEvent(management, "m365-onedrive"); got != "ms-o365-sharepoint" {
		t.Fatalf("consolidated Management route=%q", got)
	}
	conf.Tag_Schema = ""
	if got := conf.TagForManagementEvent(management, "m365-onedrive"); got != "ms-o365-sharepoint" {
		t.Fatalf("default Management route=%q", got)
	}
	conf.Tag_Prefix = "lab"
	if got := conf.TagForManagementEvent(management, "m365-onedrive"); got != "lab-ms-o365-sharepoint" {
		t.Fatalf("prefixed Management route=%q", got)
	}
}

func TestConsolidatedResolutionHasNoDuplicateEffectiveContracts(t *testing.T) {
	resolved, err := resolveDatasetsForSchema([]string{"all"}, TagSchemaConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(resolved), 80; got != want {
		t.Fatalf("consolidated all resolved %d selectors, want %d", got, want)
	}
	seen := make(map[string]string, len(resolved))
	for _, dataset := range resolved {
		key := consolidatedWireDestinationKey(dataset)
		if previous, ok := seen[key]; ok {
			t.Fatalf("duplicate effective request/destination %q and %q", previous, dataset.Name)
		}
		seen[key] = dataset.Name
	}
	for _, canonical := range []string{"azure-activity", "defender-cloud-alerts", "defender-cloud-assessments", "defender-xdr-alerts", "defender-xdr-incidents"} {
		found := false
		for _, dataset := range resolved {
			found = found || dataset.Name == canonical
		}
		if !found {
			t.Fatalf("canonical selector %q was not retained", canonical)
		}
	}
}

func TestAdvancedHuntingSelectorsStayInHuntingDestination(t *testing.T) {
	if got := destinationTag(datasets["defender-xdr-alert-evidence"], TagSchemaConsolidated); got != "ms-defender-xdr-hunting" {
		t.Fatalf("AlertEvidence destination=%q", got)
	}
}

func TestAzureNetworkSecurityGroupsRemainInventory(t *testing.T) {
	if got := destinationTag(datasets["azure-network-security-groups"], TagSchemaConsolidated); got != "ms-azure-inventory" {
		t.Fatalf("network security groups destination=%q", got)
	}
	if got := destinationTag(datasets["azure-security-recommendations"], TagSchemaConsolidated); got != "ms-defender-cloud" {
		t.Fatalf("security recommendations destination=%q", got)
	}
}

func TestConsolidatedProductTagContract(t *testing.T) {
	tests := map[string]string{
		"azure-activity":                            "ms-azure-activity",
		"azure-resource-inventory":                  "ms-azure-inventory",
		"azure-policy-assignments":                  "ms-azure-policy",
		"defender-cloud-alerts":                     "ms-defender-cloud",
		"defender-devices":                          "ms-defender-endpoint",
		"defender-xdr-alerts":                       "ms-defender-xdr-alerts",
		"defender-xdr-incidents":                    "ms-defender-xdr-incidents",
		"defender-xdr-device-events":                "ms-defender-xdr-hunting",
		"defender-secure-scores":                    "ms-defender-xdr-posture",
		"entra-directory-audits":                    "ms-entra-audit",
		"entra-signins":                             "ms-entra-signin",
		"entra-risk-detections":                     "ms-entra-risk",
		"entra-users":                               "ms-entra-directory",
		"entra-conditional-access-policies":         "ms-entra-governance",
		"microsoft365-audit-azure-active-directory": "ms-o365-azure",
		"microsoft365-audit-exchange":               "ms-o365-exchange",
		"microsoft365-audit-sharepoint":             "ms-o365-sharepoint",
		"microsoft365-audit-general":                "ms-o365-general",
		"microsoft365-dlp":                          "ms-o365-dlp",
		"microsoft365-license-assignments":          "ms-m365-inventory",
		"intune-managed-devices":                    "ms-intune",
		"fabric-activity":                           "ms-fabric",
	}
	for selector, want := range tests {
		dataset, ok := datasets[selector]
		if !ok {
			t.Fatalf("selector %q is missing", selector)
		}
		if got := destinationTag(dataset, TagSchemaConsolidated); got != want {
			t.Errorf("selector %q destination=%q, want %q", selector, got, want)
		}
	}
}

func TestAzureDirectoryAliasesUseEntraDestinations(t *testing.T) {
	want := map[string]string{
		"azure-app-registrations":  "ms-entra-directory",
		"azure-groups":             "ms-entra-directory",
		"azure-service-principals": "ms-entra-directory",
		"azure-users":              "ms-entra-directory",
		"azure-conditional-access": "ms-entra-governance",
	}
	for selector, destination := range want {
		if got := destinationTag(datasets[selector], TagSchemaConsolidated); got != destination {
			t.Errorf("selector %q destination=%q, want %q", selector, got, destination)
		}
	}
	resolved, err := resolveDatasetsForSchema([]string{"all"}, TagSchemaConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	for selector := range want {
		for _, dataset := range resolved {
			if dataset.Name == selector {
				t.Errorf("alias %q was not collapsed into its Entra canonical selector", selector)
			}
		}
	}
}

func TestConsolidatedAzureGroupExcludesEntraDirectoryAliases(t *testing.T) {
	resolved, err := resolveDatasetsForSchema([]string{"microsoft-azure-kit"}, TagSchemaConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	for _, dataset := range resolved {
		if isAzureDirectoryAlias(dataset.Name) {
			t.Errorf("Azure group retained Entra alias %q", dataset.Name)
		}
		if strings.HasPrefix(destinationTag(dataset, TagSchemaConsolidated), "ms-entra-") {
			t.Errorf("Azure group selector %q routes to Entra", dataset.Name)
		}
	}
	for _, selector := range []string{"azure-users", "azure-conditional-access"} {
		explicit, err := resolveDatasetsForSchema([]string{selector}, TagSchemaConsolidated)
		if err != nil || len(explicit) != 1 || explicit[0].Name != selector {
			t.Fatalf("explicit compatibility selector %q resolved to %#v, error=%v", selector, explicit, err)
		}
	}
	combined, err := resolveDatasetsForSchema([]string{"microsoft-azure-kit", "azure-users"}, TagSchemaConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	foundExplicit := false
	for _, dataset := range combined {
		foundExplicit = foundExplicit || dataset.Name == "azure-users"
	}
	if !foundExplicit {
		t.Fatal("explicit azure-users was dropped when combined with microsoft-azure-kit")
	}
	normalized, err := resolveDatasetsForSchema([]string{" MICROSOFT-AZURE-KIT "}, TagSchemaConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	for _, dataset := range normalized {
		if isAzureDirectoryAlias(dataset.Name) || strings.HasPrefix(destinationTag(dataset, TagSchemaConsolidated), "ms-entra-") {
			t.Errorf("normalized Azure group leaked Entra selector %q", dataset.Name)
		}
	}
	normalizedExplicit, err := resolveDatasetsForSchema([]string{" MICROSOFT-AZURE-KIT ", " AZURE-USERS "}, TagSchemaConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	foundExplicit = false
	for _, dataset := range normalizedExplicit {
		foundExplicit = foundExplicit || dataset.Name == "azure-users"
	}
	if !foundExplicit {
		t.Fatal("normalized explicit azure-users was dropped when combined with normalized microsoft-azure-kit")
	}
}

func TestManagementSourceIdentitySurvivesDestinationOverrides(t *testing.T) {
	dataset := datasets["microsoft365-audit-sharepoint"]
	_, recordType, err := prepareManagementEvent(dataset, json.RawMessage(`{"Id":"one","CreationTime":"2026-10-19T12:00:00Z","Workload":"OneDrive"}`), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if recordType != "m365-onedrive" {
		t.Fatalf("record type=%q", recordType)
	}
	for _, tc := range []struct {
		conf Config
		want string
	}{
		{Config{Tag_Schema: TagSchemaConsolidated, MultiTagConfig: hosted.MultiTagConfig{Tag_Prefix: "lab"}}, "lab-ms-o365-sharepoint"},
		{Config{Tag_Schema: TagSchemaConsolidated, MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "customer-m365"}}, "customer-m365"},
	} {
		if got := tc.conf.TagForManagementEvent(dataset, recordType); got != tc.want {
			t.Fatalf("destination=%q, want %q", got, tc.want)
		}
		item := entry.Entry{}
		if err := attachSourceIdentity(&item, dataset); err != nil {
			t.Fatal(err)
		}
		if got, _ := item.GetEnumeratedValue("_source"); got != dataset.Name {
			t.Fatalf("overridden _source=%v", got)
		}
		if item.EVCount() != 1 {
			t.Fatalf("enumerated values=%v, want only _source", item.EnumeratedValues())
		}
	}
}
