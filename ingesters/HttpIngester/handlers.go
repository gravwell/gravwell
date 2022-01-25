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
	"compress/gzip"
	"io"
	"io/ioutil"
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
	hecCompat bool
	kdsCompat bool
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
	debugout("REQUEST %s %v\n", r.Method, r.URL)
	debugout("HEADERS %v\n", r.Header)
	ip := getRemoteIP(r)
	rdr, err := getReadableBody(r)
	if err != nil {
		h.lgr.Error("failed to get body reader", log.KV("address", ip), log.KVErr(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer rdr.Close()

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
		h.lgr.Info("bad request URL", log.KV("url", r.URL.Path))
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != cfg.method {
		h.lgr.Info("bad request method", log.KV("method", r.Method), log.KV("requiredmethod", cfg.method))
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if cfg.auth != nil {
		if err := cfg.auth.AuthRequest(r); err != nil {
			h.lgr.Info("access denied", log.KV("address", getRemoteIP(r)), log.KV("url", r.URL.Path), log.KVErr(err))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	if cfg.hecCompat {
		h.handleHEC(cfg, w, rdr, ip)
	} else if cfg.kdsCompat {
		h.handleKDS(cfg, w, rdr, ip)
	} else if cfg.multiline {
		h.handleMulti(cfg, w, rdr, ip)
	} else {
		h.handleSingle(cfg, w, rdr, ip)
	}
	r.Body.Close()
}

func (h *handler) handleMulti(cfg handlerConfig, w http.ResponseWriter, rdr io.Reader, ip net.IP) {
	debugout("multhandler\n")
	scanner := bufio.NewScanner(rdr)
	for scanner.Scan() {
		if err := h.handleEntry(cfg, scanner.Bytes(), ip); err != nil {
			h.lgr.Error("failed to handle entry", log.KV("address", ip), log.KVErr(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		h.lgr.Warn("failed to handle multiline upload", log.KVErr(err))
		w.WriteHeader(http.StatusBadRequest)
	}
	return
}

func (h *handler) handleSingle(cfg handlerConfig, w http.ResponseWriter, rdr io.Reader, ip net.IP) {
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
	} else if err = h.handleEntry(cfg, b, ip); err != nil {
		h.lgr.Error("failed to handle entry", log.KV("address", ip), log.KVErr(err))
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
			h.lgr.Warn("catastrophic error from timegrinder", log.KVErr(err))
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
		h.lgr.Error("failed to send entry", log.KVErr(err))
		return
	}
	debugout("Sending entry %+v", e)
	return
}

// getReadableBody checks the encoding header and if this request is gzip compressed
// then we transparently wrap it in a gzip reader
func getReadableBody(r *http.Request) (rc io.ReadCloser, err error) {
	switch r.Header.Get("Content-Encoding") {
	case "GZIP": //because AWS...
		fallthrough
	case "gzip":
		rc, err = gzip.NewReader(r.Body)
	default:
		rc = r.Body
	}
	return
}
