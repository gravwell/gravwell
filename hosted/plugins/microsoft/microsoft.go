/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package microsoft implements a Hosted Runner plugin for stable, read-only
// Microsoft cloud APIs.
package microsoft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	Name    = "microsoft"
	ID      = "microsoft-api.ingesters.gravwell.io"
	Version = "1.0.0"
)

// streamState is reconstructed from discrete Hosted Runner state keys.
type streamState struct {
	Watermark    time.Time          `json:"watermark,omitempty"`
	WindowStart  time.Time          `json:"window_start,omitempty"`
	WindowEnd    time.Time          `json:"window_end,omitempty"`
	Continuation *FetchContinuation `json:"continuation,omitempty"`
	Fetched      int                `json:"fetched,omitempty"`
	Fabric       *fabricScan        `json:"fabric_scan,omitempty"`
	Overlap      *temporalOverlap   `json:"overlap,omitempty"`
}

const temporalOverlapVersion = 1

// temporalOverlap is one bounded, atomic receipt document for the sliding
// replay horizon. It is not an all-history record index: completion prunes
// every receipt older than the next poll's overlap start.
type temporalOverlap struct {
	Version  int               `json:"version"`
	Receipts []temporalReceipt `json:"receipts,omitempty"`
}

type temporalReceipt struct {
	Timestamp   time.Time `json:"timestamp"`
	Fingerprint string    `json:"fingerprint"`
}

type preparedTemporalRecord struct {
	Timestamp time.Time
	Payload   []byte
}

// cursorState is the cursor document saveState writes. storedCursor is the
// read side of exactly this document, with every untrusted array inside it
// streamed under its own bounds, so the two have to name the same members.
type cursorState struct {
	WindowStart  time.Time          `json:"window_start,omitempty"`
	WindowEnd    time.Time          `json:"window_end,omitempty"`
	Continuation *FetchContinuation `json:"continuation,omitempty"`
	Fetched      int                `json:"fetched,omitempty"`
}

type Microsoft struct {
	conf *Config

	client *Client
	tagMu  sync.Mutex
	tags   map[string]entry.EntryTag

	drainMu    sync.Mutex
	drainOnly  bool
	drainSince time.Time

	now func() time.Time

	overlapReceiptLimit       int
	managementDescriptorLimit int
	// managementDescriptorByteLimit overrides the cumulative descriptor-byte
	// budget one window's listing may retain.
	managementDescriptorByteLimit int
	managementReceiptLimit        int
	// managementReceiptByteLimit overrides the aggregate receipt-byte budget the
	// checkpoint path enforces before it persists the receipt set.
	managementReceiptByteLimit int
	// managementFetchSpanLimit overrides the cumulative per-pass vendor HTTP
	// span, which otherwise matches the client's single-request fetch deadline.
	managementFetchSpanLimit time.Duration
	stateSync                func() error
}

type sourceTarget struct {
	dataset            Dataset
	subscriptionID     string
	subscriptionScoped bool
	key                string
}

func New(conf *Config) *Microsoft {
	return &Microsoft{
		conf: conf, tags: make(map[string]entry.EntryTag), now: time.Now,
		overlapReceiptLimit:           maximumOverlapReceipts,
		managementDescriptorLimit:     managementMaximumDescriptors,
		managementDescriptorByteLimit: managementMaximumDescriptorBytes,
		managementReceiptLimit:        managementMaximumReceipts,
		managementReceiptByteLimit:    managementMaximumReceiptBytes,
	}
}

func NewWithStateSync(conf *Config, syncFn func() error) *Microsoft {
	m := New(conf)
	m.stateSync = syncFn
	return m
}

func (m *Microsoft) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if m.client == nil {
		client, err := NewClient(m.conf, nil)
		if err != nil {
			return nil, err
		}
		m.client = client
	}
	resolved, err := resolveDatasetsForSchema(m.conf.Api, m.conf.Tag_Schema)
	if err != nil {
		return nil, err
	}
	targets := make([]sourceTarget, 0, len(resolved))
	for _, dataset := range resolved {
		if dataset.Kind == KindManagementActivity {
			targets = append(targets, sourceTarget{dataset: dataset, key: managementStateKey(m.conf.StateNamespace(), dataset.Name)})
			continue
		}
		if dataset.Kind == KindResourceGraph || !strings.Contains(dataset.Path, "{subscriptionId}") {
			targets = append(targets, sourceTarget{dataset: dataset, key: stateKey(m.conf.StateNamespace(), dataset.Name, "")})
			continue
		}
		for _, subscriptionID := range m.conf.Subscription_ID {
			targets = append(targets, sourceTarget{
				dataset: dataset, subscriptionID: subscriptionID, subscriptionScoped: true,
				key: stateKey(m.conf.StateNamespace(), dataset.Name, subscriptionID),
			})
		}
	}

	drainPending := m.takeImmediateDrain()
	var operationalErrors []error
	continueNow := false
	managementRetryDelay := time.Duration(0)
	for _, target := range targets {
		if drainPending {
			pending, err := m.targetHasPending(rt, target)
			if err != nil {
				return nil, fmt.Errorf("inspect %s state: %w", target.dataset.Name, err)
			}
			if !pending {
				continue
			}
		}
		var pending bool
		if target.dataset.Kind == KindManagementActivity {
			pending, err = m.collectManagement(ctx, rt, target.dataset)
		} else {
			pending, err = m.collect(ctx, rt, target.dataset, target.subscriptionID)
		}
		continueNow = continueNow || pending
		if err != nil {
			var deferred *managementDeferredError
			if errors.As(err, &deferred) {
				if deferred.Delay > managementRetryDelay {
					managementRetryDelay = deferred.Delay
				}
				continue
			}
			if targetErr := m.handleTargetError(rt, target.dataset, target.subscriptionScoped, err); targetErr != nil {
				if errors.Is(targetErr, context.Canceled) {
					return nil, targetErr
				}
				operationalErrors = append(operationalErrors, targetErr)
			}
			continue
		}
	}
	if len(operationalErrors) == 0 && continueNow {
		m.scheduleImmediateDrain()
		return hosted.ContinueNow(), nil
	}
	if len(operationalErrors) == 0 && managementRetryDelay > 0 {
		return hosted.ContinueAfter(managementRetryDelay), nil
	}
	return m.conf.ContinueAfterInterval(), errors.Join(operationalErrors...)
}

func (m *Microsoft) targetHasPending(rt hosted.Storage, target sourceTarget) (bool, error) {
	if target.dataset.Kind == KindManagementActivity {
		state, err := loadManagementState(rt, target.key)
		if err != nil {
			// Only collectManagement has a Runtime and the synchronization hook
			// required to replace malformed split state safely.
			return true, nil
		}
		return state.hasPending(), err
	}
	pending, err := stateHasPending(rt, target.key)
	if errors.Is(err, errIncompatibleContinuation) {
		// Only collect can rewrite a stored document this build cannot decode,
		// so a drain pass must visit the selector instead of skipping it.
		return true, nil
	}
	return pending, err
}

