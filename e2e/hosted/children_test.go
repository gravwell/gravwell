package hosted

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"

	"github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/ingest"
)

const (
	// childPoll is how long we give the indexer's view of the runner to catch up. A child
	// registration marks the muxer state dirty and the state rides out on the next push,
	// which happens as the writer routine cycles rather than on a fixed schedule.
	childPoll = 90 * time.Second

	// the runner refreshes every child's counters on its own ticker, and out of band on
	// every reload. statsPoll only has to outlast the ticker.
	statsPoll = 2 * time.Minute
)

// TestHostedChildren covers the hosted runner reporting each configured plugin as a child
// on its ingest muxer, and keeping that set correct as the config changes underneath it.
// Everything here is read back through the ingest stats API, which is what the GUI shows,
// so this is the view an operator actually gets.
func TestHostedChildren(t *testing.T) {
	const runnerUUID = "6a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d000"
	alpha := testerPlugin{"kid-alpha", "6a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d001", "1s", "children-alpha"}
	beta := testerPlugin{"kid-beta", "6a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d002", "1s", "children-beta"}
	gamma := testerPlugin{"kid-gamma", "6a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d003", "1s", "children-gamma"}

	con := startRunner(t, "hosted-children", childrenConfig(runnerUUID, alpha, beta))
	c := e2e.GetClient(t)

	// every configured plugin registers itself at startup
	kids := waitForChildren(t, c, runnerUUID, childKey(alpha), childKey(beta))
	logChildren(t, "startup", kids)
	assertChildIdentity(t, kids, alpha)
	assertChildIdentity(t, kids, beta)

	// entries have to actually be flowing before the counters can mean anything
	assertEmitting(t, c, time.Time{}, alpha.Tag, beta.Tag)

	// a plugin added by a config reload shows up as a child too
	reload(t, con, childrenConfig(runnerUUID, alpha, beta, gamma))
	kids = waitForChildren(t, c, runnerUUID, childKey(alpha), childKey(beta), childKey(gamma))
	logChildren(t, "after add", kids)
	assertChildIdentity(t, kids, gamma)

	// the reload refreshed the two that were already running, so their counters are current
	assertChildStats(t, c, runnerUUID, alpha)
	assertChildStats(t, c, runnerUUID, beta)

	// and the registration is a reset, not just an append, a plugin that leaves the config
	// stops being reported
	reload(t, con, childrenConfig(runnerUUID, alpha, gamma))
	waitForChildGone(t, c, runnerUUID, childKey(beta))

	// the survivors are still there, the reset did not take out the bystanders
	logChildren(t, "after remove", waitForChildren(t, c, runnerUUID, childKey(alpha), childKey(gamma)))
}

// childrenConfig builds a reload config pinned to a known runner UUID so the stats lookup
// has something stable to match on. Without it the runner generates a UUID at startup and
// rolls a new one every time the config is replaced.
func childrenConfig(runnerUUID string, testers ...testerPlugin) reloadConfig {
	cfg := newReloadConfig(testers...)
	cfg.Ingester_UUID = runnerUUID
	return cfg
}

// childKey is the key the runner registers a plugin under, kind and stanza name.
func childKey(p testerPlugin) string { return "tester/" + p.Name }

// runnerChildren pulls the hosted runner's children out of the ingest stats API. The stats
// are keyed by indexer and every ingester attached to it is listed, so find ours by UUID.
func runnerChildren(t *testing.T, c *client.Client, runnerUUID string) (map[string]ingest.IngesterState, bool) {
	t.Helper()
	stats, err := c.GetIngesterStats()
	if err != nil {
		e2e.Fatalf(t, "failed to get ingester stats: %v", err)
	}
	for _, idx := range stats {
		for _, ig := range idx.Ingesters {
			if !strings.EqualFold(ig.UUID, runnerUUID) && !strings.EqualFold(ig.State.UUID, runnerUUID) {
				continue
			}
			return ig.State.Children, true
		}
	}
	return nil, false
}

// waitForChildren blocks until every wanted child key is reported, and hands back the
// children so callers can assert on what is in them.
func waitForChildren(t *testing.T, c *client.Client, runnerUUID string, want ...string) map[string]ingest.IngesterState {
	t.Helper()
	deadline := time.Now().Add(childPoll)
	var kids map[string]ingest.IngesterState
	for {
		var found bool
		if kids, found = runnerChildren(t, c, runnerUUID); found && hasAll(kids, want) {
			return kids
		}
		if time.Now().After(deadline) {
			e2e.Fatalf(t, "timed out after %v waiting for children %v, got %v", childPoll, want, childKeys(kids))
		}
		time.Sleep(2 * time.Second)
	}
}

