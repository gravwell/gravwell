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
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	managementStateVersion              = 1
	managementProgressVersion           = 1
	managementMaximumWindow             = 24 * time.Hour
	managementMinimumWindow             = time.Minute
	managementMaximumHistory            = 7 * 24 * time.Hour
	managementStartSuppression          = 15 * time.Minute
	managementMaximumPagesPerWindow     = 100
	managementMaximumDescriptors        = 10000
	managementMaximumEventsPerContent   = 100000
	managementMaximumEntrySize          = 1 << 20
	managementMaximumEntriesPerHandle   = 1000
	managementMaximumContentReplays     = 3
	managementMaximumImmediateRuns      = 16
	managementMaximumReceipts           = 200000
	managementMaximumContinuationRecent = 32
	managementMaximumSubscriptions      = 64
	managementRetentionDispatchMargin   = 5 * time.Minute
	// managementMaximumDescriptorBytes bounds the persisted descriptor document
	// for one window. managementMaximumDescriptors bounds how many descriptors
	// that document may hold but not how large they are, and a descriptor is
	// mostly a source-supplied contentUri: at managementMaximumURLLength per URI
	// plus a 512-byte contentId the capacity alone admits roughly 28 MB of
	// descriptor JSON, which the listing loop then re-marshals and re-fsyncs on
	// every page, and loadManagementState re-reads and re-validates on every
	// poll. Nothing in the listing path bounded the sum, so a source publishing
	// long URIs grew the document across pages until the window closed.
	//
	// 8 MiB admits the whole descriptor capacity at the ~300-byte descriptors
	// the source actually publishes and still bounds the document when they are
	// larger: the listing narrows its window and lists again before an append
	// would cross the budget, so the remainder is collected by the window that
	// follows rather than dropped.
	managementMaximumDescriptorBytes = 8 << 20
	// managementMaximumReceiptBytes bounds the persisted delivery-receipt
	// document for one selector. managementMaximumReceipts bounds how many
	// receipts that document may hold but not how large they are, and a receipt
	// is a source-supplied contentId plus its creation timestamp:
	// decodeManagementContent accepts a contentId up to 512 bytes, so the count
	// bound alone admits roughly 116 MB of receipt JSON, which every pass that
	// flushes receipts re-marshals and re-fsyncs and every poll re-reads.
	// Nothing bounded the sum.
	//
	// The identifiers the source actually publishes are about 100 bytes, which
	// puts the full receipt capacity at roughly 31 MB, so 32 MiB admits the
	// whole count bound at the shape the source produces and still bounds the
	// document when the identifiers are larger. Crossing it evicts oldest-first
	// exactly as the count bound does, so the budget is recoverable rather than
	// a state a selector can never get past. With
	// managementMaximumDescriptorBytes this caps one selector's two side
	// documents at 40 MiB.
	managementMaximumReceiptBytes = 32 << 20
	// managementMaximumContinuationRecentBytes bounds what the head's stored
	// continuation history costs to retain. managementMaximumContinuationRecent
	// bounds how many continuations it holds but not how long they are, and
	// nothing bounded one stored entry: a head inside managementMaximumStateBytes
	// could carry the full count with megabytes behind each element, all of it
	// retained, re-validated and re-marshalled on every poll for the life of the
	// window.
	//
	// A continuation only ever enters the history after
	// validateManagementURL has accepted it, so one entry is at most
	// managementMaximumURLLength bytes and is stored JSON-escaped. The widest
	// escape encoding/json produces is six characters per byte, so the whole
	// count at the longest admissible continuation is what this budget is: the
	// same number managementMaximumHeadFieldBytes reserves for the history, and
	// the shape the listing actually writes -- 32 URLs of a few hundred bytes --
	// is two orders of magnitude inside it. Crossing it is not a document any
	// build of this plugin wrote, and it is answered the way every other stored
	// bound is: recoverManagementState keeps the durable completed boundary and
	// discards the in-flight listing.
	managementMaximumContinuationRecentBytes = managementMaximumContinuationRecent * (managementMaximumURLLength*6 + len(`"",`))
	// managementMaximumTimestampLength bounds a wire timestamp field. RFC3339
	// with nanoseconds and an offset is under 40 bytes; parseTimestamp would
	// otherwise be handed a megabyte-long string from inside the response bound.
	managementMaximumTimestampLength = 64
	// managementMaximumContentIDLength bounds a source-supplied contentId
	// everywhere one is accepted: from a listing response, from the stored
	// descriptor set, and from the stored receipt set. It was applied only at the
	// listing decode, so an identifier no listing would have admitted could
	// still arrive from the state store, be indexed into the membership map that
	// suppresses redelivery, be reported to an operator, and be re-marshaled into
	// the next checkpoint.
	managementMaximumContentIDLength = 512
	// managementSetDocumentOverhead is the fixed JSON frame one side document
	// wraps around the budget its members are charged against. The longer frame
	// is `{"version":1,"receipts":[]}` at 27 bytes, and the per-member charge in
	// managementDescriptorBytes and managementReceiptBytes already counts the
	// separator each element needs, so 64 bytes covers either frame with room
	// for a version number far longer than one digit.
	managementSetDocumentOverhead = 64
	// managementMaximumContentSetBytes and managementMaximumReceiptSetBytes are
	// the hard ceilings on the two stored side documents, measured before either
	// is decoded. Each one is its set's aggregate budget plus the document frame,
	// which is exactly what a document this build wrote can hold: the budget
	// charges the encoded form of every member, so the sum of the charges is the
	// document less its frame.
	//
	// The ceiling is not redundant with the budget it is derived from. The budget
	// is charged against the members a decoder retained; a JSON member this
	// build does not know is discarded by the decoder and charged against
	// nothing, so an arbitrarily large document could carry a handful of
	// in-budget receipts and be adopted clean. Measuring the bytes first is what
	// bounds the read itself rather than its result.
	managementMaximumContentSetBytes = managementMaximumDescriptorBytes + managementSetDocumentOverhead
	managementMaximumReceiptSetBytes = managementMaximumReceiptBytes + managementSetDocumentOverhead
	// managementMaximumHeadFieldBytes bounds the head document's own fields. The
	// head is fixed-size apart from its continuation history: RecentPages holds
	// managementMaximumContinuationRecent URLs and NextPage one more, each
	// bounded by managementMaximumURLLength, which is 66 KB raw and under 400 KB
	// even if every byte of every URL escaped to six characters. Everything else
	// is a timestamp, a bounded counter, a managementMaximumContentIDLength
	// identifier, a 71-byte digest, or a bounded refusal reason -- of which the
	// head carries two, the live one and the listing-start snapshot, each under
	// managementMaximumRefusedReasonLength. 1 MiB covers all of it with better
	// than a factor of two to spare.
	managementMaximumHeadFieldBytes = 1 << 20
	// managementMaximumStateBytes is the hard ceiling on the stored head
	// document. The head this build writes carries neither side set and is a few
	// hundred bytes; a head written by an earlier build carries both inline, so
	// the ceiling has to admit both side-document ceilings as well as the head's
	// own fields, and that is exactly how it is derived.
	//
	// A head past it is not a document any build of this plugin produced, so
	// recoverManagementState does not decode one either: it treats the boundary
	// such a head might name as absent, which is the same answer it already gives
	// for a head whose bytes will not decode at all.
	managementMaximumStateBytes = managementMaximumContentSetBytes + managementMaximumReceiptSetBytes + managementMaximumHeadFieldBytes
	// managementMaximumProgressBytes is the hard ceiling on the stored delivery
	// checkpoint. It carries a version, two ordinals, a
	// managementMaximumContentIDLength identifier and a 71-byte digest, so even
	// with every identifier byte escaped to six characters it is under 4 KB.
	managementMaximumProgressBytes = 8 << 10
	// managementMaximumRefusedReasonLength bounds the refusal reason the listing
	// records durably for the operator.
	//
	// What this build stores is the canonical reason bounded at
	// maximumDisplayedVendorValue bytes, because the presentation contract is
	// applied where the reason is inserted into an operator-visible record
	// rather than where it is written -- see managementRefusedReason. The bound
	// stays the widest escaped presentation that input bound can produce plus
	// its truncation marker, which is what an earlier build stored, so a
	// document written by one still loads. Tightening it to the input bound
	// would refuse a reason an earlier build legitimately persisted.
	managementMaximumRefusedReasonLength = maximumDisplayedVendorValue*4 + len(displayTruncationMarker)
	// managementWireWindowPrecision is the precision the content-listing query
	// can express: managementContentListURL formats startTime and endTime with
	// time.RFC3339, which drops fractional seconds. Window boundaries are kept
	// at this precision and descriptors are accepted against the range the wire
	// query actually asked for, so the stored window and the request can never
	// disagree about a sub-second boundary.
	managementWireWindowPrecision = time.Second
)

type managementContent struct {
	ContentType       string    `json:"content_type"`
	ContentID         string    `json:"content_id"`
	ContentURI        string    `json:"content_uri"`
	ContentCreated    time.Time `json:"content_created"`
	ContentExpiration time.Time `json:"content_expiration,omitempty"`
}

type managementReceipt struct {
	ContentID      string    `json:"content_id"`
	ContentCreated time.Time `json:"content_created"`
}

