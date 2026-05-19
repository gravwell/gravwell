package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertRoutine_ExitsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	fetcher := &mockFetcher{
		listAlerts: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			t.Fatal("ListAlerts should not be called when context is already cancelled")
			return nil, nil
		},
	}

	proc := &mockProcessor{}
	cfg := baseRoutineCfg(ctx, fetcher, newMockTracker(), proc)

	runRoutine(alertRoutine, cfg)

	assert.True(t, proc.closed, "procset should be closed on exit")
}

func TestAlertRoutine_SkipsNilID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	tracker := newMockTracker()
	proc := &mockProcessor{}

	fetcher := &mockFetcher{
		listAlerts: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			cancel()
			// JSON object without an "id" field
			return []json.RawMessage{json.RawMessage(`{"title":"no id"}`)}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, tracker, proc)
	runRoutine(alertRoutine, cfg)

	assert.Equal(t, 0, proc.count(), "empty ID alert should not be ingested")
	assert.Empty(t, tracker.seen, "empty ID alert should not be recorded")
}

func TestAlertRoutine_SkipsSeenID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	tracker := newMockTracker()
	proc := &mockProcessor{}

	seenID := "already-seen-alert"
	require.NoError(t, tracker.RecordId(seenID, time.Now()))

	fetcher := &mockFetcher{
		listAlerts: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			cancel()
			return []json.RawMessage{newTestAlertJSON(seenID, time.Now())}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, tracker, proc)
	runRoutine(alertRoutine, cfg)

	assert.Equal(t, 0, proc.count(), "already seen alert should not be ingested")
}

func TestAlertRoutine_IngestsAndRecordsNewAlert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	tracker := newMockTracker()
	proc := &mockProcessor{}

	alertID := "new-alert-id"
	createdAt := time.Now().Add(-5 * time.Minute)

	fetcher := &mockFetcher{
		listAlerts: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			cancel()
			return []json.RawMessage{newTestAlertJSON(alertID, createdAt)}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, tracker, proc)
	runRoutine(alertRoutine, cfg)

	require.Equal(t, 1, proc.count(), "new alert should have been ingested")
	assert.True(t, tracker.IdExists(alertID), "%q should have been recorded in the tracker", alertID)

	ent := proc.entries[0]
	assert.NotEmpty(t, ent.Data, "ingested entry should have data")
	assert.WithinDuration(t, createdAt, ent.TS.StandardTime(), time.Second)
}

func TestAlertRoutine_IgnoreTimestamps(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	tracker := newMockTracker()
	proc := &mockProcessor{}

	alertID := "ts-ignored-alert"
	createdAt := time.Now().Add(-24 * time.Hour)

	fetcher := &mockFetcher{
		listAlerts: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			cancel()
			return []json.RawMessage{newTestAlertJSON(alertID, createdAt)}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, tracker, proc)
	cfg.ct.Ignore_Timestamps = true

	before := time.Now()
	runRoutine(alertRoutine, cfg)
	after := time.Now()

	require.Equal(t, 1, proc.count())
	ent := proc.entries[0]
	assert.WithinRange(t, ent.TS.StandardTime(), before, after)
}

func TestAlertRoutine_APIErrorExitsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	proc := &mockProcessor{}

	fetcher := &mockFetcher{
		listAlerts: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			cancel()
			return nil, errors.New("msgraph api unavailable")
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, newMockTracker(), proc)
	runRoutine(alertRoutine, cfg)

	assert.Equal(t, 0, proc.count(), "no entries should have been ingested due to API error")
	assert.True(t, proc.closed)
}

func TestAlertRoutine_DeduplicatesAcrossMultipleFetches(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	tracker := newMockTracker()
	proc := &mockProcessor{}

	alertID := "dedup-alert"
	calls := atomic.Int32{}

	fetcher := &mockFetcher{
		listAlerts: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			calls.Add(1)
			if calls.Load() >= 2 {
				cancel()
			}
			return []json.RawMessage{newTestAlertJSON(alertID, time.Now())}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, tracker, proc)
	runRoutine(alertRoutine, cfg)

	assert.Equal(t, 1, proc.count(), "duplicate alert should only be ingested once")
	assert.GreaterOrEqual(t, calls.Load(), int32(2), "fetcher should have been called at least twice")
}

func TestSecureScoreRoutine_ExitsOnCancelContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	proc := &mockProcessor{}

	fetcher := &mockFetcher{
		listSecureScores: func(_ context.Context) ([]json.RawMessage, error) {
			cancel()
			return []json.RawMessage{json.RawMessage(`{"title":"no id"}`)}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, newMockTracker(), proc)
	runRoutine(secureScoreRoutine, cfg)

	assert.Equal(t, 0, proc.count())
}

func TestSecureScoreRoutine_SkipsSeenID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	tracker := newMockTracker()
	proc := &mockProcessor{}

	seenID := "seen-score"
	require.NoError(t, tracker.RecordId(seenID, time.Now()))

	fetcher := &mockFetcher{
		listSecureScores: func(_ context.Context) ([]json.RawMessage, error) {
			cancel()
			return []json.RawMessage{newTestScoreJSON(seenID, time.Now())}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, tracker, proc)
	runRoutine(secureScoreRoutine, cfg)

	assert.Equal(t, 0, proc.count())
}

