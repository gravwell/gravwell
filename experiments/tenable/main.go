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
	"os"
	"time"

	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	glog "github.com/gravwell/gravwell/v3/ingest/log"
	"golang.org/x/time/rate"
)

const (
	name = `tenableingester`
	guid = `00000000-0000-0000-1000-000000000003`

	defaultRequestPerMinute = 6

	domain         = "https://cloud.tenable.com"
	scansURL       = "/scans"
	scanDetailsURL = "/scans/%s"

	emptySleepDur = 15 * time.Second // length of time we sleep when no results are returned at all
)

var (
	errNoLinks    = errors.New("no links provided")
	errSignalExit = errors.New("program signaled to exit")

	requestPerMinute = flag.Int("request-per-minute", defaultRequestPerMinute, "maximum API requests per minute")
)

func main() {
	var err error
	flag.Parse()
	access := os.Getenv("tenableaccess")
	secret := os.Getenv("tenablesecret")
	if access == `` || secret == `` {
		log.Fatal("missing access id or secret id")
	}

	if *requestPerMinute < 1 || *requestPerMinute > 6000 {
		log.Fatal("invalid request-per-minute, must be > 0 and < 6000", *requestPerMinute)
	}

	scanMap = make(map[string]bool)

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

	cli := &http.Client{
		Timeout: 10 * time.Second,
	}

	//get our context fired up with a quit signal lambda funtion
	ctx, ccf := context.WithCancelCause(context.Background())
	go func(cf context.CancelCauseFunc) {
		sig := utils.GetQuitChannel()
		<-sig
		ccf(errSignalExit)
	}(ccf)

	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(*requestPerMinute)), *requestPerMinute)

	var quit bool
	for quit == false {
		if err = getLogs(cli, access, secret, tag, src, igst, rl, ctx); err != nil {
			log.Println("got an error", err)
		}
		if err = igst.Sync(time.Second); err != nil {
			log.Println("Failed to sync ingest muxer ", err)
		}
		select {
		case <-ctx.Done():
			quit = true
		default:
			quit = quitableSleep(ctx, time.Minute)
		}
	}
	log.Println("Exiting...")
	if err = igst.Close(); err != nil {
		log.Fatal("Failed to close ingest muxer ", err)
	}
}

func getLogs(cli *http.Client, access, secret string, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, rl *rate.Limiter, ctx context.Context) error {
	req, err := http.NewRequest(http.MethodGet, domain, nil)
	if err != nil {
		return err
	}
	req.URL.Path = scansURL
	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`X-ApiKeys`, fmt.Sprintf("accessKey=%s;secretKey=%s", access, secret))

	var quit bool
	for quit == false {
		if err = rl.Wait(ctx); err != nil {
			return err
		}
		resp, err := cli.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		log.Printf("got scan overview with size %v", len(data))
		ent := &entry.Entry{
			TS:   entry.Now(),
			SRC:  src,
			Data: data,
		}

		// we'll deal with any ingest errors later, we still need to worry about scan results
		scanErr := igst.WriteBatchContext(ctx, []*entry.Entry{ent})
		err = checkResults(data, cli, access, secret, tag, src, igst, ctx)

		// coalesce errors
		if scanErr != nil && err != nil {
			return fmt.Errorf("multiple errors: %v, %v", scanErr, err)
		} else if scanErr != nil {
			return scanErr
		} else if err != nil {
			return err
		}
	}

	return nil
}

var scanMap map[string]bool

type scanResults struct {
	Scans []scanResult `json:"scans"`
}

type scanResult struct {
	Status string `json:"status"`
	UUID   string `json:"schedule_uuid"`
}

