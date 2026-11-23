/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package okta implements the okta ingester
package okta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/jsonparser"
	"golang.org/x/time/rate"
)

// export Name, Version, and ID as strings so they are compatible with WASM and any non-native interfaces
const (
	Name    string = `okta`
	ID      string = `okta.ingesters.gravwell.io`
	Version string = `1.1.1` // must be canonical version string with only major.minor.point

	httpBackoff = 10 * time.Second
	httpTimeout = 5 * time.Second

	rateLimitPad     = time.Second     // Okta's rate limit reset time is in whole seconds
	maxRateLimitWait = 2 * time.Minute // Okta's rate limits are per minute, a reset further off than this is not believed
)

const (
	tsKey         = `latest`
	urlKey        = `nextUrl`
	userDumpKey   = `lastUserDump`   // start of the last user dump that completed
	userPassKey   = `lastUserPass`   // start of the last user listing that completed, dump or not
	userDomainKey = `lastUserDomain` // the Domain the user listing times were saved for

	userRetryDelay    = time.Minute     // first wait before retrying a user listing that failed or was skipped
	userChangeOverlap = 5 * time.Minute // after a restart, emit users changed this long before the last listing

	emptySleepDur   = 15 * time.Second // length of time we sleep when no results are returned at all
	partialSleepDur = 3 * time.Second  // length of time we sleep when a partial results page is returned

	systemLogsPath = `/api/v1/logs`
	userLogsPath   = `/api/v1/users`

	timeFormat = `2006-01-02T15:04:05.000Z`

	// allUsersSearch matches every user. lastLogin is not searchable, so a login can only be seen by
	// listing every user and comparing with the last listing.
	allUsersSearch = `lastUpdated gt "1970-01-01T00:00:00.000Z"`
)

var (
	errNoLinks    = errors.New("no links provided")
	errSignalExit = errors.New("program signaled to exit")
	errNoNextLink = errors.New("next link not found")

	// statuses retried on the fixed httpBackoff, along with server errors. A 429 is not one of them:
	// every client waits it out until Okta's rate limit resets instead, see rateLimitWait.
	httpRetryCodes = []int{http.StatusTooEarly}
)

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

type OktaIngester struct {
	cfg            Config
	latestTS       time.Time
	systemLogsNext *url.URL
	systemTag      entry.EntryTag
	userTag        entry.EntryTag
	c              doer
	baseURL        string
	oauthState     // OAuth 2.0 service app state, see oauth.go
}

func NewOktaIngester(c Config, tn hosted.TagNegotiator) (o *OktaIngester, err error) {
	if err = c.Verify(); err != nil {
		return
	}
	o = &OktaIngester{
		cfg:     c,
		baseURL: `https://` + c.Domain,
	}
	if c.useOAuth() {
		// the org URL is the audience of the client assertion, keep it in Okta's canonical lower case
		o.baseURL = strings.ToLower(o.baseURL)
		if err = o.loadPrivateKey(); err != nil {
			o = nil
			return
		}
	}
	if o.systemTag, err = tn.NegotiateTag(oktaTag); err != nil {
		o = nil
		err = fmt.Errorf("failed to negotiate %s tag %w", oktaTag, err)
		return
	} else if o.userTag, err = tn.NegotiateTag(oktaUserTag); err != nil {
		o = nil
		err = fmt.Errorf("failed to negotiate %s tag %w", oktaUserTag, err)
		return
	}
	return
}

