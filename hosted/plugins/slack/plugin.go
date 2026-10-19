/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package slack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	pollBuffer            = time.Minute
	lateVisibilityOverlap = 5 * time.Minute
	maximumCursorCycles   = 3

	// acknowledgedWriteDeadline bounds how long one acknowledged entry write may
	// wait for an indexer to confirm it on disk. The Hosted Runner's health
	// signal does not prove a relay is available, and the native path blocks on
	// an unbuffered relay handoff plus that disk acknowledgement, so without a
	// deadline one entry could hold the poll loop indefinitely and keep the
	// plugin from reporting a stall. Exceeding it is an explicit at-least-once
	// outcome: the entry may still commit, so no receipt and no checkpoint
	// advance and the record is retried.
	acknowledgedWriteDeadline = 2 * time.Minute
)

type Plugin struct {
	conf *Config
	api  *client
	now  func() time.Time

	// writeDeadline is acknowledgedWriteDeadline; tests shorten it.
	writeDeadline time.Duration

	tagMu sync.Mutex
	tag   entry.EntryTag

	receiptMu         sync.Mutex
	receipts          *receiptLedger
	overlapReceipts   *receiptLedger
	receiptGeneration int64

	cursorMu         sync.Mutex
	cursors          *cursorLedger
	cursorGeneration int64
	cursorCycle      int64
}

var _ hosted.Job = (*Plugin)(nil)

func New(conf *Config) (*Plugin, error) {
	if conf == nil {
		return nil, errors.New("nil Slack config")
	}
	token, err := conf.credential()
	if err != nil {
		return nil, err
	}
	api, err := newClient(conf.Host, token, conf.Requests_Per_Minute, nil)
	if err != nil {
		return nil, err
	}
	return &Plugin{conf: conf, api: api, now: time.Now, writeDeadline: acknowledgedWriteDeadline}, nil
}

