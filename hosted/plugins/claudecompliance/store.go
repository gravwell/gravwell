package claudecompliance

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
)

// Checkpoint state is held in the runtime's own key/value store under one
// prefix per dataset, with every independently-meaningful scalar in its own
// typed key rather than inside a serialized object.
//
// Two values resist that treatment and are deliberately stored as a single
// serialized value each: the dedup manifest and the child worklist. Both are
// maps that have to be enumerated and selectively shrunk -- the manifest to
// evict its least recently updated identity once it is full, the worklist to
// order pending work by last attempt and to choose a capacity victim. The
// hosted.Storage interface (hosted/interface.go) offers only Get and Put on
// an exact key: it has no element listing and no batch. Spreading
// either map over one key per element would therefore make it impossible to
// enumerate for ordering, impossible to bound, and impossible to shrink --
// every evicted identity would leak a key forever. They stay whole, and
// everything that does not need enumeration does not.
//
// Because a manifest stays whole, rewriting it is proportional to the whole
// retained state. A page walk therefore does not rewrite it per page: each
// page appends one bounded manifestDelta describing only that page's own
// change, and the complete maps are rewritten once per maxWalkDeltas pages
// and once when the traversal completes.
//
// hosted.Storage offers neither deletion nor enumeration, so a key this plugin
// writes is a key it can only ever overwrite. Reclaiming a checkpoint
// therefore means clearing its values (see compact) rather than removing its
// keys, and every namespace is drawn from a finite set so that the physical
// key space stays bounded whatever the vendor's child churn or the operator's
// credential rotations do: a discovered child's namespace is named by a slot
// index taken from its root's durable worklist (see childStateKey), and a
// credential's namespace by a slot index taken from the stanza's source
// record (see slotPrefix).
const (
	keySince       = "/since"
	keyManifest    = "/manifest"
	keyHistory     = "/history"
	keyWalkCursor  = "/walk/cursor"
	keyWalkSince   = "/walk/since"
	keyWalkUntil   = "/walk/until"
	keyWalkStarted = "/walk/started"
	keyWalkHistory = "/walk/full-history"
	keyWalkEmpty   = "/walk/empty-pages"
	keyWalkMan     = "/walk/manifest"
	// keyWalkDeltas counts the valid records of the per-page manifest journal;
	// keyWalkDelta is the prefix each of those records is stored under.
	keyWalkDeltas = "/walk/deltas"
	keyWalkDelta  = "/walk/delta/"
	keyChildren   = "/children"
	// keyCleanup holds the durable obligations to reclaim retired children's
	// checkpoint namespaces, and keyTombstones the bounded registry of the
	// deletion tombstones a stanza currently retains.
	keyCleanup    = "/cleanup"
	keyTombstones = "/tombstones"

	// historyComplete is the stored marker for a finished full-history pass.
	historyComplete = "complete"

	// maxWalkDeltas bounds the per-page manifest journal. Once it is full the
	// next page folds the journal into the two complete maps, so the only
	// write proportional to the whole retained state happens once per this
	// many pages instead of on every page.
	maxWalkDeltas = 16
)

// checkpoint is the in-memory view of one dataset's stored progress. A
// deletion tombstone is deliberately not part of it: a tombstone belongs to a
// logical child identity, not to the physical namespace that identity happens
// to occupy, because that namespace is a reusable slot (see tombstone).
type checkpoint struct {
	Since           time.Time
	Manifest        manifest
	Walk            *traversal
	HistoryComplete bool
}

// manifestDelta is one page's change to the retained dedup state: the
// identities the page recorded in the in-progress walk manifest, the
// identities it consumed out of the committed manifest, and the identities
// eviction removed from the walk manifest to make room. Applying a delta is
// idempotent, so re-applying one after an interrupted fold cannot corrupt the
// retained state.
type manifestDelta struct {
	Added   manifest `json:"a,omitempty"`
	Dropped []string `json:"d,omitempty"`
	Removed []string `json:"r,omitempty"`
}

func (m manifestDelta) empty() bool {
	return len(m.Added) == 0 && len(m.Dropped) == 0 && len(m.Removed) == 0
}

