/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package slack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
)

const (
	stateVersionKey                = "slack-audit/state/version"
	stateCompletedThroughKey       = "slack-audit/state/completed-through"
	stateActiveKey                 = "slack-audit/state/active"
	stateGenerationKey             = "slack-audit/state/generation"
	stateOldestKey                 = "slack-audit/state/oldest"
	stateLatestKey                 = "slack-audit/state/latest"
	stateCursorKey                 = "slack-audit/state/cursor"
	stateCursorCycleKey            = "slack-audit/state/cursor-cycle"
	stateCursorPageCountKey        = "slack-audit/state/cursor-page-count"
	stateRetryAtKey                = "slack-audit/state/retry-at"
	receiptHeadKey                 = "slack-audit/receipts/head"
	receiptSlotPrefix              = "slack-audit/receipts"
	overlapReceiptHeadKey          = "slack-audit/overlap-receipts/head"
	overlapReceiptSlotPrefix       = "slack-audit/overlap-receipts"
	cursorMarkerSlotPrefix         = "slack-audit/cursor-markers"
	stateVersion             int64 = 1
	receiptCapacity          int64 = 10000
	cursorMarkerCapacity     int64 = 10000
	maximumCursorSize              = 4096
)

type pollState struct {
	Version          int64
	CompletedThrough int64
	Active           bool
	Generation       int64
	Oldest           int64
	Latest           int64
	Cursor           string
	CursorCycle      int64
	CursorPageCount  int64
	RetryAt          int64
}

func newPollState() pollState { return pollState{Version: stateVersion} }

func loadPollState(store hosted.Storage) (pollState, error) {
	state := newPollState()
	var err error
	if state.Version, err = loadOptionalInt64(store, stateVersionKey, stateVersion); err != nil {
		return pollState{}, err
	}
	if state.CompletedThrough, err = loadOptionalInt64(store, stateCompletedThroughKey, 0); err != nil {
		return pollState{}, err
	}
	active, err := loadOptionalInt64(store, stateActiveKey, 0)
	if err != nil {
		return pollState{}, err
	}
	if active != 0 && active != 1 {
		return pollState{}, fmt.Errorf("Slack active-state marker is invalid: %d", active)
	}
	state.Active = active == 1
	if state.Generation, err = loadOptionalInt64(store, stateGenerationKey, 0); err != nil {
		return pollState{}, err
	}
	if state.Oldest, err = loadOptionalInt64(store, stateOldestKey, 0); err != nil {
		return pollState{}, err
	}
	if state.Latest, err = loadOptionalInt64(store, stateLatestKey, 0); err != nil {
		return pollState{}, err
	}
	if state.Cursor, err = loadOptionalString(store, stateCursorKey); err != nil {
		return pollState{}, err
	}
	if state.CursorCycle, err = loadOptionalInt64(store, stateCursorCycleKey, 0); err != nil {
		return pollState{}, err
	}
	if state.CursorPageCount, err = loadOptionalInt64(store, stateCursorPageCountKey, 0); err != nil {
		return pollState{}, err
	}
	if state.RetryAt, err = loadOptionalInt64(store, stateRetryAtKey, 0); err != nil {
		return pollState{}, err
	}
	if err := validatePollState(state); err != nil {
		return pollState{}, err
	}
	if !state.Active {
		// Completion deliberately leaves traversal keys untouched. The active
		// marker is committed last, so inactive traversal values are stale and
		// must never resume a completed or partially initialized window.
		state.Oldest = 0
		state.Latest = 0
		state.Cursor = ""
		state.CursorCycle = 0
		state.CursorPageCount = 0
		state.RetryAt = 0
	}
	return state, nil
}

func loadOptionalInt64(store hosted.Storage, key string, fallback int64) (int64, error) {
	value, err := store.GetInt64(key)
	if errors.Is(err, storage.ErrStorageNotFound) {
		return fallback, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load Slack state key %q: %w", key, err)
	}
	return value, nil
}

func loadOptionalString(store hosted.Storage, key string) (string, error) {
	value, err := store.GetString(key)
	if errors.Is(err, storage.ErrStorageNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load Slack state key %q: %w", key, err)
	}
	return value, nil
}

