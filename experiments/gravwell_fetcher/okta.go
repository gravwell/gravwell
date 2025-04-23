package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v4/ingest"

	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/base"
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
	domain         string
	userTag        string
	batchSize      int
	maxBurstSize   int
	seedUsers      bool
	seedUserStart  string
	startTime      string
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
		timeout = oktaDefaultTimeout
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
			//log the error and continue
			if rc.ctx.Err() != nil {
				//context cancelled
				return
			}
			// some sort of error, backoff and then continue
			lg.Error("Retrying due to error requesting", log.KV("request", req), log.KVErr(err))
		} else if resp.StatusCode != http.StatusOK {
			//drain the body just in case
			drainResponse(resp)
			//check if this status code is something we can recover from
			if rc.isRecoverableStatus(resp.StatusCode) == false {
				lg.Error("Aborting Retry due to response code", log.KV("status", resp.Status), log.KV("code", resp.StatusCode))
				err = fmt.Errorf("non-recoverable status code %s (%d)", resp.Status, resp.StatusCode)
				return
			}
			lg.Info("Retrying due to response code", log.KV("status", resp.Status), log.KV("code", resp.StatusCode))
		} else {
			//all good
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
		// it is server side, so yes
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

		// check if there is a statetracker object for each config
		_, ok := ot.Get("okta", k)
		if !ok {

			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: time.Now(),
				Key:        "",
			}
			err := ot.Set("okta", k, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))

			}
			err = ot.Flush()
			if err != nil {
				lg.Fatal("failed to flush state tracker", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
			}
		}
		if src == nil {
			src = net.ParseIP("127.0.0.1")
		}

		hcfg := &oktaHandlerConfig{
			token:         v.OktaToken,
			domain:        v.OktaDomain,
			startTime:     v.StartTime,
			userTag:       v.UserTag,
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
			rate:          defaultRequestPerMinute,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
		}
		oktaConns[k] = hcfg
	}

	for k, v := range oktaConns {
		rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(defaultRequestPerMinute)), defaultRequestPerMinute)
		if v.maxBurstSize > 0 {
			rl.SetBurst(v.maxBurstSize)
		}
		//TODO: Make sure this is handled well
		if v.seedUserStart != `` {
			var err error
			if v.seedStartTs, err = time.Parse(oktaTimeFormat, v.seedUserStart); err != nil {
				lg.Fatal("Invalid seed-start value", log.KV("value", v.seedUserStart), log.KVErr(err))
			}
		}

		v.latestTS = time.Now().Add(-7 * 24 * time.Hour)
		if v.startTime != `` {
			var err error
			if v.latestTS, err = time.Parse(oktaTimeFormat, v.startTime); err != nil {
				lg.Fatal("Invalid timestamp format", log.KV("format", "RFC3339Nano"), log.KV("value", v.startTime))
			}
		}
		//TODO: Make suer the user tag thing is handled well. I'm not sure if this user section is setup well
		if v.userTag != "" {
			userTag, err := igst.GetTag(v.userTag)
			if err != nil {
				lg.Fatal("failed to resolve tag", log.KV("listener", k), log.KV("tag", v.userTag), log.KVErr(err))
			}
			wg.Add(1)
			go func() {
				err := userLogRoutine(v, userTag, rl)
				if err != nil {
					lg.Fatal("retrieving userlog failed with error", log.KV("listener", k), log.KV("listner", v.name), log.KVErr(err))
				}
			}()
		}
		lg.Info("Skipping system entries", log.KV("tag", v.tag))
		wg.Add(1)
		go func() {
			err := systemLogRoutine(v, rl)
			if err != nil {
				lg.Fatal("retrieving systemlog failed with error", log.KV("listener", k), log.KV("listner", v.name), log.KVErr(err))
			}
		}()
	}

	return nil
}

