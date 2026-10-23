package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

// KinesisAPI defines the subset of the Kinesis client used by KinesisPublisher.
type KinesisAPI interface {
	PutRecord(ctx context.Context, params *kinesis.PutRecordInput, optFns ...func(*kinesis.Options)) (*kinesis.PutRecordOutput, error)
}

// KinesisPublisher publishes events as records to Kinesis streams.
type KinesisPublisher struct {
	client  KinesisAPI
	verbose bool
	logf    func(string, ...any)
}

// NewKinesisPublisher creates a new KinesisPublisher.
func NewKinesisPublisher(client KinesisAPI, verbose bool, logf func(string, ...any)) *KinesisPublisher {
	return &KinesisPublisher{
		client:  client,
		verbose: verbose,
		logf:    logf,
	}
}

func (p *KinesisPublisher) ServiceName() string {
	return "Kinesis"
}

// Publish sends each event as a separate Kinesis record to the given stream.
func (p *KinesisPublisher) Publish(ctx context.Context, stream string, events []Event) (Result, error) {
	result := Result{
		Service:  p.ServiceName(),
		Resource: stream,
	}

	for i, e := range events {
		data := []byte(e.String())

		if p.verbose {
			p.logf("  putting record %d/%d to stream %s", i+1, len(events), stream)
		}

		_, err := p.client.PutRecord(ctx, &kinesis.PutRecordInput{
			StreamName:   aws.String(stream),
			Data:         data,
			PartitionKey: aws.String("genawsdata"),
		})
		if err != nil {
			result.Failed++
			if result.Err == nil {
				result.Err = fmt.Errorf("failed to put record to stream %s: %w", stream, err)
			}
			continue
		}
		result.Sent++
	}

	if result.Failed > 0 && result.Sent > 0 {
		return result, nil
	}
	return result, result.Err
}