// managementRefusalSnapshot is the durable listing-start snapshot of the
// refusal accounting for one window's listing.
//
// A refusal is charged by the state transaction that advances the listing past
// the page that published it, which is what makes the charge exactly-once
// while the listing moves forward. Narrowing does not move forward: it halves
// the window and restarts the listing from the same WindowStart, so every page
// of the prefix that was already charged is requested again, published again
// and refused again. Charging the re-list on top of the charge the discarded
// listing made counted one permanent loss twice -- and again on every further
// narrowing, and across a Handle pass boundary or a restart, because both
// sides of the accounting were already durable.
//
// WindowStart ties the snapshot to the listing it was taken for. Narrowing
// leaves WindowStart alone and every path that begins a different listing --
// startManagementWindow, rebaseExpiredManagementListing,
// dropExpiredManagementWindow, the window-completion reset -- changes or
// clears it, so a stale snapshot is never applied.
type managementRefusalSnapshot struct {
	WindowStart time.Time `json:"window_start"`
	Contents    int       `json:"contents,omitempty"`
	Unreported  int       `json:"unreported,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

type managementState struct {
	Version               int                 `json:"version"`
	SubscriptionStatus    string              `json:"subscription_status,omitempty"`
	LastSubscriptionCheck time.Time           `json:"last_subscription_check,omitempty"`
	LastStartSuccess      time.Time           `json:"last_start_success,omitempty"`
	CompletedThrough      time.Time           `json:"completed_through,omitempty"`
	TargetThrough         time.Time           `json:"target_through,omitempty"`
	WindowStart           time.Time           `json:"window_start,omitempty"`
	WindowEnd             time.Time           `json:"window_end,omitempty"`
	ListingStarted        bool                `json:"listing_started,omitempty"`
	ListingComplete       bool                `json:"listing_complete,omitempty"`
	NextPage              string              `json:"next_page,omitempty"`
	RecentPages           []string            `json:"recent_pages,omitempty"`
	Pages                 int                 `json:"pages,omitempty"`
	Contents              []managementContent `json:"contents,omitempty"`
	ContentIndex          int                 `json:"content_index,omitempty"`
	ActiveContentID       string              `json:"active_content_id,omitempty"`
	ActiveDigest          string              `json:"active_digest,omitempty"`
	ActiveDigestChanges   int                 `json:"active_digest_changes,omitempty"`
	EventIndex            int                 `json:"event_index,omitempty"`
	Receipts              []managementReceipt `json:"receipts,omitempty"`
	ImmediateRuns         int                 `json:"immediate_runs,omitempty"`
	SkippedContents       int                 `json:"skipped_contents,omitempty"`
	SkippedEvents         int                 `json:"skipped_events,omitempty"`
	SkippedFrom           time.Time           `json:"skipped_from,omitempty"`
	SkippedThrough        time.Time           `json:"skipped_through,omitempty"`

	// RefusedContents is the durable count of listing descriptors the
	// descriptor policy permanently refused for this selector, and
	// RefusedUnreported the part of that count no operator report has carried
	// yet. Both are advanced by the same state transaction that advances the
	// listing past the page that published them.
	//
	// Counting a refusal in a pass-local variable instead made the report
	// conditional on the pass reaching its own end. A page's continuation is
	// durable before the next page is requested, so a later page that failed --
	// a transport error, a deferral, a storage fault -- returned before the
	// report ran, and the refusals of every page already past the durable
	// continuation were permanent loss that was never reported and never
	// counted anywhere. The next poll resumed from the continuation and never
	// saw those descriptors again.
	//
	// RefusedReason carries the first reason behind RefusedUnreported. It is
	// stored canonically and bounded by managementRefusedReason; every
	// operator-visible insertion of it goes through displayVendorValue exactly
	// once, which is the one presentation-safe contract a restored reason and a
	// newly generated one both follow.
	RefusedContents   int    `json:"refused_contents,omitempty"`
	RefusedUnreported int    `json:"refused_unreported,omitempty"`
	RefusedReason     string `json:"refused_reason,omitempty"`

	// RefusedListingStart is the refusal accounting as it stood when the
	// listing of the current window began. Narrowing the window restarts that
	// listing from its own start, so every page of the already-charged prefix
	// is published again and refused again; restoring this snapshot is what
	// makes each refused descriptor counted exactly once across any number of
	// narrowings, Handle passes or restarts. It is keyed to WindowStart, which
	// narrowing leaves alone and every new or rebased window changes, so a
	// snapshot can never be applied to a listing it does not belong to.
	RefusedListingStart *managementRefusalSnapshot `json:"refused_listing_start,omitempty"`

	// store is never serialized. It carries what the state store already holds
	// for this selector plus the membership indexes, so a persist rewrites only
	// the parts that changed and the delivery and listing loops never rescan
	// the descriptor or receipt sets.
	store *managementStore
}

// managementStore splits one logical state document across three keys. The head
// document is small and fixed-size; the descriptor set is bounded only by
// managementMaximumDescriptors and the receipt set only by
// managementMaximumReceipts, roughly 25 MB of JSON between them at those
// bounds. The per-content persist used to re-read, re-validate, re-marshal and
// re-fsync all of it once per content blob, which is the same write
// amplification the per-event delivery checkpoint was introduced to remove, one
// level up. Descriptors are now written only by the listing loop and at window
// boundaries, and receipts only when a pass yields or a window closes.
//
// Membership is indexed rather than scanned. The delivery loop asks once per
// content and the listing loop once per listed descriptor, so the linear
// helpers this replaced cost O(descriptors x receipts) string comparisons per
// window.
type managementStore struct {
	head     storedManagementDocument
	progress storedManagementDocument
	contents storedManagementDocument
	receipts storedManagementDocument

	contentIDs map[string]struct{}
	receiptIDs map[string]struct{}

	// contentBytes accounts for the persisted size of the descriptor set so the
	// listing loop can refuse an append that would cross
	// managementMaximumDescriptorBytes without re-measuring the whole set once
	// per listed descriptor. Like the membership indexes it is never serialized
	// and is rebuilt from the decoded set on load.
	contentBytes int
	// receiptBytes does the same for the receipt set, so the once-per-pass
	// maintenance can test managementMaximumReceiptBytes in constant time
	// instead of re-measuring up to managementMaximumReceipts receipts.
	receiptBytes int
	// receiptsEvicted tallies the receipts a content completion had to evict
	// before its own persist could be validated. One pass publishes one
	// operator warning naming the whole eviction, so the tally is carried here
	// -- never serialized, rebuilt per pass with the rest of the store -- and
	// reported by the maintenance pass that runs immediately before the
	// checkpoint, rather than once per content blob.
	receiptsEvicted int
}

// storedManagementDocument remembers the bytes the state store holds for one
// key so the persist path neither re-reads it for the rollback copy nor
// rewrites it when nothing changed.
type storedManagementDocument struct {
	encoded []byte
	found   bool
	dirty   bool
}

// managementContentSet and managementReceiptSet are the two side documents. A
// document written by an earlier build carries both sets inline in the head
// document instead; loadManagementState adopts those and marks them dirty so
// the first persist migrates them rather than dropping them.
type managementContentSet struct {
	Version  int                 `json:"version"`
	Contents []managementContent `json:"contents,omitempty"`
}

type managementReceiptSet struct {
	Version  int                 `json:"version"`
	Receipts []managementReceipt `json:"receipts,omitempty"`
}

// The stored state documents carry two arrays whose length the state store
// controls, and both of them used to be decoded whole before any bound was
// consulted: json.Unmarshal materialized every member into a []managementContent
// or []managementReceipt, and only then did the count bound in
// validateManagementProgressFields, loadManagementReceiptSet's own count check
// and the aggregate byte budgets look at what had already been allocated.
//
// The pre-decode document ceilings bound the bytes that are read, not what
// decoding them retains, and the two are not the same number. A member of a
// dense array is as short as the JSON grammar allows while the struct it
// decodes to is fixed-size: managementReceipt is a string header and a
// time.Time, 40 bytes, so a 32 MiB receipt document of two-byte members
// expands to roughly four hundred megabytes of retained receipts, and a
// managementContent at 96 bytes expands further still. Every one of them was
// allocated, and the element counts were an order of magnitude past the bounds
// that then refused the whole document (CWE-770).
//
// The types below decode those arrays a member at a time, charging the count
// and the aggregate stored bytes before each member is retained, so the
// rejection happens at the bound rather than after the allocation. The outer
// documents are still decoded by json.Unmarshal, which is what keeps every
// other property of these loads exactly as it was: unknown members are still
// ignored, a member of the wrong JSON type is still a type error, and trailing
// data after the document is still refused.
//
// The bounds are the ones that already applied, moved earlier. The aggregate
// charge is the stored bytes of each member rather than the canonical
// re-encoding the byte budgets charge after the decode, which is the same
// number for a document this build wrote -- it writes compact json.Marshal
// output, and managementDescriptorBytes and managementReceiptBytes charge
// exactly that plus the separator -- and the larger, honest number for a
// document padded with whitespace or members this build discards. The overlap
// receipt set in microsoft.go has charged its stored bytes for the same reason
// since it was first bounded.
type managementStoredContents struct {
	values []managementContent
	seen   bool
}

func (contents *managementStoredContents) UnmarshalJSON(raw []byte) error {
	if contents.seen {
		return &managementStoredArrayError{err: errors.New("Microsoft Management Activity state repeats bounded member contents")}
	}
	contents.seen = true
	decoded, err := decodeBoundedManagementArray[managementContent](raw, "content descriptors",
		managementMaximumDescriptors, managementMaximumDescriptorBytes)
	if err != nil {
		return err
	}
	contents.values = decoded
	return nil
}

// managementStoredReceipts is the receipt side document's array. The aggregate
// budget validateManagementReceiptBytes applies to that document is applied
// here too, before the members are retained rather than after.
type managementStoredReceipts struct {
	values []managementReceipt
	seen   bool
}

func (receipts *managementStoredReceipts) UnmarshalJSON(raw []byte) error {
	if receipts.seen {
		return &managementStoredArrayError{err: errors.New("Microsoft Management Activity state repeats bounded member receipts")}
	}
	receipts.seen = true
	decoded, err := decodeBoundedManagementArray[managementReceipt](raw, "delivery receipts",
		managementMaximumReceipts, managementMaximumReceiptBytes)
	if err != nil {
		return err
	}
	receipts.values = decoded
	return nil
}

// managementInlineReceipts is the receipt array an earlier build's head
// document carries inline. It is deliberately held to the count bound and to
// the head document's own ceiling rather than to managementMaximumReceiptBytes:
// an inline set predates the aggregate budget entirely -- the count bound is
// all that ever applied to one -- and loadManagementState adopts it so none of
// it is lost, with maintainManagementReceipts bringing it under budget before
// its first migration persist. Refusing it here on a budget it was never
// written under would discard a usable set instead, and the count bound is
// what bounds the allocation: 200,000 retained receipts, whatever the shape of
// the array they arrived in.
type managementInlineReceipts struct {
	values []managementReceipt
	seen   bool
}

func (receipts *managementInlineReceipts) UnmarshalJSON(raw []byte) error {
	if receipts.seen {
		return &managementStoredArrayError{err: errors.New("Microsoft Management Activity state repeats bounded member receipts")}
	}
	receipts.seen = true
	decoded, err := decodeBoundedManagementArray[managementReceipt](raw, "delivery receipts",
		managementMaximumReceipts, managementMaximumStateBytes)
	if err != nil {
		return err
	}
	receipts.values = decoded
	return nil
}

// managementStoredRecentPages is the head document's continuation history. It
// is held to the exact count the listing maintains and to what that many stored
// continuations may cost, before an element is retained rather than after.
//
// The head ceiling bounds the bytes that are read and says nothing about what
// retaining them costs. A head inside that ceiling but dense in two-byte
// members expanded to millions of string headers -- and to every byte behind
// them -- before the count bound in validateManagementProgressFields ever saw
// the slice json.Unmarshal had already built (CWE-770).
type managementStoredRecentPages struct {
	values []string
	seen   bool
}

func (pages *managementStoredRecentPages) UnmarshalJSON(raw []byte) error {
	if pages.seen {
		return &managementStoredArrayError{err: errors.New("Microsoft Management Activity state repeats bounded member recent_pages")}
	}
	pages.seen = true
	decoded, err := decodeBoundedManagementArray[string](raw, "listing continuation pages",
		managementMaximumContinuationRecent, managementMaximumContinuationRecentBytes)
	if err != nil {
		return err
	}
	pages.values = decoded
	return nil
}

// decodeBoundedManagementArray decodes one stored JSON array member, charging
// and bounding every element before it is retained.
//
// raw is exactly one JSON value -- encoding/json hands an Unmarshaler the bytes
// of the member it is decoding, as a window into the document rather than a
// copy -- so there is nothing after the array to accept or refuse here, and the
// document's own trailing data is still refused by the json.Unmarshal that
// called this.
//
// The aggregate charge is measured from the decoder's own input offset rather
// than from a retained copy of each member, which is both exact and free: the
// span from just inside the opening bracket to just past the member last
// decoded is the member bytes plus the separators between them, so adding the
// one separator the document does not write for its first member gives exactly
// what managementDescriptorBytes and managementReceiptBytes sum to for a
// document this build wrote, and strictly more for one padded with whitespace.
//
// A null member is the absent member, which is what json.Unmarshal already
// leaves for one, so a stored document that writes null instead of omitting the
// array keeps loading.
func decodeBoundedManagementArray[T any](raw []byte, kind string, maximum, maximumBytes int) ([]T, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, nil
	}
	if token != json.Delim('[') {
		return nil, &managementStoredArrayError{err: fmt.Errorf("Microsoft Management Activity %s must be a JSON array", kind)}
	}
	start := decoder.InputOffset()
	var decoded []T
	for decoder.More() {
		// The count bound first, before the element is decoded into anything at
		// all. It is the cheap one and it is what bounds the allocation on its
		// own: a dense array of two-byte members is refused at the bound rather
		// than after every one of them has become a fixed-size element.
		if len(decoded) >= maximum {
			return nil, &managementStoredArrayError{err: fmt.Errorf("Microsoft Management Activity %s exceed capacity %d", kind, maximum)}
		}
		var element T
		if err := decoder.Decode(&element); err != nil {
			return nil, err
		}
		if total := int(decoder.InputOffset()-start) + 1; total > maximumBytes {
			return nil, &managementStoredArrayError{err: fmt.Errorf("Microsoft Management Activity %s exceed %d bytes", kind, maximumBytes)}
		}
		decoded = append(decoded, element)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return decoded, nil
}

// managementStoredArrayError marks a stored array refused on one of its own
// bounds rather than on its syntax. The bound is named for the operator, which
// a generic decode failure cannot do and which is worth saying: the two answers
// call for different things, and neither names any stored byte.
type managementStoredArrayError struct{ err error }

func (e *managementStoredArrayError) Error() string { return e.err.Error() }

func (e *managementStoredArrayError) Unwrap() error { return e.err }

// managementStoredDecodeError classifies a stored-document decode failure.
// A bound the document crossed is reported as itself; anything else keeps the
// generic message these loads have always used, which names the document and
// nothing it held.
func managementStoredDecodeError(err error, generic string) error {
	var bound *managementStoredArrayError
	if errors.As(err, &bound) {
		return corruptManagementState(bound)
	}
	return corruptManagementState(errors.New(generic))
}

// managementStoredHead decodes the head document with every array it carries
// held to its bounds as it is read: the two sets an earlier build's head holds
// inline, and the continuation history every head holds. The embedded
// managementState declares the same three members at a greater depth, so
// encoding/json resolves "contents", "receipts" and "recent_pages" to the
// bounded fields here and leaves the embedded slices nil; every other head field
// decodes into the embedded state exactly as before, an unknown member is still
// ignored, a member of the wrong JSON type is still a type error, and the
// document's own trailing data is still refused.
type managementStoredHead struct {
	managementState
	Contents    managementStoredContents    `json:"contents,omitempty"`
	Receipts    managementInlineReceipts    `json:"receipts,omitempty"`
	RecentPages managementStoredRecentPages `json:"recent_pages,omitempty"`
}

// state assembles the decoded head. The bounded members are adopted into the
// state document the rest of the plugin reads, so nothing downstream of the
// load has to know the bounded types exist.
func (head managementStoredHead) state() managementState {
	decoded := head.managementState
	decoded.Contents = head.Contents.values
	decoded.Receipts = head.Receipts.values
	decoded.RecentPages = head.RecentPages.values
	return decoded
}

// managementStoredBoundary decodes a head document for recoverManagementState,
// which reads exactly one field out of it.
//
// It names only that field, so every other member of the head -- including the
// two sets an earlier build may carry inline -- is a member this struct does
// not know, and encoding/json skips an unknown member at the scanner rather
// than decoding it. Nothing inside either array is parsed, allocated or walked
// member by member, which is both what keeps the repair's cost proportional to
// the bytes it was handed and what keeps the durable boundary readable out of a
// head whose sets are precisely why the document had to be replaced.
//
// The head still has to be well-formed JSON and the boundary still has to be a
// timestamp, so a head whose bytes do not decode is treated as absent exactly
// as before.
type managementStoredBoundary struct {
	Version          int       `json:"version"`
	CompletedThrough time.Time `json:"completed_through,omitempty"`
}

func managementContentsKey(key string) string {
	return key + "/contents"
}

func managementReceiptsKey(key string) string {
	return key + "/receipts"
}

func (state *managementState) ensureStore() *managementStore {
	if state.store == nil {
		state.store = &managementStore{}
	}
	if state.store.contentIDs == nil || state.store.receiptIDs == nil {
		state.indexMembership()
	}
	return state.store
}

// indexMembership rebuilds both membership indexes and the descriptor byte
// accounting from the decoded sets. It runs once per load and once per set
// reset, never per item.
func (state *managementState) indexMembership() {
	if state.store == nil {
		state.store = &managementStore{}
	}
	state.store.contentIDs = make(map[string]struct{}, len(state.Contents))
	state.store.contentBytes = 0
	for _, content := range state.Contents {
		state.store.contentIDs[content.ContentID] = struct{}{}
		state.store.contentBytes += managementDescriptorBytes(content)
	}
	state.store.receiptIDs = make(map[string]struct{}, len(state.Receipts))
	state.store.receiptBytes = 0
	for _, receipt := range state.Receipts {
		state.store.receiptIDs[receipt.ContentID] = struct{}{}
		state.store.receiptBytes += managementReceiptBytes(receipt)
	}
}

func (state *managementState) hasContent(contentID string) bool {
	_, found := state.ensureStore().contentIDs[contentID]
	return found
}

func (state *managementState) hasReceipt(contentID string) bool {
	_, found := state.ensureStore().receiptIDs[contentID]
	return found
}

func (state *managementState) appendContent(content managementContent) {
	store := state.ensureStore()
	state.Contents = append(state.Contents, content)
	store.contentIDs[content.ContentID] = struct{}{}
	store.contentBytes += managementDescriptorBytes(content)
	store.contents.dirty = true
}

// replaceContent substitutes a re-listed descriptor for the same stable
// contentId, so membership is unchanged.
func (state *managementState) replaceContent(index int, content managementContent) {
	store := state.ensureStore()
	store.contentBytes += managementDescriptorBytes(content) - managementDescriptorBytes(state.Contents[index])
	state.Contents[index] = content
	store.contentIDs[content.ContentID] = struct{}{}
	store.contents.dirty = true
}

func (state *managementState) resetContents() {
	store := state.ensureStore()
	state.Contents = nil
	store.contentIDs = make(map[string]struct{})
	store.contentBytes = 0
	store.contents.dirty = true
}

// descriptorBytes reports what the descriptor set currently costs to persist.
func (state *managementState) descriptorBytes() int {
	return state.ensureStore().contentBytes
}

// descriptorReplacementBytes reports what the descriptor set would cost to
// persist if the descriptor at index were replaced by content. It is the
// preflight replaceContent has no way to be: replaceContent applies the delta
// to the accounting and the budget is not tested until the persist that
// follows, by which point the over-budget descriptor is already the one the
// state document holds.
func (state *managementState) descriptorReplacementBytes(index int, content managementContent) int {
	return state.descriptorBytes() + managementDescriptorBytes(content) - managementDescriptorBytes(state.Contents[index])
}

// managementDescriptorBytes charges one descriptor the size of its persisted
// JSON form plus the separator the document needs to hold it. A descriptor that
// cannot be marshalled is charged the whole budget so it can never be appended
// and then fail the persist that follows.
func managementDescriptorBytes(content managementContent) int {
	encoded, err := json.Marshal(content)
	if err != nil {
		return managementMaximumDescriptorBytes
	}
	return len(encoded) + 1
}

// appendReceipt records one receipt in O(1). It neither prunes nor trims:
// bounding the set once per receipt is what made a set growing toward
// managementMaximumReceipts quadratic, and the whole set is pruned and bounded
// once per pass by maintainManagementReceipts instead.
func (state *managementState) appendReceipt(receipt managementReceipt) {
	store := state.ensureStore()
	state.Receipts = append(state.Receipts, receipt)
	store.receiptIDs[receipt.ContentID] = struct{}{}
	store.receiptBytes += managementReceiptBytes(receipt)
	store.receipts.dirty = true
}

// retainReceipts applies a prune or trim result. Only the identifiers that were
// actually dropped leave the index and only their bytes leave the accounting,
// so bounding the set stays proportional to the drop rather than to the size of
// the set.
func (state *managementState) retainReceipts(kept, dropped []managementReceipt) {
	store := state.ensureStore()
	state.Receipts = kept
	for _, receipt := range dropped {
		delete(store.receiptIDs, receipt.ContentID)
		store.receiptBytes -= managementReceiptBytes(receipt)
	}
	store.receipts.dirty = true
}

// receiptBytes reports what the receipt set currently costs to persist.
func (state *managementState) receiptBytes() int {
	return state.ensureStore().receiptBytes
}

// managementReceiptBytes charges one receipt the size of its persisted JSON
// form plus the separator the document needs to hold it, which is what makes
// the aggregate budget a bound on the bytes that are actually written rather
// than on an estimate of them. A receipt that cannot be marshalled is charged
// the whole budget so a maintenance pass always evicts it.
func managementReceiptBytes(receipt managementReceipt) int {
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return managementMaximumReceiptBytes
	}
	return len(encoded) + 1
}

// advanceContent moves past the content at ContentIndex and clears the
// per-content delivery ordinal.
func (state *managementState) advanceContent() {
	state.ContentIndex++
	state.ActiveContentID, state.ActiveDigest, state.EventIndex, state.ActiveDigestChanges = "", "", 0, 0
}

// managementProgress is the per-event delivery checkpoint. It lives on its own
// small key so accepting one event no longer re-validates, re-marshals and
// re-fsyncs the whole state document, which is bounded only by
// managementMaximumDescriptors and managementMaximumReceipts and can reach tens
// of megabytes. Durability is unchanged: the resumable ordinal is still written
// and synchronized before the next event is delivered. The full document stays
// authoritative, so the ordinal is adopted on load only when the content it was
// recorded against is still the active one.
type managementProgress struct {
	Version         int    `json:"version"`
	ContentIndex    int    `json:"content_index"`
	ActiveContentID string `json:"active_content_id,omitempty"`
	ActiveDigest    string `json:"active_digest,omitempty"`
	EventIndex      int    `json:"event_index"`
}

func managementProgressKey(key string) string {
	return key + "/progress"
}

func managementProgressFor(state managementState) managementProgress {
	return managementProgress{
		Version:         managementProgressVersion,
		ContentIndex:    state.ContentIndex,
		ActiveContentID: state.ActiveContentID,
		ActiveDigest:    state.ActiveDigest,
		EventIndex:      state.EventIndex,
	}
}

// resumes reports whether the progress document still describes the content the
// full state document is delivering. Any mismatch means the full document moved
// on and the progress ordinal is stale.
func (p managementProgress) resumes(state managementState) bool {
	return p.ContentIndex == state.ContentIndex &&
		p.ActiveContentID == state.ActiveContentID &&
		p.ActiveDigest == state.ActiveDigest
}

type managementSubscription struct {
	ContentType string `json:"contentType"`
	Status      string `json:"status"`
}

type managementContentWire struct {
	ContentType       string `json:"contentType"`
	ContentID         string `json:"contentId"`
	ContentURI        string `json:"contentUri"`
	ContentCreated    string `json:"contentCreated"`
	ContentExpiration string `json:"contentExpiration"`
}

// managementMaximumDisplayedContentID bounds the escaped presentation of a
// source-supplied contentId. decodeManagementContent accepts 512 bytes, and a
// descriptor set read back from the state document is bounded only in
// aggregate, so the presentation is bounded on its own rather than through
// whatever a sink happens to do with an arbitrarily long parameter.
const managementMaximumDisplayedContentID = maximumDisplayedVendorValue

// displayManagementContentID renders a source-supplied contentId for an
// operator-visible log parameter or status message. The identifier is decoded
// verbatim from the listing response and the Management API constrains nothing
// about its bytes, so it can carry CR, LF or any other control character. The
// default RFC5424 file sink escapes '\', '"' and ']' in a parameter value and
// writes every other byte through unchanged, so an embedded CR or LF ends the
// physical line there and the remainder of the identifier forges further
// records: an operator reading the log, and anything parsing it, is shown
// entries this plugin never wrote.
//
// displayVendorValue is the shared contract that answers it for every
// unconstrained vendor string this plugin reports -- a contentId, a
// subscription status, a parent identifier -- so one logged value is always
// exactly one physical line and the result is bounded.
//
// This is presentation only. The canonical ContentID is what the content
// request, the descriptor set, the dedup and receipt membership, the
// digest/state identity and the persisted documents carry, and none of those go
// through here.
func displayManagementContentID(contentID string) string {
	return displayVendorValue(contentID, managementMaximumDisplayedContentID)
}

// displayManagementSubscriptionStatus renders the subscription status the
// source reported for one content type. decodeManagementSubscriptions bounds it
// at 64 bytes and ensureManagementSubscription lowercases it and trims its
// edges, which leaves every interior control character intact: a status of
// "dis\r\n<13>1 ... forged" was one physical record on the wire and two in the
// operator's log.
//
// The vocabulary is not validated instead. The Management API documents
// "enabled" and "disabled" but the whole point of this report is to state a
// status the plugin did not expect, including one a later service revision
// introduces, so rejecting an unknown word would discard exactly the
// information the operator needs. The bytes are neutralized and the value is
// still reported.
//
// Presentation only: SubscriptionStatus keeps the canonical lowercased bytes in
// state, and the "enabled" comparison that gates collection reads those.
func displayManagementSubscriptionStatus(status string) string {
	return displayVendorValue(status, maximumDisplayedVendorValue)
}

const managementStateKeySuffix = "/management-v1"

func managementStateKey(namespace, dataset string) string {
	return namespace + "/" + dataset + managementStateKeySuffix
}

// managementSelectorFromKey recovers the selector name from a state key.
// managementStateKey is the only thing that builds one, so the persist path can
// name the selector in an operator warning without the dataset having to be
// threaded through every checkpoint.
func managementSelectorFromKey(key string) string {
	trimmed := strings.TrimSuffix(key, managementStateKeySuffix)
	if separator := strings.LastIndex(trimmed, "/"); separator >= 0 {
		return trimmed[separator+1:]
	}
	return trimmed
}

func managementRetryNotBeforeKey(namespace string) string {
	return namespace + "/management-origin-retry-not-before-v1"
}

func (state managementState) hasPending() bool {
	return !state.TargetThrough.IsZero() || !state.WindowStart.IsZero() || state.ListingStarted || state.NextPage != "" || state.ContentIndex < len(state.Contents)
}

// managementCorruptStateError marks a stored-state failure as recoverable
// corruption: the state store answered, and the bytes it returned are
// undecodable, of an unsupported version, structurally invalid, or carry
// progress no fetched blob can contain. Only these can be repaired by
// replacing the document.
//
// A failed storage read is a different condition entirely. It reports that the
// state store could not answer and says nothing at all about the bytes it
// holds, yet it used to arrive at the same reset: a transient state-store fault
// discarded a usable durable checkpoint and published a zero one in its place,
// which silently re-collected or permanently skipped everything after the
// boundary it threw away. It is returned unclassified so the poll fails and a
// later one retries against the state that is still there.
type managementCorruptStateError struct{ err error }

func (e *managementCorruptStateError) Error() string { return e.err.Error() }

func (e *managementCorruptStateError) Unwrap() error { return e.err }

// corruptManagementState classifies err as recoverable corruption without
// changing the message an operator is shown.
func corruptManagementState(err error) error {
	if err == nil {
		return nil
	}
	return &managementCorruptStateError{err: err}
}

// recoverableManagementCorruption reports whether err describes state bytes
// that were read successfully and cannot be used, which is the only condition
// a destructive reset repairs.
func recoverableManagementCorruption(err error) bool {
	var corrupt *managementCorruptStateError
	return errors.As(err, &corrupt)
}

func loadManagementState(rt hosted.Storage, key string) (managementState, error) {
	store := &managementStore{}
	var state managementState
	encoded, err := rt.Get(key)
	switch {
	case errors.Is(err, storage.ErrStorageNotFound):
		state.Version = managementStateVersion
	case err != nil:
		return managementState{}, err
	default:
		// Measured before the decode rather than after it. A head document this
		// large is refused whether or not it is well-formed, so the cost of
		// reading one stays bounded by the ceiling instead of by whatever the
		// state store happens to hold -- which, for the two sets an earlier
		// build's head carries inline, was the only bound they had at all.
		if len(encoded) > managementMaximumStateBytes {
			return managementState{}, corruptManagementState(fmt.Errorf("Microsoft Management Activity state document is %d bytes, over the %d-byte ceiling", len(encoded), managementMaximumStateBytes))
		}
		// The ceiling bounds the bytes that are read; the bounded inline sets in
		// managementStoredHead are what bound what decoding them retains. A head
		// whose inline array is dense in members expanded to hundreds of
		// megabytes of retained descriptors or receipts before the count bound
		// below ever saw it.
		var head managementStoredHead
		if err := json.Unmarshal(encoded, &head); err != nil {
			return managementState{}, managementStoredDecodeError(err, "decode Microsoft Management Activity state")
		}
		if err := rejectDuplicateStateMembers(encoded); err != nil {
			return managementState{}, corruptManagementState(errors.New("decode Microsoft Management Activity state"))
		}
		state = head.state()
		store.head = storedManagementDocument{encoded: encoded, found: true}
	}
	state.store = store
	// A head document written by an earlier build carries both sets inline. Keep
	// them and mark the side documents dirty: the head this build writes omits
	// them, so the first persist has to move them across.
	if len(state.Contents) != 0 {
		store.contents.dirty = true
	} else if contents, found, encodedContents, err := loadManagementContentSet(rt, key); err != nil {
		return managementState{}, err
	} else if found {
		state.Contents = contents
		store.contents = storedManagementDocument{encoded: encodedContents, found: true}
	}
	if len(state.Receipts) != 0 {
		store.receipts.dirty = true
	} else if receipts, found, encodedReceipts, err := loadManagementReceiptSet(rt, key); err != nil {
		return managementState{}, err
	} else if found {
		state.Receipts = receipts
		store.receipts = storedManagementDocument{encoded: encodedReceipts, found: true}
	}
	// The descriptor document is authoritative for its own length. A crash
	// between shrinking that document and publishing the matching head can
	// leave the older head one or more positions past the new descriptor set.
	// Clamp that torn-write state to the intended reset point. Receipts make the
	// bounded replay safe, while returning an error here would wedge the source
	// forever because no later poll could rewrite either key.
	if state.ContentIndex > len(state.Contents) {
		state.ContentIndex = len(state.Contents)
		state.ActiveContentID, state.ActiveDigest = "", ""
		state.EventIndex, state.ActiveDigestChanges = 0, 0
	}
	if err := validateManagementState(state); err != nil {
		return managementState{}, corruptManagementState(err)
	}
	state.indexMembership()
	progress, found, encodedProgress, err := loadManagementProgress(rt, key)
	if err != nil {
		return managementState{}, err
	}
	if found {
		store.progress = storedManagementDocument{encoded: encodedProgress, found: true}
	}
	if found && progress.resumes(state) && progress.EventIndex > state.EventIndex {
		state.EventIndex = progress.EventIndex
	}
	return state, nil
}

// recoverManagementState constructs a writable replacement after one of the
// split state documents becomes undecodable, unsupported, or internally
// inconsistent. The completed boundary is the only head field safe to retain:
// it is advanced only after accepted delivery and a synchronized state
// commit. All in-flight listing and delivery fields are discarded so the
// bounded overlap/receipt contract can replay from that durable boundary.
//
// Reading the prior head is therefore part of the repair, not a best effort. A
// storage read that fails leaves the durable boundary unknown, and a zero
// CompletedThrough is not a safe stand-in for an unknown one: publishing it
// restarts the selector at the configured lookback, which re-collects or
// permanently skips whatever lay beyond it. That read error is returned so the
// caller can decline to publish anything. A head that is absent is a different
// answer: the state store reports there is no prior boundary, and zero is the
// correct one.
func recoverManagementState(rt hosted.Storage, key string) (managementState, error) {
	state := managementState{Version: managementStateVersion}
	switch encoded, err := rt.Get(key); {
	case errors.Is(err, storage.ErrStorageNotFound):
	case err != nil:
		return managementState{}, err
	default:
		// The same ceiling loadManagementState applies, for the same reason: the
		// repair must not be the one path that decodes an unbounded document. A
		// head past it is not one any build of this plugin wrote, and the
		// boundary such a head might name is treated as absent -- which is
		// already the answer this path gives for a head whose bytes do not
		// decode, and is the only answer available without reading it.
		//
		// Inside the ceiling, the two sets an earlier build's head may carry
		// inline are skipped rather than decoded. The completed boundary is the
		// only field this repair keeps and every in-flight field is deliberately
		// thrown away, so decoding a set to reach the boundary allocated the
		// whole of it for nothing -- and a set dense enough to be the condition
		// that routed the document here in the first place would have allocated
		// hundreds of megabytes of it. Skipping them is also what keeps the
		// durable boundary readable: bounding them here instead would make a
		// document the bounds refuse one this path cannot read a boundary from
		// either, and publishing a zero boundary re-collects or permanently skips
		// everything beyond it.
		if len(encoded) <= managementMaximumStateBytes {
			var previous managementStoredBoundary
			if json.Unmarshal(encoded, &previous) == nil && rejectDuplicateStateMembers(encoded) == nil && !previous.CompletedThrough.IsZero() {
				state.CompletedThrough = previous.CompletedThrough.UTC()
			}
		}
	}
	state.store = &managementStore{
		head:     storedManagementDocument{dirty: true},
		contents: storedManagementDocument{dirty: true},
		receipts: storedManagementDocument{dirty: true},
		progress: storedManagementDocument{dirty: true},
	}
	return state, nil
}

func loadManagementContentSet(rt hosted.Storage, key string) ([]managementContent, bool, []byte, error) {
	encoded, err := rt.Get(managementContentsKey(key))
	if errors.Is(err, storage.ErrStorageNotFound) {
		return nil, false, nil, nil
	}
	if err != nil {
		return nil, false, nil, err
	}
	// Measured before the decode. managementMaximumDescriptors bounds how many
	// descriptors the document may carry and validateManagementDescriptors
	// charges each one, but neither says anything about bytes the decoder throws
	// away, so the document is bounded on its own first.
	if len(encoded) > managementMaximumContentSetBytes {
		return nil, false, nil, corruptManagementState(fmt.Errorf("Microsoft Management Activity content descriptor document is %d bytes, over the %d-byte ceiling", len(encoded), managementMaximumContentSetBytes))
	}
	// The array is streamed under the count bound and the aggregate budget, so
	// the member past either one is refused before it is retained rather than
	// after the whole array has been materialized. A document both over a bound
	// and of an unsupported version now reports the bound rather than the
	// version, which is the one ordering this moves: either answer replaces the
	// document from the last completed boundary.
	var document struct {
		Version  int                      `json:"version"`
		Contents managementStoredContents `json:"contents,omitempty"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, false, nil, managementStoredDecodeError(err, "decode Microsoft Management Activity content descriptors")
	}
	if err := rejectDuplicateStateMembers(encoded); err != nil {
		return nil, false, nil, corruptManagementState(errors.New("decode Microsoft Management Activity content descriptors"))
	}
	if document.Version != managementStateVersion {
		return nil, false, nil, corruptManagementState(fmt.Errorf("Microsoft Management Activity content descriptors have unsupported version %d", document.Version))
	}
	return document.Contents.values, true, encoded, nil
}

