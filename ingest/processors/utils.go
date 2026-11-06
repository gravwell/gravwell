/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"fmt"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/timegrinder"
	"github.com/inhies/go-bytesize"
)

func parseDataSize(v string) (s int, err error) {
	var bs bytesize.ByteSize
	if bs, err = bytesize.Parse(v); err == nil {
		s = int(bs)
	}
	return
}

const (
	// DefaultTimestampMaxFutureDelta is how far into the future an extracted timestamp
	// may be before timestamp extracting preprocessors ignore it and keep the existing entry timestamp.
	DefaultTimestampMaxFutureDelta = 24 * time.Hour
	DefaultTimestampMaxPastDelta   = 365 * 24 * time.Hour // 1 year in the rear is a reasonable max past delta when processing with a preprocessor
)

// tsBounds governs which extracted timestamps a preprocessor will apply to an entry.
// Timestamps before the Unix epoch are always rejected, as are timestamps outside the window.
type tsBounds struct {
	window timegrinder.TimestampWindow
}

// newTSBounds builds timestamp bounds from the Timestamp-Max-Past-Delta and Timestamp-Max-Future-Delta
// preprocessor config values. An empty past delta uses DefaultTimestampMaxPastDelta,
// an empty future delta uses DefaultTimestampMaxFutureDelta.
func newTSBounds(maxPast, maxFuture string) (b tsBounds, err error) {
	if b.window.MaxPastDelta, err = parseTSDelta(maxPast, DefaultTimestampMaxPastDelta); err != nil {
		err = fmt.Errorf("invalid Timestamp-Max-Past-Delta %q: %w", maxPast, err)
	} else if b.window.MaxFutureDelta, err = parseTSDelta(maxFuture, DefaultTimestampMaxFutureDelta); err != nil {
		err = fmt.Errorf("invalid Timestamp-Max-Future-Delta %q: %w", maxFuture, err)
	}
	return
}

func parseTSDelta(v string, def time.Duration) (d time.Duration, err error) {
	if v = strings.TrimSpace(v); v == `` {
		return def, nil
	}
	if d, err = time.ParseDuration(v); err != nil {
		return
	}
	if d < 0 {
		d = -d
	}
	if d == 0 {
		err = fmt.Errorf("delta must be non-zero")
	}
	return
}

func (b tsBounds) valid(t time.Time) bool {
	return !t.IsZero() && t.Unix() >= 0 && b.window.Valid(t)
}

// validUnix checks a Unix epoch seconds value without converting it, so values that
// would overflow entry.UnixTime are rejected.
func (b tsBounds) validUnix(sec int64) bool {
	if sec < 0 {
		return false
	}
	now := time.Now()
	if b.window.MaxFutureDelta != 0 && sec > now.Add(b.window.MaxFutureDelta).Unix() {
		return false
	}
	if b.window.MaxPastDelta != 0 && sec < now.Add(-b.window.MaxPastDelta).Unix() {
		return false
	}
	return true
}

// apply sets the entry timestamp to t if t is within bounds, otherwise the entry is left unchanged.
func (b tsBounds) apply(ent *entry.Entry, t time.Time) {
	if b.valid(t) {
		ent.TS = entry.FromStandard(t)
	}
}
