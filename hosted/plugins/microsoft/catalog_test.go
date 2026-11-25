package microsoft

import (
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

func TestMicrosoftIngesterReleaseVersion(t *testing.T) {
	if Version != "1.0.0" {
		t.Fatalf("ingester release version=%q want=1.0.0", Version)
	}
	if ID != "microsoft-api.ingesters.gravwell.io" {
		t.Fatalf("unexpected public ingester ID: %q", ID)
	}
}

func TestCatalogContainsOneHundredFiveSelectors(t *testing.T) {
	if got := len(datasets); got != 105 {
		t.Fatalf("catalog selectors=%d, want 105", got)
	}
	resolved, err := ResolveDatasets([]string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(resolved); got != 98 {
		t.Fatalf("all resolves to %d unique collection contracts, want 98", got)
	}
}

func TestMicrosoft365SpecificSelectorsOwnExactKitTags(t *testing.T) {
	want := map[string]string{
		"entra-role-assignments":           "m365-roles",
		"microsoft365-exchange-mailboxes":  "m365-exchange-mailboxes",
		"microsoft365-license-assignments": "m365-license-assignment",
	}
	for selector, tag := range want {
		dataset, ok := datasets[selector]
		if !ok || dataset.TagGroup != tag {
			t.Fatalf("selector %q dataset=%#v want tag %q", selector, dataset, tag)
		}
	}
}

func TestDefenderCloudAssessmentsUsesDocumentedAPIVersion(t *testing.T) {
	path := datasets["defender-cloud-assessments"].Path
	if !strings.Contains(path, "api-version=2020-01-01") {
		t.Fatalf("assessment endpoint is not anchored to the documented API version: %q", path)
	}
}

func TestResolveDatasetGroupsAndConsolidatedTags(t *testing.T) {
	resolved, err := ResolveDatasets([]string{"azure", "intune-audit-events"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 4 {
		t.Fatalf("resolved datasets=%d, want 4", len(resolved))
	}
	conf := &Config{Api: []string{"azure-activity", "intune-audit-events"}}
	tags := conf.Tags()
	if !slices.Equal(tags, []string{"ms-azure-activity", "ms-intune"}) {
		t.Fatalf("consolidated default tags=%#v", tags)
	}
}

func TestResolveDatasetRejectsUnknown(t *testing.T) {
	if _, err := ResolveDatasets([]string{"not-a-real-api"}); err == nil {
		t.Fatal("expected unknown selector error")
	}
}

func TestManagementActivityCompatibilityAliasesResolveToCanonicalStateOwners(t *testing.T) {
	want := map[string]string{
		"Audit.AzureActiveDirectory": "microsoft365-audit-azure-active-directory",
		"exchange":                   "microsoft365-audit-exchange",
		"sharepoint":                 "microsoft365-audit-sharepoint",
		"office365-general":          "microsoft365-audit-general",
		"DLP.All":                    "microsoft365-dlp",
	}
	for alias, canonical := range want {
		resolved, err := ResolveDatasets([]string{alias})
		if err != nil || len(resolved) != 1 || resolved[0].Name != canonical {
			t.Fatalf("alias=%q resolved=%v error=%v", alias, resolved, err)
		}
	}
	resolved, err := ResolveDatasets([]string{"Audit.Exchange", "microsoft365-audit-exchange"})
	if err != nil || len(resolved) != 1 {
		t.Fatalf("alias plus canonical resolved=%v error=%v", resolved, err)
	}
	if got := TagsForSelectors([]string{"microsoft365-management-activity"}); !slices.Equal(got, []string{"m365-o365-azure-audit", "m365-exchange-audit", "m365-audit", "m365-teams", "m365-sharepoint", "m365-onedrive", "m365-dlp"}) {
		t.Fatalf("management tags=%v", got)
	}
}

func TestTagPrefixIsExplicit(t *testing.T) {
	conf := &Config{MultiTagConfig: hosted.MultiTagConfig{Tag_Prefix: "lab"}}
	if got := conf.Tag("azure-activity"); got != "lab-azure-activity" {
		t.Fatalf("tag=%q", got)
	}
}

func TestDefenderEndpointSelectorUsesOneProductTag(t *testing.T) {
	conf := &Config{Api: []string{"defender-endpoint"}}
	if tags := conf.Tags(); !slices.Equal(tags, []string{"ms-defender-endpoint"}) {
		t.Fatalf("tags=%#v", tags)
	}
}

func TestDefenderCloudSelectorCoversPostureAndFindings(t *testing.T) {
	conf := &Config{Api: []string{"defender-cloud"}}
	if tags := conf.Tags(); !slices.Equal(tags, []string{"ms-defender-cloud"}) {
		t.Fatalf("tags=%#v", tags)
	}
}

func TestDefenderCloudDefaultTagChangeKeepsSelectorStateKeysStable(t *testing.T) {
	dataset := datasets["defender-cloud-assessments"]
	if dataset.Name != "defender-cloud-assessments" {
		t.Fatalf("default tag change altered selector: %q", dataset.Name)
	}
	if strings.HasPrefix(dataset.TagGroup, "azure-") || !strings.HasPrefix(dataset.TagGroup, "microsoft-defender-cloud-") {
		t.Fatalf("Defender for Cloud tag is not product-owned: %q", dataset.TagGroup)
	}
	if got, want := stateKey("microsoft", dataset.Name, "subscription"), "microsoft/defender-cloud-assessments/subscription"; got != want {
		t.Fatalf("default tag change altered state key: got=%q want=%q", got, want)
	}
}
