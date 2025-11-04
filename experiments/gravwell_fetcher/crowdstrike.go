//go:build crowdstrike_fetcher
/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

	// CrowdStrike SDK (Incidents; Detections use REST to avoid 405s)
	"github.com/crowdstrike/gofalcon/falcon"
	falclient "github.com/crowdstrike/gofalcon/falcon/client"
	incidents "github.com/crowdstrike/gofalcon/falcon/client/incidents"
	"github.com/crowdstrike/gofalcon/falcon/models"
)

var (
	crowdstrikeConns map[string]*crowdstrikeHandlerConfig
)

type crowdstrikeHandlerConfig struct {
	key        string
	secret     string
	domain     string
	apiType    string
	startTime  time.Time
	appID      string
	tag        entry.EntryTag
	src        net.IP
	name       string
	wg         *sync.WaitGroup
	proc       *processors.ProcessorSet
	ctx        context.Context
	rate       int
	ot         *objectTracker
	httpClient *http.Client
	fc         *falclient.CrowdStrikeAPISpecification
}

// markProgressNow bumps Updated/LatestTime so pulls can see forward motion even on empty pages
func (h *crowdstrikeHandlerConfig) markProgressNow() {
	st, ok := h.ot.Get("crowdstrike", h.name)
	if !ok {
		return
	}
	now := time.Now()
	st.Updated = now
	st.LatestTime = now
	if err := h.ot.Set("crowdstrike", h.name, st, false); err != nil {
		lg.Error("failed to update state", log.KVErr(err))
	}
}

/* =========================
   CrowdStrike SDK helpers
   ========================= */

// Map Domain: SDK cloud enum
func cloudFromDomain(domain string) falcon.CloudType {
	d := strings.TrimSpace(strings.ToLower(domain))
	switch {
	case strings.Contains(d, "api.us-2.crowdstrike.com"):
		return falcon.CloudUs2
	case strings.Contains(d, "api.eu-1.crowdstrike.com"):
		return falcon.CloudEu1
	case strings.Contains(d, "api.laggar.gcw.crowdstrike.com"):
		return falcon.CloudUsGov1
	default:
		return falcon.CloudUs1 // api.crowdstrike.com & fallback
	}
}

func newFalconClient(key, secret, domain string) (*falclient.CrowdStrikeAPISpecification, error) {
	cfg := &falcon.ApiConfig{
		ClientId:     key,
		ClientSecret: secret,
		Cloud:        cloudFromDomain(domain),
	}
	return falcon.NewClient(cfg)
}

/* =========================
   Build and start handlers
   ========================= */

func buildCrowdStrikeHandlerConfig(cfg *cfgType, src net.IP, ot *objectTracker, lg *log.Logger, igst *ingest.IngestMuxer, ib base.IngesterBase, ctx context.Context, wg *sync.WaitGroup) *crowdstrikeHandlerConfig {
	crowdstrikeConns = make(map[string]*crowdstrikeHandlerConfig)

	for k, v := range cfg.CrowdStrikeConf {
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		// Ensure state
		if _, ok := ot.Get("crowdstrike", k); !ok {
			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: v.StartTime,
				Key:        json.RawMessage(`{"seen_ids":[]}`),
			}
			if err := ot.Set("crowdstrike", k, state, false); err != nil {
				lg.Fatal("failed to set state tracker", log.KV("listener", k), log.KVErr(err))
			}
			if err := ot.Flush(); err != nil {
				lg.Fatal("failed to flush state tracker", log.KV("listener", k), log.KVErr(err))
			}
		}

		if src == nil {
			src = net.ParseIP("127.0.0.1")
		}

		hcfg := &crowdstrikeHandlerConfig{
			key:       v.Key,
			secret:    v.Secret,
			domain:    v.Domain,
			apiType:   v.APIType,
			startTime: v.StartTime,
			appID:     v.AppID,
			tag:       tag,
			name:      k,
			src:       src,
			wg:        wg,
			ctx:       ctx,
			ot:        ot,
			httpClient: &http.Client{
				Timeout: 60 * time.Second,
			},
		}

		// RateLimit
		if v.RateLimit > 0 {
			hcfg.rate = v.RateLimit
		} else {
			hcfg.rate = defaultRequestPerMinute
		}

		// Preprocessor
		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
		}

		// SDK client (for Incidents)
		if hcfg.fc, err = newFalconClient(v.Key, v.Secret, v.Domain); err != nil {
			lg.Fatal("crowdstrike auth init failed", log.KV("listener", k), log.KVErr(err))
		}

		crowdstrikeConns[k] = hcfg
	}

	for _, v := range crowdstrikeConns {
		wg.Add(1)
		go v.run()
	}
	return nil
}

