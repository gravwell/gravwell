package claudecompliance

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted/storage"
)

// TestConfiguredAndDiscoveredOwnershipCannotAlias covers checkpoint ownership.
// A stanza can select a dataset explicitly and also reach that same dataset,
// resolved path and parameter set as a discovered child: selecting
// "organizations" and "organization-roles" with Parameter="organization_id:O"
// and Follow-Children="enabled" does exactly that as soon as organization O is
// listed. The two are separate logical owners -- the discovered one owns a
// worklist entry, a retry schedule and a retirement; the configured one owns
// the stanza's child worklist, cleanup backlog and tombstone registry -- so one
// shared namespace let each overwrite the other's committed manifest and
// re-emit records, and let a child's retirement delete the configured
// selector's state. Each role must name its own namespace, and their exact-key
// sets must remain disjoint.
func TestConfiguredAndDiscoveredOwnershipCannotAlias(t *testing.T) {
	for _, tc := range []struct {
		parent, child, record string
		parameter             []string
	}{
		{"organizations", "organization-roles", `{"uuid":"O"}`, []string{"organization_id:O"}},
		{"organizations", "organization-users", `{"uuid":"O"}`, []string{"organization_id:O"}},
		{"groups", "group-members", `{"id":"G"}`, []string{"group_id:G"}},
		// The one nested relationship in the catalog: a configured selector can
		// overlap a child discovered two levels below a root as well.
		{"organization-roles", "role-permissions", `{"id":"R"}`, []string{"organization_id:O", "role_id:R"}},
	} {
		t.Run(tc.parent+"-"+tc.child, func(t *testing.T) {
			p, _ := setup(t, "activities")
			c := *p.conf
			c.Dataset = []string{tc.parent, tc.child}
			c.Parameter = tc.parameter
			c.Follow_Children = "enabled"
			c.Max_Pending = maxPendingWorkItems
			c.path = ""
			if err := c.Verify(); err != nil {
				t.Fatal(err)
			}
			configured, err := c.bind(tc.child)
			if err != nil {
				t.Fatal(err)
			}
			parent, err := c.bind(tc.parent)
			if err != nil {
				t.Fatal(err)
			}
			children, err := childWork(Datasets[tc.parent], parent.Parameter, []byte(tc.record))
			if err != nil {
				t.Fatal(err)
			}
			var discovered *Config
			for _, w := range children {
				if w.Dataset != tc.child {
					continue
				}
				w.Slot = 1
				w.StateKey = childStateKey(parent.key(), w.Slot)
				if discovered, err = childConfig(parent, w); err != nil {
					t.Fatal(err)
				}
			}
			if discovered == nil {
				t.Fatalf("%s never discovered a %s child, so this case covers nothing", tc.parent, tc.child)
			}
			// The precondition this regression exists for: both walks really do
			// resolve to one dataset, one path and one source namespace.
			if discovered.dataset.Name != configured.dataset.Name || discovered.path != configured.path ||
				discovered.sourcePrefix != configured.sourcePrefix {
				t.Fatalf("precondition: the configured selector and the discovered child must overlap: configured=%s%s discovered=%s%s",
					configured.dataset.Name, configured.path, discovered.dataset.Name, discovered.path)
			}
			if discovered.key() == configured.key() {
				t.Fatalf("configured selector and discovered child share the checkpoint namespace %q", configured.key())
			}
			if strings.HasPrefix(discovered.key(), configured.key()) || strings.HasPrefix(configured.key(), discovered.key()) {
				t.Fatalf("one role's namespace is reachable by deleting the other: configured=%q discovered=%q",
					configured.key(), discovered.key())
			}
			// Every key either owner can write has to stay outside the other's
			// deletion boundary, including the three durable records only a
			// configured selector keeps.
			for _, suffix := range []string{keySince, keyManifest, keyWalkCursor, keyChildren, keyCleanup, keyTombstones} {
				if strings.HasPrefix(configured.key()+suffix, discovered.key()) {
					t.Fatalf("deleting the discovered child's prefix would remove the configured selector's %s", suffix)
				}
				if strings.HasPrefix(discovered.key()+suffix, configured.key()) {
					t.Fatalf("deleting the configured selector's prefix would remove the discovered child's %s", suffix)
				}
			}
		})
	}
}

