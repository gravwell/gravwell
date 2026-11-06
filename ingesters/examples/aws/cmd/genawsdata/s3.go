package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3API defines the subset of the S3 client used by S3Publisher.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// S3Publisher publishes events as objects to S3 buckets.
type S3Publisher struct {
	client  S3API
	verbose bool
	logf    func(string, ...any)
}

// NewS3Publisher creates a new S3Publisher.
func NewS3Publisher(client S3API, verbose bool, logf func(string, ...any)) *S3Publisher {
	return &S3Publisher{
		client:  client,
		verbose: verbose,
		logf:    logf,
	}
}

func (p *S3Publisher) ServiceName() string {
	return "S3"
}

// Publish uploads events as a single object to the given bucket.
// The object key is "genawsdata/<timestamp>.log".
func (p *S3Publisher) Publish(ctx context.Context, bucket string, events []Event) (Result, error) {
	result := Result{
		Service:  p.ServiceName(),
		Resource: bucket,
	}

	if len(events) == 0 {
		return result, nil
	}

	// Build the object body as newline-delimited log lines.
	var buf strings.Builder
	for _, e := range events {
		buf.WriteString(e.String())
		buf.WriteByte('\n')
	}

	key := fmt.Sprintf("genawsdata/%s.log", events[0].Timestamp.Format("20060102T150405"))

	if p.verbose {
		p.logf("  uploading %d events to s3://%s/%s", len(events), bucket, key)
	}

	_, err := p.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader([]byte(buf.String())),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		result.Err = fmt.Errorf("failed to upload to s3://%s/%s: %w", bucket, key, err)
		return result, result.Err
	}

	result.Sent = len(events)
	return result, nil
}