func (p *Plugin) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Resolve acknowledged delivery against the runtime that actually provides
	// it before negotiating a tag, touching state, or contacting Slack, so an
	// unsupported runtime cannot be discovered part way through a page.
	writer, ok := hosted.AcknowledgedWriterFor(rt)
	if !ok {
		return nil, errors.New("Slack Audit requires acknowledged runtime writes")
	}
	tag, err := p.negotiateTag(rt)
	if err != nil {
		return nil, err
	}
	state, err := loadPollState(rt)
	if err != nil {
		return nil, err
	}
	now := p.now().UTC()
	token, err := p.conf.credential()
	if err != nil {
		return nil, fmt.Errorf("reload API token: %w", err)
	}
	p.api.token = token
	if !state.Active {
		latest := now.Add(-pollBuffer).Unix()
		oldest := max(int64(0), now.Add(-pollBuffer-p.conf.LookbackDuration()).Unix())
		if state.CompletedThrough > 0 {
			// Lookback bounds only the initial history request. Later windows start
			// shortly before the durable checkpoint so late-visible events get one
			// bounded replay without re-reading the full horizon every cycle.
			oldest = max(int64(0), state.CompletedThrough-int64(lateVisibilityOverlap/time.Second))
		}
		if latest < oldest {
			return p.conf.ContinueAfterInterval(), nil
		}
		if err := beginPollState(rt, &state, oldest, latest); err != nil {
			return nil, err
		}
		if err := p.startFreshReceipts(rt, state.Generation); err != nil {
			return nil, err
		}
		p.clearCursorLedger()
	}

	if state.RetryAt != 0 {
		retryAt := time.Unix(0, state.RetryAt).UTC()
		if now.Before(retryAt) {
			return hosted.ContinueAfter(min(retryAt.Sub(now), maximumRetrySleep)), nil
		}
		if err := persistRetryAtState(rt, &state, 0); err != nil {
			return nil, err
		}
	}

	ledger, overlapLedger, err := p.receiptLedgers(rt, state.Generation)
	if err != nil {
		return nil, err
	}
	cursors, err := p.cursorLedger(rt, state)
	if err != nil {
		return nil, err
	}
	if cursors.full() {
		return nil, errors.New("Slack pagination reached the cursor marker capacity; active window halted before another vendor request")
	}
	// A prior acknowledged entry may have reached an indexer even if its
	// following receipt write failed. Prove core, receipt, overlap, and cursor
	// state are writable before contacting Slack again so a persistent storage
	// failure cannot repeatedly deliver that same entry.
	if err := verifyActiveStateWritable(rt, state, ledger, overlapLedger); err != nil {
		return nil, err
	}
	result, err := p.api.fetch(ctx, state.Oldest, state.Latest, state.Cursor)
	if err != nil {
		if errors.Is(err, errInvalidCursor) && state.Cursor != "" {
			if state.CursorCycle >= maximumCursorCycles {
				return nil, errors.New("Slack invalid pagination cursor persisted after bounded resets")
			}
			if saveErr := resetCursorState(rt, &state); saveErr != nil {
				return nil, saveErr
			}
			p.clearCursorLedger()
			return hosted.ContinueNow(), nil
		}
		var deferred *retryDeferral
		if errors.As(err, &deferred) {
			if saveErr := persistRetryAtState(rt, &state, deferred.retryAt.UnixNano()); saveErr != nil {
				return nil, saveErr
			}
			return hosted.ContinueAfter(deferred.delay), nil
		}
		return nil, err
	}
	if len(result.nextCursor) > maximumCursorSize {
		return nil, errors.New("Slack pagination cursor exceeds its size bound")
	}

	written := 0
	for _, raw := range result.records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(raw) > maximumAuditRecordSize {
			digest := skippedReceiptDigest("oversized", raw)
			if !ledger.contains(digest) {
				if err := ledger.remember(rt, digest); err != nil {
					p.clearReceipts()
					return nil, err
				}
				rt.Warn("skipping oversized Slack Audit record", log.KV("bytes", len(raw)))
			}
			continue
		}
		compact, identity, timestamp, err := normalizeRecord(raw)
		if err != nil {
			digest := skippedReceiptDigest("malformed", raw)
			if !ledger.contains(digest) {
				if rememberErr := ledger.remember(rt, digest); rememberErr != nil {
					p.clearReceipts()
					return nil, rememberErr
				}
				rt.Warn("skipping malformed Slack Audit record", log.KVErr(err))
			}
			continue
		}
		digest := recordReceiptDigest(identity, compact)
		if ledger.contains(digest) || overlapLedger.contains(digest) {
			continue
		}
		targetLedger := ledger
		if timestamp.Unix() >= state.Latest-int64(lateVisibilityOverlap/time.Second) {
			targetLedger = overlapLedger
		}
		ent := entry.Entry{TS: entry.FromStandard(timestamp), Tag: tag, Data: compact}
		if err := p.writeAcknowledged(ctx, writer, ent); err != nil {
			if errors.Is(err, ingest.ErrOversizedEntry) {
				if rememberErr := targetLedger.remember(rt, digest); rememberErr != nil {
					p.clearReceipts()
					return nil, rememberErr
				}
				rt.Warn("skipping Slack Audit record above the configured ingest entry size", log.KV("bytes", len(compact)))
				continue
			}
			return nil, err
		}
		if err := targetLedger.remember(rt, digest); err != nil {
			p.clearReceipts()
			return nil, err
		}
		written++
	}

	looped := result.nextCursor != "" && (result.nextCursor == state.Cursor || cursors.contains(result.nextCursor))
	if looped {
		if state.CursorCycle >= maximumCursorCycles {
			return nil, errors.New("Slack pagination cursor loop persisted after bounded resets")
		}
		if err := resetCursorState(rt, &state); err != nil {
			return nil, err
		}
		p.clearCursorLedger()
		rt.Warn("Slack pagination cursor loop detected; restarting the active window")
		return hosted.ContinueNow(), nil
	}
	if result.nextCursor != "" {
		if err := persistCursorState(rt, &state, result.nextCursor); err != nil {
			return nil, err
		}
		if err := cursors.remember(rt, &state, result.nextCursor); err != nil {
			p.clearCursorLedger()
			return nil, err
		}
		rt.Info("processed Slack Audit page", log.KV("records", len(result.records)), log.KV("written", written))
		return hosted.ContinueNow(), nil
	}

	if err := completePollState(rt, &state); err != nil {
		return nil, err
	}
	p.clearReceipts()
	p.clearCursorLedger()
	// Report the checkpoint that was persisted. A monotonic completion can retain
	// an earlier checkpoint, and the log must not claim otherwise.
	rt.Info("completed Slack Audit window", log.KV("through", state.CompletedThrough), log.KV("records", len(result.records)), log.KV("written", written))
	return p.conf.ContinueAfterInterval(), nil
}

