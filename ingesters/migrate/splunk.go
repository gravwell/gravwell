/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	splunkTsFmt     string = "2006-01-02T03:04:05-0700"
	splunkStateType string = `splunk`

	MAXCHUNK int = 1000000
)

var (
	splunkTracker *statusTracker = newSplunkTracker()

	startTime time.Time = time.Unix(1, 0)

	ErrNotFound = errors.New("Status not found")
)

type splunk struct {
	Server                  string   // the Splunk server, e.g. splunk.example.com
	Token                   string   // a Splunk auth token
	Ingest_From_Unix_Time   int      // a timestamp to use as the default start time for this Splunk server (default 1)
	Index_Sourcetype_To_Tag []string // a mapping of index+sourcetype to Gravwell tag
	Preprocessor            []string
}

func (s *splunk) Validate(procs processors.ProcessorConfig) (err error) {
	if len(s.Server) == 0 {
		return errors.New("No Splunk server specified")
	}
	if s.Ingest_From_Unix_Time == 0 {
		s.Ingest_From_Unix_Time = 1
	}
	if err = procs.CheckProcessors(s.Preprocessor); err != nil {
		return fmt.Errorf("Files preprocessor invalid: %v", err)
	}
	return
}

func (s *splunk) ParseMappings() ([]SplunkToGravwell, error) {
	var result []SplunkToGravwell
	for _, x := range s.Index_Sourcetype_To_Tag {
		parts := strings.Split(x, ":")
		if len(parts) != 2 {
			return result, fmt.Errorf("Invalid Index-Sourcetype-To-Tag=%v", x)
		}
		s := parts[0]
		tag := parts[1]
		parts = strings.Split(s, ",")
		if len(parts) != 2 {
			return result, fmt.Errorf("Invalid Index-Sourcetype-To-Tag=%v", x)
		}
		idx := parts[0]
		sourcetype := parts[1]
		result = append(result, SplunkToGravwell{Tag: tag, Index: idx, Sourcetype: sourcetype, ConsumedUpTo: startTime})
	}
	return result, nil
}

func (s *splunk) Tags() ([]string, error) {
	var tags []string
	for _, x := range s.Index_Sourcetype_To_Tag {
		parts := strings.Split(x, ":")
		if len(parts) != 2 {
			return tags, fmt.Errorf("Invalid Index-Sourcetype-To-Tag=%v", x)
		}
		tags = append(tags, parts[1])
	}
	return tags, nil
}

// statusTracker keeps track of migration progress for each "config" (Splunk server)
type statusTracker struct {
	sync.Mutex
	splunkStatusMap map[string]splunkStatus // maps splunk cfg name to status struct
}

func newSplunkTracker() *statusTracker {
	return &statusTracker{splunkStatusMap: map[string]splunkStatus{}}
}

func (t *statusTracker) GetStatus(server string) splunkStatus {
	t.Lock()
	defer t.Unlock()
	if status, ok := t.splunkStatusMap[server]; ok {
		return status
	}
	return splunkStatus{Server: server, Progress: map[string]SplunkToGravwell{}}
}

func (t *statusTracker) GetAllStatuses() []splunkStatus {
	t.Lock()
	defer t.Unlock()
	var r []splunkStatus
	for _, v := range t.splunkStatusMap {
		r = append(r, v)
	}
	return r
}

func (t *statusTracker) UpdateServer(server string, status splunkStatus) {
	t.Lock()
	defer t.Unlock()
	t.splunkStatusMap[server] = status
}

func (t *statusTracker) Update(server string, progress SplunkToGravwell) {
	t.Lock()
	defer t.Unlock()
	status, ok := t.splunkStatusMap[server]
	if !ok {
		return
	}
	status.Update(progress)
	return
}

// a splunkStatus keeps track of how much we've migrated from a given splunk server
type splunkStatus struct {
	Name     string // the config name
	Server   string
	Progress map[string]SplunkToGravwell
}

func newSplunkStatus(name, server string) splunkStatus {
	return splunkStatus{Name: name, Server: server, Progress: map[string]SplunkToGravwell{}}
}

func (s *splunkStatus) GetAll() []SplunkToGravwell {
	var result []SplunkToGravwell
	for _, v := range s.Progress {
		result = append(result, v)
	}
	// Sort them!
	sort.Slice(result, func(i, j int) bool {
		if result[i].Index == result[j].Index {
			return result[i].Sourcetype < result[j].Sourcetype
		}
		return result[i].Index < result[j].Index
	})
	return result
}

func (s *splunkStatus) GetAllFullyMapped() []SplunkToGravwell {
	var result []SplunkToGravwell
	for _, v := range s.Progress {
		if v.Tag != "" {
			result = append(result, v)
		}
	}
	// Sort them!
	sort.Slice(result, func(i, j int) bool {
		if result[i].Index == result[j].Index {
			return result[i].Sourcetype < result[j].Sourcetype
		}
		return result[i].Index < result[j].Index
	})
	return result
}

