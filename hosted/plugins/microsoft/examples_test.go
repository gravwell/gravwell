package microsoft

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gcfg"
)

type exampleStanza struct {
	Values map[string]string
	APIs   []string
}

const (
	exampleIngesterUUID     = "11111111-1111-4111-8111-111111111111"
	exampleTenantUUID       = "22222222-2222-4222-8222-222222222222"
	exampleClientUUID       = "33333333-3333-4333-8333-333333333333"
	examplePublisherUUID    = "44444444-4444-4444-8444-444444444444"
	exampleSubscriptionUUID = "55555555-5555-4555-8555-555555555555"
)

func TestPublishedExamplesUseRoleDistinctSyntheticIdentifiers(t *testing.T) {
	for _, name := range []string{"example.conf", "microsoft365.conf.example", "microsoft_every_api.conf.example"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			if strings.Contains(text, "00000000-0000-0000-0000-000000000000") {
				t.Fatal("published example contains the all-zero UUID")
			}
			assertOnlyExampleIdentifiers(t, text)
			if !strings.Contains(text, `Ingester-UUID="`+exampleIngesterUUID+`"`) {
				t.Fatal("global Ingester-UUID does not use the synthetic ingester identifier")
			}
			stanzas := parseExampleStanzas(t, text, "Microsoft")
			if len(stanzas) != 1 {
				t.Fatalf("Microsoft stanzas=%d, want 1", len(stanzas))
			}
			for _, stanza := range stanzas {
				for key, want := range map[string]string{"Tenant-ID": exampleTenantUUID, "Client-ID": exampleClientUUID} {
					if got := stanza.Values[key]; got != want {
						t.Fatalf("%s=%q, want synthetic role identifier %q", key, got, want)
					}
				}
				if got, present := stanza.Values["Publisher-Identifier"]; present && got != examplePublisherUUID {
					t.Fatalf("Publisher-Identifier=%q, want %q", got, examplePublisherUUID)
				}
				if got, present := stanza.Values["Subscription-ID"]; present && got != exampleSubscriptionUUID {
					t.Fatalf("Subscription-ID=%q, want %q", got, exampleSubscriptionUUID)
				}
			}
		})
	}
}

func TestMicrosoft365ProfileUsesConsolidatedDestinationsAndBoundedPolling(t *testing.T) {
	body, err := os.ReadFile("microsoft365.conf.example")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"entra-applications",
		"entra-conditional-access-policies",
		"defender-xdr-alerts",
		"defender-xdr-incidents",
		"entra-devices",
		"entra-directory-audits",
		"microsoft365-exchange-mailboxes",
		"entra-groups",
		"microsoft365-license-assignments",
		"entra-risk-detections",
		"entra-risky-users",
		"entra-role-assignments",
		"entra-service-principals",
		"entra-signins",
		"entra-users",
		"microsoft365-audit-azure-active-directory",
		"microsoft365-audit-exchange",
		"microsoft365-audit-sharepoint",
		"microsoft365-audit-general",
		"microsoft365-dlp",
	}

	stanzas := parseExampleStanzas(t, string(body), "Microsoft")
	if len(stanzas) != 1 {
		t.Fatalf("Microsoft 365 stanzas=%d, want 1", len(stanzas))
	}
	stanza, ok := stanzas["microsoft-365"]
	if !ok {
		t.Fatal(`missing [Microsoft "microsoft-365"] stanza`)
	}
	if !slices.Equal(stanza.APIs, want) {
		t.Fatalf("Api values=%q, want %q", stanza.APIs, want)
	}
	if got := len(TagsForSelectors(stanza.APIs)); got != len(want)+2 {
		t.Fatalf("catalog metadata tags=%d, want %d", got, len(want)+2)
	}
	if got := stanza.Values["Tag-Schema"]; got != TagSchemaConsolidated {
		t.Fatalf("Tag-Schema=%q, want %q", got, TagSchemaConsolidated)
	}
	// The stanza opts into the consolidated schema, so the trailing comment has
	// to enumerate every destination an operator must provision. Pin the real
	// routing table here so the comment and the schema cannot drift apart.
	resolved, err := resolveDatasetsForSchema(stanza.APIs, TagSchemaConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	wantDestinations := []string{
		"ms-defender-xdr-alerts",
		"ms-defender-xdr-incidents",
		"ms-entra-audit",
		"ms-entra-directory",
		"ms-entra-governance",
		"ms-entra-risk",
		"ms-entra-signin",
		"ms-m365-inventory",
		"ms-o365-azure",
		"ms-o365-dlp",
		"ms-o365-exchange",
		"ms-o365-general",
		"ms-o365-sharepoint",
	}
	if got := destinationTags(resolved, TagSchemaConsolidated); !slices.Equal(got, wantDestinations) {
		t.Fatalf("consolidated destinations=%q, want %q", got, wantDestinations)
	}
	for _, tag := range wantDestinations {
		if !strings.Contains(string(body), tag) {
			t.Fatalf("published example does not document destination tag %q", tag)
		}
	}
	if _, ok := stanza.Values["Tag-Name"]; ok {
		t.Fatal("multi-selector profile must use consolidated destination tags")
	}
	if strings.Contains(string(body), "`_source` and") || strings.Count(string(body), "`_source`") != 1 || !strings.Contains(string(body), "preserves the exact selector identity") {
		t.Fatal("published example must document the _source selector discriminator exactly once")
	}
	if strings.Contains(string(body), "exact catalog tag") || !strings.Contains(string(body), "consolidated ms-* destination") {
		t.Fatal("published Microsoft 365 example must describe effective consolidated destinations, not catalog metadata tags")
	}
	if _, ok := stanza.Values["Ingester-UUID"]; ok {
		t.Fatal("Microsoft stanza should use the stable default UUID")
	}
	if got := stanza.Values["Requests-Per-Minute"]; got != "12" {
		t.Fatalf("Requests-Per-Minute=%q, want 12", got)
	}
	if got := stanza.Values["Request-Interval"]; got != "900" {
		t.Fatalf("Request-Interval=%q, want 900", got)
	}
	for key, want := range map[string]string{"Tenant-ID": exampleTenantUUID, "Client-ID": exampleClientUUID, "Publisher-Identifier": examplePublisherUUID} {
		if got := stanza.Values[key]; got != want {
			t.Fatalf("%s=%q, want synthetic example %q", key, got, want)
		}
	}
	if got := stanza.Values["Client-Secret"]; got != "REPLACE_WITH_CLIENT_SECRET_VALUE" {
		t.Fatalf("Client-Secret=%q", got)
	}
}

