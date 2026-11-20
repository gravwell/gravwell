package microsoft

import (
	"reflect"
	"testing"
)

func TestEveryDefaultDestinationUsesMSPrefix(t *testing.T) {
	if got, want := len(datasets), 105; got != want {
		t.Fatalf("catalog selectors=%d, want %d", got, want)
	}
	for _, dataset := range datasets {
		conf := Config{Api: []string{dataset.Name}}
		wantTags := []string{destinationTag(dataset, TagSchemaConsolidated)}
		if got := conf.Tags(); !reflect.DeepEqual(got, wantTags) {
			t.Fatalf("selector %q tags=%v, want %v", dataset.Name, got, wantTags)
		}
		if got := conf.TagForDataset(dataset); got != wantTags[0] {
			t.Fatalf("selector %q destination=%q, want %q", dataset.Name, got, wantTags[0])
		}
	}
}

func TestTagOverridesPreserveExactDatasetScope(t *testing.T) {
	dataset := datasets["entra-users"]
	conf := Config{Api: []string{dataset.Name}}
	conf.Tag_Prefix = "lab"
	if got := conf.TagForDataset(dataset); got != "lab-ms-entra-directory" {
		t.Fatalf("prefixed destination=%q", got)
	}
	conf.Tag_Name, conf.Tag_Prefix = "entra-users-custom", ""
	if got := conf.TagForDataset(dataset); got != "entra-users-custom" {
		t.Fatalf("overridden destination=%q", got)
	}
}