func (s *splunkStatus) Lookup(index, sourcetype string) (SplunkToGravwell, error) {
	if result, ok := s.Progress[fmt.Sprintf("%s,%s", index, sourcetype)]; ok {
		return result, nil
	}
	return SplunkToGravwell{}, ErrNotFound
}

func (s *splunkStatus) Update(progress SplunkToGravwell) {
	s.Progress[progress.key()] = progress
}

// SplunkToGravwell represents migration progress for a single index+sourcetype pair on a Splunk server.
type SplunkToGravwell struct {
	Tag          string    // the gravwell tag
	Index        string    // the Splunk index
	Sourcetype   string    // the Splunk source type
	ConsumedUpTo time.Time // all Splunk data up until this time stamp (exclusive) has been migrated
}

func (s SplunkToGravwell) key() string {
	return fmt.Sprintf("%s,%s", s.Index, s.Sourcetype)
}

func initializeSplunk(cfg *cfgType, st *StateTracker, ctx context.Context) (stop bool, err error) {
	cachedTracker := newSplunkTracker()
	// Read states from the state file
	var obj splunkStatus
	if err := st.GetStates(splunkStateType, &obj, func(val interface{}) error {
		if val == nil {
			return nil ///ummm ok?
		}
		s, ok := val.(*splunkStatus)
		if !ok {
			return fmt.Errorf("invalid splunk status decode value %T", val) // this really should not be possible...
		} else if s == nil {
			return fmt.Errorf("nil splunk status")
		}
		cachedTracker.UpdateServer(s.Name, *s)
		return nil
	}); err != nil {
		return true, fmt.Errorf("Failed to decode splunk states %w", err)
	}
	for k, v := range cfg.Splunk {
		// Grab every mapping for this server *from the config*
		// There is no progress record here, just index+sourcetype→tag,
		// so we also merge in any status we got from the state file
		status := newSplunkStatus(k, v.Server)
		// Merge in the cached statuses
		cached := cachedTracker.GetStatus(k)
		if mappings, err := v.ParseMappings(); err != nil {
			return true, err
		} else {
			for _, x := range mappings {
				// Set "ConsumedUpTo" to whatever the user may have set for Ingest-From-Unix-Time, then
				// if we've already done some ingestion before, it'll get overwritten by the cache lookup
				x.ConsumedUpTo = time.Unix(int64(v.Ingest_From_Unix_Time), 0)
				if c, err := cached.Lookup(x.Index, x.Sourcetype); err == nil {
					x = c
				}
				status.Update(x)
			}
		}
		splunkTracker.UpdateServer(k, status)
	}
	return false, nil
}

func splunkJob(cfgName string, progress SplunkToGravwell, cfg *cfgType, ctx context.Context, updateChan chan string) error {
	lg.Infof("Ingesting index %v sourcetype %v into tag %v\n", progress.Index, progress.Sourcetype, progress.Tag)
	tag, err := igst.NegotiateTag(progress.Tag)
	if err != nil {
		return err
	}
	pproc, err := cfg.getSplunkPreprocessors(cfgName, igst)
	if err != nil {
		return err
	}
	sc, err := cfg.getSplunkConn(cfgName)
	if err != nil {
		return err
	}
	if progress.ConsumedUpTo == startTime {
		// try to figure out when we should start
		var indexes []splunkEntry
		if indexes, err = sc.GetEventIndexes(); err != nil {
			return err
		}
		for i := range indexes {
			if indexes[i].Name == progress.Index {
				if t, err := time.Parse(splunkTsFmt, indexes[i].Content.MinTime); err != nil {
					return err
				} else {
					progress.ConsumedUpTo = t
				}
				break
			}
		}
	}
	updateChan <- fmt.Sprintf("Job started, beginning at %v", progress.ConsumedUpTo)

	var count uint64

	cb := func(s map[string]interface{}) {
		// Grab the timestamp and the raw data
		var ts time.Time
		var data string
		if x, ok := s["_time"]; ok {
			if str, ok := x.(string); ok {
				if ts, err = time.Parse("2006-01-02T15:04:05.000-07:00", str); err != nil {
					lg.Warnf("Failed to parse timestamp %v: %v\n", str, err)
				}
			}
		}
		if x, ok := s["_raw"]; ok {
			if data, ok = x.(string); !ok {
				lg.Infof("could not cast to string!\n")
			}
		}
		ent := &entry.Entry{
			SRC:  nil, // TODO
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			Data: []byte(data),
		}
		pproc.ProcessContext(ent, ctx)
		count++
	}

	chunk := 60 * time.Minute
	var done bool
	for i := 0; !done; i++ {
		if checkSig(ctx) {
			return nil
		}

		// Tweak the window if necessary
		if i%5 == 0 && chunk < 24*time.Hour {
			// increase chunk size every 5 iterations in case things are bursty
			chunk = chunk * 2
		}
		resizing := true
		for resizing {
			cb := func(s map[string]interface{}) {
				resizing = false
				if x, ok := s["count"]; ok {
					if cstr, ok := x.(string); ok {
						if count, err := strconv.Atoi(cstr); err == nil {
							if count >= MAXCHUNK && time.Duration(chunk/2) > time.Second {
								resizing = true // keep trying
								chunk = chunk / 2
							}
						}
					}
				}
			}
			query := fmt.Sprintf("| tstats count WHERE index=\"%s\" AND sourcetype=\"%s\"", progress.Index, progress.Sourcetype)
			if err := sc.RunSearch(query, progress.ConsumedUpTo, progress.ConsumedUpTo.Add(chunk), cb); err != nil {
				return err
			}
		}
		end := progress.ConsumedUpTo.Add(chunk)
		if end.After(time.Now()) {
			end = time.Now()
			done = true
		}

		// run query with current earliest=ConsumedUpTo, latest=ConsumedUpTo+60m
		query := fmt.Sprintf("search index=\"%s\" sourcetype=\"%s\"", progress.Index, progress.Sourcetype)
		if err := sc.RunSearch(query, progress.ConsumedUpTo, end, cb); err != nil {
			return err
		}
		progress.ConsumedUpTo = end
		splunkTracker.Update(cfgName, progress)
		updateChan <- fmt.Sprintf("Migrated %d entries, up to %v", count, progress.ConsumedUpTo)
	}
	return nil
}

