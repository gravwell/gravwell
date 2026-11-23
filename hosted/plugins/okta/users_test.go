/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func testUser(id, lastUpdated, lastLogin string) string {
	ll := `null`
	if lastLogin != `` {
		ll = `"` + lastLogin + `"`
	}
	return fmt.Sprintf(`{"id":"%s", "status":"ACTIVE", "lastUpdated":"%s", "lastLogin":%s, "profile":{"login":"%s@example.com"}}`,
		id, lastUpdated, ll, id)
}

func setUsers(f *fakeOkta, users ...string) {
	f.mu.Lock()
	f.users = users
	f.mu.Unlock()
}

// every pass lists every user. A dump emits them all, a poll only those that are new or whose
// lastUpdated or lastLogin moved.
func TestUsers_ChangeDetection(t *testing.T) {
	t.Parallel()
	t0 := eventBase.Format(timeFormat)
	t1 := eventBase.Add(time.Minute).Format(timeFormat)
	a, b, c := testUser(`00ua`, t0, ``), testUser(`00ub`, t0, t0), testUser(`00uc`, t0, t0)
	f := newFakeOkta(t, withData(nil, []string{a, b, c}))
	rt := newTestRuntime(t)
	o := newSSWSIngester(t, f, rt)
	users := newUserTracker()

	pass := func(full bool, want ...string) {
		t.Helper()
		before := len(rt.data(o.userTag))
		n, err := o.getUsers(rt.ctx, rt, users, full)
		if err != nil {
			t.Fatal(err)
		}
		got := rt.data(o.userTag)[before:]
		if n != len(got) {
			t.Errorf("reported %d users emitted, wrote %d", n, len(got))
		}
		assertData(t, got, want)
	}

	pass(true, a, b, c)
	pass(false) // nothing changed

	a2 := testUser(`00ua`, t0, t1) // first login, lastUpdated untouched
	b2 := testUser(`00ub`, t1, t0) // profile change
	d := testUser(`00ud`, t1, ``)  // new user
	setUsers(f, a2, b2, c, d)
	pass(false, a2, b2, d)
	pass(false)

	c2 := testUser(`00uc`, t0, t1)
	setUsers(f, a2, c2) // b and d deleted
	pass(false, c2)
	if _, ok := users.seen[`00ub`]; ok || len(users.seen) != 2 {
		t.Errorf("deleted users not pruned: %v", users.seen)
	}
	setUsers(f, a2, b2, c2) // b comes back, it is new again
	pass(false, b2)

	pass(true, a2, b2, c2) // a dump emits everyone, changed or not

	_, userQ, _, _, _ := f.snapshot()
	for _, q := range userQ {
		if q.Get(`search`) != allUsersSearch || q.Get(`limit`) != `200` {
			t.Errorf("unexpected user query %v", q)
		}
	}
	assertClean(t, rt, f, false)
}

// a pass that fails part way keeps what it saw: users already emitted are not emitted again, and
// users it did not reach are not forgotten
func TestUsers_FailedPassNotPruned(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(nil, testUsers(5)), func(f *fakeOkta) { f.foreignNext = true })
	rt := newTestRuntime(t)
	o := newSSWSIngester(t, f, rt)
	users := newUserTracker()
	users.seen[`00u4`] = userState{LastUpdated: eventBase.Format(timeFormat)}

	if _, err := o.getUsers(rt.ctx, rt, users, false); err == nil || !strings.Contains(err.Error(), `another host`) {
		t.Fatalf("got %v, want the next link refused", err)
	}
	assertData(t, rt.data(o.userTag), f.users[:2])
	if len(users.seen) != 3 {
		t.Errorf("tracked %v, want the first page and the user it did not reach", users.seen)
	}

	f.mu.Lock()
	f.foreignNext = false
	f.mu.Unlock()
	if _, err := o.getUsers(rt.ctx, rt, users, false); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.userTag), f.users[:4]) // 00u4 was unchanged
}

// Run dumps every user straight away, with no change to look for
func TestUsers_DumpAtStartup(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(nil, testUsers(3)))
	rt := newTestRuntime(t)
	o := newSSWSIngester(t, f, rt)
	done := make(chan struct{})
	go func() {
		defer close(done)
		o.userLogRoutine(rt.ctx, rt)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(rt.data(o.userTag)) < len(f.users) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	rt.cancel()
	<-done
	assertData(t, rt.data(o.userTag), f.users)
	var dumped bool
	for _, l := range rt.logged(`INFO`) {
		dumped = dumped || l.msg == `emitted all users`
	}
	if !dumped {
		t.Errorf("startup dump not logged: %v", rt.logged(``))
	}
}

// runUsers runs the user routine until stop reports true or a few seconds pass, then stops it
func runUsers(t *testing.T, o *OktaIngester, rt *testRuntime, stop func() bool) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		o.userLogRoutine(rt.ctx, rt)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !stop() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	rt.cancel()
	<-done
}