func validatePollState(state pollState) error {
	if state.Version != stateVersion {
		return fmt.Errorf("unsupported Slack state version %d", state.Version)
	}
	if state.CompletedThrough < 0 {
		return errors.New("Slack completed-through checkpoint is negative")
	}
	if state.Generation < 0 {
		return errors.New("Slack generation is negative")
	}
	if state.Active {
		if state.Generation == 0 {
			return errors.New("active Slack state has no generation")
		}
		if state.Oldest < 0 || state.Latest < state.Oldest {
			return errors.New("active Slack state has invalid time bounds")
		}
		if len(state.Cursor) > maximumCursorSize {
			return errors.New("Slack pagination cursor exceeds its size bound")
		}
		if state.CursorCycle <= 0 {
			return errors.New("active Slack state has no cursor cycle")
		}
		if state.CursorPageCount < 0 || state.CursorPageCount > cursorMarkerCapacity {
			return errors.New("active Slack state has an invalid cursor page count")
		}
		if state.RetryAt < 0 {
			return errors.New("active Slack state has an invalid retry deadline")
		}
	}
	return nil
}

func beginPollState(store hosted.Storage, state *pollState, oldest, latest int64) error {
	if state == nil {
		return errors.New("nil Slack state")
	}
	if oldest < 0 || latest < oldest {
		return errors.New("new Slack window has invalid time bounds")
	}
	if state.Generation == math.MaxInt64 {
		return errors.New("Slack state generation exhausted")
	}
	next := pollState{
		Version:          stateVersion,
		CompletedThrough: state.CompletedThrough,
		Active:           true,
		Generation:       state.Generation + 1,
		Oldest:           oldest,
		Latest:           latest,
		CursorCycle:      1,
	}
	if next.Generation == 0 {
		next.Generation = 1
	}
	if err := validatePollState(next); err != nil {
		return err
	}
	// Active is committed last. A crash during these writes leaves an
	// inactive state whose incomplete traversal values are ignored on load.
	writes := []struct {
		key   string
		value int64
	}{
		{stateVersionKey, next.Version},
		{stateGenerationKey, next.Generation},
		{stateOldestKey, next.Oldest},
		{stateLatestKey, next.Latest},
		{stateCursorCycleKey, next.CursorCycle},
		{stateCursorPageCountKey, 0},
		{stateRetryAtKey, 0},
	}
	for _, write := range writes {
		if err := store.PutInt64(write.key, write.value); err != nil {
			return fmt.Errorf("begin Slack state at %q: %w", write.key, err)
		}
	}
	if err := store.PutString(stateCursorKey, ""); err != nil {
		return fmt.Errorf("begin Slack state at %q: %w", stateCursorKey, err)
	}
	if err := store.PutInt64(stateActiveKey, 1); err != nil {
		return fmt.Errorf("activate Slack state: %w", err)
	}
	*state = next
	return nil
}

func persistCursorState(store hosted.Storage, state *pollState, cursor string) error {
	if state == nil || !state.Active {
		return errors.New("cannot persist a cursor without active Slack state")
	}
	if len(cursor) > maximumCursorSize {
		return errors.New("Slack pagination cursor exceeds its size bound")
	}
	if err := store.PutString(stateCursorKey, cursor); err != nil {
		return fmt.Errorf("persist Slack cursor: %w", err)
	}
	state.Cursor = cursor
	return nil
}

func persistRetryAtState(store hosted.Storage, state *pollState, retryAt int64) error {
	if state == nil || !state.Active {
		return errors.New("cannot persist a retry deadline without active Slack state")
	}
	if retryAt < 0 {
		return errors.New("Slack retry deadline is negative")
	}
	if err := store.PutInt64(stateRetryAtKey, retryAt); err != nil {
		return fmt.Errorf("persist Slack retry deadline: %w", err)
	}
	state.RetryAt = retryAt
	return nil
}