func checkMappings(cfgName string, ctx context.Context, updateChan chan string) error {
	updateChan <- fmt.Sprintf("Checking for new sourcetypes on server %s...", cfgName)
	var err error
	// Figure out the config we're dealing with
	var c *splunk
	for k, v := range cfg.Splunk {
		if k == cfgName {
			c = v
		}
	}
	if c == nil {
		return ErrNotFound
	}

	// Pull back the matching status
	status := splunkTracker.GetStatus(cfgName)

	// Connect to Splunk server and get a list of all sourcetypes & indexes
	sc := newSplunkConn(c.Server, c.Token)

	var st []sourcetypeIndex
	st, err = sc.GetIndexSourcetypes()
	if err != nil {
		return err
	}
	var count int
	for _, x := range st {
		for i := range x.Sourcetypes {
			// See if this one exists already
			_, err := status.Lookup(x.Index, x.Sourcetypes[i])
			if err == ErrNotFound {
				// Doesn't exist, create a new one
				tmp := SplunkToGravwell{Index: x.Index, Sourcetype: x.Sourcetypes[i], ConsumedUpTo: time.Unix(int64(c.Ingest_From_Unix_Time), 0)}
				count++
				status.Update(tmp)
			}
		}
	}
	updateChan <- fmt.Sprintf("Found %d new sourcetypes", count)
	splunkTracker.UpdateServer(cfgName, status)
	return nil
}

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
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // ignore expired SSL certificates
	}
	client := &http.Client{Transport: tr}
	return splunkConn{
		Token:   token,
		BaseURL: fmt.Sprintf("https://%s:8089/", server),
		Client:  client,
	}
}

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

func (c *splunkConn) GetIndexSourcetypes() (m []sourcetypeIndex, err error) {
	lg.Infof("Assembling full list of indexes & sourcetypes, this may take a moment\n")
	var b []byte
	var req *http.Request
	var resp *http.Response
	form := url.Values{}
	form.Add("output_mode", "json")
	form.Add("exec_mode", "blocking")
	form.Add("earliest_time", "1")
	form.Add("latest_time", "now")
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

type searchCallback func(map[string]interface{})

func (c *splunkConn) RunSearch(query string, earliest, latest time.Time, cb searchCallback) (err error) {
	var b []byte
	var req *http.Request
	var resp *http.Response
	form := url.Values{}
	form.Add("output_mode", "json")
	form.Add("max_count", fmt.Sprintf("%d", MAXCHUNK))
	form.Add("exec_mode", "blocking")
	form.Add("earliest_time", fmt.Sprintf("%d", earliest.Unix()))
	form.Add("latest_time", fmt.Sprintf("%d", latest.Unix()))
	form.Add("search", query)
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
		return err
	}

	// Now fetch and parse the results
	var offset int
	chunk := 5000
	for {
		u = fmt.Sprintf("%s/services/search/jobs/%s/results?output_mode=json&count=%d&offset=%d", c.BaseURL, sr.SID, chunk, offset)
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
		var results splunkJsonResults
		if err = json.Unmarshal(b, &results); err != nil {
			return
		}
		if err = results.WasError(); err != nil {
			lg.Errorf("Failed to get splunk results: %v", err)
		}
		if len(results.Results) == 0 {
			break
		}
		for i := range results.Results {
			cb(results.Results[i])
		}
		offset += len(results.Results)
	}
	return
}

type splunkJsonResults struct {
	baseResponse
	Results []map[string]interface{} `json:"results"`
}

type baseResponse struct {
	Messages []splunkMessage `json:"messages"`
}

type splunkMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (b *baseResponse) WasError() error {
	for _, m := range b.Messages {
		if m.Type == "FATAL" || m.Type == "WARN" {
			return fmt.Errorf("%s", m.Text)
		}
	}
	return nil
}
