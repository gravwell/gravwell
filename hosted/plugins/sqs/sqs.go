/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package sqs provides a shared SQS API client and a hosted
// ingester plugin for the Gravwell hosted ingester runtime.
package sqs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/sqs_common"
)

type sqsClient interface {
	GetMessages(ctx context.Context) ([]types.Message, error)
	DeleteMessages(ctx context.Context, m []types.Message) error
}

type Queue struct {
	conf   *Config
	client sqsClient
}

func New(conf *Config) (*Queue, error) {
	creds, err := sqs_common.GetCredentials(conf.Credentials_Type, conf.AKID, conf.Secret)
	if err != nil {
		return nil, fmt.Errorf("sqs credentials: %w", err)
	}
	client, err := sqs_common.SQSListener(&sqs_common.Config{
		Queue:       conf.Queue_URL,
		Region:      conf.Region,
		Endpoint:    conf.Endpoint,
		Credentials: creds,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to sqs queue %q: %w", conf.Queue_URL, err)
	}
	return &Queue{conf: conf, client: client}, nil
}

func (q *Queue) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	tag, err := rt.NegotiateTag(q.conf.ResolveTag(entry.DefaultTagName))
	if err != nil {
		return nil, fmt.Errorf("negotiate tag: %w", err)
	}

	msgs, err := q.client.GetMessages(ctx)
	if err != nil {
		return nil, err
	}

	var toDelete []types.Message
	for _, m := range msgs {
		ts := ExtractTimestamp(m, q.conf.Ignore_Timestamps)
		e := entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			Data: []byte(*m.Body),
		}
		if err := rt.Write(e); err != nil {
			rt.Error("failed to write entry", log.KVErr(err))
			continue
		}
		toDelete = append(toDelete, m)
	}

	if len(toDelete) > 0 {
		if err := q.client.DeleteMessages(ctx, toDelete); err != nil {
			rt.Error("failed to delete messages", log.KVErr(err))
		}
	}

	return hosted.ContinueNow(), nil
}