func TestPublishedRoutingGuidanceDistinguishesSelectorsFromDestinations(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	for _, stale := range []string{
		"resolved catalog tag",
		"source tags consumed by their kits",
		"executable selector-to-tag inventory",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("README contains stale routing guidance %q", stale)
		}
	}
	for _, required := range []string{
		"`ResolveDatasets` and the catalog tests are the executable selector",
		"`Config.Tags` applies the consolidated routing table",
		"effective runtime destinations",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("README is missing runtime routing guidance %q", required)
		}
	}
}

func TestEveryAPIHostedExampleCoversCatalogWithCompactConfig(t *testing.T) {
	body, err := os.ReadFile("microsoft_every_api.conf.example")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	stanzas := parseExampleStanzas(t, text, "Microsoft")
	if len(stanzas) != 1 {
		t.Fatalf("Microsoft stanzas=%d, want 1", len(stanzas))
	}
	stanza, ok := stanzas["complete-catalog"]
	if !ok {
		t.Fatal(`missing [Microsoft "complete-catalog"] stanza`)
	}
	if !slices.Equal(stanza.APIs, []string{"all"}) {
		t.Fatalf("Api values=%q, want [all]", stanza.APIs)
	}
	got, err := ResolveDatasets(stanza.APIs)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ResolveDatasets([]string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("resolved datasets=%d, want %d", len(got), len(want))
	}
	resolved, err := resolveDatasetsForSchema(stanza.APIs, TagSchemaConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	destinations := destinationTags(resolved, TagSchemaConsolidated)
	if len(destinations) != 22 {
		t.Fatalf("effective consolidated destinations=%d, want 22: %q", len(destinations), destinations)
	}
	for _, tag := range destinations {
		if !strings.HasPrefix(tag, "ms-") {
			t.Fatalf("effective destination %q does not use the ms-* contract", tag)
		}
	}
	if !strings.Contains(text, "22 consolidated ms-* destination tags") || strings.Contains(text, "exact catalog tag") {
		t.Fatal("complete-catalog guidance does not describe the effective consolidated destinations")
	}
	if _, ok := stanza.Values["Tag-Name"]; ok {
		t.Fatal("complete catalog example must use consolidated destination tags")
	}
	if _, ok := stanza.Values["Ingester-UUID"]; ok {
		t.Fatal("Microsoft stanza should use the stable default UUID")
	}
	assertNoActiveDirectIngestSecret(t, text)
	assertHostedGlobalCredentialSurface(t, text)
	assertOnlyExampleIdentifiers(t, text)
}

// microsoftStanzaText returns each [Microsoft "name"] stanza as standalone
// gcfg text so a test can load it with the production loader.
func microsoftStanzaText(text string) []string {
	var stanzas []string
	var current strings.Builder
	inStanza := false
	flush := func() {
		if inStanza {
			stanzas = append(stanzas, current.String())
		}
		inStanza = false
		current.Reset()
	}
	for _, raw := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(raw), "[") {
			flush()
			inStanza = strings.HasPrefix(strings.TrimSpace(raw), `[Microsoft "`)
		}
		if inStanza {
			current.WriteString(raw)
			current.WriteString("\n")
		}
	}
	flush()
	return stanzas
}

