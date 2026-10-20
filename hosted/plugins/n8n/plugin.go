/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package n8n collects metadata from the n8n public API.
package n8n

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	// recordStateEntrySize is the width of one durable per-record state entry: a
	// SHA-256 digest of the record identity followed by a SHA-256 fingerprint of
	// the normalized record body. Fixed width keeps the serialized set
	// unambiguous without a delimiter.
	recordStateEntrySize = 2 * sha256.Size

	// defaultMaxTrackedRecords bounds the durable per-record state kept for one
	// dataset. Collapsing a larger snapshot back to a single aggregate digest
	// would reintroduce the whole-snapshot re-emission this state exists to
	// prevent, so an oversized snapshot fails closed and says so instead.
	defaultMaxTrackedRecords = 250_000
)

type recordState = [recordStateEntrySize]byte

type normalizedSnapshotRecord struct {
	fingerprint [sha256.Size]byte
	page        int
}

type Plugin struct {
	conf       *Config
	maxTracked int
	maxPending int
	tagMu      sync.Mutex
	tags       map[string]entry.EntryTag
}

var _ hosted.Job = (*Plugin)(nil)

func New(conf *Config) *Plugin {
	return &Plugin{
		conf: conf, maxTracked: defaultMaxTrackedRecords, maxPending: defaultMaxRawRecords,
		tags: make(map[string]entry.EntryTag),
	}
}

func (p *Plugin) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	credential, err := p.conf.credential()
	if err != nil {
		return nil, fmt.Errorf("load Credential or Credential-File: %w", err)
	}
	client, err := newClient(p.conf.Base_URL, credential, p.conf.Requests_Per_Minute, p.conf.Page_Size, p.conf.Max_Pages, nil)
	if err != nil {
		return nil, err
	}

	var failures []error
	var unavailable []string
	for _, definition := range p.conf.definitionsCopy() {
		err := p.collect(ctx, rt, client, definition)
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		var unavailableErr *datasetUnavailableError
		if errors.As(err, &unavailableErr) {
			unavailable = append(unavailable, definition.Name)
			rt.Warn("n8n Dataset unavailable", log.KV("dataset", definition.Name), log.KV("http_status", unavailableErr.Status))
			continue
		}
		failures = append(failures, fmt.Errorf("Dataset %s: %w", definition.Name, err))
		rt.Error("n8n Dataset collection failed", log.KV("dataset", definition.Name), log.KVErr(err))
	}
	if len(unavailable) > 0 {
		sort.Strings(unavailable)
		rt.SetWarn("n8n Datasets unavailable: " + strings.Join(unavailable, ", "))
	} else {
		rt.ClearWarn()
	}
	if len(failures) > 0 {
		return nil, errors.Join(failures...)
	}
	return p.conf.ContinueAfterInterval(), nil
}

