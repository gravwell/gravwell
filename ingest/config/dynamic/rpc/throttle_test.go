/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package rpc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
	"uuid"
)

// The throttle is driven with an explicit clock rather than by sleeping, so these tests
// are exact rather than approximately timed.

// TestThrottleWindow covers the basic per address limit.
func TestThrottleWindow(t *testing.T) {
	th := newThrottle(time.Second, 16)
	t0 := time.Now()

	if ok, deg := th.allow(`10.0.0.1`, t0); !ok || deg {
		t.Fatalf("first attempt: ok=%v degraded=%v, want true false", ok, deg)
	}
	// anything inside the window is refused
	for _, d := range []time.Duration{0, time.Millisecond, 500 * time.Millisecond, 999 * time.Millisecond} {
		if ok, _ := th.allow(`10.0.0.1`, t0.Add(d)); ok {
			t.Errorf("an attempt %v after the first was allowed", d)
		}
	}
	// exactly one window later is allowed again
	if ok, _ := th.allow(`10.0.0.1`, t0.Add(time.Second)); !ok {
		t.Error(`an attempt a full window later was refused`)
	}
	// a different address is unaffected
	if ok, _ := th.allow(`10.0.0.2`, t0.Add(time.Millisecond)); !ok {
		t.Error(`a different address was caught by another address's limit`)
	}
}

// TestThrottleRefusalDoesNotExtend checks that hammering does not lock a client out for
// good, which would be a miserable failure mode for a misconfigured retry loop.
func TestThrottleRefusalDoesNotExtend(t *testing.T) {
	th := newThrottle(time.Second, 16)
	t0 := time.Now()
	if ok, _ := th.allow(`10.0.0.1`, t0); !ok {
		t.Fatal(`first attempt refused`)
	}
	// hammer every 100ms across the whole window
	for i := 1; i < 10; i++ {
		if ok, _ := th.allow(`10.0.0.1`, t0.Add(time.Duration(i)*100*time.Millisecond)); ok {
			t.Fatalf("attempt at %dms was allowed", i*100)
		}
	}
	// the window is still measured from the last accepted attempt, so it opens on time
	if ok, _ := th.allow(`10.0.0.1`, t0.Add(time.Second)); !ok {
		t.Error(`a client that kept retrying locked itself out`)
	}
}

// TestThrottlePrunes checks that every attempt clears out entries older than the window,
// which is what lets the table recover after a flood.
func TestThrottlePrunes(t *testing.T) {
	th := newThrottle(time.Second, 4096)
	t0 := time.Now()
	for i := range 1000 {
		th.allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256), t0)
	}
	if n := th.tracked(); n != 1000 {
		t.Fatalf("tracked %d, want 1000", n)
	}
	// still inside the window, nothing is pruned
	th.allow(`192.168.0.1`, t0.Add(500*time.Millisecond))
	if n := th.tracked(); n != 1001 {
		t.Errorf("tracked %d, want 1001, nothing should have been pruned yet", n)
	}
	// one attempt past the window drops everything that aged out
	th.allow(`192.168.0.2`, t0.Add(2*time.Second))
	if n := th.tracked(); n != 1 {
		t.Errorf("tracked %d, want 1, the stale entries were not pruned", n)
	}
}

// TestThrottleBounded is the address rolling case.  The table must not grow without
// limit, and once it is full everything we cannot tell apart shares one limiter.
func TestThrottleBounded(t *testing.T) {
	const max = 64
	th := newThrottle(time.Second, max)
	t0 := time.Now()

	// roll through far more addresses than the table can hold, all at one instant so
	// nothing ages out underneath us
	var allowed, degraded int
	for i := range max * 20 {
		ok, deg := th.allow(fmt.Sprintf("2001:db8::%x", i), t0)
		if ok {
			allowed++
		}
		if deg {
			degraded++
		}
		if n := th.tracked(); n > max {
			t.Fatalf("the table grew to %d, past its ceiling of %d", n, max)
		}
	}
	if th.tracked() != max {
		t.Errorf("tracked %d, want the table full at %d", th.tracked(), max)
	}
	// the first max addresses each got their one attempt and filled the table, then the
	// global limiter allowed its own first attempt and refused everything after it
	if want := max + 1; allowed != want {
		t.Errorf("allowed %d attempts out of %d, want %d", allowed, max*20, want)
	}
	if degraded == 0 {
		t.Error(`nothing was reported as degraded, the caller cannot tell this is happening`)
	}
	if th.degradedCount() == 0 {
		t.Error(`the degraded counter never moved`)
	}

	// still inside the window, so the table is still full and every new address is
	// refused by the one global limiter
	for i := range 100 {
		ok, deg := th.allow(fmt.Sprintf("2001:db8:1::%x", i), t0.Add(500*time.Millisecond))
		if ok {
			t.Fatalf("a new address was allowed while the table was full and the global limiter spent")
		} else if !deg {
			t.Fatalf("a new address was refused without being reported as degraded")
		}
	}

	// once the flood stops the entries age out and the next attempt is served normally
	// again, the degraded state is not sticky
	if ok, deg := th.allow(`10.0.0.1`, t0.Add(2*time.Second)); !ok || deg {
		t.Errorf("after the flood: ok=%v degraded=%v, want true false", ok, deg)
	}
	if n := th.tracked(); n != 1 {
		t.Errorf("tracked %d after the flood aged out, want 1", n)
	}
}

