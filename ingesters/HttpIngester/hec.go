/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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
	"strconv"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

const (
	defaultMaxHECEventSize uint = 512 * 1024 //splunk defaults to 10k characters, but thats weak
)

var (
	respSuccess = ack{Text: "Success"}
)

type hecHandler struct {
	hecHealth
	name           string
	ackLock        sync.Mutex
	ackIds         map[string]uint64
	tagRouter      map[string]entry.EntryTag
	rawLineBreaker string
	maxSize        uint
}

type hecEvent struct {
	Event      piaObj                 `json:"event"`
	Fields     map[string]interface{} `json:"fields,omitempty"`
	TS         custTime               `json:"time"`
	Host       string                 `json:"host,omitempty"`
	Source     string                 `json:"source,omitempty"`
	Sourcetype string                 `json:"sourcetype,omitempty"`
	Index      string                 `json:"index,omitempty"`
}

type custTime time.Time

func (c *custTime) UnmarshalJSON(v []byte) (err error) {
	var f float64
	raw := bytes.Trim(v, `" `)
	//trim quotes if they are there
	if len(raw) == 0 {
		return //missing, so just bail
	}
	//attempt to parse as a float for the default type
	if f, err = strconv.ParseFloat(string(raw), 64); err == nil {
		//got a good parse on a float, sanity check it
		if f < 0 || f > float64(0xffffffffff) {
			err = errors.New("invalid timestamp value")
			return
		}
		//all good, create our timestamp
		sec, dec := math.Modf(f)
		*c = custTime(time.Unix(int64(sec), int64(dec*(1e9))))
		return
	}

	//couldn't parse as a float, try the standard JSON timestamp format
	var ts time.Time
	if err = ts.UnmarshalJSON(v); err == nil {
		*c = custTime(ts)
	}
	return
}

func (hh *hecHandler) handle(h *handler, cfg routeHandler, w http.ResponseWriter, r *http.Request, rdr io.Reader, ip net.IP) {
	resp := respSuccess
	// get a local logger up that will always add some more info
	ll := log.NewLoggerWithKV(h.lgr, log.KV("HEC-Listener", hh.name), log.KV("remoteaddress", ip.String()))
	dec, err := utils.NewJsonLimitedDecoder(rdr, int64(maxBody+256)) //give some slack for the extra splunk garbage
	if err != nil {
		ll.Error("failed to create limited decoder", log.KVErr(err))
		hh.respInternalServerError(w)
		return
	}
	var counter int
loop:
	for ; ; counter++ {
		var ts entry.Timestamp
		var hev hecEvent
		tag := cfg.tag

		//try to decode the damn thing
		if err = dec.Decode(&hev); err != nil {
			// check if limited reader is exhausted so that we can throw a better error
			if errors.Is(err, utils.ErrOversizedObject) {
				ll.Error("oversized json object", log.KV("max-size", hh.maxSize))
			} else if errors.Is(err, io.EOF) {
				//no error
				break loop
			} else {
				//just a plain old error
				ll.Error("invalid json object", log.KV("max-size", hh.maxSize), log.KVErr(err))
			}
			hh.respInvalidDataFormat(w, counter)
			return // we pretty much have to just hang up
		}
		//handle timestamps
		if cfg.ignoreTs {
			ts = entry.Now()
		} else {
			//try to deal with missing timestamps and other garbage
			if time.Time(hev.TS).IsZero() {
				//attempt to derive out of the payload if there is one
				if extracted, ok, err := cfg.tg.Extract(hev.Event.Bytes()); err != nil || !ok {
					ts = entry.Now()
				} else {
					ts = entry.FromStandard(extracted)
				}
			} else {
				ts = entry.FromStandard(time.Time(hev.TS))
			}
		}

		if len(hev.Sourcetype) > 0 && len(hh.tagRouter) > 0 {
			if lt, ok := hh.tagRouter[hev.Sourcetype]; ok {
				tag = lt
			}
		}

		e := entry.Entry{
			TS:   ts,
			SRC:  ip,
			Tag:  tag,
			Data: hev.Event.Bytes(),
		}

		if hev.Host != `` {
			e.AddEnumeratedValueEx(`host`, hev.Host)
		}
		if hev.Source != `` {
			e.AddEnumeratedValueEx(`source`, hev.Source)
		}
		if hev.Fields != nil {
			//add Evs
			for k, v := range hev.Fields {
				if ed, err := entry.InferEnumeratedData(v); err == nil {
					e.AddEnumeratedValue(entry.EnumeratedValue{Name: k, Value: ed})
				} else if err == entry.ErrUnknownType {
					//try again making it a string
					if ed, err = entry.InferEnumeratedData(fmt.Sprintf("%v", v)); err == nil {
						e.AddEnumeratedValue(entry.EnumeratedValue{Name: k, Value: ed})
					}
				}
			}
		}
		debugout("Sending entry %+v", e)
		if err = cfg.pproc.Process(&e); err != nil {
			ll.Error("failed to send entry", log.KVErr(err))
			hh.respInternalServerError(w)
			return
		}
	}

	if counter == 0 {
		// no entries? Send a 400 / "No data"
		hh.respNoData(w)
		return
	}
	if doAck, ch := ackRequested(r); doAck {
		hh.setAck(ch, resp)
	}

	hh.writeResponse(w, resp)
}