func verifyActiveStateWritable(store hosted.Storage, state pollState, ledgers ...*receiptLedger) error {
	if !state.Active || len(ledgers) == 0 {
		return errors.New("cannot verify incomplete Slack state")
	}
	if err := store.PutInt64(stateActiveKey, 1); err != nil {
		return fmt.Errorf("verify Slack core state is writable: %w", err)
	}
	for _, ledger := range ledgers {
		if ledger == nil || ledger.generation != state.Generation {
			return errors.New("cannot verify incomplete Slack receipt state")
		}
		if err := store.PutInt64(ledger.headKey, ledger.next); err != nil {
			return fmt.Errorf("verify Slack receipt state is writable: %w", err)
		}
	}
	if err := store.PutInt64(stateCursorPageCountKey, state.CursorPageCount); err != nil {
		return fmt.Errorf("verify Slack cursor marker state is writable: %w", err)
	}
	return nil
}

func resetCursorState(store hosted.Storage, state *pollState) error {
	if state == nil || !state.Active {
		return errors.New("cannot reset a cursor without active Slack state")
	}
	if state.CursorCycle == math.MaxInt64 {
		return errors.New("Slack cursor cycle exhausted")
	}
	nextCycle := state.CursorCycle + 1
	// Clear the page count before advancing the cycle. Any crash between these
	// writes can repeat a page, but the receipt ledgers still suppress its
	// records and persisted cursor markers are scoped to their cycle.
	if err := store.PutInt64(stateCursorPageCountKey, 0); err != nil {
		return fmt.Errorf("clear Slack cursor marker count: %w", err)
	}
	if err := store.PutInt64(stateCursorCycleKey, nextCycle); err != nil {
		return fmt.Errorf("advance Slack cursor cycle: %w", err)
	}
	if err := store.PutString(stateCursorKey, ""); err != nil {
		return fmt.Errorf("reset Slack cursor: %w", err)
	}
	if err := store.PutInt64(stateRetryAtKey, 0); err != nil {
		return fmt.Errorf("clear Slack retry deadline: %w", err)
	}
	state.CursorCycle = nextCycle
	state.CursorPageCount = 0
	state.Cursor = ""
	state.RetryAt = 0
	return nil
}

func completePollState(store hosted.Storage, state *pollState) error {
	if state == nil || !state.Active {
		return errors.New("cannot complete inactive Slack state")
	}
	// The durable checkpoint is committed before Active is cleared. A crash
	// between them can only replay the active page through the receipt ledger;
	// it cannot advance past unacknowledged records.
	if err := store.PutInt64(stateCompletedThroughKey, state.Latest); err != nil {
		return fmt.Errorf("persist Slack completed-through checkpoint: %w", err)
	}
	if err := store.PutInt64(stateActiveKey, 0); err != nil {
		return fmt.Errorf("deactivate Slack state: %w", err)
	}
	state.CompletedThrough = state.Latest
	state.Active = false
	state.Oldest = 0
	state.Latest = 0
	state.Cursor = ""
	state.CursorCycle = 0
	state.CursorPageCount = 0
	state.RetryAt = 0
	return nil
}

type receiptMarker struct {
	Sequence   int64  `json:"sequence"`
	Generation int64  `json:"generation"`
	Digest     string `json:"digest"`
}

type receiptLedger struct {
	generation int64
	next       int64
	seen       map[string]int
	slots      map[int64]string
	headKey    string
	slotPrefix string
}

func loadReceiptLedger(store hosted.Storage, generation int64) (*receiptLedger, error) {
	return loadReceiptLedgerFor(store, generation, receiptHeadKey, receiptSlotPrefix)
}

func loadOverlapReceiptLedger(store hosted.Storage, generation int64) (*receiptLedger, error) {
	return loadReceiptLedgerFor(store, generation, overlapReceiptHeadKey, overlapReceiptSlotPrefix)
}

