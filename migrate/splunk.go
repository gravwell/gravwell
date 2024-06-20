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

	maxChunkMegabytes uint64 = 800 // For sanity's sake... we've seen systems where a user can't have more than 1GB of query results at a time, so let's try to make sure we never hit that
)

var (
	splunkTracker *statusTracker = newSplunkTracker()

	ErrNotFound = errors.New("Status not found")
)

type splunk struct {
	Server                   string   // the Splunk server, e.g. splunk.example.com
	Insecure_Skip_TLS_Verify bool     // don't verify the *splunk* certs
	Token                    string   // a Splunk auth token
	Max_Chunk                int      // maximum number of entries to try and grab at once. Defaults to a conservative 500,000
	Ingest_From_Unix_Time    int      // a timestamp to use as the default start time for this Splunk server (default 1)
	Ingest_To_Unix_Time      int      // a timestamp to use as the end time for this Splunk server (default 0, meaning "now")
	Index_Sourcetype_To_Tag  []string // a mapping of index+sourcetype to Gravwell tag
	Disable_Intrinsics       bool     // If set, no additional fields will be attached as intrinsic EVs on the ingested data
	Preprocessor             []string
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
					lg.Warn("could not determine actual start time of data from splunk-derived mintime, beginning at user-configured start time", log.KV("mintime", indexes[i].Content.MinTime), log.KVErr(err))
				} else if t.After(progress.ConsumedUpTo) {
					lg.Infof("Fast-forwarding ingest job for index %v sourcetype %v to actual beginning of data (%v)\n", progress.Index, progress.Sourcetype, t)
					progress.ConsumedUpTo = t
				}
				break
			}
		}
	}
	progress.ConsumedUpTo = progress.ConsumedUpTo.Truncate(1 * time.Second)
	updateChan <- fmt.Sprintf("Job started, beginning at %v", progress.ConsumedUpTo)

	var count uint64
	var byteTotal uint64

	// we also maintain a moving average of the entry size
	// we'll just initialize the average to something big so we don't get surprised
	avgSize := float64(4096)
	alpha := 0.1

	evWarnings := map[string]int{} // map EV names to failure counts

	lastTS := progress.ConsumedUpTo
	cb := func(s map[string]string) {
		var entSize int
		// Grab the timestamp and the raw data
		var ts time.Time
		var data string
		var ok bool
		if x, ok := s["epoch_time"]; ok {
			x = strings.Trim(strings.TrimSpace(x), `"`)
			if f, err := strconv.ParseFloat(x, 64); err == nil {
				sec, dec := math.Modf(f)
				ts = time.Unix(int64(sec), int64(dec*(1e9)))
				lastTS = ts
			} else {
				lg.Warn("Failed to parse timestamp", log.KV("epoch_time", x), log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KVErr(err))
				// just use whatever we saw last, so it's *close*
				ts = lastTS
			}
		} else {
			lg.Warn("No epoch_time field, using the most recently seen timestamp", log.KV("timestamp", x), log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag))
			ts = lastTS
		}
		if data, ok = s["_raw"]; !ok {
			lg.Info("could not get data", log.KV("incoming", s), log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KV("timestamp", ts))
			return
		}
		ent := &entry.Entry{
			SRC:  nil, // TODO
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			Data: []byte(data),
		}
		// Now add in everything else, provided they've enabled it
		for k, v := range s {
			entSize += len(v)
			if !splunkConfig.Disable_Intrinsics {
				if k == "_raw" {
					continue
				}
				// Trim space and quotes on the name, just to be sure
				k = strings.TrimSpace(k)
				k = strings.Trim(k, `"`)
				ev, err := entry.NewEnumeratedValue(k, v)
				if err != nil {
					// we'll only warn once
					warnCount, _ := evWarnings[k]
					warnCount++
					evWarnings[k] = warnCount
					continue
				}
				if err := ent.AddEnumeratedValue(ev); err != nil {
					// If we were able to make the EV, it should pretty much always be possible to attach it.
					lg.Info("failed to attach enumerated value", log.KV("name", k), log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KV("timestamp", ts), log.KVErr(err))
					continue
				}
			}
		}
		pproc.ProcessContext(ent, ctx)
		count++
		byteTotal += ent.Size()
		avgSize = (alpha * float64(entSize)) + (1.0-alpha)*avgSize
	}

	chunk := 20 * time.Second
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
				if cstr, ok := s["count"]; ok {
					if count, err := strconv.Atoi(cstr); err == nil {
						if (count >= splunkConfig.maxChunk() || uint64(count)*uint64(avgSize) > maxChunkMegabytes*ingest.MB) && time.Duration(chunk/2) > 2*time.Second {
							chunk = chunk / 2
						} else {
							resizing = false
							expectedCount = uint64(count)
						}
					}
				}
			}
			query := fmt.Sprintf("| tstats count WHERE index=\"%s\" AND sourcetype=\"%s\"", progress.Index, progress.Sourcetype)
			chunk = chunk.Truncate(time.Second)
			if err := sc.RunExportSearch(query, progress.ConsumedUpTo, progress.ConsumedUpTo.Add(chunk), false, 100, cb); err != nil {
				lg.Error("Window size estimation returned an error, cancelling job", log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KV("start", progress.ConsumedUpTo), log.KV("end", progress.ConsumedUpTo.Add(chunk)), log.KVErr(err))
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

		// Periodically output some logs
		if i%20 == 1 {
			lg.Infof("Pulling %v/%v from %v to %v. Current avgSize is %v\n", progress.Index, progress.Sourcetype, progress.ConsumedUpTo, end, avgSize)
		}

		// run query with current earliest=ConsumedUpTo, latest=ConsumedUpTo+60m
		oldCount := count
		query := fmt.Sprintf("search index=\"%s\" sourcetype=\"%s\" | rename _time AS epoch_time | table *", progress.Index, progress.Sourcetype)
		if splunkConfig.Disable_Intrinsics {
			query = fmt.Sprintf("search index=\"%s\" sourcetype=\"%s\" | rename _time AS epoch_time | table epoch_time _raw", progress.Index, progress.Sourcetype)
		}
		if err := sc.RunExportSearch(query, progress.ConsumedUpTo, end, true, expectedCount+100, cb); err != nil {
			lg.Error("Error while exporting entries, cancelling job", log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KV("start", progress.ConsumedUpTo), log.KV("end", end), log.KVErr(err))
			return fmt.Errorf("entry retrieval query returned an error: %w", err)
		}
		// If we had problems creating EVs from the data Splunk sent, warn us.
		for k, v := range evWarnings {
			lg.Info("failed to create enumerated value at least once while processing chunk", log.KV("name", k), log.KV("failurecount", v), log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KV("start", progress.ConsumedUpTo), log.KV("end", end), log.KVErr(err))
		}
		// reset the map
		evWarnings = map[string]int{}

		if count-oldCount < expectedCount {
			lg.Error("Got fewer entries than expected", log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KV("start", progress.ConsumedUpTo), log.KV("end", end), log.KV("expected", expectedCount), log.KV("actual", count-oldCount))
		} else if count-oldCount > expectedCount {
			lg.Error("Got more entries than expected", log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KV("start", progress.ConsumedUpTo), log.KV("end", end), log.KV("expected", expectedCount), log.KV("actual", count-oldCount))
		}

		progress.ConsumedUpTo = end
		splunkTracker.Update(cfgName, progress)
		elapsed := time.Now().Sub(startTime)
		if *fParanoid {
			status := splunkTracker.GetStatus(cfgName)
			st.Add(splunkStateType, status)
		}

		updateChan <- fmt.Sprintf("Migrated %d entries [%v/%v] up to %v", count, ingest.HumanEntryRate(count, elapsed), ingest.HumanRate(byteTotal, elapsed), progress.ConsumedUpTo)
	}
	lg.Info("job completed", log.KV("index", progress.Index), log.KV("sourcetype", progress.Sourcetype), log.KV("tag", progress.Tag), log.KV("start", progress.ConsumedUpTo))
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
	sc := newSplunkConn(c.Server, c.Token, c.Insecure_Skip_TLS_Verify)

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
