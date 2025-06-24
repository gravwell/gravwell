package main

import (
	"context"
	"encoding/json"
	"golang.org/x/time/rate"
	"io"

	"fmt"

	"net"
	"net/http"
	"net/url"

	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
)

var (
	mimecastConns map[string]*mimecastHandlerConfig
)

type mimecastAuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

type mimecastAuditDataPayload struct {
	Data []struct {
		EndDateTime   string `json:"endDateTime,omitempty"`
		StartDateTime string `json:"startDateTime,omitempty"`
	} `json:"data,omitempty"`
}
type mimecastSecurityDataPayload struct {
	Data []struct {
		To   string `json:"to,omitempty"`
		From string `json:"from,omitempty"`
	} `json:"data,omitempty"`
}

type mimecastMetaPayload struct {
	Meta struct {
		Pagination struct {
			PageSize  int    `json:"pageSize,omitempty"`
			PageToken string `json:"pageToken,omitempty"`
		} `json:"pagination,omitempty"`
	} `json:"meta,omitempty"`
	Data []struct {
		EndDateTime   string `json:"endDateTime,omitempty"`
		StartDateTime string `json:"startDateTime,omitempty"`
	} `json:"data"`
}
type mimecastResponse struct {
	Meta struct {
		Pagination struct {
			PageSize int    `json:"pageSize,omitempty"`
			Next     string `json:"next,omitempty"`
		} `json:"pagination,omitempty"`
		Status int `json:"status,omitempty"`
	} `json:"meta,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
	Fail []struct {
		Errors []struct {
			Code      string `json:"code,omitempty"`
			Message   string `json:"message,omitempty"`
			Retryable bool   `json:"retryable,omitempty"`
		} `json:"errors,omitempty"`
	} `json:"fail,omitempty"`
}

type mimecastAuditData []struct {
	ID        string `json:"id,omitempty"`
	AuditType string `json:"auditType,omitempty"`
	User      string `json:"user,omitempty"`
	EventTime string `json:"eventTime,omitempty"`
	EventInfo string `json:"eventInfo,omitempty"`
	Category  string `json:"category,omitempty"`
}

type mimecastURLData []struct {
	ClickLogs []struct {
		UserEmailAddress      string   `json:"userEmailAddress"`
		FromUserEmailAddress  string   `json:"fromUserEmailAddress"`
		URL                   string   `json:"url"`
		TtpDefinition         string   `json:"ttpDefinition"`
		Subject               string   `json:"subject"`
		Action                string   `json:"action"`
		AdminOverride         string   `json:"adminOverride"`
		UserOverride          string   `json:"userOverride"`
		ScanResult            string   `json:"scanResult"`
		Category              string   `json:"category"`
		SendingIP             string   `json:"sendingIp"`
		UserAwarenessAction   string   `json:"userAwarenessAction"`
		Date                  string   `json:"date"`
		Actions               string   `json:"actions"`
		Route                 string   `json:"route"`
		CreationMethod        string   `json:"creationMethod"`
		EmailPartsDescription []string `json:"emailPartsDescription"`
		MessageID             string   `json:"messageId"`
	} `json:"clickLogs"`
}

type mimecastImpersonationData []struct {
	ImpersonationLogs []struct {
		ID                   string   `json:"id"`
		SenderAddress        string   `json:"senderAddress"`
		RecipientAddress     string   `json:"recipientAddress"`
		Subject              string   `json:"subject"`
		Definition           string   `json:"definition"`
		Hits                 int      `json:"hits"`
		Identifiers          []string `json:"identifiers"`
		Action               string   `json:"action"`
		TaggedExternal       bool     `json:"taggedExternal"`
		TaggedMalicious      bool     `json:"taggedMalicious"`
		SenderIPAddress      string   `json:"senderIpAddress"`
		EventTime            string   `json:"eventTime"`
		ImpersonationResults []struct {
			ImpersonationDomainSource string `json:"impersonationDomainSource"`
			StringSimilarToDomain     string `json:"stringSimilarToDomain"`
			SimilarDomain             string `json:"similarDomain,omitempty"`
		} `json:"impersonationResults"`
		MessageID string `json:"messageId"`
	} `json:"impersonationLogs"`
	ResultCount int `json:"resultCount"`
}
type mimecastAttachmentData []struct {
	AttachmentLogs []struct {
		SenderAddress    string `json:"senderAddress"`
		RecipientAddress string `json:"recipientAddress"`
		FileName         string `json:"fileName"`
		FileType         string `json:"fileType"`
		Result           string `json:"result"`
		ActionTriggered  string `json:"actionTriggered"`
		Date             string `json:"date"`
		Details          string `json:"details"`
		Route            string `json:"route"`
		MessageID        string `json:"messageId"`
		Subject          string `json:"subject"`
		FileHash         string `json:"fileHash"`
		Definition       string `json:"definition"`
	} `json:"attachmentLogs"`
}

type mimecastHandlerConfig struct {
	clientId     string
	clientSecret string
	startTime    time.Time
	mimecastAPI  string
	tag          entry.EntryTag
	src          net.IP
	name         string
	wg           *sync.WaitGroup
	proc         *processors.ProcessorSet
	ctx          context.Context
	rate         int
	ot           *objectTracker
}

func buildMimecastHandlerConfig(cfg *cfgType, src net.IP, ot *objectTracker, lg *log.Logger, igst *ingest.IngestMuxer, ib base.IngesterBase, ctx context.Context, wg *sync.WaitGroup) *mimecastHandlerConfig {

	mimecastConns = make(map[string]*mimecastHandlerConfig)

	for k, v := range cfg.MimecastConf {
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		// check if there is a statetracker object for each config
		_, ok := ot.Get("mimecast", k)
		if !ok {

			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: time.Now(),
				Key:        json.RawMessage(`{"key": "none"}`),
			}

			if !v.StartTime.IsZero() {
				state.LatestTime = v.StartTime
			}

			err := ot.Set("mimecast", k, state, false)
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
		hcfg := &mimecastHandlerConfig{
			clientId:     v.ClientID,
			clientSecret: v.ClientSecret,
			mimecastAPI:  v.MimecastAPI,
			startTime:    v.StartTime,
			tag:          tag,
			name:         k,
			src:          src,
			wg:           wg,
			ctx:          ctx,
			ot:           ot,
			rate:         defaultRequestPerMinute,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
		}
		mimecastConns[k] = hcfg
	}

	for _, v := range mimecastConns {

		wg.Add(1) // Increment the counter before starting each goroutine
		go v.run()
	}

	return nil
}

func (h *mimecastHandlerConfig) run() {
	defer h.wg.Done()

	//var err error
	cli := &http.Client{
		Timeout: 10 * time.Second,
	}

	checkPointTime := time.Now()

	mimecastAuth, err := getMimecastAuthToken(cli, h, lg)
	if err != nil {
		lg.Fatal("failed to get auth token", log.KVErr(err))
	}

	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(h.rate)), h.rate)

	latestTS, ok := h.ot.Get("mimecast", h.name)
	if !ok {
		lg.Fatal("failed to get state tracker", log.KV("listener", h.name), log.KV("tag", h.tag))
	}
	var quit bool
	for quit == false {
		// Switch based off the MimecaseAPI set in the config. Call the function with an empty cursor for the first run
		switch h.mimecastAPI {
		case "audit":
			if err = getMimcastAuditLogs(cli, latestTS.LatestTime, checkPointTime, rl, h, lg, mimecastAuth, ""); err != nil {
				lg.Error("Error getting audit logs", log.KVErr(err))
			}
		case "url":
			if err = getMimcastURLLogs(cli, latestTS.LatestTime, checkPointTime, rl, h, lg, mimecastAuth, ""); err != nil {
				lg.Error("Error getting url logs", log.KVErr(err))
			}
		case "attachment":
			if err = getMimcastAttachmentLogs(cli, latestTS.LatestTime, checkPointTime, rl, h, lg, mimecastAuth, ""); err != nil {
				lg.Error("Error getting attachement logs", log.KVErr(err))
			}
		case "impersonation":
			if err = getMimcastImpersonationLogs(cli, latestTS.LatestTime, checkPointTime, rl, h, lg, mimecastAuth, ""); err != nil {
				lg.Error("Error getting impersonation logs", log.KVErr(err))
			}
		default:
			lg.Error("Failed to find endpoint ", log.KVErr(err))
		}
		// At the end of the run flush the settings back to the object tracker
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

func getMimcastAuditLogs(cli *http.Client, latestTS time.Time, checkpointTime time.Time, rl *rate.Limiter, h *mimecastHandlerConfig, lg *log.Logger, mimecastAuth string, cursor string) error {

	var payload mimecastAuditDataPayload
	var meta mimecastMetaPayload
	var p []byte
	var err error

	//checkpointTime := time.Now()
	// If this is NOT the first run build the payload off of the meta payload that includes the data and the meta
	if cursor != "" {
		meta = mimecastMetaPayload{
			Data: []struct {
				EndDateTime   string `json:"endDateTime,omitempty"`
				StartDateTime string `json:"startDateTime,omitempty"`
			}{
				{
					EndDateTime:   checkpointTime.Format(mimecastDateTimeFormat),
					StartDateTime: latestTS.Format(mimecastDateTimeFormat),
				},
			},
			Meta: struct {
				Pagination struct {
					PageSize  int    `json:"pageSize,omitempty"`
					PageToken string `json:"pageToken,omitempty"`
				} `json:"pagination,omitempty"`
			}{
				Pagination: struct {
					PageSize  int    `json:"pageSize,omitempty"`
					PageToken string `json:"pageToken,omitempty"`
				}{
					PageSize:  50,
					PageToken: cursor,
				},
			},
		}
		p, err = json.Marshal(meta)
	} else {
		//If this is the initial payload use the audit payload.  The audit payload is different than all the rest.
		payload = mimecastAuditDataPayload{
			Data: []struct {
				EndDateTime   string `json:"endDateTime,omitempty"`
				StartDateTime string `json:"startDateTime,omitempty"`
			}{
				{
					EndDateTime:   checkpointTime.Format(mimecastDateTimeFormat),
					StartDateTime: latestTS.Format(mimecastDateTimeFormat),
				},
			},
		}
		p, err = json.Marshal(payload)
	}
	//The api endpoints are set in the mimecastConfig.go file.
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s%s", mimecastAuthBaseDomain, mimecastAuditAPI), strings.NewReader(string(p)))
	if err != nil {
		return err
	}

	req.Header.Set(`Authorization`, fmt.Sprintf("Bearer %v", mimecastAuth))
	req.Header.Set(`Accept`, `application/json`)

	var quit bool
	for quit == false {
		//if err = rl.Wait(h.ctx); err != nil {
		//	return err
		//}
		resp, err := cli.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			//lg.Info(fmt.Sprintf("got non-200 response code", log.KV("error", string(data))))
			return fmt.Errorf("invalid status code %v", string(data))
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		lg.Info(fmt.Sprintf("got page with length %v", len(data)))

		var d mimecastResponse
		if err = json.Unmarshal(data, &d); err != nil {
			return err
		}
		// A failure can still be a 200 request. The fail payload will explain the error.
		if len(d.Fail) != 0 {
			for _, v := range d.Fail {
				for _, e := range v.Errors {
					lg.Error("failed to get logs", log.KV("code", e.Code), log.KV("message", e.Message), log.KV("retryable", e.Retryable))
				}
			}
			return fmt.Errorf("failed to get logs")
		}

		//lg.Info(fmt.Sprintf("got %v data elements", len(d.Data)))

		var ents []*entry.Entry

		var dataEntries mimecastAuditData
		if err = json.Unmarshal(d.Data, &dataEntries); err != nil {
			return err
		}

		for _, v := range dataEntries {
			var entryTime time.Time
			//Attempt to unmarshall the object

			i, err := time.Parse("2006-01-02T15:04:05-0700", v.EventTime)
			if err != nil {
				entryTime = time.Now()
				lg.Info("could not find ts")
			} else {
				entryTime = i
			}

			data, err := json.Marshal(v)
			if err != nil {
				lg.Warn("failed to re-pack entry", log.KV("mimecast", h.name), log.KVErr(err))
				continue
			}

			ent := &entry.Entry{
				Tag:  h.tag,
				TS:   entry.FromStandard(entryTime),
				SRC:  h.src,
				Data: data,
			}

			ents = append(ents, ent)
		}
		lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
		if len(ents) != 0 {
			lg.Info(fmt.Sprintf("updated time position: %v", latestTS))
			for _, v := range ents {
				if err = h.proc.ProcessContext(v, h.ctx); err != nil {
					lg.Error("failed to send entry", log.KVErr(err))
				}
			}
			//Setting the LastestTime to the request time of the API call
			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: checkpointTime,
				Key:        json.RawMessage(`{"key": "none"}`),
			}
			err := h.ot.Set("mimecast", h.name, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("mimecast", h.name), log.KV("tag", h.tag), log.KVErr(err))

			}
		}

		if len(d.Data) == 2 {
			quit = quitableSleep(h.ctx, mimecastEmptySleepDur)
			quit = true
		} else {
			lg.Info(fmt.Sprintf("got next_data URI: %v", d.Meta.Pagination.Next))
			//Call the api again with the Nextvalue as the cursor
			return getMimcastAuditLogs(cli, latestTS, checkpointTime, rl, h, lg, mimecastAuth, fmt.Sprintf("%v", d.Meta.Pagination.Next))
		}

	}
	return nil
}

func getMimcastURLLogs(cli *http.Client, latestTS time.Time, checkpointTime time.Time, rl *rate.Limiter, h *mimecastHandlerConfig, lg *log.Logger, mimecastAuth string, cursor string) error {

	var payload mimecastSecurityDataPayload
	var meta mimecastMetaPayload
	var p []byte
	var err error

	if cursor != "" {
		meta = mimecastMetaPayload{
			Data: []struct {
				EndDateTime   string `json:"endDateTime,omitempty"`
				StartDateTime string `json:"startDateTime,omitempty"`
			}{
				{
					EndDateTime:   checkpointTime.Format(mimecastDateTimeFormat),
					StartDateTime: latestTS.Format(mimecastDateTimeFormat),
				},
			},
			Meta: struct {
				Pagination struct {
					PageSize  int    `json:"pageSize,omitempty"`
					PageToken string `json:"pageToken,omitempty"`
				} `json:"pagination,omitempty"`
			}{
				Pagination: struct {
					PageSize  int    `json:"pageSize,omitempty"`
					PageToken string `json:"pageToken,omitempty"`
				}{
					PageSize:  100,
					PageToken: cursor,
				},
			},
		}
		p, err = json.Marshal(meta)
	} else {
		payload = mimecastSecurityDataPayload{
			Data: []struct {
				To   string `json:"to,omitempty"`
				From string `json:"from,omitempty"`
			}{
				{
					To:   checkpointTime.Format(mimecastDateTimeFormat),
					From: latestTS.Format(mimecastDateTimeFormat),
				},
			},
		}
		p, err = json.Marshal(payload)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s%s", mimecastAuthBaseDomain, mimecastMonitoringURLAPI), strings.NewReader(string(p)))
	if err != nil {
		return err
	}
	//req.Header.Set(`Content-Type`, `application/x-www-form-urlencoded`)
	req.Header.Set(`Authorization`, fmt.Sprintf("Bearer %v", mimecastAuth))
	req.Header.Set(`Accept`, `application/json`)

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

		var d mimecastResponse
		if err = json.Unmarshal(data, &d); err != nil {
			return err
		}
		if len(d.Fail) != 0 {
			for _, v := range d.Fail {
				for _, e := range v.Errors {
					lg.Error("failed to get logs", log.KV("code", e.Code), log.KV("message", e.Message), log.KV("retryable", e.Retryable))
				}
			}
			return fmt.Errorf("failed to get logs")
		}
		if len(d.Data) > 2 {

			lg.Info(fmt.Sprintf("got %v data elements", len(d.Data)))

			var ents []*entry.Entry

			var dataEntries mimecastURLData
			if err = json.Unmarshal(d.Data, &dataEntries); err != nil {
				return err
			}
			for _, k := range dataEntries {
				for _, v := range k.ClickLogs {
					var entryTime time.Time
					//Attempt to unmarshall the object

					i, err := time.Parse("2006-01-02T15:04:05-0700", v.Date)
					if err != nil {
						entryTime = time.Now()
						lg.Info("could not find ts")
					} else {
						entryTime = i
					}

					data, err := json.Marshal(v)
					if err != nil {
						lg.Warn("failed to re-pack entry", log.KV("mimecast", h.name), log.KVErr(err))
						continue
					}

					ent := &entry.Entry{
						Tag:  h.tag,
						TS:   entry.FromStandard(entryTime),
						SRC:  h.src,
						Data: data,
					}

					ents = append(ents, ent)
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
			}
			state := trackedObjectState{
				Updated:    time.Now(),
				LatestTime: checkpointTime,
				Key:        json.RawMessage(`{"key": "none"}`),
			}
			err := h.ot.Set("mimecast", h.name, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("mimecast", h.name), log.KV("tag", h.tag), log.KVErr(err))

			}
		}

		if len(d.Data) == 2 {
			lg.Info("next_data is null or no more data in this request, reusing latest offset")
			quit = quitableSleep(h.ctx, mimecastEmptySleepDur)
			quit = true
		} else {
			lg.Info(fmt.Sprintf("got next_data URI: %v", d.Meta.Pagination.Next))
			return getMimcastURLLogs(cli, latestTS, checkpointTime, rl, h, lg, mimecastAuth, fmt.Sprintf("%v", d.Meta.Pagination.Next))
		}

	}
	return nil
}

func getMimcastAttachmentLogs(cli *http.Client, latestTS time.Time, checkpointTime time.Time, rl *rate.Limiter, h *mimecastHandlerConfig, lg *log.Logger, mimecastAuth string, cursor string) error {

	var payload mimecastSecurityDataPayload
	var meta mimecastMetaPayload
	var p []byte
	var err error

	if cursor != "" {
		meta = mimecastMetaPayload{
			Data: []struct {
				EndDateTime   string `json:"endDateTime,omitempty"`
				StartDateTime string `json:"startDateTime,omitempty"`
			}{
				{
					EndDateTime:   checkpointTime.Format(mimecastDateTimeFormat),
					StartDateTime: latestTS.Format(mimecastDateTimeFormat),
				},
			},
			Meta: struct {
				Pagination struct {
					PageSize  int    `json:"pageSize,omitempty"`
					PageToken string `json:"pageToken,omitempty"`
				} `json:"pagination,omitempty"`
			}{
				Pagination: struct {
					PageSize  int    `json:"pageSize,omitempty"`
					PageToken string `json:"pageToken,omitempty"`
				}{
					PageSize:  100,
					PageToken: cursor,
				},
			},
		}
		p, err = json.Marshal(meta)
	} else {
		payload = mimecastSecurityDataPayload{
			Data: []struct {
				To   string `json:"to,omitempty"`
				From string `json:"from,omitempty"`
			}{
				{
					To:   checkpointTime.Format(mimecastDateTimeFormat),
					From: latestTS.Format(mimecastDateTimeFormat),
				},
			},
		}
		p, err = json.Marshal(payload)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s%s", mimecastAuthBaseDomain, mimecastMonitoringAttAPI), strings.NewReader(string(p)))
	if err != nil {
		return err
	}

	req.Header.Set(`Authorization`, fmt.Sprintf("Bearer %v", mimecastAuth))
	req.Header.Set(`Accept`, `application/json`)

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

		var d mimecastResponse
		if err = json.Unmarshal(data, &d); err != nil {
			return err
		}
		if len(d.Fail) != 0 {
			for _, v := range d.Fail {
				for _, e := range v.Errors {
					lg.Error("failed to get logs", log.KV("code", e.Code), log.KV("message", e.Message), log.KV("retryable", e.Retryable))
				}
			}
			return fmt.Errorf("failed to get logs")
		}

		lg.Info(fmt.Sprintf("got %v data elements", len(d.Data)))

		var ents []*entry.Entry

		var dataEntries mimecastAttachmentData
		if err = json.Unmarshal(d.Data, &dataEntries); err != nil {
			return err
		}
		for _, k := range dataEntries {
			for _, v := range k.AttachmentLogs {
				var entryTime time.Time
				//Attempt to unmarshall the object

				i, err := time.Parse("2006-01-02T15:04:05-0700", v.Date)
				if err != nil {
					entryTime = time.Now()
					lg.Info("could not find ts")
				} else {
					entryTime = i
				}

				data, err := json.Marshal(v)
				if err != nil {
					lg.Warn("failed to re-pack entry", log.KV("mimecast", h.name), log.KVErr(err))
					continue
				}

				ent := &entry.Entry{
					Tag:  h.tag,
					TS:   entry.FromStandard(entryTime),
					SRC:  h.src,
					Data: data,
				}
				ents = append(ents, ent)
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
				LatestTime: checkpointTime,
				Key:        json.RawMessage(`{"key": "none"}`),
			}
			err := h.ot.Set("mimecast", h.name, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("mimecast", h.name), log.KV("tag", h.tag), log.KVErr(err))

			}
		}

		if len(d.Data) == 2 {
			lg.Info("next_data is null or no more data in this request, reusing latest offset")
			quit = quitableSleep(h.ctx, mimecastEmptySleepDur)
			quit = true
		} else {
			lg.Info(fmt.Sprintf("got next_data URI: %v", d.Meta.Pagination.Next))
			return getMimcastAttachmentLogs(cli, latestTS, checkpointTime, rl, h, lg, mimecastAuth, fmt.Sprintf("%v", d.Meta.Pagination.Next))
		}

	}
	return nil
}

func getMimcastImpersonationLogs(cli *http.Client, latestTS time.Time, checkpointTime time.Time, rl *rate.Limiter, h *mimecastHandlerConfig, lg *log.Logger, mimecastAuth string, cursor string) error {

	var payload mimecastSecurityDataPayload
	var meta mimecastMetaPayload
	var p []byte
	var err error

	if cursor != "" {
		meta = mimecastMetaPayload{
			Data: []struct {
				EndDateTime   string `json:"endDateTime,omitempty"`
				StartDateTime string `json:"startDateTime,omitempty"`
			}{
				{
					EndDateTime:   checkpointTime.Format(mimecastDateTimeFormat),
					StartDateTime: latestTS.Format(mimecastDateTimeFormat),
				},
			},
			Meta: struct {
				Pagination struct {
					PageSize  int    `json:"pageSize,omitempty"`
					PageToken string `json:"pageToken,omitempty"`
				} `json:"pagination,omitempty"`
			}{
				Pagination: struct {
					PageSize  int    `json:"pageSize,omitempty"`
					PageToken string `json:"pageToken,omitempty"`
				}{
					PageSize:  100,
					PageToken: cursor,
				},
			},
		}
		p, err = json.Marshal(meta)
	} else {
		payload = mimecastSecurityDataPayload{
			Data: []struct {
				To   string `json:"to,omitempty"`
				From string `json:"from,omitempty"`
			}{
				{
					To:   checkpointTime.Format(mimecastDateTimeFormat),
					From: latestTS.Format(mimecastDateTimeFormat),
				},
			},
		}
		p, err = json.Marshal(payload)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s%s", mimecastAuthBaseDomain, mimecastMonitoringImpAPI), strings.NewReader(string(p)))
	if err != nil {
		return err
	}

	req.Header.Set(`Authorization`, fmt.Sprintf("Bearer %v", mimecastAuth))
	req.Header.Set(`Accept`, `application/json`)

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

		var d mimecastResponse
		if err = json.Unmarshal(data, &d); err != nil {
			return err
		}
		if len(d.Fail) != 0 {
			for _, v := range d.Fail {
				for _, e := range v.Errors {
					lg.Error("failed to get logs", log.KV("code", e.Code), log.KV("message", e.Message), log.KV("retryable", e.Retryable))
				}
			}
			return fmt.Errorf("failed to get logs")
		}

		lg.Info(fmt.Sprintf("got %v data elements", len(d.Data)))

		var ents []*entry.Entry

		var dataEntries mimecastImpersonationData
		if err = json.Unmarshal(d.Data, &dataEntries); err != nil {
			return err
		}
		for _, k := range dataEntries {
			for _, v := range k.ImpersonationLogs {
				var entryTime time.Time
				//Attempt to unmarshall the object

				i, err := time.Parse("2006-01-02T15:04:05-0700", v.EventTime)
				if err != nil {
					entryTime = time.Now()
					lg.Info("could not find ts")
				} else {
					entryTime = i
				}

				data, err := json.Marshal(v)
				if err != nil {
					lg.Warn("failed to re-pack entry", log.KV("mimecast", h.name), log.KVErr(err))
					continue
				}

				ent := &entry.Entry{
					Tag:  h.tag,
					TS:   entry.FromStandard(entryTime),
					SRC:  h.src,
					Data: data,
				}
				ents = append(ents, ent)
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
				LatestTime: checkpointTime,
				Key:        json.RawMessage(`{"key": "none"}`),
			}
			err := h.ot.Set("mimecast", h.name, state, false)
			if err != nil {
				lg.Fatal("failed to set state tracker", log.KV("mimecast", h.name), log.KV("tag", h.tag), log.KVErr(err))

			}
		}
		if len(d.Data) == 2 {
			lg.Info("next_data is null or no more data in this request, reusing latest offset")
			quit = quitableSleep(h.ctx, mimecastEmptySleepDur)
			quit = true
		} else {
			lg.Info(fmt.Sprintf("got next_data URI: %v", d.Meta.Pagination.Next))
			return getMimcastImpersonationLogs(cli, latestTS, checkpointTime, rl, h, lg, mimecastAuth, fmt.Sprintf("%v", d.Meta.Pagination.Next))
		}

	}
	return nil
}

func getMimecastAuthToken(cli *http.Client, h *mimecastHandlerConfig, lg *log.Logger) (string, error) {
	data := url.Values{}
	data.Set("client_id", h.clientId)
	data.Set("client_secret", h.clientSecret)
	data.Set("grant_type", "client_credentials")
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/oauth/token", mimecastAuthBaseDomain), strings.NewReader(data.Encode()))
	if err != nil {
		return "No Token", err
	}
	req.Header.Set(`Content-Type`, `application/x-www-form-urlencoded`)
	resp, err := cli.Do(req)
	if err != nil {
		return "No Token", err
	} else if resp.StatusCode != 200 {
		resp.Body.Close()
		return "No Token", fmt.Errorf("invalid status code %d", resp.StatusCode)
	}
	res, err := io.ReadAll(resp.Body)
	if err != nil {

		lg.Error("failed to decode body", log.KVErr(err))
	}
	resp.Body.Close()
	var token mimecastAuthToken
	err = json.Unmarshal(res, &token)
	if err != nil {
		lg.Error("failed to unmarshal token", log.KVErr(err))
	}
	return token.AccessToken, nil

}