func (o *OktaIngester) Run(ctx context.Context, rt hosted.Runtime) (err error) {
	// initialize our "latest timestamp"
	o.latestTS = time.Now().Add(-7 * 24 * time.Hour)

	// try to load it from the runtime
	if ts, err := rt.GetTime(tsKey); err == nil && ts.Before(time.Now()) {
		rt.Info("loaded latest timestamp", log.KV("timestamp", ts))
		o.latestTS = ts
	}

	// try to load a saved URL
	if savedURL, err := rt.GetString(urlKey); err == nil && savedURL != `` {
		if turl, err := url.ParseRequestURI(savedURL); err == nil {
			rt.Info("loaded url checkpoint")
			o.systemLogsNext = turl
		}
	}

	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(o.cfg.Request_Per_Minute)), o.cfg.Request_Per_Minute)
	if o.cfg.Request_Burst > 0 {
		rl.SetBurst(o.cfg.Request_Burst)
	}

	// a cursor saved for another org (the Domain changed) is dropped, the system log restarts from the saved timestamp
	if n := o.systemLogsNext; n != nil && !o.sameOrigin(n) {
		rt.Warn("discarding url checkpoint for another okta domain", log.KV("host", n.Host))
		o.systemLogsNext = nil
	}

	// create a share http client to avoid passing a rate limiter around, and allow connection/resource reuse.
	if o.cfg.useOAuth() {
		o.warnKeyFilePerms(rt)
		d := o.newOAuthDoer(ctx, rl, rt)
		defer d.cli.CloseIdleConnections() // each Run clones its own transport, the token client shares it
		o.c = d
	} else {
		o.c = sswsDoer{next: utils.NewRetryHttpClient(rl, httpTimeout, httpBackoff, ctx, httpRetryCodes), token: o.cfg.Token, lg: rt}
	}

	wg := &sync.WaitGroup{}
	wg.Go(func() {
		o.userLogRoutine(ctx, rt)
	})

	wg.Go(func() {
		o.systemLogRoutine(ctx, rt)
	})

	wg.Wait()
	return
}

// userLogRoutine lists every user each User-Poll-Interval and emits the ones that are new or whose
// lastUpdated or lastLogin changed. Every User-Dump-Interval it emits every user instead.
//
// When the last dump and listing completed is saved, so a restart picks up the schedule rather than
// dumping every user again. The users already emitted are only known in memory, so the first listing
// after a restart emits the users changed since the last listing (less userChangeOverlap) instead.
//
// A listing that fails is retried after userRetryDelay, doubling up to the poll interval. A dump that
// fails is retried as a dump, but does not write again the users it already wrote.
func (o *OktaIngester) userLogRoutine(ctx context.Context, rt hosted.Runtime) {
	pollEvery, dumpEvery := o.cfg.userPollInterval(), o.cfg.userDumpInterval()
	now := time.Now()
	lastDump, lastPass := o.loadUserTimes(rt, now)
	users := newUserTracker()
	if !lastPass.IsZero() {
		users.since = lastPass.Add(-userChangeOverlap)
	}

	wait := untilNextUserPass(now, lastDump, lastPass, pollEvery, dumpEvery)
	rt.Info("starting user log routine",
		log.KV("last-dump", fmtUserTime(lastDump)), log.KV("last-listing", fmtUserTime(lastPass)), log.KV("first-listing-in", wait))
	tmr := time.NewTimer(wait)
	defer tmr.Stop()
	var failures int // consecutive failed listings
	for {
		select {
		case <-ctx.Done():
			rt.Debug("shutting down user log routine", log.KV("reason", ctx.Err()))
			return
		case <-tmr.C:
		}
		if !rt.Alive() {
			// okta can back off and wait if the runtime isn't healthy, but not a whole poll
			tmr.Reset(userRetryDelay)
			continue
		}
		start := time.Now()
		full := lastDump.IsZero() || !start.Before(lastDump.Add(dumpEvery))
		rt.Debug("requesting users", log.KV("full-dump", full))
		n, err := o.getUsers(ctx, rt, users, full)
		if err != nil {
			failures++
			retry := userRetryWait(failures, pollEvery)
			if ctx.Err() == nil {
				rt.Error("failed to get users", log.KV("error", err), log.KV("full-dump", full), log.KV("retry-in", retry))
			}
			tmr.Reset(retry) // a dump is retried as a dump
			continue
		}
		failures = 0
		lastPass = start
		if full {
			lastDump = start
			rt.Info("emitted all users", log.KV("count", n))
		} else {
			rt.Debug("emitted changed users", log.KV("count", n))
		}
		if err = o.saveUserTimes(rt, lastDump, lastPass); err != nil {
			// not fatal, a restart just dumps or lists sooner than it needed to
			rt.Error("failed to save user listing state", log.KV("error", err))
		}
		// timed from the end of the listing, so one that overruns the poll does not start the next at once
		end := time.Now()
		tmr.Reset(untilNextUserPass(end, lastDump, end, pollEvery, dumpEvery))
	}
}

