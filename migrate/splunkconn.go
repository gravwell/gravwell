/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

var (
	fInsecureSkipTlsVerify = flag.Bool("insecure-skip-tls-verify", false, "Skip TLS validation for HTTPS connections")
)

type splunkEntry struct {
	Name    string        `json:"name"`
	Content splunkContent `json:"content"`
}

type splunkContent struct {
	// For /services/data/indexes
	DataType        string `json:"datatype"`
	MinTime         string `json:"minTime"`
	MaxTime         string `json:"maxTime"`
	DefaultDatabase string `json:"defaultDatabase"`
	TotalEventCount int    `json:"totalEventCount"`

	// For /services/search/jobs/<sid>
	IsDone bool `json:"isDone"`
}

type splunkMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type baseResponse struct {
	Messages []splunkMessage `json:"messages"`
}

func (b *baseResponse) WasError() error {
	for _, m := range b.Messages {
		if m.Type == "FATAL" || m.Type == "WARN" {
			return fmt.Errorf("%s", m.Text)
		}
	}
	return nil
}

type splunkResponse struct {
	baseResponse
	Entries []splunkEntry `json:"entry"`
}

type splunkConn struct {
	Token   string
	BaseURL string // e.g. "https://splunk.example.com:8089/"
	Client  *http.Client
}

func newSplunkConn(server, token string) splunkConn {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: *fInsecureSkipTlsVerify,
		}, // ignore expired SSL certificates
	}
	client := &http.Client{Transport: tr}
	return splunkConn{
		Token:   token,
		BaseURL: fmt.Sprintf("https://%s/", config.AppendDefaultPort(server, 8089)),
		Client:  client,
	}
}

