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
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	shodanConns map[string]*shodanHandlerConfig
)

// shodanHandlerConfig represents the configuration for a Shodan API handler.
// It contains all necessary settings to interact with the Shodan API and process
// the received data.
type shodanHandlerConfig struct {
	token      string                   // API token for authentication
	domain     string                   // Base domain for API requests
	startTime  time.Time                // Initial start time for data collection
	shodanAPI  string                   // Type of Shodan API to use (host, search, count)
	tag        entry.EntryTag           // Gravwell tag for ingested data
	src        net.IP                   // Source IP for entries
	name       string                   // Unique identifier for this handler
	wg         *sync.WaitGroup          // WaitGroup for goroutine synchronization
	proc       *processors.ProcessorSet // Data processor for entries
	ctx        context.Context          // Context for cancellation
	rate       int                      // Rate limit for API requests per minute
	ot         *objectTracker           // State tracker for data collection
	query      string                   // Search query for Shodan API
	httpClient *http.Client             // HTTP client for API requests
	maxRetries int                      // Maximum number of retry attempts
}

type shodanHostResponse struct {
	RegionCode  string   `json:"region_code"`
	IP          int64    `json:"ip"`
	PostalCode  string   `json:"postal_code"`
	CountryCode string   `json:"country_code"`
	City        string   `json:"city"`
	DmaCode     string   `json:"dma_code"`
	LastUpdate  string   `json:"last_update"`
	Latitude    float64  `json:"latitude"`
	Tags        []string `json:"tags"`
	AreaCode    string   `json:"area_code"`
	CountryName string   `json:"country_name"`
	Hostnames   []string `json:"hostnames"`
	Org         string   `json:"org"`
	Data        []struct {
		Shodan struct {
			ID      string   `json:"id"`
			Options struct{} `json:"options"`
			Ptr     bool     `json:"ptr"`
			Module  string   `json:"module"`
			Crawler string   `json:"crawler"`
		} `json:"_shodan"`
		Hash int64  `json:"hash"`
		OS   string `json:"os"`
		Opts struct {
			Raw string `json:"raw"`
		} `json:"opts"`
		IP        int64    `json:"ip"`
		Isp       string   `json:"isp"`
		Port      int      `json:"port"`
		Hostnames []string `json:"hostnames"`
		Location  struct {
			City         string  `json:"city"`
			RegionCode   string  `json:"region_code"`
			AreaCode     string  `json:"area_code"`
			Longitude    float64 `json:"longitude"`
			CountryCode3 string  `json:"country_code3"`
			CountryName  string  `json:"country_name"`
			PostalCode   string  `json:"postal_code"`
			DmaCode      string  `json:"dma_code"`
			CountryCode  string  `json:"country_code"`
			Latitude     float64 `json:"latitude"`
		} `json:"location"`
		DNS struct {
			ResolverHostname string `json:"resolver_hostname"`
			Recursive        bool   `json:"recursive"`
			ResolverID       string `json:"resolver_id"`
			Software         string `json:"software"`
		} `json:"dns"`
		Timestamp string   `json:"timestamp"`
		Domains   []string `json:"domains"`
		Org       string   `json:"org"`
		Data      string   `json:"data"`
		ASN       string   `json:"asn"`
		Transport string   `json:"transport"`
		IPStr     string   `json:"ip_str"`
	} `json:"data"`
	ASN          string   `json:"asn"`
	Isp          string   `json:"isp"`
	Longitude    float64  `json:"longitude"`
	CountryCode3 string   `json:"country_code3"`
	Domains      []string `json:"domains"`
	IPStr        string   `json:"ip_str"`
	OS           string   `json:"os"`
	Ports        []int    `json:"ports"`
}

type shodanSearchResponse struct {
	Matches []json.RawMessage `json:"matches"`
	Total   int               `json:"total"`
}

type shodanCountResponse struct {
	Total int `json:"total"`
}

