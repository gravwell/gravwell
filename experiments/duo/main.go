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
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	duoapi "github.com/duosecurity/duo_api_golang"
	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	glog "github.com/gravwell/gravwell/v3/ingest/log"
	"golang.org/x/time/rate"
)

const (
	name = `duoingester`
	guid = `00000000-0000-0000-4000-000000000004`

	accountLog        = "/admin/v1/info/summary"
	adminLog          = "/admin/v1/logs/administrator"
	activityLog       = "/admin/v2/logs/activity"
	telephonyLog      = "/admin/v2/logs/telephony"
	authenticationLog = "/admin/v2/logs/authentication"
	endpointLog       = "/admin/v1/endpoints"
	trustMonitorLog   = "/admin/v1/trust_monitor/events"

	sleepDur = 15 * time.Second // length of time we sleep when no results are returned at all
)

var (
	tags          = []string{"duo-account", "duo-admin", "duo-activity", "duo-telephony", "duo-authentication", "duo-endpoint", "duo-trustmonitor"}
	errNoLinks    = errors.New("no links provided")
	errSignalExit = errors.New("program signaled to exit")

	startTime = flag.String("last-log-start-time", "", "timestamp to start ingesting from")
)

func main() {
	var err error
	flag.Parse()
	domain := os.Getenv("duodomain")
	key := os.Getenv("duokey")
	secret := os.Getenv("duosecret")
	if secret == `` || key == `` || domain == `` {
		log.Fatal("missing secret, key, or domain id")
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
	if igst, src, err = getIngestMuxer(cfg, time.Second); err != nil {
		log.Fatal(err)
	}

	//get our context fired up with a quit signal lambda funtion
	ctx, ccf := context.WithCancelCause(context.Background())
	go func(cf context.CancelCauseFunc) {
		sig := utils.GetQuitChannel()
		<-sig
		ccf(errSignalExit)
	}(ccf)

	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute), 1)

	var quit bool
	for quit == false {
		if err = getLogs(key, secret, domain, latestTS, src, igst, rl, ctx); err != nil {
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

func getLogs(key, secret, domain string, latestTS time.Time, src net.IP, igst *ingest.IngestMuxer, rl *rate.Limiter, ctx context.Context) error {
	// get all the tags we need
	tAccount, err := igst.GetTag("duo-account")
	if err != nil {
		return err
	}
	tAdmin, err := igst.GetTag("duo-admin")
	if err != nil {
		return err
	}
	tActivity, err := igst.GetTag("duo-activity")
	if err != nil {
		return err
	}
	tTelephony, err := igst.GetTag("duo-telephony")
	if err != nil {
		return err
	}
	tAuth, err := igst.GetTag("duo-authentication")
	if err != nil {
		return err
	}
	tEndpoint, err := igst.GetTag("duo-endpoint")
	if err != nil {
		return err
	}
	tTrust, err := igst.GetTag("duo-trustmonitor")
	if err != nil {
		return err
	}

	api := duoapi.NewDuoApi(key, secret, domain, "")

	latestAdminTS := latestTS
	latestActivityTS := latestTS
	latestTelephonyTS := latestTS
	latestAuthTS := latestTS
	latestTrustTS := latestTS

	var quit bool
	for quit == false {
		if err := rl.Wait(ctx); err != nil {
			return err
		}

		// account log
		err = getAccountLog(api, tAccount, src, igst, ctx)
		if err != nil {
			return err
		}

		// admin log
		latestAdminTS, err = getAdminLog(api, tAdmin, latestAdminTS, src, igst, ctx)
		if err != nil {
			return err
		}

		// activity log
		latestActivityTS, err = getActivityLog(api, tActivity, latestActivityTS, src, igst, ctx, "")
		if err != nil {
			return err
		}

		// telephony log
		latestTelephonyTS, err = getTelephonyLog(api, tTelephony, latestTelephonyTS, src, igst, ctx, "")
		if err != nil {
			return err
		}

		// auth log
		latestAuthTS, err = getAuthLog(api, tAuth, latestAuthTS, src, igst, ctx, "")
		if err != nil {
			return err
		}

		// endpoint log
		err = getEndpointLog(api, tEndpoint, src, igst, ctx, 0)
		if err != nil {
			return err
		}

		// trust monitor log
		latestTrustTS, err = getTrustLog(api, tTrust, latestTrustTS, src, igst, ctx, "")
		if err != nil {
			return err
		}

		quit = quitableSleep(ctx, sleepDur)
	}

	return nil
}

func getAccountLog(api *duoapi.DuoApi, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, ctx context.Context) error {
	var accountR accountResponse
	_, data, err := api.SignedCall("GET", accountLog, nil)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, &accountR)
	if err != nil {
		return err
	}
	log.Printf("got account log page with length %v", len([]byte(accountR.Response)))
	ent := &entry.Entry{
		TS:   entry.Now(),
		SRC:  src,
		Data: []byte(accountR.Response),
		Tag:  tag,
	}
	err = igst.WriteBatchContext(ctx, []*entry.Entry{ent})
	if err != nil {
		return err
	}

	return nil
}

func getAdminLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, igst *ingest.IngestMuxer, ctx context.Context) (time.Time, error) {
	var adminR adminResponse
	vals := url.Values{}
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.Unix()))
	_, data, err := api.SignedCall("GET", adminLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &adminR)
	if err != nil {
		return latestTS, err
	}
	log.Printf("got %v admin logs", len(adminR.Response))

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
	if len(ents) != 0 {
		err = igst.WriteBatchContext(ctx, ents)
		if err != nil {
			return latestTS, err
		}
	} else {
		latestTS = time.Now()
	}
	log.Printf("moving latest admin timestamp to %v", latestTS)
	return latestTS, nil
}

func getActivityLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, igst *ingest.IngestMuxer, ctx context.Context, offset string) (time.Time, error) {
	var resp activityResponse
	vals := url.Values{}
	nextTS := time.Now().Add(-2 * time.Minute)
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.UnixMilli()))
	vals.Set("maxtime", fmt.Sprintf("%v", nextTS.UnixMilli())) // duo api says we can't poll up to two minutes before now...
	vals.Set("limit", "1000")
	if offset != "" {
		log.Printf("paginating activity log %v", offset)
		vals.Set("next_offset", offset)
	}
	_, data, err := api.SignedCall("GET", activityLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	log.Printf("got %v activity logs", len(resp.Response.Items))

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
	if len(ents) != 0 {
		err = igst.WriteBatchContext(ctx, ents)
		if err != nil {
			return latestTS, err
		}
	}

	if resp.Response.Metadata.Offset != "" {
		// paginate
		return getActivityLog(api, tag, latestTS, src, igst, ctx, resp.Response.Metadata.Offset)
	}

	log.Printf("moving latest activity timestamp to %v", nextTS)
	return nextTS, nil
}

// uses the same json shapes as activity logs
func getTelephonyLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, igst *ingest.IngestMuxer, ctx context.Context, offset string) (time.Time, error) {
	var resp activityResponse
	vals := url.Values{}
	nextTS := time.Now().Add(-2 * time.Minute)
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.UnixMilli()))
	vals.Set("maxtime", fmt.Sprintf("%v", nextTS.UnixMilli())) // duo api says we can't poll up to two minutes before now...
	vals.Set("limit", "1000")
	if offset != "" {
		log.Printf("paginating telephony log %v", offset)
		vals.Set("next_offset", offset)
	}
	_, data, err := api.SignedCall("GET", telephonyLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	log.Printf("got %v telephony logs", len(resp.Response.Items))

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
	if len(ents) != 0 {
		err = igst.WriteBatchContext(ctx, ents)
		if err != nil {
			return latestTS, err
		}
	}

	if resp.Response.Metadata.Offset != "" {
		// paginate
		return getTelephonyLog(api, tag, latestTS, src, igst, ctx, resp.Response.Metadata.Offset)
	}

	log.Printf("moving latest telephony timestamp to %v", nextTS)
	return nextTS, nil
}

func getAuthLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, igst *ingest.IngestMuxer, ctx context.Context, offset string) (time.Time, error) {
	var resp authResponse
	vals := url.Values{}
	nextTS := time.Now().Add(-2 * time.Minute)
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.UnixMilli()))
	vals.Set("maxtime", fmt.Sprintf("%v", nextTS.UnixMilli())) // duo api says we can't poll up to two minutes before now...
	vals.Set("limit", "1000")
	if offset != "" {
		log.Printf("paginating auth log %v", offset)
		vals.Set("next_offset", offset)
	}
	_, data, err := api.SignedCall("GET", authenticationLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	log.Printf("got %v auth logs", len(resp.Response.Items))

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
	if len(ents) != 0 {
		err = igst.WriteBatchContext(ctx, ents)
		if err != nil {
			return latestTS, err
		}
	}

	if len(resp.Response.Metadata.Offset) != 0 {
		// paginate
		return getAuthLog(api, tag, latestTS, src, igst, ctx, strings.Join(resp.Response.Metadata.Offset, ","))
	}

	log.Printf("moving latest auth timestamp to %v", nextTS)
	return nextTS, nil
}

func getEndpointLog(api *duoapi.DuoApi, tag entry.EntryTag, src net.IP, igst *ingest.IngestMuxer, ctx context.Context, offset int) error {
	var resp endpointResponse
	vals := url.Values{}
	vals.Set("limit", "500")
	if offset != 0 {
		log.Printf("paginating endpoint log %v", offset)
		vals.Set("offset", fmt.Sprintf("%v", offset))
	}
	_, data, err := api.SignedCall("GET", endpointLog, vals)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return err
	}
	log.Printf("got %v endpoint logs", len(resp.Response))

	var ents []*entry.Entry
	for _, v := range resp.Response {
		var tsItem endpointItem
		err = json.Unmarshal(v, &tsItem)
		if err != nil {
			return err
		}

		ents = append(ents, &entry.Entry{
			TS:   entry.FromStandard(time.Unix(tsItem.TS, 0)),
			SRC:  src,
			Data: []byte(v),
			Tag:  tag,
		})
	}
	if len(ents) != 0 {
		err = igst.WriteBatchContext(ctx, ents)
		if err != nil {
			return err
		}

		// paginate
		return getEndpointLog(api, tag, src, igst, ctx, len(ents)+offset)
	}

	return nil
}

func getTrustLog(api *duoapi.DuoApi, tag entry.EntryTag, latestTS time.Time, src net.IP, igst *ingest.IngestMuxer, ctx context.Context, offset string) (time.Time, error) {
	var resp trustResponse
	vals := url.Values{}
	nextTS := time.Now()
	vals.Set("mintime", fmt.Sprintf("%v", latestTS.UnixMilli()))
	vals.Set("maxtime", fmt.Sprintf("%v", nextTS.UnixMilli()))
	vals.Set("limit", "200")
	if offset != "" {
		log.Printf("paginating trust log %v", offset)
		vals.Set("offset", offset)
	}
	_, data, err := api.SignedCall("GET", trustMonitorLog, vals)
	if err != nil {
		return latestTS, err
	}
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return latestTS, err
	}
	log.Printf("got %v trust logs", len(resp.Response.Events))

	var ents []*entry.Entry
	for _, v := range resp.Response.Events {
		var tsItem trustItem
		err = json.Unmarshal(v, &tsItem)
		if err != nil {
			return latestTS, err
		}

		ents = append(ents, &entry.Entry{
			TS:   entry.FromStandard(time.UnixMilli(tsItem.TS)),
			SRC:  src,
			Data: []byte(v),
			Tag:  tag,
		})
	}
	if len(ents) != 0 {
		err = igst.WriteBatchContext(ctx, ents)
		if err != nil {
			return latestTS, err
		}
	}

	if resp.Response.Metadata.Offset != "" {
		// paginate
		return getTrustLog(api, tag, latestTS, src, igst, ctx, resp.Response.Metadata.Offset)
	}

	log.Printf("moving latest trust timestamp to %v", nextTS)
	return nextTS, nil
}

func getIngestMuxer(gc base.GeneratorConfig, to time.Duration) (igst *ingest.IngestMuxer, src net.IP, err error) {
	umc := ingest.UniformMuxerConfig{
		Destinations:  gc.ConnSet,
		Tags:          tags,
		Auth:          gc.Auth,
		Tenant:        gc.Tenant,
		IngesterName:  name,
		IngesterUUID:  guid,
		IngesterLabel: `duo`,
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
