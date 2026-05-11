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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/gravwell/gravwell/v3/ingest/log"
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

// GetMessages returns one or more messages from the queue on this SQS object.
func (s *SQS) GetMessages(ctx context.Context) ([]types.Message, error) {
	input := &sqs.ReceiveMessageInput{
		QueueUrl: new(s.conf.Queue),
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameSentTimestamp,
		},
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     20, // Setting this will hold the connection until messages are available or if 20s elapses. Makes the loop/sleep logic below less chatty.
	}

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

func (s *SQS) DeleteMessages(ctx context.Context, m []types.Message, lg *log.Logger) error {
	input := &sqs.DeleteMessageBatchInput{
		QueueUrl: new(s.conf.Queue),
	}

	for _, v := range m {
		input.Entries = append(input.Entries, types.DeleteMessageBatchRequestEntry{
			Id:            v.MessageId,
			ReceiptHandle: v.ReceiptHandle,
		})
	}

	_, err := s.svc.DeleteMessageBatch(ctx, input)
	if err != nil {
		_ = lg.Error("deleting messages failed, retrying", log.KVErr(err))
		//try again, this is important
		if _, err = s.svc.DeleteMessageBatch(ctx, input); err != nil {
			_ = lg.Error("deleting messages retry failed, objects will likely be duplicated", log.KVErr(err))
		}
	}

	if err != nil {
		err = fmt.Errorf("error deleting messages on queue %q: %w", s.Queue(), err)
	}

	return err
}

func GetCredentials(t, akid, secret string) (aws.CredentialsProvider, error) {
	// Empty type impilies "static" credentials.
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