func buildShodanHandlerConfig(cfg *cfgType, src net.IP, ot *objectTracker, lg *log.Logger, igst *ingest.IngestMuxer, ib base.IngesterBase, ctx context.Context, wg *sync.WaitGroup) *shodanHandlerConfig {

	shodanConns = make(map[string]*shodanHandlerConfig)

	for k, v := range cfg.ShodanConf {
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		// check if there is a statetracker object for each config
		_, ok := ot.Get("shodan", k)
		if !ok {

			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: time.Now(),
				Key:        v.Query,
			}
			err := ot.Set("shodan", k, state, false)
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
		hcfg := &shodanHandlerConfig{
			token:     v.Token,
			domain:    v.Domain,
			startTime: v.StartTime,
			tag:       tag,
			name:      k,
			shodanAPI: v.ShodanAPI,
			src:       src,
			wg:        wg,
			ctx:       ctx,
			ot:        ot,
			rate:      v.RateLimit,
			query:     v.Query,
			httpClient: &http.Client{
				Timeout: 30 * time.Second,
			},
			maxRetries: 3,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
		}
		shodanConns[k] = hcfg
	}

	for _, v := range shodanConns {
		wg.Add(1) // Increment the counter before starting each goroutine

		go v.run()
	}

	return nil
}

func (h *shodanHandlerConfig) run() {
	defer h.wg.Done()

	var err error
	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(h.rate)), h.rate)

	var quit bool
	for quit == false {
		//get the latest time from the stateTracker map

		switch h.shodanAPI {
		case "host":
			latestTS, ok := h.ot.Get("shodan", h.name)
			if !ok {
				lg.Fatal("failed to get state tracker", log.KV("listener", h.name), log.KV("tag", h.tag))
			}
			if err = getShodanHostData(h, latestTS.Key, h.src, rl, lg, h.ot); err != nil {
				lg.Error("Error getting host data", log.KVErr(err))
			}
		case "search":
			latestTS, ok := h.ot.Get("shodan", h.name)
			if !ok {
				lg.Fatal("failed to get state tracker", log.KV("listener", h.name), log.KV("tag", h.tag))
			}
			if err = getShodanSearchData(h, latestTS.Key, h.src, rl, lg, h.ot); err != nil {
				lg.Error("Error getting search data", log.KVErr(err))
			}
		case "count":
			latestTS, ok := h.ot.Get("shodan", h.name)
			if !ok {
				lg.Fatal("failed to get state tracker", log.KV("listener", h.name), log.KV("tag", h.tag))
			}
			if err = getShodanCountData(h, latestTS.Key, h.src, rl, lg, h.ot); err != nil {
				lg.Error("Error getting count data", log.KVErr(err))
			}
		default:
			lg.Error("Failed to find endpoint", log.KVErr(err))
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

func getShodanHostData(h *shodanHandlerConfig, latestTS string, src net.IP, rl *rate.Limiter, lg *log.Logger, ot *objectTracker) error {
	// For host API, we need an IP address to query
	// This is a simplified example - in a real implementation, you would need to
	// provide a list of IPs to query or a mechanism to discover IPs
	if latestTS == "" {
		lg.Info("No IP address provided for host query")
		return nil
	}

	req, err := http.NewRequest(http.MethodGet, h.domain+shodanHostDomain+latestTS, nil)
	if err != nil {
		return err
	}

	data := url.Values{}
	data.Set("key", h.token)
	req.URL.RawQuery = data.Encode()

	if err = rl.Wait(h.ctx); err != nil {
		return err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status code %d", resp.StatusCode)
	}

	var hostResp shodanHostResponse
	if err = json.NewDecoder(resp.Body).Decode(&hostResp); err != nil {
		return err
	}

	// Convert the response to JSON for ingestion
	jsonData, err := json.Marshal(hostResp)
	if err != nil {
		return err
	}

	// Create an entry with the current time
	ent := &entry.Entry{
		Tag:  h.tag,
		TS:   entry.Now(),
		SRC:  src,
		Data: jsonData,
	}

	// Process the entry
	if err = h.proc.ProcessContext(ent, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
	}

	// Update the state tracker with the current time
	state := trackedObjectState{
		Updated:    time.Now(),
		LatestTime: time.Now(),
		Key:        latestTS,
	}
	err = ot.Set("shodan", h.name, state, false)
	if err != nil {
		lg.Error("failed to set state tracker", log.KVErr(err))
	}

	return nil
}

func getShodanSearchData(h *shodanHandlerConfig, latestTS string, src net.IP, rl *rate.Limiter, lg *log.Logger, ot *objectTracker) error {
	req, err := http.NewRequest(http.MethodGet, h.domain+shodanSearchDomain, nil)
	if err != nil {
		return err
	}

	data := url.Values{}
	data.Set("key", h.token)
	data.Set("query", h.query)

	// If we have a latest timestamp, use it to filter results
	if latestTS != "" {
		data.Set("minify", "true")
	}

	req.URL.RawQuery = data.Encode()

	if err = rl.Wait(h.ctx); err != nil {
		return err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status code %d", resp.StatusCode)
	}

	var searchResp shodanSearchResponse
	if err = json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return err
	}

	lg.Info("Got search results", log.KV("count", len(searchResp.Matches)), log.KV("total", searchResp.Total))

	// Process each match
	for _, match := range searchResp.Matches {
		// Create an entry with the current time
		ent := &entry.Entry{
			Tag:  h.tag,
			TS:   entry.Now(),
			SRC:  src,
			Data: match,
		}

		// Process the entry
		if err = h.proc.ProcessContext(ent, h.ctx); err != nil {
			lg.Error("failed to send entry", log.KVErr(err))
		}

	}
	lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(searchResp.Matches), h.tag))

	// Update the state tracker with the current time
	state := trackedObjectState{
		Updated:    time.Now(),
		LatestTime: time.Now(),
		Key:        latestTS,
	}
	err = ot.Set("shodan", h.name, state, false)
	if err != nil {
		lg.Error("failed to set state tracker", log.KVErr(err))
	}

	return nil
}