func loggedMsg(rt *testRuntime, level, msg string) (n int) {
	for _, l := range rt.logged(level) {
		if strings.Contains(l.msg, msg) {
			n++
		}
	}
	return
}

// the routine dumps every user at startup unless the saved state shows a recent dump of this org.
// Saved state that is missing, empty, corrupted, in the future, inconsistent or for another domain
// counts as no dump.
func TestUsers_StartupDumpState(t *testing.T) {
	t.Parallel()
	now := time.Now()
	recent := func(domain string) func(*testRuntime) {
		return func(rt *testRuntime) {
			rt.PutString(userDomainKey, domain)
			rt.PutTime(userDumpKey, now.Add(-time.Hour))
			rt.PutTime(userPassKey, now.Add(-time.Minute))
		}
	}
	with := func(put func(*testRuntime), key string, v []byte) func(*testRuntime) {
		return func(rt *testRuntime) {
			put(rt)
			rt.Put(key, v)
		}
	}
	at := func(t time.Time) []byte { return []byte(t.Format(time.RFC3339Nano)) }
	for name, c := range map[string]struct {
		put  func(*testRuntime)
		dump bool // a dump is due at startup
		warn bool // the state is reported as unusable
	}{
		`recent`:              {put: recent(testHost)},
		`recent, other case`:  {put: recent(strings.ToUpper(testHost))},
		`missing`:             {put: func(*testRuntime) {}, dump: true},
		`empty dump`:          {put: with(recent(testHost), userDumpKey, nil), dump: true, warn: true},
		`corrupted dump`:      {put: with(recent(testHost), userDumpKey, []byte(`not a time`)), dump: true, warn: true},
		`truncated dump`:      {put: with(recent(testHost), userDumpKey, at(now)[:10]), dump: true, warn: true},
		`zero dump`:           {put: with(recent(testHost), userDumpKey, at(time.Time{})), dump: true, warn: true},
		`future dump`:         {put: with(recent(testHost), userDumpKey, at(now.Add(time.Hour))), dump: true, warn: true},
		`corrupted listing`:   {put: with(recent(testHost), userPassKey, []byte{0xff, 0x00}), dump: true, warn: true},
		`missing listing`:     {put: with(recent(testHost), userPassKey, nil), dump: true, warn: true},
		`listing before dump`: {put: with(recent(testHost), userPassKey, at(now.Add(-2*time.Hour))), dump: true, warn: true},
		`old dump`:            {put: with(recent(testHost), userDumpKey, at(now.Add(-25*time.Hour))), dump: true},
		`other domain`:        {put: recent(`other.okta.com`), dump: true},
		`no domain`:           {put: with(recent(testHost), userDomainKey, nil), dump: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFakeOkta(t, withData(nil, testUsers(3)))
			rt := newTestRuntime(t)
			o := newSSWSIngester(t, f, rt)
			c.put(rt)
			if c.dump {
				runUsers(t, o, rt, func() bool { return loggedMsg(rt, `INFO`, `emitted all users`) > 0 })
				assertData(t, rt.data(o.userTag), f.users)
				for _, key := range []string{userDumpKey, userPassKey} {
					if ts, err := rt.GetTime(key); err != nil || ts.Before(now) || ts.After(time.Now()) {
						t.Errorf("%s saved as %v, %v; want the dump's start", key, ts, err)
					}
				}
				if d, _ := rt.GetString(userDomainKey); d != testHost {
					t.Errorf("saved for domain %q, want %q", d, testHost)
				}
			} else {
				// the next listing is a poll after the saved one, nothing is requested now
				runUsers(t, o, rt, func() bool {
					if loggedMsg(rt, `INFO`, `starting user log routine`) == 0 {
						return false
					}
					time.Sleep(50 * time.Millisecond) // long enough for a listing to have started
					return true
				})
				if _, userQ, _, _, _ := f.snapshot(); len(userQ) != 0 {
					t.Errorf("listed users %d times, want none until the next poll", len(userQ))
				}
				for _, l := range rt.logged(`INFO`) {
					for _, p := range l.params {
						if p.Name == `first-listing-in` && p.Value == `0s` {
							t.Errorf("first listing at once, want it at the next poll")
						}
					}
				}
			}
			if warned := loggedMsg(rt, `WARN`, `user listing state`) > 0; warned != c.warn {
				t.Errorf("warned %v, want %v: %v", warned, c.warn, rt.logged(``))
			}
			assertClean(t, rt, f, false)
		})
	}
}