// takeImmediateDrain reports whether this pass may visit only the targets that
// have pending work. Immediate-drain mode is re-armed by every pass that still
// has one, so the sequence has no natural end: a Management Activity catch-up
// reports pending work for the whole duration of its backlog, which starved
// every sibling selector in the stanza and let sibling Management checkpoints
// age past the source's own seven-day retention. The sequence is therefore
// bounded in wall-clock time instead of in passes, so a complete pass still
// happens at least once per configured poll interval. Catch-up is not paced by
// this: the drain passes themselves still return immediately.
func (m *Microsoft) takeImmediateDrain() bool {
	m.drainMu.Lock()
	defer m.drainMu.Unlock()
	if m.drainOnly && m.now().UTC().Sub(m.drainSince) < m.drainFairnessWindow() {
		m.drainOnly = false
		return true
	}
	m.drainOnly, m.drainSince = false, time.Time{}
	return false
}

// scheduleImmediateDrain arms the next pass as a drain pass. The start of the
// sequence is stamped once so re-arming a drain that is already running cannot
// push the fairness deadline out forever.
func (m *Microsoft) scheduleImmediateDrain() {
	m.drainMu.Lock()
	if m.drainSince.IsZero() {
		m.drainSince = m.now().UTC()
	}
	m.drainOnly = true
	m.drainMu.Unlock()
}

func (m *Microsoft) drainFairnessWindow() time.Duration {
	if m.conf == nil || m.conf.Request_Interval <= 0 {
		return defaultIntervalSeconds * time.Second
	}
	return time.Duration(m.conf.Request_Interval) * time.Second
}