// untilNextUserPass is how long to wait for the next user listing: a poll after the last listing, or
// sooner if a dump falls due first. With no dump or listing on record the listing is due at once.
func untilNextUserPass(now, lastDump, lastPass time.Time, poll, dump time.Duration) time.Duration {
	if lastDump.IsZero() || lastPass.IsZero() {
		return 0
	}
	next := lastPass.Add(poll)
	if due := lastDump.Add(dump); due.Before(next) {
		next = due
	}
	return max(next.Sub(now), 0)
}

// userRetryWait is how long to wait after the given number of consecutive failed listings: a minute,
// doubling up to the poll interval, so a lasting failure such as a missing scope is not hammered
func userRetryWait(failures int, poll time.Duration) time.Duration {
	return min(userRetryDelay<<min(max(failures-1, 0), 16), poll)
}

// loadUserTimes reads when the last user dump and listing completed. Unless both are usable and were
// saved for this Domain, neither is used and the next listing is a dump: duplicates are better than
// missed users, and the times of another org say nothing about this one.
func (o *OktaIngester) loadUserTimes(rt hosted.Runtime, now time.Time) (lastDump, lastPass time.Time) {
	lastDump = loadUserTime(rt, userDumpKey, now)
	lastPass = loadUserTime(rt, userPassKey, now)
	if lastDump.IsZero() && lastPass.IsZero() {
		return // nothing saved, or nothing usable and already reported
	}
	if domain, _ := rt.GetString(userDomainKey); !strings.EqualFold(domain, o.cfg.Domain) {
		rt.Info("dropping user listing state that was not saved for this okta domain", log.KV("saved-domain", domain))
		return time.Time{}, time.Time{}
	}
	if lastDump.IsZero() || lastPass.IsZero() || lastPass.Before(lastDump) {
		rt.Warn("dropping incomplete user listing state",
			log.KV("last-dump", fmtUserTime(lastDump)), log.KV("last-listing", fmtUserTime(lastPass)))
		return time.Time{}, time.Time{}
	}
	return
}

// loadUserTime reads a saved user listing time. One that is missing, unreadable or in the future is
// treated as never.
func loadUserTime(rt hosted.Runtime, key string, now time.Time) time.Time {
	ts, err := rt.GetTime(key)
	switch {
	case errors.Is(err, storage.ErrStorageNotFound):
		return time.Time{}
	case err != nil:
		rt.Warn("ignoring unreadable user listing state", log.KV("key", key), log.KVErr(err))
		return time.Time{}
	case ts.IsZero() || ts.After(now):
		rt.Warn("ignoring invalid user listing state", log.KV("key", key), log.KV("time", ts.Format(time.RFC3339)))
		return time.Time{}
	}
	return ts
}

// saveUserTimes saves when the last dump and listing completed, and the Domain they were for
func (o *OktaIngester) saveUserTimes(rt hosted.Runtime, lastDump, lastPass time.Time) error {
	return errors.Join(
		rt.PutString(userDomainKey, o.cfg.Domain),
		rt.PutTime(userDumpKey, lastDump),
		rt.PutTime(userPassKey, lastPass),
	)
}

func fmtUserTime(t time.Time) string {
	if t.IsZero() {
		return `never`
	}
	return t.UTC().Format(timeFormat)
}

func (o *OktaIngester) systemLogRoutine(ctx context.Context, rt hosted.Runtime) {
	rt.Info("starting system log routine",
		log.KV("start-ts", o.latestTS.Format(time.RFC3339)),
		log.KV("next-url", o.systemLogsNext != nil))
	for {
		if err := o.getSystemLogs(ctx, rt); err != nil {
			rt.Error("system log error", log.KV("error", err))
		}
		if rt.Sleep(time.Minute) { // this sleep will quit if the context cancels
			return
		}
	}
}

