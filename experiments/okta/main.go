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
	"flag"
	"fmt"
	"gravwell/pkg/utils"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	glog "github.com/gravwell/gravwell/v3/ingest/log"
	"golang.org/x/time/rate"
)

const (
	name = `oktaingester`
	guid = `00000000-0000-0000-0000-000000000001`

	defaultPageSize         = 100
	defaultRequestPerMinute = 60
	defaultRequestBurst     = 10
	defaultRequestTimeout   = 20 * time.Second

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

	startTime        = flag.String("last-log-start-time", "", "timestamp to start ingesting from")
	startUrl         = flag.String("last-log-url", "", "System log next URL")
	batchSize        = flag.Int("batch-size", defaultPageSize, "Number of requests per page")
	requestPerMinute = flag.Int("request-per-minute", defaultRequestPerMinute, "maximum API requests per minute")
	maxBurst         = flag.Int("request-burst", defaultRequestBurst, "maximum burst size for requests")
	seedUsers        = flag.Bool("seed-users", false, "Acquire full user list at startup")
	seedStart        = flag.String("seed-start", "", "timestamp to start user list window (RFC3339)")
	userTagName      = flag.String("user-log-tag-name", "", "Name for user log, if empty userlog is disabled")

	seedStartTs time.Time
)

var (
	latestTS       time.Time
	systemLogsNext *url.URL
)

func main() {
	flag.Parse()
	if *seedStart != `` {
		var err error
		if seedStartTs, err = time.Parse(*seedStart, time.RFC3339); err != nil {
			log.Fatalf("seed-start value %q is invalid: %v\n", *seedStart, err)
		}
	}

	domain := os.Getenv("oktadomain")
	token := os.Getenv("oktatoken")
	if domain == `` || token == `` {
		log.Fatal("missing domain or token")
	}
	latestTS = time.Now().Add(-7 * 24 * time.Hour)
	if *startTime != `` {
		var err error
		if latestTS, err = time.Parse(time.RFC3339Nano, *startTime); err != nil {
			log.Fatal(*startTime, "invalid timestamp format, must be RFC3339Nano", time.RFC3339Nano)
		}
	} else if *startUrl != `` {
		var err error
		if systemLogsNext, err = url.ParseRequestURI(*startUrl); err != nil {
			log.Fatal(*startUrl, "invalid URL", err)
		}
	} else if *batchSize < 1 || *batchSize > 3000 {
		log.Fatal("invalid batch-size, must be > 0 and < 3000", *batchSize)
	} else if *requestPerMinute < 1 || *requestPerMinute > 6000 {
		log.Fatal("invalid request-per-minute, must be > 0 and < 6000", *requestPerMinute)
	}
	log.Println("starting at", latestTS)

	cfg, err := base.GetGeneratorConfig(`json`)
	if err != nil {
		log.Fatal(err)
	}
	var igst *ingest.IngestMuxer
	var src net.IP
	var tag entry.EntryTag
	if igst, src, err = getIngestMuxer(cfg, time.Second); err != nil {
		log.Fatal(err)
	} else if tag, err = igst.GetTag(cfg.Tag); err != nil {
		log.Fatalf("Failed to lookup tag %s: %v\n", cfg.Tag, err)
	}

	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(*requestPerMinute)), *requestPerMinute)
	if *maxBurst > 0 {
		rl.SetBurst(*maxBurst)
	}

	ctx, cf := context.WithCancelCause(context.Background())

	wg := &sync.WaitGroup{}
	if *userTagName != `` {
		userTag, err := igst.NegotiateTag(*userTagName)
		if err != nil {
			igst.Close()
			log.Fatalf("Failed to negotiate user-log-tag-name %q %v\n", *userTagName, err)
		}
		wg.Add(1)
		go userLogRoutine(domain, token, userTag, src, igst, rl, ctx, wg)
	}
	log.Println("Skipping system entries on", tag)
	wg.Add(1)
	go systemLogRoutine(domain, token, tag, src, igst, rl, ctx, wg)

	sig := utils.GetQuitChannel()
	<-sig
	cf(errSignalExit)

	log.Println("Exiting... with last timestamp of", latestTS.Format(time.RFC3339))
	log.Println("Exiting... with system log next of", systemLogsNext)
	if err = igst.Close(); err != nil {
		log.Fatal("Failed to close ingest muxer ", err)
	}
}

