package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
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
	thinkstConns map[string]*thinkstHandlerConfig
)

type thinkstHandlerConfig struct {
	token      string
	domain     string
	startTime  time.Time
	thinkstAPI string
	tag        entry.EntryTag
	src        net.IP
	name       string
	wg         *sync.WaitGroup
	proc       *processors.ProcessorSet
	ctx        context.Context
	rate       int
	ot         *objectTracker
}

type thinkstIncident struct {
	Description struct {
		Acknowledged string `json:"acknowledged"`
		Created      string `json:"created"`
		CreatedStd   string `json:"created_std"`
		Description  string `json:"description"`
		DstHost      string `json:"dst_host"`
		DstPort      string `json:"dst_port"`
		Events       []struct {
			InstanceName  string `json:"INSTANCE_NAME"`
			Key           string `json:"KEY,omitempty"`
			Localversion  string `json:"LOCALVERSION"`
			Port          int    `json:"PORT"`
			Remoteversion string `json:"REMOTEVERSION"`
			Username      string `json:"USERNAME"`
			Timestamp     int    `json:"timestamp"`
			TimestampStd  string `json:"timestamp_std"`
			Password      string `json:"PASSWORD,omitempty"`
		} `json:"events"`
		EventsCount        string `json:"events_count"`
		EventsList         string `json:"events_list"`
		FlockID            string `json:"flock_id"`
		FlockName          string `json:"flock_name"`
		IPAddress          string `json:"ip_address"`
		Ippers             string `json:"ippers"`
		LocalTime          string `json:"local_time"`
		Logtype            string `json:"logtype"`
		MacAddress         string `json:"mac_address"`
		MatchedAnnotations struct {
		} `json:"matched_annotations"`
		Name           string `json:"name"`
		NodeID         string `json:"node_id"`
		Notified       string `json:"notified"`
		SrcHost        string `json:"src_host"`
		SrcHostReverse string `json:"src_host_reverse"`
		SrcPort        string `json:"src_port"`
	} `json:"description"`
	HashID      string `json:"hash_id"`
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Updated     string `json:"updated"`
	UpdatedID   int    `json:"updated_id"`
	UpdatedStd  string `json:"updated_std"`
	UpdatedTime string `json:"updated_time"`
}

type thinkstIncidentsResponse struct {
	Cursor struct {
		Next     any `json:"next"`
		NextLink any `json:"next_link"`
		Prev     any `json:"prev"`
		PrevLink any `json:"prev_link"`
	} `json:"cursor"`
	Feed             string            `json:"feed"`
	Incidents        []json.RawMessage `json:"incidents"`
	MaxUpdatedID     int               `json:"max_updated_id"`
	Result           string            `json:"result"`
	Updated          string            `json:"updated"`
	UpdatedStd       string            `json:"updated_std"`
	UpdatedTimestamp int               `json:"updated_timestamp"`
}

type thinkstAuditTrail struct {
	ActionType            string `json:"action_type"`
	AdditionalInformation any    `json:"additional_information"`
	FlockID               string `json:"flock_id"`
	FlockName             string `json:"flock_name"`
	ID                    int    `json:"id"`
	Message               string `json:"message"`
	Timestamp             string `json:"timestamp"`
	User                  string `json:"user"`
	UserAccessRoute       string `json:"user_access_route"`
	UserBrowserAgent      string `json:"user_browser_agent"`
	UserBrowserLanguage   string `json:"user_browser_language"`
	UserIP                string `json:"user_ip"`
}
type thinkstAuditTrailsResponse struct {
	AuditTrail []json.RawMessage `json:"audit_trail"`
	Cursor     struct {
		Next any `json:"next"`
		Prev any `json:"prev"`
	} `json:"cursor"`
	PageCount  int    `json:"page_count"`
	PageNumber int    `json:"page_number"`
	Result     string `json:"result"`
}

