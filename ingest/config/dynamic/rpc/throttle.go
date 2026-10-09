/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package rpc

import (
	"sync"
	"time"
)

const (
	// defaultAuthRateWindow is the minimum gap between accepted authentication attempts
	// from one address.
	defaultAuthRateWindow = time.Second

	// defaultMaxTrackedAddrs bounds the throttle table.  The table is the only thing an
	// unauthenticated peer can make us allocate, so it has a hard ceiling rather than a
	// policy that hopes addresses are scarce.  A few thousand entries covers any real
	// deployment and costs a few hundred kilobytes.
	defaultMaxTrackedAddrs = 4096
)

// throttle rate limits authentication attempts by source address.
//
// It is bounded on purpose.  A peer on a routed IPv6 prefix has more addresses than we
// could ever track, so rather than grow a table until it is the denial of service, the
// table stops at a fixed size and everything that does not fit falls back to a single
// global limiter.  Callers key IPv6 by prefix rather than by address, see throttleKey,
// which is what keeps a flood from reaching that fallback in the first place.  The tradeoff is explicit: once an attacker fills the table, every
// address we are not already tracking shares one attempt per window, which is slow for
// legitimate clients but is never unbounded memory.
//
// A throttle is safe for concurrent use.
type throttle struct {
	mtx    sync.Mutex
	window time.Duration
	max    int

	// seen is the per address table, holding the last accepted attempt.  Entries older
	// than the window are pruned on every call, so a quiet period drains it.
	seen map[string]time.Time

	// global is the last accepted attempt made while the table was full.  This is the
	// "we gave up on telling them apart" limiter.
	global time.Time

	// degraded counts how many attempts have landed on the global limiter, so an
	// operator can tell an address rolling flood from ordinary load.
	degraded uint64
}

// newThrottle builds a throttle.  A window of zero or less means no throttling at all,
// in which case the server does not build one.
func newThrottle(window time.Duration, max int) *throttle {
	if max <= 0 {
		max = defaultMaxTrackedAddrs
	}
	return &throttle{
		window: window,
		max:    max,
		seen:   make(map[string]time.Time, max/8),
	}
}

// allow reports whether addr may attempt to authenticate at now, and whether the decision
// came from the global limiter rather than from the address's own entry.
//
// Only an accepted attempt is recorded.  A rejected attempt deliberately does not push the
// window out, otherwise a client retrying faster than the limit would lock itself out
// indefinitely, which is a miserable thing to diagnose in the field.
func (t *throttle) allow(addr string, now time.Time) (ok, degraded bool) {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	// prune first.  An entry older than the window cannot deny anything, so carrying it
	// only costs a slot that a real client might need.  The table ceiling is what keeps
	// this bounded, it is at most max entries no matter how hard we are being hit.
	t.prune(now)

	if last, tracked := t.seen[addr]; tracked {
		// an address we are already tracking keeps its own limit even while the table is
		// full, so a known client is not punished for someone else's flood
		if now.Sub(last) < t.window {
			return false, false
		}
		t.seen[addr] = now
		return true, false
	}

	if len(t.seen) >= t.max {
		// out of room, we can no longer tell these addresses apart so we stop trying
		t.degraded++
		if now.Sub(t.global) < t.window {
			return false, true
		}
		t.global = now
		return true, true
	}

	t.seen[addr] = now
	return true, false
}

// prune drops every entry older than the window.  The caller holds the lock.
func (t *throttle) prune(now time.Time) {
	for k, v := range t.seen {
		if now.Sub(v) >= t.window {
			delete(t.seen, k)
		}
	}
}

// tracked is the current table size, for tests and logging.
func (t *throttle) tracked() int {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	return len(t.seen)
}

// degradedCount is how many attempts have fallen through to the global limiter.
func (t *throttle) degradedCount() uint64 {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	return t.degraded
}
