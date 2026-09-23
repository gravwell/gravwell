package microsoft

import (
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

func TestMicrosoftCollectorReleaseVersion(t *testing.T) {
	if Version != "1.0.0" {
		t.Fatalf("ingester release version=%q want=1.0.0", Version)
	}
	if ID != "microsoft-api.ingesters.gravwell.io" {
		t.Fatalf("unexpected public ingester ID: %q", ID)
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

func TestAPIMetadataIsStableAndDocumented(t *testing.T) {
	dataset := datasets["microsoft365-license-assignments"]
	metadata := apiMetadata(dataset)
	for _, name := range []string{"_vendor", "_product", "_source", "_recordType", "_endpoint", "_apiVersion"} {
		if metadata[name] == "" {
			t.Fatalf("missing metadata %s: %#v", name, metadata)
		}
	}
	if metadata["_apiVersion"] != "v1.0" || !strings.HasPrefix(metadata["_endpoint"], "GET /v1.0/users") {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}

func TestDefenderCloudAssessmentsUsesDocumentedAPIVersion(t *testing.T) {
	path := datasets["defender-cloud-assessments"].Path
	if !strings.Contains(path, "api-version=2020-01-01") {
		t.Fatalf("assessment endpoint is not anchored to the documented API version: %q", path)
	}
}

func TestResolveDatasetGroupsAndExactTags(t *testing.T) {
	resolved, err := ResolveDatasets([]string{"azure", "intune-audit-events"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 4 {
		t.Fatalf("resolved datasets=%d, want 4", len(resolved))
	}
	conf := &Config{Api: []string{"azure-activity", "intune-audit-events"}}
	tags := conf.Tags()
	if len(tags) != 2 || tags[0] != "azure-activity" || tags[1] != "intune-graph-audit-events" {
		t.Fatalf("exact default tags not preserved: %#v", tags)
	}
}

func TestResolveDatasetRejectsUnknown(t *testing.T) {
	if _, err := ResolveDatasets([]string{"not-a-real-api"}); err == nil {
		t.Fatal("expected unknown selector error")
	}
}

func TestTagPrefixIsExplicit(t *testing.T) {
	conf := &Config{MultiTagConfig: hosted.MultiTagConfig{Tag_Prefix: "lab"}}
	if got := conf.Tag("azure-activity"); got != "lab-azure-activity" {
		t.Fatalf("tag=%q", got)
	}
}

func TestDefenderEndpointSelectorKeepsExistingKitTags(t *testing.T) {
	conf := &Config{Api: []string{"defender-endpoint"}}
	tags := conf.Tags()
	want := map[string]bool{
		"microsoft-defender-devices":         true,
		"microsoft-defender-exposure":        true,
		"microsoft-defender-recommendations": true,
		"microsoft-defender-software":        true,
		"microsoft-defender-vulnerabilities": true,
	}
	if len(tags) != len(want) {
		t.Fatalf("tags=%#v", tags)
	}
	for _, tag := range tags {
		if !want[tag] {
			t.Fatalf("unexpected tag %q", tag)
		}
	}
}

func TestDefenderCloudSelectorCoversPostureAndFindings(t *testing.T) {
	conf := &Config{Api: []string{"defender-cloud"}}
	tags := conf.Tags()
	want := map[string]bool{
		"microsoft-defender-cloud-alerts":                true,
		"microsoft-defender-cloud-assessments":           true,
		"microsoft-defender-cloud-assessment-metadata":   true,
		"microsoft-defender-cloud-subassessments":        true,
		"microsoft-defender-cloud-secure-scores":         true,
		"microsoft-defender-cloud-secure-score-controls": true,
		"microsoft-defender-cloud-regulatory-standards":  true,
	}
	if len(tags) != len(want) {
		t.Fatalf("tags=%#v", tags)
	}
	for _, tag := range tags {
		if !want[tag] {
			t.Fatalf("unexpected tag %q", tag)
		}
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
