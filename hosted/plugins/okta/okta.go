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
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"golang.org/x/time/rate"
)

// export Name, Version, and ID as strings so they are compatible with WASM and any non-native interfaces
const (
	Name    string = `okta`
	ID      string = `okta.ingesters.gravwell.io`
	Version string = `1.0.0` // must be canonical version string with only major.minor.point

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
}

func NewOktaIngester(c Config, tn hosted.TagNegotiator) (o *OktaIngester, err error) {
	if err = c.Verify(); err != nil {
		return
	}
	o = &OktaIngester{
		cfg: c,
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

	// create a share http client to avoid passing a rate limiter around, and allow connection/resource reuse.
	o.c = utils.NewRetryHttpClient(rl, httpTimeout, httpBackoff, ctx, httpRetryCodes)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.userLogRoutine(ctx, rt)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		o.systemLogRoutine(ctx, rt)
	}()

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
			rt.Debug("requesting users", log.KV("start", start.Format(timeFormat)), log.KV("end", end.Format(timeFormat)))
			if err := o.getUserLogs(start, end, rt); err != nil {
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
		if err := o.getSystemLogs(rt); err != nil {
			rt.Error("system log error", log.KV("error", err))
		}
		if rt.Sleep(time.Minute) { // this sleep will quit if the context cancels
			return
		}
	}
}

func (o *OktaIngester) getSystemLogs(rt hosted.Runtime) error {
	var req *http.Request

	req, err := http.NewRequestWithContext(rt.Context(), http.MethodGet, fmt.Sprintf("https://%s", o.cfg.Domain), nil)
	if err != nil {
		return fmt.Errorf("failed to build request %w", err)
	}

	if o.systemLogsNext != nil {
		req.URL = o.systemLogsNext
	} else {
		req.URL.Path = systemLogsPath
		values := req.URL.Query()
		values.Add(`sortOrder`, `ASCENDING`)
		values.Add(`since`, o.latestTS.Format(timeFormat))
		values.Add(`limit`, strconv.Itoa(o.cfg.Request_Batch_Size))
		req.URL.RawQuery = values.Encode()
	}

	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`Content-Type`, `application/json`)
	req.Header.Set(`Authorization`, fmt.Sprintf(`SSWS %s`, o.cfg.Token))

	var quit bool
	for !quit && rt.Context().Err() == nil {
		if !rt.Alive() {
			// okta can back off and wait if the runtime isn't healthy, just loop and wait again
			quit = rt.Sleep(emptySleepDur)
			continue
		}
		resp, err := o.c.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			utils.DrainResponse(resp)
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
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
		r = fmt.Sprintf(`lastUpdated gt %q and lastUpdated lt %q`, start.Format(timeFormat), end.Format(timeFormat))
	}
	return
}

func (o *OktaIngester) getUserLogs(start, end time.Time, rt hosted.Runtime) error {
	req, err := http.NewRequestWithContext(rt.Context(), http.MethodGet, fmt.Sprintf("https://%s", o.cfg.Domain), nil)
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
	req.Header.Set(`Authorization`, fmt.Sprintf(`SSWS %s`, o.cfg.Token))

	return o.linkFollowingRequest(req, rt)
}

func (o *OktaIngester) linkFollowingRequest(req *http.Request, rt hosted.Runtime) error {
	for {
		resp, err := o.c.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			utils.DrainResponse(resp)
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
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
