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
	"time"

	"github.com/gravwell/gravwell/v4/generators/base"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	glog "github.com/gravwell/gravwell/v4/ingest/log"
	"golang.org/x/time/rate"
)

const (
	name = `asanaingester`
	guid = `00000000-0000-0000-2000-000000000002`

	defaultRequestPerMinute = 6

	domain   = "https://app.asana.com"
	asanaURL = "/api/1.0/workspaces/%v/audit_log_events?start_at=%v"

	emptySleepDur = 15 * time.Second // length of time we sleep when no results are returned at all
)

var (
	errNoLinks    = errors.New("no links provided")
	errSignalExit = errors.New("program signaled to exit")

	requestPerMinute = flag.Int("request-per-minute", defaultRequestPerMinute, "maximum API requests per minute")
	startTime        = flag.String("last-log-start-time", "", "timestamp to start ingesting from")
)

func main() {
	var err error
	flag.Parse()
	token := os.Getenv("asanatoken")
	workspace := os.Getenv("asanaworkspace")
	if token == `` || workspace == `` {
		log.Fatal("missing token or workspace id")
	}

	if *requestPerMinute < 1 || *requestPerMinute > 6000 {
		log.Fatal("invalid request-per-minute, must be > 0 and < 6000", *requestPerMinute)
	}

	latestTS := time.Now().Add(-7 * 24 * time.Hour)
	if *startTime != `` {
		var err error
		if latestTS, err = time.Parse(time.RFC3339Nano, *startTime); err != nil {
			log.Fatal(*startTime, "invalid timestamp format, must be RFC3339Nano", time.RFC3339Nano)
		}
	}

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
		if err = getLogs(cli, token, workspace, latestTS, tag, src, igst, rl, ctx); err != nil {
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

type asanaData struct {
	Data     []json.RawMessage `json:"data"`
	NextPage *next_page        `json:"next_page"`
}

type asanaDataItem struct {
	Timestamp time.Time `json:"created_at"`
}

type next_page struct {
	Offset string `json:"offset"`
	Path   string `json:"path"`
	URI    string `json:"uri"`
}

func getLogs(cli *http.Client, token, workspace string, latestTS time.Time, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, rl *rate.Limiter, ctx context.Context) error {
	req, err := http.NewRequest(http.MethodGet, domain, nil)
	if err != nil {
		return err
	}

	req.URL, err = url.Parse(domain + fmt.Sprintf(asanaURL, workspace, latestTS.Format(time.RFC3339)))
	if err != nil {
		return err
	}
	req.Header.Set(`accept`, `application/json`)
	req.Header.Set(`authorization`, fmt.Sprintf("Bearer %v", token))

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

		log.Printf("got page with length %v", len(data))

		var d asanaData
		if err = json.Unmarshal(data, &d); err != nil {
			return err
		}

		log.Printf("got %v data elements", len(d.Data))

		var ents []*entry.Entry
		for _, v := range d.Data {
			// attempt to get the timestamp
			var t asanaDataItem
			if err = json.Unmarshal(v, &t); err != nil {
				t.Timestamp = time.Now() // could not find ts
				log.Print("could not find ts")
			} else if t.Timestamp.After(latestTS) {
				latestTS = t.Timestamp
			}

			ent := &entry.Entry{
				TS:   entry.FromStandard(t.Timestamp),
				SRC:  src,
				Data: []byte(v),
			}

			ents = append(ents, ent)
		}

		if len(ents) != 0 {
			log.Printf("updated time position: %v", latestTS)
			err = igst.WriteBatchContext(ctx, ents)
			if err != nil {
				return err
			}
		}

		if len(d.Data) == 0 || d.NextPage == nil || d.NextPage.URI == "" {
			log.Print("next_data is null or no more data in this request, reusing latest offset")
			quit = quitableSleep(ctx, emptySleepDur)
		} else {
			log.Printf("got next_data URI: %v", d.NextPage.URI)
			req.URL, err = url.Parse(d.NextPage.URI)
			if err != nil {
				return fmt.Errorf("Bad next URL %w", err)
			}
		}
	}

	return nil
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
