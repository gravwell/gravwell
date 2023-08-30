package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	manualTickerInterval = time.Minute
)

func start(wg *sync.WaitGroup, ctx context.Context, buckets []*BucketReader, sqsS3 []*SQSS3Listener, ot *objectTracker, lg *log.Logger) (err error) {
	if len(buckets) != 0 {
		wg.Add(1)
		go manualScanner(wg, ctx, buckets, ot, lg)
	}
	for _, v := range sqsS3 {
		wg.Add(1)
		go sqsS3Routine(v, wg, ctx, lg)
	}
	return
}

func sqsS3Routine(s *SQSS3Listener, wg *sync.WaitGroup, ctx context.Context, lg *log.Logger) {
	defer wg.Done()

	c := make(chan []*sqs.Message)
	for {
		var out []*sqs.Message
		go func() {
			o, err := s.sqs.GetMessages()
			if err != nil {
				lg.Error("sqs receive message error", log.KVErr(err))
				c <- nil
			}
			c <- o
		}()

		select {
		case out = <-c:
			if out == nil {
				return
			}
		case <-ctx.Done():
			lg.Info("sqs-s3 routine exiting", log.KV("name", s.Name))
			return
		}

		lg.Info("sqs received messages", log.KV("count", len(out)))

		// we may have multiple packed messages
		for _, v := range out {
			msg := []byte(*v.Body)

			// we're looking for an S3 data message, which contains
			// yet another json blob, which contains an S3
			// bucket and key.
			b := bytes.NewBuffer(msg)
			jdec := json.NewDecoder(b)
			var d s3Data
			err := jdec.Decode(&d)
			if err == nil {
				sb := strings.NewReader(d.Message)
				jdec = json.NewDecoder(sb)
				var subMessage s3SubMessage
				err := jdec.Decode(&subMessage)
				if err == nil {
					for _, x := range subMessage.S3ObjectKey {
						obj := &s3.Object{
							Key: aws.String(x),
						}
						err = ProcessContext(obj, ctx, s.svc, subMessage.S3Bucket, s.rdr, s.TG, s.src, s.Tag, s.Proc, s.MaxLineSize)
						if err != nil {
							lg.Error("processing message", log.KVErr(err))
						}
					}
				} else {
					lg.Warn("error decoding message", log.KVErr(err))
				}
			}
		}
	}
}

type s3Data struct {
	Type             string
	MessageId        string
	TopicArn         string
	Timestamp        string
	SignatureVersion string
	Signature        string
	SigningCertURL   string
	UnsubscribeURL   string

	Message string
}

type s3SubMessage struct {
	S3Bucket    string   `json:"s3Bucket"`
	S3ObjectKey []string `json:"s3ObjectKey"`
}

func manualScanner(wg *sync.WaitGroup, ctx context.Context, buckets []*BucketReader, ot *objectTracker, lg *log.Logger) {
	defer wg.Done()
	fullScan(ctx, buckets, ot, lg)
	if ctx.Err() != nil {
		return
	}

	lg.Info("completed standup scan")
	ticker := time.NewTicker(manualTickerInterval)
	defer ticker.Stop()

	//ticker loop
loop:
	for {
		select {
		case <-ticker.C:
			fullScan(ctx, buckets, ot, lg)
		case <-ctx.Done():
			break loop
		}
	}
}

func fullScan(ctx context.Context, buckets []*BucketReader, ot *objectTracker, lg *log.Logger) {
	for _, b := range buckets {
		if err := b.ManualScan(ctx, ot); err != nil {
			lg.Error("failed to scan S3 bucket objects",
				log.KV("bucket", b.Name),
				log.KVErr(err))
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := ot.Flush(); err != nil {
		lg.Error("failed to flush S3 state file", log.KVErr(err))
	}
}
