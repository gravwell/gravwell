package claudecompliance

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// TestCompactClearsFiniteCheckpointState proves reclamation uses only the
// standard Hosted Runner storage contract. Every value in a reusable slot is
// cleared explicitly; no shared prefix-deletion capability is required.
func TestCompactClearsFiniteCheckpointState(t *testing.T) {
	_, rt := setup(t, "activities")
	const prefix = "child#"
	if err := putManifest(rt, prefix+keyManifest, manifest{"identity": {Digest: "d"}}); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutString(prefix+keyWalkCursor, "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutString(prefix+keyHistory, historyComplete); err != nil {
		t.Fatal(err)
	}
	if err := compact(rt, prefix); err != nil {
		t.Fatal(err)
	}
	if m, err := getManifest(rt, prefix+keyManifest); err != nil || len(m) != 0 {
		t.Fatalf("manifest was not cleared: %v %v", m, err)
	}
	if cursor, err := getString(rt, prefix+keyWalkCursor); err != nil || cursor != "" {
		t.Fatalf("cursor=%q err=%v", cursor, err)
	}
	if history, err := getString(rt, prefix+keyHistory); err != nil || history != "" {
		t.Fatalf("history=%q err=%v", history, err)
	}
}

// TestCheckpointKeysCannotCollideAcrossDatasets covers checkpoint identity.
// Two different dataset templates can resolve to one and the same request
// path once parameter values are substituted, and a namespace keyed only on
// that path would make them share a lower bound, cursor, manifest, worklist
// and tombstone -- so each would keep rewinding and re-deduplicating the
// other's records.
func TestCheckpointKeysCannotCollideAcrossDatasets(t *testing.T) {
	p, rt := setup(t, "activities")
	c := *p.conf
	c.Dataset = []string{"project-document", "project-attachments"}
	c.Parameter = []string{"document_id:attachments", "project_id:documents"}
	c.path = ""
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
	document, err := c.bind("project-document")
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := c.bind("project-attachments")
	if err != nil {
		t.Fatal(err)
	}
	if document.path != attachments.path {
		t.Fatalf("precondition: both datasets must resolve to one path, got %q and %q", document.path, attachments.path)
	}
	if document.key() == attachments.key() {
		t.Fatalf("two datasets resolving to %q share a checkpoint namespace", document.path)
	}

	// And they really do keep independent progress end to end.
	p.conf = &c
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		// Only the row-bearing listing sends a page limit.
		if r.URL.Query().Has("limit") {
			return reply(`{"data":[{"id":"attachment-1","created_at":"2026-09-07T12:00:00Z"}],"has_more":false}`, http.StatusOK), nil
		}
		return reply(`{"id":"document-1","created_at":"2026-09-07T12:00:00Z"}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("entries=%d, want one per dataset", len(rt.entries))
	}
	documentState := readCheckpoint(t, rt, document.key())
	attachmentState := readCheckpoint(t, rt, attachments.key())
	if len(documentState.Manifest) != 1 || len(attachmentState.Manifest) != 1 {
		t.Fatalf("manifest sizes document=%d attachments=%d, want one identity each",
			len(documentState.Manifest), len(attachmentState.Manifest))
	}
	for identity := range documentState.Manifest {
		if _, shared := attachmentState.Manifest[identity]; shared {
			t.Fatal("the two datasets deduplicated against one shared manifest")
		}
	}
}

func TestStateKeySuffixCannotAliasNestedDatasetPath(t *testing.T) {
	prefix := "tenant"
	plural := (&Config{sourcePrefix: prefix, path: "/v1/compliance/groups"}).key()
	single := (&Config{sourcePrefix: prefix, path: "/v1/compliance/groups/walk"}).key()
	for _, suffix := range []string{keySince, keyManifest, keyWalkSince, keyWalkMan} {
		if plural+suffix == single+keySince || plural+suffix == single+keyManifest {
			t.Fatalf("state key alias: plural=%q single=%q suffix=%q", plural, single, suffix)
		}
	}
}

func TestRemoteFailureCarriesConfiguredRetryDelay(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"type":"error"}`, http.StatusForbidden), nil
	})
	_, err := p.Handle(t.Context(), rt)
	if err == nil {
		t.Fatal("remote failure was accepted")
	}
	var scheduled interface{ RetryDelay() time.Duration }
	if !errors.As(err, &scheduled) {
		t.Fatalf("remote failure does not carry a retry delay: %v", err)
	}
	want := time.Duration(p.conf.Request_Interval) * time.Second
	if got := scheduled.RetryDelay(); got != want {
		t.Fatalf("remote retry delay=%v, want %v", got, want)
	}
}

