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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	Name    = "n8n"
	ID      = "n8n.ingesters.gravwell.io"
	Version = "1.0.0"

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

type datasetDefinition struct {
	Name         string
	TagKind      string
	Method       string
	Path         string
	RecordsField string
	CursorField  string
}

var datasetCatalog = map[string]datasetDefinition{
	"audit": {
		Name: "audit", TagKind: "audit", Method: "POST", Path: "/audit",
	},
	"credentials": n8nList("credentials", "creds"),
	"executions":  n8nList("executions", "executions"),
	"projects":    n8nList("projects", "projects"),
	"tags":        n8nList("tags", "tags"),
	"users":       n8nList("users", "users"),
	"variables":   n8nList("variables", "variables"),
	"workflows":   n8nList("workflows", "workflows"),
}

func n8nList(name, tagKind string) datasetDefinition {
	return datasetDefinition{
		Name: name, TagKind: tagKind, Method: "GET", Path: "/" + name,
		RecordsField: "data", CursorField: "nextCursor",
	}
}

func resolveDatasets(selected []string) ([]datasetDefinition, error) {
	if len(selected) == 0 {
		return nil, fmt.Errorf("Dataset is required; name each dataset to collect (%s)", strings.Join(catalogNames(), ", "))
	}
	seen := make(map[string]struct{}, len(selected))
	result := make([]datasetDefinition, 0, len(selected))
	for _, raw := range selected {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			return nil, fmt.Errorf("Dataset must not be empty")
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("Dataset %q is configured more than once", name)
		}
		definition, ok := datasetCatalog[name]
		if !ok {
			return nil, fmt.Errorf("unsupported Dataset %q", name)
		}
		seen[name] = struct{}{}
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func catalogNames() []string {
	names := make([]string, 0, len(datasetCatalog))
	for name := range datasetCatalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type recordState = [recordStateEntrySize]byte

type normalizedSnapshotRecord struct {
	fingerprint [sha256.Size]byte
	page        int
}

type pendingIdentitySummary struct {
	count          int
	fingerprintXOR [sha256.Size]byte
}

func addPendingIdentity(summary map[[sha256.Size]byte]pendingIdentitySummary, state recordState) {
	var identity, fingerprint [sha256.Size]byte
	copy(identity[:], state[:sha256.Size])
	copy(fingerprint[:], state[sha256.Size:])
	current := summary[identity]
	current.count++
	for i := range current.fingerprintXOR {
		current.fingerprintXOR[i] ^= fingerprint[i]
	}
	summary[identity] = current
}

func removePendingIdentity(summary map[[sha256.Size]byte]pendingIdentitySummary, state recordState) {
	var identity, fingerprint [sha256.Size]byte
	copy(identity[:], state[:sha256.Size])
	copy(fingerprint[:], state[sha256.Size:])
	current := summary[identity]
	if current.count <= 1 {
		delete(summary, identity)
		return
	}
	current.count--
	for i := range current.fingerprintXOR {
		current.fingerprintXOR[i] ^= fingerprint[i]
	}
	summary[identity] = current
}

func pendingIdentityDiffers(summary map[[sha256.Size]byte]pendingIdentitySummary, state recordState) bool {
	var identity, fingerprint [sha256.Size]byte
	copy(identity[:], state[:sha256.Size])
	copy(fingerprint[:], state[sha256.Size:])
	current, ok := summary[identity]
	if !ok {
		return false
	}
	return current.count > 1 || current.fingerprintXOR != fingerprint
}

type Plugin struct {
	conf          *Config
	maxTracked    int
	maxPending    int
	maxRecordSize int64
	tagMu         sync.Mutex
	tags          map[string]entry.EntryTag
	limiterMu     sync.Mutex
	limiter       requestLimiter
	now           func() time.Time
}

var _ hosted.Job = (*Plugin)(nil)

func New(conf *Config) *Plugin {
	return &Plugin{
		conf: conf, maxTracked: defaultMaxTrackedRecords, maxPending: defaultMaxRawRecords,
		maxRecordSize: maxNormalizedRecordSize,
		tags:          make(map[string]entry.EntryTag),
		now:           time.Now,
	}
}

func (p *Plugin) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	credential, err := p.conf.credential()
	if err != nil {
		return nil, fmt.Errorf("load Credential or Credential-File: %w", err)
	}
	client, err := newClient(p.conf.Base_URL, credential, p.conf.Requests_Per_Minute, p.conf.Page_Size, p.conf.Max_Pages, nil)
	if err != nil {
		return nil, err
	}
	p.configureClient(client)

	var failures []error
	var unavailable []string
	for _, definition := range p.conf.definitionsCopy() {
		err := p.collect(ctx, rt, client, definition)
		if err == nil {
			continue
		}
		if canceled, companion := splitCancellation(err); canceled {
			if companion == nil {
				if len(failures) == 0 {
					return nil, context.Canceled
				}
				// A clean shutdown must not erase a hard failure already observed
				// for an earlier dataset. Return only those failures so the shared
				// adapter cannot mistake the joined result for clean cancellation.
				return nil, errors.Join(failures...)
			}
			// Keep cleanup and checkpoint failures visible, but remove the
			// cancellation sentinel so the shared adapter reports the failure
			// instead of treating the entire joined error as clean shutdown.
			err = companion
		}
		if unavailableErr, isolated := unaccompaniedUnavailable(err); isolated {
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p.conf.ContinueAfterInterval(), nil
}

func (p *Plugin) configureClient(client *client) {
	client.limiter = p.sharedRequestLimiter()
	client.recordLimit = p.maxRecordSize
	client.includeExecutionData = p.conf.Include_Execution_Data
}

func expandedExecutions(client *client, definition datasetDefinition) bool {
	return definition.Name == "executions" && client.includeExecutionData
}

func oversizedExecutionPayload(source collectedRecord, limit int64) ([]byte, error) {
	marker := struct {
		ID       string `json:"id"`
		Status   string `json:"status,omitempty"`
		Gravwell struct {
			Reason string `json:"reason"`
			Limit  int64  `json:"limit"`
		} `json:"_gravwell"`
	}{ID: source.identityText, Status: source.statusText}
	marker.Gravwell.Reason = "n8n execution exceeds the per-record safety limit"
	marker.Gravwell.Limit = limit
	return json.Marshal(marker)
}

func nonterminalExecutionStatus(status string) bool {
	switch status {
	case "new", "running", "unknown", "waiting":
		return true
	default:
		return false
	}
}

func executionHighwaterKey(stateKey string) string { return stateKey + "/highwater" }
func executionActiveKey(stateKey string) string    { return stateKey + "/active" }

func encodeExecutionIDs(ids map[string]struct{}, limit int) ([]byte, error) {
	if len(ids) > limit {
		return nil, errors.New("execution id state exceeds tracked-record limit")
	}
	values := make([]string, 0, len(ids))
	for id := range ids {
		if !validExecutionID(id) {
			return nil, errors.New("invalid execution id state")
		}
		values = append(values, id)
	}
	sort.Strings(values)
	return json.Marshal(values)
}

func decodeExecutionIDs(raw []byte, limit int) (map[string]struct{}, bool) {
	if len(raw) == 0 {
		return make(map[string]struct{}), true
	}
	var values []string
	if limit < 0 {
		return nil, false
	}
	maxEncodedBytes := 2 // Empty JSON array: []
	if limit > 0 {
		// Each maximum-length ID contributes maxExecutionIDLength bytes plus
		// two JSON quotes; commas separate IDs, and the array adds two brackets.
		maxEncodedBytes += limit*(maxExecutionIDLength+2) + limit - 1
	}
	if len(raw) > maxEncodedBytes || json.Unmarshal(raw, &values) != nil || len(values) > limit {
		return nil, false
	}
	result := make(map[string]struct{}, len(values))
	for _, id := range values {
		if !validExecutionID(id) {
			return nil, false
		}
		if _, duplicate := result[id]; duplicate {
			return nil, false
		}
		result[id] = struct{}{}
	}
	return result, true
}

func (p *Plugin) sharedRequestLimiter() requestLimiter {
	p.limiterMu.Lock()
	defer p.limiterMu.Unlock()
	if p.limiter == nil {
		p.limiter = newRequestLimiter(p.conf.Requests_Per_Minute)
	}
	return p.limiter
}

func sourceExecutionWindowEnd(runnerTime, sourceTime time.Time) (time.Time, error) {
	if sourceTime.IsZero() {
		return runnerTime, nil
	}
	var skew time.Duration
	if sourceTime.Before(runnerTime) {
		skew = runnerTime.Sub(sourceTime)
	} else {
		skew = sourceTime.Sub(runnerTime)
	}
	if skew > 24*time.Hour {
		return time.Time{}, errors.New("n8n response clock differs from the runner clock by more than 24 hours")
	}
	return sourceTime, nil
}

// splitCancellation separates a cancellation sentinel from errors joined to
// it. The shared Hosted Runner adapter treats any error matching
// context.Canceled as clean shutdown, so the plugin must not let a cleanup or
// checkpoint failure ride out inside that same error tree.
func splitCancellation(err error) (canceled bool, companion error) {
	if err == nil {
		return false, nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var companions []error
		for _, member := range joined.Unwrap() {
			memberCanceled, memberCompanion := splitCancellation(member)
			canceled = canceled || memberCanceled
			if memberCompanion != nil {
				companions = append(companions, memberCompanion)
			}
		}
		return canceled, errors.Join(companions...)
	}
	if errors.Is(err, context.Canceled) {
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			cause := wrapped.Unwrap()
			wrappedCanceled, wrappedCompanion := splitCancellation(cause)
			if wrappedCanceled && wrappedCompanion != nil {
				prefix := strings.TrimSuffix(err.Error(), cause.Error())
				prefix = strings.TrimSuffix(prefix, ": ")
				if prefix == "" {
					return true, wrappedCompanion
				}
				return true, fmt.Errorf("%s: %w", prefix, wrappedCompanion)
			}
		}
		return true, nil
	}
	return false, err
}

// unaccompaniedUnavailable reports the dataset-unavailable error only when it
// is the sole cause of the failure. The client joins every result with its
// normalizer cleanup, and collect joins a failed poll with its in-flight
// checkpoint, so a single-member chain is still just the unavailable endpoint.
// A join carrying any second member means a storage or cleanup failure rode
// along with it; errors.As would still find the unavailable member and hide
// that failure behind a warning, so such an error is never isolated.
func unaccompaniedUnavailable(err error) (*datasetUnavailableError, bool) {
	for err != nil {
		switch e := err.(type) {
		case *datasetUnavailableError:
			return e, true
		case interface{ Unwrap() []error }:
			members := e.Unwrap()
			if len(members) != 1 {
				return nil, false
			}
			err = members[0]
		case interface{ Unwrap() error }:
			err = e.Unwrap()
		default:
			return nil, false
		}
	}
	return nil, false
}

func (p *Plugin) collect(ctx context.Context, rt hosted.Runtime, client *client, definition datasetDefinition) (err error) {
	stateKey := p.conf.stateKey(definition)
	pendingKey := stateKey + "/inflight"
	executionExpanded := expandedExecutions(client, definition)
	droppedExecutions := 0
	highwaterKey, activeKey := "", ""
	if executionExpanded {
		highwaterKey, activeKey = executionHighwaterKey(stateKey), executionActiveKey(stateKey)
	}
	previous, err := rt.Get(stateKey)
	if err != nil {
		if !errors.Is(err, storage.ErrStorageNotFound) {
			return err
		}
		previous = nil
	}
	// Completed state crosses a durable storage boundary. Enforce the same
	// raw-record budget used for in-flight state before scanning it, then validate
	// its fixed-width ordering. Neither oversized nor malformed state can safely
	// authenticate dependent progress.
	oversized := p.maxPending < 0 || int64(len(previous)) > int64(p.maxPending)*int64(recordStateEntrySize)
	var emitted recordStateSet
	valid := false
	if !oversized {
		emitted, valid = parseRecordState(previous)
	}
	if oversized || !valid {
		// Clear dependent progress first. If either transaction fails or the
		// process stops between them, the invalid completed value remains as a
		// durable retry marker. Clearing completed state first could authenticate
		// stale progress against either its bytes or a recreated empty digest.
		//
		// The expanded-execution high-water mark and active-ID set do not depend
		// on the manifest and are left untouched. Replacing a valid high-water
		// mark here would move the next window past executions that were never
		// collected. Keeping it means the next window starts one Request-Interval
		// before the mark. With the manifest and the in-flight cache cleared, that
		// poll writes again the executions that started inside the overlap and any
		// entries that failed polls wrote since the mark.
		if err := rt.Put(pendingKey, nil); err != nil {
			return fmt.Errorf("invalidate Dataset %s in-flight state: storage write failed", definition.Name)
		}
		if err := rt.Put(stateKey, nil); err != nil {
			return fmt.Errorf("invalidate Dataset %s completed state: storage write failed", definition.Name)
		}
		reason := "unreadable"
		if oversized {
			reason = "oversized"
		}
		rt.Warn("n8n discarding invalid snapshot state",
			log.KV("dataset", definition.Name), log.KV("reason", reason), log.KV("bytes", len(previous)))
		previous = nil
		emitted = nil
	}
	var priorActive, currentActive map[string]struct{}
	var executionWindowEnd time.Time
	if executionExpanded {
		executionWindowEnd = p.now().UTC()
		interval := time.Duration(p.conf.Request_Interval) * time.Second
		highwater, loadErr := rt.GetTime(highwaterKey)
		if loadErr != nil && !errors.Is(loadErr, storage.ErrStorageNotFound) {
			var parseErr *time.ParseError
			if errors.As(loadErr, &parseErr) {
				// No substitute start time is safe: any guess could begin after
				// executions that were never collected. Fail this dataset until
				// the state is repaired, and keep the stored bytes out of the
				// error because a parse error quotes its input.
				return fmt.Errorf("load Dataset %s high-water state: stored value is not a valid timestamp", definition.Name)
			}
			return fmt.Errorf("load Dataset %s high-water state: %w", definition.Name, loadErr)
		}
		rawActive, getErr := rt.Get(activeKey)
		if getErr != nil && !errors.Is(getErr, storage.ErrStorageNotFound) {
			return fmt.Errorf("load Dataset %s active-execution state: %w", definition.Name, getErr)
		}
		if priorActive, valid = decodeExecutionIDs(rawActive, p.maxTracked); !valid {
			// Only the ID set is discarded. The high-water mark is independent of
			// it, and the status probes find executions that are still running.
			if err := rt.Put(activeKey, nil); err != nil {
				return fmt.Errorf("invalidate Dataset %s active-execution state: storage write failed", definition.Name)
			}
			priorActive = make(map[string]struct{})
			rt.Warn("n8n discarding invalid active-execution state", log.KV("dataset", definition.Name))
		}
		client.responseTime = time.Time{}
		currentActive, err = client.activeExecutionIDs(ctx, definition, p.maxTracked)
		if err != nil {
			return fmt.Errorf("probe active executions: %w", err)
		}
		// HTTP Date is n8n's source clock. Use it in either direction so an
		// execution window cannot lie wholly before or after short executions
		// created by n8n. Reject an implausible source/runner difference rather
		// than moving the checkpoint by more than a day.
		executionWindowEnd, err = sourceExecutionWindowEnd(executionWindowEnd, client.responseTime)
		if err != nil {
			return err
		}
		established := loadErr == nil && !highwater.IsZero() && !highwater.After(executionWindowEnd)
		// Lookback sizes only the first window. With no stored high-water mark
		// the poll anchors at Lookback, or one Request-Interval when it is zero,
		// and persists that anchor before the main execution-list request so a
		// failed first poll resumes from it. Every later poll starts one
		// Request-Interval before the stored mark, so executions that become
		// visible late are still collected even when their startedAt precedes the
		// last successful poll. The current manifest suppresses overlap replays.
		if established {
			client.executionStartedAfter = highwater.Add(-interval)
		} else {
			anchor := executionWindowEnd.Add(-interval)
			if p.conf.Lookback > 0 {
				anchor = executionWindowEnd.Add(-p.conf.LookbackDuration())
			}
			if err := rt.PutTime(highwaterKey, anchor); err != nil {
				return fmt.Errorf("initialize Dataset %s high-water state: storage write failed", definition.Name)
			}
			if loadErr == nil {
				rt.Warn("n8n discarding high-water state that is zero or later than the source clock", log.KV("dataset", definition.Name))
			}
			client.executionStartedAfter = anchor.Add(-time.Second)
		}
		client.executionStartedBefore = executionWindowEnd
		combined := make(map[string]struct{}, len(priorActive)+len(currentActive))
		for id := range priorActive {
			combined[id] = struct{}{}
		}
		for id := range currentActive {
			combined[id] = struct{}{}
		}
		if len(combined) > p.maxTracked {
			return fmt.Errorf("Dataset %s exceeds the per-dataset limit of %d tracked active or pending execution IDs", definition.Name, p.maxTracked)
		}
		// Store newly probed IDs before the window is listed. A poll that fails
		// later then keeps them, and a later poll fetches each one by ID once it
		// completes, even when it started before that poll's window.
		if len(combined) > len(priorActive) {
			probed, encodeErr := encodeExecutionIDs(combined, p.maxTracked)
			if encodeErr != nil {
				return fmt.Errorf("Dataset %s exceeds the per-dataset limit of %d tracked active or pending execution IDs", definition.Name, p.maxTracked)
			}
			if err := rt.Put(activeKey, probed); err != nil {
				return fmt.Errorf("persist Dataset %s active-execution state: storage write failed", definition.Name)
			}
		}
	}
	identities := make(map[[sha256.Size]byte]normalizedSnapshotRecord)
	current := make(map[recordState]struct{})
	// This cache records successful enqueues from an incomplete poll. Binding it
	// to the completed manifest prevents stale progress surviving a later full
	// snapshot (and suppressing a deleted record's eventual reappearance).
	baseDigest := sha256.Sum256(previous)
	pending := make(map[recordState]struct{})
	pendingIdentities := make(map[[sha256.Size]byte]pendingIdentitySummary)
	var eviction []recordState
	pendingBytes, getErr := rt.Get(pendingKey)
	if getErr != nil && !errors.Is(getErr, storage.ErrStorageNotFound) {
		return getErr
	}
	if len(pendingBytes) > 0 {
		var saved recordStateSet
		valid := false
		if len(pendingBytes) >= sha256.Size && bytes.Equal(pendingBytes[:sha256.Size], baseDigest[:]) {
			saved, valid = parseRecordState(pendingBytes[sha256.Size:])
			valid = valid && saved.len() <= p.maxPending
		}
		if !valid {
			// A stale binding must be durably removed before another completed
			// manifest can make it match again (A/B/A). Malformed payloads are
			// equally untrusted, including ones with a matching binding.
			if err := rt.Put(pendingKey, nil); err != nil {
				// Storage errors may contain private backend details. Report the
				// operation without exposing the error or stored bytes.
				return fmt.Errorf("invalidate Dataset %s in-flight state: storage write failed", definition.Name)
			}
			pendingBytes = nil
			rt.Warn("n8n discarding stale or unreadable in-flight state", log.KV("dataset", definition.Name))
		} else {
			for i := 0; i < saved.len(); i++ {
				var pair recordState
				copy(pair[:], saved.at(i))
				pending[pair] = struct{}{}
				addPendingIdentity(pendingIdentities, pair)
				eviction = append(eviction, pair)
			}
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
	mainObserved := make(map[string]struct{})
	collectingMain := true
	activeRetention := currentActive
	processRecord := func(source collectedRecord, body *normalizedRecord) error {
		if executionExpanded {
			if !validExecutionID(source.identityText) {
				return errors.New("expanded execution returned an invalid id")
			}
			if nonterminalExecutionStatus(source.statusText) {
				if _, tracked := activeRetention[source.identityText]; !tracked && len(activeRetention) >= p.maxTracked {
					return fmt.Errorf("Dataset %s exceeds the per-dataset limit of %d tracked active or pending execution IDs", definition.Name, p.maxTracked)
				}
				activeRetention[source.identityText] = struct{}{}
				if collectingMain {
					// The latest main-window observation decides whether a
					// probed ID stays tracked after the window.
					delete(mainObserved, source.identityText)
				}
				return nil
			}
			if collectingMain {
				mainObserved[source.identityText] = struct{}{}
			}
		}
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
		revertedAfterPendingChange := emitted.contains(state) && pendingIdentityDiffers(pendingIdentities, state)
		if (!emitted.contains(state) || revertedAfterPendingChange) && !alreadyPending && !replayed {
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
						removePendingIdentity(pendingIdentities, old)
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
			if err := ctx.Err(); err != nil {
				return err
			}
			var payload []byte
			if body.oversized {
				payload, err = oversizedExecutionPayload(source, body.limit)
				if err != nil {
					return errors.New("encode oversized n8n execution marker failed")
				}
				rt.Warn("n8n execution exceeded the per-record safety limit; wrote a marker entry",
					log.KV("dataset", definition.Name), log.KV("execution_id", source.identityText), log.KV("record_limit", body.limit))
			} else {
				payload, err = body.payload()
				if err != nil {
					return err
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			// The ingest muxer stamps SRC because this entry leaves it unset.
			if err := rt.Write(entry.Entry{Tag: tag, TS: entry.FromStandard(source.timestamp), Data: payload}); err != nil {
				return err
			}
			written++
			pending[state] = struct{}{}
			addPendingIdentity(pendingIdentities, state)
			pendingChanged = true
		}
		return nil
	}
	result, err := client.collectStreaming(ctx, definition, processRecord)
	collectingMain = false
	if err != nil {
		return err
	}
	nextActive := currentActive
	if executionExpanded {
		nextActive = make(map[string]struct{}, len(currentActive)+len(priorActive))
		for id := range currentActive {
			// The window listing returned this probed ID with a terminal status
			// after the probe, and that record was processed above. Keeping it
			// would fetch the execution by ID again on a later poll.
			if _, observed := mainObserved[id]; observed {
				continue
			}
			nextActive[id] = struct{}{}
		}
		activeRetention = nextActive
		completedIDs := make([]string, 0, len(priorActive))
		for id := range priorActive {
			if _, stillActive := currentActive[id]; !stillActive {
				completedIDs = append(completedIDs, id)
			}
		}
		sort.Strings(completedIDs)
		detailBudget := min(client.pageSize, client.maxPages)
		detailRequests := 0
		for _, id := range completedIDs {
			if _, observed := mainObserved[id]; observed {
				continue
			}
			if detailRequests >= detailBudget {
				nextActive[id] = struct{}{}
				continue
			}
			if result.Count >= client.maxRecords {
				return fmt.Errorf("raw record limit of %d exceeded", client.maxRecords)
			}
			detailRequests++
			count, found, detailErr := client.collectExecution(ctx, id, client.maxPages+detailRequests, processRecord)
			if detailErr != nil {
				return fmt.Errorf("fetch completed execution detail: %w", detailErr)
			}
			if !found {
				droppedExecutions++
				rt.Warn("n8n tracked execution no longer exists; its data was not collected",
					log.KV("dataset", definition.Name), log.KV("execution_id", id))
			}
			result.Count += count
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate all derived state before committing the completed manifest. The
	// active-ID encoding can exceed its bound after main-window records and
	// deferred prior IDs are combined; discovering that after the manifest Put
	// would leave a failed poll with only part of its checkpoint advanced.
	var activeState []byte
	if executionExpanded {
		var stateErr error
		activeState, stateErr = encodeExecutionIDs(nextActive, p.maxTracked)
		if stateErr != nil {
			return fmt.Errorf("Dataset %s exceeds the per-dataset limit of %d tracked active or pending execution IDs", definition.Name, p.maxTracked)
		}
	}

	// Emission is per record, but the checkpoint is still the whole snapshot, so
	// a deletion drops out of state without re-emitting everything that survived.
	encoded := encodeRecordState(current)
	// Commit the completed manifest before clearing its replay cache. Runtime
	// Put calls may be separate durable transactions, so the opposite order
	// creates a restart window with neither the new manifest nor replay progress.
	if err := ctx.Err(); err != nil {
		return err
	}
	if !bytes.Equal(previous, encoded) {
		if err := rt.Put(stateKey, encoded); err != nil {
			return err
		}
	}
	if executionExpanded {
		if err := rt.Put(activeKey, activeState); err != nil {
			return fmt.Errorf("persist Dataset %s active-execution state: storage write failed", definition.Name)
		}
		if err := rt.PutTime(executionHighwaterKey(stateKey), executionWindowEnd); err != nil {
			return fmt.Errorf("persist Dataset %s high-water state: storage write failed", definition.Name)
		}
	}
	// The standard Job adapter calls the BucketWriter sync function after this
	// successful Handle returns. Keep the plugin on the Runtime contract: Write
	// entries, update state, and let the adapter own persistence and liveness.
	if err := ctx.Err(); err != nil {
		return err
	}
	completed = true
	if len(pendingBytes) > 0 {
		if err := rt.Put(pendingKey, nil); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if executionExpanded {
		rt.Info("n8n Dataset collection completed",
			log.KV("dataset", definition.Name), log.KV("records", result.Count),
			log.KV("written", written), log.KV("dropped", droppedExecutions),
			log.KV("tag", p.conf.tag(definition)))
	} else {
		rt.Info("n8n Dataset collection completed",
			log.KV("dataset", definition.Name), log.KV("records", result.Count),
			log.KV("written", written), log.KV("tag", p.conf.tag(definition)))
	}
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