// TestThrottleKnownClientSurvivesFlood checks that an address already in the table keeps
// its own limit while the table is full, so a legitimate ingester is not punished for
// somebody else's flood.
func TestThrottleKnownClientSurvivesFlood(t *testing.T) {
	const max = 32
	th := newThrottle(time.Second, max)
	t0 := time.Now()

	// a real client gets in first and is therefore tracked
	if ok, _ := th.allow(`10.0.0.7`, t0); !ok {
		t.Fatal(`the client's first attempt was refused`)
	}
	// now fill the rest of the table and keep rolling
	for i := range max * 10 {
		th.allow(fmt.Sprintf("2001:db8::%x", i), t0.Add(time.Millisecond))
	}
	if n := th.tracked(); n > max {
		t.Fatalf("the table grew to %d", n)
	}
	// the known client still gets its own attempt on its own schedule, even though the
	// global limiter was consumed by the flood at this instant
	if ok, deg := th.allow(`10.0.0.7`, t0.Add(time.Second)); !ok || deg {
		t.Errorf("the known client got ok=%v degraded=%v, want true false", ok, deg)
	}
}

// TestThrottleConcurrent is a race detector target, the throttle is hit from every
// connection the server accepts.
func TestThrottleConcurrent(t *testing.T) {
	th := newThrottle(time.Second, 128)
	t0 := time.Now()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := range 100 {
				th.allow(fmt.Sprintf("10.0.0.%d", j%50), t0.Add(time.Duration(j)*time.Millisecond))
			}
		}(i)
	}
	wg.Wait()
	if n := th.tracked(); n > 128 {
		t.Errorf("tracked %d, past the ceiling", n)
	}
}

// TestServerThrottlesReconnect is the end to end behaviour: a client that reconnects too
// fast is told to go away before the upgrade and the connection is closed.
func TestServerThrottlesReconnect(t *testing.T) {
	srv, err := NewServer(ServerConfig{Token: testToken, AuthRateWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// the first attempt authenticates normally
	sess, err := Dial(context.Background(), ClientConfig{
		Webserver: ts.URL, Token: testToken, ID: uuid.New(), PingInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// the second is refused before it ever becomes a websocket
	if s2, err := Dial(context.Background(), ClientConfig{
		Webserver: ts.URL, Token: testToken, ID: uuid.New(), PingInterval: -1,
	}); err == nil {
		s2.Close()
		t.Fatal(`a client that reconnected immediately was allowed to authenticate`)
	}

	// and the refusal is a real HTTP answer, not a dropped connection
	resp, err := http.Get(ts.URL + DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
	if ra := resp.Header.Get(`Retry-After`); ra == `` {
		t.Error(`no Retry-After header`)
	} else if n, err := strconv.Atoi(ra); err != nil || n < 1 {
		t.Errorf("Retry-After = %q, want a positive number of seconds", ra)
	}
	if !resp.Close {
		t.Error(`the server did not hang up, Connection: close was not honored`)
	}
}

// TestServerThrottleRecovers checks that a client can get back in once the window passes.
func TestServerThrottleRecovers(t *testing.T) {
	srv, err := NewServer(ServerConfig{Token: testToken, AuthRateWindow: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	first, err := Dial(context.Background(), ClientConfig{
		Webserver: ts.URL, Token: testToken, ID: uuid.New(), PingInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	if s2, err := Dial(context.Background(), ClientConfig{
		Webserver: ts.URL, Token: testToken, ID: uuid.New(), PingInterval: -1,
	}); err == nil {
		s2.Close()
		t.Fatal(`an immediate reconnect was allowed`)
	}

	time.Sleep(300 * time.Millisecond)
	third, err := Dial(context.Background(), ClientConfig{
		Webserver: ts.URL, Token: testToken, ID: uuid.New(), PingInterval: -1,
	})
	if err != nil {
		t.Fatalf("a client was still refused after the window passed: %v", err)
	}
	third.Close()
}

// TestServerThrottleDisabled covers the escape hatch the rest of the suite relies on.
func TestServerThrottleDisabled(t *testing.T) {
	srv, err := NewServer(ServerConfig{Token: testToken, AuthRateWindow: -1})
	if err != nil {
		t.Fatal(err)
	}
	if srv.thr != nil {
		t.Fatal(`a negative window should build no throttle at all`)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	for i := range 5 {
		sess, err := Dial(context.Background(), ClientConfig{
			Webserver: ts.URL, Token: testToken, ID: uuid.New(), PingInterval: -1,
		})
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		sess.Close()
	}
}

// TestServerThrottleDefaults checks the defaults are the ones documented.
func TestServerThrottleDefaults(t *testing.T) {
	srv, err := NewServer(ServerConfig{Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	if srv.thr == nil {
		t.Fatal(`throttling should be on by default`)
	}
	if srv.cfg.AuthRateWindow != defaultAuthRateWindow {
		t.Errorf("window = %v, want %v", srv.cfg.AuthRateWindow, defaultAuthRateWindow)
	}
	if srv.thr.max != defaultMaxTrackedAddrs {
		t.Errorf("max tracked = %d, want %d", srv.thr.max, defaultMaxTrackedAddrs)
	}
	if defaultMaxTrackedAddrs < 1000 || defaultMaxTrackedAddrs > 100000 {
		t.Errorf("the table ceiling of %d is not a few thousand", defaultMaxTrackedAddrs)
	}
}
