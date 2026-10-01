// Package restpoller implements the Slack Hosted Runner's generic REST polling engine.
package restpoller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type Plugin struct {
	conf              *Config
	httpClientFactory func() httpClient
	tagMu             sync.Mutex
	tags              map[string]entry.EntryTag
}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

var _ hosted.Job = (*Plugin)(nil)

func New(conf *Config) *Plugin {
	return &Plugin{conf: conf, tags: make(map[string]entry.EntryTag)}
}

type datasetState struct {
	Cursor        string            `json:"cursor,omitempty"`
	Manifest      map[string]string `json:"manifest,omitempty"`
	Watermark     int64             `json:"watermark,omitempty"`
	PendingOldest int64             `json:"pending_oldest,omitempty"`
}

func (p *Plugin) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	credential, err := p.conf.Credential()
	if err != nil {
		return nil, fmt.Errorf("reload Credential-File: %w", err)
	}
	suiteQL, err := p.conf.SuiteQLBody()
	if err != nil {
		return nil, fmt.Errorf("reload SuiteQL-Query-File: %w", err)
	}
	client, err := NewClient(p.conf.Base_URL, credential, p.conf.Requests_Per_Minute, p.conf.Page_Size, p.conf.Max_Pages, nil)
	if err != nil {
		return nil, err
	}
	for _, definition := range p.conf.Definitions() {
		if err := p.collect(ctx, rt, client, definition, suiteQL); err != nil {
			if !p.conf.Emit_Errors {
				return nil, err
			}
			if writeErr := p.writeError(rt, definition, err); writeErr != nil {
				return nil, errors.Join(err, writeErr)
			}
		}
	}
	return p.conf.ContinueAfterInterval(), nil
}

func (p *Plugin) collect(ctx context.Context, rt hosted.Runtime, client *Client, definition Definition, suiteQL string) error {
	barrier, ok := rt.(interface {
		SyncDelivered(context.Context, time.Duration) error
	})
	if !ok {
		return errors.New("runtime does not support acknowledged delivery")
	}
	state, err := loadState(rt, p.conf.stateKey(definition))
	if err != nil {
		return err
	}
	query := p.conf.Queries(definition.Name)
	var completedThrough int64
	if definition.Product == "slack" {
		oldest := time.Now().UTC().Add(-time.Duration(p.conf.Lookback) * time.Hour).Unix()
		// A prior attempt for this dataset may have fetched but not fully
		// delivered a page. Reuse the oldest bound it already committed to
		// instead of recomputing from the current wall clock, or a record
		// that was in that page but never written could age out of the
		// vendor's own oldest/latest window and be lost permanently.
		if state.PendingOldest != 0 {
			oldest = state.PendingOldest
		}
		if p.conf.Start_Time != "" {
			start, _ := time.Parse(time.RFC3339, p.conf.Start_Time)
			oldest = start.Unix()
		}
		if raw := query.Get("oldest"); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil {
				return errors.New("invalid oldest seconds")
			}
			oldest = v
		}
		oldest = max(oldest, state.Watermark)
		completedThrough = time.Now().UTC().Unix()
		if raw := query.Get("latest"); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil {
				return errors.New("invalid latest seconds")
			}
			completedThrough = min(completedThrough, v)
		}
		if oldest > completedThrough {
			return errors.New("oldest is later than latest")
		}
		if oldest != state.PendingOldest {
			state.PendingOldest = oldest
			if err := saveState(rt, p.conf.stateKey(definition), state); err != nil {
				return err
			}
		}
		query.Set("oldest", strconv.FormatInt(oldest, 10))
		query.Set("latest", strconv.FormatInt(completedThrough, 10))
	}
	for _, managed := range []string{definition.CursorQuery, definition.PageSizeQuery, definition.OffsetQuery} {
		if managed != "" && query.Has(managed) {
			return fmt.Errorf("Query parameter %s is managed by the collector", managed)
		}
	}
	if definition.StartQuery != "" && !query.Has(definition.StartQuery) && state.Cursor == "" {
		if start := p.conf.EffectiveStart(definition); start != "" {
			query.Set(definition.StartQuery, start)
		}
	}
	collection, err := client.Collect(ctx, definition, query, state.Cursor, suiteQL)
	if err != nil {
		return err
	}
	current := make(map[string]string, len(collection.Records))
	written := 0
	for _, raw := range collection.Records {
		compact, identity, timestamp, err := normalizeRecord(raw)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(compact)
		fingerprint := hex.EncodeToString(digest[:])
		// current[identity] still holds the prior loop iteration's value here (if any),
		// so this also catches a repeated identity within the same poll's collected records.
		previous, hadPrevious := current[identity]
		duplicate := !p.conf.Ingest_Unchanged && (previous == fingerprint || state.Manifest[identity] == fingerprint)
		current[identity] = fingerprint
		if duplicate {
			continue
		}
		tag, err := p.negotiateTag(rt, p.conf.Tag(definition))
		if err != nil {
			return err
		}
		if err := rt.Write(entry.Entry{
			TS: entry.FromStandard(timestamp), SRC: net.ParseIP("127.0.0.1"), Tag: tag, Data: compact,
		}); err != nil {
			// This record was not written, so it must not be marked delivered.
			// Restore whatever this identity's manifest entry was before this
			// iteration (an earlier, successfully written occurrence within the
			// same poll) rather than unconditionally deleting it, or a retry
			// would re-write that earlier occurrence too.
			if hadPrevious {
				current[identity] = previous
			} else {
				delete(current, identity)
			}
			if written == 0 {
				// Nothing from this page was actually delivered yet; leave
				// state untouched so a retry replays the whole page as before.
				return err
			}
			if syncErr := barrier.SyncDelivered(ctx, 30*time.Second); syncErr != nil {
				return errors.Join(err, syncErr)
			}
			state.Manifest = current
			if saveErr := saveState(rt, p.conf.stateKey(definition), state); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			return err
		}
		written++
	}
	if err := barrier.SyncDelivered(ctx, 30*time.Second); err != nil {
		return fmt.Errorf("delivery before checkpoint: %w", err)
	}
	state.Manifest = current
	if completedThrough > 0 {
		state.Watermark = completedThrough - 300
		state.PendingOldest = 0
	}
	if collection.PersistCursor != "" {
		state.Cursor = collection.PersistCursor
	}
	if err := saveState(rt, p.conf.stateKey(definition), state); err != nil {
		return err
	}
	rt.Info(fmt.Sprintf("completed %s/%s collection: records=%d written=%d tag=%s", definition.Product, definition.Name, len(collection.Records), written, p.conf.Tag(definition)))
	return nil
}

