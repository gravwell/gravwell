package sqs_common

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSQS struct {
	receiveFunc func(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	deleteFunc  func(context.Context, *sqs.DeleteMessageBatchInput, ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error)
}

func (m *mockSQS) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return m.receiveFunc(ctx, params, optFns...)
}

func (m *mockSQS) DeleteMessageBatch(ctx context.Context, params *sqs.DeleteMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error) {
	return m.deleteFunc(ctx, params, optFns...)
}

func TestSQSListener(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		conf             *Config
		expectedEndpoint string
	}{
		{
			name: "standard aws",
			conf: &Config{
				Queue:       "https://sqs.us-east-1.amazonaws.com/12345/my-queue",
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("test-access-key-id", "test-secret-key", ""),
			},
			expectedEndpoint: "",
		},
		{
			name: "custom endpoint",
			conf: &Config{
				Queue:       "http://localhost:9324/000000000000/test-queue",
				Region:      "elasticmq",
				Endpoint:    "http://localhost:9324",
				Credentials: credentials.NewStaticCredentialsProvider("test-access-key-id", "test-secret-key", ""),
			},
			expectedEndpoint: "http://localhost:9324",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := SQSListener(tt.conf)
			require.NoError(t, err)
			require.NotNil(t, s)

			assert.Equal(t, tt.conf.Region, s.conf.Region)
			assert.Equal(t, tt.expectedEndpoint, s.Endpoint())
		})
	}
}

func TestGetCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		ctype        string
		akid         string
		secret       string
		wantErr      bool
		wantNilCreds bool
	}{
		{"static valid", "static", "test-access-key-id", "test-secret-key", false, false},
		{"static, missing id", "static", "", "test-secret-key", true, false},
		{"static, missing secret", "static", "test-access-key-id", "", true, false},
		// environment returns nil intentionally — SQSListener omits the credentials
		// option and lets the SDK use its default provider chain, which includes env vars.
		{"environment", "environment", "", "", false, true},
		{"ec2 role", "ec2role", "", "", false, false},
		{"invalid type", "foobar", "", "", true, false},
		{"default (static)", "", "test-access-key-id", "test-secret-key", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			creds, err := GetCredentials(tt.ctype, tt.akid, tt.secret)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if !tt.wantNilCreds {
					assert.NotNil(t, creds)
				}
			}
		})
	}
}

func TestSQS_GetMessages_Errors(t *testing.T) {
	t.Parallel()
	queueName := "test_queue"

	tests := []struct {
		name       string
		mockErr    error
		errContain string
	}{
		{
			name:       "queue does not exist",
			mockErr:    errors.New("The specified queue does not exist"),
			errContain: fmt.Sprintf("queue %q", queueName),
		},
		{
			name:       "generic error",
			mockErr:    errors.New("something went wrong"),
			errContain: "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockSQS{
				receiveFunc: func(_ context.Context, _ *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
					return nil, tt.mockErr
				},
			}

			s := &SQS{
				svc:  m,
				conf: &Config{Queue: queueName},
			}

			msgs, err := s.GetMessages(context.Background())
			assert.Nil(t, msgs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContain)
		})
	}
}

// TestSQS_GetMessagesOnce_ReturnsPromptlyWhenEmpty demonstrates that
// GetMessagesOnce, unlike GetMessages, makes exactly one ReceiveMessage call
// and returns immediately even if that call comes back empty. GetMessages
// would instead retry the long-poll internally until either messages show up
// or ctx is canceled, which is unsuitable for a caller (like the hosted sqs
// plugin's Handle) that must return promptly on every invocation.
func TestSQS_GetMessagesOnce_ReturnsPromptlyWhenEmpty(t *testing.T) {
	t.Parallel()

	callCount := 0
	m := &mockSQS{
		receiveFunc: func(_ context.Context, _ *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
			callCount++
			return &sqs.ReceiveMessageOutput{}, nil // empty: no messages this poll
		},
	}
	s := &SQS{svc: m, conf: &Config{Queue: "test-queue"}}

	msgs, err := s.GetMessagesOnce(context.Background())
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, 1, callCount, "GetMessagesOnce must not retry internally")
}

