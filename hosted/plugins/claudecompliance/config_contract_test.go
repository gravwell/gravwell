// Coverage for the configuration contract: how a stanza binds its datasets,
// what Host accepts, what the sanitized view may expose, and what the plugin
// requires of the muxer it is handed.
package claudecompliance

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// datasets with disjoint placeholder requirements in one stanza.
func TestDisjointPlaceholdersAcrossOneStanza(t *testing.T) {
	p, _ := setup(t, "activities")
	p.conf.Dataset = []string{"group-members", "organization-users"}
	p.conf.Parameter = []string{"group_id:g1", "organization_id:o1"}
	if e := p.conf.Verify(); e != nil {
		t.Fatalf("stanza-wide verify rejected disjoint placeholders: %v", e)
	}
	for _, name := range p.conf.Dataset {
		if _, e := p.conf.bind(name); e != nil {
			t.Errorf("bind(%s) rejected a sibling dataset parameter: %v", name, e)
		}
	}
}

func TestFollowChildrenRequiresOneAtomicDiscoveryPage(t *testing.T) {
	p, _ := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	p.conf.Page_Size = 3
	p.conf.Max_Pending = maxDiscoveredWorkPerPage - 1
	if err := p.conf.Verify(); err == nil {
		t.Fatal("Max-Pending below one maximum accepted discovery page was accepted")
	}
	p.conf.Max_Pending = maxDiscoveredWorkPerPage
	if err := p.conf.Verify(); err != nil {
		t.Fatalf("exact maximum accepted discovery floor was rejected: %v", err)
	}
	activities, _ := setup(t, "activities")
	activities.conf.Follow_Children = "enabled"
	activities.conf.Max_Children = 1
	activities.conf.Max_Pending = 1
	if err := activities.conf.Verify(); err != nil {
		t.Fatalf("childless dataset was given a discovery-capacity floor: %v", err)
	}

	want := 0
	for name, d := range Datasets {
		want = max(want, d.Limit*len(childSpecs(name)))
	}
	if want != maxDiscoveredWorkPerPage {
		t.Fatalf("catalog maximum discovery fanout=%d, bound=%d", want, maxDiscoveredWorkPerPage)
	}
	if got := discoveryPendingFloor([]string{"projects"}); got != Datasets["projects"].Limit*len(childSpecs("projects")) {
		t.Fatalf("project discovery floor=%d", got)
	}
}

func TestMaxPendingCannotExceedPersistedByteSafetyCeiling(t *testing.T) {
	p, _ := setup(t, "organizations")
	p.conf.Max_Pending = maxPendingWorkItems + 1
	if err := p.conf.Verify(); err == nil {
		t.Fatal("Max-Pending accepted a value above the persisted byte-safety ceiling")
	}
	p.conf.Max_Pending = maxPendingWorkItems
	if err := p.conf.Verify(); err != nil {
		t.Fatalf("supported Max-Pending ceiling rejected: %v", err)
	}
}

