// Coverage for the configuration contract: how a stanza binds its datasets,
// what Host accepts, what the sanitized view may expose, and what the plugin
// requires of the muxer it is handed.
package claudecompliance

import (
	"fmt"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
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

// a checkpoint must not advance without an ingest delivery barrier.
func TestBareNegotiatorCannotAdvanceACheckpoint(t *testing.T) {
	p, rt := setup(t, "activities")
	plain, e := New(p.conf, nil) // a bare TagNegotiator, no SyncContext
	if e != nil {
		t.Logf("construction refused a bare negotiator: %v (good)", e)
		return
	}
	plain.now, plain.limiter = p.now, p.limiter
	plain.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a"}],"has_more":false}`, 200), nil
	})
	if _, e := plain.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if rt.committed(plain) {
		t.Errorf("checkpoint advanced with syncIngest=%v and no ingest barrier", plain.syncIngest != nil)
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
		"Label": true, "Ingest-Secret-File": true, "Encrypted-Backend-Target": true,
		"Insecure-Skip-TLS-Verify": true, "Ingest-Cache-Path": true,
		"Max-Ingest-Cache": true, "Cache-Mode": true, "Log-Level": true,
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