func (m *Microsoft) handleTargetError(rt hosted.Runtime, dataset Dataset, subscriptionScoped bool, err error) error {
	rt.Error("Microsoft API selector poll failed",
		log.KV("selector", dataset.Name),
		log.KV("subscription_scoped", subscriptionScoped),
		log.KV("error", m.safePollError(err)))
	if errors.Is(err, context.Canceled) {
		return err
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

// The presentation contract below is the one every operator-visible insertion
// of a source-supplied Microsoft value goes through. It started as the
// Management Activity contentId escape -- displayManagementContentID is a thin
// wrapper over it now -- and is shared, because the contentId was never the
// only unconstrained vendor string a log line carries: a Defender parent
// identifier and a Management subscription status reach the same sinks from the
// same kind of value.
//
// The default RFC5424 file sink escapes '\', '"' and ']' in a parameter value
// and writes every other byte through unchanged, so an embedded CR or LF ends
// the physical record there and the remainder of the value is read as a further
// complete record this plugin never wrote (CWE-117). Every control character is
// converted to a visible escape instead, so one inserted value is always
// exactly one physical record, and the result is bounded on its own rather than
// through whatever a sink happens to do with an arbitrarily long parameter.
//
// Unicode Cc is not the whole of what a log line cannot carry. The escape
// covers three more categories, because each one reaches the same sinks through
// the same unconstrained vendor string:
//
//   - Cf, the format characters. U+202E RIGHT-TO-LEFT OVERRIDE and the
//     U+2066-U+2069 isolates reorder everything after them, so an identifier
//     can display as a different identifier, or display the fields that follow
//     it in the record in an order the record does not hold. U+200B, U+200C,
//     U+200D and U+FEFF have no glyph at all, so two identifiers an operator
//     reads as the same one are different bytes, and a value that looks empty
//     is not. None of them are stripped: the value is still reported, with the
//     characters it holds named.
//   - Zl and Zp, U+2028 LINE SEPARATOR and U+2029 PARAGRAPH SEPARATOR. The
//     RFC5424 sink writes both through, and a reader that splits records on
//     Unicode line boundaries rather than on LF -- which JavaScript, many log
//     viewers and several JSON-lines parsers do -- reads one record as two.
//
// All of it is presentation only. The canonical vendor bytes are what URL
// construction, parent/child dispatch, cursor identity, deduplication, state
// and persistence carry, and none of those go through here.
const (
	// maximumDisplayedVendorValue bounds the escaped presentation of one
	// source-supplied value. The accept-time bounds elsewhere in the plugin are
	// what keep a vendor value short to begin with; this is what keeps one log
	// parameter short even when the value reaching it came back from a
	// persisted document that was only ever bounded in aggregate.
	maximumDisplayedVendorValue = 512

	// maximumDisplayedVendorList bounds the escaped presentation of a list of
	// source-supplied values rendered into a single parameter. The lists this
	// plugin reports are already bounded by count -- maximumDiagnostics names
	// at most sixteen truncated parents -- but sixteen values at the per-value
	// bound, each of whose bytes can escape to four characters, is a 32 KB log
	// parameter built entirely out of vendor bytes. One report is worth a few
	// lines of an operator's screen rather than a screenful, so the names that
	// fit are shown and the bound is marked.
	//
	// 4096 is twice the longest escape one value can produce -- four characters
	// per byte of maximumDisplayedVendorValue plus the marker -- so the first
	// element of a list always fits and the presentation is never reduced to
	// the marker alone.
	//
	// Four characters per byte still bounds it with the Cf/Zl/Zp escape in
	// place. The widest of those is \u{10ffff} at ten characters for a
	// four-byte codepoint, and the narrowest input that reaches the codepoint
	// escape is the two-byte U+00AD at eight characters: 2.5 and 4.0
	// characters per byte respectively, against the 4.0 a one-byte Cc control
	// already cost as \xNN.
	maximumDisplayedVendorList = 4096

	// displayHexDigits renders the escape of a control character.
	displayHexDigits = "0123456789abcdef"

	// displayCodepointMinimumDigits pads a codepoint escape to the four hex
	// digits a BMP character needs, so \u{200e} is never written as \u{20e}
	// and two different characters can never share one presentation.
	displayCodepointMinimumDigits = 4

	// displayTruncationMarker states that a presentation reached its bound, so
	// a value an operator reads is never silently shortened.
	displayTruncationMarker = "..."
)

// displayVendorValue renders a source-supplied value for an operator-visible
// log parameter or status message, escaping every control character visibly and
// bounding the result at bound bytes of input.
//
// The escape is injective: a value holding the two characters "\r" cannot be
// confused with one holding a carriage return, because the backslash that would
// otherwise imitate the escape is itself escaped.
func displayVendorValue(value string, bound int) string {
	if bound <= 0 {
		bound = maximumDisplayedVendorValue
	}
	var escaped strings.Builder
	escaped.Grow(len(value))
	truncated := false
	for offset, r := range value {
		if offset >= bound {
			truncated = true
			break
		}
		switch {
		case r == '\\':
			escaped.WriteString(`\\`)
		case r == '\r':
			escaped.WriteString(`\r`)
		case r == '\n':
			escaped.WriteString(`\n`)
		case r == '\t':
			escaped.WriteString(`\t`)
		case unicode.IsControl(r):
			// Unicode Cc is U+0000-U+001F and U+007F-U+009F, so one byte of hex
			// always names the character exactly.
			escaped.WriteString(`\x`)
			escaped.WriteByte(displayHexDigits[byte(r)>>4])
			escaped.WriteByte(displayHexDigits[byte(r)&0xf])
		case unicode.Is(unicode.Cf, r):
			// Format characters: bidirectional overrides and isolates, the
			// zero-width joiners and non-joiners, the byte-order mark, the soft
			// hyphen. unicode.IsControl answers false for every one of them, so
			// they reached the sink unchanged and an operator was shown a value
			// whose displayed order or displayed length was not the value's.
			writeDisplayedCodepoint(&escaped, r)
		case unicode.Is(unicode.Zl, r):
			// U+2028. A Unicode-aware record splitter ends the line here.
			writeDisplayedCodepoint(&escaped, r)
		case unicode.Is(unicode.Zp, r):
			// U+2029, the same, one paragraph rather than one line.
			writeDisplayedCodepoint(&escaped, r)
		default:
			escaped.WriteRune(r)
		}
	}
	if truncated {
		escaped.WriteString(displayTruncationMarker)
	}
	return escaped.String()
}

// writeDisplayedCodepoint names one codepoint as \u{<hex>}. The braces are what
// make it unambiguous without a fixed width: a value whose own next character is
// a hex digit cannot extend the escape, and the escape cannot be confused with
// the value's own text, because a literal backslash is doubled before this is
// ever reached. The result is bounded at ten characters -- \u{10ffff} -- for any
// codepoint Unicode defines.
func writeDisplayedCodepoint(escaped *strings.Builder, r rune) {
	value := uint32(r)
	digits := displayCodepointMinimumDigits
	for value>>(uint(digits)*4) != 0 {
		digits++
	}
	escaped.WriteString(`\u{`)
	for shift := (digits - 1) * 4; shift >= 0; shift -= 4 {
		escaped.WriteByte(displayHexDigits[(value>>uint(shift))&0xf])
	}
	escaped.WriteString(`}`)
}

// displayVendorList renders a comma-separated list of source-supplied values
// into one operator-visible parameter. Each element carries the per-value
// contract above and the joined result is bounded as a whole, so neither a
// single long element nor a list of short ones can grow the parameter past
// maximumDisplayedVendorList.
//
// A list whose elements did not all fit says so, which is what distinguishes a
// bounded presentation from a silently shortened one. The count reported beside
// it stays exact either way.
func displayVendorList(values []string) string {
	var joined strings.Builder
	for index, value := range values {
		separator := ""
		if index != 0 {
			separator = ","
		}
		// The bound is tested against the escaped text but is never applied by
		// cutting it: a value is either shown whole, under its own per-value
		// bound, or left out. Slicing the escape instead could split a
		// multi-byte rune the value passed through, or an escape this
		// presentation wrote, and hand an operator a value it never held.
		escaped := displayVendorValue(value, maximumDisplayedVendorValue)
		if joined.Len()+len(separator)+len(escaped) > maximumDisplayedVendorList {
			joined.WriteString(displayTruncationMarker)
			return joined.String()
		}
		joined.WriteString(separator)
		joined.WriteString(escaped)
	}
	return joined.String()
}

func (m *Microsoft) collect(ctx context.Context, rt hosted.Runtime, dataset Dataset, subscriptionID string) (bool, error) {
	key := stateKey(m.conf.StateNamespace(), dataset.Name, subscriptionID)
	state, err := loadState(rt, key)
	if err != nil {
		if errors.Is(err, errIncompatibleContinuation) {
			// loadState hands back every document it could read plus a
			// replacement for each one it could not, so the durable watermark
			// survives while the unreadable keys are overwritten. Returning the
			// error instead would repeat on every poll forever.
			return m.resetStoredContinuation(rt, key, dataset, state, err)
		}
		return false, fmt.Errorf("load %s state: %w", dataset.Name, err)
	}
	now := m.now().UTC()
	if !temporalDataset(dataset) && state.Continuation == nil && !state.Watermark.IsZero() {
		interval := time.Duration(m.conf.Request_Interval) * time.Second
		if now.Before(state.Watermark.Add(interval)) {
			return false, nil
		}
	}
	start, end := time.Time{}, time.Time{}
	if state.Continuation != nil {
		start, end = state.WindowStart, state.WindowEnd
	} else {
		var ready bool
		start, end, ready = finalizedWindow(dataset, state.Watermark, now, m.conf.LookbackDuration())
		if !ready {
			return false, nil
		}
		state.WindowStart, state.WindowEnd = start, end
		state.Fetched = 0
	}

	var scan *fabricScan
	if dataset.Kind == KindFabricActivity {
		expiredPending := fabricPendingExpired(state, now)
		scan, start, end = fabricWindow(state, start, end, now)
		state.WindowStart, state.WindowEnd = start, end
		if expiredPending {
			state.Continuation = nil
			state.Fetched = 0
		}
		if start.After(scan.Anchor) && fabricRetentionLossUnread(state.Continuation, start) {
			// The window start this poll asked for is older than what Fabric
			// still serves, so the interval between them is permanently lost.
			m.reportFabricRetentionLoss(rt, dataset, scan.Anchor, start)
		}
	}

	chunk, err := m.client.FetchChunk(ctx, dataset, start, end, subscriptionID, state.Continuation)
	if err != nil {
		if errors.Is(err, errIncompatibleContinuation) && state.Continuation != nil {
			return m.resetStoredContinuation(rt, key, dataset, state, err)
		}
		return false, fmt.Errorf("collect %s: %w", dataset.Name, err)
	}
	if chunk.Malformed > 0 {
		firstPage, firstOrdinal, firstReason := 0, 0, ""
		if len(chunk.Diagnostics) != 0 {
			firstPage, firstOrdinal, firstReason = chunk.Diagnostics[0].Page, chunk.Diagnostics[0].Ordinal, chunk.Diagnostics[0].Reason
		}
		rt.Warn("Microsoft API records skipped after bounded decode failures",
			log.KV("selector", dataset.Name), log.KV("count", chunk.Malformed),
			log.KV("first_page", firstPage), log.KV("first_ordinal", firstOrdinal), log.KV("first_reason", firstReason))
	}
	prepared, overlap, outsideWindow, undated, replayed, droppedReceipts, err := m.prepareRecords(dataset, state, chunk.Records, now)
	if err != nil {
		return false, fmt.Errorf("prepare %s entries: %w", dataset.Name, err)
	}
	state.Overlap = overlap
	tag, err := m.negotiateTag(rt, dataset)
	if err != nil {
		return false, err
	}
	written := 0
	for _, record := range prepared {
		// SRC is deliberately left unset. The ingest muxer assigns the real
		// ingest source to every entry that arrives without one, so pinning an
		// address here would replace that attribution on every record.
		ent := entry.Entry{TS: entry.FromStandard(record.Timestamp), Tag: tag, Data: record.Payload}
		if err := attachSourceIdentity(&ent, dataset); err != nil {
			return false, err
		}
		if err := rt.Write(ent); err != nil {
			return false, fmt.Errorf("write %s entry: %w", dataset.Name, err)
		}
		written++
	}
	if outsideWindow > 0 {
		rt.Warn("Microsoft API records outside the finalized window were skipped",
			log.KV("selector", dataset.Name), log.KV("count", outsideWindow))
	}
	if undated > 0 {
		// Reported separately from the skip above: these records were delivered,
		// and the cause is a missing or unparsable source field rather than an
		// event timestamp outside the window being finalized.
		rt.Warn("Microsoft API records without a readable source timestamp were delivered at the window boundary",
			log.KV("selector", dataset.Name), log.KV("count", undated),
			log.KV("time_field", dataset.TimeField),
			log.KV("assigned", state.WindowEnd.Add(-time.Nanosecond).UTC().Format(time.RFC3339Nano)))
	}
	if replayed > 0 {
		rt.Debug("Microsoft API overlap records already delivered were suppressed",
			log.KV("selector", dataset.Name), log.KV("count", replayed))
	}
	if droppedReceipts > 0 {
		// The receipts that were dropped are the oldest ones inside the replay
		// horizon, so the records they covered may be delivered again by the
		// next overlapping window. Nothing is skipped, and the condition has to
		// stay visible: the poll itself completes, and hosted/job.go clears the
		// error channel after every poll that returns none.
		rt.Warn("Microsoft API overlap receipt capacity was reached; the oldest receipts in the replay horizon were dropped",
			log.KV("selector", dataset.Name), log.KV("dropped", droppedReceipts),
			log.KV("capacity", m.overlapReceiptCapacity()))
		rt.SetWarn(fmt.Sprintf("Microsoft API selector %s reached its overlap receipt capacity of %d; records inside the %s replay horizon may be delivered more than once",
			dataset.Name, m.overlapReceiptCapacity(), sourceReplayOverlap))
	}
	if chunk.ChildBudgetExhausted > 0 {
		// One or more parents of a fan-out supplied more child pages than any
		// real parent could. The rest of each of those parents' child lists is
		// permanently skipped, and that is where the loss stops: the window
		// keeps its parent-list cursor, the parents behind the offending one are
		// still collected, and this poll continues normally.
		m.reportParentChildPageBound(rt, dataset, state, chunk)
	}
	state.Fetched += len(chunk.Records)
	state.Continuation = chunk.Continuation
	if !chunk.Complete && chunk.PageBudgetExhausted {
		// The window read its whole cumulative page budget without the source
		// ever running out of cursors. Resuming it would read the same budget
		// again on every pass forever, and clearing the cursor to restart the
		// window would deliver the same pages again just as often. Close the
		// window against its own end instead, after everything this pass read
		// has been written, and state the truncation: what was not reached is
		// bounded permanent loss, and the next window starts past it.
		m.reportContinuationPageBound(rt, dataset, state, chunk.Continuation)
	} else if !chunk.Complete {
		if err := saveState(rt, key, state); err != nil {
			return false, fmt.Errorf("persist %s continuation: %w", dataset.Name, err)
		}
		rt.Info("continued Microsoft API poll", log.KV("dataset", dataset.Name), log.KV("fetched", len(chunk.Records)), log.KV("written", written))
		return true, nil
	}

	state.Watermark = state.WindowEnd
	if temporalDataset(dataset) {
		state.Overlap = pruneTemporalOverlap(state.Overlap, state.Watermark.Add(-sourceReplayOverlap))
	}
	if scan != nil {
		scan.Anchor = state.Watermark
		state.Fabric = scan
	}
	state.WindowStart, state.WindowEnd = time.Time{}, time.Time{}
	state.Continuation = nil
	fetched := state.Fetched
	state.Fetched = 0
	if err := saveState(rt, key, state); err != nil {
		return false, fmt.Errorf("persist %s state: %w", dataset.Name, err)
	}
	rt.Info("completed Microsoft API poll", log.KV("dataset", dataset.Name), log.KV("fetched", fetched), log.KV("written", written))
	return false, nil
}

// reportFabricRetentionLoss states Fabric activity history the source can no
// longer serve. Fabric retains 28 days, so a window start older than that floor
// names an interval no later reconfiguration can recover: fabricWindow resumes
// the window at the floor, and every record before it is permanently skipped.
//
// It is published on the status channel the poll loop leaves alone as well as in
// the log, for the same reason the page bounds are: the poll itself continues
// and returns a nil error, and hosted/job.go calls ClearError() after every poll
// that returns none, so an error could not reach the status API an operator
// watches. The window and the checkpoint are untouched by this report.
func (m *Microsoft) reportFabricRetentionLoss(rt hosted.Runtime, dataset Dataset, anchor, availableSince time.Time) {
	skipped := availableSince.Sub(anchor)
	rt.Error("Fabric activity history is outside the available retention window; the interval before it is permanently skipped",
		log.KV("selector", dataset.Name),
		log.KV("anchor", anchor.UTC().Format(time.RFC3339Nano)),
		log.KV("available_since", availableSince.UTC().Format(time.RFC3339Nano)),
		log.KV("skipped", skipped.String()))
	rt.SetWarn(fmt.Sprintf("Microsoft API selector %s permanently skipped %s of Fabric activity history from %s to %s; the source retains only %s",
		dataset.Name, skipped, anchor.UTC().Format(time.RFC3339), availableSince.UTC().Format(time.RFC3339), fabricRetention))
}

// fabricRetentionLossUnread reports whether the interval a retention clamp
// dropped is history no pass has read. The clamp moves with wall-clock time, so
// a multi-pass pending window whose frozen start ages below the floor is clamped
// on every later pass even though its preserved cursor has already walked past
// the new floor: the Fabric day walk only advances its cursor by completing a
// day, so a cursor at or after the clamped start proves the clamped interval was
// collected. Anchoring the report on the frozen start alone restated permanent
// loss for that history on every pass of the window.
//
// A cursor the source can no longer serve is not this case. fabricPendingExpired
// drops the pending window before this runs, which leaves no continuation here,
// so genuine expiry is still reported.
func fabricRetentionLossUnread(cursor *FetchContinuation, availableSince time.Time) bool {
	if cursor == nil {
		return true
	}
	return cursor.FabricCursor.IsZero() || cursor.FabricCursor.Before(availableSince)
}

// reportContinuationPageBound states a window that reached its cumulative page
// bound. The loss is permanent for that window and bounded to it, so it is
// published on the status channel the poll loop leaves alone as well as in the
// log: the poll itself completes, and hosted/job.go calls ClearError() after
// every poll that returns none, so an error could not reach an operator.
func (m *Microsoft) reportContinuationPageBound(rt hosted.Runtime, dataset Dataset, state streamState, cursor *FetchContinuation) {
	pages, childPages := 0, 0
	if cursor != nil {
		pages, childPages = cursor.Pages, cursor.ChildPages
	}
	// A parent-child fan-out reaches this bound through its parent-list pages
	// only -- the per-parent child bound truncates one parent and is reported on
	// its own path -- so the child count is reported here as context for which
	// parent the window stopped on rather than as the bound that stopped it.
	rt.Error("Microsoft API window reached its cumulative page bound; the pages it could not read are permanently skipped",
		log.KV("selector", dataset.Name), log.KV("pages", pages),
		log.KV("bound", m.client.continuationPageBound()),
		log.KV("child_pages", childPages),
		log.KV("child_bound", m.client.parentChildPageBound()),
		log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339Nano)),
		log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339Nano)))
	rt.SetWarn(fmt.Sprintf("Microsoft API selector %s reached its cumulative page bound of %d for the window ending %s; the remainder of that window was not collected",
		dataset.Name, m.client.continuationPageBound(), state.WindowEnd.UTC().Format(time.RFC3339)))
}