// TestMaxPendingCeilingFitsWorstCaseWorklist measures the worst case the
// Max-Pending ceiling claims to cover instead of asserting arithmetic. The
// worst case is not an identifier of maximum *length*: an identifier is
// stored three times over -- in the worklist map key, in Parameter, and
// percent-encoded inside the child's state key -- so the worst case uses the
// deepest catalog relationship (two parameters), identifiers that
// percent-encode to three bytes each, every mutable accounting field at its
// widest, and nanosecond timestamps. The state key comes from the production
// path rather than a hand-written approximation.
func TestMaxPendingCeilingFitsWorstCaseWorklist(t *testing.T) {
	// '%' is accepted by invalidOpaqueID, is not escaped by encoding/json, and
	// url.PathEscape expands it to three bytes -- the largest expansion any
	// permitted identifier can produce.
	id := strings.Repeat("%", maxDiscoveredParameterLen)
	if invalidOpaqueID(id) {
		t.Fatal("test identifier is not a permitted opaque identifier")
	}
	parent, _ := setup(t, "organizations")
	template := work{
		Dataset:          "role-permissions",
		Parameter:        []string{"organization_id:" + id, "role_id:" + id},
		Revision:         strings.Repeat("f", 64),
		Pending:          true,
		LastCompleted:    time.Unix(1<<31, 999999999).UTC(),
		LastAttempt:      time.Unix(1<<31, 999999999).UTC(),
		RetryAt:          time.Unix(1<<31, 999999999).UTC(),
		Failures:         maxChildFailures,
		ManifestEntries:  defaultMaxManifestEntries,
		ManifestMeasured: true,
		RetireAfterDrain: true,
		SeenThisScan:     true,
		AbsentStreak:     absentRetirementThreshold,
	}
	child, err := childConfig(parent.conf, template)
	if err != nil {
		t.Fatal(err)
	}
	template.StateKey = child.key()
	items := make(map[string]work, maxPendingWorkItems)
	for i := 0; i < maxPendingWorkItems; i++ {
		// Distinct keys of the same worst-case shape; the index replaces
		// leading identifier bytes so every key stays at its maximum width.
		unique := fmt.Sprintf("%06d", i) + id[6:]
		w := template
		w.Parameter = []string{"organization_id:" + unique, "role_id:" + unique}
		items["role-permissions/organization_id:"+unique+"/role_id:"+unique] = w
	}
	b, err := json.Marshal(worklist{Items: items, LastRootScan: time.Unix(1<<31, 999999999).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	want := maxPersistedWorklistBytes - worklistMutationHeadroom
	t.Logf("worst-case worklist at Max-Pending=%d is %d bytes (%.1f%% of the %d-byte target)",
		maxPendingWorkItems, len(b), 100*float64(len(b))/float64(want), want)
	if len(b) > want {
		t.Fatalf("supported worst-case worklist=%d bytes exceeds headroom target=%d", len(b), want)
	}
}

// Host must be an origin; SanitizedConfig must not emit identifiers.
func TestHostIsAnOriginAndSanitizedConfigHidesIdentifiers(t *testing.T) {
	for _, host := range []string{
		"https://user:pw@api.anthropic.com",
		"https://api.anthropic.com?a=b",
		"https://api.anthropic.com#frag",
	} {
		p, _ := setup(t, "activities")
		p.conf.Host = host
		if e := p.conf.Verify(); e == nil {
			t.Errorf("non-origin Host accepted: %q -> %q", host, p.conf.Host)
		}
	}
	p, _ := setup(t, "chat-messages")
	p.conf.Parameter = []string{"chat_id:customer-secret-chat-id"}
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
	s := fmt.Sprintf("%+v", p.conf.SanitizedConfig())
	t.Logf("sanitized = %s", s)
	if strings.Contains(s, "customer-secret-chat-id") {
		t.Error("SanitizedConfig emits opaque customer identifiers")
	}
}

// The shared constructor contract requires a delivery barrier at compile
// time, and nil is still rejected at the runtime boundary.
func TestNilNegotiatorIsRejected(t *testing.T) {
	p, _ := setup(t, "activities")
	if _, err := New(p.conf, nil); err == nil {
		t.Fatal("nil negotiator was accepted")
	}
}

// Every setting named in the README and the shipped example must exist on
// Config, so documentation cannot drift into describing a setting that was
// removed or never existed.
func TestDocumentedSettingsAllExist(t *testing.T) {
	fields := map[string]bool{}
	var collect func(reflect.Type)
	collect = func(ty reflect.Type) {
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			if f.PkgPath != "" {
				continue
			}
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				collect(f.Type)
				continue
			}
			fields[strings.ReplaceAll(f.Name, "_", "-")] = true
		}
	}
	collect(reflect.TypeOf(Config{}))
	// Settings owned by the shared runner stanzas rather than this plugin.
	shared := map[string]bool{
		"Label": true, "Ingest-Secret": true, "Encrypted-Backend-Target": true,
		"Insecure-Skip-TLS-Verify": true, "Ingest-Cache-Path": true,
		"Max-Ingest-Cache": true, "Max-Entry-Size": true, "Cache-Mode": true, "Log-Level": true,
		"Path": true, "Sync": true,
	}
	setting := regexp.MustCompile(`\b([A-Z][A-Za-z0-9]*(?:-[A-Z][A-Za-z0-9]*)+)\b`)
	for _, file := range []string{"README.md", "example.conf"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range setting.FindAllStringSubmatch(string(b), -1) {
			name := m[1]
			if fields[name] || shared[name] {
				continue
			}
			t.Errorf("%s documents %q, which is not a Config field", file, name)
		}
	}
}
