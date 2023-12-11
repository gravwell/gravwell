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
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	splunkTsFmt     string = "2006-01-02T15:04:05-0700"
	splunkStateType string = `splunk`

	defaultMaxChunk int = 500000
)

var (
	splunkTracker *statusTracker = newSplunkTracker()

	ErrNotFound = errors.New("Status not found")
)

type splunk struct {
	Server                  string   // the Splunk server, e.g. splunk.example.com
	Token                   string   // a Splunk auth token
	Max_Chunk               int      // maximum number of entries to try and grab at once. Defaults to a conservative 500,000
	Ingest_From_Unix_Time   int      // a timestamp to use as the default start time for this Splunk server (default 1)
	Ingest_To_Unix_Time     int      // a timestamp to use as the end time for this Splunk server (default 0, meaning "now")
	Index_Sourcetype_To_Tag []string // a mapping of index+sourcetype to Gravwell tag
	Disable_Intrinsics      bool     // If set, no additional fields will be attached as intrinsic EVs on the ingested data
	Preprocessor            []string
}

func (s *splunk) Validate(procs processors.ProcessorConfig) (err error) {
	if len(s.Server) == 0 {
		return errors.New("No Splunk server specified")
	}

	if err = procs.CheckProcessors(s.Preprocessor); err != nil {
		return fmt.Errorf("Files preprocessor invalid: %v", err)
	}
	return
}

func (s *splunk) maxChunk() int {
	if s.Max_Chunk <= 0 {
		return defaultMaxChunk
	}
	return s.Max_Chunk
}

func (s *splunk) startTime() time.Time {
	if s.Ingest_From_Unix_Time <= 0 {
		return time.Unix(1, 0)
	}
	return time.Unix(int64(s.Ingest_From_Unix_Time), 0)
}

func (s *splunk) endTime() time.Time {
	if s.Ingest_To_Unix_Time <= 0 {
		return time.Unix(0, 0)
	}
	return time.Unix(int64(s.Ingest_To_Unix_Time), 0)
}

func (s *splunk) ParseMappings() ([]SplunkToGravwell, error) {
	var result []SplunkToGravwell
	for _, x := range s.Index_Sourcetype_To_Tag {
		idx, sourcetype, tag, err := parseMapping(x)
		if err != nil {
			return nil, err
		}
		result = append(result, SplunkToGravwell{Tag: tag, Index: idx, Sourcetype: sourcetype, ConsumedUpTo: s.startTime()})
	}
	return result, nil
}

func parseMapping(v string) (index, sourcetype, tag string, err error) {
	var fields []string
	dec := csv.NewReader(strings.NewReader(v))
	dec.LazyQuotes = true
	dec.TrimLeadingSpace = true
	if fields, err = dec.Read(); err != nil {
		return
	} else if len(fields) != 3 {
		err = fmt.Errorf("improper index sourcetype to tag mapping %q, have %d fields need 3", v, len(fields))
		return
	}
	if index = fields[0]; len(index) == 0 {
		err = fmt.Errorf("missing index on tag mapping %q", v)
		return
	}

	if sourcetype = fields[1]; len(sourcetype) == 0 {
		err = fmt.Errorf("missing sourcetype on tag mapping %q", v)
		return
	}

	if tag = fields[2]; len(tag) == 0 {
		err = fmt.Errorf("missing tag on tag mapping %q", v)
		return
	}
	err = ingest.CheckTag(tag)
	return
}

