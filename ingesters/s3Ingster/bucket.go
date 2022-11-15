package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	maxMaxRetries      = 10
	defaultMaxRetries  = 3
	defaultMaxLineSize = 512 * 1024
)

type AuthConfig struct {
	ID         string
	Secret     string
	Region     string
	Bucket_URL string //this is the amazon ARN URL
	MaxRetries int
}

type BucketConfig struct {
	AuthConfig
	TimeConfig
	Verbose        bool
	MaxLineSize    int
	Name           string
	FileFilters    []string
	Tag            entry.EntryTag
	TagName        string
	SourceOverride string
	Proc           *processors.ProcessorSet
	TG             *timegrinder.TimeGrinder
	Logger         *log.Logger
}

type BucketReader struct {
	BucketConfig
	session *session.Session
	svc     *s3.S3
	filter  *matcher
	tg      timegrinder.TimeGrinder
	src     net.IP
}

func NewBucketReader(cfg BucketConfig) (br *BucketReader, err error) {
	if err = cfg.validate(); err != nil {
		return
	}
	var filter *matcher
	if filter, err = newMatcher(cfg.FileFilters); err != nil {
		return
	}
	var sess *session.Session
	sess, err = session.NewSession(&aws.Config{
		Region:      aws.String(cfg.Region),
		MaxRetries:  aws.Int(cfg.MaxRetries),
		Credentials: credentials.NewStaticCredentials(cfg.ID, cfg.Secret, ``),
	})
	if err != nil {
		err = fmt.Errorf("Failed to create AWS session %w", err)
		return
	}

	// Create S3 service client
	svc := s3.New(sess)

	br = &BucketReader{
		BucketConfig: cfg,
		session:      sess,
		svc:          svc,
		filter:       filter,
		src:          cfg.srcOverride(),
	}
	return
}

func (bc *BucketConfig) validate() (err error) {
	if err = bc.AuthConfig.validate(); err != nil {
		return
	} else if err = bc.TimeConfig.validate(); err != nil {
		return
	} else if bc.Proc == nil {
		err = errors.New("processor is empty")
		return
	} else if bc.Name == `` {
		err = errors.New("missing name")
		return
	} else if bc.Logger == nil {
		err = errors.New("nil logger")
		return
	}
	if bc.SourceOverride != `` {
		if net.ParseIP(bc.SourceOverride) == nil {
			err = fmt.Errorf("Source-Override %s is not a valid source", bc.SourceOverride)
			return
		}
	}
	if bc.MaxLineSize <= 0 {
		bc.MaxLineSize = defaultMaxLineSize
	}
	return
}

func (bc *BucketConfig) srcOverride() net.IP {
	if bc.SourceOverride == `` {
		return nil
	}
	return net.ParseIP(bc.SourceOverride)
}

func (ac *AuthConfig) validate() (err error) {
	if ac.Region == `` {
		err = errors.New("missing region")
	} else if ac.Bucket_URL == `` {
		err = errors.New("missing bucket URL")
	} else if ac.ID == `` {
		err = errors.New("missing ID")
	} else if ac.Secret == `` {
		err = errors.New("missing secret")
	} else if ac.MaxRetries <= 0 || ac.MaxRetries > maxMaxRetries {
		ac.MaxRetries = defaultMaxRetries
	}
	return
}

// ShouldTrack just checks if we should process this file
func (br *BucketReader) ShouldTrack(obj string) (ok bool) {
	//first check if this is in our filters
	ok = br.filter.match(obj)
	return //nope
}

// Process reads the object in and processes its contents
func (br *BucketReader) Process(obj *s3.Object) (err error) {
	return br.ProcessContext(obj, nil)
}

func (br *BucketReader) ProcessContext(obj *s3.Object, ctx context.Context) (err error) {
	var r *s3.GetObjectOutput
	r, err = br.svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(br.Bucket_URL),
		Key:    obj.Key,
	})
	if err != nil {
		return
	}
	defer r.Body.Close()

	sc := bufio.NewScanner(r.Body)
	sc.Buffer(nil, br.MaxLineSize)
	for sc.Scan() {
		bts := sc.Bytes()
		if len(bts) == 0 {
			continue
		}
		ts, ok, _ := br.TG.Extract(bts)
		if !ok {
			ts = time.Now()
		}
		ent := entry.Entry{
			TS:   entry.FromStandard(ts),
			SRC:  br.src,                      //may be nil, ingest muxer will handle if it is
			Data: append([]byte(nil), bts...), //scanner re-uses the buffer
			Tag:  br.Tag,
		}
		if ctx != nil {
			err = br.Proc.ProcessContext(&ent, ctx)
		} else {
			err = br.Proc.Process(&ent)
		}
		if err != nil {
			return //just leave
		}
	}

	return
}

func (br *BucketReader) ManualScan(ctx context.Context, ot *objectTracker) (err error) {
	//list the objects in the bucket
	req := s3.ListObjectsV2Input{
		Bucket: aws.String(br.Bucket_URL),
	}

	var lerr error
	objListHandler := func(resp *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, item := range resp.Contents {
			//do a quick check for stupidity
			if item.Size == nil || item.LastModified == nil || item.Key == nil {
				continue
			}
			sz, lm, key := *item.Size, *item.LastModified, *item.Key
			if sz == 0 || !br.ShouldTrack(key) {
				continue //skip empty objects or things we should not track
			}
			//lookup the object in the objectTracker
			state, ok := ot.Get(br.Bucket_URL, key)
			if ok && state.Updated.Equal(lm) {
				continue //already handled this
			}

			//ok, lets process this thing
			if lerr = br.Process(item); lerr != nil {
				br.Logger.Error("failed to process object",
					log.KV("bucket", br.Name),
					log.KV("url", br.Bucket_URL),
					log.KV("object", key),
					log.KVErr(err))
				return false //quit the scan
			} else {
				br.Logger.Info("consumed object",
					log.KV("bucket", br.Name),
					log.KV("url", br.Bucket_URL),
					log.KV("object", key),
					log.KV("size", sz))
			}
			state = trackedObjectState{
				Updated: lm,
				Size:    sz,
			}
			lerr = ot.Set(br.Bucket_URL, key, state, false)
			if ctx.Err() != nil {
				return false //context says stop, bail
			}
		}
		return true //continue the scan
	}

	if err = br.svc.ListObjectsV2Pages(&req, objListHandler); err == nil {
		err = lerr
	}
	return
}
