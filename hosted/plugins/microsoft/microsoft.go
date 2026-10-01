// Package microsoft implements a Hosted Runner plugin for stable, read-only
// Microsoft cloud APIs.
package microsoft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	Name    = "microsoft"
	ID      = "microsoft-api.ingesters.gravwell.io"
	Version = "1.0.0"
)

// sourcePollError marks a failure returned while reading a Microsoft source.
// The client already applies the configured bounded request retries before one
// of these reaches Handle. Returning the configured poll continuation after
// logging this error prevents the Hosted Runner's generic 30-second job-error
// retry from overriding Request-Interval for blocked or unavailable APIs.
// Local state, tag, processor, and ingest-write failures are deliberately not
// wrapped so the framework can retain its faster operational recovery path.
type sourcePollError struct {
	err error
}

func (e *sourcePollError) Error() string { return e.err.Error() }
func (e *sourcePollError) Unwrap() error { return e.err }

type recordState struct {
	Hash   string    `json:"hash"`
	SeenAt time.Time `json:"seen_at"`
}

type streamState struct {
	Watermark time.Time              `json:"watermark"`
	Records   map[string]recordState `json:"records"`
	Pending   *deliveryProgress      `json:"pending,omitempty"`
	Fabric    *fabricScan            `json:"fabric_scan,omitempty"`
}

// Receipts belong to an incomplete poll, not a completed source watermark.
// Each receipt is saved only after native synchronization. Retrying the poll
// skips those exact record versions even when Ingest-Unchanged is enabled.
type deliveryProgress struct {
	Start        time.Time       `json:"start"`
	End          time.Time       `json:"end"`
	Receipts     map[string]bool `json:"receipts"`
	Occurrences  map[string]int  `json:"occurrences,omitempty"`
	FabricAnchor time.Time       `json:"fabric_anchor,omitempty"`
}

const deliveryBatchSize = 64

type Microsoft struct {
	conf *Config
	proc *processors.ProcessorSet

	client       *Client
	processMu    sync.Mutex
	tagMu        sync.Mutex
	tags         map[string]entry.EntryTag
	source       net.IP
	now          func() time.Time
	syncDelivery func(context.Context, time.Duration) error
	syncState    func() error
}

func New(conf *Config, proc *processors.ProcessorSet) *Microsoft {
	return &Microsoft{
		conf: conf, proc: proc, tags: make(map[string]entry.EntryTag),
		source: net.ParseIP("127.0.0.1"), now: time.Now,
	}
}

func (m *Microsoft) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	ctx = withRetryWaitDeadline(ctx)
	if m.client == nil {
		client, err := NewClient(m.conf, nil)
		if err != nil {
			return nil, err
		}
		m.client = client
	}
	if err := m.client.bindRetryState(rt, m.syncState); err != nil {
		return nil, err
	}
	resolved, err := ResolveDatasets(m.conf.Api)
	if err != nil {
		return nil, err
	}
	var operationalErrors []error
	for _, dataset := range resolved {
		if dataset.Kind == KindResourceGraph || !strings.Contains(dataset.Path, "{subscriptionId}") {
			if err := m.collect(ctx, rt, dataset, ""); err != nil {
				if targetErr := m.handleTargetError(rt, dataset, false, err); targetErr != nil {
					if errors.Is(targetErr, context.Canceled) {
						return nil, targetErr
					}
					operationalErrors = append(operationalErrors, targetErr)
				}
			}
			continue
		}
		for _, subscriptionID := range m.conf.Subscription_ID {
			if err := m.collect(ctx, rt, dataset, subscriptionID); err != nil {
				if targetErr := m.handleTargetError(rt, dataset, true, err); targetErr != nil {
					if errors.Is(targetErr, context.Canceled) {
						return nil, targetErr
					}
					operationalErrors = append(operationalErrors, targetErr)
				}
			}
		}
	}
	return m.conf.ContinueAfterInterval(), errors.Join(operationalErrors...)
}

