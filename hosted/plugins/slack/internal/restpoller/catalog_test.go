package restpoller

import (
	"slices"
	"testing"
)

func TestCatalogContainsDocumentedReadOnlyFamilies(t *testing.T) {
	tests := map[string][]string{
		"atlantis":   {"drift-status", "locks"},
		"chatgpt":    {"audit-logs", "groups", "users"},
		"claude":     {"organization", "usage-messages"},
		"freshworks": {"agents", "contacts", "tickets"},
		"n8n":        {"audit", "credentials", "executions", "projects", "tags", "users", "variables", "workflows"},
		"netsuite":   {"suiteql"},
		"slack":      {"audit-logs"},
	}
	for product, want := range tests {
		definitions, err := ResolveDefinitions(product, nil)
		if err != nil {
			t.Fatalf("ResolveDefinitions(%s): %v", product, err)
		}
		got := make([]string, 0, len(definitions))
		for _, definition := range definitions {
			got = append(got, definition.Name)
			if definition.DocumentationURL == "" {
				t.Errorf("%s/%s has no vendor documentation URL", product, definition.Name)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s definitions=%v want=%v", product, got, want)
		}
	}
}

func TestN8NSourceControlMutationIsNotCataloged(t *testing.T) {
	if _, err := ResolveDefinitions("n8n", []string{"source-control"}); err == nil {
		t.Fatal("source-control unexpectedly resolved; current vendor API only documents state-changing pull")
	}
}

func TestDuplicateDatasetRejected(t *testing.T) {
	if _, err := ResolveDefinitions("n8n", []string{"users", "users"}); err == nil {
		t.Fatal("duplicate Dataset was accepted")
	}
}