/* =========================
   Main loop
   ========================= */

func (h *crowdstrikeHandlerConfig) run() {
	defer h.wg.Done()
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(h.rate)), h.rate)

	var quit bool
	for !quit {
		var err error
		switch h.apiType {
		case "stream":
			err = h.runStream(h.ctx, rl) // REST, requires appID
		case "detections":
			err = h.runDetections(h.ctx, rl) // REST (SDK path can 405)
		case "incidents":
			err = h.runIncidents(h.ctx, rl) // SDK
		case "audit":
			err = h.runAudit(h.ctx, rl) // REST + RTR fallback
		case "hosts":
			err = h.runHosts(h.ctx, rl) // REST
		default:
			lg.Error("Invalid CrowdStrike API type", log.KV("type", h.apiType))
		}

		if err != nil {
			lg.Error("crowdstrike handler error", log.KV("listener", h.name), log.KV("type", h.apiType), log.KVErr(err))
		}
		if err := h.ot.Flush(); err != nil {
			lg.Error("failed to flush state tracker", log.KVErr(err))
		}

		select {
		case <-h.ctx.Done():
			quit = true
		default:
			quit = quitableSleep(h.ctx, time.Minute)
		}
	}
	lg.Info("Exiting", log.KV("listener", h.name))
}

/* ==================================
   Helpers: auth, HTTP, batching, MRU
   ================================== */

func (h *crowdstrikeHandlerConfig) restBearerToken(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("client_id", h.key)
	form.Set("client_secret", h.secret)
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.domain, "/")+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("oauth token status %d: %s", resp.StatusCode, string(b))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("empty access_token")
	}
	return tok.AccessToken, nil
}

// Do executes with per-request rate limit and lightweight retry for 429/5xx
func (h *crowdstrikeHandlerConfig) do(ctx context.Context, rl *rate.Limiter, req *http.Request) (*http.Response, error) {
	backoff := 2 * time.Second
	for {
		if err := rl.Wait(ctx); err != nil {
			return nil, err
		}
		resp, err := h.httpClient.Do(req)
		if err != nil {
			// Network error retry with backoff unless context cancelled
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}

		// Handle 429/5xx with Retry-After, if present
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			ra := resp.Header.Get("Retry-After")
			resp.Body.Close()

			// Default backoff
			sleep := backoff

			// Retry-After: either seconds or HTTP-date
			if ra != "" {
				if sec, err := strconv.Atoi(ra); err == nil && sec > 0 {
					sleep = time.Duration(sec) * time.Second
				} else if t, err := http.ParseTime(ra); err == nil {
					if d := time.Until(t); d > 0 {
						sleep = d
					}
				}
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(sleep):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}

		return resp, nil
	}
}

func sanitizeAppID(s string) string {
	if s == "" {
		return "gravwellfetcher"
	}
	buf := make([]rune, 0, 20)
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			buf = append(buf, r)
			if len(buf) >= 20 {
				break
			}
		}
	}
	if len(buf) == 0 {
		return "gravwellfetcher"
	}
	return string(buf)
}

func chunkIDs(ids []string, n int) [][]string {
	if n <= 0 {
		n = 200
	}
	var out [][]string
	for i := 0; i < len(ids); i += n {
		j := i + n
		if j > len(ids) {
			j = len(ids)
		}
		out = append(out, ids[i:j])
	}
	return out
}

