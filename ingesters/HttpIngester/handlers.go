/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

type handlerConfig struct {
	ignoreTs  bool
	multiline bool
	tag       entry.EntryTag
	tg        *timegrinder.TimeGrinder
	method    string
	auth      authHandler
	pproc     *processors.ProcessorSet
}

type handler struct {
	lgr            *log.Logger
	mp             map[string]handlerConfig
	auth           map[string]authHandler
	igst           *ingest.IngestMuxer
	healthCheckURL string
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	//check if its just a health check
	if h.healthCheckURL == r.URL.Path {
		//just return, this is an implied 200
		return
	}

	//check if the request is an authentication request
	if ah, ok := h.auth[r.URL.Path]; ok && ah != nil {
		ah.Login(w, r)
		return
	}
	//not an auth, try the actual post URL
	cfg, ok := h.mp[r.URL.Path]
	if !ok {
		h.lgr.Info("bad request URL %v", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != cfg.method {
		h.lgr.Info("bad request Method: %s != %s", r.Method, cfg.method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if cfg.auth != nil {
		if err := cfg.auth.AuthRequest(r); err != nil {
			h.lgr.Info("%s access denied %v: %v", getRemoteIP(r), r.URL.Path, err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	if cfg.multiline {
		h.handleMulti(cfg, r, w)
	} else {
		h.handleSingle(cfg, r, w)
	}
	r.Body.Close()
}

func (h *handler) handleMulti(cfg handlerConfig, r *http.Request, w http.ResponseWriter) {
	debugout("multhandler REQUEST %s %v\n", r.Method, r.URL)
	debugout("multhandler HEADERS %v\n", r.Header)
	ip := getRemoteIP(r)
	scanner := bufio.NewScanner(r.Body)
	for scanner.Scan() {
		if err := h.handleEntry(cfg, scanner.Bytes(), ip); err != nil {
			h.lgr.Error("Failed to handle entry from %s: %v", ip, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		h.lgr.Warn("Failed to handle multiline upload: %v", err)
		w.WriteHeader(http.StatusBadRequest)
	}
	return
}

func (h *handler) handleSingle(cfg handlerConfig, r *http.Request, w http.ResponseWriter) {
	debugout("singlehandler REQUEST %s %v\n", r.Method, r.URL)
	debugout("singlehandler HEADERS %v\n", r.Header)
	b := make([]byte, maxBody)
	n, err := readAll(r.Body, b)
	if err != nil && err != io.EOF {
		h.lgr.Info("Got bad request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	} else if n == maxBody {
		h.lgr.Error("Request too large, 4MB max")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	b = b[0:n]
	if len(b) == 0 {
		h.lgr.Info("Got an empty post from %s", r.RemoteAddr)
		w.WriteHeader(http.StatusBadRequest)
	} else if err = h.handleEntry(cfg, b, getRemoteIP(r)); err != nil {
		h.lgr.Error("Failed to handle entry from %s: %v", r.RemoteAddr, err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (h *handler) handleEntry(cfg handlerConfig, b []byte, ip net.IP) (err error) {
	var ts entry.Timestamp
	if cfg.ignoreTs || cfg.tg == nil {
		ts = entry.Now()
	} else {
		var hts time.Time
		var ok bool
		if hts, ok, err = cfg.tg.Extract(b); err != nil {
			h.lgr.Warn("Catastrophic error from timegrinder: %v", err)
			ts = entry.Now()
		} else if !ok {
			ts = entry.Now()
		} else {
			ts = entry.FromStandard(hts)
		}
	}
	e := entry.Entry{
		TS:   ts,
		SRC:  ip,
		Tag:  cfg.tag,
		Data: b,
	}
	debugout("Handling: %+v\n", e)
	if err = cfg.pproc.Process(&e); err != nil {
		h.lgr.Error("Failed to send entry: %v", err)
		return
	}
	if v {
		h.lgr.Info("Sending entry %s %s", ts.String(), string(b))
	}
	return
}
