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
	"errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

// note that handleFuncs should read from the reader, not from the Request.Body.
type handleFunc func(*handler, routeHandler, http.ResponseWriter, *http.Request, io.Reader, net.IP)

type routeHandler struct {
	ignoreTs bool
	tag      entry.EntryTag
	tg       *timegrinder.TimeGrinder
	handler  handleFunc
	auth     authHandler
	pproc    *processors.ProcessorSet
}

type handler struct {
	sync.RWMutex
	igst           *ingest.IngestMuxer
	lgr            *log.Logger
	mp             map[route]routeHandler
	auth           map[route]authHandler
	custom         map[route]http.Handler
	rawLineBreaker string
	healthCheckURL string
}

func (rh routeHandler) handle(h *handler, w http.ResponseWriter, req *http.Request, rdr io.Reader, ip net.IP) {
	if w == nil {
		return
	} else if rdr == nil || h == nil || rh.handler == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	rh.handler(h, rh, w, req, rdr, ip)
}

func newHandler(igst *ingest.IngestMuxer, lgr *log.Logger) (h *handler, err error) {
	if igst == nil {
		err = errors.New("nil muxer")
	} else if lgr == nil {
		err = errors.New("nil logger")
	} else {
		h = &handler{
			RWMutex: sync.RWMutex{},
			mp:      map[route]routeHandler{},
			auth:    map[route]authHandler{},
			custom:  map[route]http.Handler{},
			igst:    igst,
			lgr:     lgr,
		}
	}
	return
}

func (h *handler) checkConflict(r route) error {
	h.RLock()
	defer h.RUnlock()
	//check heathcheck
	if r.method == http.MethodGet && h.healthCheckURL == r.uri {
		return errors.New("route conflicts with health check URL")
	}
	//check auth
	if _, ok := h.auth[r]; ok {
		return errors.New("route conflicts with authentication URL")
	}
	//check basic handlers
	if _, ok := h.mp[r]; ok {
		return errors.New("route conflicts with ingest URL")
	}
	//check custom handlers
	if _, ok := h.custom[r]; ok {
		return errors.New("route conflicts with custom handler")
	}
	return nil
}

func (h *handler) addHandler(method, pth string, cfg routeHandler) (err error) {
	r := newRoute(method, pth)
	//check if there is a conflict
	if err = h.checkConflict(r); err == nil {
		h.Lock()
		//check heathcheck
		h.mp[r] = cfg
		h.Unlock()
	}
	return
}

func (h *handler) addAuthHandler(method, pth string, ah authHandler) (err error) {
	r := newRoute(method, pth)
	//check if there is a conflict
	if err = h.checkConflict(r); err == nil {
		h.Lock()
		h.auth[r] = ah
		h.Unlock()
	}
	return
}

func (h *handler) addCustomHandler(method, pth string, ah http.Handler) (err error) {
	r := newRoute(method, pth)
	//check if there is a conflict
	if err = h.checkConflict(r); err == nil {
		h.Lock()
		h.custom[r] = ah
		h.Unlock()
	}
	return
}

func (h *handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w := &trackingRW{
		ResponseWriter: rw,
	}
	defer func(trw *trackingRW, req *http.Request) {
		if !v {
			return
		}
		debugout("REQUEST %s %v %d %d\n", req.Method, req.URL, trw.code, trw.bytes)
		debugout("\tHEADERS\n")
		for k, v := range req.Header {
			debugout("\t\t%v: %v\n", k, v)
		}
		debugout("\n")
	}(w, r)
	ip := getRemoteIP(r)
	rdr, err := getReadableBody(r)
	if err != nil {
		h.lgr.Error("failed to get body reader", log.KV("address", ip), log.KVErr(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer rdr.Close()
	rt := route{
		method: r.Method,
		uri:    path.Clean(r.URL.Path),
	}

	//check if its just a health check
	if h.healthCheckURL == rt.uri && rt.method == http.MethodGet {
		//just return, this is an implied 200
		return
	}

	h.RLock()
	//check if the request is an authentication request
	if ah, ok := h.auth[rt]; ok && ah != nil {
		h.RUnlock()
		ah.Login(w, r)
		return
	}

	//check if this is a custom handler request
	if ch, ok := h.custom[rt]; ok {
		h.RUnlock()
		if ch == nil {
			//ummm, ok?
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			ch.ServeHTTP(w, r)
		}
		return
	}

	//not an auth, try the actual post URL
	rh, ok := h.mp[rt]
	h.RUnlock()
	debugout("LOOKUP UP ROUTE: %s %s\n", rt.method, rt.uri)
	if !ok {
		debugout("NO ROUTE\n")
		h.lgr.Info("bad request URL", log.KV("url", rt.uri), log.KV("method", r.Method))
		w.WriteHeader(http.StatusNotFound)
		return
	} else if rh.handler == nil {
		h.lgr.Info("no handler", log.KV("url", rt.uri), log.KV("method", r.Method))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if rh.auth != nil {
		if err := rh.auth.AuthRequest(r); err != nil {
			h.lgr.Info("access denied", log.KV("address", getRemoteIP(r)), log.KV("url", rt.uri), log.KVErr(err))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	rh.handle(h, w, r, rdr, ip)
	r.Body.Close()
}
func (h *handler) handleEntry(cfg routeHandler, b []byte, ip net.IP) (err error) {
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

type trackingRW struct {
	http.ResponseWriter
	code  int
	bytes int
}

func (trw *trackingRW) WriteHeader(code int) {
	trw.code = code
	trw.ResponseWriter.WriteHeader(code)
}

func (trw *trackingRW) Write(b []byte) (r int, err error) {
	r, err = trw.ResponseWriter.Write(b)
	trw.bytes += r
	if trw.code == 0 {
		trw.code = 200
	}
	return
}

type route struct {
	method string
	uri    string
}

func newRoute(method, uri string) route {
	if method == `` {
		method = defaultMethod
	}
	uri = path.Clean(uri)
	return route{
		method: method,
		uri:    uri,
	}
}

func (r route) String() string {
	if r.method == `` {
		return path.Clean(r.uri)
	}
	return r.method + "://" + path.Clean(r.uri)
}

func handleMulti(h *handler, cfg routeHandler, w http.ResponseWriter, r *http.Request, rdr io.Reader, ip net.IP) {
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

func handleSingle(h *handler, cfg routeHandler, w http.ResponseWriter, r *http.Request, rdr io.Reader, ip net.IP) {
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