/* =========================
   MRU cursor stored in objectTracker.Key
   ========================= */

type csCursor struct {
	SeenIDs []string `json:"seen_ids,omitempty"` // MRU of recently processed IDs
	Offset  int      `json:"offset,omitempty"`   // Reserved (unused)
}

func (h *crowdstrikeHandlerConfig) loadCursor() (csCursor, error) {
	var cur csCursor
	st, ok := h.ot.Get("crowdstrike", h.name)
	if !ok || len(st.Key) == 0 || string(st.Key) == "null" {
		return cur, nil
	}
	if err := json.Unmarshal(st.Key, &cur); err != nil {
		// if unreadable, start fresh
		return csCursor{}, nil
	}
	return cur, nil
}

func (h *crowdstrikeHandlerConfig) saveCursor(cur csCursor) error {
	st, _ := h.ot.Get("crowdstrike", h.name) // ok=false, zero value is fine
	b, _ := json.Marshal(cur)
	st.Updated = time.Now()
	st.Key = b
	return h.ot.Set("crowdstrike", h.name, st, false)
}

func mruMerge(existing, newIDs []string, keep int) []string {
	if keep <= 0 {
		keep = 2000
	}
	seen := make(map[string]struct{}, len(existing)+len(newIDs))
	out := make([]string, 0, min(keep, len(existing)+len(newIDs)))

	// Put new first
	for _, id := range newIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
			if len(out) >= keep {
				return out
			}
		}
	}
	// MRU
	for _, id := range existing {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
			if len(out) >= keep {
				break
			}
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

/* =========================
   Event Stream (APIType=stream)
   ========================= */

func (h *crowdstrikeHandlerConfig) runStream(ctx context.Context, rl *rate.Limiter) error {
	token, err := h.restBearerToken(ctx)
	if err != nil {
		return fmt.Errorf("stream oauth: %w", err)
	}
	app := h.appID
	if strings.TrimSpace(app) == "" {
		app = sanitizeAppID(h.name) // Stable per listener
	}

	listURL := strings.TrimRight(h.domain, "/") + "/sensors/entities/datafeed/v2?appId=" + url.QueryEscape(app)
	lreq, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return err
	}
	lreq.Header.Set("Authorization", "Bearer "+token)
	lreq.Header.Set("Accept", "application/json")

	lg.Info("CrowdStrike stream bootstrap", log.KV("listener", h.name), log.KV("appId", app), log.KV("domain", h.domain))
	lresp, err := h.do(ctx, rl, lreq)
	if err != nil {
		return fmt.Errorf("list streams: %w", err)
	}
	defer lresp.Body.Close()
	if lresp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(lresp.Body)
		return fmt.Errorf("list streams status %d: %s", lresp.StatusCode, string(b))
	}
	var lout struct {
		Resources []struct {
			DataFeedURL string `json:"data_feed_url"`
			Token       string `json:"access_token"`
		} `json:"resources"`
	}
	if err := json.NewDecoder(lresp.Body).Decode(&lout); err != nil {
		return fmt.Errorf("decode list streams: %w", err)
	}
	if len(lout.Resources) == 0 || lout.Resources[0].DataFeedURL == "" || lout.Resources[0].Token == "" {
		return fmt.Errorf("no usable event stream in response")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lout.Resources[0].DataFeedURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+lout.Resources[0].Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "keep-alive")

	// Use a no-timeout client for the long-lived datafeed connection
	streamClient := &http.Client{
		Transport: h.httpClient.Transport, // Inherit proxy/TLS/keepalives
	}
	r, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect stream: %w", err)
	}
	defer r.Body.Close()
	if r.StatusCode/100 != 2 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("stream status %d: %s", r.StatusCode, string(b))
	}

	dec := json.NewDecoder(r.Body)
	for {
		var evt json.RawMessage
		if err := dec.Decode(&evt); err != nil {
			// Normal shutdowns hit EOF or ctx cancel
			if err == io.EOF || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("stream decode: %w", err)
		}
		ent := &entry.Entry{
			TS:   entry.FromStandard(time.Now()),
			SRC:  h.src,
			Tag:  h.tag,
			Data: evt,
		}
		if err := h.proc.ProcessContext(ent, h.ctx); err != nil {
			return fmt.Errorf("process stream entry: %w", err)
		}
	}
}

