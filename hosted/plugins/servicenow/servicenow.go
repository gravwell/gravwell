// Package servicenow implements a read-only multi-product ServiceNow Table API
// Hosted Runner plugin and the shared client contract used by both Fetchers.
package servicenow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

type provenanceMetadata struct {
	Vendor     string
	Product    string
	Source     string
	RecordType string
	Endpoint   string
	APIVersion string
}

func attachIntrinsicProvenance(ent *entry.Entry, metadata provenanceMetadata) error {
	values := [...]struct {
		name  string
		value string
	}{
		{name: "_vendor", value: metadata.Vendor},
		{name: "_product", value: metadata.Product},
		{name: "_source", value: metadata.Source},
		{name: "_recordType", value: metadata.RecordType},
		{name: "_endpoint", value: metadata.Endpoint},
		{name: "_apiVersion", value: metadata.APIVersion},
	}
	for _, value := range values {
		if value.value == "" {
			return fmt.Errorf("required provenance value %s is empty", value.name)
		}
		if value.name == "_endpoint" {
			for _, r := range value.value {
				if r == '\r' || r == '\n' {
					return fmt.Errorf("provenance endpoint contains a physical line break")
				}
			}
		}
		if err := ent.AddEnumeratedValueEx(value.name, []byte(value.value)); err != nil {
			return fmt.Errorf("attach provenance value %s: %w", value.name, err)
		}
	}
	return nil
}

const (
	Name    = "servicenow"
	ID      = "servicenow.ingesters.gravwell.io"
	Version = "0.1.0"
)

type cursorState struct {
	Checkpoint, WindowEnd time.Time
	HighWater             time.Time
	PageTimestamp         time.Time
	PageID                string
	Offset                int
	NextURL               string
	Seen                  map[string]time.Time
	Hashes                map[string][sha256.Size]byte
}

type entryProcessor interface {
	ProcessContext(*entry.Entry, context.Context) error
}

type ServiceNow struct {
	conf             *Config
	proc             entryProcessor
	processMu, tagMu sync.Mutex
	tags             map[string]entry.EntryTag
	source           net.IP
	now              func() time.Time
}

func New(conf *Config, proc entryProcessor) *ServiceNow {
	return &ServiceNow{conf: conf, proc: proc, tags: map[string]entry.EntryTag{}, source: net.ParseIP("127.0.0.1"), now: time.Now}
}

