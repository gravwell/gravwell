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
	"strconv"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	defaultHECUrl string = `/services/collector/event`
)

type hecCompatible struct {
	URL               string //override the URL, defaults to "/services/collector/event"
	TokenValue        string `json:"-"` //DO NOT SEND THIS when marshalling
	Tag_Name          string //the tag to assign to the request
	Ignore_Timestamps bool
	Preprocessor      []string
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

func (h *handler) handleHEC(cfg handlerConfig, w http.ResponseWriter, rdr io.Reader, ip net.IP) {
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
}

func includeHecListeners(hnd *handler, igst *ingest.IngestMuxer, cfg *cfgType, lgr *log.Logger) (err error) {
	for _, v := range cfg.HECListener {
		hcfg := handlerConfig{
			hecCompat: true,
		}
		if hcfg.tag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Error("failed to pull tag", log.KV("tag", v.Tag_Name), log.KVErr(err))
			return
		}
		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		}
		hcfg.method = http.MethodPost

		if hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Error("preprocessor construction error", log.KVErr(err))
			return
		}
		if hcfg.auth, err = newPresharedTokenHandler(`Splunk`, v.TokenValue, lgr); err != nil {
			lg.Error("failed to generate HEC-Compatible-Listener auth", log.KVErr(err))
			return
		}
		hnd.mp[v.URL] = hcfg
		debugout("HEC Handler URL %s handling %s\n", v.URL, v.Tag_Name)
	}
	return
}