// a dump that fails part way is retried as a dump, but writes only the users it had not written yet,
// plus any that changed meanwhile. Once it completes, the next dump writes everyone again.
func TestUsers_FailedDumpResumed(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(nil, testUsers(5)), func(f *fakeOkta) { f.foreignNext = true })
	rt := newTestRuntime(t)
	o := newSSWSIngester(t, f, rt)
	users := newUserTracker()

	if _, err := o.getUsers(rt.ctx, rt, users, true); err == nil {
		t.Fatal("the foreign next link was followed")
	}
	assertData(t, rt.data(o.userTag), f.users[:2])

	f.mu.Lock()
	f.foreignNext = false
	f.users[0] = testUser(`00u0`, eventBase.Add(time.Minute).Format(timeFormat), ``) // changed meanwhile
	f.mu.Unlock()
	if n, err := o.getUsers(rt.ctx, rt, users, true); err != nil || n != 4 {
		t.Fatalf("resumed dump wrote %d, %v; want the changed user and the 3 not yet written", n, err)
	}
	assertData(t, rt.data(o.userTag)[2:], []string{f.users[0], f.users[2], f.users[3], f.users[4]})
	if users.dumped != nil {
		t.Error("the completed dump is still in progress")
	}

	if n, err := o.getUsers(rt.ctx, rt, users, true); err != nil || n != 5 {
		t.Errorf("next dump wrote %d, %v; want every user", n, err)
	}
	if n, err := o.getUsers(rt.ctx, rt, users, false); err != nil || n != 0 {
		t.Errorf("poll wrote %d, %v; want nothing", n, err)
	}
}

func TestUserRetryWait(t *testing.T) {
	t.Parallel()
	poll := 10 * time.Minute
	for failures, want := range map[int]time.Duration{
		0: time.Minute, 1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 4: 8 * time.Minute, 5: poll, 50: poll, 1 << 20: poll,
	} {
		if got := userRetryWait(failures, poll); got != want {
			t.Errorf("after %d failures: %v, want %v", failures, got, want)
		}
	}
	if got := userRetryWait(1, 5*time.Minute); got != time.Minute {
		t.Errorf("first retry %v, want a minute", got)
	}
}

// after a restart that is not due a dump, the first listing emits only the users that changed
// since the saved listing, and saves the new listing time without touching the dump time
func TestUsers_RestartEmitsOnlyChanges(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	ts := func(d time.Duration) string { return now.Add(d).Format(timeFormat) }
	lastDump, lastPass := now.Add(-2*time.Hour), now.Add(-30*time.Minute) // a poll is overdue
	old := testUser(`00uold`, ts(-3*time.Hour), ts(-time.Hour))
	login := testUser(`00ulogin`, ts(-3*time.Hour), ts(-10*time.Minute))
	updated := testUser(`00uupd`, ts(-20*time.Minute), ``)
	edge := testUser(`00uedge`, ts(-33*time.Minute), ``) // inside the overlap before the last listing
	f := newFakeOkta(t, withData(nil, []string{old, login, updated, edge}))
	rt := newTestRuntime(t)
	o := newSSWSIngester(t, f, rt)
	rt.PutString(userDomainKey, testHost)
	rt.PutTime(userDumpKey, lastDump)
	rt.PutTime(userPassKey, lastPass)

	runUsers(t, o, rt, func() bool { return loggedMsg(rt, `DEBUG`, `emitted changed users`) > 0 })
	assertData(t, rt.data(o.userTag), []string{login, updated, edge})
	if n := loggedMsg(rt, `INFO`, `emitted all users`); n != 0 {
		t.Error("dumped every user, the saved dump is recent")
	}
	if got, err := rt.GetTime(userDumpKey); err != nil || !got.Equal(lastDump) {
		t.Errorf("dump time %v, %v; want it left at %v", got, err, lastDump)
	}
	if got, err := rt.GetTime(userPassKey); err != nil || !got.After(lastPass) {
		t.Errorf("listing time %v, %v; want it moved on from %v", got, err, lastPass)
	}
	assertClean(t, rt, f, false)
}

// until a listing completes, users the tracker has not seen count as changed only if they changed
// after its since; afterwards every unseen user is new
func TestUserTracker_RestartWindow(t *testing.T) {
	t.Parallel()
	since := eventBase
	users := newUserTracker()
	users.since = since
	users.begin()
	before, after := eventBase.Add(-time.Second).Format(timeFormat), eventBase.Add(time.Second).Format(timeFormat)
	for _, c := range []struct {
		user    string
		changed bool
	}{
		{testUser(`00ua`, before, ``), false},
		{testUser(`00ub`, before, before), false},
		{testUser(`00uc`, after, ``), true},
		{testUser(`00ud`, before, after), true},
		{testUser(`00ue`, `garbage`, ``), true}, // a time that does not parse is emitted
		{`{"id":"00uf","lastUpdated":"` + before + `","lastLogin":null}`, false},
	} {
		id, st, changed := users.check([]byte(c.user))
		if changed != c.changed {
			t.Errorf("%s: changed %v, want %v", c.user, changed, c.changed)
		}
		users.record(id, st)
	}
	users.prune()
	if !users.since.IsZero() {
		t.Error("the restart window outlived a complete listing")
	}
	users.begin()
	if _, _, changed := users.check([]byte(testUser(`00ug`, before, ``))); !changed {
		t.Error("a user new since the last listing was not reported")
	}
	if _, _, changed := users.check([]byte(testUser(`00ua`, before, ``))); changed {
		t.Error("an unchanged user was reported")
	}
}

