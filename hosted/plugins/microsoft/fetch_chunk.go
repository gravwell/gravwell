/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	fetchContinuationVersion = 1
	maximumDiagnostics       = 16
	maximumHuntingPartitions = 4096
	// maximumContinuationPages bounds how many source pages one window may read
	// in total, across every pass that resumes its cursor. defaultMaxPages
	// bounds a single Handle and is re-armed by the next one, and
	// maximumContinuationHistory only remembers the newest cursors, so a source
	// that cycles through more distinct URLs or skip tokens than the history
	// holds was never refused by either guard.
	//
	// 4096 pages is forty-one passes of defaultMaxPages and, at
	// defaultPageSize, four times the records one chunk may retain, so a window
	// that reaches it has long since stopped looking like an ordinary busy
	// interval. The window that reaches it is closed against its own end with
	// the loss stated, because resuming it would read the same budget again on
	// every pass and restarting it would re-deliver the same pages just as
	// often.
	//
	// Advanced Hunting charges one page per query, so a snapshot that has to
	// plan and deliver more than roughly two thousand hash partitions -- tens
	// of millions of records in one query -- reaches this bound as well, and is
	// reported the same way rather than silently truncated.
	maximumContinuationPages   = 4096
	maximumContinuationHistory = 128
	// maximumParentChildPages bounds how many child pages one parent of a
	// parent-child fan-out may supply, cumulatively across every pass that
	// resumes the same cursor. The cumulative window budget above counts
	// parent-list pages only, because charging it per child made the fan-out
	// itself the thing that exhausted it: defender-users spends at least one
	// page per device, so a tenant with more devices than the window budget
	// closed the window partway down the device list on every pass and never
	// reached the rest of it.
	//
	// That leaves one parent able to publish an endless sequence of distinct
	// child cursors, which the per-Handle page bound and the bounded child
	// history both re-arm against. This counter is what refuses it. It is
	// persisted with the cursor, reset exactly when the fan-out advances to
	// another parent, and a cursor written before the field existed decodes as
	// zero -- the same value a parent starts on.
	//
	// 1024 pages is ten passes of defaultMaxPages spent on a single parent and,
	// at defaultPageSize, roughly a hundred thousand child records under one
	// parent, which no Defender machine's logon-user list or any other
	// parent-child selector in the catalog approaches. The parent that reaches
	// it is truncated and reported, and the fan-out continues at the next
	// parent: the loss is bounded to that parent's child list rather than to
	// the window, so the parents behind it and the parent-list cursor survive.
	maximumParentChildPages = 1024
	// maximumChunkRecordBytes and maximumChunkRecords bound what one logical
	// poll may retain before the caller delivers it. defaultMaxPages bounds how
	// many pages a chunk may read and maximumAPIResponseBody bounds one
	// response, but neither bounds the sum: a page loop could retain
	// defaultMaxPages x maximumAPIResponseBody of compacted records, and
	// prepareRecords holds a second copy of all of it, before the first entry is
	// written. Endpoints that choose their own page size -- Azure Activity Logs,
	// Resource Graph, and every ServerPageSize selector -- reach that on an
	// ordinary busy tenant, not just on a hostile response.
	//
	// The budget is checked between pages, so a chunk always makes at least one
	// page of progress and overshoots by at most one response. Reaching it
	// yields the same durable continuation the page loops already return at the
	// request budget, so the next immediate pass resumes at the same page and
	// nothing is skipped or delivered twice. The record count bounds the same
	// retention for pages of many tiny records, where the per-record delivery
	// and receipt bookkeeping dominates the payload itself.
	maximumChunkRecordBytes = 32 << 20
	maximumChunkRecords     = 100000
	// maximumEnvelopePageRecords bounds how many elements one page of an
	// envelope may contribute. maximumAPIResponseBody bounds a page in bytes and
	// the retention budget bounds a chunk between pages, but neither bounds the
	// element count inside one page, and the budget was only ever consulted
	// after a page had been decoded whole. A byte-bounded page can still be
	// element-dense: `[{},{},...]` reaches roughly five million elements inside
	// the 16 MiB body bound, and decoding all of them into a []json.RawMessage
	// before the record budget is next consulted costs hundreds of megabytes of
	// slice headers, FetchedRecord values and synthetic identity digests on a
	// response the byte bound considers ordinary.
	//
	// Pages are streamed element by element instead, and the count is charged
	// before the element is decoded or retained. A page that crosses the bound is
	// rejected whole with its cursor intact, so nothing is skipped and nothing is
	// delivered twice; the next pass re-reads the same page.
	//
	// 50000 is roughly fifty times the largest page any Microsoft endpoint
	// publishes -- Graph caps $top at 999 and the ARM, Resource Graph and Fabric
	// pages this plugin reads are of the same order -- and half the per-chunk
	// record budget, so one page can never overshoot that budget by more than
	// half of it.
	maximumEnvelopePageRecords = 50000
	// The bounds below size the persisted cursor document itself. Every other
	// bound in this file limits what one pass may read or retain in memory;
	// none of them limited what the cursor carries from one pass to the next,
	// and the cursor is the only thing here that is written to durable storage
	// and re-read, re-validated and re-marshalled on every poll for as long as
	// the window stays open.
	//
	// Nothing bounded a continuation URL, a skip token, a parent identifier,
	// the parent list, or the encoded document that holds all of them. One
	// 16 MiB response could therefore be converted into a much larger cursor:
	// the parent list accumulates across pages, every identifier is stored a
	// second time JSON-escaped (up to six bytes per byte for a control
	// character) and a third time percent-escaped into the child URL path (up
	// to three), and the whole document is rewritten and fsynced once per page.
	// Each bound below is enforced where a source value is accepted and again
	// where a persisted cursor is loaded, because a document on disk may
	// predate the bound or have been written by something other than this
	// build.
	//
	// maximumContinuationURLLength bounds one continuation URL, and with it
	// every URL this plugin dispatches on the data path. Graph @odata.nextLink,
	// ARM nextLink, Fabric continuationUri and the Defender parent/child URLs
	// are a few hundred bytes; the longest are Graph links carrying a
	// $skiptoken, which stay under 2048. 8192 is four times the conservative
	// interoperable URL ceiling, so it accepts every form the services publish
	// with room for a child path that percent-escapes a full-length parent
	// identifier into it.
	maximumContinuationURLLength = 8 << 10
	// maximumContinuationSkipTokenLength bounds one Resource Graph skip token.
	// Unlike a nextLink the token is published in the response body and is not
	// a URL, so it is bounded on its own: it is both re-sent in every paged
	// request body and persisted in the cursor. The tokens Azure Resource Graph
	// returns are opaque base64 of a few hundred bytes to a couple of KB, so
	// 16 KiB is several times the largest observed form.
	maximumContinuationSkipTokenLength = 16 << 10
	// maximumContinuationParentIDLength bounds one parent identifier of a
	// parent-child fan-out. A parent identifier comes from the identity field
	// of a parent record, so it is an arbitrary JSON string; the ones the
	// catalog's fan-outs actually read are Defender machine IDs (40-character
	// hex) and Entra object IDs (36-character GUIDs). 512 bytes is an order of
	// magnitude above that and matches the bound the Management path already
	// accepts a contentId under.
	maximumContinuationParentIDLength = 512
	// maximumContinuationParents bounds how many parents one cursor may carry.
	// The list is appended to by every parent-list page of the window, and the
	// cumulative window page bound allows 4096 of those pages at up to
	// maximumEnvelopePageRecords elements each, so nothing capped the list.
	// 100000 matches maximumChunkRecords, the per-chunk record ceiling this
	// plugin already treats as the largest plausible single-window population,
	// and is ten times the largest page the Defender machines API serves.
	maximumContinuationParents = 100000
	// maximumContinuationParentBytes bounds what the parent list costs to
	// persist. The count bound above limits how many parents there are but not
	// how large they are, and at maximumContinuationParentIDLength the count
	// alone would admit roughly 51 MB of parent JSON. Each identifier is
	// charged the size of its persisted JSON form, so this bounds the
	// document's parent array directly rather than the pre-escaping bytes.
	//
	// At the ~43 encoded bytes a Defender machine ID costs, the full count
	// bound is about 4.3 MB, so the count is what binds for the shapes the
	// source really publishes and 8 MiB leaves better than 80% headroom; the
	// byte bound binds only once identifiers grow past roughly 84 bytes each.
	maximumContinuationParentBytes = 8 << 20
	// maximumContinuationHistoryBytes bounds what one stored continuation
	// history costs to retain. The count bound the stored histories are read
	// under is maximumContinuationPages -- see storedContinuationHistory for why
	// it is that and not the production count -- which says how many entries
	// there may be and nothing about how large they are, so a document inside
	// the cursor ceiling could spend the whole of it on one history.
	//
	// An entry is either a sha256 fingerprint this build wrote (71 bytes) or a
	// raw continuation value an older one did, and validateSize bounds either
	// form at maximumContinuationURLLength, so the production count at the
	// longest admissible entry is what this budget is. It admits every shape any
	// build wrote with room to spare -- the full page-bound count of
	// fingerprints is under 300 KiB, and the history this build persists is 128
	// of them at 9 KiB -- and holds all three histories together to a few MiB of
	// the 12 MiB document ceiling instead of to the ceiling itself.
	maximumContinuationHistoryBytes = maximumContinuationHistory * (maximumContinuationURLLength + len(`"",`))
	// maximumStoredHuntingWindowBytes is what one stored planned window may cost.
	// A FetchWindow is two RFC3339 timestamps: 60 bytes as this build marshals
	// one, and under 90 with nanoseconds and a numeric offset. 256 leaves room
	// for whitespace and for members this build does not know.
	maximumStoredHuntingWindowBytes = 256
	// maximumContinuationHuntingWindowBytes bounds what the stored hunting window
	// plan costs to retain, the way the history budget bounds a history: the
	// count bound validate applies to the plan says how many windows it holds
	// and not how large they are.
	maximumContinuationHuntingWindowBytes = maximumHuntingPartitions * maximumStoredHuntingWindowBytes
	// maximumCursorDocumentBytes is the hard ceiling on the encoded cursor
	// document, checked before it is persisted and again when it is loaded. It
	// is the backstop that makes the per-field bounds above add up to a
	// statement about the document rather than about its parts.
	//
	// A document this build writes stays far below it: at most
	// maximumContinuationParentBytes of parent array (8 MiB), three
	// continuation URLs and a skip token (40 KiB raw, under 240 KiB even if
	// every byte JSON-escapes), three continuation histories of at most
	// maximumContinuationHistory sha256 fingerprints each (27 KiB), and at most
	// maximumHuntingPartitions planned windows (250 KiB) -- roughly 8.6 MiB in
	// the worst case and a few hundred bytes in the ordinary one. 12 MiB
	// accepts all of that and still stays below the 16 MiB bound on the single
	// response that is supposed to be the largest thing this plugin handles.
	//
	// Crossing it when loading routes the document into the existing
	// reset/recovery path, which rewrites the cursor key and restarts the
	// window from the durable watermark without dispatching a request.
	// Crossing it when persisting fails the poll with the previous checkpoint
	// intact, so an oversized cursor is never written and no acknowledged
	// record is skipped.
	maximumCursorDocumentBytes = 12 << 20
)

