/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"testing"
	"time"
	"uuid"
)

// TestBackoff covers the reconnect schedule.
func TestBackoff(t *testing.T) {
	// it grows, it is capped, and it never returns something unusable
	var prevCeil time.Duration
	for n := 1; n <= 24; n++ {
		// the jitter makes each draw random, so the shape is checked over many draws
		var maxSeen time.Duration
		for range 200 {
			d := backoff(n)
			if d <= 0 {
				t.Fatalf("backoff(%d) returned %v, a non positive wait would spin", n, d)
			}
			if d > backoffMax {
				t.Fatalf("backoff(%d) returned %v, past the %v cap", n, d, backoffMax)
			}
			if d > maxSeen {
				maxSeen = d
			}
		}
		// the ceiling has to climb until it reaches the cap, otherwise a server that is
		// down gets hammered at a fixed rate
		if n < 7 && maxSeen < prevCeil {
			t.Errorf("backoff(%d) tops out at %v, below the previous %v", n, maxSeen, prevCeil)
		}
		prevCeil = maxSeen
	}

	// once capped it stays capped, no overflow back to something tiny
	for _, n := range []int{20, 40, 100, 1000} {
		for range 100 {
			if d := backoff(n); d <= 0 || d > backoffMax {
				t.Fatalf("backoff(%d) returned %v, the shift overflowed", n, d)
			}
		}
	}

	// the jitter is real, a fleet that lost the same server must not come back in
	// lockstep
	seen := map[time.Duration]bool{}
	for range 100 {
		seen[backoff(10)] = true
	}
	if len(seen) < 50 {
		t.Errorf("backoff produced only %d distinct waits out of 100, the jitter is not working", len(seen))
	}
}

// TestPollInterval covers the configured interval and its guards.
func TestPollInterval(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{``, DefaultPollInterval},
		{`1s`, time.Second},
		{`45s`, 45 * time.Second},
		{`5m`, 5 * time.Minute},
		{`nonsense`, DefaultPollInterval}, // Verify rejects it, this is the fallback
		{`1ms`, DefaultPollInterval},      // below the floor
	} {
		if got := (Config{Poll_Interval: tc.in}).PollInterval(); got != tc.want {
			t.Errorf("Poll_Interval %q -> %v, want %v", tc.in, got, tc.want)
		}
	}

	// Verify rejects what PollInterval would have to paper over
	base := func() Config {
		return Config{
			Webserver:  []string{`10.0.0.1:8080`},
			Auth_Token: `token`,
			Storage:    t.TempDir(),
		}
	}
	for _, bad := range []string{`nonsense`, `1ms`, `-5s`, `0s`} {
		c := base()
		c.Poll_Interval = bad
		if err := c.Verify(); err == nil {
			t.Errorf("Poll-Interval %q should not verify", bad)
		}
	}
	for _, good := range []string{``, `1s`, `30s`, `10m`} {
		c := base()
		c.Poll_Interval = good
		if err := c.Verify(); err != nil {
			t.Errorf("Poll-Interval %q should verify: %v", good, err)
		}
	}
}

// TestRunnerQueryMatches covers the rule both ends use to decide what belongs to whom.
func TestRunnerQueryMatches(t *testing.T) {
	mine := uuid.New()
	other := uuid.New()
	third := uuid.New()
	q := RunnerQuery{ID: mine, Class: `edge`, Kinds: []string{`okta`, `jamf`}}

	rd := func(kind string, a *Assignment) RunnerDefinition {
		return RunnerDefinition{Kind: kind, Name: `n`, Assigned: a}
	}

	for _, tc := range []struct {
		name string
		rd   RunnerDefinition
		want bool
	}{
		{`unassigned, supported kind`, rd(`okta`, nil), true},
		{`unassigned, unsupported kind`, rd(`wiz`, nil), false},
		{`empty assignment behaves as unassigned`, rd(`jamf`, &Assignment{}), true},

		// UUID lists
		{`our uuid alone`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{mine}}), true},
		{`our uuid among others`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{other, mine, third}}), true},
		{`a list we are not in`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{other, third}}), false},
		{`someone else alone`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{other}}), false},
		{`our uuid but wrong kind`, rd(`wiz`, &Assignment{UUIDs: []uuid.UUID{mine}}), false},

		// class lists
		{`our class alone`, rd(`okta`, &Assignment{Classes: []string{`edge`}}), true},
		{`our class among others`, rd(`okta`, &Assignment{Classes: []string{`core`, `edge`}}), true},
		{`a class list we are not in`, rd(`okta`, &Assignment{Classes: []string{`core`, `dmz`}}), false},

		// both lists, each is a filter and both have to pass
		{`both match`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{mine}, Classes: []string{`edge`}}), true},
		{`uuid matches, class does not`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{mine}, Classes: []string{`core`}}), false},
		{`class matches, uuid does not`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{other}, Classes: []string{`edge`}}), false},
		{`neither matches`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{other}, Classes: []string{`core`}}), false},

		// a group is still something we cannot evaluate
		{`assigned to a group we cannot evaluate`, rd(`okta`, &Assignment{Group: `g`}), false},
		{`our uuid but also a group`, rd(`okta`, &Assignment{UUIDs: []uuid.UUID{mine}, Group: `g`}), false},
	} {
		if got := q.Matches(tc.rd); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}

	// an ingester that has registered nothing matches nothing, it cannot run anything
	empty := RunnerQuery{ID: mine, Class: `edge`}
	if empty.Matches(rd(`okta`, nil)) {
		t.Error(`an ingester with no registered kinds should match nothing`)
	}

	// an ingester with no class is still excluded by a class list, it is not a wildcard
	noClass := RunnerQuery{ID: mine, Kinds: []string{`okta`}}
	if noClass.Matches(rd(`okta`, &Assignment{Classes: []string{`edge`}})) {
		t.Error(`an ingester with no class should not match a class list`)
	}
	if !noClass.Matches(rd(`okta`, nil)) {
		t.Error(`an ingester with no class should still get unassigned runners`)
	}
}

// TestAssignmentHelpers covers the filters on their own, including the nil receiver that
// an unassigned runner produces.
func TestAssignmentHelpers(t *testing.T) {
	id := uuid.New()
	var nilA *Assignment
	if !nilA.Empty() || !nilA.AllowsUUID(id) || !nilA.AllowsClass(`edge`) {
		t.Error(`a nil assignment should filter nothing and not panic`)
	}
	if !(&Assignment{}).Empty() {
		t.Error(`an assignment with nothing set should be empty`)
	}
	for _, a := range []*Assignment{
		{UUIDs: []uuid.UUID{id}},
		{Classes: []string{`edge`}},
		{Group: `g`},
	} {
		if a.Empty() {
			t.Errorf("%+v should not be empty", a)
		}
	}
	// an empty list is not a filter, it must not exclude everything
	a := &Assignment{UUIDs: []uuid.UUID{}, Classes: []string{}}
	if !a.AllowsUUID(id) || !a.AllowsClass(`edge`) {
		t.Error(`empty lists should not filter`)
	}
}
