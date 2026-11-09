package claudecompliance

import (
	"encoding/json"
	"errors"
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
// an exact key: it has no Delete, no prefix listing, and no batch. Spreading
// either map over one key per element would therefore make it impossible to
// enumerate for ordering, impossible to bound, and impossible to shrink --
// every evicted identity and every retired child would leak a key forever.
// They stay whole, and everything that does not need enumeration does not.
const (
	keySince       = "/since"
	keyManifest    = "/manifest"
	keyHistory     = "/history"
	keyRetired     = "/retired"
	keyWalkCursor  = "/walk/cursor"
	keyWalkSince   = "/walk/since"
	keyWalkUntil   = "/walk/until"
	keyWalkStarted = "/walk/started"
	keyWalkHistory = "/walk/full-history"
	keyWalkMan     = "/walk/manifest"
	keyChildren    = "/children"

	// historyComplete is the stored marker for a finished full-history pass.
	historyComplete = "complete"
)

// checkpoint is the in-memory view of one dataset's stored progress.
type checkpoint struct {
	Since           time.Time
	Manifest        manifest
	Walk            *traversal
	Retired         string
	HistoryComplete bool
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
	if cp.Retired, err = getString(rt, prefix+keyRetired); err != nil {
		return cp, err
	}
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
	full, err := getString(rt, prefix+keyWalkHistory)
	if err != nil {
		return cp, err
	}
	w.FullHistory = full == historyComplete
	if w.Manifest, err = getManifest(rt, prefix+keyWalkMan); err != nil {
		return cp, err
	}
	cp.Walk = &w
	return cp, nil
}

// commitPage durably records an in-progress page walk. The cursor is written
// last and is the marker that makes the walk valid, so a write interrupted
// part way through leaves no walk at all rather than a half-described one;
// the next cycle then rescans from the committed lower bound and the
// committed manifest suppresses the records it already wrote. No prefix of
// these writes can advance the committed lower bound, so none can skip data.
func commitPage(rt hosted.Runtime, prefix string, committed manifest, w traversal) error {
	// The committed manifest shrinks as the walk visits identities out of it
	// (see the compaction in handleOne); persisting it here is what keeps the
	// two manifests a single shared budget across a resumed walk rather than
	// letting the stored pair grow past the configured limit.
	if err := putManifest(rt, prefix+keyManifest, committed); err != nil {
		return err
	}
	if err := putManifest(rt, prefix+keyWalkMan, w.Manifest); err != nil {
		return err
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
	return rt.PutString(prefix+keyWalkCursor, w.Cursor)
}

// commitDataset records a completed traversal. The order is chosen so that
// every prefix of it is safe: clearing the walk first can only cause a
// rescan, writing the manifest before the lower bound can only strengthen
// deduplication, and losing only the history marker costs one extra
// full-history pass whose records the manifest already covers. No prefix of
// these writes can advance the lower bound past unwritten data.
func commitDataset(rt hosted.Runtime, prefix string, since time.Time, m manifest, historyDone bool) error {
	// Clear the walk manifest before its cursor marker. If interrupted between
	// these writes, loading the still-valid cursor can only replay records from
	// the old lower bound; it cannot skip them. This ordering also keeps the
	// physical primary-plus-walk state within one manifest budget after every
	// successful write instead of hiding stale walk data behind an empty cursor.
	if err := putManifest(rt, prefix+keyWalkMan, manifest{}); err != nil {
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

// compact clears a child's bulky state, optionally tombstoning it under the
// revision that proved it gone. Scalar progress is left alone: with discrete
// keys there is nothing to read-modify-write to preserve it.
func compact(rt hosted.Runtime, prefix, retired string) error {
	if err := rt.PutString(prefix+keyWalkCursor, ""); err != nil {
		return err
	}
	if err := putManifest(rt, prefix+keyWalkMan, manifest{}); err != nil {
		return err
	}
	if err := putManifest(rt, prefix+keyManifest, manifest{}); err != nil {
		return err
	}
	if retired == "" {
		return nil
	}
	return rt.PutString(prefix+keyRetired, retired)
}