func loadReceiptLedgerFor(store hosted.Storage, generation int64, headKey, slotPrefix string) (*receiptLedger, error) {
	head, err := store.GetInt64(headKey)
	if err != nil {
		if !errors.Is(err, storage.ErrStorageNotFound) {
			return nil, fmt.Errorf("load Slack receipt head: %w", err)
		}
		head = 0
	}
	if head < 0 {
		return nil, errors.New("Slack receipt head is negative")
	}
	ledger := &receiptLedger{
		generation: generation,
		next:       head,
		seen:       make(map[string]int),
		slots:      make(map[int64]string),
		headKey:    headKey,
		slotPrefix: slotPrefix,
	}
	start := max(int64(0), head-receiptCapacity)
	var orphan *receiptMarker
	for sequence := start; sequence < head; sequence++ {
		marker, found, err := readReceipt(store, slotPrefix, sequence)
		if err != nil {
			return nil, err
		}
		if found && head >= receiptCapacity && sequence == start && marker.Sequence == head {
			copy := marker
			orphan = &copy
			continue
		}
		if !found || marker.Sequence != sequence {
			return nil, errors.New("Slack receipt ring is incomplete or inconsistent")
		}
		ledger.commitReceipt(marker.Sequence, marker.Digest)
	}

	// The slot is written before the head. Recover the one possible orphan
	// produced when the bounded head write failed after an acknowledged entry.
	if orphan == nil {
		marker, found, err := readReceipt(store, slotPrefix, head)
		if err != nil {
			return nil, err
		}
		if found && marker.Sequence == head {
			copy := marker
			orphan = &copy
		}
	}
	if orphan != nil {
		if head == math.MaxInt64 {
			return nil, errors.New("Slack receipt sequence exhausted")
		}
		ledger.next = head + 1
		if err := store.PutInt64(headKey, ledger.next); err != nil {
			return nil, fmt.Errorf("recover Slack receipt head: %w", err)
		}
		ledger.commitReceipt(orphan.Sequence, orphan.Digest)
	}
	return ledger, nil
}

func receiptSlotKey(sequence int64) string {
	return receiptSlotKeyFor(receiptSlotPrefix, sequence)
}

func overlapReceiptSlotKey(sequence int64) string {
	return receiptSlotKeyFor(overlapReceiptSlotPrefix, sequence)
}

func receiptSlotKeyFor(prefix string, sequence int64) string {
	return fmt.Sprintf("%s/%d", prefix, sequence%receiptCapacity)
}

func readReceipt(store hosted.Storage, slotPrefix string, sequence int64) (receiptMarker, bool, error) {
	body, err := store.Get(receiptSlotKeyFor(slotPrefix, sequence))
	if err != nil {
		if errors.Is(err, storage.ErrStorageNotFound) {
			return receiptMarker{}, false, nil
		}
		return receiptMarker{}, false, fmt.Errorf("load Slack receipt: %w", err)
	}
	var marker receiptMarker
	if err := json.Unmarshal(body, &marker); err != nil {
		return receiptMarker{}, false, fmt.Errorf("decode Slack receipt: %w", err)
	}
	if marker.Sequence < 0 || marker.Generation == 0 || len(marker.Digest) != sha256.Size*2 {
		return receiptMarker{}, false, errors.New("Slack receipt is invalid")
	}
	if _, err := hex.DecodeString(marker.Digest); err != nil {
		return receiptMarker{}, false, errors.New("Slack receipt digest is invalid")
	}
	return marker, true, nil
}

func (ledger *receiptLedger) contains(digest string) bool {
	return ledger.seen[digest] > 0
}

func (ledger *receiptLedger) commitReceipt(sequence int64, digest string) {
	slot := sequence % receiptCapacity
	if prior, ok := ledger.slots[slot]; ok {
		if ledger.seen[prior] <= 1 {
			delete(ledger.seen, prior)
		} else {
			ledger.seen[prior]--
		}
	}
	ledger.slots[slot] = digest
	ledger.seen[digest]++
}

func (ledger *receiptLedger) remember(store hosted.Storage, digest string) error {
	if ledger == nil || ledger.generation == 0 {
		return errors.New("Slack receipt ledger is not initialized")
	}
	if ledger.contains(digest) {
		return nil
	}
	if ledger.next == math.MaxInt64 {
		return errors.New("Slack receipt sequence exhausted")
	}
	marker := receiptMarker{Sequence: ledger.next, Generation: ledger.generation, Digest: digest}
	body, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	if err := store.Put(receiptSlotKeyFor(ledger.slotPrefix, ledger.next), body); err != nil {
		return fmt.Errorf("persist Slack receipt: %w", err)
	}
	next := ledger.next + 1
	if err := store.PutInt64(ledger.headKey, next); err != nil {
		return fmt.Errorf("persist Slack receipt head: %w", err)
	}
	ledger.commitReceipt(ledger.next, digest)
	ledger.next = next
	return nil
}

type cursorMarker struct {
	Page       int64  `json:"page"`
	Generation int64  `json:"generation"`
	Cycle      int64  `json:"cycle"`
	Digest     string `json:"digest"`
}