func (o *OktaIngester) getSystemLogs(ctx context.Context, rt hosted.Runtime) (err error) {
	// flag failures on the ingester status, a clean page clears it
	defer func() {
		if err != nil && ctx.Err() == nil {
			rt.SetError(err)
		}
	}()
	var req *http.Request

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build request %w", err)
	}

	if o.systemLogsNext != nil {
		req.URL = o.systemLogsNext
	} else {
		req.URL.Path = systemLogsPath
		values := req.URL.Query()
		values.Add(`sortOrder`, `ASCENDING`)
		values.Add(`since`, o.latestTS.UTC().Format(timeFormat))
		values.Add(`limit`, strconv.Itoa(o.cfg.Request_Batch_Size))
		req.URL.RawQuery = values.Encode()
	}

	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`Content-Type`, `application/json`)

	var quit bool
	for !quit && ctx.Err() == nil {
		if !rt.Alive() {
			// okta can back off and wait if the runtime isn't healthy, just loop and wait again
			quit = rt.Sleep(emptySleepDur)
			continue
		}
		resp, err := o.c.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			err = o.statusError(resp)
			utils.DrainResponse(resp)
			return err
		}

		cnt, ts, err := o.handleSystemLogs(resp.Body, rt)
		utils.DrainResponse(resp)
		if err != nil {
			return fmt.Errorf("failed to handle system logs %w", err)
		}
		if cnt > 0 {
			if ts.After(o.latestTS) {
				o.latestTS = ts
			}
		}

		links := resp.Header[`Link`]
		if len(links) == 0 {
			return errNoLinks
		}
		var sln *url.URL
		next, err := getNext(links)
		if err != nil {
			return err
		} else if sln, err = url.Parse(next); err != nil {
			return fmt.Errorf("Bad next URL %w", err)
		} else if !o.sameOrigin(sln) {
			// never follow or save a next link that leaves the org: drop the cursor, the next
			// attempt restarts from the latest timestamp
			o.systemLogsNext = nil
			if serr := errors.Join(o.syncState(rt), rt.PutString(urlKey, ``)); serr != nil {
				rt.Error("failed to sync state", log.KV("error", serr))
			}
			return fmt.Errorf("okta next link points at another host %q", sln.Host)
		}
		req.URL = sln
		o.systemLogsNext = sln
		// sync the timetamp and next URL
		if err = o.syncState(rt); err != nil {
			// failing to sync the state is not fatal, keep eating, we might recover later
			rt.Error("failed to sync state", log.KV("error", err))
		}
		rt.ClearError()

		if cnt == 0 {
			quit = rt.Sleep(emptySleepDur)
		} else if cnt < o.cfg.Request_Batch_Size {
			//partial page, slow it down
			quit = rt.Sleep(partialSleepDur)
		}
	}

	return nil
}

func (o *OktaIngester) syncState(rt hosted.Runtime) (err error) {
	if !o.latestTS.IsZero() {
		if err = rt.PutTime(tsKey, o.latestTS); err != nil {
			return
		}
	}

	if o.systemLogsNext != nil {
		if err = rt.PutString(urlKey, o.systemLogsNext.String()); err != nil {
			return
		}
	}
	return
}

type systemtsdecode struct {
	Published time.Time `json:"published"`
}

func getSystemTS(msg json.RawMessage) (ts time.Time) {
	var tsd systemtsdecode
	if lerr := json.Unmarshal(msg, &tsd); lerr == nil {
		if ts = tsd.Published; ts.IsZero() {
			ts = time.Now()
		}
	} else {
		ts = time.Now() // could not find ts
	}
	return
}

type usertsdecode struct {
	Updated time.Time `json:"lastUpdated"`
}