func (p *Plugin) collect(ctx context.Context, rt hosted.Runtime, client *client, definition datasetDefinition) (err error) {
	stateKey := p.conf.stateKey(definition)
	previous, err := rt.Get(stateKey)
	if err != nil {
		if !errors.Is(err, storage.ErrStorageNotFound) {
			return err
		}
		previous = nil
	}
	emitted, ok := parseRecordState(previous)
	if !ok {
		// State that does not parse cannot be trusted to say what was already
		// emitted. Starting from empty costs one duplicate snapshot and heals
		// itself, where failing would wedge the dataset until an operator noticed.
		rt.Warn("n8n discarding unreadable snapshot state",
			log.KV("dataset", definition.Name), log.KV("bytes", len(previous)))
		emitted = nil
	}
	identities := make(map[[sha256.Size]byte]normalizedSnapshotRecord)
	current := make(map[recordState]struct{})
	// This cache records successful enqueues from an incomplete poll. Binding it
	// to the completed manifest prevents stale progress surviving a later full
	// snapshot (and suppressing a deleted record's eventual reappearance).
	pendingKey := stateKey + "/inflight"
	baseDigest := sha256.Sum256(previous)
	pending := make(map[recordState]struct{})
	var eviction []recordState
	pendingBytes, getErr := rt.Get(pendingKey)
	if getErr != nil && !errors.Is(getErr, storage.ErrStorageNotFound) {
		return getErr
	}
	if len(pendingBytes) >= sha256.Size && bytes.Equal(pendingBytes[:sha256.Size], baseDigest[:]) {
		if saved, valid := parseRecordState(pendingBytes[sha256.Size:]); valid && saved.len() <= p.maxPending {
			for i := 0; i < saved.len(); i++ {
				var pair recordState
				copy(pair[:], saved.at(i))
				pending[pair] = struct{}{}
				eviction = append(eviction, pair)
			}
		} else {
			rt.Warn("n8n discarding unreadable in-flight state", log.KV("dataset", definition.Name))
		}
	}
	pendingChanged, completed := false, false
	defer func() {
		if !completed && pendingChanged {
			encoded := encodeRecordState(pending)
			progress := make([]byte, sha256.Size+len(encoded))
			copy(progress, baseDigest[:])
			copy(progress[sha256.Size:], encoded)
			err = errors.Join(err, rt.Put(pendingKey, progress))
		}
	}()
	evictionIndex := 0
	var evicted map[recordState]struct{}
	var tag entry.EntryTag
	tagReady := false
	written := 0
	result, err := client.collectStreaming(ctx, definition, func(source collectedRecord, body *normalizedRecord) error {
		identityDigest := source.identity
		fingerprint := source.fingerprint
		page := source.page
		if existing, duplicate := identities[identityDigest]; duplicate {
			if existing.page == page {
				if existing.fingerprint != fingerprint {
					return fmt.Errorf("Dataset %s returned conflicting records with one identity on page %d", definition.Name, page)
				}
				return nil
			}
			if page < existing.page {
				return fmt.Errorf("Dataset %s returned records out of page order", definition.Name)
			}
			if existing.fingerprint == fingerprint {
				existing.page = page
				identities[identityDigest] = existing
				return nil
			}
			// A mutable record can change while pagination is in flight. For a
			// repeated identity on a later page, keep the latest observed version.
			identities[identityDigest] = normalizedSnapshotRecord{
				fingerprint: fingerprint, page: page,
			}
		} else {
			if len(identities) >= p.maxTracked {
				return fmt.Errorf("Dataset %s exceeds the per-dataset limit of %d tracked records; reduce how many records n8n retains for this dataset or remove it from the Dataset list",
					definition.Name, p.maxTracked)
			}
			identities[identityDigest] = normalizedSnapshotRecord{
				fingerprint: fingerprint, page: page,
			}
		}

		var state recordState
		copy(state[:], identityDigest[:])
		copy(state[sha256.Size:], fingerprint[:])
		current[state] = struct{}{}
		_, alreadyPending := pending[state]
		_, replayed := evicted[state]
		if !emitted.contains(state) && !alreadyPending && !replayed {
			// The current poll cannot exceed the raw-record limit. When source
			// churn fills the replay cache, remember an evicted prior pair for this
			// poll so encountering it later does not enqueue it again.
			if len(pending) >= p.maxPending {
				removed := false
				for evictionIndex < len(eviction) {
					old := eviction[evictionIndex]
					evictionIndex++
					if _, protected := current[old]; !protected {
						delete(pending, old)
						if evicted == nil {
							evicted = make(map[recordState]struct{})
						}
						evicted[old] = struct{}{}
						removed = true
						break
					}
				}
				if !removed {
					return errors.New("n8n in-flight state exceeded the raw-record budget")
				}
			}
			if !tagReady {
				var tagErr error
				tag, tagErr = p.negotiateTag(rt, p.conf.tag(definition))
				if tagErr != nil {
					return tagErr
				}
				tagReady = true
			}
			payload, err := body.payload()
			if err != nil {
				return err
			}
			// The ingest muxer stamps SRC because this entry leaves it unset.
			if err := rt.Write(entry.Entry{Tag: tag, TS: entry.FromStandard(source.timestamp), Data: payload}); err != nil {
				return err
			}
			written++
			pending[state] = struct{}{}
			pendingChanged = true
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Emission is per record, but the checkpoint is still the whole snapshot, so
	// a deletion drops out of state without re-emitting everything that survived.
	encoded := encodeRecordState(current)
	// Commit the completed manifest before clearing its replay cache. Runtime
	// Put calls may be separate durable transactions, so the opposite order
	// creates a restart window with neither the new manifest nor replay progress.
	if !bytes.Equal(previous, encoded) {
		if err := rt.Put(stateKey, encoded); err != nil {
			return err
		}
	}
	// The standard Job adapter calls the BucketWriter sync function after this
	// successful Handle returns. Keep the plugin on the Runtime contract: Write
	// entries, update state, and let the adapter own persistence and liveness.
	completed = true
	if len(pendingBytes) > 0 {
		if err := rt.Put(pendingKey, nil); err != nil {
			return err
		}
	}
	rt.Info("n8n Dataset collection completed",
		log.KV("dataset", definition.Name), log.KV("records", result.Count),
		log.KV("written", written), log.KV("tag", p.conf.tag(definition)))
	return nil
}

// recordStateSet is the durable per-record snapshot state, held in exactly the
// sorted fixed-width form it is stored in. Membership is a binary search, so a
// poll never has to allocate a map of the whole previous snapshot just to find
// out that nothing changed.
type recordStateSet []byte

// parseRecordState validates the stored state. A missing key parses as an empty
// set. Anything that is not a whole number of entries in the strictly
// increasing order encodeRecordState produces is reported as unparseable rather
// than being read as a phantom record or silently truncated.
func parseRecordState(blob []byte) (recordStateSet, bool) {
	if len(blob)%recordStateEntrySize != 0 {
		return nil, false
	}
	for offset := recordStateEntrySize; offset < len(blob); offset += recordStateEntrySize {
		if bytes.Compare(blob[offset-recordStateEntrySize:offset], blob[offset:offset+recordStateEntrySize]) >= 0 {
			return nil, false
		}
	}
	return recordStateSet(blob), true
}

func (s recordStateSet) len() int { return len(s) / recordStateEntrySize }

func (s recordStateSet) at(i int) []byte {
	return s[i*recordStateEntrySize : (i+1)*recordStateEntrySize]
}

func (s recordStateSet) contains(key recordState) bool {
	i := sort.Search(s.len(), func(i int) bool { return bytes.Compare(s.at(i), key[:]) >= 0 })
	return i < s.len() && bytes.Equal(s.at(i), key[:])
}

// encodeRecordState serializes the snapshot state in sorted order so that an
// unchanged snapshot always produces byte-identical state.
func encodeRecordState(state map[recordState]struct{}) []byte {
	keys := make([]recordState, 0, len(state))
	for key := range state {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b recordState) int { return bytes.Compare(a[:], b[:]) })
	blob := make([]byte, 0, len(keys)*recordStateEntrySize)
	for _, key := range keys {
		blob = append(blob, key[:]...)
	}
	return blob
}

func (p *Plugin) negotiateTag(rt hosted.Runtime, name string) (entry.EntryTag, error) {
	p.tagMu.Lock()
	defer p.tagMu.Unlock()
	if tag, ok := p.tags[name]; ok {
		return tag, nil
	}
	tag, err := rt.NegotiateTag(name)
	if err != nil {
		return 0, fmt.Errorf("negotiate tag %q: %w", name, err)
	}
	p.tags[name] = tag
	return tag, nil
}