// handleTargetError records the selector-specific failure without exposing a
// tenant or subscription identifier. Source-poll failures are considered a
// completed poll attempt and therefore resume on Request-Interval. Context
// cancellation and local operational failures continue through the Hosted
// Runner error path.
func (m *Microsoft) handleTargetError(rt hosted.Runtime, dataset Dataset, subscriptionScoped bool, err error) error {
	rt.Error("Microsoft API selector poll failed",
		log.KV("selector", dataset.Name),
		log.KV("subscription_scoped", subscriptionScoped),
		log.KV("error", m.safePollError(err)))
	if errors.Is(err, context.Canceled) {
		return err
	}
	var sourceErr *sourcePollError
	if errors.As(err, &sourceErr) {
		return nil
	}
	return err
}

func (m *Microsoft) safePollError(err error) string {
	message := err.Error()
	identifiers := append([]string{m.conf.Tenant_ID, m.conf.Client_ID}, m.conf.Subscription_ID...)
	for _, identifier := range identifiers {
		identifier = strings.TrimSpace(identifier)
		if identifier == "" {
			continue
		}
		message = strings.ReplaceAll(message, identifier, "[redacted-id]")
		message = strings.ReplaceAll(message, strings.ToUpper(identifier), "[redacted-id]")
	}
	return message
}