// FetchContinuation is the durable source cursor for one incomplete logical
// poll. It contains no credentials or response bodies.
type FetchContinuation struct {
	Version int  `json:"version"`
	Kind    Kind `json:"kind"`

	NextURL   string   `json:"next_url,omitempty"`
	SkipToken string   `json:"skip_token,omitempty"`
	Recent    []string `json:"recent,omitempty"`

	// Pages counts the source pages this window has read against its cumulative
	// budget, across every pass that resumed this cursor. defaultMaxPages bounds
	// one Handle and Recent remembers the newest maximumContinuationHistory
	// cursors, so a source that cycles through more distinct URLs or skip tokens
	// than the history holds published a cursor the plugin had never seen on
	// every page and the window never ended. A cursor written before this field
	// existed decodes as zero, which is the same default a window starts with.
	//
	// A parent-child fan-out charges only its parent-list pages here; the child
	// pages of one parent are bounded separately by ChildPages, because the
	// fan-out width is the ordinary size of the tenant rather than a sign of a
	// source that will not terminate.
	Pages int `json:"pages,omitempty"`

	FabricCursor time.Time `json:"fabric_cursor,omitempty"`
	FabricDayEnd time.Time `json:"fabric_day_end,omitempty"`

	ParentNextURL string   `json:"parent_next_url,omitempty"`
	ParentIDs     []string `json:"parent_ids,omitempty"`
	ParentIndex   int      `json:"parent_index,omitempty"`
	ChildNextURL  string   `json:"child_next_url,omitempty"`
	ParentRecent  []string `json:"parent_recent,omitempty"`
	ChildRecent   []string `json:"child_recent,omitempty"`
	ChildParentID string   `json:"child_parent_id,omitempty"`

	// ChildPages counts the child pages read for the parent named by
	// ChildParentID, cumulatively across every pass that resumed this cursor. It
	// is reset exactly when the fan-out advances to another parent, so it bounds
	// one parent rather than the whole window. A cursor written before this
	// field existed decodes as zero, which only grants that parent a fresh
	// budget.
	ChildPages int `json:"child_pages,omitempty"`

	HuntingWindows    []FetchWindow `json:"hunting_windows,omitempty"`
	HuntingPartitions int           `json:"hunting_partitions,omitempty"`
	HuntingPartition  int           `json:"hunting_partition,omitempty"`
	HuntingPlanning   bool          `json:"hunting_planning,omitempty"`

	// parentBytes accounts for what ParentIDs costs to persist, so the
	// parent-list loop can refuse an append that would cross
	// maximumContinuationParentBytes without re-measuring the whole list once
	// per listed parent. It is never serialized -- the budget is a property of
	// the list, not a durable field -- and is recomputed from the decoded list
	// whenever a stored cursor is resumed.
	parentBytes int
}

type FetchWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type RecordDiagnostic struct {
	Page    int    `json:"page"`
	Ordinal int    `json:"ordinal"`
	Reason  string `json:"reason"`
}

type FetchChunk struct {
	Records      []FetchedRecord
	Continuation *FetchContinuation
	Complete     bool
	Malformed    int
	Diagnostics  []RecordDiagnostic

	// PageBudgetExhausted reports that the cumulative per-window page bound is
	// what stopped this chunk. The records it did read are still deliverable;
	// the caller publishes the bounded truncation and closes the window rather
	// than resuming a cursor no later pass could finish. It describes this
	// chunk rather than the cursor, so it is never persisted.
	PageBudgetExhausted bool

	// ChildBudgetExhausted counts the parents of a parent-child fan-out whose
	// per-parent child-page bound this chunk reached. The remainder of each of
	// those parents' child lists is permanently skipped for this window, and
	// the loss stops at the parent: the fan-out advances to the next one, the
	// parent-list cursor is kept, and the window still completes. The caller
	// publishes the truncation without closing the window. Like the field
	// above it describes this chunk rather than the cursor, so it is never
	// persisted.
	ChildBudgetExhausted int

	// TruncatedParents names the first maximumDiagnostics parents counted by
	// ChildBudgetExhausted, so the report says which parent was truncated
	// rather than only how many were.
	TruncatedParents []string

	// retained accounts for the decoded payload this chunk already holds. It is
	// retention accounting rather than cursor state, so it is never persisted.
	retained int
}

// withinRetentionBudget reports whether chunk may read another page. It is a
// loop guard beside defaultMaxPages and canFetchAnotherPage, not a failure:
// every caller that stops on it returns the durable continuation it would have
// returned at the request budget.
func (c *Client) withinRetentionBudget(chunk *FetchChunk) bool {
	maximumBytes, maximumRecords := c.retentionBudget()
	return chunk.retained < maximumBytes && len(chunk.Records) < maximumRecords
}

// retentionBudget resolves the effective per-chunk retention budget. A zero or
// negative override falls back to the production bound rather than disabling
// the guard.
func (c *Client) retentionBudget() (int, int) {
	maximumBytes, maximumRecords := c.chunkRetentionBytes, c.chunkRetentionRecords
	if maximumBytes <= 0 {
		maximumBytes = maximumChunkRecordBytes
	}
	if maximumRecords <= 0 {
		maximumRecords = maximumChunkRecords
	}
	return maximumBytes, maximumRecords
}

// withinContinuationPageBudget reports whether this window may read another
// source page. defaultMaxPages bounds one Handle and is re-armed by every pass;
// this bound is cumulative, lives with the cursor, and is what makes a window
// terminate against a source that never stops publishing fresh cursors.
func (c *Client) withinContinuationPageBudget(cursor *FetchContinuation) bool {
	return cursor != nil && cursor.Pages < c.continuationPageBound()
}

// continuationPageBound resolves the effective cumulative page bound. A zero or
// negative override falls back to the production bound rather than disabling
// the guard.
func (c *Client) continuationPageBound() int {
	if c.continuationPageLimit > 0 {
		return c.continuationPageLimit
	}
	return maximumContinuationPages
}

// withinChildPageBudget reports whether the parent a parent-child fan-out is
// currently reading may supply another child page. The cumulative window budget
// counts parent-list pages, so this is the bound that makes one parent
// terminate against a source that never stops publishing fresh child cursors.
func (c *Client) withinChildPageBudget(cursor *FetchContinuation) bool {
	return cursor != nil && cursor.ChildPages < c.parentChildPageBound()
}

// parentChildPageBound resolves the effective per-parent child-page bound. A
// zero or negative override falls back to the production bound rather than
// disabling the guard.
func (c *Client) parentChildPageBound() int {
	if c.parentChildPageLimit > 0 {
		return c.parentChildPageLimit
	}
	return maximumParentChildPages
}

// boundedContinuation attaches cursor to a chunk a cumulative bound stopped,
// which the caller publishes as bounded permanent loss for that window. It is
// the explicit form of what yieldContinuation infers, used where the loop knows
// which bound it hit rather than re-deriving it from the window page count.
func boundedContinuation(chunk FetchChunk, cursor *FetchContinuation) FetchChunk {
	chunk.Continuation = cursor
	chunk.PageBudgetExhausted = true
	return chunk
}

// yieldContinuation attaches cursor to an incomplete chunk and reports whether
// the cumulative page bound is what stopped the page loop. It is used only
// where a loop ran to its own guards: a pacing or budget yield inside the loop
// resumes normally and must not be reported as a truncated window.
func (c *Client) yieldContinuation(chunk FetchChunk, cursor *FetchContinuation) FetchChunk {
	chunk.Continuation = cursor
	chunk.PageBudgetExhausted = !c.withinContinuationPageBudget(cursor)
	return chunk
}

// pageRecordBound resolves the effective per-page element bound.
func (c *Client) pageRecordBound() int {
	if c.pageRetentionRecords > 0 {
		return c.pageRetentionRecords
	}
	return maximumEnvelopePageRecords
}