// writeAcknowledged delivers one entry under a bounded wait. Exceeding the
// deadline is an at-least-once outcome rather than a rejection, so it is
// returned as an ordinary error: the caller persists no receipt, the checkpoint
// does not advance, and the record is retried on the next poll. The deadline
// error is distinct from context.Canceled, which the job adapter treats as
// shutdown.
func (p *Plugin) writeAcknowledged(ctx context.Context, writer hosted.AcknowledgedWriter, ent entry.Entry) error {
	deadline := p.writeDeadline
	if deadline <= 0 {
		deadline = acknowledgedWriteDeadline
	}
	writeCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	err := writer.WriteAcknowledged(writeCtx, ent)
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("Slack Audit acknowledged write exceeded its %s bound and remains unconfirmed: %w", deadline, err)
	}
	return err
}

func (p *Plugin) negotiateTag(rt hosted.Runtime) (entry.EntryTag, error) {
	p.tagMu.Lock()
	defer p.tagMu.Unlock()
	if p.tag != 0 {
		return p.tag, nil
	}
	tag, err := rt.NegotiateTag(p.conf.tag())
	if err != nil {
		return 0, err
	}
	p.tag = tag
	return tag, nil
}

func (p *Plugin) receiptLedgers(store hosted.Storage, generation int64) (*receiptLedger, *receiptLedger, error) {
	p.receiptMu.Lock()
	defer p.receiptMu.Unlock()
	if p.receipts != nil && p.overlapReceipts != nil && p.receiptGeneration == generation {
		return p.receipts, p.overlapReceipts, nil
	}
	ledger, err := loadReceiptLedger(store, generation)
	if err != nil {
		return nil, nil, err
	}
	overlapLedger, err := loadOverlapReceiptLedger(store, generation)
	if err != nil {
		return nil, nil, err
	}
	p.receipts = ledger
	p.overlapReceipts = overlapLedger
	p.receiptGeneration = generation
	return ledger, overlapLedger, nil
}

func (p *Plugin) clearReceipts() {
	p.receiptMu.Lock()
	p.receipts = nil
	p.overlapReceipts = nil
	p.receiptGeneration = 0
	p.receiptMu.Unlock()
}

func (p *Plugin) startFreshReceipts(store hosted.Storage, generation int64) error {
	ledger, err := loadReceiptLedger(store, generation)
	if err != nil {
		return err
	}
	overlapLedger, err := loadOverlapReceiptLedger(store, generation)
	if err != nil {
		return err
	}
	p.receiptMu.Lock()
	p.receipts = ledger
	p.overlapReceipts = overlapLedger
	p.receiptGeneration = generation
	p.receiptMu.Unlock()
	return nil
}

func (p *Plugin) cursorLedger(store hosted.Storage, state pollState) (*cursorLedger, error) {
	p.cursorMu.Lock()
	defer p.cursorMu.Unlock()
	if p.cursors != nil && p.cursorGeneration == state.Generation && p.cursorCycle == state.CursorCycle {
		return p.cursors, nil
	}
	ledger, err := loadCursorLedger(store, state)
	if err != nil {
		return nil, err
	}
	p.cursors = ledger
	p.cursorGeneration = state.Generation
	p.cursorCycle = state.CursorCycle
	return ledger, nil
}

func (p *Plugin) clearCursorLedger() {
	p.cursorMu.Lock()
	p.cursors = nil
	p.cursorGeneration = 0
	p.cursorCycle = 0
	p.cursorMu.Unlock()
}

func normalizeRecord(raw json.RawMessage) ([]byte, string, time.Time, error) {
	if !json.Valid(raw) {
		return nil, "", time.Time{}, errors.New("record is not valid JSON")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, "", time.Time{}, errors.New("record is not a JSON object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, "", time.Time{}, err
	}
	identity := recordIdentity(object, compact.Bytes())
	timestamp, err := recordTimestamp(object)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return compact.Bytes(), identity, timestamp, nil
}

func recordIdentity(object map[string]json.RawMessage, compact []byte) string {
	if raw := object["id"]; len(raw) != 0 {
		var text string
		if json.Unmarshal(raw, &text) == nil && text != "" {
			return "id:" + text
		}
		var number json.Number
		if json.Unmarshal(raw, &number) == nil && number.String() != "" {
			return "id:" + number.String()
		}
	}
	digest := sha256.Sum256(compact)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func recordTimestamp(object map[string]json.RawMessage) (time.Time, error) {
	raw := object["date_create"]
	if len(raw) == 0 {
		return time.Time{}, errors.New("record is missing date_create")
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		seconds, err := number.Int64()
		if err == nil && seconds >= 0 {
			return time.Unix(seconds, 0).UTC(), nil
		}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if seconds, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err == nil && seconds >= 0 {
			return time.Unix(seconds, 0).UTC(), nil
		}
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("record has invalid date_create %s", raw)
}