func normalizeRecord(raw json.RawMessage) ([]byte, string, time.Time, error) {
	if !json.Valid(raw) {
		return nil, "", time.Time{}, errors.New("vendor record is not valid JSON")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, "", time.Time{}, err
	}
	compact, err := json.Marshal(value)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return compact, recordIdentity(value, compact), recordTimestamp(value), nil
}

func recordIdentity(value any, compact []byte) string {
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"id", "uuid", "eventId", "event_id", "executionId", "workflowId", "email", "name"} {
			if candidate, exists := object[key]; exists {
				switch typed := candidate.(type) {
				case string:
					if typed != "" {
						return key + ":" + typed
					}
				case json.Number:
					return key + ":" + typed.String()
				}
			}
		}
	}
	digest := sha256.Sum256(compact)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func recordTimestamp(value any) time.Time {
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"effective_at", "published", "eventTime", "date_create", "created_at", "createdAt", "startedAt", "updated_at", "updatedAt", "timestamp", "time"} {
			raw, ok := object[key]
			if !ok {
				continue
			}
			switch typed := raw.(type) {
			case string:
				if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
					return parsed.UTC()
				}
			case json.Number:
				if seconds, err := typed.Int64(); err == nil {
					return time.Unix(seconds, 0).UTC()
				}
			}
		}
	}
	return time.Now().UTC()
}

func loadState(rt hosted.Storage, key string) (datasetState, error) {
	state := datasetState{Manifest: make(map[string]string)}
	body, err := rt.Get(key)
	if err != nil {
		if errors.Is(err, storage.ErrStorageNotFound) {
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return state, err
	}
	if state.Manifest == nil {
		state.Manifest = make(map[string]string)
	}
	return state, nil
}

func saveState(rt hosted.Storage, key string, state datasetState) error {
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return rt.Put(key, body)
}

func (p *Plugin) negotiateTag(rt hosted.Runtime, name string) (entry.EntryTag, error) {
	p.tagMu.Lock()
	defer p.tagMu.Unlock()
	if tag, ok := p.tags[name]; ok {
		return tag, nil
	}
	tag, err := rt.NegotiateTag(name)
	if err != nil {
		return 0, err
	}
	p.tags[name] = tag
	return tag, nil
}

func (p *Plugin) writeError(rt hosted.Runtime, definition Definition, collectionErr error) error {
	tag, err := p.negotiateTag(rt, p.conf.Error_Tag)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]any{
		"vendor": definition.Product, "dataset": definition.Name, "error": collectionErr.Error(), "timestamp": now.Format(time.RFC3339Nano),
	})
	return rt.Write(entry.Entry{TS: entry.FromStandard(now), SRC: net.ParseIP("127.0.0.1"), Tag: tag, Data: body})
}
