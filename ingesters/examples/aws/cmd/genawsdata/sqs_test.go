package main

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSQSClient struct {
	calls int
	err   error
}

func (m *mockSQSClient) SendMessage(_ context.Context, _ *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	m.calls++
	return &sqs.SendMessageOutput{}, m.err
}

func TestSQSPublisher_Publish(t *testing.T) {
	mock := &mockSQSClient{}
	pub := NewSQSPublisher(mock, false, t.Logf)

	events := GenerateEventsFrom(5, fixedTime)
	result, err := pub.Publish(context.Background(), "http://localhost:9324/queue/test", events)

	require.NoError(t, err)
	assert.Equal(t, 5, result.Sent)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, "SQS", result.Service)
	assert.Equal(t, 5, mock.calls) // one SendMessage per event
}

func TestSQSPublisher_PublishEmpty(t *testing.T) {
	mock := &mockSQSClient{}
	pub := NewSQSPublisher(mock, false, t.Logf)

	result, err := pub.Publish(context.Background(), "http://localhost:9324/queue/test", nil)

	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Equal(t, 0, mock.calls)
}

func TestSQSPublisher_PublishAllFail(t *testing.T) {
	mock := &mockSQSClient{err: assert.AnError}
	pub := NewSQSPublisher(mock, false, t.Logf)

	events := GenerateEventsFrom(3, fixedTime)
	result, err := pub.Publish(context.Background(), "http://localhost:9324/queue/test", events)

	require.Error(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Equal(t, 3, result.Failed)
}

func TestSQSPublisher_PublishVerbose(t *testing.T) {
	mock := &mockSQSClient{}
	var count int
	logf := func(format string, args ...any) { count++ }
	pub := NewSQSPublisher(mock, true, logf)

	events := GenerateEventsFrom(3, fixedTime)
	_, err := pub.Publish(context.Background(), "http://localhost:9324/queue/test", events)

	require.NoError(t, err)
	assert.Equal(t, 3, count) // one log per message
}