func buildThinkstHandlerConfig(cfg *cfgType, src net.IP, ot *objectTracker, lg *log.Logger, igst *ingest.IngestMuxer, ib base.IngesterBase, ctx context.Context, wg *sync.WaitGroup) *thinkstHandlerConfig {

	thinkstConns = make(map[string]*thinkstHandlerConfig)

	for k, v := range cfg.ThinkstConf {
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		// check if there is a statetracker object for each config
		_, ok := ot.Get("thinkst", k)
		if !ok {

			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: time.Now(),
				Key:        "",
			}
			err := ot.Set("thinkst", k, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))

			}
			err = ot.Flush()
			if err != nil {
				lg.Fatal("failed to flush state tracker", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
			}
		}
		//TODO: Fix src
		if src == nil {
			src = net.ParseIP("127.0.0.1")
		}
		hcfg := &thinkstHandlerConfig{
			token:      v.Token,
			domain:     v.Domain,
			startTime:  v.StartTime,
			tag:        tag,
			name:       k,
			thinkstAPI: v.ThinkstAPI,
			src:        src,
			wg:         wg,
			ctx:        ctx,
			ot:         ot,
			rate:       defaultRequestPerMinute,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
		}
		thinkstConns[k] = hcfg
	}

	for _, v := range thinkstConns {

		wg.Add(1) // Increment the counter before starting each goroutine
		go v.run()
	}

	return nil
}

func (h *thinkstHandlerConfig) run() {
	defer h.wg.Done()

	var err error
	cli := &http.Client{
		Timeout: 10 * time.Second,
	}
	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(h.rate)), h.rate)

	var quit bool
	for quit == false {
		//get the latest time from the stateTracker map

		switch h.thinkstAPI {
		case "incident":
			latestTS, ok := h.ot.Get("thinkst", h.name)
			if !ok {
				lg.Fatal("failed to get state tracker", log.KV("listener", h.name), log.KV("tag", h.tag))
			}
			if err = getThinkstIncidentLogs(cli, latestTS.Key, h.src, rl, h, lg, h.ot, ""); err != nil {
				lg.Error("Error getting logs", log.KVErr(err))
			}
		case "audit":
			latestTS, ok := h.ot.Get("thinkst", h.name)
			if !ok {
				lg.Fatal("failed to get state tracker", log.KV("listener", h.name), log.KV("tag", h.tag))
			}
			if err = getThinkstAuditLogs(cli, latestTS.LatestTime, h.src, rl, h, lg, h.ot, ""); err != nil {
				lg.Error("Error getting logs", log.KVErr(err))
			}

		default:
			lg.Error("Failed to find endpoint ", log.KVErr(err))
		}

		err = h.ot.Flush()
		if err != nil {
			lg.Fatal("failed to flush state tracker", log.KV("listener", h.name), log.KV("tag", h.tag), log.KVErr(err))
		}
		select {
		case <-h.ctx.Done():
			quit = true
		default:
			quit = quitableSleep(h.ctx, time.Minute)
		}

	}
	lg.Info("Exiting")
}

func getThinkstIncidentLogs(cli *http.Client, latestTS string, src net.IP, rl *rate.Limiter, h *thinkstHandlerConfig, lg *log.Logger, ot *objectTracker, cursor string) error {
	req, err := http.NewRequest(http.MethodGet, h.domain, nil)
	if err != nil {
		return err
	}
	data := url.Values{}
	if cursor != "" {
		data.Set("cursor", cursor)

	} else if latestTS != "" {
		data.Set("incidents_since", latestTS)
	}

	data.Set("auth_token", h.token)
	data.Set("limit", "100")

	req.URL.RawQuery = data.Encode()
	req.URL, err = url.Parse("https://" + h.domain + thinkstIncidentsDomain + "?" + req.URL.RawQuery)
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}
	req.Header.Set(`accept`, `application/json`)

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

		var d thinkstIncidentsResponse
		if err = json.Unmarshal(data, &d); err != nil {
			return err
		}

		lg.Info(fmt.Sprintf("got %v data elements", len(d.Incidents)))

		var ents []*entry.Entry
		lastEntryKey := latestTS
		for _, v := range d.Incidents {
			// attempt to get the timestamp
			var t thinkstIncident
			var entryTime time.Time
			if err = json.Unmarshal(v, &t); err == nil {
				r, err := time.Parse("2006-01-02 15:04:05 MST-0700", t.UpdatedStd)
				if err != nil {
					entryTime = time.Now()
					lg.Info("could not find ts")
				} else {
					entryTime = r
					if strconv.Itoa(t.UpdatedID) > lastEntryKey {
						lastEntryKey = strconv.Itoa(t.UpdatedID)
					}
				}
			}

			data, err := json.Marshal(v)
			if err != nil {
				lg.Warn("failed to re-pack entry", log.KV("thinkst", h.name), log.KVErr(err))
				continue
			}

			ent := &entry.Entry{
				Tag:  h.tag,
				TS:   entry.FromStandard(entryTime),
				SRC:  src,
				Data: data,
			}

			ents = append(ents, ent)
			lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
		}

		if len(ents) != 0 {
			lg.Info(fmt.Sprintf("updated time position: %v", latestTS))
			for _, v := range ents {
				if err = h.proc.ProcessContext(v, h.ctx); err != nil {
					lg.Error("failed to send entry", log.KVErr(err))
				}
			}
			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: time.Now(),
				Key:        lastEntryKey,
			}
			err := ot.Set("thinkst", h.name, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("thinkst", h.name), log.KV("tag", h.tag), log.KVErr(err))

			}
		}

		if len(d.Incidents) == 0 || d.Cursor.NextLink == nil {
			lg.Info("next_data is null or no more data in this request, reusing latest offset")
			quit = quitableSleep(h.ctx, thinkstEmptySleepDur)
		} else {
			lg.Info(fmt.Sprintf("got next_data URI: %v", d.Cursor.NextLink))
			return getThinkstIncidentLogs(cli, latestTS, src, rl, h, lg, ot, fmt.Sprintf("%v", d.Cursor.Next))
		}
		return nil
	}
	return nil
}