func TestSQS_GetMessagesOnce_ReturnsMessages(t *testing.T) {
	t.Parallel()

	msg := types.Message{MessageId: new("1"), ReceiptHandle: new("r1")}
	callCount := 0
	m := &mockSQS{
		receiveFunc: func(_ context.Context, _ *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
			callCount++
			return &sqs.ReceiveMessageOutput{Messages: []types.Message{msg}}, nil
		},
	}
	s := &SQS{svc: m, conf: &Config{Queue: "test-queue"}}

	msgs, err := s.GetMessagesOnce(context.Background())
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "1", *msgs[0].MessageId)
	assert.Equal(t, 1, callCount)
}

func TestSQS_GetMessagesOnce_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	m := &mockSQS{
		receiveFunc: func(_ context.Context, _ *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
			return nil, wantErr
		},
	}
	s := &SQS{svc: m, conf: &Config{Queue: "test-queue"}}

	msgs, err := s.GetMessagesOnce(context.Background())
	assert.Nil(t, msgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test-queue")
	assert.ErrorIs(t, err, wantErr)
}

func TestSQS_DeleteMessages(t *testing.T) {
	t.Parallel()
	queueName := "test-queue"

	tests := []struct {
		name                 string
		mockErr              error
		failCount            int // number of leading calls that fail with mockErr before succeeding
		partialFailUntilCall int // if > 0, calls <= this return err == nil but Failed containing message "1"
		expectErr            bool
		wantCalls            int
		wantErrContains      string
	}{
		{
			name:      "success",
			mockErr:   nil,
			expectErr: false,
			wantCalls: 1,
		},
		{
			name:      "queue deleted during operation",
			mockErr:   errors.New("The specified queue does not exist"),
			failCount: 2, // fails on the initial attempt and the retry
			expectErr: true,
			wantCalls: 2,
		},
		{
			name:      "recovers on retry",
			mockErr:   errors.New("throttled"),
			failCount: 1, // fails once, then the retry succeeds
			expectErr: false,
			wantCalls: 2,
		},
		{
			name:                 "partial failure (err == nil, Failed non-empty) recovers on retry",
			partialFailUntilCall: 1, // first call reports message "1" as failed; retry succeeds
			expectErr:            false,
			wantCalls:            2,
		},
		{
			name:                 "partial failure persists on both attempts",
			partialFailUntilCall: 2, // both the initial call and the retry report message "1" as failed
			expectErr:            true,
			wantCalls:            2,
			wantErrContains:      "1 message(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callCount := 0
			m := &mockSQS{
				deleteFunc: func(_ context.Context, i *sqs.DeleteMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error) {
					callCount++
					assert.Equal(t, queueName, *i.QueueUrl)

					if tt.mockErr != nil && callCount <= tt.failCount {
						return nil, tt.mockErr
					}
					if tt.partialFailUntilCall > 0 && callCount <= tt.partialFailUntilCall {
						return &sqs.DeleteMessageBatchOutput{
							Failed: []types.BatchResultErrorEntry{{Id: new("1")}},
						}, nil
					}

					return &sqs.DeleteMessageBatchOutput{}, nil
				},
			}

			s := &SQS{
				svc:  m,
				conf: &Config{Queue: queueName},
			}

			msgs := []types.Message{{MessageId: new("1"), ReceiptHandle: new("r1")}}
			err := s.DeleteMessages(context.Background(), msgs)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), fmt.Sprintf("queue %q", queueName))
				if tt.wantErrContains != "" {
					assert.Contains(t, err.Error(), tt.wantErrContains)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.wantCalls, callCount)
		})
	}
}

func TestSQS_Queue(t *testing.T) {
	t.Parallel()
	s := &SQS{conf: &Config{Queue: "foo"}}
	assert.Equal(t, "foo", s.Queue())
}

func TestSQS_Endpoint(t *testing.T) {
	t.Parallel()
	s := &SQS{conf: &Config{Endpoint: "http://localhost:9324"}}
	assert.Equal(t, "http://localhost:9324", s.Endpoint())
}
