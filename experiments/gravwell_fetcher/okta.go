/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"golang.org/x/time/rate"
)

var (
	oktaConns             map[string]*oktaHandlerConfig
	errNoLinks            = errors.New("no links provided")
	errSignalExit         = errors.New("program signaled to exit")
	errNoNextLink         = errors.New("next link not found")
	oktaTimeFormat        = `2006-01-02T15:04:05.000Z`
	oktaDefaultRetryCodes = []int{425, 429}
)

type oktaHandlerConfig struct {
	token          string
	domain         string // Normalized base domain (https://<org>.okta.com)
	batchSize      int
	maxBurstSize   int
	seedUsers      bool
	seedUserStart  time.Time
	startTime      time.Time
	systemLogsNext *url.URL
	seedStartTs    time.Time
	latestTS       time.Time
	tag            entry.EntryTag
	src            net.IP
	name           string
	wg             *sync.WaitGroup
	proc           *processors.ProcessorSet
	ctx            context.Context
	rate           int
	ot             *objectTracker
}

type retryClient struct {
	rl                 *rate.Limiter
	cli                *http.Client
	ctx                context.Context
	retryResponseCodes []int
	backoff            time.Duration
}

func newRetryClient(rl *rate.Limiter, timeout, backoff time.Duration, ctx context.Context, retryCodes []int) *retryClient {
	if retryCodes == nil {
		retryCodes = oktaDefaultRetryCodes
	}
	if timeout <= 0 {
		timeout = oktaDefaultRequestTimeout
	}
	if backoff <= 0 {
		backoff = oktaDefaultBackoff
	}
	return &retryClient{
		rl: rl,
		cli: &http.Client{
			Timeout: timeout,
		},
		ctx:                ctx,
		retryResponseCodes: retryCodes,
		backoff:            backoff,
	}
}

func (rc *retryClient) Do(req *http.Request) (resp *http.Response, err error) {
	if rc == nil {
		return nil, errors.New("retry client not ready")
	}
	if req == nil {
		return nil, errors.New("nil request")
	}
	for {
		if rc.rl != nil {
			if err = rc.rl.Wait(rc.ctx); err != nil {
				return nil, err
			}
		}
		if resp, err = rc.cli.Do(req.WithContext(rc.ctx)); err != nil {
			// Allow context cancellation to exit quickly
			if rc.ctx.Err() != nil {
				return
			}
			lg.Error("Retrying due to request error", log.KV("url", req.URL.String()), log.KVErr(err))
		} else if resp.StatusCode != http.StatusOK {
			drainResponse(resp)
			if !rc.isRecoverableStatus(resp.StatusCode) {
				lg.Error("Aborting retry due to response code", log.KV("status", resp.Status), log.KV("code", resp.StatusCode))
				err = fmt.Errorf("non-recoverable status code %s (%d)", resp.Status, resp.StatusCode)
				return
			}
			lg.Info("Retrying due to response code", log.KV("status", resp.Status), log.KV("code", resp.StatusCode))
		} else {
			break
		}
		if quitableSleep(rc.ctx, rc.backoff) {
			break
		}
	}
	return
}

func (rc *retryClient) isRecoverableStatus(status int) bool {
	if status >= 500 {
		return true
	}
	for _, v := range rc.retryResponseCodes {
		if v == status {
			return true
		}
	}
	return false
}

func buildOktaHandlerConfig(cfg *cfgType, src net.IP, ot *objectTracker, lg *log.Logger, igst *ingest.IngestMuxer, ib base.IngesterBase, ctx context.Context, wg *sync.WaitGroup) *oktaHandlerConfig {
	oktaConns = make(map[string]*oktaHandlerConfig)

	for k, v := range cfg.OktaConf {
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		// Ensure state
		if _, ok := ot.Get("okta", k); !ok {
			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: time.Now(),
				Key:        json.RawMessage(`{"key":"null"}`),
			}
			if err := ot.Set("okta", k, state, false); err != nil {
				lg.Fatal("failed to set state tracker", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
			}
			if err := ot.Flush(); err != nil {
				lg.Fatal("failed to flush state tracker", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
			}
		}

		if src == nil {
			src = net.ParseIP("127.0.0.1")
		}
		base := normalizeBase(v.OktaDomain)

		// RateLimt; Default to global if not set
		ratePerMin := defaultRequestPerMinute
		if v.RateLimit > 0 {
			ratePerMin = v.RateLimit
		}

		hcfg := &oktaHandlerConfig{
			token:         v.OktaToken,
			domain:        base,
			startTime:     v.StartTime,
			batchSize:     v.BatchSize,
			maxBurstSize:  v.MaxBurstSize,
			seedUsers:     v.SeedUsers,
			seedUserStart: v.SeedUserStart,
			tag:           tag,
			name:          k,
			src:           src,
			wg:            wg,
			ctx:           ctx,
			ot:            ot,
			rate:          ratePerMin,
		}
		if ps, err := cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
		} else {
			hcfg.proc = ps
		}
		oktaConns[k] = hcfg
	}

	// Run users when SeedUsers=true; system logs
	for k, v := range oktaConns {
		burst := v.rate
		if v.maxBurstSize > 0 {
			burst = v.maxBurstSize
		}
		rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(v.rate)), burst)

		// Initial
		if !v.seedUserStart.IsZero() {
			v.seedStartTs = v.seedUserStart
		}
		v.latestTS = time.Now().Add(-7 * 24 * time.Hour)
		if !v.startTime.IsZero() {
			v.latestTS = v.startTime
		}

		// Users
		if v.seedUsers {
			wg.Add(1)
			go func(h *oktaHandlerConfig, ut entry.EntryTag, r *rate.Limiter) {
				if err := userLogRoutine(h, ut, r); err != nil && !errors.Is(err, errSignalExit) {
					lg.Fatal("retrieving userlog failed", log.KV("listener", h.name), log.KVErr(err))
				}
			}(v, v.tag, rl)
		}

		// System Logs
		if strings.EqualFold(k, "okta-system") || strings.Contains(strings.ToLower(k), "system") {
			lg.Info("Starting system log routine", log.KV("listener", v.name), log.KV("tag", v.tag))
			wg.Add(1)
			go func(h *oktaHandlerConfig, r *rate.Limiter) {
				if err := systemLogRoutine(h, r); err != nil && !errors.Is(err, errSignalExit) {
					lg.Fatal("retrieving systemlog failed", log.KV("listener", h.name), log.KVErr(err))
				}
			}(v, rl)
		}
	}

	return nil
}