/* =========================
   Detections (APIType=detections) — REST + MRU + pagination
   ========================= */
func (h *crowdstrikeHandlerConfig) runDetections(ctx context.Context, rl *rate.Limiter) error {
	token, err := h.restBearerToken(ctx)
	if err != nil {
		return fmt.Errorf("detections oauth: %w", err)
	}
	base := strings.TrimRight(h.domain, "/")

	// MRU
	cur, _ := h.loadCursor()
	have := make(map[string]struct{}, len(cur.SeenIDs))
	for _, id := range cur.SeenIDs {
		have[id] = struct{}{}
	}

	const limit = 500
	offset := 0
	var sawAnything bool

	for {
		qURL := fmt.Sprintf("%s/detects/queries/detects/v1?limit=%d&offset=%d", base, limit, offset)
		qReq, err := http.NewRequestWithContext(ctx, http.MethodGet, qURL, nil)
		if err != nil {
			return err
		}
		qReq.Header.Set("Authorization", "Bearer "+token)
		qReq.Header.Set("Accept", "application/json")

		qResp, err := h.do(ctx, rl, qReq)
		if err != nil {
			return err
		}
		var qout struct {
			Resources []string `json:"resources"`
		}
		if qResp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(qResp.Body)
			qResp.Body.Close()
			return fmt.Errorf("detections query status %d: %s", qResp.StatusCode, string(b))
		}
		if err := json.NewDecoder(qResp.Body).Decode(&qout); err != nil {
			qResp.Body.Close()
			return err
		}
		qResp.Body.Close()

		if len(qout.Resources) == 0 {
			break
		}

		// Filter new IDs
		newIDs := make([]string, 0, len(qout.Resources))
		for _, id := range qout.Resources {
			if _, ok := have[id]; !ok {
				newIDs = append(newIDs, id)
				have[id] = struct{}{}
			}
		}

		if len(newIDs) > 0 {
			sawAnything = true
			// Summaries require POST
			body, _ := json.Marshal(map[string][]string{"ids": newIDs})
			sURL := fmt.Sprintf("%s/detects/entities/summaries/GET/v1", base)
			sReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sURL, bytes.NewReader(body))
			if err != nil {
				return err
			}
			sReq.Header.Set("Authorization", "Bearer "+token)
			sReq.Header.Set("Accept", "application/json")
			sReq.Header.Set("Content-Type", "application/json")

			sResp, err := h.do(ctx, rl, sReq)
			if err != nil {
				return err
			}
			if sResp.StatusCode/100 != 2 {
				b, _ := io.ReadAll(sResp.Body)
				sResp.Body.Close()
				return fmt.Errorf("detections summaries status %d: %s", sResp.StatusCode, string(b))
			}
			var sout struct {
				Resources []json.RawMessage `json:"resources"`
			}
			if err := json.NewDecoder(sResp.Body).Decode(&sout); err != nil {
				sResp.Body.Close()
				return err
			}
			sResp.Body.Close()

			for _, r := range sout.Resources {
				ent := &entry.Entry{
					TS:   entry.FromStandard(time.Now()),
					SRC:  h.src,
					Tag:  h.tag,
					Data: r,
				}
				if err := h.proc.ProcessContext(ent, h.ctx); err != nil {
					lg.Error("failed to send entry", log.KVErr(err))
				}
			}

			// Update MRU
			cur.SeenIDs = mruMerge(cur.SeenIDs, newIDs, 2000)
			if err := h.saveCursor(cur); err != nil {
				lg.Error("cursor save failed", log.KVErr(err))
			}
		}

		if len(qout.Resources) < limit {
			break
		}
		offset += limit
	}

	// Mark progress regardless of state
	h.markProgressNow()

	if !sawAnything {
		time.Sleep(crowdstrikeEmptySleepDur)
	}
	return nil
}

