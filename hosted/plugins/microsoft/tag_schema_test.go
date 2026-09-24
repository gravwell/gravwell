package microsoft

import (
	"strings"
	"testing"
)

func TestConsolidatedTagSchemaCoversCompleteCatalog(t *testing.T) {
	catalog := Catalog()
	tags := destinationTags(catalog, TagSchemaConsolidated)
	if got, want := len(catalog), 100; got != want {
		t.Fatalf("catalog selectors=%d, want %d", got, want)
	}
	if got, want := len(tags), 15; got != want {
		t.Fatalf("consolidated tags=%d, want %d: %#v", got, want, tags)
	}
	for _, dataset := range catalog {
		tag := destinationTag(dataset, TagSchemaConsolidated)
		t.Logf("TAGMAP|%s|%s|%s|%s", dataset.Name, dataset.Product, dataset.TagGroup, tag)
		if !strings.HasPrefix(tag, "microsoft-") {
			t.Fatalf("selector %q has non-consolidated tag %q", dataset.Name, tag)
		}
	}
}

func TestTagSchemaIsExplicitAndLegacyByDefault(t *testing.T) {
	dataset := datasets["entra-users"]
	if got := destinationTag(dataset, ""); got != dataset.TagGroup {
		t.Fatalf("omitted schema changed legacy tag: got=%q want=%q", got, dataset.TagGroup)
	}
	if got := destinationTag(dataset, TagSchemaConsolidated); got != "microsoft-entra-directory" {
		t.Fatalf("consolidated tag=%q", got)
	}
	if _, err := validateTagSchema("compact"); err == nil || !strings.Contains(err.Error(), "Tag-Schema") {
		t.Fatalf("invalid Tag-Schema error=%v", err)
	}
}

func TestConsolidatedSchemaPreservesSelectorStateAndRecordIdentity(t *testing.T) {
	dataset := datasets["defender-xdr-device-events"]
	if got, want := stateKey("microsoft", dataset.Name, ""), "microsoft/defender-xdr-device-events"; got != want {
		t.Fatalf("state key=%q want=%q", got, want)
	}
	metadata := apiMetadata(dataset)
	if metadata["_source"] != dataset.Name || metadata["_recordType"] != dataset.TagGroup {
		t.Fatalf("selector identity changed: %#v", metadata)
	}
}

func TestConfigConsolidatedTagsAndOverrides(t *testing.T) {
	conf := Config{Api: []string{"entra-users", "entra-groups"}, Tag_Schema: TagSchemaConsolidated}
	if got := conf.Tags(); len(got) != 1 || got[0] != "microsoft-entra-directory" {
		t.Fatalf("tags=%#v", got)
	}
	conf.Tag_Prefix = "lab"
	if got := conf.Tags(); len(got) != 1 || got[0] != "lab-microsoft-entra-directory" {
		t.Fatalf("prefixed tags=%#v", got)
	}
	conf.Tag_Name, conf.Tag_Prefix = "entra", ""
	if got := conf.Tags(); len(got) != 1 || got[0] != "entra" {
		t.Fatalf("overridden tags=%#v", got)
	}
}