func (hh *hecHandler) setAck(channel string, resp ack) {
	hh.ackLock.Lock()
	if _, ok := hh.ackIds[channel]; !ok {
		hh.ackIds[channel] = 0
	}
	val := hh.ackIds[channel] + 1
	hh.ackIds[channel] = val
	hh.ackLock.Unlock()
}

func (hh *hecHandler) writeResponse(w http.ResponseWriter, resp ack) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (hh *hecHandler) respNoData(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(ack{Code: 5, Text: "No data"})
}

func (hh *hecHandler) respInternalServerError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(ack{Code: 8, Text: "Internal server error"})
}

func (hh *hecHandler) respInvalidDataFormat(w http.ResponseWriter, index int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(ack{Code: 6, Text: "Invalid data format", InvalidEventNumber: index})
}

func (hh *hecHandler) handleRaw(h *handler, cfg routeHandler, w http.ResponseWriter, r *http.Request, rdr io.Reader, ip net.IP) {
	debugout("HEC RAW\n")
	resp := ack{Text: "Success"}
	b, err := ioutil.ReadAll(io.LimitReader(rdr, int64(maxBody+1)))
	if err != nil && err != io.EOF {
		h.lgr.Info("got bad request", log.KV("address", ip), log.KVErr(err))
		hh.respInvalidDataFormat(w, 0)
		return
	} else if len(b) > maxBody {
		h.lgr.Error("request too large, 4MB max")
		hh.respInvalidDataFormat(w, 0)
		return
	}
	if len(b) == 0 {
		h.lgr.Info("got an empty post", log.KV("address", ip))
		hh.respNoData(w)
		return
	} else {
		for i, b := range bytes.Split(b, []byte(hh.rawLineBreaker)) {
			if err = h.handleEntry(cfg, b, ip); err != nil {
				h.lgr.Error("failed to handle entry", log.KV("address", ip), log.KVErr(err))
				hh.respInvalidDataFormat(w, i)
				return
			}
		}
	}
	if doAck, ch := ackRequested(r); doAck {
		hh.setAck(ch, resp)
	}
	hh.writeResponse(w, resp)
}

func (hh *hecHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var arq ackReq
	if err := json.NewDecoder(r.Body).Decode(&arq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Figure out which channel
	_, ch := ackRequested(r)
	hh.ackLock.Lock()
	curr := hh.ackIds[ch]
	hh.ackLock.Unlock()
	resp := ackResp{
		IDs: make(map[string]bool, len(arq.IDs)),
	}
	for _, id := range arq.IDs {
		resp.IDs[strconv.FormatUint(id, 10)] = id <= curr
	}
	json.NewEncoder(w).Encode(resp)
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
	Text               string `json:"text"`
	Code               int    `json:"code"`
	InvalidEventNumber int    `json:"invalid-event-number"`
	ID                 string `json:"ackID"`
}

type ackReq struct {
	IDs []uint64 `json:"acks"`
}

type ackResp struct {
	IDs map[string]bool `json:"acks"`
}

func fixupMaxSize(v int) uint {
	if v > 0 {
		return uint(v)
	}
	return defaultMaxHECEventSize
}

// ackRequested returns true if we need to send an ackID for this request.
// It is true if they set a Channel ID in either a header named
// `X-Splunk-Request-Channel` or in a URL query parameter named `channel`.
// If true, it returns the channel ID as the second return value.
func ackRequested(r *http.Request) (bool, string) {
	if r == nil {
		// safeguard
		return false, ""
	}
	if q := r.URL.Query(); q != nil {
		if ch, ok := q["channel"]; ok && len(ch) > 0 {
			return true, ch[0]
		}
	}
	if r.Header != nil && r.Header.Get("X-Splunk-Request-Channel") != "" {
		return true, r.Header.Get("X-Splunk-Request-Channel")
	}
	return false, ""
}

// piaObj is a generic object designed to try and deal with all the "types" of data that can be thrown at this interface
// we have seen strings, integers, floats, json objects, json arrays, a damn "null" even the occaisional "undefined"
// this will deal with decoding all of those and unescape when needed.  Splunk can't support truly binary data, so we
// don't need to infer dealing with base64 encoded byte arrays, but that can happen here too some day.
type piaObj struct {
	payload []byte
}

func (p *piaObj) UnmarshalJSON(b []byte) (err error) {
	//check if its a string
	if len(b) >= 2 {
		if b[0] == '"' && b[len(b)-1] == '"' {
			var str string
			if err = json.Unmarshal(b, &str); err != nil {
				return
			}
			p.payload = []byte(str)
			return
		}
	}
	p.payload = b
	return
}

func (p piaObj) String() string {
	return string(p.payload)
}

func (p piaObj) Bytes() []byte {
	return p.payload
}