// reportParentChildPageBound states the parents of a fan-out whose per-parent
// child-page bound this pass reached. The loss is permanent for the child lists
// of those parents and bounded to them: the window itself is not closed, so
// every other parent is still collected and the next window is unaffected.
//
// It is published on the status channel the poll loop leaves alone as well as in
// the log, for the same reason the cumulative window bound is: the poll
// completes, and hosted/job.go calls ClearError() after every poll that returns
// none, so an error could not reach an operator.
//
// The parent identifiers are the only vendor-controlled bytes in this report,
// and they are decoded verbatim from the parent-list page: the identity field
// of a parent record is an arbitrary JSON string, so it can carry CR, LF or any
// other control character. Both insertions therefore go through the shared
// presentation contract. An interior CR LF used to end the physical record at
// first_parent or inside reported_parents and let the remainder of the
// identifier forge further records in the operator's log (CWE-117).
//
// Only the presentation changes. ParentIDs keeps the canonical bytes, which are
// what the child URL path escape, the parent/child dispatch, the per-parent
// cursor identity and the persisted cursor all read.
func (m *Microsoft) reportParentChildPageBound(rt hosted.Runtime, dataset Dataset, state streamState, chunk FetchChunk) {
	first := ""
	if len(chunk.TruncatedParents) != 0 {
		first = displayVendorValue(chunk.TruncatedParents[0], maximumDisplayedVendorValue)
	}
	rt.Error("Microsoft API parents reached their per-parent child page bound; the child pages they could not supply are permanently skipped",
		log.KV("selector", dataset.Name), log.KV("parents", chunk.ChildBudgetExhausted),
		log.KV("child_bound", m.client.parentChildPageBound()),
		log.KV("first_parent", first),
		log.KV("reported_parents", displayVendorList(chunk.TruncatedParents)),
		log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339Nano)),
		log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339Nano)))
	rt.SetWarn(fmt.Sprintf("Microsoft API selector %s truncated %d parent(s) at the per-parent child page bound of %d for the window ending %s; the rest of those parents was not collected",
		dataset.Name, chunk.ChildBudgetExhausted, m.client.parentChildPageBound(), state.WindowEnd.UTC().Format(time.RFC3339)))
}

