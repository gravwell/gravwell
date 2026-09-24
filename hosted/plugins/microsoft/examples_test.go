package microsoft

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestMicrosoft365ProfileUsesExactTagsAndBoundedPolling(t *testing.T) {
	body, err := os.ReadFile("microsoft365.conf.example")
	if os.IsNotExist(err) {
		t.Skip("integration-specific Microsoft 365 profile is not part of the portable plugin")
	}
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"entra-applications":                "m365-applications",
		"entra-conditional-access-policies": "m365-conditional-access",
		"defender-xdr-alerts":               "m365-defender-alerts",
		"defender-xdr-incidents":            "m365-defender-incidents",
		"entra-devices":                     "m365-devices",
		"entra-directory-audits":            "m365-directory-audit",
		"microsoft365-exchange-mailboxes":   "m365-exchange-mailboxes",
		"entra-groups":                      "m365-groups",
		"microsoft365-license-assignments":  "m365-license-assignment",
		"entra-risk-detections":             "m365-risk-detections",
		"entra-risky-users":                 "m365-risky-users",
		"entra-role-assignments":            "m365-roles",
		"entra-service-principals":          "m365-service-principals",
		"entra-signins":                     "m365-signins",
		"entra-users":                       "m365-users",
	}

	stanzas := parseExampleStanzas(t, string(body), "Microsoft")
	if len(stanzas) != len(want) {
		t.Fatalf("Microsoft 365 stanzas=%d, want %d", len(stanzas), len(want))
	}
	seen := make(map[string]bool, len(stanzas))
	for name, values := range stanzas {
		api := values["Api"]
		tag, ok := want[api]
		if !ok {
			t.Fatalf("stanza %q uses unexpected API %q", name, api)
		}
		if seen[api] {
			t.Fatalf("API %q is configured more than once", api)
		}
		seen[api] = true
		if got := values["Tag-Name"]; got != tag {
			t.Fatalf("API %q Tag-Name=%q, want %q", api, got, tag)
		}
		for key, max := range map[string]int{"Page-Size": 100, "Max-Pages": 10, "Requests-Per-Minute": 12} {
			got, err := strconv.Atoi(values[key])
			if err != nil || got < 1 || got > max {
				t.Fatalf("API %q %s=%q, want 1..%d", api, key, values[key], max)
			}
		}
		interval, err := strconv.Atoi(values["Request-Interval"])
		if err != nil || interval < 900 {
			t.Fatalf("API %q Request-Interval=%q, want at least 900 seconds", api, values["Request-Interval"])
		}
		if got := values["Normalization"]; got != "disabled" {
			t.Fatalf("API %q Normalization=%q, want disabled", api, got)
		}
		for _, key := range []string{"Ingester-UUID", "Tenant-ID", "Client-ID"} {
			if got := values[key]; got != "00000000-0000-0000-0000-000000000000" {
				t.Fatalf("API %q %s=%q, want all-zero placeholder", api, key, got)
			}
		}
	}
}

func parseExampleStanzas(t *testing.T, text, header string) map[string]map[string]string {
	t.Helper()
	stanzas := map[string]map[string]string{}
	current := ""
	prefix := `[` + header + ` "`
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, `"]`) {
			current = strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"]`)
			stanzas[current] = map[string]string{}
			continue
		}
		if strings.HasPrefix(line, "[") {
			current = ""
			continue
		}
		if current == "" || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		stanzas[current][strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"`)
	}
	return stanzas
}

func TestEveryAPIHostedExampleCoversCatalogWithDummyIdentifiers(t *testing.T) {
	body, err := os.ReadFile("microsoft_every_api.conf.example")
	if os.IsNotExist(err) {
		t.Skip("full catalog example is maintained outside the portable plugin")
	}
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	unique, err := ResolveDatasets([]string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	for _, dataset := range unique {
		name := dataset.Name
		for _, want := range []string{
			`[Microsoft "` + name + `"]`,
			`Api="` + name + `"`,
			`default exact Tag-Name: ` + dataset.TagGroup,
		} {
			if !strings.Contains(text, want) {
				t.Errorf("generated Hosted Runner example is missing %q", want)
			}
		}
	}
	if got := strings.Count(text, "[Microsoft \""); got != len(unique) {
		t.Fatalf("Microsoft stanzas=%d, want %d unique collection contracts", got, len(unique))
	}
	assertHostedSelectorStanzasAreIndependent(t, text)
	assertNoActiveDirectIngestSecret(t, text)
	assertHostedGlobalCredentialSurface(t, text)
	uuidPattern := regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	for _, value := range uuidPattern.FindAllString(text, -1) {
		if value != "00000000-0000-0000-0000-000000000000" &&
			!regexp.MustCompile(`^00000000-0000-4000-8000-[0-9]{12}$`).MatchString(value) {
			t.Fatalf("generated example contains non-example identifier")
		}
	}
}

func TestCanonicalDeploymentExampleUsesIndependentSelectorStanzas(t *testing.T) {
	body, err := os.ReadFile("microsoft_every_api.conf.example")
	if err != nil {
		t.Fatal(err)
	}
	assertSelectorStanzaShape(t, string(body), "Microsoft")
}

func TestCanonicalCollectorFormsPublishOnlyZeroUUIDPlaceholders(t *testing.T) {
	paths := []string{
		"microsoft_every_api.conf.example",
	}
	pattern := regexp.MustCompile(`(?m)^Ingester-UUID="([0-9a-f-]+)"[^\n]*replace[^\n]*$`)
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			t.Skip("full catalog example is maintained outside the portable plugin")
		}
		if err != nil {
			t.Fatal(err)
		}
		matches := pattern.FindAllStringSubmatch(string(body), -1)
		unique, err := ResolveDatasets([]string{"all"})
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != len(unique)+1 {
			t.Fatalf("%s publication UUID placeholders=%d, want %d", path, len(matches), len(unique)+1)
		}
		for _, match := range matches {
			if match[1] != "00000000-0000-0000-0000-000000000000" {
				t.Fatalf("%s publishes nonzero UUID %q", path, match[1])
			}
		}
	}
}