// admitPageElement charges one streamed element against the per-page element
// bound and the per-chunk retention ceiling before the element is decoded or
// retained. ordinal is one-based, so a page may hold exactly the bound and the
// element that would exceed it is never retained.
//
// The per-chunk budget itself stays a between-pages loop guard that may be
// overshot by one page -- that is what lets a chunk always make a page of
// forward progress -- and these are the ceilings that overshoot may not cross.
// Crossing one rejects the page rather than truncating it: the caller discards
// the chunk and leaves its cursor where it was, so the next pass re-reads the
// same page and no record is dropped or delivered twice.
func (c *Client) admitPageElement(chunk *FetchChunk, dataset Dataset, ordinal, size int) error {
	pageRecords := c.pageRecordBound()
	if ordinal > pageRecords {
		return fmt.Errorf("%s page exceeds the %d-element page bound; checkpoint retained", dataset.Name, pageRecords)
	}
	maximumBytes, maximumRecords := c.retentionBudget()
	if ceiling := maximumBytes + maximumAPIResponseBody; chunk.retained+size > ceiling {
		return fmt.Errorf("%s chunk exceeds its %d-byte retention ceiling; checkpoint retained", dataset.Name, ceiling)
	}
	if ceiling := maximumRecords + pageRecords; len(chunk.Records) >= ceiling {
		return fmt.Errorf("%s chunk exceeds its %d-record retention ceiling; checkpoint retained", dataset.Name, ceiling)
	}
	return nil
}

// errStreamElementsComplete lets a visitor stop a streamed array early once it
// already holds every element its caller can act on. streamEnvelopeObject
// absorbs it and reports success, so it never reaches the fetch paths. Stopping
// early also stops the trailing-data check, which is the point: the remainder
// of an array the caller has already decided about is work it does not have to
// do.
var errStreamElementsComplete = errors.New("streamed array reached the element bound its caller probes for")

// streamEnvelopeObject decodes one top-level JSON object from body without
// materializing its arrays. The member named array is handed to visit element
// by element, the members named in captures are retained as raw values in the
// same pass, and every other member is skipped. The returned bool reports
// whether array was present with a value other than null.
//
// A non-object document, a non-array under array, a non-string member name, a
// repeated array or captured member, and trailing data after the object are all
// rejected, which is what unmarshalling the body into a fixed envelope struct
// did before pages were streamed. The repeated-member rejection is new and
// deliberate: last-wins would otherwise let one response carry two value arrays
// whose elements both reached the record loop.
func streamEnvelopeObject(body []byte, array string, captures map[string]*json.RawMessage, visit func(json.RawMessage) error) (bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false, errors.New("response is not a JSON object")
	}
	found := false
	seen := make(map[string]struct{}, len(captures)+1)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return found, err
		}
		name, ok := token.(string)
		if !ok {
			return found, errors.New("response contains an invalid member name")
		}
		capture, captured := captures[name]
		// seen answers the repeat question for the array and the captured
		// metadata members and for nothing else, so only those names are stored.
		// Recording every distinct top-level name instead grew one map entry per
		// member the default branch does nothing but discard, and the response
		// bound admits millions of short unique names in one 16 MiB body: a map
		// proportional to the response, built out of names no later iteration
		// could ever consult. Tracking only the names the check reads keeps the
		// irrelevant members constant-space and leaves duplicate rejection for
		// the array and the captured members exactly as it was, including the
		// explicit-null array that continues before the switch is done with it.
		if captured || name == array {
			if _, repeated := seen[name]; repeated {
				return found, fmt.Errorf("response repeats the %s member", name)
			}
			seen[name] = struct{}{}
		}
		switch {
		case name == array:
			token, err := decoder.Token()
			if err != nil {
				return found, err
			}
			if token == nil {
				// An explicit null array is absent, exactly as it was when the
				// envelope decoded into a nil slice.
				continue
			}
			if token != json.Delim('[') {
				return found, fmt.Errorf("response member %s is not an array", name)
			}
			found = true
			for decoder.More() {
				var element json.RawMessage
				if err := decoder.Decode(&element); err != nil {
					return found, err
				}
				if err := visit(element); err != nil {
					if errors.Is(err, errStreamElementsComplete) {
						return found, nil
					}
					return found, err
				}
			}
			if _, err := decoder.Token(); err != nil {
				return found, err
			}
		case captured:
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				return found, err
			}
			*capture = value
		default:
			var skipped json.RawMessage
			if err := decoder.Decode(&skipped); err != nil {
				return found, err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return found, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return found, errors.New("response contains trailing data")
	}
	return found, nil
}

// envelopeString reads a captured member as a string. An absent or null member
// is empty, and a member of another type is a decode failure, which is what the
// fixed envelope struct did.
func envelopeString(raw json.RawMessage, member string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("response member %s is not a string", member)
	}
	return value, nil
}

// FetchChunk reads bounded work for one logical poll. A non-nil continuation
// is safe to persist and resume after process restart.
func (c *Client) FetchChunk(ctx context.Context, dataset Dataset, start, end time.Time, subscriptionID string, prior *FetchContinuation) (FetchChunk, error) {
	ctx, cancel := withFetchDeadline(ctx, c.fetchDeadline)
	defer cancel()
	ctx = withFetchBudget(ctx)
	cursor, err := continuationFor(dataset, prior)
	if err != nil {
		return FetchChunk{}, err
	}
	switch dataset.Kind {
	case KindODataGET, KindAzureActivity, KindPolicyStates:
		return c.fetchEnvelopeChunk(ctx, dataset, start, end, subscriptionID, cursor)
	case KindObjectGET:
		return c.fetchObjectChunk(ctx, dataset, subscriptionID)
	case KindResourceGraph:
		return c.fetchResourceGraphChunk(ctx, dataset, cursor)
	case KindFabricActivity:
		return c.fetchFabricChunk(ctx, dataset, start, end, cursor)
	case KindAdvancedHunting:
		return c.fetchHuntingChunk(ctx, dataset, start, end, cursor)
	case KindParentChild:
		return c.fetchParentChildChunk(ctx, dataset, start, end, subscriptionID, cursor)
	case KindManagementActivity:
		return FetchChunk{}, errors.New("Management Activity collection uses its durable content workflow")
	default:
		return FetchChunk{}, fmt.Errorf("unsupported dataset kind %q", dataset.Kind)
	}
}

// errIncompatibleContinuation marks a stored cursor this build cannot resume:
// a version or kind change, a structurally inconsistent document, or a cursor
// the source has already served and published again. collect clears the cursor
// and restarts the current window from the durable watermark instead of failing
// on every poll forever, so no source data is skipped.
//
// The repeated-cursor sites -- envelope, Resource Graph, Fabric, and both the
// parent and the child side of a parent-child fan-out -- all refuse before
// dispatch, so the pass that resets a persisted repeat issues no source request
// at all. Wrapping is what routes them: a plain error left the bad cursor on
// disk and failed identically on every later poll, because nothing else in the
// plugin rewrites that key. Each site keeps its own operator-facing sentence
// after the wrap, so the reset warning still names which cursor repeated.
var errIncompatibleContinuation = errors.New("stored Microsoft continuation is incompatible with this dataset")

func continuationFor(dataset Dataset, prior *FetchContinuation) (*FetchContinuation, error) {
	if prior == nil {
		return &FetchContinuation{Version: fetchContinuationVersion, Kind: dataset.Kind}, nil
	}
	if prior.Version != fetchContinuationVersion || prior.Kind != dataset.Kind {
		return nil, errIncompatibleContinuation
	}
	if err := prior.validate(); err != nil {
		return nil, err
	}
	clone := *prior
	clone.Recent = recentTail(prior.Recent)
	clone.ParentIDs = append([]string(nil), prior.ParentIDs...)
	clone.ParentRecent = recentTail(prior.ParentRecent)
	clone.ChildRecent = recentTail(prior.ChildRecent)
	clone.HuntingWindows = append([]FetchWindow(nil), prior.HuntingWindows...)
	// validate has already proved the restored list is inside both parent
	// bounds; this is what lets every later append test the byte budget in
	// constant time.
	clone.parentBytes = continuationParentBytes(clone.ParentIDs)
	return &clone, nil
}

// continuationParentIDBytes charges one parent identifier the size of its
// persisted JSON form plus the separator the array needs to hold it, which is
// what makes the budget a statement about the document rather than about the
// identifier's own bytes. An identifier that cannot be marshalled is charged
// the whole budget so it can never be appended and then fail the persist that
// follows.
func continuationParentIDBytes(id string) int {
	encoded, err := json.Marshal(id)
	if err != nil {
		return maximumContinuationParentBytes
	}
	return len(encoded) + len(",")
}

func continuationParentBytes(ids []string) int {
	total := 0
	for _, id := range ids {
		total += continuationParentIDBytes(id)
	}
	return total
}

// admitParentID charges one listed parent against the per-identifier, count and
// aggregate-byte bounds before it is retained in the cursor.
//
// Crossing any of them rejects the whole chunk with the cursor as it was, which
// is the same contract admitPageElement keeps: the parent-list page is re-read
// by the next pass, nothing oversized is persisted, and no parent is quietly
// dropped from a fan-out whose remaining work the cursor is supposed to
// describe. The error names the selector and the bound and never the vendor
// bytes, because it is reported to the operator through safePollError.
func admitParentID(cursor *FetchContinuation, dataset Dataset, id string) error {
	if len(id) > maximumContinuationParentIDLength {
		return fmt.Errorf("%s parent identifier exceeds the %d-byte bound; checkpoint retained", dataset.Name, maximumContinuationParentIDLength)
	}
	if len(cursor.ParentIDs) >= maximumContinuationParents {
		return fmt.Errorf("%s fan-out exceeds the %d-parent cursor bound; checkpoint retained", dataset.Name, maximumContinuationParents)
	}
	charged := cursor.parentBytes + continuationParentIDBytes(id)
	if charged > maximumContinuationParentBytes {
		return fmt.Errorf("%s parent list exceeds its %d-byte cursor budget; checkpoint retained", dataset.Name, maximumContinuationParentBytes)
	}
	cursor.ParentIDs = append(cursor.ParentIDs, id)
	cursor.parentBytes = charged
	return nil
}

