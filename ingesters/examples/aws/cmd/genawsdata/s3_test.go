package main

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockS3Client struct {
	calls   int
	lastKey string
	err     error
}

func (m *mockS3Client) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	m.calls++
	if input.Key != nil {
		m.lastKey = *input.Key
	}
	return &s3.PutObjectOutput{}, m.err
}

func TestS3Publisher_Publish(t *testing.T) {
	mock := &mockS3Client{}
	pub := NewS3Publisher(mock, false, t.Logf)

	events := GenerateEventsFrom(5, fixedTime)
	result, err := pub.Publish(context.Background(), "test-bucket", events)

	require.NoError(t, err)
	assert.Equal(t, 5, result.Sent)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, "S3", result.Service)
	assert.Equal(t, "test-bucket", result.Resource)
	assert.Equal(t, 1, mock.calls) // single PutObject call
	assert.Contains(t, mock.lastKey, "genawsdata/")
}

func TestS3Publisher_PublishEmpty(t *testing.T) {
	mock := &mockS3Client{}
	pub := NewS3Publisher(mock, false, t.Logf)

	result, err := pub.Publish(context.Background(), "test-bucket", nil)

	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Equal(t, 0, mock.calls)
}

func TestS3Publisher_PublishError(t *testing.T) {
	mock := &mockS3Client{err: assert.AnError}
	pub := NewS3Publisher(mock, false, t.Logf)

	events := GenerateEventsFrom(3, fixedTime)
	result, err := pub.Publish(context.Background(), "test-bucket", events)

	require.Error(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.NotNil(t, result.Err)
}

func TestS3Publisher_PublishVerbose(t *testing.T) {
	mock := &mockS3Client{}
	var logged bool
	logf := func(format string, args ...any) { logged = true }
	pub := NewS3Publisher(mock, true, logf)

	events := GenerateEventsFrom(1, fixedTime)
	_, err := pub.Publish(context.Background(), "test-bucket", events)

	require.NoError(t, err)
	assert.True(t, logged)
}