func systemLogRoutine(h *oktaHandlerConfig, rl *rate.Limiter) error {
	rc := newRetryClient(rl, oktaDefaultRequestTimeout, oktaDefaultBackoff, h.ctx, oktaDefaultRetryCodes)
	defer h.wg.Done()
	var quit bool
	for quit == false {
		lg.Info("Starting requests", log.KV("latestTS", h.latestTS.Format(time.RFC3339)), log.KV("systemLogsNext", h.systemLogsNext))
		if err := getSystemLogs(h, rc, rl); err != nil {
			lg.Error("Error getting logs", log.KVErr(err))
		}
		//TODO: Is this still needed if we use the ctx processor
		//if err := igst.Sync(time.Second); err != nil {
		//	log.Println("Failed to sync ingest muxer ", err)
		//}
		select {
		case <-h.ctx.Done():
			quit = true
		default:
			quit = quitableSleep(h.ctx, time.Minute)
		}
	}
	return nil
}

func getSystemLogs(h *oktaHandlerConfig, rc *retryClient, rl *rate.Limiter) error {
	var req *http.Request

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://%s", h.domain), nil)
	if err != nil {
		return err
	}

	if h.systemLogsNext != nil {
		req.URL = h.systemLogsNext
	} else {
		req.URL.Path = oktaSystemLogsPath
		values := req.URL.Query()
		values.Add(`sortOrder`, `ASCENDING`)
		values.Add(`since`, h.latestTS.Format(oktaTimeFormat))
		values.Add(`limit`, strconv.Itoa(h.batchSize))
		req.URL.RawQuery = values.Encode()
	}

	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`Content-Type`, `application/json`)
	req.Header.Set(`Authorization`, fmt.Sprintf(`SSWS %s`, h.token))

	var quit bool
	for quit == false {
		if err = rl.Wait(h.ctx); err != nil {
			return err
		}
		resp, err := rc.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
		}

		cnt, ts, err := handleSystemLogs(h, resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if cnt > 0 {
			lg.Info("Got entries", log.KV("count", cnt))
			if ts.After(h.latestTS) {
				h.latestTS = ts
				lg.Info("Updating latest timestamp", log.KV("timestamp", ts))
			}
		}

		links := resp.Header[`Link`]
		if len(links) == 0 {
			return errNoLinks
		}
		next, err := getNext(links)
		if err != nil {
			return err
		} else if h.systemLogsNext, err = url.Parse(next); err != nil {
			return fmt.Errorf("Bad next URL %w", err)
		}
		req.URL = h.systemLogsNext
		lg.Info("System logs next", log.KV("next", next))

		if cnt == 0 {
			quit = quitableSleep(h.ctx, oktaEmptySleepDur)
		} else if cnt < h.batchSize {
			//partial page, slow it down
			quit = quitableSleep(h.ctx, oktaPartialSleepDur)
		}
	}

	return nil
}

type systemtsdecode struct {
	Published time.Time `json:"published"`
}

func getSystemTS(msg json.RawMessage) (ts time.Time) {
	var tsd systemtsdecode
	if lerr := json.Unmarshal(msg, &tsd); lerr == nil {
		if ts = tsd.Published; ts.IsZero() {
			lg.Info("Zero system timestamp")
			ts = time.Now()
		}
	} else {
		ts = time.Now() // could not find ts
		lg.Error("Missed system timestamp")
	}
	return
}

