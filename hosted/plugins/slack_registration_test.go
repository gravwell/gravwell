package plugins

import (
	"errors"
	"github.com/gravwell/gcfg"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type slackTags struct {
	reject bool
	seen   []string
}

func (n *slackTags) NegotiateTag(s string) (entry.EntryTag, error) {
	if n.reject {
		return 0, errors.New("synthetic tag rejection")
	}
	n.seen = append(n.seen, s)
	return 7, nil
}
func TestSlackRealRegistrationAndTagNegotiation(t *testing.T) {
	d := t.TempDir()
	secret := filepath.Join(d, "credential")
	if e := os.WriteFile(secret, []byte("synthetic-only"), 0600); e != nil {
		t.Fatal(e)
	}
	query := filepath.Join(d, "query.sql")
	if e := os.WriteFile(query, []byte("SELECT id FROM customer ORDER BY id"), 0600); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile("slack/example.conf")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	a := strings.Index(s, "[Slack ")
	if a < 0 {
		t.Fatal("missing named source stanza")
	}
	s = s[a:]
	s = strings.ReplaceAll(s, "REPLACE_WITH_DISTINCT_PERSISTENT_UUID", "291e5806-07f4-4533-a979-3296263389bf")
	s = strings.ReplaceAll(s, "/opt/gravwell/secrets/slack-token", secret)
	s = strings.ReplaceAll(s, "/opt/gravwell/etc/netsuite/read-only-query.sql", query)
	var cfg Configs
	if e = gcfg.ReadStringInto(&cfg, s); e != nil {
		t.Fatal(e)
	}
	if e = cfg.Verify(); e != nil {
		t.Fatal(e)
	}
	if cfg.IngesterCount() != 1 {
		t.Fatal("registration lost source")
	}
	tags, e := cfg.Tags()
	if e != nil || len(tags) != 1 || tags[0] != "slack-audit" {
		t.Fatalf("tags %v %v", tags, e)
	}
	count := 0
	for name, b := range cfg.Builders() {
		count++
		if name != "primary" {
			t.Fatal(name)
		}
		n := &slackTags{}
		job, e := b.Build(n, func() error { return nil })
		if e != nil || job == nil || len(n.seen) != 1 {
			t.Fatalf("builder failed %v", e)
		}
		if _, e = b.Build(&slackTags{reject: true}, nil); e == nil {
			t.Fatal("tag rejection ignored")
		}
	}
	if count != 1 {
		t.Fatal("builder missing")
	}
}