func loadManagementReceiptSet(rt hosted.Storage, key string) ([]managementReceipt, bool, []byte, error) {
	encoded, err := rt.Get(managementReceiptsKey(key))
	if errors.Is(err, storage.ErrStorageNotFound) {
		return nil, false, nil, nil
	}
	if err != nil {
		return nil, false, nil, err
	}
	// Measured before the decode, for the reason the count bound cannot cover:
	// managementMaximumReceipts bounds how many receipts the document may carry
	// and not how large any one of them is, and a member this build does not know
	// is discarded without being charged against anything.
	if len(encoded) > managementMaximumReceiptSetBytes {
		return nil, false, nil, corruptManagementState(fmt.Errorf("Microsoft Management Activity delivery receipt document is %d bytes, over the %d-byte ceiling", len(encoded), managementMaximumReceiptSetBytes))
	}
	// Both bounds are applied as the array is streamed, which is where they
	// have to be: the count bound and the aggregate budget below used to be
	// tested against a slice json.Unmarshal had already materialized, so a
	// document dense in short members allocated an order of magnitude more
	// receipts than either bound admits before either one refused it. A
	// document both over a bound and of an unsupported version now reports the
	// bound rather than the version; both answers replace the document.
	var document struct {
		Version  int                      `json:"version"`
		Receipts managementStoredReceipts `json:"receipts,omitempty"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, false, nil, managementStoredDecodeError(err, "decode Microsoft Management Activity delivery receipts")
	}
	if err := rejectDuplicateStateMembers(encoded); err != nil {
		return nil, false, nil, corruptManagementState(errors.New("decode Microsoft Management Activity delivery receipts"))
	}
	if document.Version != managementStateVersion {
		return nil, false, nil, corruptManagementState(fmt.Errorf("Microsoft Management Activity delivery receipts have unsupported version %d", document.Version))
	}
	// The aggregate budget is durable, not only a maintenance-time guard. This
	// build writes this document only after maintainManagementReceipts has
	// bounded the set, so a side document over the budget is one no build of this
	// plugin wrote: it is replaced from the last completed boundary rather than
	// adopted, indexed, and re-marshaled into the next checkpoint.
	//
	// The streamed charge above is the stored bytes of each member; this charges
	// the canonical re-encoding of what they parsed to. For a document this
	// build wrote they are the same number, and keeping both means neither a
	// padded document nor one whose members re-encode larger than they were
	// stored can be adopted over budget.
	if err := validateManagementReceiptBytes(document.Receipts.values); err != nil {
		return nil, false, nil, corruptManagementState(err)
	}
	return document.Receipts.values, true, encoded, nil
}

func loadManagementProgress(rt hosted.Storage, key string) (managementProgress, bool, []byte, error) {
	encoded, err := rt.Get(managementProgressKey(key))
	if errors.Is(err, storage.ErrStorageNotFound) {
		return managementProgress{}, false, nil, nil
	}
	if err != nil {
		return managementProgress{}, false, nil, err
	}
	// Measured before the decode. The checkpoint is written once per accepted
	// event and read once per poll, and every field in it is fixed-size, so a
	// document over the ceiling is refused without being parsed.
	if len(encoded) > managementMaximumProgressBytes {
		return managementProgress{}, false, nil, corruptManagementState(fmt.Errorf("Microsoft Management Activity delivery progress document is %d bytes, over the %d-byte ceiling", len(encoded), managementMaximumProgressBytes))
	}
	var progress managementProgress
	if err := json.Unmarshal(encoded, &progress); err != nil {
		return managementProgress{}, false, nil, corruptManagementState(errors.New("decode Microsoft Management Activity delivery progress"))
	}
	if err := rejectDuplicateStateMembers(encoded); err != nil {
		return managementProgress{}, false, nil, corruptManagementState(errors.New("decode Microsoft Management Activity delivery progress"))
	}
	if progress.Version != managementProgressVersion {
		return managementProgress{}, false, nil, corruptManagementState(fmt.Errorf("Microsoft Management Activity delivery progress has unsupported version %d", progress.Version))
	}
	// The checkpoint carries the same ordinal as the head document and is adopted
	// in its place when it is ahead, so it carries the same absolute bound. A
	// corrupt ordinal here is refused rather than adopted, which routes the
	// selector through recoverManagementState instead of handing the delivery
	// loop a position no fetched blob can contain.
	//
	// The identifier is bounded here too. resumes compares it against the head
	// document's own ActiveContentID, which the state/progress relationship
	// invariant ties to the descriptor at ContentIndex, so a checkpoint naming an
	// identifier no listing could have admitted can only have arrived from
	// outside this plugin.
	if progress.ContentIndex < 0 || progress.EventIndex < 0 || progress.EventIndex > managementMaximumEventsPerContent ||
		len(progress.ActiveContentID) > managementMaximumContentIDLength {
		return managementProgress{}, false, nil, corruptManagementState(errors.New("Microsoft Management Activity delivery progress contains invalid progress"))
	}
	return progress, true, encoded, nil
}

func validateManagementState(state managementState) error {
	if err := validateManagementProgressFields(state); err != nil {
		return err
	}
	if err := validateManagementReceipts(state.Receipts); err != nil {
		return err
	}
	return validateManagementDescriptors(state.Contents)
}

// validateManagementProgressFields checks everything that does not depend on
// the size of the descriptor set, so the per-content persist stays O(1). The
// descriptor loop runs on load and whenever the descriptor set itself is
// rewritten, which is the only way an invalid descriptor can be introduced.
func validateManagementProgressFields(state managementState) error {
	if state.Version != managementStateVersion {
		return fmt.Errorf("Microsoft Management Activity state has unsupported version %d", state.Version)
	}
	if len(state.Contents) > managementMaximumDescriptors || len(state.Receipts) > managementMaximumReceipts || len(state.RecentPages) > managementMaximumContinuationRecent {
		return errors.New("Microsoft Management Activity state exceeds a safety bound")
	}
	// managementMaximumEventsPerContent is the absolute bound on the delivery
	// ordinal: decodeManagementEvents refuses the event past that bound before it
	// is retained, so no blob this plugin can fetch holds more, and the delivery
	// loop only ever increments EventIndex up to the length of the blob it is
	// delivering. A larger stored ordinal is a structural impossibility, and
	// bounding only the sign of it is what let it through load validation: the
	// delivery loop then compared it against the fetched event count, failed, and
	// mutated nothing, so every later poll fetched the same blob and failed
	// identically until the window aged out of the source's retention. Refusing
	// it here routes the document through recoverManagementState, which keeps the
	// durable completed boundary and discards the in-flight ordinal.
	if state.ContentIndex < 0 || state.ContentIndex > len(state.Contents) || state.EventIndex < 0 || state.EventIndex > managementMaximumEventsPerContent || state.Pages < 0 || state.Pages > managementMaximumPagesPerWindow {
		return errors.New("Microsoft Management Activity state contains invalid progress")
	}
	// The in-flight content and the descriptor it was recorded against are one
	// relationship, and it is checked against the position this same document
	// carries, so it stays O(1) and the per-content persist is unaffected.
	//
	// Bounding the two fields separately let the pair disagree. A head naming
	// in-flight content that is not the descriptor at ContentIndex passed every
	// bound above, and the delivery loop then fetched the blob at ContentIndex,
	// compared the identifiers, and returned a plain error having mutated
	// nothing at all -- so every later poll read the same document back, fetched
	// the same blob and failed identically until the window crossed the source's
	// seven-day retention floor and the whole interval was lost with it.
	// Classifying it here routes the document through recoverManagementState,
	// which keeps the durable completed boundary and discards only the in-flight
	// work.
	if state.ActiveContentID != "" &&
		(state.ContentIndex >= len(state.Contents) || state.Contents[state.ContentIndex].ContentID != state.ActiveContentID) {
		return errors.New("Microsoft Management Activity state contains in-flight content that is not the descriptor it was recorded against")
	}
	if state.ImmediateRuns < 0 || state.ImmediateRuns > managementMaximumImmediateRuns {
		return errors.New("Microsoft Management Activity state contains invalid immediate-run progress")
	}
	if state.ActiveDigestChanges < 0 || state.ActiveDigestChanges > managementMaximumContentReplays {
		return errors.New("Microsoft Management Activity state contains invalid content-replay progress")
	}
	if state.SkippedContents < 0 || state.SkippedEvents < 0 || state.RefusedContents < 0 ||
		state.RefusedUnreported < 0 || state.RefusedUnreported > state.RefusedContents ||
		len(state.RefusedReason) > managementMaximumRefusedReasonLength {
		return errors.New("Microsoft Management Activity state contains invalid permanent-loss accounting")
	}
	// The listing-start snapshot is restored into the live accounting by a
	// narrowing, so it carries exactly the invariants the live accounting does
	// -- or the restore would publish a document the persist that follows it
	// refuses, and the narrowing that exists to keep a dense window moving
	// would become the thing that stalled it. Its count cannot exceed the live
	// one either: the live count only grows while a listing runs, and the only
	// thing that lowers it is this snapshot.
	if snapshot := state.RefusedListingStart; snapshot != nil &&
		(snapshot.Contents < 0 || snapshot.Unreported < 0 || snapshot.Unreported > snapshot.Contents ||
			snapshot.Contents > state.RefusedContents ||
			len(snapshot.Reason) > managementMaximumRefusedReasonLength) {
		return errors.New("Microsoft Management Activity state contains an invalid listing-start refusal snapshot")
	}
	if state.WindowStart.IsZero() != state.WindowEnd.IsZero() || (!state.WindowStart.IsZero() && (!state.WindowStart.Before(state.WindowEnd) || state.WindowEnd.Sub(state.WindowStart) > managementMaximumWindow)) {
		return errors.New("Microsoft Management Activity state contains an invalid window")
	}
	return nil
}

// validateManagementReceipts checks the shape of every restored receipt. The
// count bound in validateManagementProgressFields says how many receipts the
// document may carry and nothing at all about what any one of them holds, and a
// restored receipt is not inert: it goes straight into the membership index that
// suppresses redelivery, it is charged against the aggregate byte budget, and it
// is re-marshaled into the document the next checkpoint persists.
//
// An identifier past managementMaximumContentIDLength is one no listing would
// have admitted, so it can only have arrived from outside this plugin. A receipt
// with no creation time is worse than useless: pruneManagementReceipts and
// trimManagementReceipts both key on ContentCreated and the zero time is before
// every retention horizon, so it is dropped by the first maintenance pass --
// having been trusted to suppress delivery in the meantime.
//
// Neither error names the identifier. The canonical bytes are source-supplied
// and an operator-visible diagnostic is not where they belong; the descriptor
// paths that do name one go through displayManagementContentID.
func validateManagementReceipts(receipts []managementReceipt) error {
	for _, receipt := range receipts {
		if strings.TrimSpace(receipt.ContentID) == "" || len(receipt.ContentID) > managementMaximumContentIDLength {
			return fmt.Errorf("Microsoft Management Activity state contains a delivery receipt whose content identifier is empty or over %d bytes", managementMaximumContentIDLength)
		}
		if receipt.ContentCreated.IsZero() {
			return errors.New("Microsoft Management Activity state contains a delivery receipt with no creation time")
		}
	}
	return nil
}

// validateManagementReceiptBytes charges the whole restored receipt set against
// the aggregate budget the maintenance pass enforces before it writes one. It is
// separate from validateManagementReceipts because the two answers differ for
// the two documents a receipt set can arrive in: a set in the receipt side key
// was written by this build, under this budget, so one over it is corruption,
// while a set carried inline by an earlier build's head predates the budget
// entirely and is pruned to it by the maintenance pass that runs before its
// first migration persist.
func validateManagementReceiptBytes(receipts []managementReceipt) error {
	total := 0
	for _, receipt := range receipts {
		if total += managementReceiptBytes(receipt); total > managementMaximumReceiptBytes {
			return fmt.Errorf("Microsoft Management Activity delivery receipts exceed %d bytes", managementMaximumReceiptBytes)
		}
	}
	return nil
}

func validateManagementDescriptors(contents []managementContent) error {
	total := 0
	for _, content := range contents {
		if content.ContentID == "" || content.ContentURI == "" || content.ContentType == "" {
			return errors.New("Microsoft Management Activity state contains an invalid content descriptor")
		}
		// The same identifier bound the listing decode applies. A descriptor set
		// is re-read on every poll, indexed, reported and re-marshaled, so an
		// identifier the listing would have refused must not be able to arrive
		// from the state store instead.
		if len(content.ContentID) > managementMaximumContentIDLength || content.ContentCreated.IsZero() {
			return fmt.Errorf("Microsoft Management Activity state contains a content descriptor whose identifier is over %d bytes or whose creation time is missing", managementMaximumContentIDLength)
		}
		// The cumulative bound is durable, not only a listing-time guard: a
		// document written by an earlier build, or one whose descriptors grew
		// another way, is refused here and replaced from the last completed
		// boundary by recoverManagementState rather than carried forward.
		if total += managementDescriptorBytes(content); total > managementMaximumDescriptorBytes {
			return fmt.Errorf("Microsoft Management Activity content descriptors exceed %d bytes", managementMaximumDescriptorBytes)
		}
	}
	return nil
}

// persistManagementState durably records the state document. Only the parts
// that changed are written: the head document and the delivery checkpoint are
// small and fixed-size, the descriptor set is rewritten by the listing loop and
// at window boundaries, and the receipt set is flushed by
// persistManagementCheckpoint. One content completion therefore costs two small
// writes and one synchronization however large those sets have grown.
func (m *Microsoft) persistManagementState(rt hosted.Storage, key string, state *managementState) error {
	return m.persistManagementDocument(rt, key, state, false)
}

// persistManagementCheckpoint additionally flushes the receipt set. Receipts are
// only consulted across windows -- inside a window it is ContentIndex in the
// head document that prevents redelivery, and that is synchronized per content
// -- so they may lag until a pass yields or a window closes. Every path that
// hands state back to the next window goes through here.
//
// This is also the one place the receipt set is pruned and bounded, immediately
// before the bytes that carry it are written.
func (m *Microsoft) persistManagementCheckpoint(rt hosted.Runtime, key string, state *managementState) error {
	m.maintainManagementReceipts(rt, key, state)
	return m.persistManagementDocument(rt, key, state, true)
}

// maintainManagementReceipts is the single deterministic receipt-maintenance
// pass: expired receipts are pruned in one linear pass and then both the count
// and the aggregate-byte budget are enforced, oldest-first.
//
// Doing this after every recorded receipt is what made the receipt set
// quadratic. A set growing toward managementMaximumReceipts paid one linear
// prune scan per insertion, every overflow stable-sorted and copied the whole
// set, and every overflow published its own operator warning -- so a tenant
// sustaining a receipt set near the bound paid O(n^2) comparisons and copies
// per window and one warning per content blob. Insertion is now O(1) and the
// set is visited once per pass, immediately before the checkpoint that persists
// it, which is the only point at which the bounds have to hold.
//
// A set nothing was recorded into is left alone. It is already the bounded set
// the last pass wrote, so re-measuring it would only dirty a document that does
// not need rewriting.
func (m *Microsoft) maintainManagementReceipts(rt hosted.Logger, key string, state *managementState) {
	store := state.ensureStore()
	if !store.receipts.dirty {
		return
	}
	if kept, dropped := pruneManagementReceipts(state.Receipts, m.now().UTC().Add(-managementMaximumHistory)); len(dropped) > 0 {
		state.retainReceipts(kept, dropped)
	}
	evicted := 0
	if kept, dropped := trimManagementReceipts(state.Receipts, m.receiptCapacity()); len(dropped) > 0 {
		state.retainReceipts(kept, dropped)
		evicted += len(dropped)
	}
	if kept, dropped := trimManagementReceiptBytes(state.Receipts, state.receiptBytes(), m.receiptByteCapacity()); len(dropped) > 0 {
		state.retainReceipts(kept, dropped)
		evicted += len(dropped)
	}
	// Whatever a content completion had to evict to make itself persistable is
	// reported here too, so a pass publishes one warning naming the whole
	// eviction however the receipts went.
	evicted += store.receiptsEvicted
	store.receiptsEvicted = 0
	if evicted == 0 {
		return
	}
	rt.Warn("Microsoft Management Activity dropped the oldest delivery receipts at capacity",
		log.KV("selector", managementSelectorFromKey(key)), log.KV("dropped", evicted),
		log.KV("capacity", m.receiptCapacity()), log.KV("byte_capacity", m.receiptByteCapacity()))
}

type pendingManagementWrite struct {
	key      string
	document *storedManagementDocument
	encoded  []byte
	absent   []byte
}

func (m *Microsoft) persistManagementDocument(rt hosted.Storage, key string, state *managementState, flushReceipts bool) error {
	if m.stateSync == nil {
		return errors.New("Microsoft Management Activity requires state-store synchronization")
	}
	if err := validateManagementProgressFields(*state); err != nil {
		return err
	}
	store := state.ensureStore()
	if store.contents.dirty {
		if err := validateManagementDescriptors(state.Contents); err != nil {
			return err
		}
	}
	writes := make([]pendingManagementWrite, 0, 4)
	if store.contents.dirty {
		encoded, err := json.Marshal(managementContentSet{Version: managementStateVersion, Contents: state.Contents})
		if err != nil {
			return err
		}
		absent, _ := json.Marshal(managementContentSet{Version: managementStateVersion})
		writes = append(writes, pendingManagementWrite{managementContentsKey(key), &store.contents, encoded, absent})
	}
	// A receipt set adopted from an inline head document written by an earlier
	// build is flushed immediately whether or not this is a checkpoint: the head
	// written below no longer carries it.
	if store.receipts.dirty && (flushReceipts || !store.receipts.found) {
		encoded, err := json.Marshal(managementReceiptSet{Version: managementStateVersion, Receipts: state.Receipts})
		if err != nil {
			return err
		}
		absent, _ := json.Marshal(managementReceiptSet{Version: managementStateVersion})
		writes = append(writes, pendingManagementWrite{managementReceiptsKey(key), &store.receipts, encoded, absent})
	}
	head := *state
	head.Contents, head.Receipts = nil, nil
	encodedHead, err := json.Marshal(head)
	if err != nil {
		return err
	}
	absentHead, _ := json.Marshal(managementState{Version: managementStateVersion})
	writes = append(writes, pendingManagementWrite{key, &store.head, encodedHead, absentHead})
	// The delivery checkpoint is republished alongside the head document in the
	// same synchronization, so a per-event ordinal can never be read back as
	// progress against content the head document has already moved past.
	encodedProgress, err := json.Marshal(managementProgressFor(*state))
	if err != nil {
		return err
	}
	absentProgress, _ := json.Marshal(managementProgress{Version: managementProgressVersion})
	writes = append(writes, pendingManagementWrite{managementProgressKey(key), &store.progress, encodedProgress, absentProgress})
	return m.commitManagementWrites(rt, writes)
}

// commitManagementWrites applies every pending write and synchronizes once. A
// failed synchronization restores each key from the bytes the store recorded
// when it was last read or written, so no rollback copy has to be read back.
func (m *Microsoft) commitManagementWrites(rt hosted.Storage, writes []pendingManagementWrite) error {
	for _, write := range writes {
		if err := rt.Put(write.key, write.encoded); err != nil {
			return err
		}
	}
	if err := m.stateSync(); err != nil {
		for _, write := range writes {
			rollback := write.document.encoded
			if !write.document.found {
				rollback = write.absent
			}
			if putErr := rt.Put(write.key, rollback); putErr != nil {
				return errors.Join(err, fmt.Errorf("rollback Microsoft Management Activity state: %w", putErr))
			}
		}
		if syncErr := m.stateSync(); syncErr != nil {
			return errors.Join(err, fmt.Errorf("synchronize Microsoft Management Activity rollback: %w", syncErr))
		}
		return fmt.Errorf("synchronize Microsoft Management Activity state: %w", err)
	}
	for _, write := range writes {
		*write.document = storedManagementDocument{encoded: write.encoded, found: true}
	}
	return nil
}

// persistManagementProgress durably records one accepted event. It writes
// only the small delivery checkpoint, so per-event state I/O is independent of
// the size of the content and receipt sets. The write boundary is
// unchanged: the ordinal is still synchronized before the next event is sent.
func (m *Microsoft) persistManagementProgress(rt hosted.Storage, key string, state *managementState) error {
	if m.stateSync == nil {
		return errors.New("Microsoft Management Activity requires state-store synchronization")
	}
	store := state.ensureStore()
	encoded, err := json.Marshal(managementProgressFor(*state))
	if err != nil {
		return err
	}
	absent, _ := json.Marshal(managementProgress{Version: managementProgressVersion})
	if err := m.commitManagementWrites(rt, []pendingManagementWrite{
		{managementProgressKey(key), &store.progress, encoded, absent},
	}); err != nil {
		return fmt.Errorf("Microsoft Management Activity delivery progress: %w", err)
	}
	return nil
}

func loadManagementRetryNotBefore(rt hosted.Storage, key string) (time.Time, error) {
	value, err := rt.GetTime(key)
	if errors.Is(err, storage.ErrStorageNotFound) {
		return time.Time{}, nil
	}
	return value.UTC(), err
}

func (m *Microsoft) persistManagementRetryNotBefore(rt hosted.Storage, key string, notBefore time.Time) error {
	if m.stateSync == nil {
		return errors.New("Microsoft Management Activity requires state-store synchronization")
	}
	previous, previousErr := loadManagementRetryNotBefore(rt, key)
	if previousErr != nil {
		return previousErr
	}
	if !notBefore.After(previous) {
		return nil
	}
	if err := rt.PutTime(key, notBefore.UTC()); err != nil {
		return err
	}
	if err := m.stateSync(); err == nil {
		return nil
	} else {
		if putErr := rt.PutTime(key, previous); putErr != nil {
			return errors.Join(err, fmt.Errorf("rollback Microsoft Management Activity retry state: %w", putErr))
		}
		if syncErr := m.stateSync(); syncErr != nil {
			return errors.Join(err, fmt.Errorf("synchronize Microsoft Management Activity retry rollback: %w", syncErr))
		}
		return fmt.Errorf("synchronize Microsoft Management Activity retry state: %w", err)
	}
}

func (m *Microsoft) managementRequest(ctx context.Context, rt hosted.Storage, method, endpoint string) (managementResponse, error) {
	key := managementRetryNotBeforeKey(m.conf.StateNamespace())
	notBefore, err := loadManagementRetryNotBefore(rt, key)
	if err != nil {
		return managementResponse{}, err
	}
	if delay := notBefore.Sub(m.now().UTC()); delay > 0 {
		return managementResponse{}, &managementDeferredError{Delay: delay}
	}
	response, err := m.dispatchManagementRequest(ctx, method, endpoint)
	var deferred *managementDeferredError
	if !errors.As(err, &deferred) {
		return response, err
	}
	delay := deferred.Delay
	if delay < 0 {
		delay = 0
	}
	if delay > managementMaximumRetryAfter {
		delay = managementMaximumRetryAfter
	}
	if delay > 0 {
		if persistErr := m.persistManagementRetryNotBefore(rt, key, m.now().UTC().Add(delay)); persistErr != nil {
			return managementResponse{}, persistErr
		}
	}
	return managementResponse{}, &managementDeferredError{Delay: delay}
}

// dispatchManagementRequest bounds one vendor request. The fetch deadline covers
// authentication, rate waits, retries and the response body of this request
// only. It used to wrap the whole of collectManagement, including the
// delivery loop. The cumulative span keeps a pass bounded: it spends at most
// one span plus one request deadline in the source, then persists a continuation
// so the next pass resumes instead of monopolizing sibling selectors.
func (m *Microsoft) dispatchManagementRequest(ctx context.Context, method, endpoint string) (managementResponse, error) {
	if err := claimManagementFetchSpan(ctx); err != nil {
		return managementResponse{}, err
	}
	fetchCtx, cancel := withFetchDeadline(ctx, m.client.fetchDeadline)
	defer cancel()
	started := time.Now()
	response, err := m.client.managementRequest(fetchCtx, method, endpoint)
	chargeManagementFetchSpan(ctx, time.Since(started))
	return response, err
}

func (m *Microsoft) managementFetchSpan() time.Duration {
	if m.managementFetchSpanLimit > 0 {
		return m.managementFetchSpanLimit
	}
	return m.client.fetchDeadline
}

func (m *Microsoft) collectManagement(ctx context.Context, rt hosted.Runtime, dataset Dataset) (bool, error) {
	if m.stateSync == nil {
		return false, errors.New("Microsoft Management Activity requires state-store synchronization")
	}
	key := managementStateKey(m.conf.StateNamespace(), dataset.Name)
	state, err := loadManagementState(rt, key)
	if err != nil {
		// Only state the store actually handed back and this build cannot use is
		// repaired by replacing it. A read that failed reports on the state store,
		// not on the bytes: resetting on it threw away a usable durable checkpoint
		// for a condition the next poll may not even see. Return it with nothing
		// written and no request issued to the source.
		if !recoverableManagementCorruption(err) {
			return false, fmt.Errorf("load %s state: %w", dataset.Name, err)
		}
		// An undecodable, unsupported, or invalid state document cannot repair
		// itself. Preserve a usable durable completed boundary when the head is
		// still decodable, replace every split document, and let the next poll
		// restart from that boundary instead of returning the same error forever.
		recovered, recoverErr := recoverManagementState(rt, key)
		if recoverErr != nil {
			// The durable boundary could not be read, so there is nothing safe to
			// restart from. Publishing a zero one would re-collect or skip
			// everything beyond it; report instead and keep the stored state.
			return false, fmt.Errorf("reset %s state: %w", dataset.Name, recoverErr)
		}
		state = recovered
		rt.Warn("Microsoft Management Activity stored state was reset; collection restarts from the last usable checkpoint",
			log.KV("selector", dataset.Name),
			log.KV("completed_through", state.CompletedThrough.UTC().Format(time.RFC3339Nano)),
			log.KV("reason", m.safePollError(err)))
		if err := m.persistManagementCheckpoint(rt, key, &state); err != nil {
			return false, fmt.Errorf("reset %s state: %w", dataset.Name, err)
		}
		return false, nil
	}
	// A head document written by an earlier build carries its sets inline and,
	// for the receipts, under no aggregate budget at all: the count bound is all
	// that ever applied to one. loadManagementState adopts the set so none of it
	// is lost and marks it dirty, and the first persist then migrates it to the
	// receipt side key -- whether or not that persist is a checkpoint, because
	// the head this build writes no longer carries it -- and marks it clean.
	// Clean is the state maintainManagementReceipts returns from immediately, so
	// a set that crossed the budget was written once and then never visited
	// again.
	//
	// The migration is performed here, before anything else in the pass can
	// append to the set, and the set is bounded immediately before it. Leaving it
	// to whichever persist happened to come first would have written the bounded
	// set plus whatever that pass had already recorded on top of it, which is a
	// document the next load would refuse. A set already in its own side key is
	// clean on load, so this costs nothing on an ordinary poll.
	if store := state.ensureStore(); store.receipts.dirty && !store.receipts.found {
		m.maintainManagementReceipts(rt, key, &state)
		if err := m.persistManagementState(rt, key, &state); err != nil {
			return false, fmt.Errorf("migrate %s state: %w", dataset.Name, err)
		}
	}
	// fetchCtx carries the per-pass HTTP work budget and span. The job context
	// itself is what the delivery loop writes on, so a slow ingest link can no
	// longer consume the deadline that belongs to the source.
	fetchCtx := withManagementFetchSpan(withFetchBudget(ctx), m.managementFetchSpan())

	ready, err := m.ensureManagementSubscription(fetchCtx, rt, key, dataset, &state)
	if err != nil {
		if deferredFetch(err) {
			return m.managementContinuation(rt, key, &state)
		}
		return false, err
	}
	if !ready {
		return false, nil
	}
	if state.WindowStart.IsZero() {
		oldest := m.managementRetentionFloor()
		if !state.CompletedThrough.IsZero() && state.CompletedThrough.Before(oldest) {
			m.reportManagementRetentionLoss(rt, dataset,
				"Microsoft Management Activity checkpoint is outside source retention; the unavailable interval is permanently skipped",
				state.CompletedThrough.UTC(), oldest)
		}
		if !m.startManagementWindow(&state) {
			state.ImmediateRuns = 0
			if err := m.persistManagementCheckpoint(rt, key, &state); err != nil {
				return false, err
			}
			return false, nil
		}
		if err := m.persistManagementCheckpoint(rt, key, &state); err != nil {
			return false, err
		}
	}
	if !state.ListingComplete {
		rebased, unavailableFrom, resumedAt, expired, err := m.rebaseExpiredManagementListing(&state)
		if err != nil {
			return false, err
		}
		if rebased {
			m.reportManagementContentLoss(rt, dataset, &state, expired, 0,
				"Microsoft Management Activity listed content aged out of source retention before delivery; those content blobs are permanently skipped")
			m.reportManagementRetentionLoss(rt, dataset,
				"Microsoft Management Activity pending listing crossed source retention; the unavailable interval is permanently skipped",
				unavailableFrom, resumedAt)
			if err := m.persistManagementCheckpoint(rt, key, &state); err != nil {
				return false, err
			}
		}
		if err := m.listManagementContents(fetchCtx, rt, key, dataset, &state); err != nil {
			if deferredFetch(err) {
				return m.managementContinuation(rt, key, &state)
			}
			return false, err
		}
		if !state.ListingComplete {
			return m.managementContinuation(rt, key, &state)
		}
	}

	// A window whose own end has crossed the retention floor cannot be delivered
	// at all: every blob it still points at is gone and re-listing it is
	// rejected outright. Report the exact permanent loss and advance the
	// checkpoint past it rather than re-reading the same dead descriptor on
	// every poll forever.
	if expired, dropped := m.dropExpiredManagementWindow(&state); dropped {
		m.reportManagementContentLoss(rt, dataset, &state, expired, 0,
			"Microsoft Management Activity window aged out of source retention before delivery completed; the undelivered content is permanently skipped")
		return m.managementContinuation(rt, key, &state)
	}

	// processed charges every attempted unit of delivery work against one
	// per-pass bound, whether it was an accepted event, an event skipped as
	// malformed, or a whole descriptor disposed of without reading it.
	//
	// Charging only accepted writes left the bound blind to the skip path: a
	// skip-dense blob could run the whole of managementMaximumEventsPerContent
	// inside a single Handle call, each skip costing a durable synchronization, a
	// log line and a status warning, with cancellation observed nowhere in
	// between -- and because the counter stayed at zero the same pass then ran on
	// into every remaining descriptor. The skip already publishes its corrected
	// ordinal durably, so charging it here yields the pass at the same bound a
	// delivered event does and the next pass resumes from that ordinal.
	//
	// The three descriptor-disposal branches below were blind to it the same way.
	// Each one advances ContentIndex and synchronizes, and two of them also
	// append a receipt, report permanent loss and republish a status warning, yet
	// none of them charged anything: one Handle call could therefore dispose of
	// the entire descriptor set -- up to managementMaximumDescriptors
	// synchronizations and operator warnings -- with no cancellation observed in
	// between. A Tenant-ID or Management-Host change makes that the ordinary
	// case rather than the rare one, because it invalidates every stored
	// descriptor at once. Each disposal is durable before the loop continues, so
	// charging it yields the pass at managementMaximumEntriesPerHandle and the
	// next pass resumes from the ContentIndex this one published.
	processed := 0
	for state.ContentIndex < len(state.Contents) && processed < managementMaximumEntriesPerHandle {
		content := state.Contents[state.ContentIndex]
		if state.hasReceipt(content.ContentID) {
			state.advanceContent()
			processed++
			if err := m.persistManagementState(rt, key, &state); err != nil {
				return false, err
			}
			continue
		}
		if err := m.client.validateManagementURL(content.ContentURI); err != nil {
			// A descriptor persisted by an earlier build under a URL policy this
			// one no longer agrees with can never be fetched. Dispatching it
			// failed the same way on every poll, which turned bounded loss of
			// one blob into total cessation of collection for the content type.
			// Record the exact loss, leave a receipt so an overlapping listing
			// cannot resurrect it, and move on.
			m.recordManagementReceiptForCompletion(&state, content)
			m.reportManagementContentLoss(rt, dataset, &state, []managementContent{content}, 0,
				"Microsoft Management Activity stored content URI is refused by the Microsoft Management Activity URL policy; it is permanently skipped: "+m.safePollError(err))
			state.advanceContent()
			processed++
			if err := m.persistManagementState(rt, key, &state); err != nil {
				return false, err
			}
			continue
		}
		delivered := state.EventIndex
		events, digest, err := m.fetchManagementContent(fetchCtx, rt, key, dataset, &state, state.ContentIndex)
		if err != nil {
			if deferredFetch(err) {
				return m.managementContinuation(rt, key, &state)
			}
			// The blob has aged out of the source's seven-day retention and no
			// refresh can bring it back. Returning the error left ContentIndex,
			// Contents and ListingComplete untouched, so every later poll read
			// the same descriptor and failed the same way: a bounded loss of one
			// unavailable blob became permanent, total cessation of collection
			// for the content type. Skip it as permanent loss instead, with a
			// receipt so an overlapping listing cannot resurrect it.
			var unavailable *managementContentUnavailableError
			if errors.As(err, &unavailable) {
				m.recordManagementReceiptForCompletion(&state, content)
				m.reportManagementContentLoss(rt, dataset, &state, []managementContent{content}, delivered,
					"Microsoft Management Activity content is no longer available from the source; it is permanently skipped: "+unavailable.reason)
				state.advanceContent()
				processed++
				if err := m.persistManagementState(rt, key, &state); err != nil {
					return false, err
				}
				continue
			}
			return false, err
		}
		if state.ActiveContentID == "" {
			state.ActiveContentID, state.ActiveDigest = content.ContentID, digest
			state.EventIndex, state.ActiveDigestChanges = 0, 0
			if err := m.persistManagementState(rt, key, &state); err != nil {
				return false, err
			}
		} else if state.ActiveContentID != content.ContentID {
			// Unreachable, and kept as the assertion that says so. The
			// state/progress relationship invariant in
			// validateManagementProgressFields rejects exactly this pair on
			// load and refuses to persist it, so a document that reaches the
			// delivery loop cannot name in-flight content other than the
			// descriptor at ContentIndex. Returning an error here was the whole
			// of the old handling, and it mutated nothing: the next poll read
			// the same document, fetched the same blob and failed identically
			// until the window aged out of retention.
			return false, errors.New("Microsoft Management Activity active content does not match the listed descriptor")
		} else if state.ActiveDigest != digest {
			// The source can re-serialize a blob when an expired content URI is
			// refreshed or a different backend serves the same contentId. Keeping
			// the old ordinal against a different body can skip records, while
			// returning an error here leaves the selector permanently pinned to the
			// blob. Publish a new durable boundary before writing any event. Replay
			// a bounded number of times; if a contentId keeps changing in violation
			// of the source contract, retain the durable ordinal so the entire
			// content type cannot be held indefinitely. Entries accepted during a
			// replay may be delivered again under the at-least-once contract.
			previousEventIndex := state.EventIndex
			state.ActiveDigest = digest
			replay := state.ActiveDigestChanges < managementMaximumContentReplays
			if replay {
				state.ActiveDigestChanges++
				state.EventIndex = 0
			} else if state.EventIndex > len(events) {
				state.EventIndex = len(events)
			}
			if err := m.persistManagementState(rt, key, &state); err != nil {
				return false, err
			}
			if replay {
				rt.Warn("Microsoft Management Activity content changed while partially delivered; replaying it from the beginning",
					log.KV("selector", dataset.Name), log.KV("content_id", displayManagementContentID(content.ContentID)),
					log.KV("previous_event_index", previousEventIndex),
					log.KV("replay", state.ActiveDigestChanges))
			} else {
				rt.Warn("Microsoft Management Activity content repeatedly changed while partially delivered; continuing from the durable ordinal",
					log.KV("selector", dataset.Name), log.KV("content_id", displayManagementContentID(content.ContentID)),
					log.KV("event_index", state.EventIndex),
					log.KV("replays", state.ActiveDigestChanges))
			}
		}
		if state.EventIndex > len(events) {
			// The absolute bound in validateManagementProgressFields is what load
			// validation can check; only the fetched blob states how many events it
			// actually holds. An ordinal inside that bound and past this body is
			// still corruption -- the delivery loop increments it only up to
			// len(events), and the digest branch above clamps it when the source
			// re-serves a shortened blob -- and returning it mutated nothing at
			// all, so the next poll fetched the same blob at the same ordinal and
			// failed the same way for as long as the window stayed in retention.
			// Replay this blob from its own beginning instead, which is the bounded
			// at-least-once recovery the changed-content path already performs, and
			// publish the corrected ordinal before any event is written.
			corrupt := state.EventIndex
			state.EventIndex = 0
			if err := m.persistManagementState(rt, key, &state); err != nil {
				return false, err
			}
			rt.Warn("Microsoft Management Activity stored event progress exceeded the content it was recorded against; the content is replayed from the beginning",
				log.KV("selector", dataset.Name), log.KV("content_id", displayManagementContentID(content.ContentID)),
				log.KV("stored_event_index", corrupt), log.KV("events", len(events)))
			rt.SetWarn(fmt.Sprintf("Microsoft Management Activity selector %s reset a corrupt delivery ordinal of %d against the %d events content %s holds; that content is replayed under the at-least-once contract",
				dataset.Name, corrupt, len(events), displayManagementContentID(content.ContentID)))
		}
		for state.EventIndex < len(events) && processed < managementMaximumEntriesPerHandle {
			record, recordType, err := prepareManagementEvent(dataset, events[state.EventIndex], m.now().UTC())
			if err != nil {
				ordinal := state.EventIndex
				state.EventIndex++
				state.SkippedEvents++
				processed++
				rt.Warn("Microsoft Management Activity event was skipped after a bounded decode failure",
					log.KV("selector", dataset.Name), log.KV("content_id", displayManagementContentID(content.ContentID)),
					log.KV("event_ordinal", ordinal), log.KV("skipped_total", state.SkippedEvents),
					log.KV("reason", m.safePollError(err)))
				rt.SetWarn(fmt.Sprintf("Microsoft Management Activity selector %s skipped %d malformed or oversized events; inspect runner warnings for the first affected content and ordinal", dataset.Name, state.SkippedEvents))
				if err := m.persistManagementProgress(rt, key, &state); err != nil {
					return false, err
				}
				continue
			}
			tag, err := m.negotiateTagName(rt, m.conf.TagForManagementEvent(dataset, recordType))
			if err != nil {
				return false, err
			}
			item := entry.Entry{TS: entry.FromStandard(record.Timestamp), Tag: tag, Data: record.Raw}
			if err := attachSourceIdentity(&item, dataset); err != nil {
				return false, err
			}
			if err := rt.Write(item); err != nil {
				return false, fmt.Errorf("write %s entry: %w", dataset.Name, err)
			}
			state.EventIndex++
			processed++
			if err := m.persistManagementProgress(rt, key, &state); err != nil {
				return false, err
			}
		}
		if state.EventIndex < len(events) {
			return m.managementContinuation(rt, key, &state)
		}
		m.recordManagementReceiptForCompletion(&state, content)
		state.advanceContent()
		if err := m.persistManagementState(rt, key, &state); err != nil {
			return false, err
		}
	}
	if state.ContentIndex < len(state.Contents) {
		return m.managementContinuation(rt, key, &state)
	}

	state.CompletedThrough = state.WindowEnd
	state.WindowStart, state.WindowEnd = time.Time{}, time.Time{}
	state.ListingStarted, state.ListingComplete = false, false
	state.NextPage, state.RecentPages = "", nil
	state.Pages, state.ContentIndex = 0, 0
	state.resetContents()
	// The last advanceContent has already cleared these, and the state/progress
	// relationship invariant is what guarantees it: no document reaching here can
	// name in-flight content, because one that did would have been classified as
	// corrupt on load. Clearing them beside the descriptor reset anyway keeps
	// every window-boundary path in this file identical.
	state.ActiveContentID, state.ActiveDigest, state.EventIndex, state.ActiveDigestChanges = "", "", 0, 0
	if !state.TargetThrough.IsZero() && state.CompletedThrough.Before(state.TargetThrough) {
		if !m.startManagementWindow(&state) {
			return false, errors.New("Microsoft Management Activity could not start the next bounded catch-up window")
		}
		return m.managementContinuation(rt, key, &state)
	}
	state.TargetThrough = time.Time{}
	state.ImmediateRuns = 0
	if err := m.persistManagementCheckpoint(rt, key, &state); err != nil {
		return false, err
	}
	return false, nil
}

// recordManagementReceipt records one content blob as finished with this
// selector, whether it was delivered or permanently skipped. It is O(1): the
// set is pruned and bounded once per pass by maintainManagementReceipts,
// immediately before the checkpoint that persists it.
func recordManagementReceipt(state *managementState, content managementContent) {
	state.appendReceipt(managementReceipt{ContentID: content.ContentID, ContentCreated: content.ContentCreated})
}

// recordManagementReceiptForCompletion is what the delivery loop records a
// finished content blob through. It makes room for the receipt before it is
// appended, because the persist that carries the completion is validated
// against the same hard bounds the checkpoint is and cannot evict anything
// itself.
//
// Exactly managementMaximumReceipts retained receipts is a valid document. One
// more completed content appended receipt 200,001, and the per-content persist
// -- persistManagementState, which deliberately does not run receipt
// maintenance, because running it per receipt is what made the set quadratic
// -- refused the whole state transaction on the count bound. Nothing was
// written, so the next poll read the same 200,000 receipts back, resumed at the
// same ContentIndex, re-fetched the same blob, found it fully delivered,
// appended the same receipt and refused identically: collection for that
// content type stopped at the capacity it was supposed to recover from, and
// kept reporting pending work while it did.
//
// The receipt being recorded is never a candidate for eviction, because room
// is made before it exists. That is what keeps an unacknowledged identity --
// the one whose delivery this very transaction is about to make durable -- out
// of the eviction, while the receipts that do go are the oldest by
// ContentCreated, which is the order every other eviction in this file uses.
func (m *Microsoft) recordManagementReceiptForCompletion(state *managementState, content managementContent) {
	m.reserveManagementReceipt(state, managementReceipt{ContentID: content.ContentID, ContentCreated: content.ContentCreated})
	recordManagementReceipt(state, content)
}

// reserveManagementReceipt evicts oldest-first until the set can hold one more
// receipt inside both hard bounds, and does nothing at all when it already
// can -- which is every completion of every selector that is not sitting at a
// capacity, so insertion stays O(1) for all of them.
//
// Eviction clears headroom rather than exactly the one receipt that crossed
// the bound. A set held at capacity would otherwise cross it again on the very
// next content, so every content blob would pay one whole-set prune, one
// whole-set heapify and one whole-set copy: that is the quadratic cost
// maintainManagementReceipts was introduced to remove, reintroduced one level
// down. One headroom covers more content blobs than managementMaximumEntriesPerHandle
// admits in a pass, so a selector at capacity pays that whole-set pass at most
// once per pass, and the receipts it clears are the oldest ones the next
// maintenance pass would have dropped anyway.
func (m *Microsoft) reserveManagementReceipt(state *managementState, receipt managementReceipt) {
	charge := managementReceiptBytes(receipt)
	countCapacity, byteCapacity := m.receiptCapacity(), m.receiptByteCapacity()
	if len(state.Receipts) < countCapacity && state.receiptBytes()+charge <= byteCapacity {
		return
	}
	countTarget := countCapacity - 1 - managementReceiptEvictionHeadroom(countCapacity, 1)
	byteTarget := byteCapacity - charge - managementReceiptEvictionHeadroom(byteCapacity, charge)
	evicted := 0
	if countTarget < 1 || byteTarget < 1 {
		// Defensive only: receiptCapacity admits the whole production count
		// bound and receiptByteCapacity admits one receipt by five orders of
		// magnitude. A capacity that cannot hold one receipt beside anything at
		// all still has to make room rather than leave the completion
		// unpersistable, which is the stall this whole path repairs.
		if dropped := state.Receipts; len(dropped) > 0 {
			state.retainReceipts(nil, dropped)
			evicted = len(dropped)
		}
	} else {
		if kept, dropped := trimManagementReceipts(state.Receipts, countTarget); len(dropped) > 0 {
			state.retainReceipts(kept, dropped)
			evicted += len(dropped)
		}
		if kept, dropped := trimManagementReceiptBytes(state.Receipts, state.receiptBytes(), byteTarget); len(dropped) > 0 {
			state.retainReceipts(kept, dropped)
			evicted += len(dropped)
		}
	}
	state.ensureStore().receiptsEvicted += evicted
}

// managementReceiptEvictionHeadroom is how far below a bound the per-content
// eviction takes the receipt set, so the whole-set pass it costs amortizes
// across the contents that follow instead of running once per content. unit is
// what one receipt costs against that bound -- one, for the count bound, and
// the receipt's own charge for the byte budget -- so both headrooms are the
// same number of content blobs rather than the same number of units.
//
// It is a small fraction of the bound and never more than one pass's worth of
// content blobs, so a production set loses a few thousand of its oldest
// receipts at 200,000 rather than one at a time, and a test capacity of a
// handful loses only the excess.
func managementReceiptEvictionHeadroom(capacity, unit int) int {
	headroom := capacity / 16
	if ceiling := managementMaximumEntriesPerHandle * unit; headroom > ceiling {
		headroom = ceiling
	}
	return headroom
}

func (m *Microsoft) ensureManagementSubscription(ctx context.Context, rt hosted.Runtime, key string, dataset Dataset, state *managementState) (bool, error) {
	endpoint := m.managementFeedURL("subscriptions/list", nil)
	response, err := m.managementRequest(ctx, rt, http.MethodGet, endpoint)
	if err != nil {
		return false, err
	}
	subscriptions, err := decodeManagementSubscriptions(response.Body, managementMaximumSubscriptions)
	if err != nil {
		return false, err
	}
	status := ""
	for _, subscription := range subscriptions {
		if len(subscription.ContentType) > 128 || len(subscription.Status) > 64 {
			return false, errors.New("Microsoft Management Activity returned an invalid subscription descriptor")
		}
		if subscription.ContentType == dataset.ContentType {
			status = strings.ToLower(strings.TrimSpace(subscription.Status))
			break
		}
	}
	state.LastSubscriptionCheck = m.now().UTC()
	state.SubscriptionStatus = status
	if status == "enabled" {
		if err := m.persistManagementState(rt, key, state); err != nil {
			return false, err
		}
		return true, nil
	}
	if !state.LastStartSuccess.IsZero() && m.now().UTC().Sub(state.LastStartSuccess) < managementStartSuppression {
		// The source still does not report this content type as enabled and the
		// bounded suppression interval says not to ask again yet, so this poll
		// collects nothing. It returns no error, and hosted/job.go calls
		// ClearError() after every poll that returns none, so without this the
		// selector went quiet with nothing an operator could see.
		observed := state.SubscriptionStatus
		if observed == "" {
			observed = "absent"
		}
		// The status is a source-supplied string the subscription descriptor
		// bounds at 64 bytes and nothing else constrains, so every
		// operator-visible insertion of it is escaped once here and both the
		// structured log and the status channel report that one presentation.
		// A legitimate status is unchanged by the escape, so "disabled" reads
		// as "disabled"; an interior CR LF no longer ends either record.
		displayed := displayManagementSubscriptionStatus(observed)
		rt.Warn("Microsoft Management Activity subscription is not enabled; the start request is suppressed and this poll collects nothing",
			log.KV("selector", dataset.Name), log.KV("content_type", dataset.ContentType),
			log.KV("status", displayed),
			log.KV("last_start", state.LastStartSuccess.UTC().Format(time.RFC3339)),
			log.KV("suppressed_for", managementStartSuppression.String()))
		rt.SetWarn(fmt.Sprintf(`Microsoft Management Activity selector %s is collecting nothing: the source reports the %s subscription as "%s" and the start request is suppressed for %s after the last one`,
			dataset.Name, dataset.ContentType, displayed, managementStartSuppression))
		if err := m.persistManagementCheckpoint(rt, key, state); err != nil {
			return false, err
		}
		return false, nil
	}
	start := m.managementFeedURL("subscriptions/start", url.Values{"contentType": {dataset.ContentType}})
	if _, err := m.managementRequest(ctx, rt, http.MethodPost, start); err != nil {
		return false, err
	}
	state.LastStartSuccess = m.now().UTC()
	// A start call is a request, not an observation. Recording "enabled" here
	// claimed a status no list response had confirmed, which is exactly the
	// status the suppression branch above has to report when the source
	// disagrees. Collection still proceeds on the strength of the accepted
	// start; the next list is what confirms it.
	state.SubscriptionStatus = "start-requested"
	if err := m.persistManagementState(rt, key, state); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Microsoft) startManagementWindow(state *managementState) bool {
	if state.TargetThrough.IsZero() {
		state.TargetThrough = m.now().UTC().Truncate(time.Second)
	}
	end := state.TargetThrough
	oldest := m.managementRetentionFloor()
	if !end.After(oldest) {
		end = m.now().UTC().Truncate(time.Second)
		state.TargetThrough = end
	}
	start := state.CompletedThrough.UTC()
	if start.IsZero() {
		start = end.Add(-m.conf.LookbackDuration())
	}
	if start.Before(oldest) {
		start = oldest
	}
	if !start.Before(end) {
		state.TargetThrough = time.Time{}
		return false
	}
	windowEnd := start.Add(managementMaximumWindow)
	if windowEnd.After(end) {
		windowEnd = end
	}
	state.WindowStart, state.WindowEnd = start, windowEnd
	state.ListingStarted, state.ListingComplete = false, false
	state.NextPage, state.RecentPages = "", nil
	state.Pages, state.ContentIndex = 0, 0
	state.resetContents()
	state.ActiveContentID, state.ActiveDigest, state.EventIndex, state.ActiveDigestChanges = "", "", 0, 0
	return true
}

// reportManagementRetentionLoss records an interval the Management Activity API
// can no longer serve. The loss is permanent and unrecoverable by any later
// reconfiguration, so the skipped duration is stated explicitly and published on
// the status channel the poll loop leaves alone. The error channel cannot carry
// it: the retention paths continue and return a continuation with a nil error,
// and hosted/job.go calls ClearError() after every poll that returns none --
// including this one, microseconds after it was set -- so an error here would
// never reach the status API that operators actually watch. ClearWarn() is only
// reached from the ingest-link recovery path, so the condition stays visible.
func (m *Microsoft) reportManagementRetentionLoss(rt hosted.Runtime, dataset Dataset, message string, unavailableFrom, resumedAt time.Time) {
	skipped := resumedAt.Sub(unavailableFrom)
	rt.Error(message,
		log.KV("selector", dataset.Name),
		log.KV("unavailable_from", unavailableFrom.UTC().Format(time.RFC3339)),
		log.KV("resumed_at", resumedAt.UTC().Format(time.RFC3339)),
		log.KV("skipped", skipped.String()))
	rt.SetWarn(fmt.Sprintf("Microsoft Management Activity selector %s permanently skipped %s of audit history from %s; the source retains only %s",
		dataset.Name, skipped, unavailableFrom.UTC().Format(time.RFC3339), managementMaximumHistory))
}

// reportManagementContentLoss records content blobs the source can no longer
// serve. The loss is permanent and bounded to the named descriptors, so the
// exact count and the exact contentCreated range are accumulated in the state
// document and republished on the status channel the poll loop leaves alone.
// delivered names how many events of the first descriptor had already been
// accepted, which is non-zero only when a blob expired part way through its
// own delivery.
func (m *Microsoft) reportManagementContentLoss(rt hosted.Runtime, dataset Dataset, state *managementState, skipped []managementContent, delivered int, message string) {
	if len(skipped) == 0 {
		return
	}
	state.SkippedContents += len(skipped)
	for _, content := range skipped {
		created := content.ContentCreated.UTC()
		if state.SkippedFrom.IsZero() || created.Before(state.SkippedFrom) {
			state.SkippedFrom = created
		}
		if created.After(state.SkippedThrough) {
			state.SkippedThrough = created
		}
	}
	rt.Error(message,
		log.KV("selector", dataset.Name),
		log.KV("content_id", displayManagementContentID(skipped[0].ContentID)),
		log.KV("content_created", skipped[0].ContentCreated.UTC().Format(time.RFC3339)),
		log.KV("skipped_contents", len(skipped)),
		log.KV("delivered_events", delivered),
		log.KV("skipped_total", state.SkippedContents))
	rt.SetWarn(fmt.Sprintf("Microsoft Management Activity selector %s permanently skipped %d audit content blobs the source can no longer serve, created between %s and %s; the source retains only %s",
		dataset.Name, state.SkippedContents,
		state.SkippedFrom.Format(time.RFC3339), state.SkippedThrough.Format(time.RFC3339), managementMaximumHistory))
}

func (m *Microsoft) managementRetentionFloor() time.Time {
	return m.now().UTC().Truncate(time.Second).Add(-managementMaximumHistory).Add(managementRetentionDispatchMargin)
}

// expiredManagementDescriptors returns the undelivered descriptors whose blobs
// are already past the retention floor. Anything newer is still retrievable and
// is re-listed by the window that replaces this one, so it is not loss.
func expiredManagementDescriptors(state *managementState, oldest time.Time) []managementContent {
	var expired []managementContent
	for index := state.ContentIndex; index >= 0 && index < len(state.Contents); index++ {
		if state.Contents[index].ContentCreated.Before(oldest) {
			expired = append(expired, state.Contents[index])
		}
	}
	return expired
}

// dropExpiredManagementWindow abandons the undelivered descriptors of a window
// whose own end has crossed the retention floor. Every blob it still points at
// was created before the floor, so no refresh finds it and the re-list the
// refresh needs is rejected for a startTime outside retention. The checkpoint is
// advanced to the end of the dead window so the next poll plans forward instead
// of re-reading the same descriptor forever; the interval itself is reported as
// permanent loss by both this call and the checkpoint check on the next pass.
func (m *Microsoft) dropExpiredManagementWindow(state *managementState) ([]managementContent, bool) {
	if !state.ListingComplete || state.WindowEnd.IsZero() || state.ContentIndex >= len(state.Contents) {
		return nil, false
	}
	oldest := m.managementRetentionFloor()
	if state.WindowEnd.After(oldest) {
		return nil, false
	}
	expired := expiredManagementDescriptors(state, oldest)
	state.CompletedThrough = state.WindowEnd
	state.WindowStart, state.WindowEnd = time.Time{}, time.Time{}
	state.ListingStarted, state.ListingComplete = false, false
	state.NextPage, state.RecentPages = "", nil
	state.Pages, state.ContentIndex = 0, 0
	state.resetContents()
	state.ActiveContentID, state.ActiveDigest, state.EventIndex, state.ActiveDigestChanges = "", "", 0, 0
	return expired, true
}

func (m *Microsoft) rebaseExpiredManagementListing(state *managementState) (bool, time.Time, time.Time, []managementContent, error) {
	if state.ListingComplete || state.WindowStart.IsZero() {
		return false, time.Time{}, time.Time{}, nil, nil
	}
	oldest := m.managementRetentionFloor()
	if !state.WindowStart.Before(oldest) {
		return false, time.Time{}, time.Time{}, nil, nil
	}
	// Delivery having begun used to make this a hard error, which nothing could
	// ever clear: the window only ages further, so every later poll produced the
	// same error. The descriptors still undelivered and already past the floor
	// are reported as permanent loss instead, and everything newer is re-listed
	// by the rebased window.
	expired := expiredManagementDescriptors(state, oldest)
	unavailableFrom := state.WindowStart
	if !state.TargetThrough.After(oldest) {
		state.TargetThrough = m.now().UTC().Truncate(time.Second)
	}
	state.WindowStart = oldest
	state.WindowEnd = oldest.Add(managementMaximumWindow)
	if state.WindowEnd.After(state.TargetThrough) {
		state.WindowEnd = state.TargetThrough
	}
	if !state.WindowStart.Before(state.WindowEnd) {
		return false, time.Time{}, time.Time{}, nil, errors.New("Microsoft Management Activity retention rebase produced an empty window")
	}
	state.ListingStarted, state.ListingComplete = false, false
	state.NextPage, state.RecentPages = "", nil
	state.Pages, state.ContentIndex = 0, 0
	state.resetContents()
	state.ActiveContentID, state.ActiveDigest, state.EventIndex, state.ActiveDigestChanges = "", "", 0, 0
	state.ImmediateRuns = 0
	return true, unavailableFrom, state.WindowStart, expired, nil
}

// narrowManagementWindow halves the window whose listing overflowed the
// descriptor bound and restarts that listing. The remainder of the original
// window is collected by the following window, because the completion path
// resumes from CompletedThrough while TargetThrough is still outstanding.
// Nothing is dropped and nothing is delivered twice: a listing only overflows
// before delivery starts, which startManagementWindow and this function both
// assert by resetting ContentIndex together with Contents. A window that is
// already at the floor cannot be narrowed further, and that remains a hard
// error: more than a capacity's worth of content blobs inside one minute is not
// a shape the source produces.
func narrowManagementWindow(state *managementState) bool {
	span := state.WindowEnd.Sub(state.WindowStart)
	if span <= managementMinimumWindow {
		return false
	}
	span /= 2
	// Keep the boundary at the precision managementContentListURL can express.
	// Halving without truncation produced sub-second spans after eight
	// narrowings (24h down to 5m37.5s), and the completion path then carried the
	// fraction into CompletedThrough and so into the next window's start, where
	// the truncated wire query legitimately returned a descriptor in the
	// sub-second gap that the local half-open assertion rejected.
	span = span.Truncate(managementWireWindowPrecision)
	if span < managementMinimumWindow {
		span = managementMinimumWindow
	}
	state.WindowEnd = state.WindowStart.Add(span)
	state.ListingStarted, state.ListingComplete = false, false
	state.NextPage, state.RecentPages = "", nil
	state.Pages, state.ContentIndex = 0, 0
	state.resetContents()
	state.ActiveContentID, state.ActiveDigest, state.EventIndex, state.ActiveDigestChanges = "", "", 0, 0
	// The listing restarts from the same WindowStart, so the refusals of every
	// page it had already charged are about to be published and refused again.
	// Restoring the listing-start snapshot is what keeps each refused
	// descriptor counted exactly once: the re-list charges them, once, as its
	// own progress becomes durable, and the charge the discarded listing made
	// is given back in the same transaction that discards it.
	state.restoreRefusalListingStart()
	return true
}

// snapshotRefusalListingStart records the refusal accounting this window's
// listing starts from, unless a snapshot for this same WindowStart is already
// durable -- which is exactly the state a narrowed, resumed or restarted
// listing is in, and re-taking it there would snapshot the charges the restore
// exists to give back.
func (state *managementState) snapshotRefusalListingStart() {
	if state.RefusedListingStart != nil && state.RefusedListingStart.WindowStart.Equal(state.WindowStart) {
		return
	}
	state.RefusedListingStart = &managementRefusalSnapshot{
		WindowStart: state.WindowStart,
		Contents:    state.RefusedContents,
		Unreported:  state.RefusedUnreported,
		Reason:      state.RefusedReason,
	}
}

// restoreRefusalListingStart gives back everything the listing of this window
// has charged so far. A snapshot taken for a different WindowStart cannot
// describe this listing's prefix and is not applied; a document written by an
// earlier build carries none at all, and leaving the accounting alone is the
// behaviour it already had.
func (state *managementState) restoreRefusalListingStart() {
	snapshot := state.RefusedListingStart
	if snapshot == nil || !snapshot.WindowStart.Equal(state.WindowStart) {
		return
	}
	state.RefusedContents, state.RefusedUnreported, state.RefusedReason = snapshot.Contents, snapshot.Unreported, snapshot.Reason
}

// managementContentInRequestedWindow reports whether a descriptor falls inside
// the half-open window the listing query actually expressed.
// managementContentListURL formats startTime with time.RFC3339, which drops
// fractional seconds, so a stored window whose start carries a fraction asks the
// source for slightly more than it holds. A descriptor in that sub-second gap is
// a legitimate answer to the request and is collected rather than rejected.
func managementContentInRequestedWindow(created, start, end time.Time) bool {
	return !created.Before(start.Truncate(managementWireWindowPrecision)) && created.Before(end)
}

func (m *Microsoft) listManagementContents(ctx context.Context, rt hosted.Runtime, key string, dataset Dataset, state *managementState) error {
	truncated, truncatedReason := false, ""
	// Taken before the first page can charge anything and persisted by that
	// same page's transaction, so a narrowing has somewhere to restore from
	// however many passes or restarts separate it from here.
	state.snapshotRefusalListingStart()
	for !state.ListingComplete {
		if state.Pages >= managementMaximumPagesPerWindow {
			if !narrowManagementWindow(state) {
				return fmt.Errorf("Microsoft Management Activity content listing exceeds %d pages", managementMaximumPagesPerWindow)
			}
			rt.Warn("Microsoft Management Activity listing window narrowed at page ceiling",
				log.KV("selector", dataset.Name), log.KV("pages", managementMaximumPagesPerWindow),
				log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339)),
				log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339)))
			if err := m.persistManagementState(rt, key, state); err != nil {
				return err
			}
			continue
		}
		endpoint := state.NextPage
		if endpoint == "" {
			endpoint = m.managementContentListURL(dataset.ContentType, state.WindowStart, state.WindowEnd)
		} else if err := m.client.validateManagementURL(endpoint); err != nil {
			// A continuation persisted by an earlier build, under a URL policy
			// this one no longer agrees with, is refused here rather than on
			// every poll for the rest of the window's life in retention.
			truncated, truncatedReason = true, m.safePollError(err)
			state.NextPage, state.ListingComplete = "", true
			if err := m.persistManagementState(rt, key, state); err != nil {
				return err
			}
			break
		}
		if stringSliceContains(state.RecentPages, endpoint) {
			// A continuation the source has already served cannot be followed
			// again, and both sides of that decision are durable: the page
			// history and the continuation itself are persisted by the page that
			// published them. Returning the repeat as this pass's error therefore
			// left exactly the same NextPage and the same RecentPages on disk, so
			// every later poll read them back, refused the same URI before
			// issuing a request, and produced the same error until the window
			// crossed the seven-day retention floor and the whole interval was
			// lost with it. End the listing here the same way a policy-refused
			// continuation does: the pages already listed stay durable and are
			// delivered, the loss is bounded to whatever sat behind the repeat,
			// and the window that follows starts exactly at this WindowEnd.
			truncated, truncatedReason = true, "the source repeated a NextPageUri this window had already followed"
			state.NextPage, state.ListingComplete = "", true
			if err := m.persistManagementState(rt, key, state); err != nil {
				return err
			}
			break
		}
		page, err := m.managementListPage(ctx, rt, dataset, endpoint)
		if err != nil {
			// One page carrying more descriptors than the state document may
			// hold for a whole window is the same condition as the capacity
			// overflow below, reached a page earlier. Narrow and list again
			// rather than failing on the same page on every poll forever.
			if errors.Is(err, errManagementContentPageOverflow) {
				if !narrowManagementWindow(state) {
					return fmt.Errorf("Microsoft Management Activity content page exceeded descriptor capacity %d", m.contentPageCapacity())
				}
				rt.Warn("Microsoft Management Activity listing window narrowed at a descriptor-dense page",
					log.KV("selector", dataset.Name), log.KV("capacity", m.contentPageCapacity()),
					log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339)),
					log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339)))
				if err := m.persistManagementState(rt, key, state); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if page.truncated {
			truncated, truncatedReason = true, page.truncatedReason
		}
		narrowed := false
		outside, firstOutside := 0, managementContent{}
		for _, content := range page.contents {
			// The dedup skip runs first. A descriptor this build already holds or
			// has already accepted must never be fatal, whatever the source
			// says its contentCreated is.
			if state.hasContent(content.ContentID) || state.hasReceipt(content.ContentID) {
				continue
			}
			if !managementContentInRequestedWindow(content.ContentCreated, state.WindowStart, state.WindowEnd) {
				// A descriptor genuinely outside the requested range belongs to
				// the window that does cover it: the next window starts exactly
				// at this WindowEnd. Report and skip it rather than failing the
				// listing, which stalled the selector on the persisted NextPage
				// until the window crossed the retention floor and the whole
				// interval was lost.
				if outside == 0 {
					firstOutside = content
				}
				outside++
				continue
			}
			if len(state.Contents) >= m.descriptorCapacity() {
				// The window published more content than the state document may
				// hold. Narrow it and list again rather than failing: the error
				// this replaced could never clear, because the next poll planned
				// the same window and overflowed at the same descriptor.
				if !narrowManagementWindow(state) {
					return fmt.Errorf("Microsoft Management Activity descriptor capacity %d exceeded", m.descriptorCapacity())
				}
				rt.Warn("Microsoft Management Activity listing window narrowed at descriptor capacity",
					log.KV("selector", dataset.Name), log.KV("capacity", m.descriptorCapacity()),
					log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339)),
					log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339)))
				narrowed = true
				break
			}
			if retained, charge := state.descriptorBytes(), managementDescriptorBytes(content); retained+charge > m.descriptorByteCapacity() {
				// The window published more descriptor bytes than the state
				// document may hold. Narrow it and list again for the same
				// reason the count bound does: a descriptor set the persist path
				// would refuse is one no later poll could get past, and the
				// remainder of the window is collected by the window that
				// follows because TargetThrough is still outstanding. Nothing
				// already listed is dropped and CompletedThrough does not move.
				if !narrowManagementWindow(state) {
					return fmt.Errorf("Microsoft Management Activity descriptor byte capacity %d exceeded", m.descriptorByteCapacity())
				}
				rt.Warn("Microsoft Management Activity listing window narrowed at descriptor byte capacity",
					log.KV("selector", dataset.Name), log.KV("capacity", m.descriptorByteCapacity()),
					log.KV("retained_bytes", retained), log.KV("descriptor_bytes", charge),
					log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339)),
					log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339)))
				narrowed = true
				break
			}
			state.appendContent(content)
		}
		if outside > 0 {
			rt.Warn("Microsoft Management Activity skipped content descriptors outside the requested window",
				log.KV("selector", dataset.Name), log.KV("count", outside),
				log.KV("content_id", displayManagementContentID(firstOutside.ContentID)),
				log.KV("content_created", firstOutside.ContentCreated.UTC().Format(time.RFC3339Nano)),
				log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339Nano)),
				log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339Nano)))
		}
		if narrowed {
			// The narrowed window re-lists from its own start, so this page's
			// descriptors -- including the ones it refused -- are published
			// again and refused again. Charging them now would count the same
			// permanent loss twice, so nothing from a discarded page is counted:
			// the re-list is what counts it, once, when its own progress becomes
			// durable.
			//
			// The pages before this one are the other half of that, and leaving
			// them charged was the double count this snapshot repairs: their
			// refusals were already durable, the re-list publishes and refuses
			// exactly the same descriptors, and it charges them again.
			// narrowManagementWindow has given the whole prefix back, in this
			// same transaction, so the re-list starts from the accounting the
			// discarded listing started from.
			//
			// The pass-local truncation report is part of that prefix. This page
			// may have published a continuation the URL policy refuses, which
			// sets it and leaves page.next empty; the page is then discarded
			// here, the narrowed window lists the same interval again, and
			// whatever continuation that re-listing publishes is what gets
			// reported. Carrying the flag across the discard reported a refused
			// continuation the state document no longer holds: a listing that
			// went on to complete cleanly still warned about a truncated window
			// and still returned the refusal as this pass's error.
			truncated, truncatedReason = false, ""
			if err := m.persistManagementState(rt, key, state); err != nil {
				return err
			}
			continue
		}
		// A refused descriptor is permanent loss of exactly that content. The
		// policy refusal stands and the descriptor is never admitted, so the
		// count and the first reason are folded into the same state transaction
		// that advances the listing past the page that published them. That is
		// what makes the charge exactly-once in both directions: a persist that
		// fails charges nothing and the page is requested again, and one that
		// succeeds moves the continuation past a page no later poll can see.
		if page.refused > 0 {
			state.RefusedContents += page.refused
			state.RefusedUnreported += page.refused
			if state.RefusedReason == "" {
				// Stored canonically and bounded, never as a presentation of
				// itself. Escaping it here and escaping it again where it is
				// displayed would show an operator a doubly escaped reason, and
				// escaping it only here left the reason a hostile state store
				// supplies -- which is length-checked and nothing else -- to
				// reach log.KV unneutralized. The presentation contract is
				// applied once, at every insertion, so a restored reason and a
				// reason this pass generated follow exactly one contract.
				state.RefusedReason = managementRefusedReason(page.refusedReason)
			}
		}
		state.ListingStarted = true
		state.Pages++
		state.RecentPages = appendRecentString(state.RecentPages, endpoint, managementMaximumContinuationRecent)
		state.NextPage = page.next
		state.ListingComplete = page.next == ""
		if err := m.persistManagementState(rt, key, state); err != nil {
			return err
		}
	}
	return m.reportManagementRefusedListing(rt, key, dataset, state, truncated, truncatedReason)
}

// reportManagementRefusedListing publishes what the listing had to refuse and
// returns it as this pass's error. The listing progress is already durable when
// this runs, so the selector keeps moving -- the next poll delivers what was
// listed and goes on to the next window -- while the condition still reaches
// both the log and the status channel the poll loop leaves alone.
//
// What it reports is the durable count, not a pass-local one. Every refusal was
// charged to the state document by the transaction that advanced the listing
// past it, so refusals a pass could not get as far as reporting -- because a
// later page failed, or because the runner restarted -- are still here to be
// reported by the pass that can. The reported part is acknowledged in the state
// document before this returns, so the report is published once rather than on
// every later poll; the cumulative RefusedContents is never cleared and is named
// in the report, which is what keeps the loss visible after the status channel
// has been cleared.
//
// A failed acknowledgement is returned as this pass's error with the count
// unchanged, so the next pass reports the same refusals again. The report can
// repeat; the count cannot double.
//
// Both reasons are inserted through displayVendorValue, here and nowhere else.
// RefusedReason is a durable field, so the bytes behind it are whatever the
// state store holds: load validation bounds its length and says nothing at all
// about its contents, and a restored reason carrying CR, LF, a Unicode Cf
// format character or a Zl/Zp separator used to reach log.KV and the returned
// error unneutralized, where the default RFC5424 file sink writes it straight
// through and the record ends mid-value (CWE-117). Escaping it here instead of
// where it is written is also what keeps one reason from being escaped twice.
func (m *Microsoft) reportManagementRefusedListing(rt hosted.Runtime, key string, dataset Dataset, state *managementState, truncated bool, truncatedReason string) error {
	refused := state.RefusedUnreported
	if refused == 0 && !truncated {
		return nil
	}
	refusedReason := displayVendorValue(state.RefusedReason, maximumDisplayedVendorValue)
	displayedTruncatedReason := displayVendorValue(truncatedReason, maximumDisplayedVendorValue)
	if refused > 0 {
		rt.Warn("Microsoft Management Activity refused content descriptors the source published; that content is permanently skipped",
			log.KV("selector", dataset.Name), log.KV("count", refused), log.KV("reason", refusedReason),
			log.KV("refused_total", state.RefusedContents),
			log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339)),
			log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339)))
	}
	if truncated {
		rt.Warn("Microsoft Management Activity refused a listing continuation; the window is listed only as far as the refused page",
			log.KV("selector", dataset.Name), log.KV("reason", displayedTruncatedReason),
			log.KV("window_start", state.WindowStart.UTC().Format(time.RFC3339)),
			log.KV("window_end", state.WindowEnd.UTC().Format(time.RFC3339)))
	}
	rt.SetWarn(fmt.Sprintf("Microsoft Management Activity selector %s refused %d content descriptor(s) and %d continuation(s) the source published for the window ending %s; that content was not collected, and %d descriptor(s) have been permanently refused for this selector in total",
		dataset.Name, refused, boolToInt(truncated), state.WindowEnd.UTC().Format(time.RFC3339), state.RefusedContents))
	if refused > 0 {
		state.RefusedUnreported, state.RefusedReason = 0, ""
		if err := m.persistManagementState(rt, key, state); err != nil {
			return err
		}
	}
	if truncated {
		return fmt.Errorf("Microsoft Management Activity refused %d content descriptor(s) and a listing continuation for %s: %s",
			refused, dataset.Name, firstNonEmpty(displayedTruncatedReason, refusedReason))
	}
	return fmt.Errorf("Microsoft Management Activity refused %d content descriptor(s) for %s: %s", refused, dataset.Name, refusedReason)
}

// managementRefusedReason bounds the reason the listing records durably. The
// stored field carries the canonical sentence, bounded at the same number of
// input bytes every other operator-visible vendor value is bounded at, and the
// escape is applied where it is inserted into a record rather than here --
// which is what makes one reason escaped exactly once however many passes read
// it back and report it.
//
// The bound never cuts a rune in half. The stored reason is text an operator
// reads through displayVendorValue, and half a rune is not text: it would be
// presented as the replacement escape for bytes the reason never held.
func managementRefusedReason(reason string) string {
	if len(reason) <= maximumDisplayedVendorValue {
		return reason
	}
	bounded := reason[:maximumDisplayedVendorValue]
	for len(bounded) > 0 {
		if r, size := utf8.DecodeLastRuneInString(bounded); r != utf8.RuneError || size > 1 {
			break
		}
		bounded = bounded[:len(bounded)-1]
	}
	return bounded + displayTruncationMarker
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// managementPage is one decoded content-listing page.
//
// A descriptor this policy refuses and a continuation this policy refuses are
// both bounded loss of one item. Returning either as the page's error made them
// unbounded loss of the whole selector: the listing failed on the same page on
// every poll, the window could never be completed, and the interval it covers
// was collected by nobody until it crossed the source's retention floor. They
// are counted here instead, and the listing loop reports them once it has
// persisted the progress it did make.
type managementPage struct {
	contents []managementContent
	next     string
	// refused counts descriptors this page published that the descriptor
	// policy rejects, with the first reason for the operator.
	refused       int
	refusedReason string
	// truncated marks a continuation this page published that the URL policy
	// rejects. The listing ends here rather than following it.
	truncated       bool
	truncatedReason string
}

func (m *Microsoft) managementListPage(ctx context.Context, rt hosted.Storage, dataset Dataset, endpoint string) (managementPage, error) {
	if err := m.client.validateManagementURL(endpoint); err != nil {
		return managementPage{}, err
	}
	parsed, _ := url.Parse(endpoint)
	if got := parsed.Query().Get("contentType"); got != dataset.ContentType {
		return managementPage{}, errors.New("Microsoft Management Activity content page changed contentType")
	}
	response, err := m.managementRequest(ctx, rt, http.MethodGet, endpoint)
	if err != nil {
		return managementPage{}, err
	}
	wire, err := decodeManagementContentPage(response.Body, m.contentPageCapacity())
	if err != nil {
		return managementPage{}, err
	}
	page := managementPage{contents: make([]managementContent, 0, len(wire))}
	for _, item := range wire {
		content, err := m.decodeManagementContent(dataset, item)
		if err != nil {
			page.refused++
			if page.refusedReason == "" {
				page.refusedReason = m.safePollError(err)
			}
			continue
		}
		page.contents = append(page.contents, content)
	}
	next := strings.TrimSpace(response.Header.Get("NextPageUri"))
	if next == "" {
		return page, nil
	}
	if err := m.client.validateManagementURL(next); err != nil {
		page.truncated, page.truncatedReason = true, m.safePollError(err)
		return page, nil
	}
	nextURL, _ := url.Parse(next)
	if nextURL.Query().Get("contentType") != dataset.ContentType {
		page.truncated, page.truncatedReason = true, "the continuation changed contentType"
		return page, nil
	}
	page.next = next
	return page, nil
}

func (m *Microsoft) decodeManagementContent(dataset Dataset, item managementContentWire) (managementContent, error) {
	if item.ContentType != dataset.ContentType || strings.TrimSpace(item.ContentID) == "" || len(item.ContentID) > managementMaximumContentIDLength {
		return managementContent{}, errors.New("Microsoft Management Activity returned an invalid content descriptor")
	}
	// validateManagementURL bounds the contentUri, which is the field that
	// dominates a persisted descriptor; the timestamps are bounded here so a
	// megabyte-long string from inside the response bound never reaches
	// parseTimestamp or the descriptor document.
	if err := m.client.validateManagementURL(item.ContentURI); err != nil {
		return managementContent{}, err
	}
	if len(item.ContentCreated) > managementMaximumTimestampLength || len(item.ContentExpiration) > managementMaximumTimestampLength {
		return managementContent{}, errors.New("Microsoft Management Activity content descriptor has an oversized timestamp")
	}
	created := parseTimestamp(item.ContentCreated)
	if created.IsZero() {
		return managementContent{}, errors.New("Microsoft Management Activity content descriptor has an invalid contentCreated")
	}
	expires := parseTimestamp(item.ContentExpiration)
	if item.ContentExpiration != "" && expires.IsZero() {
		return managementContent{}, errors.New("Microsoft Management Activity content descriptor has an invalid contentExpiration")
	}
	return managementContent{ContentType: item.ContentType, ContentID: item.ContentID, ContentURI: item.ContentURI, ContentCreated: created, ContentExpiration: expires}, nil
}

// managementContentUnavailableError marks a content blob that the source can
// never serve again: it has aged out of the seven-day retention, so the
// descriptor cannot be refreshed and the window it belongs to cannot be
// re-listed. It is reported as bounded permanent loss rather than retried,
// because no later poll produces a different answer.
type managementContentUnavailableError struct {
	contentID string
	reason    string
}

func (e *managementContentUnavailableError) Error() string {
	return "Microsoft Management Activity content is no longer available from the source: " + e.reason
}

var errManagementContentNotListed = errors.New("Microsoft Management Activity expired content could not be refreshed")

// managementContentUnavailable classifies a failed refresh of an expired blob.
// Either the source no longer lists the identifier at all, or it refuses the
// re-list for a window that is now outside retention. Any other failure stays
// retryable.
func managementContentUnavailable(contentID string, err error) *managementContentUnavailableError {
	if errors.Is(err, errManagementContentNotListed) {
		return &managementContentUnavailableError{contentID: contentID, reason: "the source no longer lists the content identifier"}
	}
	var httpErr *managementHTTPError
	if errors.As(err, &httpErr) && (httpErr.expiredContent() || httpErr.status == http.StatusBadRequest) {
		return &managementContentUnavailableError{contentID: contentID, reason: "the source refused to re-list the window the content belongs to"}
	}
	return nil
}

func (m *Microsoft) fetchManagementContent(ctx context.Context, rt hosted.Runtime, key string, dataset Dataset, state *managementState, index int) ([]json.RawMessage, string, error) {
	content := state.Contents[index]
	response, err := m.managementRequest(ctx, rt, http.MethodGet, content.ContentURI)
	if err != nil {
		var httpErr *managementHTTPError
		if !errors.As(err, &httpErr) || !httpErr.expiredContent() {
			return nil, "", err
		}
		refreshed, refreshErr := m.refreshManagementContent(ctx, rt, dataset, *state, content.ContentID)
		if refreshErr != nil {
			if unavailable := managementContentUnavailable(content.ContentID, refreshErr); unavailable != nil {
				return nil, "", unavailable
			}
			return nil, "", refreshErr
		}
		// A refreshed descriptor is individually valid -- decodeManagementContent
		// has already held its contentUri to the Management URL policy, its
		// origin, its path prefix and managementMaximumURLLength -- and can still
		// be longer than the one it replaces, so retaining it can push the
		// aggregate descriptor budget over.
		//
		// The replacement used to be applied before the persist that validates
		// it. The persist then refused the whole state transaction on the
		// aggregate bound, nothing was written, and the next poll read back the
		// same expired descriptor, refreshed it to the same individually valid
		// over-budget URI and failed identically: a recoverable budget became
		// permanent cessation of collection for the content type, every poll,
		// until the window crossed the source's seven-day retention floor and the
		// whole interval was lost with it.
		//
		// Preflight it against the aggregate before mutating anything. A
		// replacement that fits is retained and persisted exactly as before. One
		// that does not is still used for this fetch -- it is what the source just
		// published for this contentId, and the policy has already accepted it --
		// and simply not written down. Delivery proceeds, the per-event ordinal
		// and the completion are persisted against the descriptor the set already
		// holds, the receipt names the same contentId, and the only cost is that a
		// later pass which has to fetch this same blob refreshes it again. Nothing
		// over budget becomes durable and nothing stalls.
		if state.descriptorReplacementBytes(index, refreshed) <= m.descriptorByteCapacity() {
			state.replaceContent(index, refreshed)
			if persistErr := m.persistManagementState(rt, key, state); persistErr != nil {
				return nil, "", persistErr
			}
		} else {
			rt.Warn("Microsoft Management Activity refreshed content URI is longer than the descriptor byte budget has room for; it is used for this fetch and not retained",
				log.KV("selector", dataset.Name),
				log.KV("content_id", displayManagementContentID(content.ContentID)),
				log.KV("capacity", m.descriptorByteCapacity()),
				log.KV("retained_bytes", state.descriptorBytes()),
				log.KV("replacement_bytes", state.descriptorReplacementBytes(index, refreshed)))
		}
		content = refreshed
		response, err = m.managementRequest(ctx, rt, http.MethodGet, content.ContentURI)
		if err != nil {
			var retryErr *managementHTTPError
			if errors.As(err, &retryErr) && retryErr.expiredContent() {
				return nil, "", &managementContentUnavailableError{contentID: content.ContentID,
					reason: "the re-listed content URI is expired as well"}
			}
			return nil, "", err
		}
	}
	events, err := decodeManagementEvents(response.Body, managementMaximumEventsPerContent)
	if err != nil {
		return nil, "", err
	}
	digest, err := managementContentDigest(events)
	if err != nil {
		return nil, "", err
	}
	return events, digest, nil
}

// These tag what one event contributed to the content digest, so a canonical
// form and an oversized marker can never be read as each other.
const (
	managementDigestCanonicalFrame = 'C'
	managementDigestOversizedFrame = 'O'
)

// managementEventOversized reports whether one raw content event exceeds the
// per-entry bound delivery enforces. managementContentDigest applies the
// identical bound before it materializes an event at all, so the digest and
// prepareManagementEvent can never disagree about which events delivery skips.
func managementEventOversized(raw json.RawMessage) bool {
	return len(raw) > managementMaximumEntrySize
}

// managementContentDigest identifies the ordered JSON values rather than the
// response bytes. Microsoft can serve one immutable contentId from different
// backend nodes, and insignificant whitespace or object-member ordering must
// not reset an in-progress ordinal on every request. UseNumber preserves the
// source's numeric representation while encoding/json provides deterministic
// object-key ordering for the canonical form.
//
// Each event is folded into one hash as it is canonicalized. Decoding the whole
// array into a retained []any and marshalling one canonical array held a second
// whole copy of the content as a decoded graph and a third as canonical JSON,
// and encoding/json escapes '<', '>' and '&' into six-byte sequences by
// default: a well formed blob inside the 16 MiB response bound amplified into
// roughly six times its own size here, before delivery had refused a single
// event. So the per-entry delivery bound is applied first, and an event past it
// -- one delivery will skip -- contributes a fixed-size marker derived from the
// raw event rather than a copy or a canonical form, which still changes
// whenever the raw event does. Every contribution is framed with its kind and
// its length, so no two event sequences can fold to the same bytes.
func managementContentDigest(events []json.RawMessage) (string, error) {
	hash := sha256.New()
	var frame [9]byte
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	for _, raw := range events {
		if managementEventOversized(raw) {
			marker := sha256.Sum256(raw)
			frame[0] = managementDigestOversizedFrame
			binary.BigEndian.PutUint64(frame[1:], uint64(len(raw)))
			hash.Write(frame[:])
			hash.Write(marker[:])
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return "", errors.New("canonicalize Microsoft Management Activity content event")
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			return "", errors.New("Microsoft Management Activity content event contains trailing data")
		}
		canonical.Reset()
		if err := encoder.Encode(value); err != nil {
			return "", errors.New("canonicalize Microsoft Management Activity content")
		}
		// Encode terminates the value with a newline that is not part of it.
		encoded := bytes.TrimSuffix(canonical.Bytes(), []byte("\n"))
		frame[0] = managementDigestCanonicalFrame
		binary.BigEndian.PutUint64(frame[1:], uint64(len(encoded)))
		hash.Write(frame[:])
		hash.Write(encoded)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (m *Microsoft) refreshManagementContent(ctx context.Context, rt hosted.Storage, dataset Dataset, state managementState, contentID string) (managementContent, error) {
	endpoint := m.managementContentListURL(dataset.ContentType, state.WindowStart, state.WindowEnd)
	seen := make(map[string]struct{})
	for page := 0; page < managementMaximumPagesPerWindow; page++ {
		if _, ok := seen[endpoint]; ok {
			return managementContent{}, errors.New("Microsoft Management Activity refresh returned a repeated NextPageUri")
		}
		seen[endpoint] = struct{}{}
		page, err := m.managementListPage(ctx, rt, dataset, endpoint)
		if err != nil {
			return managementContent{}, err
		}
		for _, content := range page.contents {
			if content.ContentID == contentID {
				if !managementContentInRequestedWindow(content.ContentCreated, state.WindowStart, state.WindowEnd) {
					return managementContent{}, errors.New("Microsoft Management Activity refreshed content is outside the requested half-open window")
				}
				return content, nil
			}
		}
		// A refused descriptor or a refused continuation leaves the identifier
		// unfound, which is the condition the caller already treats as bounded
		// permanent loss of this one blob.
		if page.next == "" {
			return managementContent{}, errManagementContentNotListed
		}
		endpoint = page.next
	}
	return managementContent{}, fmt.Errorf("Microsoft Management Activity refresh exceeds %d pages", managementMaximumPagesPerWindow)
}

func prepareManagementEvent(dataset Dataset, raw json.RawMessage, fallback time.Time) (FetchedRecord, string, error) {
	if managementEventOversized(raw) {
		return FetchedRecord{}, "", fmt.Errorf("Microsoft Management Activity event exceeds %d bytes", managementMaximumEntrySize)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return FetchedRecord{}, "", errors.New("compact Microsoft Management Activity event")
	}
	payload := append([]byte(nil), compact.Bytes()...)
	if bytes.ContainsAny(payload, "\r\n") {
		return FetchedRecord{}, "", errors.New("compacted Microsoft Management Activity event contains a physical line break")
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil || object == nil {
		return FetchedRecord{}, "", errors.New("Microsoft Management Activity event must be a JSON object")
	}
	timestamp := parseTimestamp(stringValueAtPath(object, dataset.TimeField))
	if timestamp.IsZero() {
		timestamp = fallback
	}
	tagGroup := dataset.TagGroup
	workload := strings.ToLower(strings.TrimSpace(stringValueAtPath(object, "Workload")))
	switch dataset.ContentType {
	case "Audit.SharePoint":
		if workload == "onedrive" {
			tagGroup = "m365-onedrive"
		}
	case "Audit.General":
		if workload == "microsoftteams" {
			tagGroup = "m365-teams"
		}
	}
	id := stringValueAtPath(object, dataset.IDField)
	if id == "" {
		digest := sha256.Sum256(payload)
		id = "sha256:" + hex.EncodeToString(digest[:])
	}
	return FetchedRecord{Raw: payload, ID: id, Timestamp: timestamp}, tagGroup, nil
}

func (m *Microsoft) managementContinuation(rt hosted.Runtime, key string, state *managementState) (bool, error) {
	state.ImmediateRuns++
	pending := true
	if state.ImmediateRuns >= managementMaximumImmediateRuns {
		// validateManagementState caps the field, so the counter has to wrap.
		state.ImmediateRuns = 0
		// Falling back to Request-Interval here is what pinned a high-volume
		// tenant behind a fixed per-interval ceiling until its backlog fell off
		// the source's seven-day retention. Every caller has already made
		// bounded forward progress, so staying immediate while a catch-up
		// target is outstanding drains the backlog and still terminates: the
		// completion path clears TargetThrough once CompletedThrough reaches it.
		pending = !state.TargetThrough.IsZero() && state.CompletedThrough.Before(state.TargetThrough)
	}
	// Every continuation is a pass boundary, so the deferred receipt set is
	// flushed here: the next window must see every receipt this pass recorded.
	if err := m.persistManagementCheckpoint(rt, key, state); err != nil {
		return false, err
	}
	return pending, nil
}

func (m *Microsoft) managementFeedURL(operation string, query url.Values) string {
	base := strings.TrimRight(m.conf.Management_Host, "/") + "/api/v1.0/" + url.PathEscape(m.conf.Tenant_ID) + "/activity/feed/" + operation
	values := make(url.Values, len(query)+1)
	for key, items := range query {
		values[key] = slices.Clone(items)
	}
	values.Set("PublisherIdentifier", m.conf.Publisher_Identifier)
	return base + "?" + values.Encode()
}

func (m *Microsoft) managementContentListURL(contentType string, start, end time.Time) string {
	query := url.Values{
		"contentType": {contentType},
		"startTime":   {start.UTC().Format(time.RFC3339)},
		"endTime":     {end.UTC().Format(time.RFC3339)},
	}
	return m.managementFeedURL("subscriptions/content", query)
}

func (m *Microsoft) receiptCapacity() int {
	if m.managementReceiptLimit > 0 {
		return m.managementReceiptLimit
	}
	return managementMaximumReceipts
}

// receiptByteCapacity resolves the aggregate receipt-byte budget. It has to
// admit at least one receipt, which managementMaximumReceiptBytes does by five
// orders of magnitude: a receipt is bounded by
// managementMaximumContentIDLength plus its timestamp, so no single receipt can
// ever be refused by it.
//
// An override may only tighten the durable bound. loadManagementReceiptSet
// refuses a stored document over managementMaximumReceiptBytes, so a capacity
// above it would let a pass write the document the next poll replaces.
func (m *Microsoft) receiptByteCapacity() int {
	if m.managementReceiptByteLimit > 0 && m.managementReceiptByteLimit < managementMaximumReceiptBytes {
		return m.managementReceiptByteLimit
	}
	return managementMaximumReceiptBytes
}

func (m *Microsoft) descriptorCapacity() int {
	if m.managementDescriptorLimit > 0 {
		return m.managementDescriptorLimit
	}
	return managementMaximumDescriptors
}

// contentPageCapacity bounds how many descriptors one listing page may carry.
// A page cannot usefully carry more than a whole window may hold, and the
// decoder admits exactly one descriptor past that so the listing loop -- which
// has a window to narrow and an operator message to publish -- stays the thing
// that reports an ordinary capacity overflow. A page denser than that is
// refused by the decoder itself, before the descriptor that exceeds the bound
// is retained, and the listing narrows on that too.
func (m *Microsoft) contentPageCapacity() int {
	return m.descriptorCapacity() + 1
}

// descriptorByteCapacity resolves the cumulative descriptor-byte budget. It has
// to admit at least one descriptor, which managementMaximumDescriptorBytes does
// by three orders of magnitude: a descriptor is bounded by
// managementMaximumURLLength plus a 512-byte contentId, so the narrowing always
// has somewhere to go and a window never fails on its first descriptor.
func (m *Microsoft) descriptorByteCapacity() int {
	if m.managementDescriptorByteLimit > 0 {
		return m.managementDescriptorByteLimit
	}
	return managementMaximumDescriptorBytes
}

// trimManagementReceipts drops the oldest receipts by ContentCreated until the
// set fits its capacity bound, keeping the rest in order. Overflow used to be a
// hard error with no state write and nothing that could ever clear it: the next
// poll resumed at the same content, re-fetched it, found it already fully
// delivered, appended the same receipt and overflowed again, so a tenant that
// sustained more content blobs per retention window than the bound wedged that
// selector permanently. Dropping the oldest cannot re-deliver anything: a
// receipt is only consulted for the content at ContentIndex, which has already
// advanced past every receipt old enough to be dropped here, and the active
// window's own descriptors are recorded in Contents. The dropped receipts are
// returned so the membership index can forget exactly those identifiers instead
// of being rebuilt from the whole set.
func trimManagementReceipts(receipts []managementReceipt, maximum int) ([]managementReceipt, []managementReceipt) {
	if maximum < 1 || len(receipts) <= maximum {
		return receipts, nil
	}
	excess := len(receipts) - maximum
	ages := newManagementReceiptAgeHeap(receipts)
	removed := make(map[int]struct{}, excess)
	for range excess {
		removed[heap.Pop(ages).(int)] = struct{}{}
	}
	return partitionManagementReceipts(receipts, removed)
}

// trimManagementReceiptBytes drops the oldest receipts until the persisted
// receipt document fits managementMaximumReceiptBytes. total is the accounting
// the store already maintains, so the budget is tested in constant time and
// only the receipts that are actually evicted are re-measured.
//
// The count bound alone does not bound the bytes: managementMaximumReceipts
// receipts carrying identifiers near the 512-byte contentId ceiling are roughly
// 116 MB of JSON that every flushing pass re-marshals and re-fsyncs. This is
// the same recoverable eviction the count bound performs, keyed the same way,
// so a tenant whose identifiers are large loses its oldest receipts rather than
// its collection.
func trimManagementReceiptBytes(receipts []managementReceipt, total, maximumBytes int) ([]managementReceipt, []managementReceipt) {
	if maximumBytes < 1 || total <= maximumBytes {
		return receipts, nil
	}
	ages := newManagementReceiptAgeHeap(receipts)
	removed := make(map[int]struct{})
	for total > maximumBytes && ages.Len() > 0 {
		index := heap.Pop(ages).(int)
		removed[index] = struct{}{}
		total -= managementReceiptBytes(receipts[index])
	}
	if len(removed) == 0 {
		return receipts, nil
	}
	return partitionManagementReceipts(receipts, removed)
}

// partitionManagementReceipts splits the set into survivors and evictions in
// one linear pass. Both keep their original relative order, so a survivor set
// is indistinguishable from the set it came from and the dropped receipts are
// named rather than counted: the membership index and the byte accounting
// forget exactly those identifiers instead of being rebuilt from the whole set.
func partitionManagementReceipts(receipts []managementReceipt, removed map[int]struct{}) ([]managementReceipt, []managementReceipt) {
	kept := make([]managementReceipt, 0, len(receipts)-len(removed))
	dropped := make([]managementReceipt, 0, len(removed))
	for index, receipt := range receipts {
		if _, drop := removed[index]; drop {
			dropped = append(dropped, receipt)
			continue
		}
		kept = append(kept, receipt)
	}
	return kept, dropped
}

// managementReceiptAgeHeap orders receipt positions oldest-first by
// (ContentCreated, original insertion index), which is exactly the order the
// stable sort this replaced produced: equal timestamps evict in the order they
// were recorded.
//
// Heapifying the whole set costs O(n) and each eviction O(log n), so bounding a
// set by count, by bytes, or by both is O(n + k log n) for k evictions and never
// sorts the set. Sorting it instead -- once per recorded receipt, which is what
// the per-insertion trim did -- is what made a receipt set growing toward
// managementMaximumReceipts quadratic.
type managementReceiptAgeHeap struct {
	receipts []managementReceipt
	order    []int
}

func newManagementReceiptAgeHeap(receipts []managementReceipt) *managementReceiptAgeHeap {
	order := make([]int, len(receipts))
	for index := range order {
		order[index] = index
	}
	ages := &managementReceiptAgeHeap{receipts: receipts, order: order}
	heap.Init(ages)
	return ages
}

func (h *managementReceiptAgeHeap) Len() int { return len(h.order) }

func (h *managementReceiptAgeHeap) Less(left, right int) bool {
	first, second := h.order[left], h.order[right]
	if ordering := h.receipts[first].ContentCreated.Compare(h.receipts[second].ContentCreated); ordering != 0 {
		return ordering < 0
	}
	return first < second
}

func (h *managementReceiptAgeHeap) Swap(left, right int) {
	h.order[left], h.order[right] = h.order[right], h.order[left]
}

func (h *managementReceiptAgeHeap) Push(value any) {
	h.order = append(h.order, value.(int))
}

func (h *managementReceiptAgeHeap) Pop() any {
	last := len(h.order) - 1
	value := h.order[last]
	h.order = h.order[:last]
	return value
}

// pruneManagementReceipts drops every receipt older than the source retention
// horizon and reports exactly which ones went, returning the original slice
// untouched when nothing is out of date so a content completion does not
// reallocate or reindex the whole set.
func pruneManagementReceipts(receipts []managementReceipt, retainFrom time.Time) ([]managementReceipt, []managementReceipt) {
	stale := 0
	for _, receipt := range receipts {
		if receipt.ContentCreated.Before(retainFrom) {
			stale++
		}
	}
	if stale == 0 {
		return receipts, nil
	}
	kept := make([]managementReceipt, 0, len(receipts)-stale)
	dropped := make([]managementReceipt, 0, stale)
	for _, receipt := range receipts {
		if receipt.ContentCreated.Before(retainFrom) {
			dropped = append(dropped, receipt)
			continue
		}
		kept = append(kept, receipt)
	}
	return kept, dropped
}

func appendRecentString(values []string, value string, maximum int) []string {
	values = append(values, value)
	if len(values) > maximum {
		values = values[len(values)-maximum:]
	}
	return values
}

func stringSliceContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