// resetStoredContinuation clears a cursor document this build cannot resume and
// leaves the durable watermark, the Fabric anchor, and the overlap receipts
// untouched, so the next scheduled poll restarts the window from the last
// completed boundary. Documents that could not be decoded at all arrive here
// already replaced by loadState, so saveState overwrites those keys rather than
// skipping them. Returning the error instead would repeat forever: nothing else
// in the plugin clears the cursor key. The reset is not reported as pending
// work because a drain pass skips any target whose state has no continuation.
//
// It is published on the status channel the poll loop leaves alone as well as
// in the log. The reset is safe but not free: the window it discards is
// re-fetched from the retained watermark, and a source that keeps handing back
// a cursor this build refuses repeats that on every poll indefinitely. The poll
// itself returns no error, and hosted/job.go calls ClearError() after every
// poll that returns none, so the log line alone could not reach an operator.
func (m *Microsoft) resetStoredContinuation(rt hosted.Runtime, key string, dataset Dataset, state streamState, cause error) (bool, error) {
	rt.Warn("Microsoft API stored continuation was reset; the current window restarts from the durable checkpoint",
		log.KV("selector", dataset.Name),
		log.KV("watermark", state.Watermark.UTC().Format(time.RFC3339Nano)),
		log.KV("reason", m.safePollError(cause)))
	rt.SetWarn(fmt.Sprintf("Microsoft API selector %s reset a stored continuation it cannot resume and restarts from the retained watermark %s; reason: %s",
		dataset.Name, state.Watermark.UTC().Format(time.RFC3339), m.safePollError(cause)))
	state.Continuation = nil
	state.Fetched = 0
	state.WindowStart, state.WindowEnd = time.Time{}, time.Time{}
	if err := saveState(rt, key, state); err != nil {
		return false, fmt.Errorf("reset %s continuation: %w", dataset.Name, err)
	}
	return false, nil
}

func finalizedWindow(dataset Dataset, previous, now time.Time, lookback time.Duration) (time.Time, time.Time, bool) {
	end := now.UTC()
	if !temporalDataset(dataset) {
		return end.Add(-lookback), end, true
	}
	end = end.Add(-sourceConsistencyDelay)
	if dataset.Kind == KindFabricActivity {
		end = end.Truncate(time.Millisecond)
	}
	start := end.Add(-lookback)
	if !previous.IsZero() {
		if !previous.UTC().Before(end) {
			return time.Time{}, time.Time{}, false
		}
		start = previous.UTC().Add(-sourceReplayOverlap)
	} else if dataset.Kind == KindFabricActivity {
		start = start.Truncate(time.Millisecond)
	}
	return start, end, start.Before(end)
}

// overlapReceiptCapacity resolves the effective overlap receipt bound. A zero
// or negative override falls back to the production bound rather than
// disabling the guard.
func (m *Microsoft) overlapReceiptCapacity() int {
	if m.overlapReceiptLimit > 0 {
		return m.overlapReceiptLimit
	}
	return maximumOverlapReceipts
}

func timestampInHalfOpenWindow(timestamp, start, end time.Time) bool {
	return !timestamp.IsZero() && !timestamp.Before(start) && timestamp.Before(end)
}

