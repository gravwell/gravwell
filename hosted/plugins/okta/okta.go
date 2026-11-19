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
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"golang.org/x/time/rate"
)

// export Name, Version, and ID as strings so they are compatible with WASM and any non-native interfaces
const (
	Name    string = `okta`
	ID      string = `okta.ingesters.gravwell.io`
	Version string = `1.1.0` // must be canonical version string with only major.minor.point

	httpBackoff = 10 * time.Second
	httpTimeout = 5 * time.Second
)

const (
	tsKey  = `latest`
	urlKey = `nextUrl`

	emptySleepDur   = 15 * time.Second // length of time we sleep when no results are returned at all
	partialSleepDur = 3 * time.Second  // length of time we sleep when a partial results page is returned

	userLogWindowSize = 10 * time.Minute // how often do we check for updated users
	userLogWindowLag  = time.Minute      // how far do we lag the window when taking samples

	systemLogsPath = `/api/v1/logs`
	userLogsPath   = `/api/v1/users`

	timeFormat = `2006-01-02T15:04:05.000Z`
)

var (
	errNoLinks    = errors.New("no links provided")
	errSignalExit = errors.New("program signaled to exit")
	errNoNextLink = errors.New("next link not found")

	httpRetryCodes = []int{425, 429} // basically just Too Early and too many requests
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
	if n := o.systemLogsNext; n != nil && !strings.EqualFold(n.Scheme+`://`+n.Host, o.baseURL) {
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
		o.c = sswsDoer{next: utils.NewRetryHttpClient(rl, httpTimeout, httpBackoff, ctx, httpRetryCodes), token: o.cfg.Token}
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

// userLogRoutine is just a goroutine that
func (o *OktaIngester) userLogRoutine(ctx context.Context, rt hosted.Runtime) {
	startTs := time.Now()
	tckr := time.NewTicker(userLogWindowSize)
	for {
		var ts time.Time
		select {
		case <-ctx.Done():
			rt.Debug("shutting down user log routine", log.KV("reason", ctx.Err()))
			return
		case ts = <-tckr.C:
			if !rt.Alive() {
				continue // okta can back off and wait if the runtime isn't healthy
			}
			start := startTs
			end := ts.Add(-1 * userLogWindowLag).Round(time.Second)
			if !end.After(start) {
				continue //skip this, not sure how this could happen, but skip it
			}
			//just look for changes over the last boundary
			rt.Debug("requesting users", log.KV("start", start.UTC().Format(timeFormat)), log.KV("end", end.UTC().Format(timeFormat)))
			if err := o.getUserLogs(ctx, start, end, rt); err != nil {
				rt.Error("failed to get users", log.KV("error", err))
			} else {
				//success, update last ts run
				startTs = end
			}
		}
	}
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

func lastUpdatedFilter(start, end time.Time) (r string) {
	if start.IsZero() && end.IsZero() {
		return //neither are set
	}
	if !start.IsZero() && !end.IsZero() {
		//both
		// timeFormat ends in a literal Z, the times must be UTC
		r = fmt.Sprintf(`lastUpdated gt %q and lastUpdated lt %q`, start.UTC().Format(timeFormat), end.UTC().Format(timeFormat))
	}
	return
}

func (o *OktaIngester) getUserLogs(ctx context.Context, start, end time.Time, rt hosted.Runtime) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL, nil)
	if err != nil {
		return err
	}

	req.URL.Path = userLogsPath
	values := req.URL.Query()
	values.Add(`limit`, `200`) // users API restricts to 200
	if filter := lastUpdatedFilter(start, end); filter != `` {
		values.Add(`search`, filter)
	}
	req.URL.RawQuery = values.Encode()
	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`Content-Type`, `application/json`)

	err = o.linkFollowingRequest(req, rt)
	if isInsufficientScope(err) {
		rt.Warn("the okta.users.read scope is not granted to the service app, users cannot be collected", log.KVErr(err))
	}
	return err
}

// sswsDoer is the doer for Token mode, it adds the SSWS token to every request
type sswsDoer struct {
	next  doer
	token string
}

func (d sswsDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set(`Authorization`, `SSWS `+d.token)
	return d.next.Do(req)
}

// statusError describes a non-200 response with what Okta said
func (o *OktaIngester) statusError(resp *http.Response) error {
	e := newAPIError(resp)
	if e.status == http.StatusForbidden && o.cfg.useOAuth() {
		e.msg += `; check that the service app is assigned the Read-only Administrator role and granted the okta.logs.read and okta.users.read scopes`
	}
	return e
}

func (o *OktaIngester) linkFollowingRequest(req *http.Request, rt hosted.Runtime) error {
	for {
		resp, err := o.c.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			err = o.statusError(resp)
			utils.DrainResponse(resp)
			return err
		}

		//consume results
		err = o.handleUserLogs(resp.Body, rt)
		utils.DrainResponse(resp)
		if err != nil {
			return err
		}
		//try to find the "next" response in the header
		if links := resp.Header[`Link`]; len(links) == 0 {
			// we are done
			break
		} else if next, err := getNext(links); err != nil {
			if err == errNoNextLink {
				// not an error, just at the end
				err = nil
				break
			}
			return err
		} else if req.URL, err = url.Parse(next); err != nil {
			return fmt.Errorf("Bad next URL %w", err)
		}
	}
	return nil
}

func (o *OktaIngester) handleUserLogs(rdr io.Reader, rt hosted.Runtime) (err error) {
	var lgs []json.RawMessage
	if err = json.NewDecoder(rdr).Decode(&lgs); err != nil {
		return
	}
	for _, lg := range lgs {
		err = rt.Write(entry.Entry{
			Tag:  o.userTag,
			TS:   entry.Now(),
			Data: lg,
		})
		if err != nil {
			rt.Error("failed to write user log entry", log.KV("error", err))
			break
		}
	}
	return
}