// TestRetiringDiscoveredChildKeepsConfiguredSelectorState drives that same
// overlap end to end on the production Bolt storage implementation. One stanza
// selects "organizations" and
// "organization-roles" with Parameter="organization_id:O" and child discovery
// enabled, so the configured organization-roles selector and the discovered
// organization-roles child of organization O cover one dataset, path and
// parameter set. The configured selector is itself a parent, so its namespace
// also holds the stanza's child worklist, cleanup backlog and tombstone
// registry. Retiring the discovered child -- here through the plugin's own
// absence-retirement path, which stages a durable obligation and then clears
// the child's known checkpoint keys -- must leave every one of those intact and must not
// rewind the configured selector's lower bound.
func TestRetiringDiscoveredChildKeepsConfiguredSelectorState(t *testing.T) {
	db, err := storage.OpenBoltHandler(filepath.Join(t.TempDir(), "state.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bucket, err := db.GetBucketWriter("plugin")
	if err != nil {
		t.Fatal(err)
	}
	p, base := setup(t, "organizations")
	// Production exposes the standard exact-key storage contract. The plugin
	// reclaims finite checkpoint slots by clearing each value it owns.
	rt := persistedRuntime{Runtime: base, bucket: bucket}
	p.conf.Dataset = []string{"organizations", "organization-roles"}
	p.conf.Parameter = []string{"organization_id:O"}
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 10
	p.conf.Max_Pending = maxPendingWorkItems
	p.conf.path = ""
	if err = p.conf.Verify(); err != nil {
		t.Fatal(err)
	}

	present := true
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			if !present {
				return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
			}
			return reply(`{"data":[{"uuid":"O"}],"has_more":false}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/users"):
			return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/permissions"):
			return reply(`{"data":[{"resource_type":"organization","resource_id":"O","action":"read"}],"has_more":false}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/roles"):
			return reply(`{"data":[{"id":"R","updated_at":"2026-09-07T12:00:00Z"}],"has_more":false}`, http.StatusOK), nil
		}
		t.Fatalf("unexpected request %s", r.URL.Path)
		return nil, nil
	})

	// Time advances by one poll interval per cycle, so every cycle re-lists the
	// root; that is what makes an absence observation authoritative.
	start := p.now()
	interval := time.Duration(p.conf.Request_Interval) * time.Second
	cycle := 0
	step := func() {
		cycle++
		at := start.Add(time.Duration(cycle) * interval)
		p.now = func() time.Time { return at }
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatalf("cycle %d: %v", cycle, e)
		}
	}
	// Let discovery settle: organizations, their roles and users, then each
	// role's permissions.
	for i := 0; i < 4; i++ {
		step()
	}

	configured, err := p.conf.bind("organization-roles")
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.conf.bind("organizations")
	if err != nil {
		t.Fatal(err)
	}
	stored := func(key string) worklist {
		t.Helper()
		b, e := rt.Get(key)
		if e != nil {
			t.Fatalf("worklist %q: %v", key, e)
		}
		var list worklist
		if e = json.Unmarshal(b, &list); e != nil {
			t.Fatal(e)
		}
		return list
	}
	const (
		rolesChild  = "organization-roles/organization_id:O"
		permissions = "role-permissions/organization_id:O/role_id:R"
	)
	item, ok := stored(root.key() + keyChildren).Items[rolesChild]
	if !ok {
		t.Fatalf("the organization's roles were never discovered as child work: %d items",
			len(stored(root.key()+keyChildren).Items))
	}
	if item.StateKey == "" {
		t.Fatal("the discovered child never recorded its checkpoint namespace")
	}
	if item.StateKey == configured.key() {
		t.Fatalf("the discovered child and the configured selector share the namespace %q", item.StateKey)
	}
	configuredBefore, err := loadCheckpoint(rt, configured.key())
	if err != nil {
		t.Fatal(err)
	}
	if configuredBefore.Since.IsZero() || len(configuredBefore.Manifest) == 0 {
		t.Fatal("the configured selector never committed a checkpoint of its own")
	}
	if childBefore, e := loadCheckpoint(rt, item.StateKey); e != nil {
		t.Fatal(e)
	} else if childBefore.Since.IsZero() {
		t.Fatal("the discovered child never committed a checkpoint of its own")
	}
	// The configured selector is a parent in its own right, so its namespace
	// carries the stanza's child worklist as well as its checkpoint.
	if _, ok = stored(configured.key() + keyChildren).Items[permissions]; !ok {
		t.Fatalf("the configured selector never stored its own descendant work: %d items",
			len(stored(configured.key()+keyChildren).Items))
	}
	// Only organization-roles entries are counted here: both discovery chains
	// of this stanza reach the very same role-permissions child -- one dataset,
	// path and parameter set, reached from two different configured roots, and
	// so owned under two separate namespaces of its own (see
	// TestTwoDiscoveryRootsCannotShareADescendantNamespace) -- which says
	// nothing about the configured selector's own progress, the subject of this
	// regression.
	roleEntries := func() int {
		n := 0
		for _, ent := range base.entries {
			if source, _ := ent.GetEnumeratedValue("_source"); source == "organization-roles" {
				n++
			}
		}
		return n
	}
	before := roleEntries()
	if before < 2 {
		t.Fatalf("organization-roles entries=%d; the configured selector and the discovered child each collect the role under their own namespace and dedup it against their own manifest", before)
	}

	// The organization disappears. Absence retirement is the plugin's own
	// durable path: stage the obligation, release ownership, then compact the
	// child's whole prefix.
	present = false
	for i := 0; i < absentRetirementThreshold; i++ {
		step()
	}
	if _, still := stored(root.key() + keyChildren).Items[rolesChild]; still {
		t.Fatalf("the discovered child was never retired after %d complete scans without its organization",
			absentRetirementThreshold)
	}
	if since, e := getTime(rt, item.StateKey+keySince); e != nil || !since.IsZero() {
		t.Fatalf("the retirement did not clear the discovered child's checkpoint: %v %v", since, e)
	}
	// A deleted checkpoint would make the configured selector collect its whole
	// lookback window again, so re-emission is what proves the retirement did
	// not reach it. Its own lower bound keeps advancing on every cycle, so the
	// bound is only required never to move backwards.
	if got := roleEntries(); got != before {
		t.Fatalf("the configured selector re-emitted %d records when the discovered child retired", got-before)
	}
	after, err := loadCheckpoint(rt, configured.key())
	if err != nil {
		t.Fatal(err)
	}
	if after.Since.IsZero() || after.Since.Before(configuredBefore.Since) || len(after.Manifest) == 0 {
		t.Fatalf("retiring the discovered child rewound the configured selector: since=%s want >= %s manifest=%d",
			after.Since, configuredBefore.Since, len(after.Manifest))
	}
	if _, ok = stored(configured.key() + keyChildren).Items[permissions]; !ok {
		t.Fatal("retiring the discovered child destroyed the configured selector's child worklist")
	}

	// The remaining two durable records a retirement writes through live under
	// the configured selector's prefix as well, so the call a retirement makes
	// -- compact on the child's prefix -- must not reach them. Seeded here
	// rather than driven, because nothing retires a configured selector's
	// descendant with a tombstone in this catalog.
	descendant, err := childConfig(configured, work{Dataset: "role-permissions", Parameter: []string{"organization_id:O", "role_id:R"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = putCleanup(rt, configured.key()+keyCleanup, []cleanupTask{{StateKey: descendant.key(), Retired: "revision"}}); err != nil {
		t.Fatal(err)
	}
	if err = putTombstones(rt, configured.key()+keyTombstones, []tombstone{{Key: permissions, Revision: "revision"}}); err != nil {
		t.Fatal(err)
	}
	if err = compact(rt, item.StateKey); err != nil {
		t.Fatal(err)
	}
	if tasks, e := loadCleanup(rt, configured.key()+keyCleanup); e != nil || len(tasks) != 1 {
		t.Fatalf("retiring the discovered child destroyed the configured selector's cleanup backlog: %v %v", tasks, e)
	}
	if keys, e := loadTombstones(rt, configured.key()+keyTombstones); e != nil || len(keys) != 1 {
		t.Fatalf("retiring the discovered child destroyed the configured selector's tombstone registry: %v %v", keys, e)
	}
	if _, e := rt.Get(configured.key() + keySince); e != nil {
		t.Fatalf("retiring the discovered child deleted the configured selector's lower bound: %v", e)
	}
	if _, ok = stored(configured.key() + keyChildren).Items[permissions]; !ok {
		t.Fatal("retiring the discovered child deleted the configured selector's child worklist")
	}
	if keys, e := loadTombstones(rt, configured.key()+keyTombstones); e != nil || !retiredIdentity(keys, permissions, "revision") {
		t.Fatalf("the logical tombstone was not retained: %v %v", keys, e)
	}
}

// TestCatalogNamesAreSafeKeyComponents pins the property both bounded
// catalog-name components of a state key rely on (see Config.key). A name
// holding the separator could run the owner segment into the dataset segment
// that follows it, and a name holding the key terminator could place one
// owner's key inside another owner's deletion prefix.
func TestCatalogNamesAreSafeKeyComponents(t *testing.T) {
	for name, d := range Datasets {
		if name != d.Name {
			t.Errorf("catalog entry %q declares the name %q, so a key could be named by one and owned by the other", name, d.Name)
		}
		if name == "" || strings.ContainsAny(name, "/#") {
			t.Errorf("catalog name %q cannot be a state-key component", name)
		}
		if !strings.HasPrefix(d.Path, "/") {
			t.Errorf("dataset %s path %q does not begin with the separator that follows its name in a state key", name, d.Path)
		}
	}
}

// TestDescendantPathsCarryEveryRootParameter is the catalog contract that lets
// a discovered state key name its owner with the root's bounded catalog name
// alone (see Config.key). Two configured roots of the same dataset differ only
// in the parameter values their path consumes, so the owner name separates them
// only if every descendant resolves those same parameters into its own path.
// Were a descendant ever to drop one, two roots of one dataset configured with
// different values for it would hand that descendant a single shared
// owner-qualified key, and the owner component would have to carry the root's
// parameters as well.
func TestDescendantPathsCarryEveryRootParameter(t *testing.T) {
	placeholders := func(name string) map[string]bool {
		out := map[string]bool{}
		for _, m := range placeholder.FindAllStringSubmatch(Datasets[name].Path, -1) {
			out[m[1]] = true
		}
		return out
	}
	roots := 0
	for name := range Datasets {
		descendants := deeperDescendants(name)
		for _, s := range childSpecs(name) {
			descendants[s.name] = true
		}
		if len(descendants) == 0 {
			continue
		}
		roots++
		for _, descendant := range sortedKeys(descendants) {
			resolved := placeholders(descendant)
			for _, param := range sortedKeys(placeholders(name)) {
				if !resolved[param] {
					t.Errorf("root %s is configured with %s but its descendant %s does not resolve %s into its own path; two %s roots with different %s values would share one owner-qualified %s key",
						name, param, descendant, param, name, param, descendant)
				}
			}
		}
	}
	if roots == 0 {
		t.Fatal("no dataset in the catalog has descendants, so this contract covers nothing")
	}
}

// TestTwoDiscoveryRootsCannotShareADescendantNamespace covers the other half of
// checkpoint ownership: not whether a key was reached by configuration or by
// discovery, but which configured discovery root owns it.
//
// A stanza keeps one child worklist per configured root, and the accepted
// configuration below reaches one descendant from two of them: selecting
// "organizations" and "organization-roles" with Parameter="organization_id:O",
// the organizations worklist reaches role R's permissions through organization
// O's roles, while the organization-roles worklist reaches the very same
// role-permissions -- one dataset, path and parameter set -- directly. Each
// worklist schedules, retries, retires, capacity-evicts and compacts its own
// entry, so a single shared namespace let either one delete a descendant
// prefix the other still owned, resetting a manifest and cursor still in use
// and replaying records or restarting a traversal under the surviving owner.
// Each root must therefore own its own descendant namespace, each namespace
// must lie outside the other's deletion boundary, and the stored key must be
// the one the scheduling and reclamation paths derive -- they recover it from
// the root plus the work item, which is what makes a compaction reach only its
// own owner's state.
func TestTwoDiscoveryRootsCannotShareADescendantNamespace(t *testing.T) {
	db, err := storage.OpenBoltHandler(filepath.Join(t.TempDir(), "state.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bucket, err := db.GetBucketWriter("plugin")
	if err != nil {
		t.Fatal(err)
	}
	p, base := setup(t, "organizations")
	rt := persistedRuntime{Runtime: base, bucket: bucket}
	p.conf.Dataset = []string{"organizations", "organization-roles"}
	p.conf.Parameter = []string{"organization_id:O"}
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 10
	p.conf.Max_Pending = maxPendingWorkItems
	p.conf.path = ""
	if err = p.conf.Verify(); err != nil {
		t.Fatal(err)
	}

	present := true
	permissionCalls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			if !present {
				return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
			}
			return reply(`{"data":[{"uuid":"O"}],"has_more":false}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/users"):
			return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/permissions"):
			permissionCalls++
			return reply(`{"data":[{"resource_type":"organization","resource_id":"O","action":"read"}],"has_more":false}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/roles"):
			return reply(`{"data":[{"id":"R","updated_at":"2026-09-07T12:00:00Z"}],"has_more":false}`, http.StatusOK), nil
		}
		t.Fatalf("unexpected request %s", r.URL.Path)
		return nil, nil
	})

	// Time advances by one poll interval per cycle, so every cycle re-lists
	// each root; that is what makes an absence observation authoritative.
	start := p.now()
	interval := time.Duration(p.conf.Request_Interval) * time.Second
	cycle := 0
	step := func() {
		cycle++
		at := start.Add(time.Duration(cycle) * interval)
		p.now = func() time.Time { return at }
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatalf("cycle %d: %v", cycle, e)
		}
	}
	// Let both chains settle: organizations, their roles and users, then each
	// role's permissions.
	for i := 0; i < 4; i++ {
		step()
	}

	root, err := p.conf.bind("organizations")
	if err != nil {
		t.Fatal(err)
	}
	configured, err := p.conf.bind("organization-roles")
	if err != nil {
		t.Fatal(err)
	}
	stored := func(key string) worklist {
		t.Helper()
		b, e := rt.Get(key)
		if e != nil {
			t.Fatalf("worklist %q: %v", key, e)
		}
		var list worklist
		if e = json.Unmarshal(b, &list); e != nil {
			t.Fatal(e)
		}
		return list
	}
	const permissions = "role-permissions/organization_id:O/role_id:R"
	fromRoot, ok := stored(root.key() + keyChildren).Items[permissions]
	if !ok {
		t.Fatalf("the organizations root never reached the role's permissions: %d items",
			len(stored(root.key()+keyChildren).Items))
	}
	fromConfigured, ok := stored(configured.key() + keyChildren).Items[permissions]
	if !ok {
		t.Fatalf("the organization-roles root never reached the role's permissions: %d items",
			len(stored(configured.key()+keyChildren).Items))
	}
	if fromRoot.StateKey == "" || fromConfigured.StateKey == "" {
		t.Fatalf("a worklist never recorded its descendant's namespace: %q %q",
			fromRoot.StateKey, fromConfigured.StateKey)
	}

	// The precondition this regression exists for: both roots really do resolve
	// the same descendant, in one source namespace. Deriving each key the way
	// the scheduling and reclamation paths do -- from the root plus the work
	// item -- also proves the stored key is the one a compaction would delete.
	descendant := work{Dataset: "role-permissions", Parameter: []string{"organization_id:O", "role_id:R"}}
	rootDescendant := descendant
	rootDescendant.Slot, rootDescendant.StateKey = fromRoot.Slot, fromRoot.StateKey
	configuredDescendant := descendant
	configuredDescendant.Slot, configuredDescendant.StateKey = fromConfigured.Slot, fromConfigured.StateKey
	viaRoot, err := childConfig(root, rootDescendant)
	if err != nil {
		t.Fatal(err)
	}
	viaConfigured, err := childConfig(configured, configuredDescendant)
	if err != nil {
		t.Fatal(err)
	}
	if viaRoot.dataset.Name != viaConfigured.dataset.Name || viaRoot.path != viaConfigured.path ||
		viaRoot.sourcePrefix != viaConfigured.sourcePrefix {
		t.Fatalf("precondition: both roots must reach one resolved descendant: %s%s vs %s%s",
			viaRoot.dataset.Name, viaRoot.path, viaConfigured.dataset.Name, viaConfigured.path)
	}
	if fromRoot.StateKey != viaRoot.key() || fromConfigured.StateKey != viaConfigured.key() {
		t.Fatalf("a stored descendant key is not the one its owner derives: stored=%q derived=%q / stored=%q derived=%q",
			fromRoot.StateKey, viaRoot.key(), fromConfigured.StateKey, viaConfigured.key())
	}
	if fromRoot.StateKey == fromConfigured.StateKey {
		t.Fatalf("two discovery roots own one descendant namespace %q", fromRoot.StateKey)
	}
	if strings.HasPrefix(fromRoot.StateKey, fromConfigured.StateKey) ||
		strings.HasPrefix(fromConfigured.StateKey, fromRoot.StateKey) {
		t.Fatalf("one owner's descendant namespace is reachable by deleting the other's: %q %q",
			fromRoot.StateKey, fromConfigured.StateKey)
	}
	// Every key either owner can write for that descendant has to stay outside
	// the other's deletion boundary.
	for _, suffix := range []string{keySince, keyManifest, keyWalkCursor, keyWalkMan, walkDeltaKey("", 0)} {
		if strings.HasPrefix(fromRoot.StateKey+suffix, fromConfigured.StateKey) ||
			strings.HasPrefix(fromConfigured.StateKey+suffix, fromRoot.StateKey) {
			t.Fatalf("deleting one owner's descendant prefix would remove the other's %s", suffix)
		}
	}
	// Inside one discovery root the same descendant stays one logical owner
	// however it is reached. Discovery binds it from the intermediate child
	// that found it, while scheduling and reclamation bind it from the root
	// plus the work item; both must name the one namespace, or a retirement
	// would reclaim a prefix the worklist is not holding.
	rolesWork, ok := stored(root.key() + keyChildren).Items["organization-roles/organization_id:O"]
	if !ok {
		t.Fatal("the organizations root never retained its organization-roles child")
	}
	rolesChild, err := childConfig(root, rolesWork)
	if err != nil {
		t.Fatal(err)
	}
	viaChild, err := childConfig(rolesChild, rootDescendant)
	if err != nil {
		t.Fatal(err)
	}
	if viaChild.key() != viaRoot.key() {
		t.Fatalf("one discovery root split a descendant it reaches once into %q and %q",
			viaChild.key(), viaRoot.key())
	}

	rootBefore, err := loadCheckpoint(rt, fromRoot.StateKey)
	if err != nil {
		t.Fatal(err)
	}
	configuredBefore, err := loadCheckpoint(rt, fromConfigured.StateKey)
	if err != nil {
		t.Fatal(err)
	}
	if rootBefore.Since.IsZero() || len(rootBefore.Manifest) == 0 ||
		configuredBefore.Since.IsZero() || len(configuredBefore.Manifest) == 0 {
		t.Fatalf("each owner must have collected the permissions under its own namespace: %v/%d %v/%d",
			rootBefore.Since, len(rootBefore.Manifest), configuredBefore.Since, len(configuredBefore.Manifest))
	}
	permissionEntries := func() int {
		n := 0
		for _, ent := range base.entries {
			if source, _ := ent.GetEnumeratedValue("_source"); source == "role-permissions" {
				n++
			}
		}
		return n
	}

	// The organization disappears, so the organizations root retires its roles
	// child and cascades to that child's permissions descendant. That is the
	// plugin's own durable path: stage the obligation, release ownership, then
	// compact the descendant's whole prefix. The organization-roles root is
	// configured with organization O directly, so it keeps listing the role and
	// still owns the identical permissions descendant throughout.
	present = false
	for i := 0; i < absentRetirementThreshold; i++ {
		step()
	}
	if _, still := stored(root.key() + keyChildren).Items[permissions]; still {
		t.Fatalf("the organizations root never retired its permissions descendant after %d complete scans without the organization",
			absentRetirementThreshold)
	}
	if since, e := getTime(rt, fromRoot.StateKey+keySince); e != nil || !since.IsZero() {
		t.Fatalf("the retirement did not clear the retiring owner's descendant checkpoint: %v %v", since, e)
	}
	surviving, ok := stored(configured.key() + keyChildren).Items[permissions]
	if !ok || surviving.StateKey != fromConfigured.StateKey {
		t.Fatalf("the other root lost its own descendant work: present=%v key=%q want %q",
			ok, surviving.StateKey, fromConfigured.StateKey)
	}
	after, err := loadCheckpoint(rt, fromConfigured.StateKey)
	if err != nil {
		t.Fatal(err)
	}
	if after.Since.IsZero() || after.Since.Before(configuredBefore.Since) || len(after.Manifest) == 0 {
		t.Fatalf("compacting one root's descendant reset the other's checkpoint: since=%s want >= %s manifest=%d",
			after.Since, configuredBefore.Since, len(after.Manifest))
	}

	// And the surviving owner's retained dedup state is really still usable: an
	// hour on, its hourly child refresh re-lists the identical permission
	// record, which a surviving manifest suppresses and a reset one would
	// re-emit.
	calls, before := permissionCalls, permissionEntries()
	at := p.now().Add(2 * time.Hour)
	p.now = func() time.Time { return at }
	if _, err = p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if permissionCalls == calls {
		t.Fatal("the surviving owner never re-listed its permissions, so this case proves nothing about its retained state")
	}
	if got := permissionEntries(); got != before {
		t.Fatalf("the surviving owner re-emitted %d permission records after the other root's descendant was compacted", got-before)
	}
}
