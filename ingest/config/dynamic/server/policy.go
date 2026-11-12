/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package server

import (
	"errors"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// The rules in this file are protocol policy that used to live inside SQL.  They are here
// because a second backend reimplementing either one by hand would get it subtly wrong and
// nobody would notice for months.

// MergeStatuses computes the rows that replace one ingester's current set.
//
// existing is what the backend holds for that ingester, incoming is what it just reported,
// now is the server's clock.  Since is carried forward while a row's error is unchanged, so
// a row answers "how long has this been broken" and not just "is it broken now"; a
// different error, or an error where there was none, restarts it.  Both stamps are the
// server's, because an operator comparing two ingesters needs one clock rather than two
// that disagree by however wrong those hosts are.
//
// A status with a nil runner UUID is dropped: there is nothing to key it on, and the
// ingester has already logged it.
//
// The caller runs this inside whatever transaction its read-modify-write needs.  Handing
// the merge out rather than the transaction is what lets a registry backend and a SQL one
// share the rule without sharing a storage model.
func MergeStatuses(existing []StatusRow, incoming []dynamic.RunnerStatus,
	ingester uuid.UUID, now time.Time) []StatusRow {
	// what is already held, so an unchanged state keeps its clock running
	prior := make(map[uuid.UUID]StatusRow, len(existing))
	for _, row := range existing {
		prior[row.Runner] = row
	}
	out := make([]StatusRow, 0, len(incoming))
	for _, rs := range incoming {
		if rs.UUID == uuid.Nil() {
			continue
		}
		row := StatusRow{
			Runner:   rs.UUID,
			Ingester: ingester,
			Kind:     rs.Kind,
			Name:     rs.Name,
			Error:    rs.Error,
			Since:    now,
			Updated:  now,
		}
		if was, ok := prior[rs.UUID]; ok && was.Error == rs.Error {
			row.Since = was.Since // same state as last time, the clock keeps running
		}
		out = append(out, row)
	}
	return out
}

// NormalizeKind prepares a registration for storage.
//
// A registration describes a type and carries no identity, so anything that wandered in is
// stripped and the stored prototype stays clean.  An empty Kind is an error: it is the key
// everything else is filed under.
func NormalizeKind(rd dynamic.RunnerDefinition) (dynamic.RunnerDefinition, error) {
	if rd.Kind == `` {
		return rd, errors.New("registration has no kind")
	}
	rd.Name = ``
	rd.UUID = uuid.Nil()
	return rd, nil
}