func systemLogRoutine(h *oktaHandlerConfig, rl *rate.Limiter) error {
	rc := newRetryClient(rl, oktaDefaultRequestTimeout, oktaDefaultBackoff, h.ctx, oktaDefaultRetryCodes)
	defer h.wg.Done()

	var quit bool
	for !quit {
		lg.Info("System log request window", log.KV("latestTS", h.latestTS.Format(time.RFC3339)), log.KV("next", h.systemLogsNext))
		if err := getSystemLogs(h, rc); err != nil {
			if errors.Is(err, errSignalExit) {
				return err
			}
			lg.Error("System log pull error", log.KVErr(err))
		}
		select {
		case <-h.ctx.Done():
			quit = true
		default:
			quit = quitableSleep(h.ctx, time.Minute)
		}
	}
	return nil
}

func getSystemLogs(h *oktaHandlerConfig, rc *retryClient) error {
	req, err := http.NewRequest(http.MethodGet, h.domain, nil)
	if err != nil {
		return err
	}

	if h.systemLogsNext != nil {
		req.URL = h.systemLogsNext
	} else {
		req.URL.Path = oktaSystemLogsPath
		q := req.URL.Query()
		q.Add("sortOrder", "ASCENDING")
		q.Add("since", h.latestTS.Format(oktaTimeFormat))
		if h.batchSize > 0 {
			q.Add("limit", strconv.Itoa(h.batchSize))
		}
		req.URL.RawQuery = q.Encode()
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("SSWS %s", h.token))

	for {
		resp, err := rc.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
		}

		cnt, ts, err := handleSystemLogs(h, resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if cnt > 0 && ts.After(h.latestTS) {
			h.latestTS = ts
			lg.Info("Updated latest system TS", log.KV("ts", ts))
			setObjectTracker(h.ot, "okta", h.name, h.latestTS)
		}

		links := resp.Header["Link"]
		if len(links) == 0 {
			return nil
		}
		next, err := getNext(links)
		if err != nil {
			if errors.Is(err, errNoNextLink) {
				return nil
			}
			return err
		}
		if h.systemLogsNext, err = url.Parse(next); err != nil {
			return fmt.Errorf("bad next URL: %w", err)
		}
		req.URL = h.systemLogsNext

		if cnt == 0 {
			if quitableSleep(h.ctx, oktaEmptySleepDur) {
				return nil
			}
		} else if h.batchSize > 0 && cnt < h.batchSize {
			if quitableSleep(h.ctx, oktaPartialSleepDur) {
				return nil
			}
		}
	}
}

type systemtsdecode struct {
	Published time.Time `json:"published"`
}

func getSystemTS(msg json.RawMessage) (ts time.Time) {
	var tsd systemtsdecode
	if lerr := json.Unmarshal(msg, &tsd); lerr == nil {
		ts = tsd.Published
		if ts.IsZero() {
			ts = time.Now()
			lg.Info("Zero system timestamp")
		}
	} else {
		ts = time.Now()
		lg.Error("Failed to decode system timestamp")
	}
	return
}

func handleSystemLogs(h *oktaHandlerConfig, rdr io.Reader) (cnt int, latest time.Time, err error) {
	var lgs []json.RawMessage
	if err = json.NewDecoder(rdr).Decode(&lgs); err != nil {
		return
	}
	ents := make([]*entry.Entry, 0, len(lgs))
	for i, lgmsg := range lgs {
		ts := getSystemTS(lgmsg)
		if i == 0 || ts.After(latest) {
			latest = ts
		}
		ents = append(ents, &entry.Entry{
			Tag:  h.tag,
			TS:   entry.FromStandard(ts),
			SRC:  h.src,
			Data: []byte(lgmsg),
		})
	}
	cnt = len(lgs)
	lg.Info("System logs batch", log.KV("count", cnt), log.KV("latest", latest))
	if cnt > 0 {
		if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
			lg.Error("failed to send system entries", log.KVErr(err))
		}
	}
	return
}