func handleSystemLogs(h *oktaHandlerConfig, rdr io.Reader) (cnt int, latest time.Time, err error) {
	var lgs []json.RawMessage
	var ents []*entry.Entry
	if err = json.NewDecoder(rdr).Decode(&lgs); err != nil {
		return
	}
	ents = make([]*entry.Entry, 0, len(lgs))
	for i, lg := range lgs {
		ts := getSystemTS(lg)
		if i == 0 {
			latest = ts
		} else if ts.After(latest) {
			latest = ts
		}
		ents = append(ents, &entry.Entry{
			Tag:  h.tag,
			TS:   entry.FromStandard(ts),
			SRC:  h.src,
			Data: []byte(lg),
		})
	}
	lg.Info("System logs retrieved", log.KV("count", len(lgs)), log.KV("latest", latest))
	cnt = len(lgs)
	if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
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

func userLogRoutine(h *oktaHandlerConfig, tag entry.EntryTag, rl *rate.Limiter) error {
	var startTs time.Time
	defer h.wg.Done()

	//check if we are supposed to seed users
	if h.seedUsers {
		var start, end time.Time
		lg.Info("Seeding users", log.KV("start", start.Format(oktaTimeFormat)), log.KV("end", end.Format(oktaTimeFormat)))
		if err := getUserLogs(h, start, end, tag, rl); err != nil {
			lg.Error("Error getting user logs on seed", log.KVErr(err))
		}
	} else {
		//NOT doing a full seed, so check the seedStartTs
		if startTs = h.seedStartTs; startTs.IsZero() {
			startTs = time.Now()
		}
	}
	tckr := time.NewTicker(oktaUserLogWindowSize)
loop:
	for {
		var ts time.Time
		select {
		case <-h.ctx.Done():
			break loop
		case ts = <-tckr.C:
			start := startTs
			end := ts.Add(-1 * oktaUserLogWindowLag).Round(time.Second)
			if end.After(start) == false {
				continue //skip this, not sure how this could happen, but skip it
			}
			//just look for changes over the last boundary
			lg.Info("Requesting users", log.KV("start", start.Format(oktaTimeFormat)), log.KV("end", end.Format(oktaTimeFormat)))
			if err := getUserLogs(h, start, end, tag, rl); err != nil {
				lg.Error("Error getting user logs", log.KVErr(err))
			} else {
				//success, update last ts run
				startTs = end
			}
		}
	}
	return nil
}

func lastUpdatedFilter(start, end time.Time) (r string) {
	if start.IsZero() && end.IsZero() {
		return //neither are set
	}
	if start.IsZero() == false && end.IsZero() == false {
		//both
		r = fmt.Sprintf(`lastUpdated gt %q and lastUpdated lt %q`, start.Format(oktaTimeFormat), end.Format(oktaTimeFormat))
	}
	return
}

func getUserLogs(h *oktaHandlerConfig, start, end time.Time, tag entry.EntryTag, rl *rate.Limiter) error {
	rc := newRetryClient(rl, oktaDefaultRequestTimeout, oktaDefaultBackoff, h.ctx, oktaDefaultRetryCodes)

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://%s", h.domain), nil)
	if err != nil {
		return err
	}

	req.URL.Path = oktaUserLogsPath
	values := req.URL.Query()
	values.Add(`limit`, `200`) // users API restricts to 200
	if filter := lastUpdatedFilter(start, end); filter != `` {
		values.Add(`search`, filter)
	}
	req.URL.RawQuery = values.Encode()
	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`Content-Type`, `application/json`)
	req.Header.Set(`Authorization`, fmt.Sprintf(`SSWS %s`, h.token))

	return linkFollowingRequest(h, rc, req, tag, rl)
}

func linkFollowingRequest(h *oktaHandlerConfig, rc *retryClient, req *http.Request, tag entry.EntryTag, rl *rate.Limiter) error {
	lg.Info("Starting cursor follower", log.KV("url", req.URL.String()))
	for {
		//execute the request
		if err := rl.Wait(h.ctx); err != nil {
			return err
		}
		resp, err := rc.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
		}

		//consume results
		cnt, err := handleUserLogs(h, resp.Body, tag)
		resp.Body.Close()
		if err != nil {
			return err
		}
		lg.Info("Got user updates", log.KV("count", cnt))

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

func handleUserLogs(h *oktaHandlerConfig, rdr io.Reader, tag entry.EntryTag) (cnt int, err error) {
	var lgs []json.RawMessage
	var ents []*entry.Entry
	if err = json.NewDecoder(rdr).Decode(&lgs); err != nil {
		return
	}
	ents = make([]*entry.Entry, 0, len(lgs))
	for _, lg := range lgs {
		ents = append(ents, &entry.Entry{
			Tag:  tag,
			TS:   entry.Now(),
			SRC:  h.src,
			Data: []byte(lg),
		})
		cnt++
	}
	if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
	}
	return
}

func drainResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	return
}