func TestUntilNextUserPass(t *testing.T) {
	t.Parallel()
	now := eventBase
	poll, dump := 10*time.Minute, 24*time.Hour
	for name, c := range map[string]struct {
		lastDump, lastPass time.Time
		want               time.Duration
	}{
		`never dumped`:     {time.Time{}, now.Add(-time.Minute), 0},
		`never listed`:     {now.Add(-time.Hour), time.Time{}, 0},
		`poll ahead`:       {now.Add(-time.Hour), now.Add(-4 * time.Minute), 6 * time.Minute},
		`poll overdue`:     {now.Add(-time.Hour), now.Add(-time.Hour), 0},
		`dump before poll`: {now.Add(-dump + 2*time.Minute), now.Add(-time.Minute), 2 * time.Minute},
		`dump overdue`:     {now.Add(-2 * dump), now.Add(-time.Minute), 0},
		`just listed`:      {now.Add(-time.Hour), now, poll},
	} {
		if got := untilNextUserPass(now, c.lastDump, c.lastPass, poll, dump); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func TestParseUser(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]struct {
		id string
		st userState
	}{
		`{"id":"00ua","lastUpdated":"2026-10-01T12:00:00.000Z","lastLogin":"2026-10-01T13:00:00.000Z"}`: {`00ua`, userState{`2026-10-01T12:00:00.000Z`, `2026-10-01T13:00:00.000Z`}},
		`{"id":"00ua","lastUpdated":"2026-10-01T12:00:00.000Z","lastLogin":null}`:                       {`00ua`, userState{LastUpdated: `2026-10-01T12:00:00.000Z`}},
		// keys of the same name further down are not the user's
		`{"profile":{"id":"nested","lastLogin":"x"},"_links":{"self":{"id":"y"}},"id":"00ub","lastUpdated":"t"}`: {`00ub`, userState{LastUpdated: `t`}},
		`{"id":"00uc","lastUpdated":"a\"b"}`: {`00uc`, userState{LastUpdated: `a"b`}},
		`{"id":7,"lastUpdated":"t"}`:         {``, userState{LastUpdated: `t`}},
		`{}`:                                 {},
		`not json`:                           {},
	} {
		if id, st := parseUser([]byte(raw)); id != want.id || st != want.st {
			t.Errorf("%s: got %q %+v, want %q %+v", raw, id, st, want.id, want.st)
		}
	}
}

// pages are walked in place: records go out byte for byte, and a page that is not an array of
// objects fails the listing
func TestHandleUserLogs_Pages(t *testing.T) {
	t.Parallel()
	a, b := testUser(`00ua`, eventBase.Format(timeFormat), ``), testUser(`00ub`, eventBase.Format(timeFormat), ``)
	for name, c := range map[string]struct {
		page string
		want []string
		err  bool
		warn bool // non-user elements skipped
	}{
		`empty`:         {page: `[]`},
		`blank`:         {page: "  \n[ ]\n"},
		`spaced`:        {page: "\n  [\n    " + a + " ,\n\t" + b + "\n  ]\n", want: []string{a, b}},
		`not an array`:  {page: `{"errorCode":"E0000047","errorSummary":"API call exceeded rate limit"}`, err: true},
		`no body`:       {page: ``, err: true},
		`truncated`:     {page: `[` + a + `,` + b[:20], want: []string{a}, err: true},
		`not objects`:   {page: `[null,` + a + `,"00ua",7]`, want: []string{a}, warn: true},
		`missing comma`: {page: `[` + a + ` ` + b + `]`, want: []string{a}, err: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rt := newTestRuntime(t)
			o := &OktaIngester{userTag: 7}
			users := newUserTracker()
			users.begin()
			n, err := o.handleUserLogs(strings.NewReader(c.page), rt, users)
			if (err != nil) != c.err {
				t.Errorf("error %v, want error %v", err, c.err)
			}
			assertData(t, rt.data(o.userTag), c.want)
			if n != len(c.want) {
				t.Errorf("reported %d written, want %d", n, len(c.want))
			}
			if warned := loggedMsg(rt, `WARN`, `not user objects`) > 0; warned != c.warn {
				t.Errorf("warned %v, want %v: %v", warned, c.warn, rt.logged(``))
			}
		})
	}
}
