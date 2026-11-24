/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package server is the webserver half of the dynamic ingester configuration protocol.
//
// It holds everything a server has to do that is not deployment policy: answering an
// ingester's registration and poll, pushing a configuration to the ingesters it is meant
// for, and the rules for what gets persisted.  What backs that storage is the caller's
// business — the Gravwell webserver uses its own registry, the test server under
// test_server uses SQLite, and the conformance suite in this package is what keeps the two
// from drifting.
//
// The transport lives in ingest/config/dynamic/rpc and the wire types in
// ingest/config/dynamic.  Building the rpc.Server itself is left to the caller: timeouts,
// logging and TrustedProxies are deployment decisions, not protocol.
package server

import (
	"errors"
	"time"
	"uuid"
)

// ErrNotFound is returned when a lookup names something the store does not hold.
var ErrNotFound = errors.New("not found")

// Ingester is what an interface needs to offer an assignment target: an ingester the
// server has heard from, and what it said it could run.
//
// It comes out of storage rather than out of a live session, which is the point.  An
// operator asking "what can run here" needs an answer while the fleet is switched off.
type Ingester struct {
	UUID     uuid.UUID
	Class    string
	Kinds    []string
	LastSeen time.Time
}

// StatusRow is one ingester's last word about one configured runner.
//
// Error is empty when that ingester accepted the configuration, which is how a runner that
// has come good is told apart from one nobody has reported on.  Since and Updated are both
// the server's clock, see MergeStatuses.
type StatusRow struct {
	Runner   uuid.UUID
	Ingester uuid.UUID
	Kind     string
	Name     string
	Error    string
	Since    time.Time
	Updated  time.Time
}

// OK reports whether the reporting ingester accepted the configuration.
func (sr StatusRow) OK() bool { return sr.Error == `` }
