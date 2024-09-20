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
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const (
	manualTickerInterval = time.Minute
	ERROR_BACKOFF        = 5 * time.Second
	QUEUE_DEPTH          = 2 // this MUST be small, other wise we outrun the workers and then amazon jsut piles it back onto the queue
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
	queue := make(chan []*sqs.Message, QUEUE_DEPTH)
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

		queue <- out
	}
	close(queue)
	workerWg.Wait()
}

func (s *SQSS3Listener) worker(ctx context.Context, lg *log.Logger, wg *sync.WaitGroup, queue <-chan []*sqs.Message, workerID int) {
	var s3rtt, rtt time.Duration
	var sz int64
	defer wg.Done()

	lg.Infof("worker %v started", workerID)

	for sm := range queue {
		var deleteQueue []*sqs.Message
		for _, m := range sm {
			if m == nil || m.Body == nil {
				continue
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
				// should we bother with this key?
				if !s.filter.match(x) {
					lg.Info("skipping key based on filter", log.KV("key", x))
					continue
				}

				obj := &s3.Object{
					Key: aws.String(x),
				}

				if obj != nil && obj.Size != nil && *obj.Size == int64(0) {
					// don't even bother fetching it, just delete and move on
					lg.Info("skipping zero-byte object",
						log.KV("worker", workerID),
						log.KV("bucket", buckets[i]),
						log.KV("key", x))
					continue
				}

				sz, s3rtt, rtt, err = ProcessContext(obj, ctx, s.svc, buckets[i], s.rdr, s.TG, s.src, s.Tag, s.Proc, s.MaxLineSize)
				if err != nil {
					shouldDelete = false
					lg.Error("error processing message", log.KV("bucket", buckets[i]), log.KV("key", x), log.KVErr(err))
				} else {
					lg.Info("successfully processed message",
						log.KV("worker", workerID),
						log.KV("bucket", buckets[i]),
						log.KV("key", x),
						log.KV("s3-rtt", s3rtt),
						log.KV("rtt", rtt),
						log.KV("size", sz))
				}
			}

			if shouldDelete {
				deleteQueue = append(deleteQueue, m)
			}
		}

		// delete messages we successfully processed
		if len(deleteQueue) != 0 {
			err := s.sqs.DeleteMessages(deleteQueue, lg)
			if err != nil {
				lg.Error("deleting messages", log.KVErr(err))
			}
		}

		if ctx.Err() != nil {
			break
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
	} else if d.Message == "" {
		return nil, nil, fmt.Errorf("empty message")
	}

	var buckets []string
	var keys []string

	sb := strings.NewReader(d.Message)
	jdec = json.NewDecoder(sb)

	var subMessage s3SubMessage
	err = jdec.Decode(&subMessage)
	if err != nil || subMessage.S3Bucket == "" || len(subMessage.S3ObjectKey) == 0 {
		// try again with the records format instead
		var records s3Records
		sb.Reset(d.Message)
		err = jdec.Decode(&records)
		if err != nil {
			return nil, nil, err
		} else if len(records.Records) == 0 {
			return nil, nil, fmt.Errorf("empty records")
		}

		for _, v := range records.Records {
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
	} else {
		// all the buckets are the same in this message type
		for _, v := range subMessage.S3ObjectKey {
			if v == "" {
				return nil, nil, errEmptyKey
			}
			buckets = append(buckets, subMessage.S3Bucket)
			keys = append(keys, v)
		}
	}
	return buckets, keys, nil
}

func s3Decode(input []byte) ([]string, []string, error) {
	b := bytes.NewBuffer(input)
	jdec := json.NewDecoder(b)
	var d s3Records
	err := jdec.Decode(&d)
	if err != nil {
		return nil, nil, err
	} else if len(d.Records) == 0 {
		return nil, nil, fmt.Errorf("empty records")
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
	lg.Info("starting full manual scan")
	for _, b := range buckets {
		// start workers
		queue := make(chan *s3.Object, QUEUE_DEPTH)
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go b.worker(lg, ctx, ot, queue, &wg)

		}
		if err := b.ManualScan(lg, ctx, ot, queue); err != nil {
			lg.Error("failed to scan S3 bucket objects",
				log.KV("bucket", b.Name),
				log.KVErr(err))
		}
		close(queue)
		if ctx.Err() != nil {
			break
		}
	}
	lg.Info("completed full manual scan")
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