var rx = regexp.MustCompile(`^\<(?P<url>\S+)\>;\s+rel="next"$`)

func getNext(links []string) (next string, err error) {
	for _, v := range links {
		toks := strings.Split(v, ",")
		for _, tok := range toks {
			tok = strings.TrimSpace(tok)
			if subs := rx.FindStringSubmatch(tok); len(subs) == 2 {
				return subs[1], nil
			}
		}
	}
	return "", errNoNextLink
}

func userLogRoutine(h *oktaHandlerConfig, tag entry.EntryTag, rl *rate.Limiter) error {
	defer h.wg.Done()

	if h.seedUsers {
		start := h.seedStartTs
		if start.IsZero() && !h.startTime.IsZero() {
			start = h.startTime
		}
		if start.IsZero() {
			start = time.Now().Add(-24 * time.Hour)
		}
		end := time.Now()
		lg.Info("Seeding users", log.KV("start", start.Format(oktaTimeFormat)), log.KV("end", end.Format(oktaTimeFormat)))
		if err := getUserLogs(h, start, end, tag, rl); err != nil {
			lg.Error("user seed error", log.KVErr(err))
		}
	}

	// Increment
	startTs := h.seedStartTs
	if startTs.IsZero() {
		startTs = time.Now()
	}
	tckr := time.NewTicker(oktaUserLogWindowSize)
	defer tckr.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return nil
		case ts := <-tckr.C:
			start := startTs
			end := ts.Add(-1 * oktaUserLogWindowLag).Round(time.Second)
			if !end.After(start) {
				continue
			}
			lg.Info("Requesting users", log.KV("start", start.Format(oktaTimeFormat)), log.KV("end", end.Format(oktaTimeFormat)))
			if err := getUserLogs(h, start, end, tag, rl); err != nil {
				lg.Error("user pull error", log.KVErr(err))
			} else {
				startTs = end
			}
		}
	}
}

func lastUpdatedFilter(start, end time.Time) (r string) {
	if start.IsZero() && end.IsZero() {
		return
	}
	if !start.IsZero() && !end.IsZero() {
		// Okta Users API SCIM lastUpdated
		r = fmt.Sprintf(`lastUpdated gt %q and lastUpdated lt %q`, start.Format(oktaTimeFormat), end.Format(oktaTimeFormat))
	}
	return
}

func getUserLogs(h *oktaHandlerConfig, start, end time.Time, tag entry.EntryTag, rl *rate.Limiter) error {
	rc := newRetryClient(rl, oktaDefaultRequestTimeout, oktaDefaultBackoff, h.ctx, oktaDefaultRetryCodes)

	req, err := http.NewRequest(http.MethodGet, h.domain, nil)
	if err != nil {
		return err
	}
	req.URL.Path = oktaUserLogsPath
	q := req.URL.Query()
	q.Add("limit", "200") // Okta Users API limit
	if filter := lastUpdatedFilter(start, end); filter != "" {
		// "Search" param expected for users
		q.Add("search", filter)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("SSWS %s", h.token))

	return linkFollowingRequest(h, rc, req, tag)
}

func linkFollowingRequest(h *oktaHandlerConfig, rc *retryClient, req *http.Request, tag entry.EntryTag) error {
	lg.Info("Starting cursor follower", log.KV("url", req.URL.String()))
	for {
		resp, err := rc.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
		}

		cnt, err := handleUserLogs(h, resp.Body, tag)
		resp.Body.Close()
		if err != nil {
			return err
		}
		lg.Info("Got user updates", log.KV("count", cnt))

		if links := resp.Header["Link"]; len(links) == 0 {
			break
		} else if next, err := getNext(links); err != nil {
			if errors.Is(err, errNoNextLink) {
				break
			}
			return err
		} else if req.URL, err = url.Parse(next); err != nil {
			return fmt.Errorf("bad next URL: %w", err)
		}
	}
	return nil
}

func handleUserLogs(h *oktaHandlerConfig, rdr io.Reader, tag entry.EntryTag) (cnt int, err error) {
	var lgs []json.RawMessage
	if err = json.NewDecoder(rdr).Decode(&lgs); err != nil {
		return
	}
	ents := make([]*entry.Entry, 0, len(lgs))
	for _, lgmsg := range lgs {
		ents = append(ents, &entry.Entry{
			Tag:  tag,
			TS:   entry.Now(),
			SRC:  h.src,
			Data: []byte(lgmsg),
		})
		cnt++
	}
	if cnt > 0 {
		if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
			lg.Error("failed to send user entries", log.KVErr(err))
		}
	}
	return
}

func drainResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
}

// Normalize base domain
func normalizeBase(in string) string {
	s := strings.TrimSpace(in)
	s = strings.TrimRight(s, "/")
	s = strings.Replace(s, "-admin.okta.com", ".okta.com", 1)
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "https://" + s
	}
	return s
}