func (s *ServiceNow) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	datasets, err := s.conf.Datasets()
	if err != nil {
		return nil, err
	}
	client := NewClient(s.conf.Instance, s.conf.Secret_File, time.Duration(s.conf.Timeout)*time.Second, s.conf.MaxRetries(), s.conf.Requests_Per_Minute, nil)
	var errs []error
	for _, d := range datasets {
		if err := s.collect(ctx, rt, client, d); err != nil {
			if s.conf.Skip_Unavailable && IsUnavailable(err) {
				rt.Warn("ServiceNow dataset unavailable for current product/ACL", log.KV("dataset", d.Name), log.KVErr(err))
				continue
			}
			errs = append(errs, fmt.Errorf("%s: %w", d.Name, err))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return s.conf.ContinueAfterInterval(), nil
}

func (s *ServiceNow) collect(ctx context.Context, rt hosted.Runtime, client *Client, d Dataset) error {
	key := s.conf.StateNamespace() + "/" + d.Name
	fallbacks := make([]string, 0, len(s.conf.FallbackStateNamespaces()))
	for _, namespace := range s.conf.FallbackStateNamespaces() {
		fallbacks = append(fallbacks, namespace+"/"+d.Name)
	}
	state, copied, err := loadCursorWithFallback(rt, key, fallbacks...)
	if err != nil {
		return err
	}
	now := s.now().UTC().Truncate(time.Second)
	if state.Checkpoint.IsZero() {
		state.Checkpoint = now.Add(-time.Duration(s.conf.Lookback) * time.Hour)
	}
	if state.WindowEnd.IsZero() {
		state.WindowEnd = now
	}
	if state.Seen == nil {
		state.Seen = map[string]time.Time{}
	}
	if state.Hashes == nil {
		state.Hashes = map[string][sha256.Size]byte{}
	}
	// Table API keyset pagination always starts at offset zero. Replaying the
	// fixed window from its beginning is safe because per-record hashes suppress
	// accepted records.
	if d.REST == nil && state.Offset != 0 {
		state.Offset = 0
		copied = true
	}
	if copied {
		if err := saveCursor(rt, key, state); err != nil {
			return err
		}
	}
	fetched, written := 0, 0
	for pageNo := 0; pageNo < s.conf.Max_Pages; pageNo++ {
		query := WindowQuery(d, state.Checkpoint, state.WindowEnd, s.conf.OverlapSeconds())
		offset := state.Offset
		if d.REST == nil {
			query = WindowQueryAfter(d, state.Checkpoint, state.WindowEnd, s.conf.OverlapSeconds(), state.PageTimestamp, state.PageID)
			offset = 0
		}
		page, err := client.FetchPageAfter(ctx, d, query, s.conf.Page_Size, offset, state.NextURL)
		if err != nil {
			// The Service Catalog API can publish a stale next link and then reject
			// the persisted terminal offset. Reset only that precise recoverable
			// condition; all other HTTP 400 responses remain hard failures.
			if d.REST != nil && d.REST.OffsetParameter != "" && state.Offset > 0 && fetched == 0 && IsIllegalParameters(err) {
				state.Offset = 0
				if saveErr := saveCursor(rt, key, state); saveErr != nil {
					return saveErr
				}
				rt.Warn("reset stale ServiceNow endpoint offset after terminal-page rejection", log.KV("dataset", d.Name))
				return nil
			}
			return err
		}
		pageHighWater := state.HighWater
		for _, record := range page.Records {
			fetched++
			if d.REST == nil && !validKeysetID(record.ID) {
				return fmt.Errorf("ServiceNow dataset %s record omitted a safe sys_id required for keyset pagination", d.Name)
			}
			recordTime := record.Timestamp
			if recordTime.IsZero() {
				if d.REST == nil {
					return fmt.Errorf("ServiceNow dataset %s record %s omitted or invalidated ordering timestamp %s", d.Name, record.ID, d.Timestamp)
				}
				recordTime = state.WindowEnd
			}
			prepared, err := PrepareRecord(s.conf, d, record.Raw)
			if err != nil {
				return err
			}
			payload := prepared.Data
			digest := sha256.Sum256(payload)
			observedAt := record.Timestamp
			if d.REST != nil {
				observedAt = state.WindowEnd
			} else if recordTime.After(pageHighWater) {
				pageHighWater = recordTime
			}
			state.Seen[record.ID] = observedAt
			if previous, ok := state.Hashes[record.ID]; ok && previous == digest {
				continue
			}
			tag, err := s.tag(rt, d)
			if err != nil {
				return err
			}
			ent := &entry.Entry{TS: entry.FromStandard(recordTime), SRC: s.source, Tag: tag, Data: payload}
			source, recordType, endpoint, apiVersion := Provenance(d)
			if err := attachIntrinsicProvenance(ent, provenanceMetadata{
				Vendor: "ServiceNow", Product: d.Product, Source: source, RecordType: recordType,
				Endpoint: endpoint, APIVersion: apiVersion,
			}); err != nil {
				return err
			}
			if err := AttachNormalizationIntrinsic(ent, prepared.Intrinsic); err != nil {
				return err
			}
			s.processMu.Lock()
			err = s.proc.ProcessContext(ent, ctx)
			s.processMu.Unlock()
			if err != nil {
				return err
			}
			state.Hashes[record.ID] = digest
			// Persist stable per-item deduplication after the strongest acceptance
			// boundary exposed by hosted.Process. The page high-water mark remains
			// uncommitted until every record on the page has succeeded.
			if err := saveCursor(rt, key, state); err != nil {
				return err
			}
			written++
		}
		state.HighWater = pageHighWater
		if page.HasNext {
			if d.REST != nil && d.REST.OffsetParameter == "" {
				state.NextURL = page.NextURL
			} else if d.REST != nil {
				state.Offset += len(page.Records)
			} else {
				last := page.Records[len(page.Records)-1]
				state.PageTimestamp = last.Timestamp
				state.PageID = last.ID
				state.Offset = 0
			}
		} else {
			if d.REST != nil {
				state.Checkpoint = state.WindowEnd
			} else if state.HighWater.After(state.Checkpoint) {
				state.Checkpoint = state.HighWater
			}
			state.WindowEnd = time.Time{}
			state.HighWater = time.Time{}
			state.PageTimestamp = time.Time{}
			state.PageID = ""
			state.Offset = 0
			state.NextURL = ""
		}
		if d.REST == nil {
			pruneSeen(state.Seen, state.Hashes, state.Checkpoint.Add(-time.Duration(s.conf.OverlapSeconds())*time.Second))
		} else if !page.HasNext {
			// REST profiles are full snapshots. Retain only records observed in
			// the completed cycle so deleted objects do not grow state forever.
			pruneSeen(state.Seen, state.Hashes, state.Checkpoint)
		}
		if err := saveCursor(rt, key, state); err != nil {
			return err
		}
		if !page.HasNext {
			rt.Info("completed ServiceNow API poll", log.KV("dataset", d.Name), log.KV("fetched", fetched), log.KV("written", written), log.KV("normalization", s.conf.Normalization))
			return nil
		}
	}
	rt.Warn("ServiceNow API page limit reached; cursor retained for the next poll",
		log.KV("dataset", d.Name), log.KV("fetched", fetched), log.KV("written", written), log.KV("max_pages", s.conf.Max_Pages))
	return nil
}

// Provenance returns the stable source and API identity for a ServiceNow
// dataset without inspecting or rewriting the collected record bytes.
func Provenance(d Dataset) (source, recordType, endpoint, apiVersion string) {
	source = d.Name
	recordType = d.Table
	endpoint = "GET /api/now/table/" + d.Table
	apiVersion = "unversioned"
	if d.REST == nil {
		return
	}
	recordType = d.Name
	endpoint = "GET " + d.REST.Path
	for _, segment := range strings.Split(strings.Trim(d.REST.Path, "/"), "/") {
		if len(segment) > 1 && segment[0] == 'v' && segment[1] >= '0' && segment[1] <= '9' {
			apiVersion = segment
			break
		}
	}
	return
}

func WindowQuery(d Dataset, checkpoint, windowEnd time.Time, overlap int) string {
	parts := []string{}
	if strings.TrimSpace(d.Query) != "" {
		parts = append(parts, strings.Trim(d.Query, "^"))
	}
	start := checkpoint.Add(-time.Duration(overlap) * time.Second)
	parts = append(parts, fmt.Sprintf("%s>%s", d.Timestamp, start.Format("2006-01-02 15:04:05")), fmt.Sprintf("%s<=%s", d.Timestamp, windowEnd.Format("2006-01-02 15:04:05")), "ORDERBY"+d.Timestamp, "ORDERBYsys_id")
	return strings.Join(parts, "^")
}

// WindowQueryAfter returns a stable Table API keyset query over a fixed
// window. Ordering by timestamp and sys_id avoids the row-shift loss inherent
// to offsets when records are updated between page requests.
func WindowQueryAfter(d Dataset, checkpoint, windowEnd time.Time, overlap int, afterTimestamp time.Time, afterID string) string {
	if afterTimestamp.IsZero() && afterID == "" {
		return WindowQuery(d, checkpoint, windowEnd, overlap)
	}
	if afterTimestamp.IsZero() || !validKeysetID(afterID) {
		return WindowQuery(d, checkpoint, windowEnd, overlap)
	}
	filter := strings.Trim(strings.TrimSpace(d.Query), "^")
	timestamp := afterTimestamp.UTC().Format("2006-01-02 15:04:05")
	upper := fmt.Sprintf("%s<=%s", d.Timestamp, windowEnd.UTC().Format("2006-01-02 15:04:05"))
	branch := func(parts ...string) string {
		if filter != "" {
			parts = append([]string{filter}, parts...)
		}
		return strings.Join(parts, "^")
	}
	newer := branch(fmt.Sprintf("%s>%s", d.Timestamp, timestamp), upper)
	sameTimestamp := branch(fmt.Sprintf("%s=%s", d.Timestamp, timestamp), "sys_id>"+afterID, upper)
	return newer + "^NQ" + sameTimestamp + "^ORDERBY" + d.Timestamp + "^ORDERBYsys_id"
}

func validKeysetID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
func FormatRecord(conf *Config, d Dataset, raw []byte) ([]byte, error) {
	prepared, err := PrepareRecord(conf, d, raw)
	return prepared.Data, err
}
func (s *ServiceNow) tag(rt hosted.Runtime, d Dataset) (entry.EntryTag, error) {
	name := s.conf.Tag(d)
	s.tagMu.Lock()
	defer s.tagMu.Unlock()
	if tag, ok := s.tags[name]; ok {
		return tag, nil
	}
	tag, err := rt.NegotiateTag(name)
	if err == nil {
		s.tags[name] = tag
	}
	return tag, err
}
func loadCursor(rt hosted.Storage, key string) (cursorState, error) {
	state, _, err := loadCursorIfExists(rt, key)
	return state, err
}

func loadCursorIfExists(rt hosted.Storage, key string) (cursorState, bool, error) {
	var state cursorState
	raw, err := rt.Get(key)
	if errors.Is(err, storage.ErrStorageNotFound) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	if len(raw) > 0 {
		err = json.Unmarshal(raw, &state)
	}
	return state, true, err
}

func loadCursorWithFallback(rt hosted.Storage, key string, fallbacks ...string) (cursorState, bool, error) {
	state, exists, err := loadCursorIfExists(rt, key)
	if err != nil || exists {
		return state, false, err
	}
	for _, fallback := range fallbacks {
		state, exists, err = loadCursorIfExists(rt, fallback)
		if err != nil {
			return cursorState{}, false, err
		}
		if exists {
			return state, true, nil
		}
	}
	return cursorState{}, false, nil
}
func saveCursor(rt hosted.Storage, key string, state cursorState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return rt.Put(key, raw)
}
func pruneSeen(seen map[string]time.Time, hashes map[string][sha256.Size]byte, cutoff time.Time) {
	for id, t := range seen {
		if t.Before(cutoff) {
			delete(seen, id)
			delete(hashes, id)
		}
	}
}