func getUserTS(msg json.RawMessage) (ts time.Time) {
	var tsd usertsdecode
	if lerr := json.Unmarshal(msg, &tsd); lerr == nil {
		if ts = tsd.Updated; ts.IsZero() {
			ts = time.Now()
		}
	} else {
		ts = time.Now() // could not find ts
	}
	return
}

func (o *OktaIngester) handleSystemLogs(rdr io.Reader, rt hosted.Runtime) (cnt int, latest time.Time, err error) {
	var lgs []json.RawMessage
	if err = json.NewDecoder(rdr).Decode(&lgs); err != nil {
		return
	}
	cnt = len(lgs)
	for i, lg := range lgs {
		ts := getSystemTS(lg)
		if i == 0 {
			latest = ts
		} else if ts.After(latest) {
			latest = ts
		}
		err = rt.Write(entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  o.systemTag,
			Data: lg,
		})
		if err != nil {
			return
		}
	}
	return
}

var rx = regexp.MustCompile(`^\<(?P<url>\S+)\>;\s+rel="next"$`)

func getNext(links []string) (next string, err error) {
	for _, v := range links {
		if subs := rx.FindStringSubmatch(v); len(subs) == 2 {
			next = subs[1]
			return
		}
	}
	err = errNoNextLink
	return
}

// getUsers lists every user and emits the ones the tracker reports as new or changed, or all of
// them when full is set. A dump that fails part way is picked up by the next full call without
// writing again the users it already wrote. It returns the number of users emitted.
func (o *OktaIngester) getUsers(ctx context.Context, rt hosted.Runtime, users *userTracker, full bool) (n int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL, nil)
	if err != nil {
		return
	}

	req.URL.Path = userLogsPath
	values := req.URL.Query()
	values.Add(`limit`, `200`) // users API restricts to 200
	// A listing with no search or filter leaves out DEPROVISIONED users, so a user being deprovisioned
	// would drop out of the listing and never be emitted. A search that matches every user returns
	// them too. See the search and filter notes on List all users:
	// https://developer.okta.com/docs/api/openapi/okta-management/management/tag/User/#tag/User/operation/listUsers
	values.Add(`search`, allUsersSearch)
	req.URL.RawQuery = values.Encode()
	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`Content-Type`, `application/json`)

	if full {
		users.startDump()
	}
	users.begin()
	if n, err = o.linkFollowingRequest(req, rt, users); err == nil {
		// only a complete listing shows which users are gone
		users.prune()
		if full {
			users.endDump()
		}
	} else if isInsufficientScope(err) {
		rt.Warn("the okta.users.read scope is not granted to the service app, users cannot be collected", log.KVErr(err))
	}
	return
}

// sswsDoer is the doer for Token mode, it adds the SSWS token to every request and waits out
// rate limiting. Requests must not carry a body.
type sswsDoer struct {
	next  doer
	token string
	lg    hosted.Logger // optional
}

func (d sswsDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set(`Authorization`, `SSWS `+d.token)
	for {
		resp, err := d.next.Do(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}
		wait := rateLimitWait(resp, httpBackoff)
		utils.DrainResponse(resp)
		logRateLimited(d.lg, req.URL.Path, wait)
		if utils.QuitableSleep(req.Context(), wait) {
			return nil, req.Context().Err()
		}
	}
}

// rateLimitWait is how long to back off after a 429. Okta sends the time its rate limit window resets
// as X-Rate-Limit-Reset, in Unix seconds. It is measured against Okta's own clock, the Date header,
// so clock skew does not matter. Without a usable reset time the fallback is used.
func rateLimitWait(resp *http.Response, fallback time.Duration) time.Duration {
	reset, err := strconv.ParseInt(resp.Header.Get(`X-Rate-Limit-Reset`), 10, 64)
	if err != nil || reset <= 0 {
		return fallback
	}
	now := time.Now()
	if d, err := http.ParseTime(resp.Header.Get(`Date`)); err == nil {
		now = d
	}
	wait := time.Unix(reset, 0).Sub(now) + rateLimitPad
	return min(max(wait, rateLimitPad), maxRateLimitWait)
}