func systemLogRoutine(domain, token string, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, rl *rate.Limiter, ctx context.Context, wg *sync.WaitGroup) error {
	rc := newRetryClient(rl, defaultRequestTimeout, defaultBackoff, ctx, defaultRetryCodes)
	defer wg.Done()
	var quit bool
	for quit == false {
		log.Println("Starting requests with", latestTS.Format(time.RFC3339), systemLogsNext)
		if err := getSystemLogs(rc, domain, token, tag, src, igst, rl, ctx); err != nil {
			log.Println("got an error", err)
		}
		if err := igst.Sync(time.Second); err != nil {
			log.Println("Failed to sync ingest muxer ", err)
		}
		select {
		case <-ctx.Done():
			quit = true
		default:
			quit = quitableSleep(ctx, time.Minute)
		}
	}
	return nil
}

func getSystemLogs(rc *retryClient, domain, token string, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, rl *rate.Limiter, ctx context.Context) error {
	var req *http.Request

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://%s", domain), nil)
	if err != nil {
		return err
	}

	if systemLogsNext != nil {
		req.URL = systemLogsNext
	} else {
		req.URL.Path = systemLogsPath
		values := req.URL.Query()
		values.Add(`sortOrder`, `ASCENDING`)
		values.Add(`since`, latestTS.Format(timeFormat))
		values.Add(`limit`, strconv.Itoa(*batchSize))
		req.URL.RawQuery = values.Encode()
	}

	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`Content-Type`, `application/json`)
	req.Header.Set(`Authorization`, fmt.Sprintf(`SSWS %s`, token))

	var quit bool
	for quit == false {
		if err = rl.Wait(ctx); err != nil {
			return err
		}
		resp, err := rc.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
		}

		cnt, ts, err := handleSystemLogs(resp.Body, tag, src, igst)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if cnt > 0 {
			log.Println("GOT", cnt)
			if ts.After(latestTS) {
				latestTS = ts
				log.Println("updating latest", ts)
			}
		}

		links := resp.Header[`Link`]
		if len(links) == 0 {
			return errNoLinks
		}
		next, err := getNext(links)
		if err != nil {
			return err
		} else if systemLogsNext, err = url.Parse(next); err != nil {
			return fmt.Errorf("Bad next URL %w", err)
		}
		req.URL = systemLogsNext
		log.Println("Systemlogs NEXT", next)

		if cnt == 0 {
			quit = quitableSleep(ctx, emptySleepDur)
		} else if cnt < *batchSize {
			//partial page, slow it down
			quit = quitableSleep(ctx, partialSleepDur)
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
			log.Println("zero systemtsd")
			ts = time.Now()
		}
	} else {
		ts = time.Now() // could not find ts
		log.Println("Missed systemtsd")
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
			log.Println("zero usertsd")
			ts = time.Now()
		}
	} else {
		ts = time.Now() // could not find ts
		log.Println("Missed usertsd")
	}
	return
}

func handleSystemLogs(rdr io.Reader, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer) (cnt int, latest time.Time, err error) {
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
			TS:   entry.FromStandard(ts),
			SRC:  src,
			Data: []byte(lg),
		})
	}
	log.Println("system logs got", len(lgs), latest)
	cnt = len(lgs)
	ctx, cf := context.WithTimeout(context.Background(), 5*time.Second)
	defer cf()
	err = igst.WriteBatchContext(ctx, ents)
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

