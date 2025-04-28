/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package timegrinder

import (
	"time"
)

// TimestampWindow specifies deltas into the past and
// future. Timestamps falling within those deltas of the current time
// will be considered valid; other timestamps are invalid.
type TimestampWindow struct {
	MaxPastDelta   time.Duration
	MaxFutureDelta time.Duration
}

// Enabled returns true if at least one duration has been set in the
// timestamp window struct.
func (w *TimestampWindow) Enabled() bool {
	return w.MaxPastDelta != 0 || w.MaxFutureDelta != 0
}

// Valid takes a timestamp and returns a boolean. If the provided
// timestamp falls within the deltas defined by TimestampWindow, Valid
// returns true. If it falls outside the window, it returns false
func (w *TimestampWindow) Valid(t time.Time) bool {
	now := time.Now()
	if w.MaxPastDelta != 0 && t.Before(now.Add(-1*w.MaxPastDelta)) {
		return false
	}
	if w.MaxFutureDelta != 0 && t.After(now.Add(w.MaxFutureDelta)) {
		return false
	}
	return true
}

// Override takes a timesatmp and returns a potentially overriden timestamp
// this is a shorthand for if !w.Valid(ts) {return entry.Now()}
func (w *TimestampWindow) Override(t time.Time) time.Time {
	if w.Enabled() {
		now := time.Now()
		if w.MaxPastDelta != 0 && t.Before(now.Add(-1*w.MaxPastDelta)) {
			t = now //override to now
		} else if w.MaxFutureDelta != 0 && t.After(now.Add(w.MaxFutureDelta)) {
			t = now //override to now
		}
	}
	return t // all good
}