// checkResults does the following:
//   - any keys in the scanmap that aren't in these results is discarded (the scan no longer exists)
//   - any results with 100% completion are checked against the scanmap.
//   - if they're in there, we skip, as we've already ingested those results
//   - if they aren't in there, we ingest the results and put the UUID in the scanmap
//
// Sample scanresults (from the api docs, not real data):
//
//	{
//	 "scans": [
//	   {
//	     "control": true,
//	     "creation_date": 1667846780,
//	     "enabled": false,
//	     "id": 32,
//	     "last_modification_date": 1667955712,
//	     "legacy": false,
//	     "name": "Example Scan 1",
//	     "owner": "example@example.com",
//	     "policy_id": 30,
//	     "read": false,
//	     "schedule_uuid": "26cf08d3-3f94-79f4-8038-996376eabd4f186741fe15533e70",
//	     "shared": true,
//	     "status": "completed",
//	     "template_uuid": "131a8e52-3ea6-a291-ec0a-d2ff0619c19d7bd788d6be818b65",
//	     "has_triggers": false,
//	     "type": "remote",
//	     "permissions": 16,
//	     "user_permissions": 128,
//	     "uuid": "c1d84965-c4c6-47ea-99c6-2111b803bcae",
//	     "wizard_uuid": "931a8e52-3ea6-a291-ec0a-d2ff0619c19d7bd788d6be818b65",
//	     "progress": 100,
//	     "total_targets": 3072
//	   },
func checkResults(data []byte, cli *http.Client, access, secret string, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, ctx context.Context) error {
	// attempt to decode the portion of the scan results that we need
	var r scanResults
	err := json.Unmarshal(data, &r)
	if err != nil {
		return err
	}

	log.Printf("scanresults with %v items", len(r.Scans))

	// cleanup the scan map
	for k, _ := range scanMap {
		var found bool
		for _, v := range r.Scans {
			if k == v.UUID {
				found = true
				break
			}
		}
		if !found {
			log.Printf("removing scan %v from scanmap", k)
			delete(scanMap, k)
			// this is really noisy
			//	} else {
			//		log.Printf("skipping scan %v, already ingested", k)
		}
	}

	// check for new completed scans
	for _, v := range r.Scans {
		if ctx.Err() != nil {
			log.Printf("exiting scanresults loop: %v", ctx.Err())
			return nil
		}
		if v.Status == "completed" && !scanMap[v.UUID] {
			err = getScanDetails(v.UUID, cli, access, secret, tag, src, igst, ctx)
			if err != nil {
				return err
			}
			scanMap[v.UUID] = true
		}
	}
	return nil
}

type scanDetails struct {
	Info scanInfo `json:"info"`
}

type scanInfo struct {
	Timestamp int64 `json:"timestamp"`
}

func getScanDetails(uuid string, cli *http.Client, access, secret string, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, ctx context.Context) error {
	log.Printf("new completed scan %v", uuid)

	req, err := http.NewRequest(http.MethodGet, domain, nil)
	if err != nil {
		return err
	}
	req.URL.Path = fmt.Sprintf(scanDetailsURL, uuid)
	req.Header.Set(`Accept`, `application/json`)
	req.Header.Set(`X-ApiKeys`, fmt.Sprintf("accessKey=%s;secretKey=%s", access, secret))

	resp, err := cli.Do(req)
	if err != nil {
		return err
	} else if resp.StatusCode != 200 {
		resp.Body.Close()
		return fmt.Errorf("invalid status code %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body.Close()

	// attempt to grab the timestamp
	var d scanDetails
	err = json.Unmarshal(data, &d)
	var ts entry.Timestamp
	if err != nil {
		// just use now?
		log.Printf("failed to get timestamp for scan %v", uuid)
		ts = entry.Now()
	} else {
		ts = entry.FromStandard(time.Unix(d.Info.Timestamp, 0))
	}

	log.Printf("scan %v with length %v, time %v", uuid, len(data), ts.String())

	ent := &entry.Entry{
		TS:   ts,
		SRC:  src,
		Data: data,
	}

	ctx, cf := context.WithTimeout(context.Background(), 5*time.Second)
	defer cf()
	return igst.WriteBatchContext(ctx, []*entry.Entry{ent})
}

func getIngestMuxer(gc base.GeneratorConfig, to time.Duration) (igst *ingest.IngestMuxer, src net.IP, err error) {
	umc := ingest.UniformMuxerConfig{
		Destinations:  gc.ConnSet,
		Tags:          []string{gc.Tag},
		Auth:          gc.Auth,
		Tenant:        gc.Tenant,
		IngesterName:  name,
		IngesterUUID:  guid,
		IngesterLabel: `tenable`,
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

func quitableSleep(ctx context.Context, to time.Duration) (quit bool) {
	tmr := time.NewTimer(to)
	defer tmr.Stop()
	select {
	case <-tmr.C:
	case <-ctx.Done():
		quit = true
	}
	return
}