// validate rejects a persisted cursor whose structure no growth path could have
// produced. loadState accepts any well-formed JSON for this document, so these
// bounds are what keep a corrupted cursor from indexing out of range in
// fetchParentChildChunk, stalling the hunting delivery loop against a partition
// target it can never reach, or restarting the Fabric day walk at the zero time.
//
// The size bounds are here for the same reason. Every one of them is also
// enforced where the source value is accepted, but a document already on disk
// was written before the bound existed or by something other than this build,
// and re-validating on load is what keeps an oversized cursor from being
// re-marshalled and re-persisted on every poll. Refusing routes it into the
// reset path with no request dispatched, which restarts the window from the
// durable watermark.
func (f *FetchContinuation) validate() error {
	if err := f.validateSize(); err != nil {
		return err
	}
	if f.ParentIndex < 0 || f.ParentIndex > len(f.ParentIDs) {
		return fmt.Errorf("%w: parent index %d of %d parents", errIncompatibleContinuation, f.ParentIndex, len(f.ParentIDs))
	}
	if f.ChildNextURL != "" && f.ParentIndex >= len(f.ParentIDs) {
		return fmt.Errorf("%w: child continuation has no parent at index %d of %d", errIncompatibleContinuation, f.ParentIndex, len(f.ParentIDs))
	}
	if f.Pages < 0 || f.Pages > maximumContinuationPages {
		return fmt.Errorf("%w: cumulative page count %d", errIncompatibleContinuation, f.Pages)
	}
	if f.ChildPages < 0 || f.ChildPages > maximumParentChildPages {
		return fmt.Errorf("%w: per-parent child page count %d", errIncompatibleContinuation, f.ChildPages)
	}
	if f.HuntingPartitions < 0 || f.HuntingPartitions > maximumHuntingPartitions {
		return fmt.Errorf("%w: hunting partition count %d", errIncompatibleContinuation, f.HuntingPartitions)
	}
	if len(f.HuntingWindows) > maximumHuntingPartitions {
		return fmt.Errorf("%w: hunting window count %d", errIncompatibleContinuation, len(f.HuntingWindows))
	}
	// Both hunting fields are zero on every ordinary non-snapshot cursor, so the
	// partition bound only applies once partitioning has actually been planned.
	if f.HuntingPartition < 0 || (f.HuntingPartitions > 0 && f.HuntingPartition >= f.HuntingPartitions) ||
		(f.HuntingPartitions == 0 && f.HuntingPartition != 0) {
		return fmt.Errorf("%w: hunting partition %d of %d", errIncompatibleContinuation, f.HuntingPartition, f.HuntingPartitions)
	}
	if err := f.validateFabric(); err != nil {
		return err
	}
	return nil
}

// validateSize refuses a persisted cursor larger than the document bounds the
// accept paths enforce. Each message names the field and the bound and carries
// none of the stored bytes: the reason reaches the operator's log through
// resetStoredContinuation.
//
// The continuation histories are bounded here by the length of one entry rather
// than by their count. recentTail already truncates a longer history to the
// newest maximumContinuationHistory entries, which is what the repeat detector
// is specified against and what every later persist writes back, so rejecting
// on count would discard resumable cursors for a condition the state machine
// already repairs. The entry length is the part nothing bounded: an entry is
// either a sha256 fingerprint this build wrote or a raw continuation value an
// older one did, so the URL bound is the right ceiling for both.
func (f *FetchContinuation) validateSize() error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"next URL", f.NextURL},
		{"parent next URL", f.ParentNextURL},
		{"child next URL", f.ChildNextURL},
	} {
		if len(field.value) > maximumContinuationURLLength {
			return fmt.Errorf("%w: %s exceeds %d bytes", errIncompatibleContinuation, field.name, maximumContinuationURLLength)
		}
	}
	if len(f.SkipToken) > maximumContinuationSkipTokenLength {
		return fmt.Errorf("%w: skip token exceeds %d bytes", errIncompatibleContinuation, maximumContinuationSkipTokenLength)
	}
	if len(f.ChildParentID) > maximumContinuationParentIDLength {
		return fmt.Errorf("%w: child parent identifier exceeds %d bytes", errIncompatibleContinuation, maximumContinuationParentIDLength)
	}
	if len(f.ParentIDs) > maximumContinuationParents {
		return fmt.Errorf("%w: parent count %d exceeds %d", errIncompatibleContinuation, len(f.ParentIDs), maximumContinuationParents)
	}
	parentBytes := 0
	for _, id := range f.ParentIDs {
		if len(id) > maximumContinuationParentIDLength {
			return fmt.Errorf("%w: parent identifier exceeds %d bytes", errIncompatibleContinuation, maximumContinuationParentIDLength)
		}
		parentBytes += continuationParentIDBytes(id)
	}
	if parentBytes > maximumContinuationParentBytes {
		return fmt.Errorf("%w: parent list costs %d bytes to persist, over the %d-byte budget",
			errIncompatibleContinuation, parentBytes, maximumContinuationParentBytes)
	}
	for _, history := range [][]string{f.Recent, f.ParentRecent, f.ChildRecent} {
		for _, entry := range history {
			if len(entry) > maximumContinuationURLLength {
				return fmt.Errorf("%w: continuation history entry exceeds %d bytes", errIncompatibleContinuation, maximumContinuationURLLength)
			}
		}
	}
	return nil
}

// storedCursor is the stored cursor document, read with every untrusted array
// inside it streamed under its own bounds.
//
// loadState decoded the document straight into cursorState, so json.Unmarshal
// materialized Recent, ParentIDs, ParentRecent, ChildRecent and HuntingWindows
// whole -- one retained element per member, plus every byte behind it -- before
// FetchContinuation.validate was reached, and stateHasPending reached that same
// decode without validating at all.
//
// maximumCursorDocumentBytes bounds the bytes that are read and says nothing
// about what retaining them costs. A member of a dense array is two or three
// bytes while the element it decodes to is a 16-byte string header or a 48-byte
// window, so a document inside the 12 MiB ceiling carried millions of members
// and the decode allocated an order of magnitude more elements than any of
// these bounds admits -- hundreds of megabytes of them -- before the bound that
// then refused the whole document was consulted (CWE-770).
//
// Every bound enforced here is one the accept path and validate already
// enforce, moved in front of the retention, so a cursor any build of this plugin
// wrote is accepted exactly as it was before and the reasons an operator sees
// still name the field and the bound and never a stored byte. A document that
// crosses one is answered the way an undecodable cursor already is: loadState reports
// errIncompatibleContinuation alongside the watermark it did read, collect
// replaces the cursor key, and the window restarts from that watermark with no
// request dispatched and no token acquired.
type storedCursor struct {
	WindowStart  time.Time                `json:"window_start,omitempty"`
	WindowEnd    time.Time                `json:"window_end,omitempty"`
	Continuation storedContinuationMember `json:"continuation,omitempty"`
	Fetched      int                      `json:"fetched,omitempty"`
}

// storedContinuationMember rejects a repeated top-level continuation before a
// second storedContinuation can receive fresh nested array budgets. A null
// member remains equivalent to an absent continuation.
type storedContinuationMember struct {
	value *storedContinuation
	seen  bool
}

func (member *storedContinuationMember) UnmarshalJSON(raw []byte) error {
	if member.seen {
		return errors.New("stored cursor repeats bounded member continuation")
	}
	member.seen = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var continuation storedContinuation
	if err := json.Unmarshal(raw, &continuation); err != nil {
		return err
	}
	member.value = &continuation
	return nil
}

// storedContinuation shadows every array member of FetchContinuation with a
// bounded streaming decoder. The embedded FetchContinuation declares the same
// members at a greater depth, so encoding/json resolves each of those names to
// the bounded field here and leaves the embedded slice nil; every other cursor
// field decodes into the embedded struct exactly as before, an unknown member
// is still ignored, a member of the wrong JSON type is still a type error, and
// the document's own trailing data is still refused.
type storedContinuation struct {
	FetchContinuation
	Recent         storedContinuationHistory `json:"recent,omitempty"`
	ParentIDs      storedContinuationParents `json:"parent_ids,omitempty"`
	ParentRecent   storedContinuationHistory `json:"parent_recent,omitempty"`
	ChildRecent    storedContinuationHistory `json:"child_recent,omitempty"`
	HuntingWindows storedContinuationWindows `json:"hunting_windows,omitempty"`
}

