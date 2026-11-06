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
	"errors"
	"testing"
	"time"
)

// TestIdleTimeoutResolution pins the rule the documentation now states: zero takes the
// default, negative disables.  These two fields were documented the other way round from
// what they did, which is the sort of thing nobody notices until a long lived session is
// being dropped every two minutes and the field that was meant to stop it reads as off.
func TestIdleTimeoutResolution(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{`zero takes the default`, 0, defaultIdleTimeout},
		{`negative disables`, -1, 0},
		{`a value is honoured`, 45 * time.Second, 45 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewServer(ServerConfig{Token: `t`, IdleTimeout: tc.in})
			if err != nil {
				t.Fatal(err)
			}
			if s.cfg.IdleTimeout != tc.want {
				t.Errorf("IdleTimeout %v resolved to %v, want %v", tc.in, s.cfg.IdleTimeout, tc.want)
			}
		})
	}
}

// TestPingIntervalResolution is the client side of the same rule.
func TestPingIntervalResolution(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   time.Duration
		want time.Duration // zero means no keepalive at all
	}{
		{`zero takes the default`, 0, defaultPingInterval},
		{`negative disables`, -1, 0},
		{`a value is honoured`, 5 * time.Second, 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePingInterval(tc.in); got != tc.want {
				t.Errorf("PingInterval %v resolved to %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestKeepaliveStaysInsideTheServerIdleTimeout is the relationship that actually matters:
// the client's default ping has to fire comfortably before the server's default idle
// timeout, or every idle session is dropped and reconnected on a timer.
func TestKeepaliveStaysInsideTheServerIdleTimeout(t *testing.T) {
	if defaultPingInterval*2 >= defaultIdleTimeout {
		t.Errorf("default ping %v leaves no margin inside the default idle timeout %v",
			defaultPingInterval, defaultIdleTimeout)
	}
}

// TestWaitContextIsBounded is the guard on shutdown.
//
// Wait blocks until every handler has returned, and a handler blocked on a filesystem
// that has gone away never does.  Waiting on one forever turns an orderly shutdown into a
// process that can only be killed, so the caller has to be able to give up.
func TestWaitContextIsBounded(t *testing.T) {
	s := &Session{
		done:   make(chan struct{}),
		served: make(chan struct{}),
	}
	// nothing will ever close served, which is the wedged handler
	ctx, cf := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cf()
	start := time.Now()
	err := s.WaitContext(ctx)
	if err == nil {
		t.Fatal(`WaitContext returned success on a session that never drained`)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want a deadline error", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("waited %v, the bound was not applied", d)
	}

	// and a session that does drain returns promptly with no error
	close(s.served)
	ctx2, cf2 := context.WithTimeout(context.Background(), time.Second)
	defer cf2()
	if err = s.WaitContext(ctx2); err != nil {
		t.Errorf("a drained session reported %v", err)
	}
}
