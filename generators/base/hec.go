/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package base

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/klauspost/compress/gzip"
)

type hecIgst struct {
	GeneratorConfig
	name  string
	auth  string
	uri   *url.URL
	to    time.Duration
	src   net.IP
	tags  map[entry.EntryTag]string
	wg    *sync.WaitGroup
	errch chan error
	wtr   io.WriteCloser
	rdr   io.ReadCloser
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
		errch:           make(chan error, 1),
	}
	if hec.src, err = hec.test(); err != nil {
		return
	}
	hec.rdr, hec.wtr = io.Pipe()
	if gc.Compression {
		hec.wtr = gzip.NewWriter(hec.wtr)
	}
	go hec.httpRoutine(hec.rdr)
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

func (hec *hecIgst) httpRoutine(rdr io.ReadCloser) {
	if !hec.GeneratorConfig.ChaosMode {
		hec.multiLineRequest(rdr)
	} else {
		hec.singleEntryRequest(rdr)
	}
	rdr.Close()
}

func (hec *hecIgst) multiLineRequest(rdr io.Reader) {
	var err error
	var req *http.Request
	var resp *http.Response
	var cli http.Client

	defer close(hec.errch)
	if req, err = http.NewRequest(http.MethodPost, hec.uri.String(), rdr); err != nil {
		hec.errch <- err
		return
	}
	req.Header.Add(`Authorization`, hec.auth)
	req.Header.Set(`User-Agent`, hec.name)
	if hec.Compression {
		req.Header.Add(`Content-Encoding`, `gzip`)
	}

	if hec.modeHECRaw {
		//attach URL parameters
		uri := req.URL
		values := uri.Query()
		values.Add(`sourcetype`, hec.Tag)
		req.URL.RawQuery = values.Encode()
	}

	if resp, err = cli.Do(req); err != nil {
		hec.errch <- err
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var msg string
		lr := &io.LimitedReader{R: resp.Body, N: 512}
		if body, err := io.ReadAll(lr); err == nil || err == io.EOF {
			msg = string(body)
		} else {
			msg = err.Error()
		}
		hec.errch <- fmt.Errorf("invalid status %d (%v)\n%s", resp.StatusCode, resp.Status, msg)
	} else {
		hec.errch <- nil
	}
}

func (hec *hecIgst) requester(ch chan []byte, wg *sync.WaitGroup, id int) {
	defer wg.Done()
	var err error
	var req *http.Request
	var resp *http.Response
	d := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	cli := http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2:     (id & 0x1) == 1,
			DisableKeepAlives:     (id & 0x2) == 0x2,
			DisableCompression:    (id & 0x1) == 0x1,
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           d.DialContext,
			MaxIdleConns:          3,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	if req, err = http.NewRequest(http.MethodPost, hec.uri.String(), nil); err != nil {
		hec.errch <- err
		return
	}
	req.Header.Add(`Authorization`, hec.auth)
	req.Header.Set(`User-Agent`, hec.name)

	if hec.modeHECRaw {
		//attach URL parameters
		uri := req.URL
		values := uri.Query()
		values.Add(`sourcetype`, hec.Tag)
		values.Add(`source`, hec.SRC.String())
		req.URL.RawQuery = values.Encode()
	}
	body := readCloser{
		Reader: bytes.NewReader(nil),
	}
	for ln := range ch {
		body.Reset(ln)
		req.Body = body
		if resp, err = cli.Do(req); err != nil {
			hec.errch <- err
			return
		} else if resp.StatusCode != http.StatusOK {
			var msg string
			lr := &io.LimitedReader{R: resp.Body, N: 1024 * 1024}
			if body, err := io.ReadAll(lr); err == nil || err == io.EOF {
				msg = string(body)
			} else {
				msg = err.Error()
			}
			io.Copy(&nilWriter{}, resp.Body)
			resp.Body.Close()
			fmt.Println("POST failed", msg)
			break
		} else {
			io.Copy(&nilWriter{}, resp.Body)
			resp.Body.Close()
		}
	}
}

func (hec *hecIgst) singleEntryRequest(rdr io.Reader) {
	defer close(hec.errch)
	ch := make(chan []byte, hec.ChaosWorkers)
	wg := &sync.WaitGroup{}
	for i := 0; i < hec.ChaosWorkers; i++ {
		wg.Add(1)
		go hec.requester(ch, wg, i)
	}
	lr := bufio.NewReaderSize(rdr, 1024*1024)
	for {
		ln, err := lr.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				hec.errch <- nil
			} else {
				hec.errch <- nil
			}
			break
		} else if len(ln) == 0 {
			continue
		}
		ch <- bytes.Clone(ln)
	}
	close(ch)
	wg.Wait()
}

type nilWriter struct {
}

func (nw *nilWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

type readCloser struct {
	*bytes.Reader
}

func (rc readCloser) Close() error {
	return nil
}

func (hec *hecIgst) WaitForHot(time.Duration) (err error) {
	return
}

func (hec *hecIgst) Close() (err error) {
	hec.wtr.Close()
	err = <-hec.errch
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
	for _, v := range ents {
		if err := hec.WriteEntry(v); err != nil {
			return err
		}
	}
	return nil
}

func (hec *hecIgst) WriteEntry(ent *entry.Entry) (err error) {
	if hec.modeHECRaw {
		err = hec.sendRaw(ent)
	} else {
		err = hec.sendEvent(ent)
	}
	return
}

type hecEnt struct {
	Time  float64
	ST    string
	Event json.RawMessage
}

func (hec *hecIgst) sendRaw(ent *entry.Entry) error {
	if _, err := hec.wtr.Write(ent.Data); err != nil {
		return err
	} else if _, err = hec.wtr.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

type hecent struct {
	Event json.RawMessage `json:"event,omitempty"`
	Time  float64         `json:"time,omitempty"`
	ST    string          `json:"sourcetype,omitempty"`
}

var osc bool

func setData(data []byte) json.RawMessage {
	//check if this is a JSON object
	if len(data) >= 2 && data[0] == '{' && data[len(data)-1] == '}' {
		if osc = !osc; osc {
			//return it as is
			return json.RawMessage(data)
		}
		//else fall through and encode as a string
	}
	if v, err := json.Marshal(string(data)); err == nil {
		return json.RawMessage(v)
	}
	return nil
}

func (hec *hecIgst) sendEvent(ent *entry.Entry) (err error) {
	if ent != nil {
		v := hecent{
			Time:  timeFloat(ent.TS),
			Event: setData(ent.Data),
			ST:    hec.Tag,
		}
		err = json.NewEncoder(hec.wtr).Encode(v)
	}
	return
}

const (
	TS_SIZE int = 12

	secondsPerMinute       = 60
	secondsPerHour         = 60 * 60
	secondsPerDay          = 24 * secondsPerHour
	secondsPerWeek         = 7 * secondsPerDay
	daysPer400Years        = 365*400 + 97
	daysPer100Years        = 365*100 + 24
	daysPer4Years          = 365*4 + 1
	unixToInternal   int64 = (1969*365 + 1969/4 - 1969/100 + 1969/400) * secondsPerDay
)

func timeFloat(ts entry.Timestamp) (r float64) {
	//assign unix time seconds, the HEC ingester can't handle timestamps before the unix EPOC, so if its before that just set it to zero
	if ts.Sec < unixToInternal {
		return //just return zero
	}
	r = float64(ts.Sec - unixToInternal)
	//no add in the nano portion as ms
	r += float64(ts.Nsec) / float64(1000000000)
	return
}
