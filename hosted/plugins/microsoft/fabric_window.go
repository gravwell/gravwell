/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import "time"

// Fabric's API limits requests to the last 28 days and one UTC day per request.
// Scan metadata is deliberately separate from the greatest delivered event
// timestamp: newer events do not prove that older events are already visible.
// The client still splits this bounded range into individual UTC days.
const fabricRetention = 28 * 24 * time.Hour

type fabricScan struct {
	Anchor         time.Time `json:"anchor"`
	AvailableSince time.Time `json:"available_since"`
	Through        time.Time `json:"through"`
}

func fabricPendingExpired(state streamState, now time.Time) bool {
	if state.Continuation == nil {
		return false
	}
	floor := now.UTC().Add(-fabricRetention)
	if state.WindowEnd.Before(floor) {
		return true
	}
	// The frozen overall window may still overlap retention after its active
	// day has aged out. In that case both a saved continuation URI and a fresh
	// request made from the old cursor are outside Fabric's available range.
	return !state.Continuation.FabricCursor.IsZero() && state.Continuation.FabricCursor.Before(floor)
}

func fabricWindow(state streamState, start, end, now time.Time) (*fabricScan, time.Time, time.Time) {
	anchor := start
	// collect supplies either the frozen pending-window start or the completed
	// watermark minus the bounded replay overlap. Do not replace that start with
	// older Fabric metadata or the late-event replay window would be skipped.
	finalizedEnd := now.UTC().Add(-sourceConsistencyDelay).Truncate(time.Millisecond)
	if state.Continuation == nil && end.After(finalizedEnd) {
		end = finalizedEnd
	}
	floor := now.UTC().Add(-fabricRetention)
	if start.Before(floor) {
		start = floor
	}
	// An entirely expired range cannot be refetched. Bound replacement work to
	// the still-available range and the finalized horizon.
	if end.Before(start) {
		end = finalizedEnd
	}
	// Transport endpoints have millisecond precision. Round inward only at
	// the retention edge; the client rounds other starts outward for ties.
	if start.UTC().Truncate(time.Millisecond).Before(floor) {
		start = floor.Add(time.Millisecond - time.Nanosecond).Truncate(time.Millisecond)
	}
	end = end.UTC().Truncate(time.Millisecond)
	if start.After(end) {
		start = end
	}
	scan := &fabricScan{Anchor: anchor, AvailableSince: start, Through: end}
	if state.Fabric != nil {
		if scan.AvailableSince.Before(state.Fabric.AvailableSince) {
			scan.AvailableSince = state.Fabric.AvailableSince
		}
		if scan.Through.Before(state.Fabric.Through) {
			scan.Through = state.Fabric.Through
		}
	}
	return scan, start, end
}