func logRateLimited(lg hosted.Logger, path string, wait time.Duration) {
	if lg != nil {
		lg.Warn("okta rate limit reached, backing off before retrying", log.KV("path", path), log.KV("wait", wait))
	}
}

// statusError describes a non-200 response with what Okta said
func (o *OktaIngester) statusError(resp *http.Response) error {
	e := newAPIError(resp)
	if e.status == http.StatusForbidden && o.cfg.useOAuth() {
		e.msg += `; check that the service app is assigned the Read-only Administrator role and granted the okta.logs.read and okta.users.read scopes`
	}
	return e
}

func (o *OktaIngester) linkFollowingRequest(req *http.Request, rt hosted.Runtime, users *userTracker) (n int, err error) {
	for {
		resp, err := o.c.Do(req)
		if err != nil {
			return n, err
		} else if resp.StatusCode != 200 {
			err = o.statusError(resp)
			utils.DrainResponse(resp)
			return n, err
		}

		//consume results
		cnt, err := o.handleUserLogs(resp.Body, rt, users)
		n += cnt
		utils.DrainResponse(resp)
		if err != nil {
			return n, err
		}
		//try to find the "next" response in the header
		if links := resp.Header[`Link`]; len(links) == 0 {
			// we are done
			break
		} else if next, err := getNext(links); err != nil {
			if err == errNoNextLink {
				// not an error, just at the end
				break
			}
			return n, err
		} else if nu, err := url.Parse(next); err != nil {
			return n, fmt.Errorf("Bad next URL %w", err)
		} else if !o.sameOrigin(nu) {
			return n, fmt.Errorf("okta next link points at another host %q", nu.Host)
		} else {
			req.URL = nu
		}
	}
	return n, nil
}

// sameOrigin reports whether u is on the configured org. Next links are absolute URLs from Okta or
// the state store, credentials must not follow one to another host.
func (o *OktaIngester) sameOrigin(u *url.URL) bool {
	return strings.EqualFold(u.Scheme+`://`+u.Host, o.baseURL)
}

// handleUserLogs writes the users in a page that the tracker wants written: new or changed ones, or
// during a dump every one not yet written. It returns the number written. The page is read once and
// walked in place: each record is written as the bytes Okta sent, and only scanned for the keys the
// tracker needs.
func (o *OktaIngester) handleUserLogs(rdr io.Reader, rt hosted.Runtime, users *userTracker) (n int, err error) {
	page, err := io.ReadAll(rdr)
	if err != nil {
		return
	}
	// ArrayEach skips whatever opens the page, so check it is an array
	if trimmed := bytes.TrimLeft(page, " \t\r\n"); len(trimmed) == 0 || trimmed[0] != '[' {
		return 0, errors.New("users page is not a JSON array")
	}
	var werr error
	var skipped []string // types of the elements that are not user objects
	_, err = jsonparser.ArrayEach(page, func(lg []byte, vt jsonparser.ValueType, _ int, _ error) {
		if werr != nil {
			return // a write failed, skip the rest of the page
		} else if vt != jsonparser.Object {
			// not a user, skip it rather than fail every listing at this page
			skipped = append(skipped, vt.String())
			return
		}
		id, st, changed := users.check(lg)
		if !users.wanted(id, changed) {
			users.record(id, st) // a user first seen after a restart, unchanged since the last listing
			return
		}
		if werr = rt.Write(entry.Entry{
			Tag:  o.userTag,
			TS:   entry.Now(),
			Data: lg, // a slice of page, which is not reused
		}); werr != nil {
			rt.Error("failed to write user log entry", log.KV("error", werr))
			return
		}
		// recorded only once written, so a user that failed to write is emitted on the next pass
		users.record(id, st)
		n++
	})
	if len(skipped) > 0 {
		rt.Warn("skipped elements of a users page that are not user objects",
			log.KV("count", len(skipped)), log.KV("types", strings.Join(skipped, ",")))
	}
	if werr != nil {
		err = werr
	} else if err != nil {
		err = fmt.Errorf("malformed users page: %w", err)
	}
	return
}
