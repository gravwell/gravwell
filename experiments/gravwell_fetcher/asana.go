package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"golang.org/x/time/rate"
)

var (
	asanaConns map[string]*asanaHandlerConfig
)

type asanaHandlerConfig struct {
	token     string
	workspace string
	startTime time.Time
	tag       entry.EntryTag
	src       net.IP
	wg        *sync.WaitGroup
	proc      *processors.ProcessorSet
	ctx       context.Context
	rate      int
	ot        *objectTracker
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

func buildAsanaHandlerConfig(cfg *cfgType, src net.IP, ot *objectTracker, lg *log.Logger, igst *ingest.IngestMuxer, ib base.IngesterBase, ctx context.Context, wg *sync.WaitGroup) *asanaHandlerConfig {

	asanaConns = make(map[string]*asanaHandlerConfig)

	for k, v := range cfg.AsanaConf {
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		// check if there is a statetracker object for each config
		_, ok := ot.Get("asana", v.Workspace)
		if !ok {

			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: time.Now(),
				Key:        "",
			}
			err := ot.Set("asana", v.Workspace, state, false)
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
		hcfg := &asanaHandlerConfig{
			token:     v.Token,
			workspace: v.Workspace,
			startTime: v.StartTime,
			tag:       tag,
			src:       src,
			wg:        wg,
			ctx:       ctx,
			ot:        ot,
			rate:      defaultRequestPerMinute,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
		}
		asanaConns[k] = hcfg
	}

	for _, v := range asanaConns {
		go v.run()
	}

	return nil
}

func (h *asanaHandlerConfig) run() {
	var err error
	cli := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       100,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: false,
			MaxConnsPerHost:    10,
		},
	}
	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(h.rate)), h.rate)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	var quit bool
	for quit == false {
		//get the latest time from the stateTracker map
		latestTS, ok := h.ot.Get("asana", h.workspace)
		if !ok {
			lg.Fatal("failed to get state tracker", log.KV("listener", h.workspace), log.KV("tag", h.tag))
		}

		if err = getAsanaLogs(cli, latestTS.LatestTime, h.src, rl, h, lg, h.ot); err != nil {
			lg.Error("Error getting logs", log.KVErr(err))
		}

		select {
		case <-h.ctx.Done():
			quit = true
		case <-ticker.C:
			if err := h.ot.Flush(); err != nil {
				lg.Error("failed to flush state tracker", log.KVErr(err))
			}
		default:
			quit = quitableSleep(h.ctx, time.Minute)
		}
	}
	lg.Info("Exiting")
}

func getAsanaLogs(cli *http.Client, latestTS time.Time, src net.IP, rl *rate.Limiter, h *asanaHandlerConfig, lg *log.Logger, ot *objectTracker) error {
	req, err := http.NewRequest(http.MethodGet, asanaDomain, nil)
	if err != nil {
		return err
	}

	req.URL, err = url.Parse(asanaDomain + fmt.Sprintf(asanaURL, h.workspace, latestTS.Format(time.RFC3339)))
	if err != nil {
		return err
	}
	req.Header.Set(`accept`, `application/json`)
	req.Header.Set(`authorization`, fmt.Sprintf("Bearer %v", h.token))

	var quit bool
	for quit == false {
		if err = rl.Wait(h.ctx); err != nil {
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

		lg.Info(fmt.Sprintf("got page with length %v", len(data)))

		var d asanaData
		if err = json.Unmarshal(data, &d); err != nil {
			return err
		}

		lg.Info(fmt.Sprintf("got %v data elements", len(d.Data)))

		var ents []*entry.Entry
		lastTimeEntry := time.Now()
		for _, v := range d.Data {
			// attempt to get the timestamp
			var t asanaDataItem
			if err = json.Unmarshal(v, &t); err != nil {
				t.Timestamp = time.Now() // could not find ts
				lg.Info("could not find ts")
			} else if t.Timestamp.After(latestTS) {
				latestTS = t.Timestamp
			}

			ent := &entry.Entry{
				TS:   entry.FromStandard(t.Timestamp),
				SRC:  src,
				Data: []byte(v),
			}

			ents = append(ents, ent)
			latestTS = latestTS.Add(time.Second)
		}
		lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
		if len(ents) != 0 {
			lg.Info(fmt.Sprintf("updated time position: %v", latestTS))
			for _, v := range ents {
				if err = h.proc.ProcessContext(v, h.ctx); err != nil {
					lg.Error("failed to send entry", log.KVErr(err))
				}
			}
			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: lastTimeEntry,
			}
			err := ot.Set("asana", h.workspace, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("workspace", h.workspace), log.KV("tag", h.tag), log.KVErr(err))

			}
		}

		if len(d.Data) == 0 || d.NextPage == nil || d.NextPage.URI == "" {
			lg.Info("next_data is null or no more data in this request, reusing latest offset")
			quit = quitableSleep(h.ctx, asanaEmptySleepDur)
		} else {
			lg.Info(fmt.Sprintf("got next_data URI: %v", d.NextPage.URI))
			req.URL, err = url.Parse(d.NextPage.URI)
			if err != nil {
				return fmt.Errorf("Bad next URL %w", err)
			}
		}
	}

	return nil
}