func (stored *storedContinuation) UnmarshalJSON(raw []byte) error {
	type plain storedContinuation
	decoded := plain{
		Recent:         storedContinuationHistory{member: "recent"},
		ParentIDs:      storedContinuationParents{},
		ParentRecent:   storedContinuationHistory{member: "parent_recent"},
		ChildRecent:    storedContinuationHistory{member: "child_recent"},
		HuntingWindows: storedContinuationWindows{},
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*stored = storedContinuation(decoded)
	return nil
}

// continuation assembles the decoded cursor. The bounded arrays are adopted
// into the cursor the rest of the plugin reads, so nothing downstream of the
// load has to know the bounded types exist.
func (stored *storedContinuation) continuation() *FetchContinuation {
	decoded := stored.FetchContinuation
	decoded.Recent = stored.Recent.values
	decoded.ParentIDs = stored.ParentIDs.values
	decoded.ParentRecent = stored.ParentRecent.values
	decoded.ChildRecent = stored.ChildRecent.values
	decoded.HuntingWindows = stored.HuntingWindows.values
	return &decoded
}

// storedContinuationHistory streams one stored continuation history.
//
// The count bound is deliberately not maximumContinuationHistory, for the
// reason validateSize already declines to refuse a longer history on count:
// recentTail truncates one to the newest maximumContinuationHistory entries,
// which is what the repeat detector is specified against and what every later
// persist writes back, so refusing on the production count would discard
// resumable cursors for a condition the state machine already repairs -- and
// would do it to the cursor of a window that is mid-flight.
//
// maximumContinuationPages is the widest count any build could have persisted:
// a window remembers at most one continuation per page it reads, and that is
// the cumulative bound on its pages. So nothing any build wrote is refused
// here, while what the decode retains stops being a function of how many
// members the document holds.
//
// The production count still applies where it always did and before the history
// is used for anything: continuationFor trims what this returns to the newest
// maximumContinuationHistory entries, and that is the history the repeat
// detector reads and the next persist writes back.
type storedContinuationHistory struct {
	values []string
	seen   bool
	member string
}

func (history *storedContinuationHistory) UnmarshalJSON(raw []byte) error {
	if history.seen {
		return fmt.Errorf("stored continuation repeats bounded member %s", history.member)
	}
	history.seen = true
	decoded, err := decodeBoundedContinuationArray[string](raw, "continuation history",
		maximumContinuationPages, maximumContinuationHistoryBytes, storedContinuationElementBytes[string], nil)
	if err != nil {
		return err
	}
	history.values = decoded
	return nil
}

// storedContinuationParents streams the stored parent list of a parent-child
// fan-out under exactly the three bounds admitParentID charges a listed parent
// against and validateSize re-checks on load: the per-identifier length, the
// count, and the aggregate cost of persisting the list.
type storedContinuationParents struct {
	values []string
	seen   bool
}

func (parents *storedContinuationParents) UnmarshalJSON(raw []byte) error {
	if parents.seen {
		return errors.New("stored continuation repeats bounded member parent_ids")
	}
	parents.seen = true
	decoded, err := decodeBoundedContinuationArray[string](raw, "parent list",
		maximumContinuationParents, maximumContinuationParentBytes,
		func(id string, _ int) int { return continuationParentIDBytes(id) },
		func(id string) error {
			if len(id) > maximumContinuationParentIDLength {
				return fmt.Errorf("stored parent identifier exceeds %d bytes", maximumContinuationParentIDLength)
			}
			return nil
		})
	if err != nil {
		return err
	}
	parents.values = decoded
	return nil
}

// storedContinuationWindows streams the stored hunting window plan under the
// count bound validate applies to it and under what that many planned windows
// may cost to retain.
type storedContinuationWindows struct {
	values []FetchWindow
	seen   bool
}

func (windows *storedContinuationWindows) UnmarshalJSON(raw []byte) error {
	if windows.seen {
		return errors.New("stored continuation repeats bounded member hunting_windows")
	}
	windows.seen = true
	decoded, err := decodeBoundedContinuationArray[FetchWindow](raw, "hunting window plan",
		maximumHuntingPartitions, maximumContinuationHuntingWindowBytes,
		storedContinuationElementBytes[FetchWindow], nil)
	if err != nil {
		return err
	}
	windows.values = decoded
	return nil
}

// storedContinuationElementBytes charges one element the bytes it occupies in
// the document, which is both exact and free: the decoder's own input offset
// measures it, and it is the honest number for a member padded with whitespace
// or carrying members this build discards.
func storedContinuationElementBytes[T any](_ T, stored int) int { return stored }

// decodeBoundedContinuationArray decodes one stored cursor array member,
// charging and bounding every element before it is retained.
//
// raw is exactly one JSON value -- encoding/json hands an Unmarshaler the bytes
// of the member it is decoding, as a window into the document rather than a
// copy -- so there is nothing after the array to accept or refuse here, and the
// document's own trailing data is still refused by the json.Unmarshal that
// called this. A null member is the absent member, which is what decoding into
// a slice already left, so a cursor that writes null instead of omitting an
// array keeps loading.
//
// charge is what one element costs the aggregate budget. The histories and the
// window plan are charged the bytes they occupy in the document, measured from
// the decoder's input offset; the parent list is charged the re-encoded form
// instead, because that is what admitParentID charged when it grew the list and
// what validateSize re-checks, so a cursor this build wrote is charged here
// exactly what it was charged then. check is the per-element bound the accept
// path applies, which is what keeps one oversized element out of a list whose
// aggregate still has room for it.
func decodeBoundedContinuationArray[T any](raw []byte, field string, maximum, maximumBytes int,
	charge func(element T, stored int) int, check func(element T) error) ([]T, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, nil
	}
	if token != json.Delim('[') {
		return nil, fmt.Errorf("stored %s must be a JSON array", field)
	}
	offset, total := decoder.InputOffset(), 0
	var decoded []T
	for decoder.More() {
		// The count bound first, before the element is decoded into anything at
		// all. It is the cheap one and it is what bounds the allocation on its
		// own: a dense array of two-byte members is refused at the bound rather
		// than after every one of them has become a retained element.
		if len(decoded) >= maximum {
			return nil, fmt.Errorf("stored %s exceeds the %d-element bound", field, maximum)
		}
		var element T
		if err := decoder.Decode(&element); err != nil {
			return nil, err
		}
		stored := int(decoder.InputOffset() - offset)
		offset = decoder.InputOffset()
		if check != nil {
			if err := check(element); err != nil {
				return nil, err
			}
		}
		if total += charge(element, stored); total > maximumBytes {
			return nil, fmt.Errorf("stored %s costs more than its %d-byte budget", field, maximumBytes)
		}
		decoded = append(decoded, element)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return decoded, nil
}

// validateFabric checks the whole invariant a Fabric cursor is written under.
// fetchFabricChunk reads one UTC day per request and sets NextURL together with
// the exclusive end of that day, advances FabricCursor to that day end when the
// page is terminal, and clears both together. A pending cursor therefore names
// both, in that order, and a cursor between days names neither.
//
// Only the pieces of that were checked. A NextURL with a zero day end satisfied
// every other bound, and the page it named completed by advancing FabricCursor
// to the zero time: the window restarted at year 1, asked the source for a day
// it has never held, and failed with an ordinary vendor error -- which is not
// errIncompatibleContinuation, so collect reported it instead of resetting, and
// no later poll could answer differently because nothing else rewrites that key.
// Refusing the document here routes it to resetStoredContinuation before any
// request is dispatched, which keeps the durable watermark and restarts the
// window from it on the next poll.
//
// A cursor written before FabricDayEnd existed carries no pending page either,
// so it decodes as a between-days cursor and stays resumable.
func (f *FetchContinuation) validateFabric() error {
	if f.Kind != KindFabricActivity {
		return nil
	}
	if f.NextURL == "" {
		if !f.FabricDayEnd.IsZero() {
			return fmt.Errorf("%w: Fabric day end %s without a pending page", errIncompatibleContinuation, f.FabricDayEnd.UTC().Format(time.RFC3339Nano))
		}
		return nil
	}
	if f.FabricCursor.IsZero() || f.FabricDayEnd.IsZero() {
		return fmt.Errorf("%w: pending Fabric page with cursor %s and day end %s", errIncompatibleContinuation,
			f.FabricCursor.UTC().Format(time.RFC3339Nano), f.FabricDayEnd.UTC().Format(time.RFC3339Nano))
	}
	if !f.FabricCursor.Before(f.FabricDayEnd) {
		return fmt.Errorf("%w: Fabric day end %s does not follow its cursor %s", errIncompatibleContinuation,
			f.FabricDayEnd.UTC().Format(time.RFC3339Nano), f.FabricCursor.UTC().Format(time.RFC3339Nano))
	}
	return nil
}

func (c *Client) fetchObjectChunk(ctx context.Context, dataset Dataset, subscriptionID string) (FetchChunk, error) {
	path, err := expandPath(dataset.Path, subscriptionID)
	if err != nil {
		return FetchChunk{}, err
	}
	body, err := c.datasetRequest(ctx, http.MethodGet, dataset, c.conf.Base(dataset.Service)+path, nil)
	if err != nil {
		if errors.Is(err, errFetchPacing) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// One request with no cursor has nothing to resume, so the fetch
			// deadline the page loops turn into a continuation stays the hard
			// deadline failure it has always been here. The next poll reissues
			// the same single request from the same watermark.
			return FetchChunk{}, context.DeadlineExceeded
		}
		return FetchChunk{}, err
	}
	chunk := FetchChunk{Complete: true}
	appendDecodedRecord(&chunk, dataset, json.RawMessage(body), 1, 1)
	return chunk, nil
}

func (c *Client) fetchEnvelopeChunk(ctx context.Context, dataset Dataset, start, end time.Time, subscriptionID string, cursor *FetchContinuation) (FetchChunk, error) {
	if cursor.NextURL == "" {
		first, err := c.envelopeStartURL(dataset, start, end, subscriptionID)
		if err != nil {
			return FetchChunk{}, err
		}
		cursor.NextURL = first
	}
	chunk := FetchChunk{}
	for page := 1; cursor.NextURL != "" && page <= defaultMaxPages && canFetchAnotherPage(ctx) &&
		c.withinRetentionBudget(&chunk) && c.withinContinuationPageBudget(cursor); page++ {
		current := cursor.NextURL
		if continuationSeen(cursor, current) {
			return FetchChunk{}, fmt.Errorf("%w: %s returned a repeated continuation URL", errIncompatibleContinuation, dataset.Name)
		}
		method := http.MethodGet
		var requestBody []byte
		if dataset.Kind == KindPolicyStates {
			method, requestBody = http.MethodPost, []byte("{}")
		}
		body, err := c.datasetRequest(ctx, method, dataset, current, requestBody)
		if err != nil {
			if deferredFetch(err) {
				chunk.Continuation = cursor
				return chunk, nil
			}
			return FetchChunk{}, err
		}
		// The cumulative count is charged for a page that was actually read, not
		// for one that was attempted. A pacing or budget yield persists the cursor
		// unchanged above, so a window that is repeatedly deferred before its
		// first page -- a paced tenant, a short deadline -- no longer spends its
		// window budget on pages no pass ever saw and no longer closes itself
		// against unread data. Charging here rather than before the request keeps
		// the bound exact: the loop header re-reads the count before the next
		// page, so a window still reads at most its bound and is still reported
		// as truncated on the page that reaches it.
		cursor.Pages++
		next, err := c.decodeEnvelopePage(&chunk, dataset, body, current, page)
		if err != nil {
			return FetchChunk{}, err
		}
		if next == "" {
			cursor.NextURL = ""
			chunk.Complete = true
			return chunk, nil
		}
		if err := rememberContinuation(cursor, current); err != nil {
			return FetchChunk{}, fmt.Errorf("%s continuation: %w", dataset.Name, err)
		}
		cursor.NextURL = next
	}
	if cursor.NextURL == "" {
		chunk.Complete = true
		return chunk, nil
	}
	return c.yieldContinuation(chunk, cursor), nil
}