func (m *Microsoft) collect(ctx context.Context, rt hosted.Runtime, dataset Dataset, subscriptionID string) error {
	key := stateKey(m.conf.StateNamespace(), dataset.Name, subscriptionID)
	state, err := loadState(rt, key)
	if err != nil {
		return fmt.Errorf("load %s state: %w", dataset.Name, err)
	}
	now := m.now().UTC()
	start := now.Add(-time.Duration(m.conf.Lookback) * time.Hour)
	if !state.Watermark.IsZero() {
		start = state.Watermark.Add(-time.Duration(m.conf.Overlap) * time.Second)
	}
	end := now
	if state.Pending != nil {
		start, end = state.Pending.Start, state.Pending.End
	}
	var scan *fabricScan
	if dataset.Kind == KindFabricActivity {
		scan, start, end = fabricWindow(state, start, end, now)
		if start.After(scan.Anchor) {
			rt.Warn("Fabric activity history is outside the available retention window",
				log.KV("anchor", scan.Anchor.Format(time.RFC3339Nano)),
				log.KV("available_since", start.Format(time.RFC3339Nano)))
		}
	}
	records, err := m.client.Fetch(ctx, dataset, start, end, subscriptionID)
	if err != nil {
		var stateErr *retryStateError
		if errors.As(err, &stateErr) {
			return err
		}
		return &sourcePollError{err: fmt.Errorf("collect %s: %w", dataset.Name, err)}
	}
	tag, err := m.negotiateTag(rt, dataset)
	if err != nil {
		return err
	}
	if state.Records == nil {
		state.Records = make(map[string]recordState)
	}
	nextRecords := make(map[string]recordState, len(state.Records)+len(records))
	if dataset.Mode != ModeSnapshot {
		for id, value := range state.Records {
			nextRecords[id] = value
		}
	}
	if state.Pending == nil {
		state.Pending = &deliveryProgress{Start: start, End: end, Receipts: make(map[string]bool)}
	}
	if scan != nil {
		// Rebase only unavailable portions of a pending Fabric window. Retain
		// the original anchor and acknowledged versions across an expired retry.
		state.Pending.Start, state.Pending.End = start, end
		state.Pending.FabricAnchor = scan.Anchor
	}
	if state.Pending.Receipts == nil {
		state.Pending.Receipts = make(map[string]bool)
	}
	if state.Pending.Occurrences == nil {
		state.Pending.Occurrences = make(map[string]int)
	}
	// Fabric's retained window is discovery coverage, not a request to replay
	// every unchanged historical event on each poll.
	ingestUnchanged := m.conf.Ingest_Unchanged && dataset.Kind != KindFabricActivity
	// Keep a pending snapshot receipt set bounded by the currently fetched
	// versions. Versions no longer present cannot suppress a future mutation.
	fingerprints := make([]string, len(records))
	activeReceipts := make(map[string]bool, len(records))
	activeIdentities := make(map[string]bool)
	for i, record := range records {
		fingerprint, err := recordDigest(record.Raw, dataset)
		if err != nil {
			return fmt.Errorf("digest %s entry: %w", dataset.Name, err)
		}
		fingerprints[i] = fingerprint
		activeReceipts[record.ID+":"+fingerprint] = true
		if scan != nil {
			activeIdentities[record.ID] = true
			nextRecords[record.ID] = recordState{Hash: fingerprint, SeenAt: now}
		}
	}
	if scan != nil {
		// Preflight before any delivery or durable state mutation. Evicting a
		// current-window identity would guarantee replay on the next rescan.
		if err := pruneFabricRecords(nextRecords, activeIdentities, now); err != nil {
			return &sourcePollError{err: err}
		}
	}
	for receipt := range state.Pending.Receipts {
		if !activeReceipts[receipt] {
			delete(state.Pending.Receipts, receipt)
			delete(state.Pending.Occurrences, receipt)
		}
	}
	written := 0
	unsynchronized := 0
	flush := func() error {
		if unsynchronized == 0 {
			return nil
		}
		if m.syncDelivery != nil {
			if err := m.syncDelivery(ctx, 30*time.Second); err != nil {
				return fmt.Errorf("synchronize Microsoft ingestion before partial receipt: %w", err)
			}
		}
		if err := saveState(rt, key, state); err != nil {
			return fmt.Errorf("persist %s partial receipts: %w", dataset.Name, err)
		}
		if m.syncState != nil {
			if err := m.syncState(); err != nil {
				return fmt.Errorf("synchronize %s partial receipt storage: %w", dataset.Name, err)
			}
		}
		unsynchronized = 0
		return nil
	}
	fail := func(err error) error {
		return errors.Join(err, flush())
	}
	occurrences := make(map[string]int)
	for i, record := range records {
		fingerprint := fingerprints[i]
		previous, exists := state.Records[record.ID]
		if dataset.Mode == ModeSnapshot {
			if observed, ok := nextRecords[record.ID]; ok {
				previous, exists = observed, true
			}
		}
		nextRecords[record.ID] = recordState{Hash: fingerprint, SeenAt: now}
		receipt := record.ID + ":" + fingerprint
		occurrences[receipt]++
		acknowledged := state.Pending.Occurrences[receipt]
		if state.Pending.Receipts[receipt] && acknowledged == 0 {
			// A predecessor boolean receipt acknowledges the first occurrence.
			acknowledged = 1
		}
		if (ingestUnchanged && occurrences[receipt] <= acknowledged) ||
			(!ingestUnchanged && (state.Pending.Receipts[receipt] || (exists && previous.Hash == fingerprint))) {
			continue
		}
		timestamp := record.Timestamp
		if timestamp.IsZero() {
			timestamp = now
		}
		payload, metadata, err := formatRecord(record.Raw, record.ID, timestamp, dataset, m.conf.Normalization)
		if err != nil {
			return fail(fmt.Errorf("format %s entry: %w", dataset.Name, err))
		}
		ent := entry.Entry{TS: entry.FromStandard(timestamp), SRC: m.source, Tag: tag, Data: payload}
		if err := attachIntrinsic(&ent, metadata); err != nil {
			return fail(fmt.Errorf("attach %s metadata: %w", dataset.Name, err))
		}
		m.processMu.Lock()
		err = m.proc.ProcessContext(&ent, ctx)
		m.processMu.Unlock()
		if err != nil {
			return fail(fmt.Errorf("write %s entry: %w", dataset.Name, err))
		}
		state.Pending.Receipts[receipt] = true
		state.Pending.Occurrences[receipt] = occurrences[receipt]
		written++
		unsynchronized++
		if unsynchronized == deliveryBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := flush(); err != nil {
		return err
	}
	state.Records = nextRecords
	state.Pending = nil
	watermarkStart := start
	if scan != nil {
		watermarkStart = scan.Anchor
		state.Fabric = scan
	}
	state.Watermark = nextWatermark(dataset, state.Watermark, watermarkStart, end, records)
	if scan == nil {
		pruneState(&state, dataset.Mode, now)
	}
	if err := saveState(rt, key, state); err != nil {
		return fmt.Errorf("persist %s state: %w", dataset.Name, err)
	}
	rt.Info("completed Microsoft API poll",
		log.KV("dataset", dataset.Name), log.KV("fetched", len(records)), log.KV("written", written))
	return nil
}

// Temporal windows follow source timestamps, never the time of an empty poll.
// Persist the first lower bound even when the source has not made any records
// visible yet. Existing progress and equal/subsecond boundaries are monotonic.
func nextWatermark(dataset Dataset, previous, start, end time.Time, records []FetchedRecord) time.Time {
	if !temporalDataset(dataset) {
		if end.After(previous) {
			return end
		}
		return previous
	}
	next := previous
	if next.IsZero() {
		next = start
	}
	for _, record := range records {
		candidate := record.Timestamp
		if candidate.After(end) {
			candidate = end
		}
		if candidate.After(next) {
			next = candidate
		}
	}
	return next
}

func temporalDataset(dataset Dataset) bool {
	if dataset.Mode == ModeSnapshot {
		return false
	}
	return dataset.Kind == KindAzureActivity || dataset.Kind == KindAdvancedHunting || dataset.Kind == KindFabricActivity ||
		(dataset.Service == ServiceGraph && dataset.TimeField != "")
}

func (m *Microsoft) negotiateTag(rt hosted.Runtime, dataset Dataset) (entry.EntryTag, error) {
	name := m.conf.TagForDataset(dataset)
	m.tagMu.Lock()
	defer m.tagMu.Unlock()
	if tag, ok := m.tags[name]; ok {
		return tag, nil
	}
	tag, err := rt.NegotiateTag(name)
	if err == nil {
		m.tags[name] = tag
	}
	return tag, err
}

func stateKey(namespace, dataset, subscriptionID string) string {
	if subscriptionID == "" {
		return namespace + "/" + dataset
	}
	return namespace + "/" + dataset + "/" + subscriptionID
}

func loadState(rt hosted.Storage, key string) (streamState, error) {
	encoded, err := rt.Get(key)
	if err != nil {
		if errors.Is(err, storage.ErrStorageNotFound) {
			return streamState{Records: make(map[string]recordState)}, nil
		}
		return streamState{}, err
	}
	var state streamState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return streamState{}, err
	}
	if state.Records == nil {
		state.Records = make(map[string]recordState)
	}
	return state, nil
}