func getShodanCountData(h *shodanHandlerConfig, latestTS string, src net.IP, rl *rate.Limiter, lg *log.Logger, ot *objectTracker) error {
	req, err := http.NewRequest(http.MethodGet, h.domain+shodanCountDomain, nil)
	if err != nil {
		return err
	}

	data := url.Values{}
	data.Set("key", h.token)
	data.Set("query", h.query)

	// If we have a latest timestamp, use it to filter results
	if latestTS != "" {
		data.Set("minify", "true")
	}

	req.URL.RawQuery = data.Encode()

	if err = rl.Wait(h.ctx); err != nil {
		return err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status code %d", resp.StatusCode)
	}

	var countResp shodanCountResponse
	if err = json.NewDecoder(resp.Body).Decode(&countResp); err != nil {
		return err
	}

	lg.Info("Got count results", log.KV("total", countResp.Total))

	// Create a simple entry with the count data
	countData := map[string]int{
		"total": countResp.Total,
	}

	jsonData, err := json.Marshal(countData)
	if err != nil {
		return err
	}

	// Create an entry with the current time
	ent := &entry.Entry{
		Tag:  h.tag,
		TS:   entry.Now(),
		SRC:  src,
		Data: jsonData,
	}

	// Process the entry
	if err = h.proc.ProcessContext(ent, h.ctx); err != nil {
		lg.Error("failed to send entry", log.KVErr(err))
	}

	// Update the state tracker with the current time
	state := trackedObjectState{
		Updated:    time.Now(),
		LatestTime: time.Now(),
		Key:        latestTS,
	}
	err = ot.Set("shodan", h.name, state, false)
	if err != nil {
		lg.Error("failed to set state tracker", log.KVErr(err))
	}

	return nil
}