/* =========================
   Incidents (APIType=incidents) — SDK + MRU
   ========================= */

func (h *crowdstrikeHandlerConfig) runIncidents(ctx context.Context, rl *rate.Limiter) error {
	// Incident IDs
	var strIDs []string
	var off int64 = 0
	var limit int64 = 500

	for {
		if err := rl.Wait(ctx); err != nil {
			return err
		}
		q := incidents.NewQueryIncidentsParams().
			WithContext(ctx).
			WithOffset(&off).
			WithLimit(&limit)

		res, err := h.fc.Incidents.QueryIncidents(q)
		if err != nil {
			return fmt.Errorf("query incidents: %w", err)
		}
		if res == nil || res.Payload == nil || len(res.Payload.Resources) == 0 {
			break
		}
		for _, id := range res.Payload.Resources {
			strIDs = append(strIDs, fmt.Sprint(id))
		}
		if int64(len(res.Payload.Resources)) < limit {
			break
		}
		off += limit
	}

	if len(strIDs) == 0 {
		h.markProgressNow()
		time.Sleep(crowdstrikeEmptySleepDur)
		return nil
	}

	// MRU
	cur, _ := h.loadCursor()
	have := make(map[string]struct{}, len(cur.SeenIDs))
	for _, id := range cur.SeenIDs {
		have[id] = struct{}{}
	}
	newIDs := make([]string, 0, len(strIDs))
	for _, id := range strIDs {
		if _, ok := have[id]; !ok {
			newIDs = append(newIDs, id)
		}
	}
	if len(newIDs) == 0 {
		time.Sleep(crowdstrikeEmptySleepDur)
		h.markProgressNow()
		return nil
	}

	// GetIncidents fetch + ingest
	for _, ids := range chunkIDs(newIDs, 200) {
		if err := rl.Wait(ctx); err != nil {
			return err
		}
		gi := incidents.NewGetIncidentsParams().
			WithContext(ctx).
			WithBody(&models.MsaIdsRequest{Ids: ids})

		out, err := h.fc.Incidents.GetIncidents(gi)
		if err != nil {
			return fmt.Errorf("get incidents: %w", err)
		}
		if out == nil || out.Payload == nil {
			continue
		}

		for _, v := range out.Payload.Resources {
			b, _ := json.Marshal(v)
			ent := &entry.Entry{
				TS:   entry.FromStandard(time.Now()),
				SRC:  h.src,
				Tag:  h.tag,
				Data: b,
			}
			if err := h.proc.ProcessContext(ent, h.ctx); err != nil {
				lg.Error("failed to send incident entry", log.KV("listener", h.name), log.KVErr(err))
				continue
			}
		}
	}

	// MRU
	cur.SeenIDs = mruMerge(cur.SeenIDs, newIDs, 2000)
	if err := h.saveCursor(cur); err != nil {
		lg.Error("cursor save failed", log.KV("listener", h.name), log.KVErr(err))
	}
	// Mark progress regardless of state
	h.markProgressNow()
	return nil
}

/* =========================
   Hosts / Devices (APIType=hosts) — REST
   ========================= */

