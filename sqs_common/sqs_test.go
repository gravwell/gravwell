package sqs_common

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSQS struct {
	sqsiface.SQSAPI
	receiveFunc func(*sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error)
	deleteFunc  func(*sqs.DeleteMessageBatchInput) (*sqs.DeleteMessageBatchOutput, error)
}

func (m *mockSQS) ReceiveMessage(i *sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error) {
	return m.receiveFunc(i)
}

func (m *mockSQS) DeleteMessageBatch(i *sqs.DeleteMessageBatchInput) (*sqs.DeleteMessageBatchOutput, error) {
	return m.deleteFunc(i)
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
				Credentials: credentials.NewStaticCredentials("akid", "secret", ""),
			},
			expectedEndpoint: "",
		},
		{
			name: "custom endpoint",
			conf: &Config{
				Queue:       "http://localhost:9324/000000000000/test-queue",
				Region:      "elasticmq",
				Endpoint:    "http://localhost:9324",
				Credentials: credentials.NewStaticCredentials("akid", "secret", ""),
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
			require.NotNil(t, s.sess)

			assert.Equal(t, tt.conf.Region, *s.sess.Config.Region)

			if tt.expectedEndpoint == "" {
				assert.Nil(t, s.sess.Config.Endpoint)
			} else {
				require.NotNil(t, s.sess.Config.Endpoint)
				assert.Equal(t, tt.expectedEndpoint, *s.sess.Config.Endpoint)
			}
		})
	}
}

func TestGetCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctype   string
		akid    string
		secret  string
		wantErr bool
	}{
		{"static valid", "static", "akid", "secret", false},
		{"static, missing id", "static", "", "secret", true},
		{"static, missing secret", "static", "akid", "", true},
		{"environment", "environment", "", "", false},
		{"ec2 role", "ec2role", "", "", false},
		{"invalid type", "foobar", "", "", true},
		{"default (static)", "", "akid", "secret", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			creds, err := GetCredentials(tt.ctype, tt.akid, tt.secret)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, creds)
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
			mockErr:    awserr.New(sqs.ErrCodeQueueDoesNotExist, "The specified queue does not exist", nil),
			errContain: fmt.Sprintf("queue '%s'", queueName),
		},
		{
			name:       "generic aws error",
			mockErr:    awserr.New("InternalError", "something went wrong", nil),
			errContain: "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockSQS{
				receiveFunc: func(i *sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error) {
					return nil, tt.mockErr
				},
			}

			s := &SQS{
				svc:  m,
				conf: &Config{Queue: queueName},
			}

			msgs, err := s.GetMessages()
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
			mockErr:   awserr.New(sqs.ErrCodeQueueDoesNotExist, "The specified queue does not exist", nil),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callCount := 0
			m := &mockSQS{
				deleteFunc: func(i *sqs.DeleteMessageBatchInput) (*sqs.DeleteMessageBatchOutput, error) {
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

			msgs := []*sqs.Message{{MessageId: aws.String("1"), ReceiptHandle: aws.String("r1")}}
			err := s.DeleteMessages(msgs, lg)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), fmt.Sprintf("queue '%s'", queueName))
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
