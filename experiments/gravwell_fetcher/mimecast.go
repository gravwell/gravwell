package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/time/rate"
	"io"
	"io/ioutil"
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

type mimecastSIEMBatchResponse struct {
	Value []struct {
		URL    string    `json:"url"`
		Expiry time.Time `json:"expiry,omitempty"`
		Size   int       `json:"size"`
	} `json:"value"`
	NextPage   string `json:"@nextPage"`
	IsCaughtUp bool   `json:"isCaughtUp"`
}

type mimecastNextPage struct {
	Delivery      string `json:"delivery,omitempty"`
	Receipt       string `json:"receipt,omitempty"`
	Process       string `json:"process,omitempty"`
	AV            string `json:"av,omitempty"`
	Spam          string `json:"spam,omitempty"`
	Internal      string `json:"internal email protect,omitempty"`
	Impersonation string `json:"impersonation protect,omitempty"`
	URL           string `json:"url protect,omitempty"`
	Attachment    string `json:"attachment protect,omitempty"`
	Journal       string `json:"journal,omitempty"`
}

type mimecastMTAInternalData struct {
	ProcessingID          string `json:"processingId"`
	AggregateID           string `json:"aggregateId"`
	Subject               string `json:"subject"`
	MonitoredDomainSource string `json:"monitoredDomainSource"`
	SimilarDomain         string `json:"similarDomain"`
	SenderEnvelope        string `json:"senderEnvelope"`
	MessageID             string `json:"messageId"`
	EventType             string `json:"eventType"`
	ScanResults           string `json:"scanResults"`
	AccountID             string `json:"accountId"`
	Route                 string `json:"route"`
	Recipients            string `json:"recipients"`
	URLCategory           string `json:"urlCategory"`
	Timestamp             int64  `json:"timestamp"`
}

type mimecastMTAProcessData struct {
	ProcessingID         string `json:"processingId,omitempty"`
	AggregateID          string `json:"aggregateId,omitempty"`
	NumberAttachments    string `json:"numberAttachments,omitempty"`
	Attachments          any    `json:"attachments,omitempty"`
	Subject              string `json:"subject,omitempty"`
	SenderEnvelope       string `json:"senderEnvelope,omitempty"`
	MessageID            string `json:"messageId,omitempty"`
	EventType            string `json:"eventType,omitempty"`
	AccountID            string `json:"accountId,omitempty"`
	Action               string `json:"action,omitempty"`
	HoldReason           any    `json:"holdReason,omitempty"`
	SubType              string `json:"subType,omitempty"`
	TotalSizeAttachments string `json:"totalSizeAttachments,omitempty"`
	Timestamp            int64  `json:"timestamp,omitempty"`
	EmailSize            string `json:"emailSize,omitempty"`
}

type mimecastMTAAVData struct {
	ProcessingID         string `json:"processingId"`
	AggregateID          string `json:"aggregateId"`
	FileName             string `json:"fileName"`
	Sha256               string `json:"sha256"`
	Subject              string `json:"subject"`
	SenderEnvelope       string `json:"senderEnvelope"`
	MessageID            string `json:"messageId"`
	SenderDomainInternal string `json:"senderDomainInternal"`
	EventType            string `json:"eventType"`
	Sha1                 string `json:"sha1"`
	AccountID            string `json:"accountId"`
	VirusFound           string `json:"virusFound"`
	Route                string `json:"route"`
	Recipients           string `json:"recipients"`
	FileExtension        string `json:"fileExtension"`
	SenderIP             string `json:"senderIp"`
	SenderDomain         string `json:"senderDomain"`
	Timestamp            int64  `json:"timestamp"`
	EmailSize            string `json:"emailSize"`
	Md5                  string `json:"md5"`
}