func TestRemoteContentFailureCarriesConfiguredRetryDelay(t *testing.T) {
	for _, body := range []string{
		`{`,
		`{"type":"error","error":{"type":"invalid_request_error"}}`,
		`{"data":[],"has_more":true}`,
	} {
		p, rt := setup(t, "activities")
		p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
			return reply(body, http.StatusOK), nil
		})
		_, err := p.Handle(t.Context(), rt)
		if err == nil {
			t.Fatalf("remote content failure was accepted: %s", body)
		}
		var scheduled interface{ RetryDelay() time.Duration }
		if !errors.As(err, &scheduled) {
			t.Fatalf("remote content failure has no retry delay: %v", err)
		}
		want := time.Duration(p.conf.Request_Interval) * time.Second
		if got := scheduled.RetryDelay(); got != want {
			t.Fatalf("remote content retry delay=%v, want %v", got, want)
		}
	}
}

func TestDiscoveredManifestBudgetIsAggregate(t *testing.T) {
	_, rt := setup(t, "organizations")
	list := worklist{Items: map[string]work{}}
	for i := 0; i < 2; i++ {
		stateKey := fmt.Sprintf("child-%d#", i)
		m := manifest{}
		for n := 0; n < 4; n++ {
			m[fmt.Sprintf("%d-%d", i, n)] = manifestEntry{Digest: "digest", Seen: int64(n)}
		}
		seedCheckpoint(t, rt, stateKey, checkpoint{Manifest: m})
		list.Items[fmt.Sprintf("child-%d", i)] = work{StateKey: stateKey, LastCompleted: time.Unix(int64(i+1), 0)}
	}
	dirty := false
	if err := reconcileManifestBudget(rt, &list, &dirty, 5, ""); err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Fatal("aggregate budget reconciliation was not persisted in the worklist")
	}
	total := 0
	for _, item := range list.Items {
		count, err := checkpointManifestEntries(rt, item.StateKey)
		if err != nil {
			t.Fatal(err)
		}
		total += count
		if item.ManifestEntries != count || !item.ManifestMeasured {
			t.Fatalf("worklist accounting=%d/%v checkpoint=%d", item.ManifestEntries, item.ManifestMeasured, count)
		}
	}
	if total > 5 {
		t.Fatalf("aggregate child manifests=%d, budget=5", total)
	}
}

func TestDiscoveredManifestBudgetIsSharedFairly(t *testing.T) {
	_, rt := setup(t, "organizations")
	list := worklist{Items: map[string]work{}}
	for i := 0; i < 3; i++ {
		stateKey := fmt.Sprintf("fair-child-%d#", i)
		m := manifest{}
		for n := 0; n < 80; n++ {
			m[fmt.Sprintf("%d-%d", i, n)] = manifestEntry{Digest: "digest", Seen: int64(n)}
		}
		seedCheckpoint(t, rt, stateKey, checkpoint{Manifest: m})
		list.Items[fmt.Sprintf("child-%d", i)] = work{StateKey: stateKey}
	}
	dirty := false
	if err := reconcileManifestBudget(rt, &list, &dirty, 90, ""); err != nil {
		t.Fatal(err)
	}
	for key, item := range list.Items {
		if item.ManifestEntries != 30 {
			t.Fatalf("%s retained %d entries, want fair share 30", key, item.ManifestEntries)
		}
	}
}

func TestZeroManifestIsMeasuredOnce(t *testing.T) {
	_, rt := setup(t, "organizations")
	list := worklist{Items: map[string]work{"child": {StateKey: "empty-child#"}}}
	dirty := false
	if err := reconcileManifestBudget(rt, &list, &dirty, maxAggregateManifestEntries, ""); err != nil {
		t.Fatal(err)
	}
	if !dirty || !list.Items["child"].ManifestMeasured {
		t.Fatal("empty checkpoint was not durably classified as measured")
	}
	dirty = false
	if err := reconcileManifestBudget(rt, &list, &dirty, maxAggregateManifestEntries, ""); err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Fatal("measured empty checkpoint was re-read and rewritten")
	}
}

