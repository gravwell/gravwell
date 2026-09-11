package main

import (
	"context"
	"testing"
	"time"

	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime is a shared test anchor time.
var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// fakeBatch is a test double for EventBatch that overflows after maxEvents
// events, standing in for the real SDK's byte-size-based limit.
type fakeBatch struct {
	maxEvents int
	events    []*eventhubs.EventData
}

func (b *fakeBatch) AddEventData(ed *eventhubs.EventData, _ *eventhubs.AddEventDataOptions) error {
	if len(b.events) >= b.maxEvents {
		return eventhubs.ErrEventDataTooLarge
	}
	b.events = append(b.events, ed)
	return nil
}

func (b *fakeBatch) NumEvents() int32 {
	return int32(len(b.events))
}

// mockEventHubsClient is a test double for EventHubsAPI.
type mockEventHubsClient struct {
	maxEvents int // events per batch before AddEventData reports ErrEventDataTooLarge
	newErr    error
	sendErr   error

	newCalls  int
	sentBatch []int32 // NumEvents() of each batch passed to SendEventDataBatch
}

func (m *mockEventHubsClient) NewEventDataBatch(_ context.Context, _ *eventhubs.EventDataBatchOptions) (EventBatch, error) {
	m.newCalls++
	if m.newErr != nil {
		return nil, m.newErr
	}
	return &fakeBatch{maxEvents: m.maxEvents}, nil
}

func (m *mockEventHubsClient) SendEventDataBatch(_ context.Context, batch EventBatch, _ *eventhubs.SendEventDataBatchOptions) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentBatch = append(m.sentBatch, batch.NumEvents())
	return nil
}

func TestEventHubsPublisher_Publish(t *testing.T) {
	mock := &mockEventHubsClient{maxEvents: 100}
	pub := NewEventHubsPublisher(mock, false, t.Logf)

	events := GenerateEventsFrom(5, fixedTime)
	result, err := pub.Publish(context.Background(), "test-hub", events)

	require.NoError(t, err)
	assert.Equal(t, 5, result.Sent)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, "test-hub", result.Resource)
	assert.Equal(t, []int32{5}, mock.sentBatch)
}

func TestEventHubsPublisher_PublishEmpty(t *testing.T) {
	mock := &mockEventHubsClient{maxEvents: 100}
	pub := NewEventHubsPublisher(mock, false, t.Logf)

	result, err := pub.Publish(context.Background(), "test-hub", nil)

	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Equal(t, 0, mock.newCalls)
}

func TestEventHubsPublisher_MultipleBatches(t *testing.T) {
	mock := &mockEventHubsClient{maxEvents: 2}
	pub := NewEventHubsPublisher(mock, false, t.Logf)

	events := GenerateEventsFrom(5, fixedTime)
	result, err := pub.Publish(context.Background(), "test-hub", events)

	require.NoError(t, err)
	assert.Equal(t, 5, result.Sent)
	assert.Equal(t, 0, result.Failed)
	// 5 events with a 2-event batch limit -> batches of 2, 2, 1.
	assert.Equal(t, []int32{2, 2, 1}, mock.sentBatch)
}

func TestEventHubsPublisher_NewBatchError(t *testing.T) {
	mock := &mockEventHubsClient{newErr: assert.AnError}
	pub := NewEventHubsPublisher(mock, false, t.Logf)

	events := GenerateEventsFrom(3, fixedTime)
	result, err := pub.Publish(context.Background(), "test-hub", events)

	require.Error(t, err)
	assert.Equal(t, 0, result.Sent)
}

func TestEventHubsPublisher_SendError(t *testing.T) {
	mock := &mockEventHubsClient{maxEvents: 100, sendErr: assert.AnError}
	pub := NewEventHubsPublisher(mock, false, t.Logf)

	events := GenerateEventsFrom(3, fixedTime)
	result, err := pub.Publish(context.Background(), "test-hub", events)

	require.Error(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Equal(t, 3, result.Failed)
}

func TestEventHubsPublisher_PublishVerbose(t *testing.T) {
	mock := &mockEventHubsClient{maxEvents: 2}
	var count int
	logf := func(format string, args ...any) { count++ }
	pub := NewEventHubsPublisher(mock, true, logf)

	events := GenerateEventsFrom(5, fixedTime)
	_, err := pub.Publish(context.Background(), "test-hub", events)

	require.NoError(t, err)
	// One verbose log line per batch sent (2, 2, 1 -> 3 batches).
	assert.Equal(t, 3, count)
}
