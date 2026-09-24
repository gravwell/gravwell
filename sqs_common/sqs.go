/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package sqs_common implements the core of the of the SQS systems for
// the Gravwell S3 and SQS ingesters
package sqs_common

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type SQSHandler interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessageBatch(ctx context.Context, params *sqs.DeleteMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error)
}

type Config struct {
	Queue       string
	Region      string
	Endpoint    string
	Credentials aws.CredentialsProvider
}

type SQS struct {
	conf *Config
	svc  SQSHandler
}

// SQSListener creates a new SQS connection from a given Config object.
func SQSListener(c *Config) (*SQS, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(c.Region),
	}
	if c.Credentials != nil {
		opts = append(opts, config.WithCredentialsProvider(c.Credentials))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("load default config: %w", err)
	}

	var clientOpts []func(*sqs.Options)
	if c.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *sqs.Options) {
			o.BaseEndpoint = new(c.Endpoint)
		})
	}

	sqsSvc := sqs.NewFromConfig(cfg, clientOpts...)
	return &SQS{
		conf: c,
		svc:  sqsSvc,
	}, nil
}

func (s *SQS) receiveMessageInput() *sqs.ReceiveMessageInput {
	return &sqs.ReceiveMessageInput{
		QueueUrl: new(s.conf.Queue),
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameSentTimestamp,
		},
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     20, // Setting this will hold the connection until messages are available or if 20s elapses. Makes the loop/sleep logic below less chatty.
	}
}

// GetMessages returns one or more messages from the queue on this SQS object.
// It blocks, internally retrying the long-poll ReceiveMessage call, until
// either messages are available or ctx is canceled. Callers that must return
// promptly on every call (e.g. a scheduler that re-evaluates state between
// calls) should use GetMessagesOnce instead.
func (s *SQS) GetMessages(ctx context.Context) ([]types.Message, error) {
	input := s.receiveMessageInput()

	var (
		out *sqs.ReceiveMessageOutput
		err error
	)
	for out == nil || len(out.Messages) == 0 {
		out, err = s.svc.ReceiveMessage(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("error getting messages on queue %q: %w", s.Queue(), err)
		}
		if out != nil && len(out.Messages) > 0 {
			return out.Messages, nil
		}
		// Queue was empty for the full WaitTimeSeconds window.
		// Check if the ctx was cancelled before immediately retrying (WaitTimeSeconds should have already elapsed).
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	return out.Messages, nil
}

// GetMessagesOnce performs a single ReceiveMessage long-poll call and returns
// immediately with whatever it got, including zero messages. Unlike
// GetMessages, it never retries internally, so a call to it is bounded by a
// single WaitTimeSeconds window rather than blocking indefinitely on an idle
// queue.
func (s *SQS) GetMessagesOnce(ctx context.Context) ([]types.Message, error) {
	out, err := s.svc.ReceiveMessage(ctx, s.receiveMessageInput())
	if err != nil {
		return nil, fmt.Errorf("error getting messages on queue %q: %w", s.Queue(), err)
	}
	return out.Messages, nil
}

// DeleteMessages removes the given messages from the queue. A call can come
// back with err == nil but still report individual entries as rejected via
// DeleteMessageBatchOutput.Failed (e.g. AWS SQS itself hiccupping on specific
// entries); those are treated the same as a hard error since the messages
// they refer to will otherwise be silently redelivered/duplicated.
func (s *SQS) DeleteMessages(ctx context.Context, m []types.Message) error {
	input := &sqs.DeleteMessageBatchInput{
		QueueUrl: new(s.conf.Queue),
	}

	for _, v := range m {
		input.Entries = append(input.Entries, types.DeleteMessageBatchRequestEntry{
			Id:            v.MessageId,
			ReceiptHandle: v.ReceiptHandle,
		})
	}

	failed, err := s.deleteMessageBatch(ctx, input)
	if err == nil && len(failed) == 0 {
		return nil
	}

	// Retry once — undeleted messages will be redelivered and duplicated downstream.
	// If the call errored outright, retry the whole batch since we don't know
	// which entries, if any, were actually processed. If it succeeded but
	// reported specific entries as failed, only retry those.
	retryInput := input
	if err == nil {
		retryInput = &sqs.DeleteMessageBatchInput{
			QueueUrl: new(s.conf.Queue),
			Entries:  entriesByID(input.Entries, failed),
		}
	}
	failed, err = s.deleteMessageBatch(ctx, retryInput)
	if err != nil {
		return fmt.Errorf("error deleting messages on queue %q (retry failed, messages will likely be duplicated): %w", s.Queue(), err)
	}
	if len(failed) > 0 {
		return fmt.Errorf("error deleting %d message(s) on queue %q after retry (ids: %s), messages will likely be duplicated",
			len(failed), s.Queue(), strings.Join(failed, ", "))
	}
	return nil
}

// deleteMessageBatch issues a single DeleteMessageBatch call and returns the
// IDs of any entries reported as failed in the response.
func (s *SQS) deleteMessageBatch(ctx context.Context, input *sqs.DeleteMessageBatchInput) ([]string, error) {
	out, err := s.svc.DeleteMessageBatch(ctx, input)
	if err != nil {
		return nil, err
	}
	var failed []string
	for _, f := range out.Failed {
		if f.Id != nil {
			failed = append(failed, *f.Id)
		}
	}
	return failed, nil
}

// entriesByID returns the subset of entries whose Id is in ids.
func entriesByID(entries []types.DeleteMessageBatchRequestEntry, ids []string) []types.DeleteMessageBatchRequestEntry {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []types.DeleteMessageBatchRequestEntry
	for _, e := range entries {
		if e.Id != nil && want[*e.Id] {
			out = append(out, e)
		}
	}
	return out
}

func GetCredentials(t, akid, secret string) (aws.CredentialsProvider, error) {
	// Empty type implies "static" credentials.
	t = cmp.Or(t, "static")

	switch t {
	case "static":
		if akid == "" {
			return nil, errors.New("missing ID")
		} else if secret == "" {
			return nil, errors.New("missing secret")
		}
		return aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(akid, secret, "")), nil
	case "environment":
		return nil, nil
	case "ec2role":
		return aws.NewCredentialsCache(ec2rolecreds.New()), nil
	default:
		return nil, fmt.Errorf("invalid Credentials-Type %q", t)
	}
}

// Queue returns the SQS queue its configured to use.
func (s *SQS) Queue() string {
	return s.conf.Queue
}

// Endpoint returns the custom endpoint its configured to use (empty if not specified).
func (s *SQS) Endpoint() string {
	return s.conf.Endpoint
}