// TestActiveManifestReservationSurvivesLargeWorklist covers the reservation an
// active child writes before its request runs. With a large worklist the equal
// share is tiny, so passing the freshly reserved child straight back through
// share reconciliation trimmed the reservation to that share before the
// request it was made for even happened -- and the page then arrived again on
// every later cycle. The reservation has to survive, the stanza-wide aggregate
// has to hold in both stored and in-memory accounting, and the accounting has
// to stay correct across a restart.
func TestActiveManifestReservationSurvivesLargeWorklist(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	p.conf.Max_Pending = maxPendingWorkItems
	p.conf.Page_Size = Datasets["group-members"].Limit
	workKey := p.conf.key() + keyChildren

	// More than a thousand idle siblings, so the equal share is far below one
	// page of the active child's records.
	const idle = 1200
	const members = 500
	items := make(map[string]work, idle+1)
	for i := 0; i < idle; i++ {
		id := fmt.Sprintf("idle-%04d", i)
		items["group-members/group_id:"+id] = work{
			Dataset: "group-members", Parameter: []string{"group_id:" + id},
			Revision: "idle", LastCompleted: p.now(), ManifestMeasured: true,
		}
	}
	activeWork := work{
		Dataset: "group-members", Parameter: []string{"group_id:active"},
		Revision: "active", Pending: true, LastAttempt: p.now().Add(-time.Hour),
	}
	activeWork.Slot = idle + 1
	activeWork.StateKey = childStateKey(p.conf.key(), activeWork.Slot)
	items["group-members/group_id:active"] = activeWork
	if share := maxAggregateManifestEntries / (idle + 1); share >= members {
		t.Fatalf("precondition: the equal share %d must be smaller than one page of %d records", share, members)
	}
	seed, err := json.Marshal(worklist{Items: items, LastRootScan: p.now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}

	var rows strings.Builder
	rows.WriteString(`{"data":[`)
	for i := 0; i < members; i++ {
		if i > 0 {
			rows.WriteByte(',')
		}
		fmt.Fprintf(&rows, `{"user_id":"member-%04d"}`, i)
	}
	rows.WriteString(`],"has_more":false}`)
	rootCalls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			rootCalls++
			return reply(`{"data":[{"id":"active"}],"has_more":false}`, http.StatusOK), nil
		}
		if !strings.Contains(r.URL.Path, "/groups/active/members") {
			t.Fatalf("unexpected child request %s", r.URL.Path)
		}
		return reply(rows.String(), http.StatusOK), nil
	})

	if _, err = p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if rootCalls != 0 {
		t.Fatalf("root was re-listed while healthy child work was ready: calls=%d", rootCalls)
	}
	memberEntries := func() int {
		n := 0
		for _, ent := range rt.entries {
			if source, _ := ent.GetEnumeratedValue("_source"); source == "group-members" {
				n++
			}
		}
		return n
	}
	if got := memberEntries(); got != members {
		t.Fatalf("member entries=%d, want %d", got, members)
	}
	retained, err := checkpointManifestEntries(rt, activeWork.StateKey)
	if err != nil {
		t.Fatal(err)
	}
	if retained != members {
		t.Fatalf("active child retained %d identities of %d; its page reservation was trimmed before the request it was made for", retained, members)
	}
	accountedFor := func() (int, worklist) {
		var stored worklist
		if e := json.Unmarshal(mustGet(t, rt, workKey), &stored); e != nil {
			t.Fatal(e)
		}
		total := 0
		for _, item := range stored.Items {
			total += item.ManifestEntries
		}
		return total, stored
	}
	total, stored := accountedFor()
	if stored.Items["group-members/group_id:active"].ManifestEntries != members {
		t.Fatalf("stored accounting=%d, want the %d identities actually retained",
			stored.Items["group-members/group_id:active"].ManifestEntries, members)
	}
	if total > maxAggregateManifestEntries {
		t.Fatalf("stanza-wide aggregate=%d exceeds the budget %d", total, maxAggregateManifestEntries)
	}

	// A second cycle must not re-ingest the page the reservation paid for.
	before := memberEntries()
	now := p.now()
	p.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err = p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := memberEntries(); got != before {
		t.Fatalf("the active child's page was ingested again: %d new entries", got-before)
	}

	// The accounting still matches reality after a restart.
	fresh := restart(t, p, rt)
	if _, err = fresh.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	total, stored = accountedFor()
	if total > maxAggregateManifestEntries {
		t.Fatalf("aggregate after restart=%d exceeds the budget", total)
	}
	retained, err = checkpointManifestEntries(rt, activeWork.StateKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := stored.Items["group-members/group_id:active"].ManifestEntries; got != retained {
		t.Fatalf("accounting drifted across restart: worklist=%d checkpoint=%d", got, retained)
	}
}

