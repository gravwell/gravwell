package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	duoapi "github.com/duosecurity/duo_api_golang"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/base"

	"golang.org/x/time/rate"
)

/*
Change this variable to duo Conns. and change the handler to duoHandlerConfig
*/
var (
	duoConns map[string]*duoHandlerConfig
)

/*
This is the custom handler for the new fetcher type.

The following are manditory
: tag, src, wg, proc, ctx, rate, ot

Everything else is brought in from the config
*/
type duoHandlerConfig struct {
	domain     string
	secret     string
	key        string
	startTime  time.Time
	tag        entry.EntryTag
	name       string
	duoAPI     string
	src        net.IP
	wg         *sync.WaitGroup
	proc       *processors.ProcessorSet
	ctx        context.Context
	rate       int
	ot         *objectTracker
	httpClient *http.Client
	maxRetries int
}

/*
Bring in any new types needed to pull out the data
*/
type accountResponse struct {
	Stat     string          `json:"stat"`
	Response json.RawMessage `json:"response"`
}

type adminResponse struct {
	Stat     string            `json:"stat"`
	Response []json.RawMessage `json:"response"`
}

type adminItem struct {
	Timestamp int64 `json:"timestamp"`
}

type activityResponse struct {
	Stat     string                   `json:"stat"`
	Response activityInternalResponse `json:"response"`
}

type activityInternalResponse struct {
	Items    []json.RawMessage `json:"items"`
	Metadata activityMetaData  `json:"metadata"`
}

type activityMetaData struct {
	Offset string `json:"next_offset"`
	Total  int64  `json:"total_objects"`
}

type activityItem struct {
	TS time.Time `json:"ts"`
}

type authResponse struct {
	Stat     string               `json:"stat"`
	Response authInternalResponse `json:"response"`
}

type authInternalResponse struct {
	Items    []json.RawMessage `json:"authlogs"`
	Metadata authMetaData      `json:"metadata"`
}

type authMetaData struct {
	Offset []string `json:"next_offset"`
	Total  int64    `json:"total_objects"`
}

type authItem struct {
	TS int64 `json:"timestamp"`
}

type endpointResponse struct {
	Stat     string            `json:"stat"`
	Response []json.RawMessage `json:"response"`
}

type endpointItem struct {
	TS int64 `json:"last_updated"`
}

type trustResponse struct {
	Stat     string                `json:"stat"`
	Response trustInternalResponse `json:"response"`
}

type trustInternalResponse struct {
	Events   []json.RawMessage `json:"events"`
	Metadata trustMetaData     `json:"metadata"`
}

type trustMetaData struct {
	Offset string `json:"next_offset"`
}

type trustItem struct {
	TS int64 `json:"surfaced_timestamp"`
}

func buildDuoHandlerConfig(cfg *cfgType, src net.IP, ot *objectTracker, lg *log.Logger, igst *ingest.IngestMuxer, ib base.IngesterBase, ctx context.Context, wg *sync.WaitGroup) *duoHandlerConfig {

	duoConns = make(map[string]*duoHandlerConfig)
	for k, v := range cfg.DuoConf {
		tag, err := igst.GetTag(v.Tag_Name)
		// check if there is a statetracker object for each config
		_, ok := ot.Get("duo", k)
		if !ok {

			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: time.Now(),
				Key:        "",
			}
			err := ot.Set("duo", k, state, false)
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
		hcfg := &duoHandlerConfig{
			domain:    v.Domain,
			secret:    v.Secret,
			key:       v.Key,
			startTime: v.StartTime,
			duoAPI:    v.DuoAPI,
			name:      k,
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
		duoConns[k] = hcfg
	}

	for _, v := range duoConns {
		wg.Add(1)
		go v.run()
	}

	return nil
}

func (h *duoHandlerConfig) run() {
	defer h.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			if err := h.ot.Flush(); err != nil {
				lg.Error("failed to flush state tracker", log.KVErr(err))
			}
		default:
			var err error
			//get our API rate limiter built up, start with a full buckets
			rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(h.rate)), h.rate)

			//Build duo api client
			api := duoapi.NewDuoApi(h.key, h.secret, h.domain, "")

			var quit bool
			for quit == false {
				//get the latest time from the stateTracker map
				latestOT, ok := h.ot.Get("duo", h.name)
				if !ok {
					lg.Fatal("failed to get state tracker", log.KV("listener", h.domain), log.KV("tag", h.tag))
				}

				if err = getDuoLogs(api, latestOT.LatestTime, h.src, rl, h, lg, h.ot); err != nil {
					lg.Error("Error getting logs", log.KVErr(err))
				}

				err = h.ot.Flush()
				if err != nil {
					lg.Fatal("failed to flush state tracker", log.KV("listener", h.domain), log.KV("tag", h.tag), log.KVErr(err))
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
	}
}

