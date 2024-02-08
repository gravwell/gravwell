/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sqs_common

import (
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

type Config struct {
	Queue       string
	Region      string
	Credentials *credentials.Credentials
}

type SQS struct {
	conf *Config
	sess *session.Session
	svc  *sqs.SQS
}

// Creates a new SQS connection from a given Config object.
func SQSListener(c *Config) (*SQS, error) {
	var err error

	s := &SQS{
		conf: c,
	}

	s.sess, err = session.NewSession(&aws.Config{
		Region:      aws.String(c.Region),
		Credentials: c.Credentials,
	})
	if err != nil {
		return nil, err
	}

	s.svc = sqs.New(s.sess)

	return s, nil
}

// Returns one or more messages from the queue on this SQS object.
func (s *SQS) GetMessages() ([]*sqs.Message, error) {
	// aws uses string pointers, so we have to declare it on the
	// stack in order to take it's reference... why aws, why......
	an := "SentTimestamp"
	var maxMessages int64 = 10
	req := &sqs.ReceiveMessageInput{
		AttributeNames:      []*string{&an},
		MaxNumberOfMessages: &maxMessages,
	}

	req = req.SetQueueUrl(s.conf.Queue)
	err := req.Validate()
	if err != nil {
		return nil, err
	}

	var out *sqs.ReceiveMessageOutput
	for out == nil || len(out.Messages) == 0 {
		out, err = s.svc.ReceiveMessage(req)
		if err != nil {
			return nil, err
		}
		if len(out.Messages) == 0 {
			time.Sleep(time.Second)
		}
	}

	return out.Messages, nil
}

func (s *SQS) DeleteMessages(m []*sqs.Message, lg *log.Logger) error {
	deleter := &sqs.DeleteMessageBatchInput{
		QueueUrl: aws.String(s.conf.Queue),
	}

	for _, v := range m {
		deleter.Entries = append(deleter.Entries, &sqs.DeleteMessageBatchRequestEntry{
			Id:            v.MessageId,
			ReceiptHandle: v.ReceiptHandle,
		})
	}

	_, err := s.svc.DeleteMessageBatch(deleter)
	if err != nil {
		lg.Error("deleting messages failed, retrying", log.KVErr(err))
		//try again, this is important
		if _, err = s.svc.DeleteMessageBatch(deleter); err != nil {
			lg.Error("deleting messages retry failed, objects will likely be duplicated", log.KVerr(err))
		}
	}
	return err
}

func GetCredentials(t, akid, secret string) (*credentials.Credentials, error) {
	var c *credentials.Credentials

	if t == `` {
		//empty implies static
		t = `static`
	}
	switch t {
	case "static":
		if akid == `` {
			return nil, errors.New("missing ID")
		} else if secret == `` {
			return nil, errors.New("missing secret")
		}
		c = credentials.NewStaticCredentials(akid, secret, ``)
	case "environment":
		if c = credentials.NewEnvCredentials(); c == nil {
			//make sure we can get credentials, this won't check if they are valid
			return nil, errors.New("no environment credentials available")
		}
	case "ec2role":
		c = ec2rolecreds.NewCredentials(session.New())
	default:
		return nil, fmt.Errorf("invalid Credentials-Type %q", t)
	}

	return c, nil
}