// TestActiveManifestGrowsIntoFreeAggregateCapacity covers the other half of the
// active child's reservation: it has to be able to *grow*. With the production
// default Page-Size and a stanza-sized worklist, both one page and the equal
// share are far below the identity count of a single large child, so a
// reservation fixed at max(page, share) left that child remembering one page of
// itself forever -- every unwindowed hourly refresh then re-delivered every
// record it had been forced to forget, without end. While aggregate capacity is
// free, a measured child must gain at least one page per completed run until its
// manifest is complete; the stanza-wide budget must hold at every persisted
// boundary, and the accounting must still match the checkpoint after a restart.
func TestActiveManifestGrowsIntoFreeAggregateCapacity(t *testing.T) {
	p, rt := setup(t, "remote-sessions")
	// Remote sessions intentionally stay outside absence retirement: active
	// transcripts are refreshed every poll, so the stored siblings stay put and
	// the geometry under test is fixed for the whole run.
	if absenceTrackedParents[p.conf.dataset.Name] {
		t.Fatal("precondition: an absence-tracked root would retire the stored siblings mid-run")
	}
	p.conf.Follow_Children = "enabled"
	// Exactly the production defaults: Verify applies Page-Size 100 and
	// Max-Pages 1000 to a stanza that sets neither.
	p.conf.Page_Size, p.conf.Max_Pages = 0, 0
	if err := p.conf.Verify(); err != nil {
		t.Fatal(err)
	}
	if p.conf.Page_Size != 100 || p.conf.Max_Pages != 1000 {
		t.Fatalf("precondition: production defaults are Page-Size=100 and Max-Pages=1000, got %d and %d",
			p.conf.Page_Size, p.conf.Max_Pages)
	}
	const (
		idle        = 1200
		permissions = 500
		siblingHeld = 5
	)
	pageSize := p.conf.Page_Size
	if share := maxAggregateManifestEntries / (idle + 1); share >= pageSize {
		t.Fatalf("precondition: the equal share %d must be below one page of %d", share, pageSize)
	}

	// Twelve hundred small roles already collected, each holding a handful of
	// identities, which leaves the overwhelming majority of the stanza-wide
	// budget free.
	items := make(map[string]work, idle)
	for i := 0; i < idle; i++ {
		w := work{
			Dataset:          "remote-session-messages",
			Parameter:        []string{fmt.Sprintf("session_id:idle-%04d", i)},
			Revision:         "idle",
			LastCompleted:    p.now(),
			ManifestEntries:  siblingHeld,
			ManifestMeasured: true,
		}
		child, err := childConfig(p.conf, w)
		if err != nil {
			t.Fatal(err)
		}
		w.StateKey = child.key()
		m := manifest{}
		for n := 0; n < siblingHeld; n++ {
			m[fmt.Sprintf("identity-%04d-%d", i, n)] = manifestEntry{Digest: "digest", Seen: p.now().Unix()}
		}
		seedCheckpoint(t, rt, w.StateKey, checkpoint{Since: p.now(), Manifest: m})
		items["remote-session-messages/"+strings.Join(w.Parameter, "/")] = w
	}
	workKey := p.conf.key() + keyChildren
	seed, err := json.Marshal(worklist{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if err = rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}

	// One role carries five pages of permissions, served one production-default
	// page at a time.
	permissionPage := func(n int) string {
		var b strings.Builder
		b.WriteString(`{"data":[`)
		for i := n * pageSize; i < min((n+1)*pageSize, permissions); i++ {
			if i > n*pageSize {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"resource_type":"document","resource_id":"doc-%04d","action":"read"}`, i)
		}
		b.WriteString(`]`)
		if (n+1)*pageSize < permissions {
			fmt.Fprintf(&b, `,"has_more":true,"next_page":"p%d"`, n+1)
		} else {
			b.WriteString(`,"next_page":null`)
		}
		b.WriteString(`}`)
		return b.String()
	}
	childPages := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sessions/remote"):
			return reply(`{"data":[{"id":"big","status":"active"}],"next_page":null}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/sessions/remote/big/messages"):
			childPages++
			page := 0
			if token := r.URL.Query().Get("page"); token != "" {
				if _, e := fmt.Sscanf(token, "p%d", &page); e != nil {
					t.Fatalf("unexpected page token %q", token)
				}
			}
			return reply(permissionPage(page), http.StatusOK), nil
		}
		t.Fatalf("unexpected request path %s", r.URL.Path)
		return nil, nil
	})

	// Every persisted worklist has to satisfy the stanza-wide budget, not only
	// the one a cycle happens to leave behind.
	rt.putFn = func(key string, b []byte) error {
		if key != workKey {
			return nil
		}
		var persisted worklist
		if e := json.Unmarshal(b, &persisted); e != nil {
			t.Errorf("persisted worklist is not decodable: %v", e)
			return nil
		}
		total := 0
		for _, item := range persisted.Items {
			total += item.ManifestEntries
		}
		if total > maxAggregateManifestEntries {
			t.Errorf("a persisted worklist accounts for %d identities; the stanza-wide budget is %d",
				total, maxAggregateManifestEntries)
		}
		return nil
	}

	const activeKey = "remote-session-messages/session_id:big"
	stored := func() worklist {
		var list worklist
		if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
			t.Fatal(e)
		}
		return list
	}
	collected := func() int {
		n := 0
		for _, ent := range rt.entries {
			if source, _ := ent.GetEnumeratedValue("_source"); source == "remote-session-messages" {
				n++
			}
		}
		return n
	}
	// Each cycle is an ordinary unwindowed refresh of an unchanged parent.
	start := p.now()
	refreshAt := func(cycle int) {
		at := start.Add(time.Duration(cycle-1) * 2 * time.Hour)
		p.now = func() time.Time { return at }
	}
	refreshes := (permissions + pageSize - 1) / pageSize
	for cycle := 1; cycle <= refreshes; cycle++ {
		refreshAt(cycle)
		before := collected()
		if _, err = p.Handle(t.Context(), rt); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
		active := stored().Items[activeKey]
		if active.StateKey == "" {
			t.Fatalf("cycle %d: the large child is not owned by the worklist", cycle)
		}
		retained, e := checkpointManifestEntries(rt, active.StateKey)
		if e != nil {
			t.Fatal(e)
		}
		if want := min(cycle*pageSize, permissions); retained != want {
			t.Fatalf("cycle %d: the large child retained %d of %d identities, want %d -- one page more than the cycle before",
				cycle, retained, permissions, want)
		}
		if active.ManifestEntries != retained {
			t.Fatalf("cycle %d: worklist accounting=%d, checkpoint=%d", cycle, active.ManifestEntries, retained)
		}
		if delivered := collected() - before; delivered == 0 {
			t.Fatalf("cycle %d: the refresh delivered nothing while the manifest was still incomplete", cycle)
		}
	}

	// Converged. The refreshes keep happening and stop re-delivering.
	for cycle := refreshes + 1; cycle <= refreshes+2; cycle++ {
		refreshAt(cycle)
		before := collected()
		if _, err = p.Handle(t.Context(), rt); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
		if delivered := collected() - before; delivered != 0 {
			t.Fatalf("cycle %d: a complete manifest delivered %d records again", cycle, delivered)
		}
	}

	// A restart re-reads the accounting it left behind; it must still match the
	// checkpoint exactly, and the aggregate must still hold.
	refreshAt(refreshes + 3)
	fresh := restart(t, p, rt)
	if _, err = fresh.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	list := stored()
	total := 0
	for _, item := range list.Items {
		total += item.ManifestEntries
	}
	if total > maxAggregateManifestEntries {
		t.Fatalf("aggregate accounting after restart=%d exceeds the budget %d", total, maxAggregateManifestEntries)
	}
	retained, err := checkpointManifestEntries(rt, list.Items[activeKey].StateKey)
	if err != nil {
		t.Fatal(err)
	}
	if retained != permissions || list.Items[activeKey].ManifestEntries != retained {
		t.Fatalf("accounting drifted across restart: worklist=%d checkpoint=%d want %d",
			list.Items[activeKey].ManifestEntries, retained, permissions)
	}
	if childPages == 0 {
		t.Fatal("the large child was never collected at all")
	}
}

