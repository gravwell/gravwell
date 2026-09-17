package claudecompliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Plugin struct {
	conf       *Config
	http       *http.Client
	now        func() time.Time
	wait       func(context.Context, time.Duration) error
	limiter    *rate.Limiter
	onRecord   func(Dataset, []byte) error
	syncIngest func(context.Context, time.Duration) error
}

// New binds the muxer's existing synchronization capability at build time.
// Runtime wrappers need not expose anything beyond hosted.Runtime.
func New(c *Config, tn hosted.TagNegotiator) (*Plugin, error) {
	s, ok := tn.(interface {
		SyncContext(context.Context, time.Duration) error
	})
	if !ok {
		return nil, errors.New("Compliance requires an ingest muxer with SyncContext")
	}
	return &Plugin{conf: c, http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now, wait: sleep, limiter: sharedLimiter(c.Scope_Identity, c.Requests_Per_Minute), syncIngest: s.SyncContext}, nil
}
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type state struct {
	Since    time.Time         `json:"since"`
	Manifest map[string]string `json:"manifest"`
}

func (p *Plugin) handleOne(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if p.syncIngest == nil {
		return nil, errors.New("Compliance ingest synchronization is not configured")
	}
	c := p.conf
	token, e := credential(c.Credential_File)
	if e != nil {
		return nil, e
	}
	st := state{Manifest: map[string]string{}}
	b, e := rt.Get(c.key())
	if e == nil {
		if e = json.Unmarshal(b, &st); e != nil {
			return nil, errors.New("invalid Compliance state")
		}
	} else if !errors.Is(e, storage.ErrStorageNotFound) {
		return nil, e
	}
	now := p.now().UTC()
	since := st.Since
	if since.IsZero() {
		if c.Start_Time != "" {
			since, _ = time.Parse(time.RFC3339, c.Start_Time)
		} else {
			since = now.Add(-time.Duration(c.Lookback) * time.Hour)
		}
	}
	q := url.Values{}
	d := c.dataset
	if d.Rows != "" {
		q.Set("limit", strconv.Itoa(c.Page_Size))
	}
	// Discovery must see unchanged parents: membership and child content need
	// not advance the parent's updated_at. Bound the full traversal with the
	// configured page/record limits, retaining queued children on any failure.
	fullParents := c.Follow_Children != "disabled" && (d.Name == "chats" || d.Name == "projects" || d.Name == "local-sessions")
	if d.Window != "" && !fullParents {
		q.Set(d.Window+".gte", since.Format(time.RFC3339Nano))
		if d.Name != "local-sessions" {
			q.Set(d.Window+".lte", now.Format(time.RFC3339Nano))
		}
	}
	if d.Name == "activities" || strings.HasSuffix(d.Name, "messages") {
		q.Set("order", "asc")
	}
	if d.Name == "chats" {
		q.Set("order_by", "updated_at")
	}
	tag, e := rt.NegotiateTag(c.Tag_Name)
	if e != nil {
		return nil, e
	}
	limiter := p.limiter
	nextState := state{Since: now.Add(-time.Duration(c.Overlap_Seconds) * time.Second), Manifest: map[string]string{}}
	seen := map[string]bool{}
	count := 0
	for page := 0; page < c.Max_Pages; page++ {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		requestKey := q.Encode()
		if seen[requestKey] {
			return nil, errors.New("Compliance returned a repeated cursor")
		}
		seen[requestKey] = true
		body, e := p.request(ctx, token, q, limiter)
		if e != nil {
			return nil, e
		}
		var envelope map[string]json.RawMessage
		if e = json.Unmarshal(body, &envelope); e != nil || envelope == nil {
			return nil, errors.New("Compliance response is not an object")
		}
		if raw := envelope["error"]; len(raw) > 0 && string(raw) != "null" {
			return nil, errors.New("Compliance returned an application error")
		}
		rows := []json.RawMessage{body}
		if d.Rows != "" {
			raw, ok := envelope[d.Rows]
			if !ok || string(raw) == "null" || json.Unmarshal(raw, &rows) != nil {
				return nil, fmt.Errorf("Compliance response missing %s array", d.Rows)
			}
		}
		for _, raw := range rows {
			count++
			if count > 100000 {
				return nil, errors.New("Compliance scan exceeds 100000 records; narrow collection window")
			}
			compact, e := compactObject(raw, c.Max_Entry_Bytes)
			if e != nil {
				return nil, e
			}
			h := sha256.Sum256(compact)
			digest := hex.EncodeToString(h[:])
			key := identity(compact, digest)
			nextState.Manifest[key] = digest
			if st.Manifest[key] == digest {
				if p.onRecord != nil {
					if e = p.onRecord(d, compact); e != nil {
						return nil, e
					}
				}
				continue
			}
			ent := entry.Entry{TS: entry.FromStandard(sourceTime(compact, now)), Tag: tag, Data: compact}
			for _, kv := range [][2]string{{"_vendor", "Anthropic"}, {"_product", "Claude Enterprise Compliance"}, {"_source", d.Name}, {"_recordType", d.Name}, {"_endpoint", "/v1/compliance" + d.Path}, {"_apiVersion", "2023-06-01"}, {"_parent", strings.Join(c.Parameter, ",")}} {
				if e = ent.AddEnumeratedValueEx(kv[0], kv[1]); e != nil {
					return nil, e
				}
			}
			// Session/chat envelope context is retained intrinsically for each message.
			if raw := envelope["session"]; d.Rows != "" && len(raw) > 0 && string(raw) != "null" {
				v, e := compactObject(raw, c.Max_Entry_Bytes)
				if e != nil {
					return nil, e
				}
				if e = ent.AddEnumeratedValueEx("_session", string(v)); e != nil {
					return nil, e
				}
			}
			if e = rt.Write(ent); e != nil {
				return nil, e
			}
			if p.onRecord != nil {
				if e = p.onRecord(d, compact); e != nil {
					return nil, e
				}
			}
		}
		next, more, e := continuation(envelope, d)
		if e != nil {
			return nil, e
		}
		if !more {
			// SyncContext is the upstream muxer barrier, not proof that every
			// cached entry has reached a backend. Keep cache and state together.
			if e = p.syncIngest(ctx, 2*time.Minute); e != nil {
				return nil, fmt.Errorf("Compliance ingest synchronization: %w", e)
			}
			if e = ctx.Err(); e != nil {
				return nil, e
			}
			b, e = json.Marshal(nextState)
			if e != nil {
				return nil, e
			}
			if e = rt.Put(c.key(), b); e != nil {
				return nil, e
			}
			return c.ContinueAfterInterval(), nil
		}
		q.Set(d.Cursor, next)
	}
	return nil, errors.New("Compliance Max-Pages reached; state retained for replay")
}
func continuation(env map[string]json.RawMessage, d Dataset) (string, bool, error) {
	if d.Cursor == "" {
		return "", false, nil
	}
	var has bool
	if b, ok := env["has_more"]; ok {
		if json.Unmarshal(b, &has) != nil {
			return "", false, errors.New("invalid has_more")
		}
	}
	field := "next_page"
	if d.Cursor == "after_id" {
		field = "last_id"
		if _, ok := env["has_more"]; !ok {
			return "", false, errors.New("missing has_more")
		}
		if !has {
			return "", false, nil
		}
	}
	var next string
	if b, ok := env[field]; ok && string(b) != "null" {
		if json.Unmarshal(b, &next) != nil {
			return "", false, errors.New("invalid pagination token")
		}
	}
	if has && next == "" {
		return "", false, errors.New("continuing response has no next token")
	}
	return next, next != "", nil
}
func (p *Plugin) request(ctx context.Context, token string, q url.Values, limiter *rate.Limiter) ([]byte, error) {
	for attempt := 0; attempt <= p.conf.Max_Retries; attempt++ {
		if e := limiter.Wait(ctx); e != nil {
			return nil, e
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com"+p.conf.path+"?"+q.Encode(), nil)
		if e != nil {
			return nil, errors.New("invalid Compliance request")
		}
		req.Header.Set("x-api-key", token)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Accept", "application/json")
		resp, e := p.http.Do(req)
		if e != nil {
			return nil, errors.New("Compliance transport failed")
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, int64(p.conf.Max_Response_Bytes)+1))
		resp.Body.Close()
		if e != nil {
			return nil, errors.New("incomplete Compliance response")
		}
		if len(body) > p.conf.Max_Response_Bytes {
			return nil, errors.New("Compliance response exceeds size limit")
		}
		if resp.StatusCode == 200 {
			return body, nil
		}
		retry := (resp.StatusCode == 429 || resp.StatusCode >= 500) && resp.Header.Get("x-should-retry") != "false"
		if !retry || attempt == p.conf.Max_Retries {
			return nil, fmt.Errorf("Compliance HTTP %d; state retained", resp.StatusCode)
		}
		delay := time.Duration(min(1<<attempt, 60)) * time.Second
		if seconds, e := strconv.ParseUint(resp.Header.Get("retry-after"), 10, 32); e == nil {
			delay = max(delay, time.Duration(seconds)*time.Second)
		} else if when, e := http.ParseTime(resp.Header.Get("retry-after")); e == nil {
			delay = max(delay, time.Until(when))
		}
		if e = p.wait(ctx, delay); e != nil {
			return nil, e
		}
	}
	return nil, errors.New("Compliance retries exhausted")
}
func compactObject(raw []byte, maxBytes int) ([]byte, error) {
	s := bytes.TrimSpace(raw)
	if len(s) == 0 || s[0] != '{' {
		return nil, errors.New("Compliance record is not an object")
	}
	var b bytes.Buffer
	if e := json.Compact(&b, s); e != nil {
		return nil, errors.New("invalid Compliance JSON record")
	}
	if b.Len() > maxBytes {
		return nil, errors.New("Compliance entry exceeds size limit")
	}
	return b.Bytes(), nil
}
func identity(raw []byte, fallback string) string {
	var v map[string]json.RawMessage
	_ = json.Unmarshal(raw, &v)
	for _, k := range []string{"id", "uuid"} {
		var s string
		if json.Unmarshal(v[k], &s) == nil && s != "" {
			return k + ":" + s
		}
	}
	return "sha256:" + fallback
}
func sourceTime(raw []byte, fallback time.Time) time.Time {
	var v map[string]json.RawMessage
	_ = json.Unmarshal(raw, &v)
	for _, k := range []string{"updated_at", "created_at", "timestamp"} {
		var s string
		if json.Unmarshal(v[k], &s) == nil {
			if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
				return t.UTC()
			}
		}
	}
	return fallback
}
