/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	kdsAuthTokenHeader = `X-Amz-Firehose-Access-Key`
)

// KinesisDeliveryStream
type kds struct {
	URL               string //override the URL, defaults to "/services/collector/event"
	TokenValue        string `json:"-"` //DO NOT SEND THIS when marshalling
	Tag_Name          string //the tag to assign to the request
	Ignore_Timestamps bool
	Preprocessor      []string
}

func (v *kds) validate(name string) (string, error) {
	if len(v.URL) == 0 {
		return ``, errors.New("Missing URL")
	}
	p, err := url.Parse(v.URL)
	if err != nil {
		return ``, fmt.Errorf("URL structure is invalid: %v", err)
	}
	if p.Scheme != `` {
		return ``, errors.New("May not specify scheme in listening URL")
	} else if p.Host != `` {
		return ``, errors.New("May not specify host in listening URL")
	}
	pth := p.Path
	if len(v.Tag_Name) == 0 {
		v.Tag_Name = entry.DefaultTagName
	}
	if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
		return ``, errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + name)
	}
	//normalize the path
	v.URL = pth
	return pth, nil
}

type kinesisRequest struct {
	RequestId string   `json:"requestId"`
	Timestamp int64    `json:"timestamp"`
	Records   []record `json:"records"`
}

func (kr kinesisRequest) TS() time.Time {
	if kr.Timestamp == 0 {
		return time.Now().UTC()
	}
	return time.UnixMilli(kr.Timestamp)
}

type record struct {
	Data []byte `json:"data"`
}

func handleKDS(h *handler, cfg routeHandler, w http.ResponseWriter, rdr io.Reader, ip net.IP) {
	var kr kinesisRequest
	if err := json.NewDecoder(io.LimitReader(rdr, int64(maxBody+256))).Decode(&kr); err != nil {
		h.lgr.Info("bad request", log.KV("address", ip), log.KVErr(err))
		sendKDSError(w, http.StatusBadRequest, ``, nil)
		return
	} else if len(kr.Records) == 0 {
		h.lgr.Info("bad request", log.KV("address", ip), log.KVErr(errors.New("empty records")))
		sendKDSError(w, http.StatusBadRequest, kr.RequestId, errors.New("empty records"))
		return
	}
	reqTS := entry.FromStandard(kr.TS())
	batch := make([]*entry.Entry, 0, len(kr.Records))
	for _, r := range kr.Records {
		e := &entry.Entry{
			TS:   reqTS,
			SRC:  ip,
			Tag:  cfg.tag,
			Data: r.Data,
		}
		if cfg.tg != nil {
			if hts, ok, err := cfg.tg.Extract(r.Data); err == nil && ok {
				e.TS = entry.FromStandard(hts)
			}
		}
		batch = append(batch, e)
	}
	if err := cfg.pproc.ProcessBatch(batch); err != nil {
		h.lgr.Error("failed to send entries", log.KVErr(err))
		sendKDSError(w, http.StatusInternalServerError, kr.RequestId, err)
	} else {
		sendKDSOk(w, kr.RequestId)
	}
}

type kdsresp struct {
	RequestId string `json:"requestId"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"errorMessage,omitempty"`
}

func (k kdsresp) send(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(k)
}

func sendKDSError(w http.ResponseWriter, code int, id string, err error) {
	r := kdsresp{
		RequestId: id,
		Timestamp: time.Now().UTC().UnixMilli(),
	}
	if err != nil {
		r.Message = err.Error()
	}
	r.send(w, code)
}

func sendKDSOk(w http.ResponseWriter, id string) {
	r := kdsresp{
		RequestId: id,
		Timestamp: time.Now().UTC().UnixMilli(),
	}
	r.send(w, http.StatusOK)
}

func includeKDSListeners(hnd *handler, igst *ingest.IngestMuxer, cfg *cfgType, lgr *log.Logger) (err error) {
	for _, v := range cfg.KDSListener {
		hcfg := routeHandler{
			handler: handleKDS,
		}
		if hcfg.tag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Error("failed to pull tag", log.KV("tag", v.Tag_Name), log.KVErr(err))
			return
		}
		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		} else {
			if hcfg.tg, err = timegrinder.New(timegrinder.Config{}); err != nil {
				lg.Error("Failed to create timegrinder", log.KVErr(err))
				return
			}
		}

		if hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Error("preprocessor construction error", log.KVErr(err))
			return
		}
		if hcfg.auth, err = newPresharedHeaderTokenHandler(kdsAuthTokenHeader, v.TokenValue, lgr); err != nil {
			lg.Error("failed to generate Kinesis-Delivery-Stream auth", log.KVErr(err))
			return
		}
		if hnd.addHandler(http.MethodPost, v.URL, hcfg); err != nil {
			return
		}
		debugout("KDS Handler URL %s handling %s\n", v.URL, v.Tag_Name)
	}
	return
}
