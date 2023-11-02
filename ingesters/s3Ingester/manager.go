package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	ERROR_BACKOFF        = 5 * time.Second
	QUEUE_DEPTH          = 1000
)

var (
	errEmptyBucket = errors.New("empty bucket name")
	errEmptyKey    = errors.New("empty key name")
)

func start(wg *sync.WaitGroup, ctx context.Context, buckets []*BucketReader, sqsS3 []*SQSS3Listener, ot *objectTracker, lg *log.Logger, numWorkers int) (err error) {
	if len(buckets) != 0 {
		wg.Add(1)
		go manualScanner(wg, ctx, buckets, ot, lg, numWorkers)
	}
	for _, v := range sqsS3 {
		wg.Add(1)
		go sqsS3Routine(v, wg, ctx, lg, numWorkers)
	}
	return
}

func sqsS3Routine(s *SQSS3Listener, wg *sync.WaitGroup, ctx context.Context, lg *log.Logger, numWorkers int) {
	defer wg.Done()

	// create workers
	var workerWg sync.WaitGroup
	queue := make(chan *sqs.Message, QUEUE_DEPTH)
	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go s.worker(ctx, lg, &workerWg, queue, i)
	}

	c := make(chan []*sqs.Message)
OUTER:
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
				lg.Error("received empty SQS response")
				sleepContext(ctx, ERROR_BACKOFF)
				continue
			}
		case <-ctx.Done():
			lg.Info("sqs-s3 routine exiting", log.KV("name", s.Name))
			break OUTER
		}

		lg.Info("sqs received messages", log.KV("count", len(out)))

		if s.Verbose {
			for _, v := range out {
				fmt.Println(*v.Body)
			}
		}

		// we may have multiple packed messages
		for _, v := range out {
			queue <- v
		}
	}
	close(queue)
	workerWg.Wait()
}

func (s *SQSS3Listener) worker(ctx context.Context, lg *log.Logger, wg *sync.WaitGroup, queue <-chan *sqs.Message, workerID int) {
	defer wg.Done()

	lg.Infof("worker %v started", workerID)

	for m := range queue {
		if m == nil {
			return
		}

		msg := []byte(*m.Body)

		// Messages that we care about are either SNS wrapped
		// or s3 put/post/create/whatever messages. Try for
		// both, error if it's neither.
		buckets, keys, err := snsDecode(msg)
		if err != nil {
			buckets, keys, err = s3Decode(msg)
			if err != nil {
				lg.Warn("error decoding message", log.KVErr(err))
				continue
			} else {
				logSnsKeyDecode(lg, "S3", buckets, keys)
			}
		} else {
			logSnsKeyDecode(lg, "SNS", buckets, keys)
		}

		shouldDelete := true
		for i, x := range keys {
			obj := &s3.Object{
				Key: aws.String(x),
			}
			err = ProcessContext(obj, ctx, s.svc, buckets[i], s.rdr, s.TG, s.src, s.Tag, s.Proc, s.MaxLineSize)
			if err != nil {
				shouldDelete = false
				lg.Error("processing message", log.KV("bucket", buckets[i]), log.KV("key", x), log.KVErr(err))
			} else {
				lg.Info("successfully processed message", log.KV("bucket", buckets[i]), log.KV("key", x))
			}
		}

		// delete messages we successfully processed
		if shouldDelete {
			err = s.sqs.DeleteMessages([]*sqs.Message{m})
			if err != nil {
				lg.Error("deleting message", log.KVErr(err))
			}
		}

		if ctx.Err() != nil {
			return
		}
	}
	lg.Infof("worker %v exiting", workerID)
}

func snsDecode(input []byte) ([]string, []string, error) {
	b := bytes.NewBuffer(input)
	jdec := json.NewDecoder(b)
	var d s3Data
	err := jdec.Decode(&d)
	if err != nil {
		return nil, nil, err
	}
	sb := strings.NewReader(d.Message)
	jdec = json.NewDecoder(sb)
	var subMessage s3SubMessage
	err = jdec.Decode(&subMessage)
	if err != nil {
		return nil, nil, err
	}

	if subMessage.S3Bucket == "" {
		return nil, nil, errEmptyBucket
	}

	var buckets []string

	// all the buckets are the same in this message type
	for _, v := range subMessage.S3ObjectKey {
		if v == "" {
			return nil, nil, errEmptyKey
		}
		buckets = append(buckets, subMessage.S3Bucket)
	}

	return buckets, subMessage.S3ObjectKey, nil
}

func s3Decode(input []byte) ([]string, []string, error) {
	b := bytes.NewBuffer(input)
	jdec := json.NewDecoder(b)
	var d *s3Records
	err := jdec.Decode(&d)
	if err != nil {
		return nil, nil, err
	}

	var buckets []string
	var keys []string
	for _, v := range d.Records {
		if strings.Contains(v.EventName, "ObjectCreated") {
			if v.S3.Bucket.Name == "" {
				return nil, nil, errEmptyBucket
			} else if v.S3.Object.Key == "" {
				return nil, nil, errEmptyKey
			}
			buckets = append(buckets, v.S3.Bucket.Name)
			keys = append(keys, v.S3.Object.Key)
		}
	}

	return buckets, keys, nil
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

type s3Records struct {
	Records []s3InnerRecord
}

type s3InnerRecord struct {
	EventName string         `json:"eventName"`
	S3        s3RecordObject `json:"s3"`
}

type s3RecordObject struct {
	Bucket s3BucketObject `json:"bucket"`
	Object s3ObjectObject `json:"object"`
}

type s3BucketObject struct {
	Name string `json:"name"`
}

type s3ObjectObject struct {
	Key string `json:"key"`
}

func manualScanner(wg *sync.WaitGroup, ctx context.Context, buckets []*BucketReader, ot *objectTracker, lg *log.Logger, numWorkers int) {
	defer wg.Done()
	fullScan(ctx, buckets, ot, lg, numWorkers)
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
			fullScan(ctx, buckets, ot, lg, numWorkers)
		case <-ctx.Done():
			break loop
		}
	}
}

func fullScan(ctx context.Context, buckets []*BucketReader, ot *objectTracker, lg *log.Logger, numWorkers int) {
	var wg sync.WaitGroup
	for _, b := range buckets {
		// start workers
		queue := make(chan *s3.Object, QUEUE_DEPTH)
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go b.worker(ctx, ot, queue, &wg)

		}
		if err := b.ManualScan(ctx, ot, queue); err != nil {
			lg.Error("failed to scan S3 bucket objects",
				log.KV("bucket", b.Name),
				log.KVErr(err))
		}
		close(queue)
		if ctx.Err() != nil {
			break
		}
	}
	wg.Wait()
	if err := ot.Flush(); err != nil {
		lg.Error("failed to flush S3 state file", log.KVErr(err))
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