type mimecastMTASpamData struct {
	ProcessingID   string `json:"processingId"`
	AggregateID    string `json:"aggregateId"`
	Subject        string `json:"subject"`
	SenderEnvelope string `json:"senderEnvelope"`
	MessageID      string `json:"messageId"`
	SenderHeader   string `json:"senderHeader"`
	EventType      string `json:"eventType"`
	AccountID      string `json:"accountId"`
	Route          string `json:"route"`
	Recipients     string `json:"recipients"`
	SenderIP       string `json:"senderIp"`
	SenderDomain   string `json:"senderDomain"`
	Timestamp      int64  `json:"timestamp"`
}

type mimecastMTAImpersonationData struct {
	ProcessingID                  string `json:"processingId"`
	AggregateID                   string `json:"aggregateId"`
	TaggedMalicious               string `json:"taggedMalicious"`
	Subject                       string `json:"subject"`
	InternalUserName              string `json:"internalUserName"`
	SenderEnvelope                string `json:"senderEnvelope"`
	PolicyDefinition              string `json:"policyDefinition"`
	NewDomain                     string `json:"newDomain"`
	CustomThreatDictionary        string `json:"customThreatDictionary"`
	Action                        string `json:"action"`
	SenderIP                      string `json:"senderIp"`
	Timestamp                     int64  `json:"timestamp"`
	SimilarInternalDomain         string `json:"similarInternalDomain"`
	MessageID                     string `json:"messageId"`
	EventType                     string `json:"eventType"`
	ItemsDetected                 string `json:"itemsDetected"`
	MimecastThreatDictionary      string `json:"mimecastThreatDictionary"`
	AccountID                     string `json:"accountId"`
	CustomNameMatch               string `json:"customNameMatch"`
	Route                         string `json:"route"`
	SimilarMimecastExternalDomain string `json:"similarMimecastExternalDomain"`
	Recipients                    string `json:"recipients"`
	SimilarCustomExternalDomain   string `json:"similarCustomExternalDomain"`
	SubType                       string `json:"subType"`
	TaggedExternal                string `json:"taggedExternal"`
	ReplyMismatch                 string `json:"replyMismatch"`
}

type mimecastMTAURLData struct {
	ProcessingID   string `json:"processingId"`
	AggregateID    string `json:"aggregateId"`
	Subject        any    `json:"subject"`
	SenderEnvelope string `json:"senderEnvelope"`
	MessageID      string `json:"messageId"`
	EventType      string `json:"eventType"`
	Analysis       any    `json:"analysis"`
	URL            string `json:"url"`
	AccountID      string `json:"accountId"`
	Route          string `json:"route"`
	SourceIP       any    `json:"sourceIp"`
	Recipients     string `json:"recipients"`
	Action         string `json:"action"`
	SubType        string `json:"subType"`
	URLCategory    string `json:"urlCategory"`
	BlockReason    any    `json:"blockReason"`
	SenderDomain   string `json:"senderDomain"`
	Timestamp      int64  `json:"timestamp"`
}

type mimecastMTAAttachmentData struct {
	ProcessingID   string `json:"processingId"`
	AggregateID    string `json:"aggregateId"`
	Sha1           string `json:"sha1"`
	AccountID      string `json:"accountId"`
	FileName       string `json:"fileName"`
	SizeAttachment string `json:"sizeAttachment"`
	SenderIP       string `json:"senderIp"`
	SenderDomain   string `json:"senderDomain"`
	Sha256         string `json:"sha256"`
	FileExtension  string `json:"fileExtension"`
	EventType      string `json:"eventType"`
	Timestamp      int64  `json:"timestamp"`
	SenderEnvelope string `json:"senderEnvelope"`
	MessageID      string `json:"messageId"`
	Subject        string `json:"subject"`
	Recipients     string `json:"recipients"`
	CustomerIP     string `json:"customerIp"`
	Route          string `json:"route"`
	FileMime       string `json:"fileMime"`
	Md5            string `json:"md5"`
}

type mimecastMTAJournalData struct {
	ProcessingID   string `json:"processingId"`
	AggregateID    string `json:"aggregateId"`
	AccountID      string `json:"accountId"`
	Recipients     string `json:"recipients"`
	SenderEnvelope string `json:"senderEnvelope"`
	EventType      string `json:"eventType"`
	Timestamp      int64  `json:"timestamp"`
	Direction      string `json:"direction"`
	Subject        string `json:"subject"`
}