type cursorLedger struct {
	generation int64
	cycle      int64
	next       int64
	seen       map[string]struct{}
}

func loadCursorLedger(store hosted.Storage, state pollState) (*cursorLedger, error) {
	if !state.Active || state.Generation == 0 || state.CursorCycle <= 0 {
		return nil, errors.New("cannot load cursor markers without active Slack state")
	}
	if state.CursorPageCount < 0 || state.CursorPageCount > cursorMarkerCapacity {
		return nil, errors.New("Slack cursor marker count exceeds its bound")
	}
	ledger := &cursorLedger{
		generation: state.Generation,
		cycle:      state.CursorCycle,
		next:       state.CursorPageCount,
		seen:       make(map[string]struct{}, state.CursorPageCount),
	}
	for page := int64(0); page < state.CursorPageCount; page++ {
		body, err := store.Get(cursorMarkerSlotKey(page))
		if err != nil {
			if errors.Is(err, storage.ErrStorageNotFound) {
				return nil, errors.New("Slack cursor marker ledger is incomplete")
			}
			return nil, fmt.Errorf("load Slack cursor marker: %w", err)
		}
		var marker cursorMarker
		if err := json.Unmarshal(body, &marker); err != nil {
			return nil, fmt.Errorf("decode Slack cursor marker: %w", err)
		}
		if marker.Page != page || marker.Generation != state.Generation || marker.Cycle != state.CursorCycle || len(marker.Digest) != sha256.Size*2 {
			return nil, errors.New("Slack cursor marker is invalid")
		}
		if _, err := hex.DecodeString(marker.Digest); err != nil {
			return nil, errors.New("Slack cursor marker digest is invalid")
		}
		ledger.seen[marker.Digest] = struct{}{}
	}
	return ledger, nil
}

func cursorMarkerSlotKey(page int64) string {
	return fmt.Sprintf("%s/%d", cursorMarkerSlotPrefix, page)
}

func cursorDigest(cursor string) string {
	digest := sha256.Sum256([]byte(cursor))
	return hex.EncodeToString(digest[:])
}

func (ledger *cursorLedger) contains(cursor string) bool {
	if ledger == nil || cursor == "" {
		return false
	}
	_, ok := ledger.seen[cursorDigest(cursor)]
	return ok
}

func (ledger *cursorLedger) full() bool {
	return ledger != nil && ledger.next >= cursorMarkerCapacity
}

func (ledger *cursorLedger) remember(store hosted.Storage, state *pollState, cursor string) error {
	if ledger == nil || state == nil || !state.Active || ledger.generation != state.Generation || ledger.cycle != state.CursorCycle || ledger.next != state.CursorPageCount {
		return errors.New("Slack cursor marker ledger is not initialized")
	}
	if cursor == "" {
		return errors.New("cannot persist an empty Slack cursor marker")
	}
	digest := cursorDigest(cursor)
	if _, ok := ledger.seen[digest]; ok {
		return nil
	}
	if ledger.next >= cursorMarkerCapacity {
		return errors.New("Slack pagination exceeded the cursor marker capacity")
	}
	marker := cursorMarker{Page: ledger.next, Generation: ledger.generation, Cycle: ledger.cycle, Digest: digest}
	body, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	if err := store.Put(cursorMarkerSlotKey(ledger.next), body); err != nil {
		return fmt.Errorf("persist Slack cursor marker: %w", err)
	}
	next := ledger.next + 1
	if err := store.PutInt64(stateCursorPageCountKey, next); err != nil {
		return fmt.Errorf("persist Slack cursor marker count: %w", err)
	}
	ledger.seen[digest] = struct{}{}
	ledger.next = next
	state.CursorPageCount = next
	return nil
}

func recordReceiptDigest(identity string, compact []byte) string {
	fingerprint := sha256.Sum256(compact)
	digest := sha256.Sum256(append(append([]byte(identity), 0), fingerprint[:]...))
	return hex.EncodeToString(digest[:])
}

func skippedReceiptDigest(kind string, raw []byte) string {
	digest := sha256.Sum256(append(append([]byte(kind), 0), raw...))
	return hex.EncodeToString(digest[:])
}
