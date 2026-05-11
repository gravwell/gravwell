package sqs_common

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/gravwell/gravwell/v3/ingest/log"
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
				Credentials: credentials.NewStaticCredentialsProvider("akid", "secret", ""),
			},
			expectedEndpoint: "",
		},
		{
			name: "custom endpoint",
			conf: &Config{
				Queue:       "http://localhost:9324/000000000000/test-queue",
				Region:      "elasticmq",
				Endpoint:    "http://localhost:9324",
				Credentials: credentials.NewStaticCredentialsProvider("akid", "secret", ""),
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
		{"static valid", "static", "akid", "secret", false, false},
		{"static, missing id", "static", "", "secret", true, false},
		{"static, missing secret", "static", "akid", "", true, false},
		// environment returns nil intentionally — SQSListener omits the credentials
		// option and lets the SDK use its default provider chain, which includes env vars.
		{"environment", "environment", "", "", false, true},
		{"ec2 role", "ec2role", "", "", false, false},
		{"invalid type", "foobar", "", "", true, false},
		{"default (static)", "", "akid", "secret", false, false},
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

func TestSQS_DeleteMessages(t *testing.T) {
	t.Parallel()
	queueName := "test-queue"
	lg := log.NewDiscardLogger()

	tests := []struct {
		name      string
		mockErr   error
		expectErr bool
	}{
		{
			name:      "success",
			mockErr:   nil,
			expectErr: false,
		},
		{
			name:      "queue deleted during operation",
			mockErr:   errors.New("The specified queue does not exist"),
			expectErr: true,
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

					if tt.mockErr != nil {
						return nil, tt.mockErr
					}

					return &sqs.DeleteMessageBatchOutput{}, nil
				},
			}

			s := &SQS{
				svc:  m,
				conf: &Config{Queue: queueName},
			}

			msgs := []types.Message{{MessageId: new("1"), ReceiptHandle: new("r1")}}
			err := s.DeleteMessages(context.Background(), msgs, lg)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), fmt.Sprintf("queue %q", queueName))
			} else {
				assert.NoError(t, err)
			}

			if tt.mockErr != nil {
				assert.Equal(t, 2, callCount, "should have attempted retry")
			} else {
				assert.Equal(t, 1, callCount)
			}
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