func (h *crowdstrikeHandlerConfig) runHosts(ctx context.Context, rl *rate.Limiter) error {
	token, err := h.restBearerToken(ctx)
	if err != nil {
		return fmt.Errorf("hosts oauth: %w", err)
	}
	base := strings.TrimRight(h.domain, "/")

	offset := 0
	limit := 500
	var allIDs []string

	for {
		qURL := fmt.Sprintf("%s/devices/queries/devices/v1?limit=%d&offset=%d", base, limit, offset)
		qReq, err := http.NewRequestWithContext(ctx, http.MethodGet, qURL, nil)
		if err != nil {
			return err
		}
		qReq.Header.Set("Authorization", "Bearer "+token)
		qReq.Header.Set("Accept", "application/json")

		qResp, err := h.do(ctx, rl, qReq)
		if err != nil {
			return err
		}
		var qout struct {
			Resources []string `json:"resources"`
		}
		if qResp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(qResp.Body)
			qResp.Body.Close()
			return fmt.Errorf("hosts query status %d: %s", qResp.StatusCode, string(b))
		}
		if err := json.NewDecoder(qResp.Body).Decode(&qout); err != nil {
			qResp.Body.Close()
			return err
		}
		qResp.Body.Close()

		if len(qout.Resources) == 0 {
			break
		}
		allIDs = append(allIDs, qout.Resources...)

		if len(qout.Resources) < limit {
			break
		}
		offset += limit
	}

	if len(allIDs) == 0 {
		time.Sleep(crowdstrikeEmptySleepDur)
		h.markProgressNow()
		return nil
	}

	// Chunk ids
	for _, ids := range chunkIDs(allIDs, 200) {
		vals := url.Values{}
		for _, id := range ids {
			vals.Add("ids", id)
		}
		dURL := fmt.Sprintf("%s/devices/entities/devices/v2?%s", base, vals.Encode())
		dReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dURL, nil)
		if err != nil {
			return err
		}
		dReq.Header.Set("Authorization", "Bearer "+token)
		dReq.Header.Set("Accept", "application/json")

		dResp, err := h.do(ctx, rl, dReq)
		if err != nil {
			return err
		}
		if dResp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(dResp.Body)
			dResp.Body.Close()
			return fmt.Errorf("hosts entities status %d: %s", dResp.StatusCode, string(b))
		}
		var entities struct {
			Resources []json.RawMessage `json:"resources"`
		}
		if err := json.NewDecoder(dResp.Body).Decode(&entities); err != nil {
			dResp.Body.Close()
			return err
		}
		dResp.Body.Close()

		for _, r := range entities.Resources {
			ent := &entry.Entry{
				TS:   entry.FromStandard(time.Now()),
				SRC:  h.src,
				Tag:  h.tag,
				Data: r,
			}
			if err := h.proc.ProcessContext(ent, h.ctx); err != nil {
				lg.Error("failed to send entry", log.KVErr(err))
			}
		}
	}

	// Mark progress regardless of state
	h.markProgressNow()
	return nil

}

/* =========================
   Audit Events (APIType=audit) — REST + RTR fallback + MRU + Pagination
   ========================= */

