/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sqs_common

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type Config struct {
	Queue  string
	Region string
	AKID   string
	Secret string
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
		Credentials: credentials.NewStaticCredentials(c.AKID, c.Secret, ""),
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
	req := &sqs.ReceiveMessageInput{
		AttributeNames: []*string{&an},
	}

	req = req.SetQueueUrl(s.conf.Queue)
	err := req.Validate()
	if err != nil {
		return nil, err
	}

	out, err := s.svc.ReceiveMessage(req)
	if err != nil {
		return nil, err
	}

	return out.Messages, nil
}