// prepareRecords converts one fetched chunk into deliverable entries and the
// overlap receipts that suppress its replay. The final count reports how many
// receipts the capacity bound had to drop, which is bounded replay the caller
// has to publish rather than a failure.
func (m *Microsoft) prepareRecords(dataset Dataset, state streamState, records []FetchedRecord, now time.Time) ([]preparedTemporalRecord, *temporalOverlap, int, int, int, int, error) {
	prepared := make([]preparedTemporalRecord, 0, len(records))
	if !temporalDataset(dataset) {
		for _, record := range records {
			timestamp := record.Timestamp
			if timestamp.IsZero() {
				timestamp = now
			}
			payload, err := formatRecord(record.Raw)
			if err != nil {
				return nil, state.Overlap, 0, 0, 0, 0, err
			}
			prepared = append(prepared, preparedTemporalRecord{Timestamp: timestamp, Payload: payload})
		}
		return prepared, state.Overlap, 0, 0, 0, 0, nil
	}

	overlap := pruneTemporalOverlap(state.Overlap, state.WindowStart)
	seen := make(map[string]struct{}, len(overlap.Receipts)+len(records))
	for _, receipt := range overlap.Receipts {
		seen[receipt.Fingerprint] = struct{}{}
	}
	retainFrom := state.WindowEnd.Add(-sourceReplayOverlap)
	limit := m.overlapReceiptCapacity()
	// A record the source left undated is admitted at the inside edge of the
	// window being finalized, which is the latest instant it is known not to
	// have happened after.
	undatedTimestamp := state.WindowEnd.Add(-time.Nanosecond)
	if state.WindowEnd.IsZero() {
		undatedTimestamp = now
	}
	outside, undated, replayed := 0, 0, 0
	for _, record := range records {
		timestamp, fingerprint := record.Timestamp, ""
		if timestamp.IsZero() {
			// The source omitted its own time field, or wrote it in a layout
			// parseTimestamp does not accept. Skipping the record loses it
			// permanently: the window completes and the watermark advances past
			// it. Deliver it instead, and key its receipt on source identity
			// alone -- the substituted timestamp moves with the window, so a
			// timestamp-keyed receipt could never suppress the replay.
			undated++
			timestamp = undatedTimestamp
			fingerprint = undatedReceiptFingerprint(record.ID)
		} else {
			if !timestampInHalfOpenWindow(timestamp, state.WindowStart, state.WindowEnd) {
				outside++
				continue
			}
			fingerprint = temporalReceiptFingerprint(timestamp, record.ID)
		}
		if _, ok := seen[fingerprint]; ok {
			replayed++
			continue
		}
		seen[fingerprint] = struct{}{}
		payload, err := formatRecord(record.Raw)
		if err != nil {
			return nil, state.Overlap, 0, 0, 0, 0, err
		}
		prepared = append(prepared, preparedTemporalRecord{Timestamp: timestamp, Payload: payload})
		if !timestamp.Before(retainFrom) {
			overlap.Receipts = append(overlap.Receipts, temporalReceipt{Timestamp: timestamp, Fingerprint: fingerprint})
		}
	}
	// The set is bounded once, after the batch, rather than per record: what it
	// may hold in between is the pruned set plus one chunk, which the chunk
	// retention budget already bounds, and trimming inside the loop would sort
	// the whole set once per record.
	retained, dropped := trimTemporalReceipts(overlap.Receipts, limit)
	overlap.Receipts = retained
	return prepared, overlap, outside, undated, replayed, dropped, nil
}

// trimTemporalReceipts bounds the overlap receipt set at the newest receipts it
// can still use. The condition this replaced was terminal: the capacity error
// was never persisted and no later poll could produce a different answer, so a
// window dense enough to fill the horizon stopped the selector for good while
// its own records were never delivered.
//
// Delivery is at-least-once, and a receipt is only ever an optimization on top
// of that: dropping the oldest receipts inside the horizon can replay the
// records they covered on the next overlapping window, which is bounded by the
// horizon itself and reported to the operator. Receipts are ordered by
// timestamp and then by fingerprint so the retained set is deterministic
// whatever order the source published the records in.
func trimTemporalReceipts(receipts []temporalReceipt, limit int) ([]temporalReceipt, int) {
	if limit < 1 || len(receipts) <= limit {
		return receipts, 0
	}
	slices.SortStableFunc(receipts, func(a, b temporalReceipt) int {
		if !a.Timestamp.Equal(b.Timestamp) {
			return a.Timestamp.Compare(b.Timestamp)
		}
		return strings.Compare(a.Fingerprint, b.Fingerprint)
	})
	return append([]temporalReceipt(nil), receipts[len(receipts)-limit:]...), len(receipts) - limit
}

func pruneTemporalOverlap(overlap *temporalOverlap, retainFrom time.Time) *temporalOverlap {
	pruned := &temporalOverlap{Version: temporalOverlapVersion}
	if overlap == nil {
		return pruned
	}
	pruned.Receipts = make([]temporalReceipt, 0, len(overlap.Receipts))
	for _, receipt := range overlap.Receipts {
		if !receipt.Timestamp.Before(retainFrom) {
			pruned.Receipts = append(pruned.Receipts, receipt)
		}
	}
	return pruned
}

// undatedReceiptFingerprint keys the overlap receipt of an admitted record that
// carried no source timestamp. The identity is still the record's own, so two
// different undated records never collapse onto one receipt, and the key is
// stable across windows so the next overlapping poll suppresses the replay.
func undatedReceiptFingerprint(identity string) string {
	return temporalReceiptFingerprint(time.Time{}, identity)
}

func temporalReceiptFingerprint(timestamp time.Time, identity string) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(timestamp.UTC().Format(time.RFC3339Nano)))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(identity))
	return hex.EncodeToString(digest.Sum(nil))
}

func temporalDataset(dataset Dataset) bool {
	if dataset.Mode == ModeSnapshot {
		return false
	}
	return dataset.Kind == KindAzureActivity || dataset.Kind == KindAdvancedHunting || dataset.Kind == KindFabricActivity ||
		(dataset.Service == ServiceGraph && dataset.TimeField != "")
}

func (m *Microsoft) negotiateTag(rt hosted.Runtime, dataset Dataset) (entry.EntryTag, error) {
	return m.negotiateTagName(rt, m.conf.TagForDataset(dataset))
}

