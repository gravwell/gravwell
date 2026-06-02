package main

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime is a shared test anchor time.
var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type mockKinesisClient struct {
	calls int
	err   error
}

func (m *mockKinesisClient) PutRecord(_ context.Context, _ *kinesis.PutRecordInput, _ ...func(*kinesis.Options)) (*kinesis.PutRecordOutput, error) {
	m.calls++
	return &kinesis.PutRecordOutput{}, m.err
}

func TestKinesisPublisher_Publish(t *testing.T) {
	mock := &mockKinesisClient{}
	pub := NewKinesisPublisher(mock, false, t.Logf)

	events := GenerateEventsFrom(5, fixedTime)
	result, err := pub.Publish(context.Background(), "test-stream", events)

	require.NoError(t, err)
	assert.Equal(t, 5, result.Sent)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, "Kinesis", result.Service)
	assert.Equal(t, "test-stream", result.Resource)
	assert.Equal(t, 5, mock.calls)
}

func TestKinesisPublisher_PublishEmpty(t *testing.T) {
	mock := &mockKinesisClient{}
	pub := NewKinesisPublisher(mock, false, t.Logf)

	result, err := pub.Publish(context.Background(), "test-stream", nil)

	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Equal(t, 0, mock.calls)
}

func TestKinesisPublisher_PublishAllFail(t *testing.T) {
	mock := &mockKinesisClient{err: assert.AnError}
	pub := NewKinesisPublisher(mock, false, t.Logf)

	events := GenerateEventsFrom(3, fixedTime)
	result, err := pub.Publish(context.Background(), "test-stream", events)

	require.Error(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Equal(t, 3, result.Failed)
}

func TestKinesisPublisher_PublishVerbose(t *testing.T) {
	mock := &mockKinesisClient{}
	var count int
	logf := func(format string, args ...any) { count++ }
	pub := NewKinesisPublisher(mock, true, logf)

	events := GenerateEventsFrom(3, fixedTime)
	_, err := pub.Publish(context.Background(), "test-stream", events)

	require.NoError(t, err)
	assert.Equal(t, 3, count)
}