func TestSecureScoreRoutine_IngestsAndRecordsNewScore(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	tracker := newMockTracker()
	proc := &mockProcessor{}

	scoreID := "new-score-id"
	createdAt := time.Now().Add(-1 * time.Hour)

	fetcher := &mockFetcher{
		listSecureScores: func(_ context.Context) ([]json.RawMessage, error) {
			cancel()
			return []json.RawMessage{newTestScoreJSON(scoreID, createdAt)}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, tracker, proc)
	runRoutine(secureScoreRoutine, cfg)

	require.Equal(t, 1, proc.count())
	assert.True(t, tracker.IdExists(scoreID))

	ent := proc.entries[0]
	assert.NotEmpty(t, ent.Data)
	assert.WithinDuration(t, createdAt, ent.TS.StandardTime(), time.Second)
}

func TestSecureScoreRoutine_APIErrorExitsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	proc := &mockProcessor{}

	fetcher := &mockFetcher{
		listSecureScores: func(_ context.Context) ([]json.RawMessage, error) {
			cancel()
			return nil, errors.New("msgraph api unavailable")
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, newMockTracker(), proc)
	runRoutine(secureScoreRoutine, cfg)

	assert.Equal(t, 0, proc.count())
	assert.True(t, proc.closed)
}

func TestSecureScoreProfileRoutine_ExitsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	fetcher := &mockFetcher{
		listSecureScoreControlProfiles: func(_ context.Context) ([]json.RawMessage, error) {
			t.Fatal("should not be called when context is already cancelled")
			return nil, nil
		},
	}

	proc := &mockProcessor{}
	cfg := baseRoutineCfg(ctx, fetcher, newMockTracker(), proc)
	runRoutine(secureScoreProfileRoutine, cfg)

	assert.True(t, proc.closed)
}

func TestSecureScoreProfileRoutine_SkipsNilID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	proc := &mockProcessor{}

	fetcher := &mockFetcher{
		listSecureScoreControlProfiles: func(_ context.Context) ([]json.RawMessage, error) {
			cancel()
			return []json.RawMessage{json.RawMessage(`{"title":"no id"}`)}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, newMockTracker(), proc)
	runRoutine(secureScoreProfileRoutine, cfg)

	assert.Equal(t, 0, proc.count())
}

func TestSecureScoreProfileRoutine_IngestsNewProfile(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	proc := &mockProcessor{}

	profileID := "profile-id"

	fetcher := &mockFetcher{
		listSecureScoreControlProfiles: func(_ context.Context) ([]json.RawMessage, error) {
			cancel()
			return []json.RawMessage{newTestProfileJSON(profileID)}, nil
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, newMockTracker(), proc)
	runRoutine(secureScoreProfileRoutine, cfg)

	require.Equal(t, 1, proc.count())
	assert.NotEmpty(t, proc.entries[0].Data)
}

func TestSecureScoreProfileRoutine_APIErrorExitsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	proc := &mockProcessor{}

	fetcher := &mockFetcher{
		listSecureScoreControlProfiles: func(_ context.Context) ([]json.RawMessage, error) {
			cancel()
			return nil, errors.New("msgraph api unavailable")
		},
	}

	cfg := baseRoutineCfg(ctx, fetcher, newMockTracker(), proc)
	runRoutine(secureScoreProfileRoutine, cfg)

	assert.Equal(t, 0, proc.count())
	assert.True(t, proc.closed)
}

type mockFetcher struct {
	listAlerts                     func(ctx context.Context, filter string) ([]json.RawMessage, error)
	listSecureScores               func(ctx context.Context) ([]json.RawMessage, error)
	listSecureScoreControlProfiles func(ctx context.Context) ([]json.RawMessage, error)
}

func (m *mockFetcher) ListAlerts(ctx context.Context, filter string) ([]json.RawMessage, error) {
	return m.listAlerts(ctx, filter)
}

func (m *mockFetcher) ListSecureScores(ctx context.Context) ([]json.RawMessage, error) {
	return m.listSecureScores(ctx)
}

func (m *mockFetcher) ListSecureScoreControlProfiles(ctx context.Context) ([]json.RawMessage, error) {
	return m.listSecureScoreControlProfiles(ctx)
}

type mockTracker struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	recordErr error
}

func newMockTracker() *mockTracker {
	return &mockTracker{seen: make(map[string]time.Time)}
}

func (m *mockTracker) IdExists(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.seen[id]
	return ok
}

func (m *mockTracker) RecordId(id string, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recordErr != nil {
		return m.recordErr
	}
	m.seen[id] = t
	return nil
}

type mockProcessor struct {
	mu       sync.Mutex
	entries  []*entry.Entry
	closed   bool
	closeErr error
}

func (m *mockProcessor) ProcessContext(ent *entry.Entry, _ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, ent)
	return nil
}

func (m *mockProcessor) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.closeErr
}

func (m *mockProcessor) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

func newTestAlertJSON(id string, createdAt time.Time) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"createdDateTime":%q,"title":"test"}`, id, createdAt.Format(time.RFC3339)))
}

func newTestScoreJSON(id string, createdAt time.Time) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"createdDateTime":%q,"currentScore":80}`, id, createdAt.Format(time.RFC3339)))
}

func newTestProfileJSON(id string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"title":"test profile"}`, id))
}

func baseRoutineCfg(ctx context.Context, fetcher msGraphFetcher, tracker stateTrackable, proc entryProcessor) routineCfg {
	return routineCfg{
		ct:          &contentType{Content_Type: "test"},
		cfg:         &cfgType{},
		graphClient: fetcher,
		ctx:         ctx,
		procset:     proc,
		tracker:     tracker,
		lg:          log.NewDiscardLogger(),
		src:         net.ParseIP("127.0.0.1"),
	}
}

func runRoutine(
	fn func(cfg routineCfg, errWait, successWait time.Duration),
	cfg routineCfg,
) {
	errWait := 100 * time.Millisecond
	successWait := 1 * time.Millisecond
	var wg sync.WaitGroup
	wg.Go(func() {
		fn(cfg, errWait, successWait)
	})
	wg.Wait()
}