func (c *Client) envelopeStartURL(dataset Dataset, start, end time.Time, subscriptionID string) (string, error) {
	path, err := expandPath(dataset.Path, subscriptionID)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(c.conf.Base(dataset.Service) + path)
	if err != nil {
		return "", err
	}
	query := u.Query()
	switch dataset.Kind {
	case KindODataGET:
		if dataset.Service == ServiceGraph && !dataset.ServerPageSize && query.Get("$top") == "" {
			query.Set("$top", strconv.Itoa(defaultPageSize))
		}
		if dataset.Service == ServiceGraph && dataset.TimeField != "" && dataset.Mode != ModeSnapshot {
			query.Set("$filter", fmt.Sprintf("%s ge %s and %s lt %s", dataset.TimeField, start.UTC().Format(time.RFC3339Nano), dataset.TimeField, end.UTC().Format(time.RFC3339Nano)))
		} else if dataset.Filter != "" {
			query.Set("$filter", dataset.Filter)
		}
	case KindAzureActivity:
		filter := fmt.Sprintf("eventTimestamp ge '%s' and eventTimestamp le '%s'", start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
		query.Set("$filter", filter)
	case KindPolicyStates:
		query.Set("$top", strconv.Itoa(defaultPageSize))
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (c *Client) decodeEnvelopePage(chunk *FetchChunk, dataset Dataset, body []byte, current string, page int) (string, error) {
	return c.decodeEnvelopePageWithIdentityPolicy(chunk, dataset, body, current, page, false)
}

func (c *Client) decodeEnvelopePageRequiringIdentity(chunk *FetchChunk, dataset Dataset, body []byte, current string, page int) (string, error) {
	return c.decodeEnvelopePageWithIdentityPolicy(chunk, dataset, body, current, page, true)
}

func (c *Client) decodeEnvelopePageWithIdentityPolicy(chunk *FetchChunk, dataset Dataset, body []byte, current string, page int, requireIdentity bool) (string, error) {
	var nextLinkRaw, odataNextLinkRaw json.RawMessage
	elements := 0
	found, err := streamEnvelopeObject(body, "value",
		map[string]*json.RawMessage{"nextLink": &nextLinkRaw, "@odata.nextLink": &odataNextLinkRaw},
		func(element json.RawMessage) error {
			elements++
			if err := c.admitPageElement(chunk, dataset, elements, len(element)); err != nil {
				return err
			}
			appendDecodedRecordWithIdentityPolicy(chunk, dataset, element, page, elements, requireIdentity)
			return nil
		})
	if err != nil {
		return "", fmt.Errorf("decode %s page: %w", dataset.Name, err)
	}
	if !found {
		return "", fmt.Errorf("%s response is missing the value array", dataset.Name)
	}
	nextLink, err := envelopeString(nextLinkRaw, "nextLink")
	if err != nil {
		return "", fmt.Errorf("decode %s page: %w", dataset.Name, err)
	}
	odataNextLink, err := envelopeString(odataNextLinkRaw, "@odata.nextLink")
	if err != nil {
		return "", fmt.Errorf("decode %s page: %w", dataset.Name, err)
	}
	rawNext := nextLink
	if rawNext == "" {
		rawNext = odataNextLink
	}
	if rawNext == "" {
		return "", nil
	}
	next, err := c.resolveContinuation(current, rawNext)
	if err != nil {
		return "", fmt.Errorf("%s continuation URL: %w", dataset.Name, err)
	}
	return next, nil
}

func (c *Client) fetchResourceGraphChunk(ctx context.Context, dataset Dataset, cursor *FetchContinuation) (FetchChunk, error) {
	chunk := FetchChunk{}
	endpoint := c.conf.Base(dataset.Service) + dataset.Path
	for page := 1; page <= defaultMaxPages && canFetchAnotherPage(ctx) &&
		c.withinRetentionBudget(&chunk) && c.withinContinuationPageBudget(cursor); page++ {
		options := map[string]any{"$top": defaultPageSize, "$resultFormat": "objectArray"}
		if cursor.SkipToken != "" {
			options["$skipToken"] = cursor.SkipToken
		}
		body, err := json.Marshal(map[string]any{"subscriptions": c.conf.Subscription_ID, "query": dataset.Query, "options": options})
		if err != nil {
			return FetchChunk{}, err
		}
		response, err := c.datasetRequest(ctx, http.MethodPost, dataset, endpoint, body)
		if err != nil {
			if deferredFetch(err) {
				chunk.Continuation = cursor
				return chunk, nil
			}
			return FetchChunk{}, err
		}
		// Charged for a page that was read; see fetchEnvelopeChunk.
		cursor.Pages++
		var skipTokenRaw, dollarSkipTokenRaw, resultTruncated json.RawMessage
		elements := 0
		found, err := streamEnvelopeObject(response, "data", map[string]*json.RawMessage{
			"skipToken": &skipTokenRaw, "$skipToken": &dollarSkipTokenRaw, "resultTruncated": &resultTruncated,
		}, func(element json.RawMessage) error {
			elements++
			if err := c.admitPageElement(&chunk, dataset, elements, len(element)); err != nil {
				return err
			}
			appendDecodedRecord(&chunk, dataset, element, page, elements)
			return nil
		})
		if err != nil {
			return FetchChunk{}, fmt.Errorf("decode Resource Graph page: %w", err)
		}
		if !found {
			return FetchChunk{}, errors.New("Resource Graph response is missing the data array")
		}
		skipToken, err := envelopeString(skipTokenRaw, "skipToken")
		if err != nil {
			return FetchChunk{}, fmt.Errorf("decode Resource Graph page: %w", err)
		}
		dollarSkipToken, err := envelopeString(dollarSkipTokenRaw, "$skipToken")
		if err != nil {
			return FetchChunk{}, fmt.Errorf("decode Resource Graph page: %w", err)
		}
		if skipToken == "" {
			skipToken = dollarSkipToken
		}
		// The token is both re-sent in the next request body and persisted in
		// the cursor, and the response bound alone would admit a 16 MiB one.
		// Refusing it keeps the chunk's cursor exactly as it was, so the next
		// pass re-reads the same page and nothing already acknowledged is
		// skipped.
		if len(skipToken) > maximumContinuationSkipTokenLength {
			return FetchChunk{}, fmt.Errorf("Resource Graph skip token exceeds %d bytes; checkpoint retained", maximumContinuationSkipTokenLength)
		}
		switch string(bytes.TrimSpace(resultTruncated)) {
		case "", "false", `"false"`:
		case "true", `"true"`:
			if skipToken == "" {
				return FetchChunk{}, errors.New("Resource Graph result is truncated without continuation; checkpoint retained")
			}
		default:
			return FetchChunk{}, errors.New("Resource Graph returned an invalid truncation indicator")
		}
		if skipToken == "" {
			chunk.Complete = true
			return chunk, nil
		}
		if continuationSeen(cursor, skipToken) {
			return FetchChunk{}, fmt.Errorf("%w: Resource Graph returned a repeated skip token", errIncompatibleContinuation)
		}
		if err := rememberContinuation(cursor, skipToken); err != nil {
			return FetchChunk{}, fmt.Errorf("Resource Graph continuation: %w", err)
		}
		cursor.SkipToken = skipToken
	}
	return c.yieldContinuation(chunk, cursor), nil
}

func (c *Client) fetchFabricChunk(ctx context.Context, dataset Dataset, start, end time.Time, cursor *FetchContinuation) (FetchChunk, error) {
	if end.Sub(start) > 28*24*time.Hour {
		return FetchChunk{}, errors.New("Fabric activity window exceeds 28 days")
	}
	start = start.UTC().Truncate(time.Millisecond)
	end = end.UTC().Truncate(time.Millisecond)
	if cursor.FabricCursor.IsZero() {
		cursor.FabricCursor = start
	}
	chunk := FetchChunk{}
	for page := 1; cursor.FabricCursor.Before(end) && page <= defaultMaxPages && canFetchAnotherPage(ctx) &&
		c.withinRetentionBudget(&chunk) && c.withinContinuationPageBudget(cursor); page++ {
		if cursor.NextURL == "" {
			dayEndExclusive := time.Date(cursor.FabricCursor.Year(), cursor.FabricCursor.Month(), cursor.FabricCursor.Day()+1, 0, 0, 0, 0, time.UTC)
			if dayEndExclusive.After(end) {
				dayEndExclusive = end
			}
			u, err := url.Parse(c.conf.Base(dataset.Service) + dataset.Path)
			if err != nil {
				return FetchChunk{}, err
			}
			query := u.Query()
			query.Set("startDateTime", "'"+cursor.FabricCursor.Format(time.RFC3339Nano)+"'")
			query.Set("endDateTime", "'"+dayEndExclusive.Add(-time.Millisecond).Format(time.RFC3339Nano)+"'")
			u.RawQuery = query.Encode()
			cursor.NextURL, cursor.FabricDayEnd = u.String(), dayEndExclusive
		}
		current := cursor.NextURL
		if continuationSeen(cursor, current) {
			return FetchChunk{}, fmt.Errorf("%w: Fabric returned a repeated continuation URI", errIncompatibleContinuation)
		}
		body, err := c.datasetRequest(ctx, http.MethodGet, dataset, current, nil)
		if err != nil {
			if deferredFetch(err) {
				chunk.Continuation = cursor
				return chunk, nil
			}
			return FetchChunk{}, err
		}
		// Charged for a page that was read; see fetchEnvelopeChunk. The day URL
		// built above is still persisted by the yield, so a deferred first page
		// resumes at the same day with the window budget untouched.
		cursor.Pages++
		var continuationURIRaw json.RawMessage
		elements := 0
		found, err := streamEnvelopeObject(body, "activityEventEntities",
			map[string]*json.RawMessage{"continuationUri": &continuationURIRaw},
			func(element json.RawMessage) error {
				elements++
				if err := c.admitPageElement(&chunk, dataset, elements, len(element)); err != nil {
					return err
				}
				appendDecodedRecord(&chunk, dataset, element, page, elements)
				return nil
			})
		if err != nil {
			return FetchChunk{}, fmt.Errorf("decode Fabric activity page: %w", err)
		}
		if !found {
			return FetchChunk{}, errors.New("Fabric response is missing the activityEventEntities array")
		}
		continuationURI, err := envelopeString(continuationURIRaw, "continuationUri")
		if err != nil {
			return FetchChunk{}, fmt.Errorf("decode Fabric activity page: %w", err)
		}
		if continuationURI != "" {
			next, err := c.resolveContinuation(current, continuationURI)
			if err != nil {
				return FetchChunk{}, fmt.Errorf("Fabric continuation URI: %w", err)
			}
			if err := rememberContinuation(cursor, current); err != nil {
				return FetchChunk{}, fmt.Errorf("Fabric continuation: %w", err)
			}
			cursor.NextURL = next
		} else {
			cursor.FabricCursor = cursor.FabricDayEnd
			cursor.FabricDayEnd = time.Time{}
			cursor.NextURL = ""
		}
	}
	if !cursor.FabricCursor.Before(end) && cursor.NextURL == "" {
		chunk.Complete = true
		return chunk, nil
	}
	return c.yieldContinuation(chunk, cursor), nil
}

func (c *Client) fetchParentChildChunk(ctx context.Context, dataset Dataset, start, end time.Time, subscriptionID string, cursor *FetchContinuation) (FetchChunk, error) {
	if cursor.ParentNextURL == "" && len(cursor.ParentIDs) == 0 && cursor.ChildNextURL == "" {
		parent := dataset
		parent.Kind, parent.Path, parent.Mode, parent.TimeField = KindODataGET, dataset.ParentPath, ModeSnapshot, ""
		parent.IDField, parent.IDFields = "id", nil
		first, err := c.envelopeStartURL(parent, start, end, subscriptionID)
		if err != nil {
			return FetchChunk{}, err
		}
		cursor.ParentNextURL = first
	}
	chunk := FetchChunk{}
	// The cumulative window budget is charged against parent-list pages only,
	// and is therefore checked where a parent-list page is about to be read
	// rather than in the loop header: a window that has spent its last
	// parent-list page still has to deliver the children of the parents it
	// already listed before it closes.
	for page := 1; page <= defaultMaxPages && canFetchAnotherPage(ctx) &&
		c.withinRetentionBudget(&chunk); page++ {
		if cursor.ChildNextURL != "" {
			parentID := cursor.ParentIDs[cursor.ParentIndex]
			current := cursor.ChildNextURL
			if cursor.ChildParentID != parentID {
				cursor.ChildParentID = parentID
				cursor.ChildRecent, cursor.ChildPages = nil, 0
			}
			if !c.withinChildPageBudget(cursor) {
				// One parent has supplied more child pages than any real fan-out
				// could. Bound the loss to that parent rather than to the window:
				// state the truncation, drop this parent's child cursor, history
				// and count, and carry on with the next parent.
				//
				// Closing the whole window here instead -- which is what the
				// cumulative page bound does, and what this site used to do --
				// threw away every parent behind this one and the parent-list
				// cursor with them, advanced the watermark past the window
				// anyway, and then repeated exactly that loss on the next
				// window, because the next window meets the same parent again.
				// Resuming this parent is equally useless: it would spend the
				// same budget on the same child list on every pass.
				recordTruncatedParent(&chunk, parentID)
				cursor.ChildNextURL, cursor.ChildParentID = "", ""
				cursor.ChildRecent, cursor.ChildPages = nil, 0
				cursor.ParentIndex++
				// The iteration is charged against the per-Handle page bound even
				// though it issued no request, so a fan-out that truncates one
				// parent after another still yields to the next pass instead of
				// walking an entire parent list inside this one.
				continue
			}
			if continuationValueSeen(cursor.ChildRecent, current) {
				return FetchChunk{}, fmt.Errorf("%w: parent-child source returned a repeated child continuation URI", errIncompatibleContinuation)
			}
			body, err := c.datasetRequest(ctx, http.MethodGet, dataset, current, nil)
			if err != nil {
				if deferredFetch(err) {
					chunk.Continuation = cursor
					return chunk, nil
				}
				return FetchChunk{}, err
			}
			// Charged for a child page that was read; see fetchEnvelopeChunk. A
			// parent repeatedly deferred before its first child page no longer
			// spends its per-parent budget and is no longer truncated unread.
			cursor.ChildPages++
			pageChunk := FetchChunk{}
			next, err := c.decodeEnvelopePage(&pageChunk, dataset, body, current, page)
			if err != nil {
				return FetchChunk{}, err
			}
			for i := range pageChunk.Records {
				identity, err := json.Marshal([]string{parentID, pageChunk.Records[i].ID})
				if err != nil {
					return FetchChunk{}, err
				}
				pageChunk.Records[i].ID = string(identity)
			}
			mergeChunk(&chunk, pageChunk)
			cursor.ChildNextURL = next
			if next == "" {
				// Advancing to another parent is the only thing that resets the
				// per-parent child-page counter, alongside the history that
				// detects a repeat within the same parent. Both are cleared here
				// and again where the next parent's first child URL is built, so
				// a pass that ends between the two resumes on a clean count.
				cursor.ParentIndex++
				cursor.ChildParentID = ""
				cursor.ChildRecent, cursor.ChildPages = nil, 0
			} else if err := rememberRecent(&cursor.ChildRecent, current); err != nil {
				return FetchChunk{}, fmt.Errorf("parent-child child continuation: %w", err)
			}
			continue
		}
		if cursor.ParentIndex < len(cursor.ParentIDs) {
			parentID := cursor.ParentIDs[cursor.ParentIndex]
			path := strings.ReplaceAll(dataset.ChildPath, "{parentId}", url.PathEscape(cursor.ParentIDs[cursor.ParentIndex]))
			child := dataset
			child.Kind, child.Path = KindODataGET, path
			first, err := c.envelopeStartURL(child, start, end, subscriptionID)
			if err != nil {
				return FetchChunk{}, err
			}
			cursor.ChildParentID = parentID
			cursor.ChildRecent, cursor.ChildPages = nil, 0
			cursor.ChildNextURL = first
			page--
			continue
		}
		// The byte budget belongs to the list, so it is released with it.
		cursor.ParentIDs, cursor.ParentIndex, cursor.parentBytes = nil, 0, 0
		if cursor.ParentNextURL == "" {
			chunk.Complete = true
			return chunk, nil
		}
		if !c.withinContinuationPageBudget(cursor) {
			return boundedContinuation(chunk, cursor), nil
		}
		current := cursor.ParentNextURL
		if continuationValueSeen(cursor.ParentRecent, current) {
			return FetchChunk{}, fmt.Errorf("%w: parent-child source returned a repeated parent continuation URI", errIncompatibleContinuation)
		}
		body, err := c.datasetRequest(ctx, http.MethodGet, dataset, current, nil)
		if err != nil {
			if deferredFetch(err) {
				chunk.Continuation = cursor
				return chunk, nil
			}
			return FetchChunk{}, err
		}
		// Charged for a parent-list page that was read; see fetchEnvelopeChunk.
		cursor.Pages++
		parent := dataset
		parent.IDField, parent.IDFields = "id", nil
		pageChunk := FetchChunk{}
		next, err := c.decodeEnvelopePageRequiringIdentity(&pageChunk, parent, body, current, page)
		if err != nil {
			return FetchChunk{}, err
		}
		mergeDiagnostics(&chunk, pageChunk)
		for _, record := range pageChunk.Records {
			if record.ID == "" {
				continue
			}
			if err := admitParentID(cursor, dataset, record.ID); err != nil {
				return FetchChunk{}, err
			}
		}
		cursor.ParentNextURL = next
		if next == "" && len(cursor.ParentIDs) == 0 {
			chunk.Complete = true
			return chunk, nil
		}
		if next != "" {
			if err := rememberRecent(&cursor.ParentRecent, current); err != nil {
				return FetchChunk{}, fmt.Errorf("parent-child parent continuation: %w", err)
			}
		}
	}
	// Both cumulative bounds return above with the truncation stated, so a loop
	// that falls out here stopped on a per-pass guard that the next pass re-arms:
	// the fan-out resumes at the same parent and the same child page.
	chunk.Continuation = cursor
	return chunk, nil
}

func (c *Client) fetchHuntingChunk(ctx context.Context, dataset Dataset, start, end time.Time, cursor *FetchContinuation) (FetchChunk, error) {
	limit := defaultPageSize * defaultMaxPages
	if limit < 1 {
		return FetchChunk{}, errors.New("advanced hunting requires a positive internal result budget")
	}
	if limit > 99999 {
		limit = 99999
	}
	if dataset.Mode != ModeSnapshot && len(cursor.HuntingWindows) == 0 {
		cursor.HuntingWindows = []FetchWindow{{Start: start, End: end}}
	}
	if dataset.Mode == ModeSnapshot && cursor.HuntingPartitions == 0 {
		// Preflight the unpartitioned query first. If it overflows, planning
		// widens to deterministic hash partitions before any record is returned.
		cursor.HuntingPartitions = 1
		cursor.HuntingPartition = 0
		cursor.HuntingPlanning = true
	}
	chunk := FetchChunk{}
	for work := 0; work < defaultMaxPages && canFetchAnotherPage(ctx) &&
		c.withinRetentionBudget(&chunk) && c.withinContinuationPageBudget(cursor); work++ {
		query := dataset.Query
		request := map[string]string{}
		if dataset.Mode == ModeSnapshot {
			if cursor.HuntingPartitions > 1 {
				query += " | where " + huntingPartitionExpression(dataset, cursor.HuntingPartitions, cursor.HuntingPartition)
			}
		} else {
			if len(cursor.HuntingWindows) == 0 {
				chunk.Complete = true
				return chunk, nil
			}
			window := cursor.HuntingWindows[0]
			field := dataset.TimeField
			if field == "" {
				field = "Timestamp"
			}
			query += fmt.Sprintf(" | where %s >= datetime(%s) and %s < datetime(%s)", field, window.Start.UTC().Format(time.RFC3339Nano), field, window.End.UTC().Format(time.RFC3339Nano))
			request["Timespan"] = window.Start.UTC().Format(time.RFC3339Nano) + "/" + window.End.UTC().Format(time.RFC3339Nano)
		}
		request["Query"] = fmt.Sprintf("%s | take %d", query, limit+1)
		body, err := json.Marshal(request)
		if err != nil {
			return FetchChunk{}, err
		}
		response, err := c.datasetRequest(ctx, http.MethodPost, dataset, c.conf.Base(dataset.Service)+dataset.Path, body)
		if err != nil {
			if deferredFetch(err) {
				chunk.Continuation = cursor
				return chunk, nil
			}
			return FetchChunk{}, err
		}
		// Charged for a query that was answered; see fetchEnvelopeChunk. Advanced
		// Hunting charges one page per query, including the planning and
		// window-splitting queries below, so every iteration that reaches here is
		// still charged exactly once.
		cursor.Pages++
		// The query asks for limit+1 records so one extra proves the window or
		// partition overflowed. Streaming stops at that proof rather than
		// decoding whatever a source that ignores `take` actually returned, so an
		// element-dense response costs one element past the probe instead of all
		// of it. The overflow decision below is unchanged: it only ever asks
		// whether more than limit records came back.
		results := make([]json.RawMessage, 0, 64)
		found, err := streamEnvelopeObject(response, "results", nil, func(element json.RawMessage) error {
			results = append(results, element)
			if len(results) > limit {
				return errStreamElementsComplete
			}
			return nil
		})
		if err != nil {
			return FetchChunk{}, fmt.Errorf("decode %s hunting results: %w", dataset.Name, err)
		}
		if !found {
			return FetchChunk{}, fmt.Errorf("%s hunting response is missing the results array", dataset.Name)
		}
		if dataset.Mode == ModeSnapshot && cursor.HuntingPlanning {
			if len(results) > limit {
				if cursor.HuntingPartitions >= maximumHuntingPartitions {
					return FetchChunk{}, fmt.Errorf("%s snapshot exceeds the supported %d-partition capacity before delivery; checkpoint retained", dataset.Name, maximumHuntingPartitions)
				}
				if cursor.HuntingPartitions == 1 {
					cursor.HuntingPartitions = 16
				} else {
					cursor.HuntingPartitions *= 2
				}
				cursor.HuntingPartition = 0
				continue
			}
			cursor.HuntingPartition++
			if cursor.HuntingPartition >= cursor.HuntingPartitions {
				cursor.HuntingPlanning = false
				cursor.HuntingPartition = 0
			}
			continue
		}
		if len(results) > limit {
			if dataset.Mode == ModeSnapshot {
				// A source change between planning and delivery may invalidate the
				// plan. Keep the current partition checkpoint and fail instead of
				// repartitioning records which may already have been published.
				return FetchChunk{}, fmt.Errorf("%s snapshot partition exceeded its preflight capacity; checkpoint retained", dataset.Name)
			}
			window := cursor.HuntingWindows[0]
			if !window.End.After(window.Start.Add(time.Millisecond)) {
				return FetchChunk{}, fmt.Errorf("%s hunting millisecond exceeds the %d-record capacity; checkpoint retained", dataset.Name, limit)
			}
			mid := window.Start.Add(window.End.Sub(window.Start) / 2).Truncate(time.Millisecond)
			if !mid.After(window.Start) {
				mid = window.Start.Add(time.Millisecond)
			}
			cursor.HuntingWindows = append([]FetchWindow{{Start: window.Start, End: mid}, {Start: mid, End: window.End}}, cursor.HuntingWindows[1:]...)
			continue
		}
		for i, raw := range results {
			appendDecodedRecord(&chunk, dataset, raw, work+1, i+1)
		}
		if dataset.Mode == ModeSnapshot {
			if cursor.HuntingPartitions == 0 {
				chunk.Complete = true
				return chunk, nil
			}
			cursor.HuntingPartition++
			if cursor.HuntingPartition >= cursor.HuntingPartitions {
				chunk.Complete = true
				return chunk, nil
			}
		} else {
			cursor.HuntingWindows = cursor.HuntingWindows[1:]
		}
	}
	if dataset.Mode != ModeSnapshot && len(cursor.HuntingWindows) == 0 {
		chunk.Complete = true
		return chunk, nil
	}
	return c.yieldContinuation(chunk, cursor), nil
}

func huntingPartitionExpression(dataset Dataset, partitions, partition int) string {
	fields := dataset.IDFields
	if len(fields) == 0 && dataset.IDField != "" {
		fields = []string{dataset.IDField}
	}
	parts := make([]string, 0, len(fields)*2)
	for i, field := range fields {
		if i > 0 {
			parts = append(parts, `"|"`)
		}
		parts = append(parts, "tostring("+field+")")
	}
	return fmt.Sprintf("hash(strcat(%s), %d) == %d", strings.Join(parts, ", "), partitions, partition)
}

func appendDecodedRecord(chunk *FetchChunk, dataset Dataset, raw json.RawMessage, page, ordinal int) {
	appendDecodedRecordWithIdentityPolicy(chunk, dataset, raw, page, ordinal, false)
}

func appendDecodedRecordWithIdentityPolicy(chunk *FetchChunk, dataset Dataset, raw json.RawMessage, page, ordinal int, requireIdentity bool) {
	if dataset.ActivityCategory != "" {
		matches, err := matchesActivityCategory(raw, dataset.ActivityCategory)
		if err != nil {
			recordDecodeFailure(chunk, page, ordinal, err)
			return
		}
		if !matches {
			return
		}
	}
	var record FetchedRecord
	var err error
	if requireIdentity {
		record, err = decodeRecordRequiringIdentity(dataset, raw)
	} else {
		record, err = decodeRecord(dataset, raw)
	}
	if err == nil {
		chunk.Records = append(chunk.Records, record)
		chunk.retained += len(record.Raw)
		return
	}
	recordDecodeFailure(chunk, page, ordinal, err)
}

func matchesActivityCategory(raw json.RawMessage, want string) (bool, error) {
	var record struct {
		Category struct {
			Value string `json:"value"`
		} `json:"category"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return false, err
	}
	return record.Category.Value == want, nil
}

func recordDecodeFailure(chunk *FetchChunk, page, ordinal int, err error) {
	chunk.Malformed++
	if len(chunk.Diagnostics) < maximumDiagnostics {
		chunk.Diagnostics = append(chunk.Diagnostics, RecordDiagnostic{Page: page, Ordinal: ordinal, Reason: safeDecodeReason(err)})
	}
}

func safeDecodeReason(err error) string {
	switch {
	case strings.Contains(err.Error(), "missing its required source identity"):
		return "record is missing its required source identity"
	case strings.Contains(err.Error(), "must be a JSON object"):
		return "record is not a JSON object"
	case strings.Contains(err.Error(), "physical line break"):
		return "compacted record contains a physical line break"
	default:
		return "record is not a valid JSON object"
	}
}

func mergeChunk(dst *FetchChunk, src FetchChunk) {
	dst.Records = append(dst.Records, src.Records...)
	dst.retained += src.retained
	mergeDiagnostics(dst, src)
}

func mergeDiagnostics(dst *FetchChunk, src FetchChunk) {
	dst.Malformed += src.Malformed
	remaining := maximumDiagnostics - len(dst.Diagnostics)
	if remaining > len(src.Diagnostics) {
		remaining = len(src.Diagnostics)
	}
	if remaining > 0 {
		dst.Diagnostics = append(dst.Diagnostics, src.Diagnostics[:remaining]...)
	}
}

// recordTruncatedParent records a parent whose per-parent child-page bound this
// chunk reached. The count is exact and the names are bounded the way record
// diagnostics are, so a window that truncates more parents than that is still
// reported in full and names as many of them as one report can carry.
func recordTruncatedParent(chunk *FetchChunk, parentID string) {
	chunk.ChildBudgetExhausted++
	if len(chunk.TruncatedParents) < maximumDiagnostics {
		chunk.TruncatedParents = append(chunk.TruncatedParents, parentID)
	}
}

func continuationSeen(cursor *FetchContinuation, value string) bool {
	return continuationValueSeen(cursor.Recent, value)
}

func continuationValueSeen(recent []string, value string) bool {
	fingerprint := continuationFingerprint(value)
	for _, recorded := range recent {
		// Continuation history accepts both raw URL and fingerprint forms.
		if recorded == value || recorded == fingerprint {
			return true
		}
	}
	return false
}

func rememberContinuation(cursor *FetchContinuation, value string) error {
	return rememberRecent(&cursor.Recent, value)
}

func rememberRecent(recent *[]string, value string) error {
	fingerprint := continuationFingerprint(value)
	if len(*recent) >= maximumContinuationHistory {
		copy((*recent)[0:], (*recent)[len(*recent)-maximumContinuationHistory+1:])
		(*recent)[maximumContinuationHistory-1] = fingerprint
		*recent = (*recent)[:maximumContinuationHistory]
		return nil
	}
	*recent = append(*recent, fingerprint)
	return nil
}

func recentTail(recent []string) []string {
	if len(recent) > maximumContinuationHistory {
		recent = recent[len(recent)-maximumContinuationHistory:]
	}
	return append([]string(nil), recent...)
}

func continuationFingerprint(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