// TestRetiredChildReleasesItsDeliveryLedgerEntry covers the in-process ledger
// that remembers writes from a page whose checkpoint has not committed yet. It
// is keyed per checkpoint namespace and was only ever cleared when one of that
// namespace's own pages committed, so every removed, evicted, retired,
// cross-family or deleted child left its entry behind forever. A namespace may
// only be released once the durable worklist no longer owns the child and its
// checkpoint has actually been reclaimed.
func TestRetiredChildReleasesItsDeliveryLedgerEntry(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	workKey := p.conf.key() + keyChildren
	ledgerSize := func() int {
		p.delivery.mu.Lock()
		defer p.delivery.mu.Unlock()
		return len(p.delivery.pages) + len(p.delivery.windows)
	}
	holds := func(prefix string) bool {
		p.delivery.mu.Lock()
		defer p.delivery.mu.Unlock()
		_, pages := p.delivery.pages[prefix]
		_, windows := p.delivery.windows[prefix]
		return pages || windows
	}

	present := "g1"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(fmt.Sprintf(`{"data":[{"id":%q}],"has_more":false}`, present), http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})
	// The shape a page that was written but never checkpointed leaves behind.
	uncommitted := func(stateKey string, page byte) {
		p.delivery.pinWindow(stateKey, traversal{
			Since: p.now().Add(-time.Hour), Until: p.now(), Started: p.now(),
		})
		if err := p.delivery.writeOnce(stateKey, [sha256.Size]byte{page}, "identity", rt, entry.Entry{}); err != nil {
			t.Fatal(err)
		}
	}

	// Repeated child creation and retirement must not grow the ledger. Each
	// cycle introduces one new group, which evicts the previous one, and each
	// evicted child is left holding ledger state the way a page that was
	// written but never checkpointed does.
	peak := 0
	for cycle := 0; cycle < 20; cycle++ {
		present = fmt.Sprintf("g%d", cycle)
		var stored worklist
		if b, err := rt.Get(workKey); err == nil {
			if err = json.Unmarshal(b, &stored); err != nil {
				t.Fatal(err)
			}
			for _, item := range stored.Items {
				if item.StateKey == "" {
					continue
				}
				uncommitted(item.StateKey, byte(cycle))
			}
		}
		if _, err := p.Handle(t.Context(), rt); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
		peak = max(peak, ledgerSize())
	}
	// One live child plus its own window is the whole steady state.
	if peak > 4 {
		t.Fatalf("delivery ledger grew to %d entries across repeated child retirement", peak)
	}

	// A failed reclamation must keep both the obligation and the ledger entry:
	// the checkpoint is still there, so the ledger must still suppress it.
	var stored worklist
	if err := json.Unmarshal(mustGet(t, rt, workKey), &stored); err != nil {
		t.Fatal(err)
	}
	var survivor work
	for _, item := range stored.Items {
		survivor = item
	}
	if survivor.StateKey == "" {
		t.Fatal("no live child to retire")
	}
	uncommitted(survivor.StateKey, 0xff)
	rt.putFn = func(key string, _ []byte) error {
		if strings.HasPrefix(key, survivor.StateKey) {
			return errors.New("synthetic reclamation failure")
		}
		return nil
	}
	present = "g-final"
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("a failed checkpoint reclamation was reported as success")
	}
	if !holds(survivor.StateKey) {
		t.Fatal("the ledger released a prefix whose checkpoint was never reclaimed")
	}
	rt.putFn = nil
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if holds(survivor.StateKey) {
		t.Fatal("the ledger kept a prefix after its checkpoint was reclaimed")
	}
}