func (s *splunk) Tags() ([]string, error) {
	var tags []string
	if stg, err := s.ParseMappings(); err != nil {
		return nil, err
	} else {
		for _, v := range stg {
			tags = append(tags, v.Tag)
		}
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
	Tag            string    // the gravwell tag
	Index          string    // the Splunk index
	Sourcetype     string    // the Splunk source type
	ConsumedUpTo   time.Time // all Splunk data up until this time stamp (exclusive) has been migrated
	ConsumeEndTime time.Time // read up to this time stamp. if zero, it'll read until now
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
				x.ConsumedUpTo = v.startTime()
				x.ConsumeEndTime = v.endTime()
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
	splunkConfig, err := cfg.getSplunkConfig(cfgName)
	if err != nil {
		return err
	}
	if progress.ConsumedUpTo == splunkConfig.startTime() {
		// try to figure out when we should start
		var indexes []splunkEntry
		if indexes, err = sc.GetEventIndexes(); err != nil {
			return err
		}
		for i := range indexes {
			if indexes[i].Name == progress.Index {
				if t, err := time.Parse(splunkTsFmt, indexes[i].Content.MinTime); err != nil {
					return err
				} else if t.After(progress.ConsumedUpTo) {
					lg.Infof("Fast-forwarding ingest job for index %v sourcetype %v to actual beginning of data (%v)\n", progress.Index, progress.Sourcetype, t)
					progress.ConsumedUpTo = t
				}
				break
			}
		}
	}
	updateChan <- fmt.Sprintf("Job started, beginning at %v", progress.ConsumedUpTo)

	var count uint64
	var byteTotal uint64

	cb := func(s map[string]string) {
		// Grab the timestamp and the raw data
		var ts time.Time
		var data string
		var ok bool
		if x, ok := s["epoch_time"]; ok {
			if f, err := strconv.ParseFloat(x, 64); err == nil {
				sec, dec := math.Modf(f)
				ts = time.Unix(int64(sec), int64(dec*(1e9)))
			} else {
				lg.Warnf("Failed to parse timestamp %v: %v\n", x, err)
			}
		}
		if data, ok = s["_raw"]; !ok {
			lg.Infof("could not get data! incoming Splunk entry was: %v\n", s)
			return
		}
		ent := &entry.Entry{
			SRC:  nil, // TODO
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			Data: []byte(data),
		}
		// Now add in everything else, provided they've enabled it
		if !splunkConfig.Disable_Intrinsics {
			for k, v := range s {
				if k == "_raw" {
					continue
				}
				ev, err := entry.NewEnumeratedValue(k, v)
				if err != nil {
					lg.Infof("failed to make enumerated value %v: %v\n", k, err)
					continue
				}
				if err := ent.AddEnumeratedValue(ev); err != nil {
					lg.Infof("failed to attach enumerated value %v: %v\n", k, err)
					continue
				}
			}
		}
		pproc.ProcessContext(ent, ctx)
		count++
		byteTotal += ent.Size()
	}

	chunk := 60 * time.Minute
	var done bool
	startTime := time.Now()
	for i := 0; !done; i++ {
		if checkSig(ctx) {
			return nil
		}

		// Tweak the window if necessary
		if i%5 == 1 && chunk < 24*time.Hour {
			// increase chunk size every 5 iterations in case things are bursty
			chunk = chunk * 2
		}
		resizing := true
		var expectedCount uint64
		for resizing {
			cb := func(s map[string]string) {
				resizing = false
				if cstr, ok := s["count"]; ok {
					if count, err := strconv.Atoi(cstr); err == nil {
						if count >= splunkConfig.maxChunk() && time.Duration(chunk/2) > time.Second {
							resizing = true // keep trying
							chunk = chunk / 2
						} else {
							expectedCount = uint64(count)
						}
					}
				}
			}
			query := fmt.Sprintf("| tstats count WHERE index=\"%s\" AND sourcetype=\"%s\"", progress.Index, progress.Sourcetype)
			if err := sc.RunExportSearch(query, progress.ConsumedUpTo, progress.ConsumedUpTo.Add(chunk), cb); err != nil {
				return fmt.Errorf("window size estimation query returned an error: %w", err)
			}
		}
		end := progress.ConsumedUpTo.Add(chunk)
		if !progress.ConsumeEndTime.IsZero() && progress.ConsumeEndTime.Unix() != 0 && end.After(progress.ConsumeEndTime) {
			end = progress.ConsumeEndTime
			done = true
		} else if end.After(time.Now()) {
			end = time.Now()
			done = true
		}
		// It's possible we ended up with start == end, in which case we're good!
		if progress.ConsumedUpTo == end {
			break
		}

		// run query with current earliest=ConsumedUpTo, latest=ConsumedUpTo+60m
		oldCount := count
		query := fmt.Sprintf("search index=\"%s\" sourcetype=\"%s\" | rename _time AS epoch_time | table epoch_time _raw", progress.Index, progress.Sourcetype)
		if err := sc.RunExportSearch(query, progress.ConsumedUpTo, end, cb); err != nil {
			return fmt.Errorf("entry retrieval query returned an error: %w", err)
		}
		if count-oldCount < expectedCount {
			lg.Error("Got fewer entries than expected", log.KV("start", progress.ConsumedUpTo), log.KV("end", end), log.KV("expected", expectedCount), log.KV("actual", count-oldCount))
		}
		progress.ConsumedUpTo = end
		splunkTracker.Update(cfgName, progress)
		elapsed := time.Now().Sub(startTime)

		updateChan <- fmt.Sprintf("Migrated %d entries [%v EPS/%v] up to %v", count, count/uint64(elapsed.Seconds()), ingest.HumanRate(byteTotal, elapsed), progress.ConsumedUpTo)
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
	st, err = sc.GetIndexSourcetypes(c.Ingest_From_Unix_Time, c.Ingest_To_Unix_Time)
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
				tmp := SplunkToGravwell{Index: x.Index, Sourcetype: x.Sourcetypes[i], ConsumedUpTo: c.startTime(), ConsumeEndTime: c.endTime()}
				count++
				status.Update(tmp)
			}
		}
	}
	updateChan <- fmt.Sprintf("Found %d new sourcetypes", count)
	splunkTracker.UpdateServer(cfgName, status)
	return nil
}