// apply folds one journal record into the two complete maps.
func (m manifestDelta) apply(committed, walk manifest) {
	for _, key := range m.Dropped {
		delete(committed, key)
	}
	for _, key := range m.Removed {
		delete(walk, key)
	}
	for key, value := range m.Added {
		walk[key] = value
	}
}

// missing reports whether err just means the key has never been written.
func missing(err error) bool { return errors.Is(err, storage.ErrStorageNotFound) }

func getTime(rt hosted.Runtime, key string) (time.Time, error) {
	v, err := rt.GetTime(key)
	if missing(err) {
		return time.Time{}, nil
	}
	return v, err
}

func getString(rt hosted.Runtime, key string) (string, error) {
	v, err := rt.GetString(key)
	if missing(err) {
		return "", nil
	}
	return v, err
}

func getInt64(rt hosted.Runtime, key string) (int64, error) {
	v, err := rt.GetInt64(key)
	if missing(err) {
		return 0, nil
	}
	return v, err
}

func getManifest(rt hosted.Runtime, key string) (manifest, error) {
	b, err := rt.Get(key)
	if missing(err) {
		return manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := manifest{}
	if len(b) == 0 {
		return m, nil
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, errors.New("invalid Compliance manifest state")
	}
	if m == nil {
		m = manifest{}
	}
	return m, nil
}

func putManifest(rt hosted.Runtime, key string, m manifest) error {
	if m == nil {
		m = manifest{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return rt.Put(key, b)
}

// walkDeltaKey names one journal record of a page walk.
func walkDeltaKey(prefix string, index int64) string {
	return prefix + keyWalkDelta + strconv.FormatInt(index, 10)
}

func getManifestDelta(rt hosted.Runtime, key string) (manifestDelta, error) {
	var d manifestDelta
	b, err := rt.Get(key)
	if missing(err) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if len(b) == 0 {
		return d, nil
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return d, errors.New("invalid Compliance manifest journal state")
	}
	return d, nil
}

func putManifestDelta(rt hosted.Runtime, key string, d manifestDelta) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return rt.Put(key, b)
}

// writeManifests rewrites both complete maps and empties the journal. The
// maps are written before the journal count is cleared, so an interruption
// between them only re-applies records whose content is already folded in.
func writeManifests(rt hosted.Runtime, prefix string, committed, walk manifest) error {
	if err := putManifest(rt, prefix+keyManifest, committed); err != nil {
		return err
	}
	if err := putManifest(rt, prefix+keyWalkMan, walk); err != nil {
		return err
	}
	return rt.PutInt64(prefix+keyWalkDeltas, 0)
}

// loadCheckpoint reads one dataset's progress from its discrete keys.
func loadCheckpoint(rt hosted.Runtime, prefix string) (checkpoint, error) {
	var cp checkpoint
	var err error
	if cp.Since, err = getTime(rt, prefix+keySince); err != nil {
		return cp, err
	}
	if cp.Manifest, err = getManifest(rt, prefix+keyManifest); err != nil {
		return cp, err
	}
	history, err := getString(rt, prefix+keyHistory)
	if err != nil {
		return cp, err
	}
	cp.HistoryComplete = history == historyComplete
	cursor, err := getString(rt, prefix+keyWalkCursor)
	if err != nil {
		return cp, err
	}
	if cursor == "" {
		return cp, nil
	}
	w := traversal{Cursor: cursor}
	if w.Since, err = getTime(rt, prefix+keyWalkSince); err != nil {
		return cp, err
	}
	if w.Until, err = getTime(rt, prefix+keyWalkUntil); err != nil {
		return cp, err
	}
	if w.Started, err = getTime(rt, prefix+keyWalkStarted); err != nil {
		return cp, err
	}
	if w.ConsecutiveEmptyPages, err = getInt64(rt, prefix+keyWalkEmpty); err != nil {
		return cp, err
	}
	if w.ConsecutiveEmptyPages < 0 || w.ConsecutiveEmptyPages > maxConsecutiveEmptyPages {
		return cp, errors.New("invalid Compliance empty-page checkpoint")
	}
	full, err := getString(rt, prefix+keyWalkHistory)
	if err != nil {
		return cp, err
	}
	w.FullHistory = full == historyComplete
	if w.Manifest, err = getManifest(rt, prefix+keyWalkMan); err != nil {
		return cp, err
	}
	// Fold the page journal on top of the two complete maps. Only the counted
	// records are read, so a record written by a page whose count never
	// reached the store is ignored rather than half-applied.
	if w.Deltas, err = getInt64(rt, prefix+keyWalkDeltas); err != nil {
		return cp, err
	}
	if w.Deltas < 0 || w.Deltas > maxWalkDeltas {
		return cp, errors.New("invalid Compliance manifest journal checkpoint")
	}
	for i := int64(0); i < w.Deltas; i++ {
		d, e := getManifestDelta(rt, walkDeltaKey(prefix, i))
		if e != nil {
			return cp, e
		}
		d.apply(cp.Manifest, w.Manifest)
	}
	cp.Walk = &w
	return cp, nil
}

// commitPage durably records an in-progress page walk. The cursor is written
// last. An interrupted first page leaves no valid walk; an interrupted later
// page leaves the preceding cursor valid. Either case may replay records, but
// no prefix of these writes can advance the committed lower bound or skip
// data.
func commitPage(rt hosted.Runtime, prefix string, committed manifest, w *traversal, delta manifestDelta, manifestsChanged bool) error {
	// The committed manifest shrinks as the walk visits identities out of it
	// (see handleOne); persisting that is what keeps the two manifests a single
	// shared budget across a resumed walk rather than letting the stored pair
	// grow past the configured limit. Persisting it by rewriting both complete
	// maps would make every page cost the whole retained state, so a page
	// appends only its own bounded change and the journal is folded once it is
	// full. An empty page changes neither manifest and writes no record at all.
	if manifestsChanged {
		if w.Deltas >= maxWalkDeltas {
			if err := writeManifests(rt, prefix, committed, w.Manifest); err != nil {
				return err
			}
			w.Deltas = 0
		} else {
			if err := putManifestDelta(rt, walkDeltaKey(prefix, w.Deltas), delta); err != nil {
				return err
			}
			if err := rt.PutInt64(prefix+keyWalkDeltas, w.Deltas+1); err != nil {
				return err
			}
			w.Deltas++
		}
	}
	if err := rt.PutTime(prefix+keyWalkSince, w.Since); err != nil {
		return err
	}
	if err := rt.PutTime(prefix+keyWalkUntil, w.Until); err != nil {
		return err
	}
	if err := rt.PutTime(prefix+keyWalkStarted, w.Started); err != nil {
		return err
	}
	full := ""
	if w.FullHistory {
		full = historyComplete
	}
	if err := rt.PutString(prefix+keyWalkHistory, full); err != nil {
		return err
	}
	if err := rt.PutInt64(prefix+keyWalkEmpty, w.ConsecutiveEmptyPages); err != nil {
		return err
	}
	return rt.PutString(prefix+keyWalkCursor, w.Cursor)
}

// commitDataset records a completed traversal. The order is chosen so that
// every prefix of it is loss-free: clearing the walk first can cause a rescan,
// and interruption after writing a pruned manifest but before the lower bound
// can replay duplicates. Losing only the history marker costs one extra
// full-history pass. No prefix of these writes can advance the lower bound past
// unwritten data.
func commitDataset(rt hosted.Runtime, prefix string, since time.Time, m manifest, historyDone bool) error {
	// Clear the walk manifest and its journal before the cursor marker. If
	// interrupted between these writes, loading the still-valid cursor can only
	// replay records from the old lower bound; it cannot skip them. This
	// ordering also keeps the physical primary-plus-walk state within one
	// manifest budget after every successful write instead of hiding stale walk
	// data behind an empty cursor.
	if err := putManifest(rt, prefix+keyWalkMan, manifest{}); err != nil {
		return err
	}
	if err := rt.PutInt64(prefix+keyWalkDeltas, 0); err != nil {
		return err
	}
	if err := rt.PutInt64(prefix+keyWalkEmpty, 0); err != nil {
		return err
	}
	if err := rt.PutString(prefix+keyWalkCursor, ""); err != nil {
		return err
	}
	if err := putManifest(rt, prefix+keyManifest, m); err != nil {
		return err
	}
	if err := rt.PutTime(prefix+keySince, since); err != nil {
		return err
	}
	if !historyDone {
		return nil
	}
	return rt.PutString(prefix+keyHistory, historyComplete)
}

// compact reclaims one checkpoint namespace. hosted.Storage cannot remove a
// key, so reclamation clears every value the namespace can hold -- including
// the whole page journal, which the exact-key path previously left behind --
// and leaves the keys themselves in place. That is sufficient and bounded:
// each namespace is one slot out of a finite set (see childStateKey and
// slotPrefix), so a reclaimed namespace is reused rather than abandoned and
// the stanza's physical key space cannot grow with child churn.
//
// Every value is cleared, not just the dedup state, because the next occupant
// of a reused slot must observe exactly what it would observe in a slot that
// had never been written: no lower bound, no cursor, no manifest and no
// history marker.
func compact(rt hosted.Runtime, prefix string) error {
	if err := rt.PutString(prefix+keyWalkCursor, ""); err != nil {
		return err
	}
	if err := rt.PutInt64(prefix+keyWalkEmpty, 0); err != nil {
		return err
	}
	if err := putManifest(rt, prefix+keyWalkMan, manifest{}); err != nil {
		return err
	}
	for i := int64(0); i < maxWalkDeltas; i++ {
		if err := putManifestDelta(rt, walkDeltaKey(prefix, i), manifestDelta{}); err != nil {
			return err
		}
	}
	if err := rt.PutInt64(prefix+keyWalkDeltas, 0); err != nil {
		return err
	}
	if err := putManifest(rt, prefix+keyManifest, manifest{}); err != nil {
		return err
	}
	if err := rt.PutTime(prefix+keyWalkSince, time.Time{}); err != nil {
		return err
	}
	if err := rt.PutTime(prefix+keyWalkUntil, time.Time{}); err != nil {
		return err
	}
	if err := rt.PutTime(prefix+keyWalkStarted, time.Time{}); err != nil {
		return err
	}
	if err := rt.PutString(prefix+keyWalkHistory, ""); err != nil {
		return err
	}
	if err := rt.PutString(prefix+keyHistory, ""); err != nil {
		return err
	}
	// The lower bound is cleared last: every prefix of this write order leaves
	// a namespace that reads as "collect from the configured lookback", never
	// one that reads as "already collected up to some time" over state that is
	// no longer there.
	return rt.PutTime(prefix+keySince, time.Time{})
}

// cleanupTask is one durable obligation to reclaim a retired child's
// checkpoint namespace. It is recorded before the worklist releases ownership
// of the child and is acted on only once the stored worklist no longer owns
// it, so a failed worklist write can never orphan an owned checkpoint and a
// failed reclamation is retried on a later cycle or after a restart.
type cleanupTask struct {
	StateKey string
	// Slot is the checkpoint slot StateKey names. It is carried here so that a
	// slot whose reclamation is still outstanding is never handed to a new
	// child: the obligation to clear it outlives the worklist entry that owned
	// it, and reusing it first would let the clearing wipe its successor.
	Slot int `json:",omitempty"`
	// Key is the logical work identity this obligation belongs to. A deletion
	// tombstone is recorded against that identity rather than against the
	// physical slot, so a slot later reused by a different child cannot
	// inherit a tombstone it never earned.
	Key string `json:",omitempty"`
	// Retired is the revision to leave behind as a deletion tombstone, and is
	// empty for every reclamation that is not a vendor-confirmed deletion.
	Retired string `json:",omitempty"`
}

func loadCleanup(rt hosted.Runtime, key string) ([]cleanupTask, error) {
	b, err := rt.Get(key)
	if missing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	var tasks []cleanupTask
	if err = json.Unmarshal(b, &tasks); err != nil {
		return nil, errors.New("invalid Compliance checkpoint cleanup state")
	}
	if len(tasks) > maxCleanupTasks {
		return nil, errors.New("invalid Compliance checkpoint cleanup state")
	}
	return tasks, nil
}

func putCleanup(rt hosted.Runtime, key string, tasks []cleanupTask) error {
	b, err := json.Marshal(tasks)
	if err != nil {
		return err
	}
	return rt.Put(key, b)
}

// tombstone records that one logical child work identity was retired because
// the vendor confirmed its parent deleted, together with the parent revision
// that proved it. It is keyed by the worklist identity rather than by the
// child's checkpoint prefix: that prefix is a reusable slot, so a
// prefix-keyed tombstone would be inherited by whichever unrelated child the
// slot is handed to next and would suppress it forever.
type tombstone struct {
	Key      string `json:"k"`
	Revision string `json:"r,omitempty"`
}

// loadTombstones reads the bounded registry of deletion tombstones one
// configured root currently retains, oldest first.
func loadTombstones(rt hosted.Runtime, key string) ([]tombstone, error) {
	b, err := rt.Get(key)
	if missing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	var list []tombstone
	if err = json.Unmarshal(b, &list); err != nil {
		return nil, errors.New("invalid Compliance tombstone registry state")
	}
	if len(list) > maxTombstones {
		return nil, errors.New("invalid Compliance tombstone registry state")
	}
	return list, nil
}

func putTombstones(rt hosted.Runtime, key string, list []tombstone) error {
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return rt.Put(key, b)
}

// retiredIdentity reports whether list already holds a tombstone for the
// logical identity key under exactly revision. A different revision means the
// parent record changed after it was reported deleted -- an undeletion, for
// instance -- so the child becomes discoverable again.
func retiredIdentity(list []tombstone, key, revision string) bool {
	if key == "" || revision == "" {
		return false
	}
	for _, t := range list {
		if t.Key == key {
			return t.Revision == revision
		}
	}
	return false
}

// addTombstone records or refreshes one identity's tombstone, reclaiming the
// oldest once the registry is full. Forgetting a tombstone costs at most one
// more final drain of an already deleted child; it can never resurrect live
// data, and it is what keeps retirement bookkeeping bounded under sustained
// child churn.
func addTombstone(list []tombstone, key, revision string) ([]tombstone, bool) {
	if key == "" || revision == "" {
		return list, false
	}
	for i := range list {
		if list[i].Key != key {
			continue
		}
		if list[i].Revision == revision {
			return list, false
		}
		list[i].Revision = revision
		return list, true
	}
	list = append(list, tombstone{Key: key, Revision: revision})
	for len(list) > maxTombstones {
		list = list[1:]
	}
	return list, true
}

// loadWorklist reads one configured root's child worklist. The worklist is the
// ownership record for every discovered checkpoint slot under that root, so an
// unreadable one is an error rather than an empty list.
func loadWorklist(rt hosted.Runtime, key string) (worklist, error) {
	list := worklist{Items: map[string]work{}}
	b, err := rt.Get(key)
	if missing(err) {
		return list, nil
	}
	if err != nil {
		return list, err
	}
	if len(b) == 0 {
		return list, nil
	}
	if err = json.Unmarshal(b, &list); err != nil || list.Items == nil {
		return worklist{Items: map[string]work{}}, errors.New("invalid Compliance child-work state")
	}
	return list, nil
}

func putWorklist(rt hosted.Runtime, key string, list worklist) error {
	if list.Items == nil {
		list.Items = map[string]work{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return rt.Put(key, b)
}

// sourceRecord tracks the finite set of state namespaces this stanza can
// occupy. It holds only the one-way fingerprints that name an origin and
// credential, never credential material, and lives outside every namespace so
// that it remains findable after a rotation.
//
// A namespace is a numbered slot rather than the fingerprint itself. That is
// the difference that keeps the stanza's key space bounded: hosted.Storage
// cannot remove a key, so naming a namespace after the credential that
// selected it would leave one dead key set behind on every rotation, forever.
// A slot is instead cleared and handed back (see reclaimSlot), and a slot is
// reused only once it is completely clear, so no credential can ever observe
// another's checkpoint.
//
// Roots is what makes a slot's key space derivable without storage
// enumeration: it is the bounded list of configured dataset namespaces written
// under that slot, newest configured first. Everything else in a slot hangs off
// one of those roots -- its page walk, its dedup manifests, its child worklist,
// its reclamation backlog, its tombstone registry, and each of its discovered
// children's numbered checkpoint slots.
type sourceRecord struct {
	// Current is the fingerprint of the namespace this stanza collects into.
	Current string `json:",omitempty"`
	// Slots is indexed by slot number and is always maxSourceSlots long once
	// written. An empty Fingerprint means the slot is free and clear.
	Slots []sourceSlot `json:",omitempty"`
}

type sourceSlot struct {
	Fingerprint string   `json:",omitempty"`
	Roots       []string `json:",omitempty"`
}

// keySourceRecord is deliberately outside every "source/v4/" namespace prefix
// so reclaiming a superseded namespace cannot destroy the record that tracks
// it.
const keySourceRecord = "sources/v2"

const (
	// maxSourceSlots bounds how many physical state namespaces one stanza can
	// ever write into. Rotations are rare and a superseded namespace is
	// retained until a proven, complete cycle reclaims it, so a handful of
	// slots is ample; what matters is that the number is finite.
	maxSourceSlots = 4
	// maxTrackedRoots bounds the configured dataset namespaces remembered per
	// slot. An operator who edits a path Parameter leaves the previous
	// configured namespace behind, and this is what keeps that finite: the
	// least recently configured namespace is cleared and dropped rather than
	// retained forever. It is far above the 26-entry catalog, so a live
	// selector is never the one dropped.
	maxTrackedRoots = 64
	// maxReclaimPerCycle bounds how many checkpoint namespaces one cycle may
	// clear, so reclaiming a large superseded namespace cannot hold the runner
	// for an unbounded time. Progress is durable, so the remainder is simply
	// reclaimed by the next authorized cycle.
	maxReclaimPerCycle = 512
)

// errRootNamespaceRecycling defers collection while a stale configured root
// is being cleared. The root remains recorded until clearRoot reports that
// every child, cleanup obligation, registry and checkpoint key is clear.
var errRootNamespaceRecycling = errors.New("Compliance is reclaiming a stale configured state namespace; collection resumes once it is clear")

// slotPrefix names one of the finite state namespaces.
func slotPrefix(slot int) string {
	return "source/v4/" + strconv.Itoa(slot)
}

// superseded lists the fingerprints of occupied slots that are no longer the
// one this stanza collects into.
func (r sourceRecord) superseded() []string {
	var out []string
	for _, s := range r.Slots {
		if s.Fingerprint != "" && s.Fingerprint != r.Current {
			out = append(out, s.Fingerprint)
		}
	}
	return out
}

func loadSourceRecord(rt hosted.Runtime) (sourceRecord, error) {
	var r sourceRecord
	b, err := rt.Get(keySourceRecord)
	if missing(err) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if len(b) == 0 {
		return r, nil
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, errors.New("invalid Compliance source namespace state")
	}
	if len(r.Slots) > maxSourceSlots {
		return r, errors.New("invalid Compliance source namespace state")
	}
	return r, nil
}

func putSourceRecord(rt hosted.Runtime, r sourceRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return rt.Put(keySourceRecord, b)
}

// trackRoots records the configured dataset namespaces written under one slot.
// The currently configured ones are kept first, so a selector that is still in
// use can never be the entry the bound drops; only a namespace the
// configuration has stopped naming is cleared and forgotten.
func (r *sourceRecord) trackRoots(rt hosted.Runtime, slot int, roots []string) (bool, error) {
	s := &r.Slots[slot]
	seen := make(map[string]bool, len(roots)+len(s.Roots))
	merged := make([]string, 0, len(roots)+len(s.Roots))
	for _, group := range [][]string{roots, s.Roots} {
		for _, root := range group {
			if root == "" || seen[root] {
				continue
			}
			seen[root] = true
			merged = append(merged, root)
		}
	}
	for len(merged) > maxTrackedRoots {
		stale := merged[len(merged)-1]
		budget := maxReclaimPerCycle
		done, err := clearRoot(rt, stale, &budget)
		if err != nil {
			return false, err
		}
		if !done {
			return false, errRootNamespaceRecycling
		}
		merged = merged[:len(merged)-1]
	}
	if slices.Equal(merged, s.Roots) {
		return false, nil
	}
	s.Roots = merged
	return true, nil
}

// clearRoot reclaims one configured dataset namespace and everything that
// hangs off it, a bounded number of checkpoint namespaces per call. Progress
// is durable at every step -- a child is removed from the stored worklist only
// after its checkpoint is clear, and the root itself is cleared last -- so an
// interrupted reclamation resumes instead of restarting or being forgotten.
func clearRoot(rt hosted.Runtime, root string, budget *int) (bool, error) {
	list, err := loadWorklist(rt, root+keyChildren)
	if err != nil {
		return false, err
	}
	for len(list.Items) > 0 {
		if *budget <= 0 {
			return false, putWorklist(rt, root+keyChildren, list)
		}
		key := sortedWorkKeys(list.Items)[0]
		if stateKey := list.Items[key].StateKey; stateKey != "" {
			if err = compact(rt, stateKey); err != nil {
				return false, err
			}
		}
		delete(list.Items, key)
		*budget--
	}
	tasks, err := loadCleanup(rt, root+keyCleanup)
	if err != nil {
		return false, err
	}
	for i, task := range tasks {
		if *budget <= 0 {
			return false, putCleanup(rt, root+keyCleanup, tasks[i:])
		}
		if task.StateKey != "" {
			if err = compact(rt, task.StateKey); err != nil {
				return false, err
			}
		}
		*budget--
	}
	if err = putCleanup(rt, root+keyCleanup, nil); err != nil {
		return false, err
	}
	if err = putTombstones(rt, root+keyTombstones, nil); err != nil {
		return false, err
	}
	if err = putWorklist(rt, root+keyChildren, worklist{Items: map[string]work{}}); err != nil {
		return false, err
	}
	if err = compact(rt, root); err != nil {
		return false, err
	}
	*budget--
	return true, nil
}

// reclaimSlot clears every namespace recorded under one state slot. A root is
// dropped from the slot only once it is completely clear, and the slot reports
// done only once nothing is recorded under it, so a slot is never reused while
// any part of its predecessor's state is still readable.
func reclaimSlot(rt hosted.Runtime, slot *sourceSlot, budget *int) (bool, error) {
	for len(slot.Roots) > 0 {
		if *budget <= 0 {
			return false, nil
		}
		done, err := clearRoot(rt, slot.Roots[len(slot.Roots)-1], budget)
		if err != nil {
			return false, err
		}
		if !done {
			return false, nil
		}
		slot.Roots = slot.Roots[:len(slot.Roots)-1]
	}
	return true, nil
}

// reclaimSupersededSources reclaims every namespace slot this stanza no longer
// collects into. It is the second phase of a source handover and the only
// place a namespace is cleared, so its caller must call it only after this
// cycle has actually used the current namespace successfully (see
// sourceUseProof), completed without error, *and* left none of its own
// collection outstanding: a second cycle under the same unusable credential,
// another failed request, a cycle that collected one dataset and was refused
// the next for want of scope, and a cycle that ended cleanly while a traversal
// continuation or a still-Pending discovered child remained all authorize
// nothing. The record is written outside every namespace, so it survives the
// clearing it drives; a slot that the per-cycle budget could not finish stays
// recorded and is reclaimed by a later proven cycle rather than being
// forgotten. The current namespace is never a candidate.
func reclaimSupersededSources(rt hosted.Runtime, current string) error {
	if current == "" {
		return nil
	}
	record, err := loadSourceRecord(rt)
	if err != nil {
		return err
	}
	// The proof belongs to the namespace this cycle selected. If the stored
	// record no longer names it, this cycle has nothing to authorize.
	if record.Current != current || len(record.superseded()) == 0 {
		return nil
	}
	budget := maxReclaimPerCycle
	dirty := false
	for i := range record.Slots {
		if record.Slots[i].Fingerprint == "" || record.Slots[i].Fingerprint == current {
			continue
		}
		done, e := reclaimSlot(rt, &record.Slots[i], &budget)
		dirty = true
		if e != nil {
			// Keep whatever progress was made so a later cycle resumes rather
			// than clearing the same namespaces again, then report the failure.
			if pe := putSourceRecord(rt, record); pe != nil {
				return pe
			}
			return e
		}
		if done {
			record.Slots[i] = sourceSlot{}
		}
		if budget <= 0 {
			break
		}
	}
	if !dirty {
		return nil
	}
	return putSourceRecord(rt, record)
}

// sortedKeySet returns the keys of set in a stable order, so a serialized
// journal record or a retirement pass does not depend on map iteration order.
func sortedKeySet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