func TestDeliveryBarrierFailureStopsBeforeAnotherChild(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	now := p.now().UTC()
	list := worklist{Items: map[string]work{}, LastRootScan: now}
	for i, id := range []string{"a", "b"} {
		w := work{Dataset: "organization-users", Parameter: []string{"organization_id:" + id}, Revision: "r", Pending: true, LastAttempt: now.Add(time.Duration(i) * time.Second)}
		child, err := childConfig(p.conf, w)
		if err != nil {
			t.Fatal(err)
		}
		w.StateKey = child.key()
		list.Items[id] = w
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(p.conf.key()+keyChildren, b); err != nil {
		t.Fatal(err)
	}
	var paths []string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return reply(`{"data":[{"id":"u"}],"has_more":false}`, http.StatusOK), nil
	})
	rt.failDelivery = true
	for i := 0; i < 2; i++ {
		if _, err := p.Handle(t.Context(), rt); err == nil {
			t.Fatal("delivery barrier failure was accepted")
		}
	}
	if len(paths) != 2 || paths[0] != paths[1] {
		t.Fatalf("delivery failure rotated across children: %v", paths)
	}
	p.delivery.mu.Lock()
	prefixes := len(p.delivery.pages)
	p.delivery.mu.Unlock()
	if prefixes != 1 {
		t.Fatalf("delivery ledger retained %d child prefixes, want 1", prefixes)
	}
}

