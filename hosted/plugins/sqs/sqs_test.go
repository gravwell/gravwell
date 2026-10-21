package sqs

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

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
	rt := hosted.NewMock(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Errorf("expected ContinueNow, got %v", cont)
	}
	entries := rt.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries written, got %d", len(entries))
	}
	if entries[0].TS != entry.FromStandard(ts) {
		t.Errorf("wrong TS: %v", entries[0].TS)
	}
	if string(entries[0].Data) != "body-1" {
		t.Errorf("wrong data: %q", entries[0].Data)
	}
	wantTag, err := rt.NegotiateTag(q.conf.ResolveTag(entry.DefaultTagName))
	if err != nil {
		t.Fatalf("unexpected error negotiating tag: %v", err)
	}
	for i, e := range entries {
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
	rt := hosted.NewMock(t.Context())
	rt.WriteErrs = 1 // first write fails

	if _, err := q.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries := rt.Entries(); len(entries) != 1 {
		t.Fatalf("expected 1 entry written, got %d", len(entries))
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
	rt := hosted.NewMock(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Errorf("expected ContinueNow, got %v", cont)
	}

	entries := rt.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry written, got %d", len(entries))
	}
	if string(entries[0].Data) != "body-2" {
		t.Errorf("wrong data written: %q", entries[0].Data)
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
	rt := hosted.NewMock(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if cont != nil {
		t.Errorf("expected nil continuation, got %v", cont)
	}
	if len(rt.Entries()) != 0 || len(client.deletedBatches) != 0 {
		t.Error("expected no writes or deletes")
	}
}

func TestQueue_Handle_GetMessagesCanceled(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		getMessages: func(_ context.Context) ([]types.Message, error) { return nil, context.Canceled },
	}
	q := &Queue{conf: &Config{}, client: client}
	rt := hosted.NewMock(t.Context())

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
	rt := hosted.NewMock(t.Context())

	cont, err := q.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Errorf("expected ContinueNow, got %v", cont)
	}
	if entries := rt.Entries(); len(entries) != 1 {
		t.Fatalf("expected 1 entry written, got %d", len(entries))
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
	rt := hosted.NewMock(t.Context())

	if _, err := q.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entries := rt.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry written, got %d", len(entries))
	}
	got := entries[0].TS.StandardTime()
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
	rt := hosted.NewMock(t.Context())
	rt.WriteErrs = len(msgs) // every write in the batch fails

	cont, err := q.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Errorf("expected ContinueNow, got %v", cont)
	}
	if entries := rt.Entries(); len(entries) != 0 {
		t.Errorf("expected no entries written, got %d", len(entries))
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
	rt := hosted.NewMock(t.Context())
	rt.NegotiateErr = wantErr

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
	if len(rt.Entries()) != 0 {
		t.Errorf("expected no entries written, got %d", len(rt.Entries()))
	}
}