// waitForChildGone blocks until the key is no longer reported.
func waitForChildGone(t *testing.T, c *client.Client, runnerUUID, key string) {
	t.Helper()
	deadline := time.Now().Add(childPoll)
	for {
		kids, found := runnerChildren(t, c, runnerUUID)
		if found {
			if _, still := kids[key]; !still {
				return
			}
		}
		if time.Now().After(deadline) {
			e2e.Fatalf(t, "child %q was still registered %v after its plugin left the config, got %v",
				key, childPoll, childKeys(kids))
		}
		time.Sleep(2 * time.Second)
	}
}

// assertChildIdentity checks the fields the runner stamps on a child at registration. Name
// is the plugin kind and Label is the configured stanza name, which is how a child is told
// apart from every other ingester the indexer knows about.
func assertChildIdentity(t *testing.T, kids map[string]ingest.IngesterState, p testerPlugin) {
	t.Helper()
	kid, ok := kids[childKey(p)]
	if !ok {
		e2e.Fatalf(t, "child %q is not registered, got %v", childKey(p), childKeys(kids))
	}
	if kid.Name != "tester" {
		e2e.Fatalf(t, "child %q reported kind %q, want \"tester\"", childKey(p), kid.Name)
	}
	if kid.Label != p.Name {
		e2e.Fatalf(t, "child %q reported label %q, want %q", childKey(p), kid.Label, p.Name)
	}
	if !strings.EqualFold(kid.UUID, p.UUID) {
		e2e.Fatalf(t, "child %q reported UUID %q, want %q", childKey(p), kid.UUID, p.UUID)
	}
	if kid.Version == "" {
		e2e.Fatalf(t, "child %q reported no version", childKey(p))
	}
	assertChildConfig(t, kid, p)
}

// assertChildConfig checks the sanitized config the tester plugin opts in to reporting. A
// plugin that does not implement hosted.ConfigSanitizer has nothing here at all, so this
// doubles as proof that the opt in is what gets a config reported.
func assertChildConfig(t *testing.T, kid ingest.IngesterState, p testerPlugin) {
	t.Helper()
	if len(kid.Configuration) == 0 {
		e2e.Fatalf(t, "child %q reported no sanitized config", childKey(p))
	}
	var cfg struct {
		Tag_Name string
		Interval string
	}
	if err := json.Unmarshal(kid.Configuration, &cfg); err != nil {
		e2e.Fatalf(t, "child %q reported an unreadable config %s: %v", childKey(p), kid.Configuration, err)
	}
	if cfg.Tag_Name != p.Tag {
		e2e.Fatalf(t, "child %q reported tag %q in its config, want %q: %s",
			childKey(p), cfg.Tag_Name, p.Tag, kid.Configuration)
	}
	if cfg.Interval != p.Interval {
		e2e.Fatalf(t, "child %q reported interval %q in its config, want %q: %s",
			childKey(p), cfg.Interval, p.Interval, kid.Configuration)
	}
}

// assertChildStats waits for a child's ingest counters to reflect the entries its plugin has
// written. A child is registered the instant its plugin starts, before it has written
// anything, so the counters only fill in when the runner next refreshes the registration.
func assertChildStats(t *testing.T, c *client.Client, runnerUUID string, p testerPlugin) {
	t.Helper()
	deadline := time.Now().Add(statsPoll)
	var kid ingest.IngesterState
	for {
		kids, found := runnerChildren(t, c, runnerUUID)
		if found {
			kid = kids[childKey(p)]
			if kid.Entries > 0 && kid.Size > 0 && slices.Contains(kid.Tags, p.Tag) {
				return
			}
		}
		if time.Now().After(deadline) {
			e2e.Fatalf(t, "child %q counters never caught up after %v: entries=%d size=%d tags=%v, want tag %q",
				childKey(p), statsPoll, kid.Entries, kid.Size, kid.Tags, p.Tag)
		}
		time.Sleep(2 * time.Second)
	}
}

// logChildren records exactly what the stats API reported at each stage, both as a test log
// and as a saved artifact, so a failure here can be read without re-running the whole thing.
func logChildren(t *testing.T, stage string, kids map[string]ingest.IngesterState) {
	t.Helper()
	for _, k := range childKeys(kids) {
		kid := kids[k]
		t.Logf("%s: %s -> kind=%q label=%q uuid=%s version=%s entries=%d size=%d tags=%v config=%s",
			stage, k, kid.Name, kid.Label, kid.UUID, kid.Version, kid.Entries, kid.Size, kid.Tags, kid.Configuration)
	}
	body, err := json.MarshalIndent(kids, "", "  ")
	if err != nil {
		return
	}
	e2e.WriteArtifact(t, e2e.Log, "children-"+strings.ReplaceAll(stage, " ", "-")+".json", body)
}

func hasAll(kids map[string]ingest.IngesterState, want []string) bool {
	for _, k := range want {
		if _, ok := kids[k]; !ok {
			return false
		}
	}
	return true
}

func childKeys(kids map[string]ingest.IngesterState) []string {
	keys := make([]string, 0, len(kids))
	for k := range kids {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