func TestFailingChildDoesNotDelayReadySibling(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	now := p.now().UTC()
	list := worklist{Items: map[string]work{}, LastRootScan: now}
	for i, id := range []string{"a", "b"} {
		w := work{Dataset: "organization-users", Parameter: []string{"organization_id:" + id}, Revision: "r", Pending: true, LastAttempt: now.Add(time.Duration(i) * time.Second)}
		child, err := childConfig(p.conf, w)
		if err != nil {
			t.Fatal(err)
		}
		w.StateKey = child.key()
		list.Items[id] = w
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(p.conf.key()+keyChildren, b); err != nil {
		t.Fatal(err)
	}
	var paths []string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if len(paths) == 1 {
			return reply(`{`, http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("malformed child response was accepted")
	} else {
		var scheduled interface{ RetryDelay() time.Duration }
		if !errors.As(err, &scheduled) || scheduled.RetryDelay() != 100*time.Millisecond {
			t.Fatalf("ready sibling did not request the bounded short retry: %v", err)
		}
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatalf("ready sibling did not drain: %v", err)
	}
	if len(paths) != 2 || paths[0] == paths[1] {
		t.Fatalf("requests did not move from failed child to ready sibling: %v", paths)
	}
}

func TestCompactClearsOwnedCheckpointSlot(t *testing.T) {
	db, err := storage.OpenBoltHandler(filepath.Join(t.TempDir(), "state.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bucket, err := db.GetBucketWriter("plugin")
	if err != nil {
		t.Fatal(err)
	}
	_, base := setup(t, "activities")
	rt := persistedRuntime{Runtime: base, bucket: bucket}
	prefix := "child#"
	for _, suffix := range []string{keySince, keyManifest, keyWalkCursor, keyWalkMan, keyHistory} {
		if err := rt.PutString(prefix+suffix, "value"); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.PutString("child-other#"+keySince, "keep"); err != nil {
		t.Fatal(err)
	}
	if err := compact(rt, prefix); err != nil {
		t.Fatal(err)
	}
	if since, err := getTime(rt, prefix+keySince); err != nil || !since.IsZero() {
		t.Fatalf("lower bound=%v err=%v", since, err)
	}
	if m, err := getManifest(rt, prefix+keyManifest); err != nil || len(m) != 0 {
		t.Fatalf("manifest=%v err=%v", m, err)
	}
	if cursor, err := getString(rt, prefix+keyWalkCursor); err != nil || cursor != "" {
		t.Fatalf("cursor=%q err=%v", cursor, err)
	}
	if got, err := rt.GetString("child-other#" + keySince); err != nil || got != "keep" {
		t.Fatalf("unrelated prefix changed: %q %v", got, err)
	}
}
