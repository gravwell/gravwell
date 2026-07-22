package sqs

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/crewjam/rfc5424"

	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type mockRuntime struct {
	mu           sync.Mutex
	entries      []entry.Entry
	store        map[string][]byte
	tags         map[string]entry.EntryTag
	nextTag      entry.EntryTag
	ctx          context.Context
	cancel       context.CancelFunc
	writeErrs    int   // number of subsequent Write calls that should fail
	negotiateErr error // if set, NegotiateTag returns this error
}

func newMockRuntime(ctx context.Context) *mockRuntime {
	ctx, cancel := context.WithCancel(ctx)
	return &mockRuntime{entries: []entry.Entry{}, store: map[string][]byte{}, tags: map[string]entry.EntryTag{}, ctx: ctx, cancel: cancel}
}

func (m *mockRuntime) Alive() bool              { return true }
func (m *mockRuntime) Context() context.Context { return m.ctx }
func (m *mockRuntime) Sleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return false
	case <-m.ctx.Done():
		return true
	}
}
func (m *mockRuntime) Debug(_ string, _ ...rfc5424.SDParam)    {}
func (m *mockRuntime) Info(_ string, _ ...rfc5424.SDParam)     {}
func (m *mockRuntime) Warn(_ string, _ ...rfc5424.SDParam)     {}
func (m *mockRuntime) Error(_ string, _ ...rfc5424.SDParam)    {}
func (m *mockRuntime) Critical(_ string, _ ...rfc5424.SDParam) {}
func (m *mockRuntime) Write(e entry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErrs > 0 {
		m.writeErrs--
		return errors.New("write failed")
	}
	m.entries = append(m.entries, e)
	return nil
}
func (m *mockRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.negotiateErr != nil {
		return 0, m.negotiateErr
	}
	if t, ok := m.tags[name]; ok {
		return t, nil
	}
	m.nextTag++
	m.tags[name] = m.nextTag
	return m.nextTag, nil
}
func (m *mockRuntime) Get(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return v, nil
}
func (m *mockRuntime) Put(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
	return nil
}
func (m *mockRuntime) GetString(key string) (string, error) {
	v, err := m.Get(key)
	return string(v), err
}
func (m *mockRuntime) PutString(key, value string) error { return m.Put(key, []byte(value)) }
func (m *mockRuntime) GetInt64(_ string) (int64, error)  { return 0, storage.ErrStorageNotFound }
func (m *mockRuntime) PutInt64(_ string, _ int64) error  { return nil }
func (m *mockRuntime) GetTime(key string) (time.Time, error) {
	v, err := m.GetString(key)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, v)
}
func (m *mockRuntime) PutTime(key string, value time.Time) error {
	return m.PutString(key, value.Format(time.RFC3339Nano))
}

type fakeClient struct {
	getMessages     func(ctx context.Context) ([]types.Message, error)
	deleteMessages  func(ctx context.Context, m []types.Message) error
	deletedBatches  [][]types.Message
	getMessageCalls int
}

func (f *fakeClient) GetMessagesOnce(ctx context.Context) ([]types.Message, error) {
	f.getMessageCalls++
	return f.getMessages(ctx)
}

func (f *fakeClient) DeleteMessages(ctx context.Context, m []types.Message) error {
	f.deletedBatches = append(f.deletedBatches, m)
	if f.deleteMessages != nil {
		return f.deleteMessages(ctx, m)
	}
	return nil
}

func newMessage(id, body, sentTimestamp string) types.Message {
	attrs := map[string]string{}
	if sentTimestamp != "" {
		attrs["SentTimestamp"] = sentTimestamp
	}
	return types.Message{
		MessageId:     new(id),
		ReceiptHandle: new("receipt-" + id),
		Body:          new(body),
		Attributes:    attrs,
	}
}

func TestQueue_Handle_WritesAndDeletesAll(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	msgs := []types.Message{
		newMessage("1", "body-1", strconv.FormatInt(ts.UnixMilli(), 10)),
		newMessage("2", "body-2", strconv.FormatInt(ts.UnixMilli(), 10)),
	}
	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) { return msgs, nil },
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := newMockRuntime(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Errorf("expected ContinueNow, got %v", cont)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("expected 2 entries written, got %d", len(rt.entries))
	}
	if rt.entries[0].TS != entry.FromStandard(ts) {
		t.Errorf("wrong TS: %v", rt.entries[0].TS)
	}
	if string(rt.entries[0].Data) != "body-1" {
		t.Errorf("wrong data: %q", rt.entries[0].Data)
	}
	wantTag, err := rt.NegotiateTag(q.conf.ResolveTag(entry.DefaultTagName))
	if err != nil {
		t.Fatalf("unexpected error negotiating tag: %v", err)
	}
	for i, e := range rt.entries {
		if e.Tag != wantTag {
			t.Errorf("entry %d: wrong tag: got %v, want %v", i, e.Tag, wantTag)
		}
	}
	if len(client.deletedBatches) != 1 || len(client.deletedBatches[0]) != 2 {
		t.Fatalf("expected a single delete batch of 2, got %v", client.deletedBatches)
	}
}

