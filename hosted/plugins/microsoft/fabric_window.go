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

func fabricWindow(state streamState, start, end, now time.Time) (*fabricScan, time.Time, time.Time) {
	anchor := start
	if state.Fabric != nil && !state.Fabric.Anchor.IsZero() {
		anchor = state.Fabric.Anchor
	} else if state.Pending != nil && !state.Pending.FabricAnchor.IsZero() {
		anchor = state.Pending.FabricAnchor
	}
	// State without scan metadata needs no destructive rewrite. Its watermark
	// minus overlap or pending start supplied by collect becomes the durable anchor.
	if state.Pending == nil {
		start = anchor
	}
	if end.After(now) {
		end = now
	}
	floor := now.UTC().Add(-fabricRetention)
	if start.Before(floor) {
		start = floor
	}
	// An entirely expired pending poll cannot be refetched. Resume the full
	// still-available anchored range, retaining version receipts for dedup.
	if end.Before(start) {
		end = now
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