func (m *Microsoft) negotiateTagName(rt hosted.Runtime, name string) (entry.EntryTag, error) {
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

// stateHasPending reports whether a selector has an in-flight cursor, without
// collecting it. It reads through loadState, so the stored arrays it has to get
// past are the ones storedCursor streams under their own bounds: the pending
// check a drain pass runs for every target cannot be made to materialize what
// those bounds refuse. A document it cannot read is reported to
// targetHasPending, which visits the selector anyway so collect can replace it.
func stateHasPending(rt hosted.Storage, key string) (bool, error) {
	state, err := loadState(rt, key)
	return state.Continuation != nil, err
}

// loadState reconstructs a selector's checkpoint from its discrete state keys.
// A storage failure is returned as-is, but a document that is present and
// unreadable -- corrupt bytes, or an overlap document written by a build this
// one was rolled back from -- is reported as errIncompatibleContinuation
// alongside the state that did load, with a replacement document in place of
// each unreadable one. Nothing else in the plugin rewrites these keys, so
// failing outright would wedge the selector on every later poll even though the
// durable watermark needed to resume is intact.
func loadState(rt hosted.Storage, key string) (streamState, error) {
	var state streamState
	var unreadable []error
	if watermark, err := rt.GetTime(key + "/timestamp"); err == nil {
		state.Watermark = watermark
	} else if !errors.Is(err, storage.ErrStorageNotFound) {
		return streamState{}, err
	}
	if encoded, err := rt.Get(key + "/cursor"); err == nil {
		var cursor storedCursor
		if len(encoded) > maximumCursorDocumentBytes {
			// Measured before the decode rather than after it. A document this
			// large is refused whether or not it is well-formed, so the cost of
			// reading a stored cursor stays bounded by the ceiling instead of
			// by whatever the state store happens to hold.
			unreadable = append(unreadable, fmt.Errorf("Microsoft cursor document is %d bytes, over the %d-byte ceiling", len(encoded), maximumCursorDocumentBytes))
		} else if err := json.Unmarshal(encoded, &cursor); err != nil {
			// A reset always rewrites the cursor key, so no replacement is
			// needed here; the window simply restarts from the watermark.
			//
			// The ceiling bounds the bytes that are read; the bounded arrays in
			// storedCursor are what bound what decoding them retains. Inside the
			// ceiling, a cursor dense in members expanded to millions of
			// continuation entries, parent identifiers or planned windows before
			// FetchContinuation.validate ever saw the slices.
			unreadable = append(unreadable, fmt.Errorf("decode Microsoft cursor: %w", err))
		} else if err := rejectDuplicateStateMembers(encoded); err != nil {
			unreadable = append(unreadable, fmt.Errorf("decode Microsoft cursor: %w", err))
		} else {
			state.WindowStart, state.WindowEnd = cursor.WindowStart, cursor.WindowEnd
			state.Fetched = cursor.Fetched
			if cursor.Continuation.value != nil {
				state.Continuation = cursor.Continuation.value.continuation()
			}
		}
	} else if !errors.Is(err, storage.ErrStorageNotFound) {
		return streamState{}, err
	}
	if encoded, err := rt.Get(key + "/fabric"); err == nil {
		var scan fabricScan
		if len(encoded) > maximumFabricStateBytes {
			// Measured before both decoders. Unknown members are discarded by
			// json.Unmarshal, so measuring the decoded three-timestamp struct
			// would not bound the stored bytes read and scanned here.
			state.Fabric = &fabricScan{}
			unreadable = append(unreadable, fmt.Errorf("Microsoft Fabric state document is %d bytes, over the %d-byte ceiling", len(encoded), maximumFabricStateBytes))
		} else if err := json.Unmarshal(encoded, &scan); err != nil {
			// An empty scan is inert: fabricWindow only ever widens the
			// available range from stored metadata, never narrows it.
			state.Fabric = &fabricScan{}
			unreadable = append(unreadable, fmt.Errorf("decode Microsoft Fabric state: %w", err))
		} else if err := rejectDuplicateStateMembers(encoded); err != nil {
			state.Fabric = &fabricScan{}
			unreadable = append(unreadable, fmt.Errorf("decode Microsoft Fabric state: %w", err))
		} else {
			state.Fabric = &scan
		}
	} else if !errors.Is(err, storage.ErrStorageNotFound) {
		return streamState{}, err
	}
	if encoded, err := rt.Get(key + "/overlap"); err == nil {
		overlap, err := decodeTemporalOverlap(encoded)
		if err != nil {
			// Dropping the receipts can only replay records the overlap horizon
			// still covers, which delivery is idempotent against by design.
			state.Overlap = &temporalOverlap{Version: temporalOverlapVersion}
			unreadable = append(unreadable, err)
		} else {
			state.Overlap = overlap
		}
	} else if !errors.Is(err, storage.ErrStorageNotFound) {
		return streamState{}, err
	}
	if len(unreadable) != 0 {
		return state, fmt.Errorf("%w: %w", errIncompatibleContinuation, errors.Join(unreadable...))
	}
	return state, nil
}

// storedTemporalOverlap is the stored overlap document, read one receipt at a
// time.
//
// Decoding straight into []temporalReceipt materialized the whole set before
// any bound applied and then charged each receipt the canonical re-encoding of
// what it parsed to, which is not what reading or retaining it costs: the
// stored bytes are what the aggregate budget has to be charged against, and
// they are only available before the element is parsed.
//
// Holding the members as []json.RawMessage instead moved the charge to the
// right bytes but not the allocation: json.Unmarshal still built the whole
// slice -- one 24-byte header plus a copy of its bytes per member -- before
// either the count bound or the aggregate budget was consulted, so a document
// inside the pre-decode ceiling but dense in two-byte members allocated an
// order of magnitude more members than the count bound admits and hundreds of
// megabytes with them (CWE-770). The array is streamed now, so both bounds are
// applied to each member before anything retains it.
type storedTemporalOverlap struct {
	Version  int                           `json:"version"`
	Receipts storedTemporalOverlapReceipts `json:"receipts,omitempty"`
}

// storedTemporalOverlapReceipts streams the stored receipt array under the
// count bound and the aggregate stored-byte budget, parsing and validating each
// member only once both have admitted it. A null member is the absent member,
// which is what decoding into a slice already left.
type storedTemporalOverlapReceipts struct {
	values []temporalReceipt
	seen   bool
}

func (receipts *storedTemporalOverlapReceipts) UnmarshalJSON(raw []byte) error {
	if receipts.seen {
		return &temporalOverlapStateError{err: errors.New("Microsoft overlap state repeats bounded member receipts")}
	}
	receipts.seen = true
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	if token != json.Delim('[') {
		return &temporalOverlapStateError{err: errors.New("Microsoft overlap state receipts must be a JSON array")}
	}
	total := 0
	for decoder.More() {
		// The count bound first, because it is the cheap one and it is what
		// bounds the allocation on its own: nothing past it is ever decoded,
		// let alone retained.
		if len(receipts.values) >= maximumOverlapReceipts {
			return &temporalOverlapStateError{err: fmt.Errorf("Microsoft overlap state exceeds receipt capacity %d", maximumOverlapReceipts)}
		}
		var element json.RawMessage
		if err := decoder.Decode(&element); err != nil {
			return err
		}
		// Charged and bounded before the receipt is parsed, so neither the
		// suppression map prepareRecords builds nor the document the next
		// completion persists can be grown past the budget by a document that
		// satisfied the count bound.
		if total += len(element) + 1; total > maximumOverlapReceiptBytes {
			return &temporalOverlapStateError{err: fmt.Errorf("Microsoft overlap receipts occupy more than the %d-byte budget", maximumOverlapReceiptBytes)}
		}
		var receipt temporalReceipt
		if err := json.Unmarshal(element, &receipt); err != nil {
			return &temporalOverlapStateError{err: fmt.Errorf("decode Microsoft overlap receipt: %w", err)}
		}
		if err := validateTemporalReceipt(receipt); err != nil {
			return &temporalOverlapStateError{err: err}
		}
		receipts.values = append(receipts.values, receipt)
	}
	_, err = decoder.Token()
	return err
}

// temporalOverlapStateError marks an overlap receipt refused on a bound or on
// its own shape rather than on the document's syntax, so decodeTemporalOverlap
// can report it as itself instead of behind a generic decode message. Neither
// form names a stored byte.
type temporalOverlapStateError struct{ err error }

func (e *temporalOverlapStateError) Error() string { return e.err.Error() }

func (e *temporalOverlapStateError) Unwrap() error { return e.err }

func decodeTemporalOverlap(encoded []byte) (*temporalOverlap, error) {
	// Measured before the decode rather than after it, so the cost of reading a
	// stored overlap document is bounded by the ceiling instead of by whatever
	// the state store happens to hold -- which is the only bound the receipt set
	// inside it had in aggregate.
	if len(encoded) > maximumOverlapDocumentBytes {
		return nil, fmt.Errorf("Microsoft overlap document is %d bytes, over the %d-byte ceiling", len(encoded), maximumOverlapDocumentBytes)
	}
	var stored storedTemporalOverlap
	if err := json.Unmarshal(encoded, &stored); err != nil {
		// The receipts are streamed during this decode, so a document that is
		// both over a bound and of an unsupported version reports the bound
		// rather than the version. loadState answers both the same way: the
		// document is replaced and the pass reports errIncompatibleContinuation.
		var state *temporalOverlapStateError
		if errors.As(err, &state) {
			return nil, state.err
		}
		return nil, fmt.Errorf("decode Microsoft overlap state: %w", err)
	}
	if err := rejectDuplicateStateMembers(encoded); err != nil {
		return nil, fmt.Errorf("decode Microsoft overlap state: %w", err)
	}
	if stored.Version != temporalOverlapVersion {
		return nil, fmt.Errorf("Microsoft overlap state has unsupported version %d", stored.Version)
	}
	return &temporalOverlap{Version: stored.Version, Receipts: stored.Receipts.values}, nil
}

// validateTemporalReceipt requires the canonical receipt shape. Neither field
// can be trusted from the store: a receipt with no timestamp survives no prune
// -- pruneTemporalOverlap keys on it and the zero time is before every horizon
// -- yet would have been trusted to suppress delivery for the pass that read
// it, and a fingerprint that is not a canonical digest is a key no record this
// plugin fetches can ever produce, so it can only occupy budget.
//
// The value is never named in the error. A stored fingerprint is vendor-derived
// bytes as far as this path is concerned, and an operator diagnostic is not
// where they belong.
func validateTemporalReceipt(receipt temporalReceipt) error {
	if receipt.Timestamp.IsZero() {
		return errors.New("Microsoft overlap state contains an invalid receipt with no timestamp")
	}
	if !canonicalTemporalFingerprint(receipt.Fingerprint) {
		return fmt.Errorf("Microsoft overlap state contains an invalid receipt whose fingerprint is not %d lowercase hexadecimal characters", temporalFingerprintLength)
	}
	return nil
}

// canonicalTemporalFingerprint reports whether fingerprint is exactly the form
// temporalReceiptFingerprint produces. Uppercase hexadecimal is refused as well
// as non-hexadecimal: the same digest written in two cases is two different map
// keys, so admitting both would make the suppression set able to hold two
// receipts for one record and bill each of them against the budget.
func canonicalTemporalFingerprint(fingerprint string) bool {
	if len(fingerprint) != temporalFingerprintLength {
		return false
	}
	for index := 0; index < len(fingerprint); index++ {
		switch character := fingerprint[index]; {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			return false
		}
	}
	return true
}

type fabricStateMarshalFunc func(*fabricScan) ([]byte, error)

func marshalFabricState(scan *fabricScan) ([]byte, error) {
	return json.Marshal(scan)
}

func saveState(rt hosted.Storage, key string, state streamState) error {
	return saveStateWithFabricMarshal(rt, key, state, marshalFabricState)
}

// saveStateWithFabricMarshal keeps the Fabric encoder injectable only for the
// boundary regression: fabricScan's fixed three-field shape cannot naturally
// cross its deliberately future-proof ceiling. The production path always
// supplies marshalFabricState.
func saveStateWithFabricMarshal(rt hosted.Storage, key string, state streamState, marshalFabric fabricStateMarshalFunc) error {
	// The cursor document is encoded and measured before any key is written.
	// It is the one document here whose size is driven by source values -- the
	// continuation URLs, the skip token and the parent list all live in it --
	// and the per-field bounds the fetch paths enforce are a statement about
	// the document only if the document itself is checked. Failing before the
	// first Put leaves the whole previous checkpoint intact, so an oversized
	// cursor is never half-written and the poll that produced it re-reads its
	// pages instead of advancing past them.
	progress := cursorState{
		WindowStart: state.WindowStart, WindowEnd: state.WindowEnd,
		Continuation: state.Continuation, Fetched: state.Fetched,
	}
	encoded, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	if len(encoded) > maximumCursorDocumentBytes {
		return fmt.Errorf("Microsoft cursor document is %d bytes, over the %d-byte ceiling", len(encoded), maximumCursorDocumentBytes)
	}
	// The overlap document is encoded and measured here, beside the cursor and
	// before the first Put, for the same reason: it is the other document whose
	// size source values drive, and a document the next load would refuse must
	// never be the one on disk. Failing before any key is written leaves the
	// whole previous checkpoint intact.
	var overlap []byte
	if state.Overlap != nil {
		if overlap, err = json.Marshal(state.Overlap); err != nil {
			return err
		}
		if len(overlap) > maximumOverlapDocumentBytes {
			return fmt.Errorf("Microsoft overlap document is %d bytes, over the %d-byte ceiling", len(overlap), maximumOverlapDocumentBytes)
		}
	}
	// Fabric metadata is encoded and measured beside the cursor and overlap,
	// before the first Put. A late refusal after either of those keys changed
	// would splice two checkpoints together and could advance delivery state
	// without its matching scan bounds.
	var fabric []byte
	if state.Fabric != nil {
		if fabric, err = marshalFabric(state.Fabric); err != nil {
			return err
		}
		if len(fabric) > maximumFabricStateBytes {
			return fmt.Errorf("Microsoft Fabric state document is %d bytes, over the %d-byte ceiling", len(fabric), maximumFabricStateBytes)
		}
	}
	if overlap != nil {
		if err := rt.Put(key+"/overlap", overlap); err != nil {
			return err
		}
	}
	if err := rt.Put(key+"/cursor", encoded); err != nil {
		return err
	}
	if fabric != nil {
		if err := rt.Put(key+"/fabric", fabric); err != nil {
			return err
		}
	}
	// Publish the timestamp last. A partial storage failure can replay a page,
	// but cannot advance the completed-window boundary before its cursor state.
	if !state.Watermark.IsZero() {
		return rt.PutTime(key+"/timestamp", state.Watermark)
	}
	return nil
}

var _ hosted.Job = (*Microsoft)(nil)