type mimecastMTAReceiptData struct {
	ProcessingID         string `json:"processingId"`
	AggregateID          string `json:"aggregateId"`
	SpamProcessingDetail string `json:"spamProcessingDetail"`
	NumberAttachments    string `json:"numberAttachments"`
	Subject              string `json:"subject"`
	TLSVersion           string `json:"tlsVersion"`
	SenderEnvelope       string `json:"senderEnvelope"`
	MessageID            string `json:"messageId"`
	SenderHeader         string `json:"senderHeader"`
	RejectionType        string `json:"rejectionType"`
	EventType            string `json:"eventType"`
	AccountID            string `json:"accountId"`
	Recipients           string `json:"recipients"`
	TLSCipher            string `json:"tlsCipher"`
	Action               string `json:"action"`
	SubType              string `json:"subType"`
	SpamInfo             any    `json:"spamInfo"`
	SenderIP             string `json:"senderIp"`
	Timestamp            int64  `json:"timestamp"`
	Direction            string `json:"direction"`
	SpamScore            string `json:"spamScore"`
	SpamDetectionLevel   string `json:"spamDetectionLevel"`
}

type mimecastMTADeliveryData struct {
	NumberAttachments    string `json:"numberAttachments,omitempty"`
	TLSUsed              string `json:"tlsUsed,omitempty"`
	Subject              string `json:"subject,omitempty"`
	SenderEnvelope       string `json:"senderEnvelope,omitempty"`
	Delivered            string `json:"delivered,omitempty"`
	DestinationIP        string `json:"destinationIp,omitempty"`
	AggregateID          string `json:"aggregateId,omitempty"`
	ProcessingID         string `json:"processingId,omitempty"`
	TLSCipher            string `json:"tlsCipher,omitempty"`
	Timestamp            int64  `json:"timestamp,omitempty"`
	DeliveryTime         string `json:"deliveryTime,omitempty"`
	Direction            string `json:"direction,omitempty"`
	EmailSize            string `json:"emailSize,omitempty"`
	TLSVersion           string `json:"tlsVersion,omitempty"`
	Hostname             string `json:"Hostname,omitempty"`
	MessageID            string `json:"messageId,omitempty"`
	EventType            string `json:"eventType,omitempty"`
	DeliveryAttempts     string `json:"deliveryAttempts,omitempty"`
	AccountID            string `json:"accountId,omitempty"`
	Route                string `json:"route,omitempty"`
	SubType              string `json:"subType,omitempty"`
	TotalSizeAttachments string `json:"totalSizeAttachments,omitempty"`
	DeliveryErrors       string `json:"deliveryErrors,omitempty"`
	RejectionType        string `json:"rejectionType,omitempty"`
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
				Key:        json.RawMessage(`{"delivery": "none", "receipt": "none", "process": "none", "av": "none", "spam": "none", "internal email protect": "none", "impersonation protect": "none", "url protect": "none", "attachment protect": "none", "journal": "none"}`),
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

	//get our API rate limiter built up, start with a full buckets
	rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(h.rate)), h.rate)

	var quit bool
	for quit == false {
		mimecastAuth, err := getMimecastAuthToken(cli, h, lg)
		if err != nil {
			lg.Fatal("failed to get auth token", log.KVErr(err))
		}
		checkPointTime := time.Now()
		latestTS, ok := h.ot.Get("mimecast", h.name)

		var cursor mimecastNextPage
		if err = json.Unmarshal(latestTS.Key, &cursor); err != nil {
			lg.Error("Error getting mta logs", log.KVErr(err))
		}

		if !ok {
			lg.Fatal("failed to get state tracker", log.KV("listener", h.name), log.KV("tag", h.tag))
		}
		// Switch based off the MimecaseAPI set in the config. Call the function with an empty cursor for the first run
		switch h.mimecastAPI {
		case "audit":
			if err = getMimecastAuditLogs(cli, latestTS.LatestTime, checkPointTime, rl, h, lg, mimecastAuth, ""); err != nil {
				lg.Error("Error getting audit logs", log.KVErr(err))
			}
		case "mta-delivery":

			if err = getMimecastMTALogs(cli, checkPointTime, "delivery", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA Delivery logs", log.KVErr(err))
			}
		case "mta-receipt":

			if err = getMimecastMTALogs(cli, checkPointTime, "receipt", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA Reciept logs", log.KVErr(err))
			}
		case "mta-process":

			if err = getMimecastMTALogs(cli, checkPointTime, "process", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA Proccess logs", log.KVErr(err))
			}
		case "mta-av":

			if err = getMimecastMTALogs(cli, checkPointTime, "av", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA AV logs", log.KVErr(err))
			}
		case "mta-spam":

			if err = getMimecastMTALogs(cli, checkPointTime, "spam", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA Spam logs", log.KVErr(err))
			}
		case "mta-internal":

			if err = getMimecastMTALogs(cli, checkPointTime, "internal email protect", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA Internal logs", log.KVErr(err))
			}
		case "mta-impersonation":

			if err = getMimecastMTALogs(cli, checkPointTime, "impersonation protect", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA Impersonation logs", log.KVErr(err))
			}
		case "mta-url":

			if err = getMimecastMTALogs(cli, checkPointTime, "url protect", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA URL logs", log.KVErr(err))
			}
		case "mta-attachment":

			if err = getMimecastMTALogs(cli, checkPointTime, "attachment protect", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA Attachment logs", log.KVErr(err))
			}
		case "mta-journal":

			if err = getMimecastMTALogs(cli, checkPointTime, "journal", rl, h, lg, mimecastAuth, cursor); err != nil {
				lg.Error("Error getting MTA Journal logs", log.KVErr(err))
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
			quit = quitableSleep(h.ctx, mimecastEmptySleepDur)
		}

	}
	lg.Info("Exiting")
}