func userLogRoutine(domain, token string, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, rl *rate.Limiter, ctx context.Context, wg *sync.WaitGroup) error {
	var startTs time.Time
	defer wg.Done()

	//check if we are supposed to seed users
	if *seedUsers {
		var start, end time.Time
		log.Println("seeding users from", start.Format(timeFormat), "to", end.Format(timeFormat))
		if err := getUserLogs(start, end, domain, token, tag, src, igst, rl, ctx); err != nil {
			log.Println("getUserLogs error on seed", err)
		}
	} else {
		//NOT doing a full seed, so check the seedStartTs
		if startTs = seedStartTs; startTs.IsZero() {
			startTs = time.Now()
		}
	}
	tckr := time.NewTicker(userLogWindowSize)
loop:
	for {
		var ts time.Time
		select {
		case <-ctx.Done():
			break loop
		case ts = <-tckr.C:
			start := startTs
			end := ts.Add(-1 * userLogWindowLag).Round(time.Second)
			if end.After(start) == false {
				continue //skip this, not sure how this could happen, but skip it
			}
			//just look for changes over the last boundary
			log.Println("Requesting users from", start.Format(timeFormat), "to", end.Format(timeFormat))
			if err := getUserLogs(start, end, domain, token, tag, src, igst, rl, ctx); err != nil {
				log.Println("getUserLogs error", err)
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
		r = fmt.Sprintf(`lastUpdated gt %q and lastUpdated lt %q`, start.Format(timeFormat), end.Format(timeFormat))
	}
	return
}

func getUserLogs(start, end time.Time, domain, token string, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, rl *rate.Limiter, ctx context.Context) error {
	rc := newRetryClient(rl, defaultRequestTimeout, defaultBackoff, ctx, defaultRetryCodes)

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://%s", domain), nil)
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
	req.Header.Set(`Authorization`, fmt.Sprintf(`SSWS %s`, token))

	return linkFollowingRequest(rc, req, tag, src, igst, rl, ctx)
}

func linkFollowingRequest(rc *retryClient, req *http.Request, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, rl *rate.Limiter, ctx context.Context) error {
	log.Println("starting cursor follower with", req.URL.String())
	for {
		//execute the request
		if err := rl.Wait(ctx); err != nil {
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
		cnt, err := handleUserLogs(resp.Body, tag, src, igst)
		resp.Body.Close()
		if err != nil {
			return err
		}
		log.Println("got", cnt, "user updates")

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

func handleUserLogs(rdr io.Reader, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer) (cnt int, err error) {
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
			SRC:  src,
			Data: []byte(lg),
		})
		cnt++
	}
	ctx, cf := context.WithTimeout(context.Background(), 5*time.Second)
	err = igst.WriteBatchContext(ctx, ents)
	cf()
	return
}

func getIngestMuxer(gc base.GeneratorConfig, to time.Duration) (igst *ingest.IngestMuxer, src net.IP, err error) {
	umc := ingest.UniformMuxerConfig{
		Destinations:  gc.ConnSet,
		Tags:          []string{gc.Tag},
		Auth:          gc.Auth,
		Tenant:        gc.Tenant,
		IngesterName:  name,
		IngesterUUID:  guid,
		IngesterLabel: `okta`,
		Logger:        glog.NewDiscardLogger(),
		LogLevel:      gc.LogLevel.String(),
		IngestStreamConfig: config.IngestStreamConfig{
			Enable_Compression: gc.Compression,
		},
	}
	if igst, err = ingest.NewUniformMuxer(umc); err != nil {
		return
	} else if err = igst.Start(); err != nil {
		igst.Close()
		return
	} else if err = igst.WaitForHot(to); err != nil {
		igst.Close()
		return
	}
	if gc.SRC != nil {
		src = gc.SRC
	} else if src, err = igst.SourceIP(); err != nil {
		igst.Close()
	}
	return
}