func saveState(rt hosted.Storage, key string, state streamState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return rt.Put(key, encoded)
}

const maximumStateRecords = 500000

// Retain every identity in the completed Fabric scan. Old identities absent
// from this scan can expire or make room, but capacity cannot discard current
// coverage. The caller must handle failure before writing any fetched record.
func pruneFabricRecords(records map[string]recordState, active map[string]bool, now time.Time) error {
	if len(active) > maximumStateRecords {
		return fmt.Errorf("Fabric scan exceeded the %d identity state capacity; no entries written and checkpoint retained", maximumStateRecords)
	}
	cutoff := now.Add(-35 * 24 * time.Hour)
	var inactive []string
	for id, value := range records {
		if active[id] {
			continue
		}
		if value.SeenAt.Before(cutoff) {
			delete(records, id)
		} else {
			inactive = append(inactive, id)
		}
	}
	if len(records) > maximumStateRecords {
		sort.Slice(inactive, func(i, j int) bool {
			a, b := records[inactive[i]].SeenAt, records[inactive[j]].SeenAt
			if a.Equal(b) {
				return inactive[i] < inactive[j]
			}
			return a.Before(b)
		})
		remove := len(records) - maximumStateRecords
		for _, id := range inactive[:remove] {
			delete(records, id)
		}
	}
	return nil
}

func pruneState(state *streamState, mode Mode, now time.Time) {
	if mode == ModeSnapshot {
		return
	}
	cutoff := now.Add(-35 * 24 * time.Hour)
	for id, value := range state.Records {
		if value.SeenAt.Before(cutoff) {
			delete(state.Records, id)
		}
	}
	const maximum = maximumStateRecords
	if len(state.Records) <= maximum {
		return
	}
	type item struct {
		id string
		at time.Time
	}
	items := make([]item, 0, len(state.Records))
	for id, value := range state.Records {
		items = append(items, item{id: id, at: value.SeenAt})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.Before(items[j].at) })
	for i := 0; i < len(items)-maximum; i++ {
		delete(state.Records, items[i].id)
	}
}

var _ hosted.Job = (*Microsoft)(nil)