func getMimecastAuditLogs(cli *http.Client, latestTS time.Time, checkpointTime time.Time, rl *rate.Limiter, h *mimecastHandlerConfig, lg *log.Logger, mimecastAuth string, cursor string) error {

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
			return getMimecastAuditLogs(cli, latestTS, checkpointTime, rl, h, lg, mimecastAuth, fmt.Sprintf("%v", d.Meta.Pagination.Next))
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

func getMimecastMTALogs(cli *http.Client, checkpointTime time.Time, logType string, rl *rate.Limiter, h *mimecastHandlerConfig, lg *log.Logger, mimecastAuth string, cursor mimecastNextPage) error {

	var otState trackedObjectState
	var ok bool
	// Get the current state
	if otState, ok = h.ot.Get("mimecast", h.name); !ok {
		return fmt.Errorf("failed to get state tracker for %s", h.name)
	}

	var nextPageData mimecastNextPage
	if err := json.Unmarshal(otState.Key, &nextPageData); err != nil {
		return fmt.Errorf("failed to unmarshal next page data: %v", err)
	}

	// Get the appropriate cursor based on log type
	var currentCursor string
	switch logType {
	case "delivery":
		currentCursor = nextPageData.Delivery
	case "receipt":
		currentCursor = nextPageData.Receipt
	case "process":
		currentCursor = nextPageData.Process
	case "av":
		currentCursor = nextPageData.AV
	case "spam":
		currentCursor = nextPageData.Spam
	case "internal email protect":
		currentCursor = nextPageData.Internal
	case "impersonation protect":
		currentCursor = nextPageData.Impersonation
	case "url protect":
		currentCursor = nextPageData.URL
	case "attachment protect":
		currentCursor = nextPageData.Attachment
	case "journal":
		currentCursor = nextPageData.Journal
	default:
		return fmt.Errorf("invalid log type: %s", logType)
	}

	// Prepare request parameters and create request
	req, err := http.NewRequest(http.MethodGet, mimecastAuthBaseDomain, nil)
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Set("type", logType)
	params.Set("dateRangeStartsAt", otState.LatestTime.Format(mimecastMTADateTimeFormat))
	params.Set("dateRangeEndsAt", checkpointTime.Format(mimecastMTADateTimeFormat))
	params.Set("pageSize", mimecastMTAPageSize)
	if currentCursor != "none" {
		params.Set("nextPage", currentCursor)
	}
	req.Header.Set(`Authorization`, fmt.Sprintf("Bearer %v", mimecastAuth))
	req.Header.Set(`Accept`, `application/json`)

	req.URL.RawQuery = params.Encode()
	req.URL, err = url.Parse(mimecastAuthBaseDomain + mimecastSIEMAPI + "?" + req.URL.RawQuery)

	var quit bool
	for !quit {
		if err = rl.Wait(h.ctx); err != nil {
			return err
		}
		resp, err := cli.Do(req)
		if err != nil {
			return err
		} else if resp.StatusCode != 200 {
			resp.Body.Close()
			foo, _ := io.ReadAll(resp.Body)
			fmt.Println(string(foo))
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		lg.Info(fmt.Sprintf("got page with length %v", len(data)))

		var batch mimecastSIEMBatchResponse
		if err = json.Unmarshal(data, &batch); err != nil {
			return err
		}

		for _, file := range batch.Value {
			fileReq, err := http.NewRequest(http.MethodGet, file.URL, nil)
			fileResp, err := cli.Do(fileReq)
			if err != nil {
				return err
			} else if fileResp.StatusCode != 200 {
				fileResp.Body.Close()
				foo, _ := io.ReadAll(fileResp.Body)
				fmt.Println(string(foo))
				return fmt.Errorf("invalid status code %d", resp.StatusCode)
			}

			fileData, err := io.ReadAll(fileResp.Body)
			if err != nil {
				return err
			}
			reader := bytes.NewReader(fileData)
			gzreader, err := gzip.NewReader(reader)
			if err != nil {
				return err
			}
			fileData, err = ioutil.ReadAll(gzreader)
			if err != nil {
				return err
			}
			fileResp.Body.Close()

			splitEntries := strings.Split(string(fileData), "\n")

			var T any
			switch logType {
			case "delivery":
				T, ok = T.(mimecastMTADeliveryData)
			case "receipt":
				T, ok = T.(mimecastMTAReceiptData)
			case "process":
				T, ok = T.(mimecastMTAProcessData)
			case "av":
				T, ok = T.(mimecastMTAAVData)
			case "spam":
				T, ok = T.(mimecastMTASpamData)
			case "internal email protect":
				T, ok = T.(mimecastMTAInternalData)
			case "impersonation protect":
				T, ok = T.(mimecastMTAImpersonationData)
			case "url protect":
				T, ok = T.(mimecastMTAURLData)
			case "attachment protect":
				T, ok = T.(mimecastMTAAttachmentData)
			case "journal":
				T, ok = T.(mimecastMTAJournalData)
			}

			var ents []*entry.Entry
			for _, v := range splitEntries {
				if v != "" {
					var entryTime time.Time
					var entryData []byte

					switch logType {
					case "delivery":
						var deliveryEntry mimecastMTADeliveryData
						if err = json.Unmarshal([]byte(v), &deliveryEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(deliveryEntry.Timestamp)
						entryData, err = json.Marshal(deliveryEntry)

					case "receipt":
						var receiptEntry mimecastMTAReceiptData
						if err = json.Unmarshal([]byte(v), &receiptEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(receiptEntry.Timestamp)
						entryData, err = json.Marshal(receiptEntry)

					case "process":
						var processEntry mimecastMTAProcessData
						if err = json.Unmarshal([]byte(v), &processEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(processEntry.Timestamp)
						entryData, err = json.Marshal(processEntry)

					case "av":
						var avEntry mimecastMTAAVData
						if err = json.Unmarshal([]byte(v), &avEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(avEntry.Timestamp)
						entryData, err = json.Marshal(avEntry)

					case "spam":
						var spamEntry mimecastMTASpamData
						if err = json.Unmarshal([]byte(v), &spamEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(spamEntry.Timestamp)
						entryData, err = json.Marshal(spamEntry)
					case "intneral email protect":
						var intneralEntry mimecastMTAInternalData
						if err = json.Unmarshal([]byte(v), &intneralEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(intneralEntry.Timestamp)
						entryData, err = json.Marshal(intneralEntry)
					case "impersonation protect":
						var impersonationEntry mimecastMTAImpersonationData
						if err = json.Unmarshal([]byte(v), &impersonationEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(impersonationEntry.Timestamp)
						entryData, err = json.Marshal(impersonationEntry)
					case "url protect":
						var urlEntry mimecastMTAURLData
						if err = json.Unmarshal([]byte(v), &urlEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(urlEntry.Timestamp)
						entryData, err = json.Marshal(urlEntry)
					case "attachment protect":
						var urlEntry mimecastMTAAttachmentData
						if err = json.Unmarshal([]byte(v), &urlEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(urlEntry.Timestamp)
						entryData, err = json.Marshal(urlEntry)
					case "journal":
						var urlEntry mimecastMTAJournalData
						if err = json.Unmarshal([]byte(v), &urlEntry); err != nil {
							return err
						}
						entryTime = time.UnixMilli(urlEntry.Timestamp)
						entryData, err = json.Marshal(urlEntry)
					default:
						return fmt.Errorf("unknown log type: %s", logType)
					}

					if err != nil {
						lg.Warn("failed to re-pack entry", log.KV("mimecast", h.name), log.KVErr(err))
						continue
					}

					ent := &entry.Entry{
						Tag:  h.tag,
						TS:   entry.FromStandard(entryTime),
						SRC:  h.src,
						Data: entryData,
					}

					ents = append(ents, ent)
				}
			}

			lg.Info(fmt.Sprintf("ingested %v entries in tag %v", len(ents), h.tag))
			if len(ents) != 0 {
				lg.Info(fmt.Sprintf("updated time position: %v", otState.LatestTime))
				for _, v := range ents {
					if err = h.proc.ProcessContext(v, h.ctx); err != nil {
						lg.Error("failed to send entry", log.KVErr(err))
					}
				}

			}
		}
		// Update next page cursor

		switch logType {
		case "delivery":
			nextPageData.Delivery = batch.NextPage
		case "receipt":
			nextPageData.Receipt = batch.NextPage
		case "process":
			nextPageData.Process = batch.NextPage
		case "av":
			nextPageData.AV = batch.NextPage
		case "spam":
			nextPageData.Spam = batch.NextPage
		case "internal email protect":
			nextPageData.Internal = batch.NextPage
		case "impersonation protect":
			nextPageData.Impersonation = batch.NextPage
		case "url protect":
			nextPageData.URL = batch.NextPage
		case "attachment protect":
			nextPageData.Attachment = batch.NextPage
		case "journal":
			nextPageData.Journal = batch.NextPage
		}
		key, err := json.Marshal(nextPageData)

		//This workaround serves the use case where a user is starting from scratch and needs to pull multiple days worth of data. The checkpoint time needs to remain the same for the first run.
		var checkTime time.Time
		if batch.IsCaughtUp == true {
			checkTime = otState.LatestTime
		} else {
			checkTime = checkpointTime
		}

		state := trackedObjectState{
			Updated:    time.Now(),
			LatestTime: checkTime,
			Key:        key,
		}
		err = h.ot.Set("mimecast", h.name, state, false)
		if err != nil {
			lg.Fatal("failed to set state tracker", log.KV("mimecast", h.name), log.KV("tag", h.tag), log.KVErr(err))

		}

		if batch.IsCaughtUp == true {
			lg.Info("next_data is null or no more data in this request, reusing latest offset")
			quit = quitableSleep(h.ctx, mimecastEmptySleepDur)
			quit = true
		} else {
			lg.Info(fmt.Sprintf("got next_data URI: %v", batch.NextPage))
			return getMimecastMTALogs(cli, checkpointTime, logType, rl, h, lg, mimecastAuth, cursor)
		}

	}
	return nil

}