func TestQueue_Handle_WriteFailureExcludedFromDelete(t *testing.T) {
	t.Parallel()

	msgs := []types.Message{
		newMessage("1", "body-1", ""),
		newMessage("2", "body-2", ""),
	}
	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) { return msgs, nil },
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := newMockRuntime(t.Context())
	rt.writeErrs = 1 // first write fails

	if _, err := q.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("expected 1 entry written, got %d", len(rt.entries))
	}
	if len(client.deletedBatches) != 1 || len(client.deletedBatches[0]) != 1 {
		t.Fatalf("expected delete batch of 1, got %v", client.deletedBatches)
	}
	if *client.deletedBatches[0][0].MessageId != "2" {
		t.Errorf("expected message 2 to be deleted, got %v", *client.deletedBatches[0][0].MessageId)
	}
}

func TestQueue_Handle_NilBodySkippedNoPanic(t *testing.T) {
	t.Parallel()

	nilBody := types.Message{
		MessageId:     new("nil-body"),
		ReceiptHandle: new("receipt-nil-body"),
		Body:          nil,
		Attributes:    map[string]string{},
	}
	msgs := []types.Message{
		nilBody,
		newMessage("2", "body-2", ""),
	}
	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) { return msgs, nil },
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := newMockRuntime(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Errorf("expected ContinueNow, got %v", cont)
	}

	if len(rt.entries) != 1 {
		t.Fatalf("expected 1 entry written, got %d", len(rt.entries))
	}
	if string(rt.entries[0].Data) != "body-2" {
		t.Errorf("wrong data written: %q", rt.entries[0].Data)
	}

	if len(client.deletedBatches) != 1 || len(client.deletedBatches[0]) != 1 {
		t.Fatalf("expected delete batch of 1, got %v", client.deletedBatches)
	}
	if *client.deletedBatches[0][0].MessageId != "2" {
		t.Errorf("expected only message 2 to be deleted, got %v", *client.deletedBatches[0][0].MessageId)
	}
}

func TestQueue_Handle_GetMessagesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) { return nil, wantErr },
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := newMockRuntime(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if cont != nil {
		t.Errorf("expected nil continuation, got %v", cont)
	}
	if len(rt.entries) != 0 || len(client.deletedBatches) != 0 {
		t.Error("expected no writes or deletes")
	}
}

func TestQueue_Handle_GetMessagesCanceled(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) { return nil, context.Canceled },
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := newMockRuntime(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if cont != nil {
		t.Errorf("expected nil continuation, got %v", cont)
	}
}

func TestQueue_Handle_DeleteMessagesErrorIsNotFatal(t *testing.T) {
	t.Parallel()

	msgs := []types.Message{newMessage("1", "body-1", "")}
	client := &fakeClient{
		getMessages:    func(_ context.Context) ([]types.Message, error) { return msgs, nil },
		deleteMessages: func(_ context.Context, _ []types.Message) error { return errors.New("delete failed") },
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := newMockRuntime(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Errorf("expected ContinueNow, got %v", cont)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("expected 1 entry written, got %d", len(rt.entries))
	}
}

func TestQueue_Handle_IgnoreTimestamps(t *testing.T) {
	t.Parallel()

	oldTS := time.Now().Add(-24 * time.Hour)
	msgs := []types.Message{newMessage("1", "body-1", strconv.FormatInt(oldTS.UnixMilli(), 10))}
	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) { return msgs, nil },
	}
	q := &Queue{conf: &Config{Ignore_Timestamps: true}, client: client}
	rt := newMockRuntime(t.Context())

	if _, err := q.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("expected 1 entry written, got %d", len(rt.entries))
	}
	got := rt.entries[0].TS.StandardTime()
	if diff := time.Since(got); diff < 0 || diff > 5*time.Second {
		t.Errorf("expected TS close to now, got %v", got)
	}
}

func TestQueue_Handle_AllWritesFailNoDelete(t *testing.T) {
	t.Parallel()

	msgs := []types.Message{
		newMessage("1", "body-1", ""),
		newMessage("2", "body-2", ""),
	}
	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) { return msgs, nil },
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := newMockRuntime(t.Context())
	rt.writeErrs = len(msgs) // every write in the batch fails

	cont, err := q.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Errorf("expected ContinueNow, got %v", cont)
	}
	if len(rt.entries) != 0 {
		t.Errorf("expected no entries written, got %d", len(rt.entries))
	}
	if len(client.deletedBatches) != 0 {
		t.Errorf("expected DeleteMessages to never be called, got %v", client.deletedBatches)
	}
}

func TestQueue_Handle_NegotiateTagError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("negotiate boom")
	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) {
			return []types.Message{newMessage("1", "body-1", "")}, nil
		},
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := newMockRuntime(t.Context())
	rt.negotiateErr = wantErr

	cont, err := q.Handle(t.Context(), rt)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped %v, got %v", wantErr, err)
	}
	if cont != nil {
		t.Errorf("expected nil continuation, got %v", cont)
	}
	if client.getMessageCalls != 0 {
		t.Errorf("expected GetMessages to never be called, got %d calls", client.getMessageCalls)
	}
	if len(client.deletedBatches) != 0 {
		t.Errorf("expected DeleteMessages to never be called, got %v", client.deletedBatches)
	}
	if len(rt.entries) != 0 {
		t.Errorf("expected no entries written, got %d", len(rt.entries))
	}
}
