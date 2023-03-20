/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package base

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type hecIgst struct {
	GeneratorConfig
	name string
	auth string
	uri  *url.URL
	to   time.Duration
	src  net.IP
	tags map[entry.EntryTag]string
	cli  http.Client
}

func newHecConn(name string, gc GeneratorConfig, to time.Duration) (hec *hecIgst, err error) {
	var uri *url.URL
	if uri, err = url.Parse(gc.HEC); err != nil {
		return
	}
	hec = &hecIgst{
		GeneratorConfig: gc,
		to:              to,
		uri:             uri,
		name:            name,
		tags:            map[entry.EntryTag]string{0: gc.Tag},
		auth:            fmt.Sprintf(`Splunk %s`, gc.Auth),
	}
	hec.src, err = hec.test()
	return
}

func (hec *hecIgst) test() (ip net.IP, err error) {
	var conn *net.TCPConn
	var raddr *net.TCPAddr
	var ipstr string

	if raddr, err = net.ResolveTCPAddr(`tcp`, hec.uri.Host); err != nil {
		return
	} else if conn, err = net.DialTCP(`tcp`, nil, raddr); err != nil {
		return
	} else if ipstr, _, err = net.SplitHostPort(conn.LocalAddr().String()); err != nil {
		return
	}
	hec.src = net.ParseIP(ipstr)
	ip = hec.src
	err = conn.Close()
	return
}

func (hec *hecIgst) WaitForHot(time.Duration) (err error) {
	return
}

func (hec *hecIgst) Close() (err error) {
	return
}

func (hec *hecIgst) Sync(time.Duration) (err error) {
	return //no...
}

func (hec *hecIgst) SourceIP() (net.IP, error) {
	return hec.src, nil
}

func (hec *hecIgst) LookupTag(tag entry.EntryTag) (string, bool) {
	v, ok := hec.tags[tag]
	return v, ok
}

func (hec *hecIgst) NegotiateTag(v string) (tag entry.EntryTag, err error) {
	if err = ingest.CheckTag(v); err != nil {
		return
	}
	for k, vv := range hec.tags {
		if v == vv {
			tag = k
			return
		}
	}

	tag = entry.EntryTag(len(hec.tags))
	hec.tags[tag] = v
	return
}

func (hec *hecIgst) GetTag(v string) (tag entry.EntryTag, err error) {
	for k, vv := range hec.tags {
		if v == vv {
			tag = k
			return
		}
	}
	err = errors.New("not found")
	return
}

func (hec *hecIgst) Write(ts entry.Timestamp, tag entry.EntryTag, data []byte) error {
	return hec.WriteEntry(&entry.Entry{
		TS:   ts,
		Tag:  tag,
		Data: data,
	})
}

func (hec *hecIgst) WriteBatch(ents []*entry.Entry) error {
	if hec.modeHECRaw {
		for _, v := range ents {
			if err := hec.postRaw(v); err != nil {
				return err
			}
		}
		return nil
	}
	for _, v := range ents {
		if err := hec.postRaw(v); err != nil {
			return err
		}
	}
	return nil
}

func (hec *hecIgst) WriteEntry(ent *entry.Entry) error {
	if hec.modeHECRaw {
		return hec.postRaw(ent)
	}
	return hec.postRaw(ent)
}

type hecEnt struct {
	Time  float64
	ST    string
	Event json.RawMessage
}

func (hec *hecIgst) postRaw(ent *entry.Entry) error {
	req, err := http.NewRequest(http.MethodPost, hec.uri.String(), bytes.NewBuffer(ent.Data))
	if err != nil {
		return err
	}
	var tagname string
	if v, ok := hec.tags[ent.Tag]; ok {
		tagname = v
	}
	req.Header.Set(`User-Agent`, hec.name)

	//attach URL parameters
	uri := req.URL
	values := uri.Query()
	values.Add(`Authorization`, hec.auth)
	values.Add(`sourcetype`, tagname)
	req.URL.RawQuery = values.Encode()
	resp, err := hec.cli.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status %d (%v)", resp.StatusCode, resp.Status)
	}
	return nil
}

// TODO FIXME
func (hec *hecIgst) encode(wtr io.Writer, ent entry.Entry) (err error) {
	return
}

func (hec *hecIgst) postEvent(ent entry.Entry) error {
	bb := bytes.NewBuffer(nil)
	if err := hec.encode(bb, ent); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, hec.uri.String(), bb)
	if err != nil {
		return err
	}
	req.Header.Add(`Authorization`, hec.auth)
	req.Header.Set(`User-Agent`, hec.name)
	resp, err := hec.cli.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status %d (%v)", resp.StatusCode, resp.Status)
	}
	return nil
}
