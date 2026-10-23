package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSAPI defines the subset of the SQS client used by SQSPublisher.
type SQSAPI interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

// SQSPublisher publishes events as messages to SQS queues.
type SQSPublisher struct {
	client  SQSAPI
	verbose bool
	logf    func(string, ...any)
}

// NewSQSPublisher creates a new SQSPublisher.
func NewSQSPublisher(client SQSAPI, verbose bool, logf func(string, ...any)) *SQSPublisher {
	return &SQSPublisher{
		client:  client,
		verbose: verbose,
		logf:    logf,
	}
}

func (p *SQSPublisher) ServiceName() string {
	return "SQS"
}

// Publish sends each event as a separate SQS message to the given queue URL.
func (p *SQSPublisher) Publish(ctx context.Context, queueURL string, events []Event) (Result, error) {
	result := Result{
		Service:  p.ServiceName(),
		Resource: queueURL,
	}

	for i, e := range events {
		body := e.String()

		if p.verbose {
			p.logf("  sending message %d/%d to %s", i+1, len(events), queueURL)
		}

		_, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    aws.String(queueURL),
			MessageBody: aws.String(body),
		})
		if err != nil {
			result.Failed++
			if result.Err == nil {
				result.Err = fmt.Errorf("failed to send message to %s: %w", queueURL, err)
			}
			continue
		}
		result.Sent++
	}

	if result.Failed > 0 && result.Sent > 0 {
		// Partial failure — don't return error so we can still report the result.
		return result, nil
	}
	return result, result.Err
}