func getThinkstAuditLogs(cli *http.Client, latestTS time.Time, src net.IP, rl *rate.Limiter, h *thinkstHandlerConfig, lg *log.Logger, ot *objectTracker, cursor string) error {
	req, err := http.NewRequest(http.MethodGet, h.domain, nil)
	if err != nil {
		return err
	}
	data := url.Values{}
	if cursor != "" {
		data.Set("cursor", cursor)

	}
	data.Set("auth_token", h.token)
	data.Set("limit", "100")

	req.URL.RawQuery = data.Encode()
	req.URL, err = url.Parse("https://" + h.domain + thinkstAuditTrailDomain + "?" + req.URL.RawQuery)
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}
	req.Header.Set(`accept`, `application/json`)

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

		var d thinkstAuditTrailsResponse
		if err = json.Unmarshal(data, &d); err != nil {
			return err
		}

		lg.Info(fmt.Sprintf("got %v data elements", len(d.AuditTrail)))

		var ents []*entry.Entry
		lastTimeEntry := time.Now()
		for _, v := range d.AuditTrail {

			var at thinkstAuditTrail
			var entryTime time.Time
			//Attempt to unmarshall the object
			if err = json.Unmarshal(v, &at); err == nil {
				i, err := time.Parse("2006-01-02 15:04:05 MST-0700", at.Timestamp)
				if err != nil {
					entryTime = time.Now()
					lg.Info("could not find ts")
				} else {
					entryTime = i
				}

			} else {

				lg.Info("could not unmarshal audit trail")
				return err
			}

			if entryTime.After(latestTS) {
				data, err := json.Marshal(v)
				if err != nil {
					lg.Warn("failed to re-pack entry", log.KV("thinkst", h.name), log.KVErr(err))
					continue
				}

				ent := &entry.Entry{
					Tag:  h.tag,
					TS:   entry.FromStandard(entryTime),
					SRC:  src,
					Data: data,
				}

				ents = append(ents, ent)
				// Grab the largest timestamp as the next lastTimeEntry to update state
				if entryTime.After(lastTimeEntry) {
					lastTimeEntry = entryTime
				}

			}

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
				Key:        "",
			}
			err := ot.Set("thinkst", h.name, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("thinkst", h.name), log.KV("tag", h.tag), log.KVErr(err))

			}
		}

		if len(d.AuditTrail) == 0 || d.Cursor.Next == nil {
			lg.Info("next_data is null or no more data in this request, reusing latest offset")
			quit = quitableSleep(h.ctx, thinkstEmptySleepDur)
		} else {
			lg.Info(fmt.Sprintf("got next_data URI: %v", d.Cursor.Next))
			return getThinkstAuditLogs(cli, latestTS, src, rl, h, lg, ot, fmt.Sprintf("%v", d.Cursor.Next))
		}
	}
	return nil
}
