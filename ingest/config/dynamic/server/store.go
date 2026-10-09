/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package server

import (
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// ProtocolStore is the part of storage the protocol half actually touches: what an
// ingester registers, what it reports, and what it should be running.
//
// It is separate from Store because NewAPI needs only this.  A caller that wants to serve
// ingesters without also serving a management interface implements four methods instead of
// sixteen, and the split says plainly which of them the wire depends on.
//
// Two rules run through the whole thing and are the caller's to honour, not the backend's
// to invent:
//
//   - The Replace methods replace the complete set held for one ingester.  They never
//     merge.  That is what lets a kind dropped from a build disappear, and a runner that
//     has come good clear itself without anyone having to send a retraction.
//   - Identity is the authenticated session's.  A store is never handed an ingester UUID
//     that came out of a request body.
type ProtocolStore interface {
	// ReplaceKinds records the complete set one ingester says it can run, and stamps that
	// ingester's class and last-seen.  Definitions are normalized first, see
	// NormalizeKind.  A nil ingester UUID is an error.
	ReplaceKinds(ingester uuid.UUID, class string, kinds []dynamic.RunnerDefinition) error

	// ReplaceStatuses records one ingester's complete set of verdicts.  The since
	// carry-forward is not the backend's to invent, see MergeStatuses.  A nil ingester
	// UUID is an error.
	ReplaceStatuses(ingester uuid.UUID, statuses []dynamic.RunnerStatus) error

	// IngesterKinds is what one ingester registered.  Served from storage rather than
	// from the live session, which is what lets push targeting and the poll agree, and
	// lets an interface answer with the fleet switched off.
	IngesterKinds(ingester uuid.UUID) ([]dynamic.RunnerDefinition, error)

	// Runners is every configured runner.  The server filters with
	// dynamic.RunnerQuery.Matches rather than asking the backend to understand
	// assignments.
	Runners() ([]dynamic.RunnerDefinition, error)
}

// Store is everything the server half needs persisted: the protocol's four, plus what a
// management interface reads and writes.
//
// It is deliberately not a SQL interface.  The Gravwell webserver backs this with the
// asset registry and the test server backs it with SQLite; nothing here should be easier
// to implement with one than the other.  TestStore in this package is the contract, and
// running it is how a new backend finds out it disagrees.
type Store interface {
	ProtocolStore

	// --- the catalog: what can be configured ---

	// Kinds is one definition per kind across every ingester.  Where two ingesters have
	// registered the same kind, see the note on collisions in TestStore: the rule is the
	// backend's to state and a consumer that cannot live with it should read
	// IngesterKinds instead.
	Kinds() ([]dynamic.RunnerDefinition, error)

	// Kind is one registration, or ErrNotFound.
	Kind(kind string) (dynamic.RunnerDefinition, error)

	// KindMetadata is the icon, documentation and version each kind ships, keyed by kind.
	// A kind that describes nothing is absent rather than present and empty, so a caller
	// can tell "this plugin describes itself" from "this plugin does not".
	KindMetadata() (map[string]*dynamic.RunnerMetadata, error)

	// KindIngesters is the ingesters that have registered a kind, which is the legal set
	// of pin targets: pinning to one that has not registered it produces a runner that is
	// never delivered.
	KindIngesters(kind string) ([]Ingester, error)

	// --- the fleet: who can run what ---

	// Ingesters is every ingester that has ever registered, newest first.
	Ingesters() ([]Ingester, error)

	// Classes is the distinct classes that have been seen, so an interface can offer them
	// rather than asking an operator to remember what they called things.
	Classes() ([]string, error)

	// ForgetIngester removes everything held about one ingester: its registrations, the
	// statuses it reported, and the ingester record itself.  All of it or none of it, in
	// one transaction.
	//
	// Runners are not touched.  A runner pinned to this UUID keeps its pin and is reported
	// as naming an unknown target, the same as any other pin to an ingester that has not
	// registered the kind.  Forgetting a box that has gone away must not quietly rewrite
	// what an operator asked for.
	//
	// Forgetting is not a ban.  The ingester's next registerKinds puts all of it back.
	//
	// Returns ErrNotFound when no such ingester was known, having still removed any
	// registrations or statuses orphaned under that UUID — that cleanup is the whole point
	// on that path, because orphaned rows are exactly how a UUID ends up with no record
	// and registrations anyway.
	ForgetIngester(id uuid.UUID) error

	// --- the runners themselves ---

	// Runner is one configured runner, or ErrNotFound.
	Runner(id uuid.UUID) (dynamic.RunnerDefinition, error)

	// PutRunner stores a configured runner, replacing any with the same UUID.
	//
	// Kind and Name together must be unique, and enforcing that is the backend's job.  In
	// the SQLite store it falls out of a unique index, which makes it invisible from here;
	// a registry backed implementation has to do it deliberately.  Kind, Name and UUID are
	// all required.
	//
	// Metadata is not stored.  An icon and a version describe a plugin, not one
	// configuration of it, and the registration already holds them.
	PutRunner(rd dynamic.RunnerDefinition) error

	// DeleteRunner removes a runner and, in the same transaction, every status any
	// ingester reported about it.  Leaving those behind strands an error against a runner
	// that no longer exists, and nothing can ever clear it: an ingester only reports on
	// runners it was handed, so it will never mention this one again.  ErrNotFound when
	// the UUID is unknown.
	DeleteRunner(id uuid.UUID) error

	// --- status ---

	// Statuses is every status row, newest report first.
	Statuses() ([]StatusRow, error)

	// RunnerStatuses is what every ingester has said about one runner, failures first so
	// the thing an operator opened the page to read is at the top.
	RunnerStatuses(id uuid.UUID) ([]StatusRow, error)
}