func (h *crowdstrikeHandlerConfig) runAudit(ctx context.Context, rl *rate.Limiter) error {
	token, err := h.restBearerToken(ctx)
	if err != nil {
		return fmt.Errorf("audit oauth: %w", err)
	}
	base := strings.TrimRight(h.domain, "/")

	// MRU
	cur, _ := h.loadCursor()
	have := make(map[string]struct{}, len(cur.SeenIDs))
	for _, id := range cur.SeenIDs {
		have[id] = struct{}{}
	}

	const limit = 500
	offset := 0
	sawAnything := false

	for {
		qURL := fmt.Sprintf("%s/audit/queries/audit-events/v1?limit=%d&offset=%d", base, limit, offset)
		qReq, err := http.NewRequestWithContext(ctx, http.MethodGet, qURL, nil)
		if err != nil {
			return err
		}
		qReq.Header.Set("Authorization", "Bearer "+token)
		qReq.Header.Set("Accept", "application/json")

		qResp, err := h.do(ctx, rl, qReq)
		if err != nil {
			return err
		}
		// Try RTR audit if Audit 404/405
		if qResp.StatusCode == http.StatusNotFound || qResp.StatusCode == http.StatusMethodNotAllowed {
			qResp.Body.Close()
			return h.runRTRAudit(ctx, rl, token)
		}
		if qResp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(qResp.Body)
			qResp.Body.Close()
			return fmt.Errorf("audit query status %d: %s", qResp.StatusCode, string(b))
		}

		var qout struct {
			Resources []string `json:"resources"`
		}
		if err := json.NewDecoder(qResp.Body).Decode(&qout); err != nil {
			qResp.Body.Close()
			return err
		}
		qResp.Body.Close()

		if len(qout.Resources) == 0 {
			if offset == 0 {
				time.Sleep(crowdstrikeEmptySleepDur)
			}
			break
		}

		// Filter new
		idsNew := make([]string, 0, len(qout.Resources))
		for _, id := range qout.Resources {
			if _, ok := have[id]; !ok {
				idsNew = append(idsNew, id)
				have[id] = struct{}{}
			}
		}

		if len(idsNew) > 0 {
			sawAnything = true

			// Fetch IDs
			values := url.Values{}
			for _, id := range idsNew {
				values.Add("ids", id)
			}
			eURL := fmt.Sprintf("%s/audit/entities/audit-events/v1?%s", base, values.Encode())
			eReq, err := http.NewRequestWithContext(ctx, http.MethodGet, eURL, nil)
			if err != nil {
				return err
			}
			eReq.Header.Set("Authorization", "Bearer "+token)
			eReq.Header.Set("Accept", "application/json")

			eResp, err := h.do(ctx, rl, eReq)
			if err != nil {
				return err
			}
			if eResp.StatusCode/100 != 2 {
				b, _ := io.ReadAll(eResp.Body)
				eResp.Body.Close()
				return fmt.Errorf("audit entities status %d: %s", eResp.StatusCode, string(b))
			}
			var entities struct {
				Resources []json.RawMessage `json:"resources"`
			}
			if err := json.NewDecoder(eResp.Body).Decode(&entities); err != nil {
				eResp.Body.Close()
				return err
			}
			eResp.Body.Close()

			for _, r := range entities.Resources {
				ent := &entry.Entry{
					TS:   entry.FromStandard(time.Now()),
					SRC:  h.src,
					Tag:  h.tag,
					Data: r,
				}
				if err := h.proc.ProcessContext(ent, h.ctx); err != nil {
					lg.Error("failed to send entry", log.KVErr(err))
				}
			}

			// Update MRU
			cur.SeenIDs = mruMerge(cur.SeenIDs, idsNew, 2000)
			if err := h.saveCursor(cur); err != nil {
				lg.Error("cursor save failed", log.KVErr(err))
			}
		}

		if len(qout.Resources) < limit {
			break
		}
		offset += limit
	}

	// Mark progress regardless of state
	h.markProgressNow()
	if !sawAnything {
		time.Sleep(crowdstrikeEmptySleepDur)
	}
	return nil
}

// RTR audit fallback
func (h *crowdstrikeHandlerConfig) runRTRAudit(ctx context.Context, rl *rate.Limiter, token string) error {
	base := strings.TrimRight(h.domain, "/")
	offset := 0
	limit := 500

	for {
		rURL := fmt.Sprintf("%s/real-time-response/entities/audit/v1?limit=%d&offset=%d", base, limit, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := h.do(ctx, rl, req)
		if err != nil {
			return fmt.Errorf("rtr audit: %w", err)
		}
		if resp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("rtr audit status %d: %s", resp.StatusCode, string(b))
		}
		var entities struct {
			Resources []json.RawMessage `json:"resources"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&entities); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		if len(entities.Resources) == 0 {
			if offset == 0 {
				time.Sleep(crowdstrikeEmptySleepDur)
			}
			h.markProgressNow()
			return nil
		}
		for _, r := range entities.Resources {
			ent := &entry.Entry{
				TS:   entry.FromStandard(time.Now()),
				SRC:  h.src,
				Tag:  h.tag,
				Data: r,
			}
			if err := h.proc.ProcessContext(ent, h.ctx); err != nil {
				lg.Error("failed to send entry", log.KVErr(err))
			}
		}

		if len(entities.Resources) < limit {
			h.markProgressNow()
			return nil
		}
		offset += limit
	}
}