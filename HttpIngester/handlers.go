/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"io"
	"net/http"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/timegrinder"
)

type handlerConfig struct {
	ignoreTs bool
	tag      entry.EntryTag
	tg       *timegrinder.TimeGrinder
	method   string
	auth     authHandler
}

type handler struct {
	mp   map[string]handlerConfig
	auth map[string]authHandler
	igst *ingest.IngestMuxer
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	//check if the request is an authentication request
	if ah, ok := h.auth[r.URL.Path]; ok && ah != nil {
		ah.Login(w, r)
		return
	}
	//not an auth, try the actual post URL
	cfg, ok := h.mp[r.URL.Path]
	if !ok {
		lg.Info("bad request URL %v", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != cfg.method {
		lg.Info("bad request Method: %s != %s", r.Method, cfg.method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if cfg.auth != nil {
		if err := cfg.auth.AuthRequest(r); err != nil {
			lg.Info("%s access denied %v: %v", getRemoteIP(r), r.URL.Path, err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	b := make([]byte, maxBody)
	n, err := readAll(r.Body, b)
	if err != nil && err != io.EOF {
		lg.Info("Got bad request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	} else if n == maxBody {
		lg.Error("Request too large, 4MB max")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	b = b[0:n]
	if len(b) == 0 {
		lg.Info("Got an empty post from %s", r.RemoteAddr)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var ts entry.Timestamp
	if cfg.ignoreTs || cfg.tg == nil {
		ts = entry.Now()
	} else {
		var hts time.Time
		var ok bool
		if hts, ok, err = cfg.tg.Extract(b); err != nil {
			lg.Warn("Catastrophic error from timegrinder: %v", err)
			ts = entry.Now()
		} else if !ok {
			ts = entry.Now()
		} else {
			ts = entry.FromStandard(hts)
		}
	}
	e := entry.Entry{
		TS:   ts,
		SRC:  getRemoteIP(r),
		Tag:  cfg.tag,
		Data: b,
	}
	if err = h.igst.WriteEntry(&e); err != nil {
		lg.Error("Failed to send entry: %v", err)
	}
	if v {
		lg.Info("Sending entry %s %s", ts.String(), string(b))
	}
}