func assertHostedSelectorStanzasAreIndependent(t *testing.T, text string) {
	t.Helper()
	assertSelectorStanzaShape(t, text, "Microsoft")
}

func assertSelectorStanzaShape(t *testing.T, text, header string) {
	t.Helper()
	current := ""
	apiCount := map[string]int{}
	apiValue := map[string]string{}
	stanzaUUID := map[string]string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		prefix := `[` + header + ` "`
		if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, `"]`) {
			current = strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"]`)
			apiCount[current] = 0
			continue
		}
		if strings.HasPrefix(line, "[") {
			current = ""
			continue
		}
		if current == "" {
			continue
		}
		if strings.HasPrefix(line, "Api=") {
			apiCount[current]++
			apiValue[current] = strings.Trim(strings.TrimPrefix(line, "Api="), `"`)
		}
		if strings.HasPrefix(line, "Ingester-UUID=") {
			stanzaUUID[current] = strings.Trim(strings.TrimPrefix(line, "Ingester-UUID="), `"`)
		}
	}
	covered := map[string]bool{}
	for name := range apiCount {
		dataset, ok := datasets[name]
		if !ok {
			t.Fatalf("unknown selector stanza %q", name)
		}
		covered[CollectionContractKey(dataset)] = true
		if apiCount[name] != 1 {
			t.Fatalf("%s stanza %q has %d Api fields, want exactly 1", header, name, apiCount[name])
		}
		if apiValue[name] != name {
			t.Fatalf("%s stanza %q selects Api=%q", header, name, apiValue[name])
		}
		uuid := stanzaUUID[name]
		if uuid != "00000000-0000-0000-0000-000000000000" {
			t.Fatalf("%s stanza %q publishes nonzero UUID %q", header, name, uuid)
		}
	}
	for _, dataset := range datasets {
		if !covered[CollectionContractKey(dataset)] {
			t.Fatalf("missing collection contract for %s", dataset.Name)
		}
	}
}

func assertHostedGlobalCredentialSurface(t *testing.T, text string) {
	t.Helper()
	inGlobal := false
	foundIngestSecret := false
	foundIngestSecretFile := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		commented := strings.HasPrefix(line, "#")
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if strings.HasPrefix(line, "[") {
			inGlobal = line == "[Global]"
			continue
		}
		if !inGlobal {
			continue
		}
		key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		lower := strings.ToLower(key)
		if key == "Ingest-Secret" || key == "Ingest-Secret-File" {
			if !commented {
				t.Fatalf("published %s must remain commented", key)
			}
			if key == "Ingest-Secret" {
				foundIngestSecret = true
			} else {
				foundIngestSecretFile = true
			}
			continue
		}
		for _, marker := range []string{"secret", "token", "credential", "password", "client-id", "tenant-id"} {
			if strings.Contains(lower, marker) {
				t.Fatalf("nonstandard credential field %q found in [Global]", key)
			}
		}
	}
	if !foundIngestSecret || !foundIngestSecretFile {
		t.Fatal("[Global] is missing the commented direct/file ingest-secret choices")
	}
}

func assertNoActiveDirectIngestSecret(t *testing.T, text string) {
	t.Helper()
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if strings.HasPrefix(line, "Ingest-Secret=") {
			t.Fatal("generated publication example activates Ingest-Secret")
		}
	}
}

func TestEveryAPIExampleProvidesRequiredSubscription(t *testing.T) {
	body, err := os.ReadFile("microsoft_every_api.conf.example")
	if os.IsNotExist(err) {
		t.Skip("catalog profile is not included in portable plugin")
	}
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range parseExampleStanzas(t, string(body), "Microsoft") {
		datasets, err := ResolveDatasets([]string{values["Api"]})
		if err != nil {
			t.Fatal(err)
		}
		for _, dataset := range datasets {
			if (dataset.Kind == KindResourceGraph || strings.Contains(dataset.Path, "{subscriptionId}")) && values["Subscription-ID"] == "" {
				t.Errorf("stanza %s requires Subscription-ID", name)
			}
		}
	}
}