// TestPublishedExampleOptionsBindToConfigFields loads every published Microsoft
// stanza with the same gcfg reader the runner uses. gcfg rejects a variable with
// no matching field, so this fails if a documented option name does not actually
// reach Config.
func TestPublishedExampleOptionsBindToConfigFields(t *testing.T) {
	for _, path := range []string{"example.conf", "microsoft365.conf.example", "microsoft_every_api.conf.example"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		stanzas := microsoftStanzaText(string(body))
		if len(stanzas) != 1 {
			t.Fatalf("%s: Microsoft stanzas=%d, want 1", path, len(stanzas))
		}
		var parsed struct {
			Microsoft map[string]*Config
		}
		if err := gcfg.ReadStringInto(&parsed, stanzas[0]); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(parsed.Microsoft) != 1 {
			t.Fatalf("%s: parsed configs=%d", path, len(parsed.Microsoft))
		}
		for name, conf := range parsed.Microsoft {
			if conf.Defender_Host == "" {
				continue // the stanza does not override any origin
			}
			if conf.Defender_Host != defaultDefenderHost {
				t.Fatalf("%s stanza %q Defender-Host=%q, want %q", path, name, conf.Defender_Host, defaultDefenderHost)
			}
			if conf.Defender_XDR_Host != defaultDefenderXDRHost {
				t.Fatalf("%s stanza %q Defender-XDR-Host=%q, want %q", path, name, conf.Defender_XDR_Host, defaultDefenderXDRHost)
			}
		}
	}
}

func TestCanonicalIngesterFormsUseDefaultMicrosoftUUID(t *testing.T) {
	for _, path := range []string{"example.conf", "microsoft365.conf.example", "microsoft_every_api.conf.example"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for name, stanza := range parseExampleStanzas(t, string(body), "Microsoft") {
			if _, ok := stanza.Values["Ingester-UUID"]; ok {
				t.Fatalf("%s Microsoft stanza %q overrides the stable default UUID", path, name)
			}
		}
	}
}

func TestEveryAPIExampleProvidesRequiredSubscription(t *testing.T) {
	body, err := os.ReadFile("microsoft_every_api.conf.example")
	if err != nil {
		t.Fatal(err)
	}
	for name, stanza := range parseExampleStanzas(t, string(body), "Microsoft") {
		resolved, err := ResolveDatasets(stanza.APIs)
		if err != nil {
			t.Fatal(err)
		}
		for _, dataset := range resolved {
			if (dataset.Kind == KindResourceGraph || strings.Contains(dataset.Path, "{subscriptionId}")) && stanza.Values["Subscription-ID"] == "" {
				t.Errorf("stanza %s requires Subscription-ID", name)
			}
		}
	}
}

func parseExampleStanzas(t *testing.T, text, header string) map[string]exampleStanza {
	t.Helper()
	stanzas := map[string]exampleStanza{}
	current := ""
	prefix := `[` + header + ` "`
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, `"]`) {
			current = strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"]`)
			stanzas[current] = exampleStanza{Values: map[string]string{}}
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
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		stanza := stanzas[current]
		if key == "Api" {
			stanza.APIs = append(stanza.APIs, value)
		} else {
			stanza.Values[key] = value
		}
		stanzas[current] = stanza
	}
	return stanzas
}

func assertOnlyExampleIdentifiers(t *testing.T, text string) {
	t.Helper()
	allowed := map[string]struct{}{
		exampleIngesterUUID: {}, exampleTenantUUID: {}, exampleClientUUID: {},
		examplePublisherUUID: {}, exampleSubscriptionUUID: {},
	}
	pattern := regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	for _, value := range pattern.FindAllString(text, -1) {
		if _, ok := allowed[value]; !ok {
			t.Fatalf("generated example contains an identifier outside the synthetic role allowlist: %q", value)
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