// GetEventIndexes returns an array of indexes on the Splunk server
func (c *splunkConn) GetEventIndexes() (indexes []splunkEntry, err error) {
	var b []byte
	var req *http.Request
	var resp *http.Response
	idxURL := fmt.Sprintf("%s/services/data/indexes?output_mode=json", c.BaseURL)
	if req, err = http.NewRequest(http.MethodGet, idxURL, nil); err != nil {
		return
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	if resp, err = c.Client.Do(req); err != nil {
		return
	}
	if b, err = ioutil.ReadAll(resp.Body); err != nil {
		return
	}

	var x splunkResponse
	if err = json.Unmarshal(b, &x); err != nil {
		return
	}

	for _, v := range x.Entries {
		if v.Content.DataType == "event" {
			indexes = append(indexes, v)
		}
	}
	return
}

type splunkSearchLaunchResponse struct {
	baseResponse
	SID string `json:"sid"`
}

// GetSourceTypes returns a list of sourcetypes found on the Splunk server
func (c *splunkConn) GetSourceTypes() (sourcetypes []string, err error) {
	var b []byte
	var req *http.Request
	var resp *http.Response
	u := fmt.Sprintf("%s/services/saved/sourcetypes?output_mode=json&count=0", c.BaseURL)
	if req, err = http.NewRequest(http.MethodGet, u, nil); err != nil {
		return
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	if resp, err = c.Client.Do(req); err != nil {
		return
	}
	if b, err = ioutil.ReadAll(resp.Body); err != nil {
		return
	}
	var x splunkResponse
	if err = json.Unmarshal(b, &x); err != nil {
		return
	}

	for _, v := range x.Entries {
		sourcetypes = append(sourcetypes, v.Name)
	}
	return
}

type sourcetypes []string

func (s *sourcetypes) UnmarshalJSON(v []byte) (err error) {
	var x []string
	var str string
	if err = json.Unmarshal(v, &x); err == nil {
		*s = sourcetypes(x)
	} else if err = json.Unmarshal(v, &str); err == nil {
		*s = sourcetypes([]string{str})
	} else {
		return errors.New("Cannot unmarshal sourcetype")
	}
	return nil
}

type sourcetypeIndex struct {
	Index       string      `json:"index"`
	Sourcetypes sourcetypes `json:"sourcetypes"`
}

// GetIndexSourcetypes returns a list of all index+sourcetype combinations found on the server.
func (c *splunkConn) GetIndexSourcetypes(start, end int) (m []sourcetypeIndex, err error) {
	lg.Infof("Assembling full list of indexes & sourcetypes, this may take a moment\n")
	var b []byte
	var req *http.Request
	var resp *http.Response
	form := url.Values{}
	form.Add("output_mode", "json")
	form.Add("exec_mode", "blocking")
	form.Add("earliest_time", fmt.Sprintf("%d", start))
	if end != 0 {
		form.Add("latest_time", fmt.Sprintf("%d", end))
	} else {
		form.Add("latest_time", "now")
	}
	form.Add("time_format", "%s")
	form.Add("search", `| tstats count WHERE index=* OR sourcetype=* by index,sourcetype | stats values(sourcetype) AS sourcetypes by index`)
	u := fmt.Sprintf("%s/services/search/jobs", c.BaseURL)
	if req, err = http.NewRequest(http.MethodPost, u, strings.NewReader(form.Encode())); err != nil {
		return
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	if resp, err = c.Client.Do(req); err != nil {
		return
	}
	if b, err = ioutil.ReadAll(resp.Body); err != nil {
		return
	}

	var sr splunkSearchLaunchResponse
	if err = json.Unmarshal(b, &sr); err != nil {
		return
	}
	if err = sr.WasError(); err != nil {
		return
	}

	// Now fetch and parse the results
	u = fmt.Sprintf("%s/services/search/jobs/%s/results?output_mode=json", c.BaseURL, sr.SID)
	if req, err = http.NewRequest(http.MethodGet, u, nil); err != nil {
		return
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	if resp, err = c.Client.Do(req); err != nil {
		return
	}
	if b, err = ioutil.ReadAll(resp.Body); err != nil {
		return
	}

	var x struct {
		baseResponse
		Results []sourcetypeIndex `json:"results"`
	}
	if err = json.Unmarshal(b, &x); err != nil {
		return
	}
	if err = x.WasError(); err != nil {
		return
	}
	m = x.Results
	return
}

type exportCallback func(map[string]string)

// RunExportSearch runs a query on the Splunk server between the specified times.
// The callback function is called once per result.
// Note that this uses the `export` REST API.
func (c *splunkConn) RunExportSearch(query string, earliest, latest time.Time, preview bool, maxcount uint64, cb exportCallback) (err error) {
	var req *http.Request
	var resp *http.Response
	form := url.Values{}
	id := rand.Int31()
	form.Add("output_mode", "csv")
	form.Add("id", fmt.Sprintf("%d", id))
	form.Add("earliest_time", fmt.Sprintf("%d", earliest.Unix()))
	form.Add("latest_time", fmt.Sprintf("%d", latest.Unix()))
	form.Add("preview", fmt.Sprintf("%v", preview))
	form.Add("max_count", fmt.Sprintf("%d", maxcount))
	form.Add("search", query)
	u := fmt.Sprintf("%s/services/search/v2/jobs/export", c.BaseURL)
	if req, err = http.NewRequest(http.MethodPost, u, strings.NewReader(form.Encode())); err != nil {
		return
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	if resp, err = c.Client.Do(req); err != nil {
		return
	}
	defer resp.Body.Close()
	// Clean up on the way out
	defer func() {
		u := fmt.Sprintf("%s/services/search/jobs/%d", c.BaseURL, id)
		if req, err = http.NewRequest(http.MethodDelete, u, nil); err != nil {
			return
		}
		if _, err := c.Client.Do(req); err != nil {
			lg.Warn("failed to delete Splunk search job after reading results", log.KV("id", id))
		}
	}()
	// wrap the body in a CSV reader
	rdr := csv.NewReader(resp.Body)
	rdr.LazyQuotes = true
	// we'll test the record length ourselves
	rdr.FieldsPerRecord = -1

	// get the header
	header, err := rdr.Read()
	if err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	if len(header) == 0 {
		// weird, nothing?
		return nil
	}
	// For some stupid reason, sometimes they send leading
	// whitespace. This also messes with CSV's ability to trim out
	// the double-quotes.
	for i := range header {
		header[i] = strings.Trim(strings.TrimSpace(header[i]), `"`)
	}
	// If Splunk gets upset at us, we'll get xml back. Bail
	if header[0] == `<?xml version="1.0" encoding="UTF-8"?>` {
		return errors.New("Splunk returned an error message, giving up")
	}
	ent := map[string]string{}
	for {
		record, err := rdr.Read()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if len(record) != len(header) {
			return fmt.Errorf("record length mismatch, record = %v, header = %v\n", record, header)
		}
		for i := range header {
			ent[header[i]] = record[i]
		}
		cb(ent)
	}
	return
}
