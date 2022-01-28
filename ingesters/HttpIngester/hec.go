/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	defaultHECUrl    string = `/services/collector/event`
	defaultHECRawUrl string = `/services/collector/raw`
)

type hecCompatible struct {
	URL               string //override the URL, defaults to "/services/collector/event"
	TokenValue        string `json:"-"` //DO NOT SEND THIS when marshalling
	Tag_Name          string //the tag to assign to the request
	Ignore_Timestamps bool
	Ack               bool
	Preprocessor      []string
}

type hecHandler struct {
	hecHealth
	acking bool
	id     uint64
}

func (v *hecCompatible) validate(name string) (string, error) {
	if len(v.URL) == 0 {
		v.URL = defaultHECUrl
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
		v.Tag_Name = `default`
	}
	if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
		return ``, errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + name)
	}
	//normalize the path
	v.URL = pth
	return pth, nil
}

type hecEvent struct {
	Event json.RawMessage `json:"event"`
	TS    custTime        `json:"time"`
}

type custTime time.Time

func (c *custTime) UnmarshalJSON(v []byte) (err error) {
	var f float64
	v = bytes.Trim(v, `"`) //trim quotes if they are there
	if f, err = strconv.ParseFloat(string(v), 64); err != nil {
		return
	} else if f < 0 || f > float64(0xffffffffff) {
		err = errors.New("invalid timestamp value")
	}
	sec, dec := math.Modf(f)
	*c = custTime(time.Unix(int64(sec), int64(dec*(1e9))))
	return
}

func (hh *hecHandler) handle(h *handler, cfg routeHandler, w http.ResponseWriter, rdr io.Reader, ip net.IP) {
	b, err := ioutil.ReadAll(io.LimitReader(rdr, int64(maxBody+256))) //give some slack for the extra splunk garbage
	if err != nil && err != io.EOF {
		h.lgr.Info("bad request", log.KV("address", ip), log.KVErr(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	} else if len(b) > maxBody {
		h.lgr.Error("request too large", log.KV("requestsize", len(b)), log.KV("maxsize", maxBody))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(b) == 0 {
		h.lgr.Info("got an empty post", log.KV("address", ip))
		w.WriteHeader(http.StatusBadRequest)
	}
	var x hecEvent
	if err = json.Unmarshal(b, &x); err == nil {
		b = []byte(x.Event)
	} //else means we just keep the entire raw thing

	//if we couldn't get the timestmap, use now
	if time.Time(x.TS).IsZero() {
		x.TS = custTime(time.Now().UTC())
	}
	e := entry.Entry{
		TS:   entry.FromStandard(time.Time(x.TS)),
		SRC:  ip,
		Tag:  cfg.tag,
		Data: b,
	}
	if err = cfg.pproc.Process(&e); err != nil {
		h.lgr.Error("failed to send entry", log.KVErr(err))
		return
	}
	debugout("Sending entry %+v", e)
	if hh.acking {
		json.NewEncoder(w).Encode(ack{
			ID: strconv.FormatUint(atomic.AddUint64(&hh.id, 1), 10),
		})
	}
}

func (hh *hecHandler) handleRaw(h *handler, cfg routeHandler, w http.ResponseWriter, rdr io.Reader, ip net.IP) {
	debugout("HEC RAW\n")
	b, err := ioutil.ReadAll(io.LimitReader(rdr, int64(maxBody+1)))
	if err != nil && err != io.EOF {
		h.lgr.Info("got bad request", log.KV("address", ip), log.KVErr(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	} else if len(b) > maxBody {
		h.lgr.Error("request too large, 4MB max")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(b) == 0 {
		h.lgr.Info("got an empty post", log.KV("address", ip))
		w.WriteHeader(http.StatusBadRequest)
		return
	} else if err = h.handleEntry(cfg, b, ip); err != nil {
		h.lgr.Error("failed to handle entry", log.KV("address", ip), log.KVErr(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if hh.acking {
		json.NewEncoder(w).Encode(ack{
			ID: strconv.FormatUint(atomic.AddUint64(&hh.id, 1), 10),
		})
	}
}

func (hh *hecHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var arq ackReq
	if err := json.NewDecoder(r.Body).Decode(&arq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	curr := atomic.LoadUint64(&hh.id)
	resp := ackResp{
		IDs: make(map[string]bool, len(arq.IDs)),
	}
	for _, id := range arq.IDs {
		resp.IDs[strconv.FormatUint(id, 10)] = id <= curr
	}
	json.NewEncoder(w).Encode(resp)
}

func includeHecListeners(hnd *handler, igst *ingest.IngestMuxer, cfg *cfgType, lgr *log.Logger) (err error) {
	for _, v := range cfg.HECListener {
		hh := &hecHandler{
			hecHealth: hecHealth{
				igst:  hnd.igst,
				token: v.TokenValue,
			},
		}
		hcfg := routeHandler{
			handler: hh.handle,
		}
		if hcfg.tag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Error("failed to pull tag", log.KV("tag", v.Tag_Name), log.KVErr(err))
			return
		}
		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		}

		if hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Error("preprocessor construction error", log.KVErr(err))
			return
		}
		if hcfg.auth, err = newPresharedTokenHandler(`Splunk`, v.TokenValue, lgr); err != nil {
			lg.Error("failed to generate HEC-Compatible-Listener auth", log.KVErr(err))
			return
		}
		//had the main handler for events
		if err = hnd.addHandler(http.MethodPost, v.URL, hcfg); err != nil {
			lg.Error("failed to add HEC-Compatible-Listener handler", log.KVErr(err))
			return
		}
		// add the other handlers for health, ack, and raw mode
		bp := path.Dir(v.URL)
		if v.Ack {
			if err = hnd.addCustomHandler(http.MethodPost, path.Join(bp, `ack`), hh); err != nil {
				lg.Error("failed to add HEC-Compatible-Listener ACK handler", log.KVErr(err))
				return
			}
		}
		if err = hnd.addCustomHandler(http.MethodGet, path.Join(bp, `health`), &hh.hecHealth); err != nil {
			lg.Error("failed to add HEC-Compatible-Listener ACK health handler", log.KVErr(err))
			return
		}
		// add in the raw handler
		hcfg.handler = hh.handleRaw
		if err = hnd.addHandler(http.MethodPost, path.Join(bp, `raw`), hcfg); err != nil {
			lg.Error("failed to add HEC-Compatible-Listener handler", log.KVErr(err))
			return
		}

		debugout("HEC Handler URL %s handling %s\n", v.URL, v.Tag_Name)
	}
	return
}

type hecHealth struct {
	token string
	igst  *ingest.IngestMuxer
}

func (hh *hecHealth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if hh == nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else if r.URL.Query().Get(`token`) != hh.token {
		w.WriteHeader(http.StatusBadRequest)
	} else if hh.igst == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else if cnt, err := hh.igst.Hot(); err != nil || cnt == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}

type ack struct {
	ID string `json:"ackID"`
}

type ackReq struct {
	IDs []uint64 `json:"acks"`
}

type ackResp struct {
	IDs map[string]bool `json:"acks"`
}