func getDuoLogs(api *duoapi.DuoApi, latestTS time.Time, src net.IP, rl *rate.Limiter, h *duoHandlerConfig, lg *log.Logger, ot *objectTracker) error {
	// get all the tags we need
	var err error

	var quit bool
	for quit == false {
		if err := rl.Wait(h.ctx); err != nil {
			return err
		}

		switch h.duoAPI {
		case "account":
			latestTS, err = getDuoAccountLog(api, h.tag, latestTS, src, h, lg)
			if err != nil {
				return err
			}
			setObjectTracker(ot, "duo", h.name, latestTS)
		case "admin":

			latestTS, err = getDuoAdminLog(api, h.tag, latestTS, src, h, lg)
			if err != nil {
				return err
			}
			setObjectTracker(ot, "duo", h.name, latestTS)
		case "activity":
			latestTS, err = getDuoActivityLog(api, h.tag, latestTS, src, h, lg, "")
			if err != nil {
				return err
			}
			setObjectTracker(ot, "duo", h.name, latestTS)
		case "telephony":
			latestTS, err = getDuoTelephonyLog(api, h.tag, latestTS, src, h, lg, "")
			if err != nil {
				return err
			}
			setObjectTracker(ot, "duo", h.name, latestTS)
		case "authentication":
			latestTS, err = getDuoAuthLog(api, h.tag, latestTS, src, h, lg, "")
			if err != nil {
				return err
			}
			setObjectTracker(ot, "duo", h.name, latestTS)
		case "endpoint":
			latestTS, err = getDuoEndpointLog(api, h.tag, latestTS, src, h, lg, 0)
			if err != nil {
				return err
			}
			setObjectTracker(ot, "duo", h.name, latestTS)
		case "trust":
			latestTS, err = getDuoTrustLog(api, h.tag, latestTS, src, h, lg, "")
			if err != nil {
				return err
			}
			setObjectTracker(ot, "duo", h.name, latestTS)
		default:
			lg.Error("Failed to find endpoint ", log.KVErr(err))
		}

		quit = quitableSleep(h.ctx, duoEmptySleepDur)

	}
	return nil
}

func getDuoAccountLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, h *duoHandlerConfig, lg *log.Logger) (time.Time, error) {
	var accountR accountResponse

	_, data, err := api.SignedCall("GET", duoAccountLog, nil)
	if err != nil {
		lg.Error(fmt.Sprintf("Failed SignedCall for Duo %s", h.name), log.KVErr(err))
		return latestTS, err
	}
	err = json.Unmarshal(data, &accountR)
	if err != nil {
		lg.Error(fmt.Sprintf("Failed JSON unmarshall for Duo %s", h.name), log.KVErr(err))
		return latestTS, err
	}
	lg.FatalCode(0, fmt.Sprintf("got account log page with length %v", len([]byte(accountR.Response))), log.KVErr(err))

	ent := &entry.Entry{
		TS:   entry.Now(),
		SRC:  src,
		Data: []byte(accountR.Response),
		Tag:  tag,
	}

	if err = h.proc.ProcessContext(ent, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("ingested %v entries in tag %v", "1", h.tag))
	return time.Now(), nil
}

func getDuoAdminLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, h *duoHandlerConfig, lg *log.Logger) (time.Time, error) {
	var adminR adminResponse
	vals := url.Values{}
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.Unix()))
	_, data, err := api.SignedCall("GET", duoAdminLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &adminR)
	if err != nil {
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("got %v admin logs", len(adminR.Response)), log.KVErr(err))

	var ents []*entry.Entry
	for _, v := range adminR.Response {
		var tsItem adminItem
		err = json.Unmarshal(v, &tsItem)
		if err != nil {
			return latestTS, err
		}

		if time.Unix(tsItem.Timestamp, 0).After(latestTS) {
			latestTS = time.Unix(tsItem.Timestamp+1, 0) // note the plus 1... this is sad.
		}

		ents = append(ents, &entry.Entry{
			TS:   entry.FromStandard(time.Unix(tsItem.Timestamp, 0)),
			SRC:  src,
			Data: []byte(v),
			Tag:  tag,
		})
	}
	lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
	if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("moving latest admin timestamp to %v", latestTS))

	return time.Now(), nil
}

func getDuoActivityLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, h *duoHandlerConfig, lg *log.Logger, offset string) (time.Time, error) {
	var resp activityResponse
	vals := url.Values{}
	nextTS := time.Now().Add(-2 * time.Minute)
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.UnixMilli()))
	vals.Set("maxtime", fmt.Sprintf("%v", nextTS.UnixMilli())) // duo api says we can't poll up to two minutes before now...
	vals.Set("limit", "1000")
	if offset != "" {
		lg.Info(fmt.Sprintf("paginating activity log %v", offset))
		vals.Set("next_offset", offset)
	}
	_, data, err := api.SignedCall("GET", duoActivityLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("got %v activity logs", len(resp.Response.Items)))

	var ents []*entry.Entry
	for _, v := range resp.Response.Items {
		var tsItem activityItem
		err = json.Unmarshal(v, &tsItem)
		if err != nil {
			return latestTS, err
		}

		ents = append(ents, &entry.Entry{
			TS:   entry.FromStandard(tsItem.TS),
			SRC:  src,
			Data: []byte(v),
			Tag:  tag,
		})
	}
	lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
	if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("moving latest admin timestamp to %v", latestTS))
	if resp.Response.Metadata.Offset != "" {
		// paginate
		return getDuoActivityLog(api, tag, latestTS, src, h, lg, resp.Response.Metadata.Offset)
	}
	lg.Info(fmt.Sprintf("moving latest admin timestamp to %v", nextTS))
	return nextTS, nil
}

// uses the same json shapes as activity logs
func getDuoTelephonyLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, h *duoHandlerConfig, lg *log.Logger, offset string) (time.Time, error) {
	var resp activityResponse
	vals := url.Values{}
	nextTS := time.Now().Add(-2 * time.Minute)
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.UnixMilli()))
	vals.Set("maxtime", fmt.Sprintf("%v", nextTS.UnixMilli())) // duo api says we can't poll up to two minutes before now...
	vals.Set("limit", "1000")
	if offset != "" {
		lg.Info(fmt.Sprintf("paginating telephony log %v", offset))
		vals.Set("next_offset", offset)
	}
	_, data, err := api.SignedCall("GET", duoTelephonyLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("got %v telephony logs", len(resp.Response.Items)))

	var ents []*entry.Entry
	for _, v := range resp.Response.Items {
		var tsItem activityItem
		err = json.Unmarshal(v, &tsItem)
		if err != nil {
			return latestTS, err
		}

		ents = append(ents, &entry.Entry{
			TS:   entry.FromStandard(tsItem.TS),
			SRC:  src,
			Data: []byte(v),
			Tag:  tag,
		})
	}
	lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
	if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("moving latest admin timestamp to %v", latestTS))
	if resp.Response.Metadata.Offset != "" {
		// paginate
		return getDuoTelephonyLog(api, tag, latestTS, src, h, lg, resp.Response.Metadata.Offset)
	}
	lg.Info(fmt.Sprintf("moving latest admin timestamp to %v", nextTS))
	return nextTS, nil
}

func getDuoAuthLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, h *duoHandlerConfig, lg *log.Logger, offset string) (time.Time, error) {
	var resp authResponse
	vals := url.Values{}
	nextTS := time.Now().Add(-2 * time.Minute)
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.UnixMilli()))
	vals.Set("maxtime", fmt.Sprintf("%v", nextTS.UnixMilli())) // duo api says we can't poll up to two minutes before now...
	vals.Set("limit", "1000")
	if offset != "" {
		lg.Info(fmt.Sprintf("paginating auth log %v", offset))
		vals.Set("next_offset", offset)
	}
	_, data, err := api.SignedCall("GET", duoAuthenticationLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("got %v auth logs", len(resp.Response.Items)))

	var ents []*entry.Entry
	for _, v := range resp.Response.Items {
		var tsItem authItem
		err = json.Unmarshal(v, &tsItem)
		if err != nil {
			return latestTS, err
		}

		ents = append(ents, &entry.Entry{
			TS:   entry.FromStandard(time.Unix(tsItem.TS, 0)),
			SRC:  src,
			Data: []byte(v),
			Tag:  tag,
		})
	}
	lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
	if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("moving latest admin timestamp to %v", latestTS))
	if len(resp.Response.Metadata.Offset) != 0 {
		// paginate
		return getDuoAuthLog(api, tag, latestTS, src, h, lg, strings.Join(resp.Response.Metadata.Offset, ","))
	}
	lg.Info(fmt.Sprintf("moving latest admin timestamp to %v", nextTS))
	return nextTS, nil
}

func getDuoEndpointLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, h *duoHandlerConfig, lg *log.Logger, offset int) (time.Time, error) {
	var resp endpointResponse
	vals := url.Values{}
	vals.Set("limit", "500")
	if offset != 0 {
		lg.Info(fmt.Sprintf("paginating endpoint log %v", offset))
		vals.Set("offset", fmt.Sprintf("%v", offset))
	}
	_, data, err := api.SignedCall("GET", duoEndpointLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("got %v endpoint logs", len(resp.Response)))

	var ents []*entry.Entry
	for _, v := range resp.Response {
		var tsItem endpointItem
		err = json.Unmarshal(v, &tsItem)
		if err != nil {
			return latestTS, err
		}
		latestTS = time.Unix(tsItem.TS, 0)
		ents = append(ents, &entry.Entry{
			TS:   entry.FromStandard(time.Unix(tsItem.TS, 0)),
			SRC:  src,
			Data: []byte(v),
			Tag:  tag,
		})
	}
	lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
	if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
		return latestTS, err
	}

	// If we got a full page of results, continue paginating
	if len(ents) == 500 {
		return getDuoEndpointLog(api, tag, latestTS, src, h, lg, offset+len(ents))
	}

	lg.Info(fmt.Sprintf("moving latest endpoint timestamp to %v", latestTS))
	return latestTS, nil
}

func getDuoTrustLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, h *duoHandlerConfig, lg *log.Logger, offset string) (time.Time, error) {
	var resp trustResponse
	vals := url.Values{}
	nextTS := time.Now()
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.UnixMilli()))
	vals.Set("maxtime", fmt.Sprintf("%v", nextTS.UnixMilli()))
	vals.Set("limit", "200")
	if offset != "" {
		lg.Info(fmt.Sprintf("paginating trust log %v", offset))
		vals.Set("offset", offset)
	}
	_, data, err := api.SignedCall("GET", duoTrustMonitorLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	lg.Info(fmt.Sprintf("got %v trust logs", len(resp.Response.Events)))

	var ents []*entry.Entry
	for _, v := range resp.Response.Events {
		var tsItem trustItem
		err = json.Unmarshal(v, &tsItem)
		if err != nil {
			return latestTS, err
		}

		data, err := json.Marshal(v)
		if err != nil {
			lg.Warn("failed to re-pack entry", log.KV("thinkst", h.name), log.KVErr(err))
			continue
		}

		ents = append(ents, &entry.Entry{
			TS:   entry.FromStandard(time.UnixMilli(tsItem.TS)),
			SRC:  src,
			Data: data,
			Tag:  tag,
		})
	}
	lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
	if len(ents) != 0 {
		if err = h.proc.ProcessBatchContext(ents, h.ctx); err != nil {
			return latestTS, err
		}
	}

	if resp.Response.Metadata.Offset != "" {
		// paginate
		return getDuoTrustLog(api, tag, latestTS, src, h, lg, resp.Response.Metadata.Offset)
	}

	lg.Info(fmt.Sprintf("moving latest trust timestamp to %v", nextTS))
	return nextTS, nil
}
