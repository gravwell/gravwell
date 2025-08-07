package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/sqs_common"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	maxMaxRetries      = 10
	defaultMaxRetries  = 3
	defaultMaxLineSize = 4 * 1024 * 1024
	defaultRegion      = `us-east-1`
)

type AuthConfig struct {
	Region              string
	Bucket_ARN          string // Amazon ARN (should be JUST the bucket ARN)
	Endpoint            string // arbitrary endpoint
	Bucket_Name         string // defined bucket
	Bucket_URL          string `json:"-"` // DEPRECATED DO NOT USE
	MaxRetries          int
	Disable_TLS         bool // allows disable SSL on the upstream
	S3_Force_Path_Style bool //for endpoints where bucket name is on the PATH of a url
}

type BucketConfig struct {
	AuthConfig
	TimeConfig
	Verbose          bool
	MaxLineSize      int
	Reader           string //defaults to line
	Name             string
	FileFilters      []string
	Tag              entry.EntryTag
	TagName          string
	SourceOverride   string
	Proc             *processors.ProcessorSet
	TG               *timegrinder.TimeGrinder
	Logger           *log.Logger
	ID               string `json:"-"` //do not ship this as part of a config report
	Secret           string `json:"-"` //do not ship this as part of a config report
	Credentials_Type string
}

type BucketReader struct {
	BucketConfig
	prefixFilter string
	session      *session.Session
	svc          *s3.S3
	filter       *matcher
	tg           timegrinder.TimeGrinder
	src          net.IP
	rdr          reader
}

func NewBucketReader(cfg BucketConfig) (br *BucketReader, err error) {
	var rdr reader
	var sess *session.Session
	if err = cfg.validate(); err != nil {
		return
	}
	var filter *matcher
	if filter, err = newMatcher(cfg.FileFilters); err != nil {
		return
	}
	if rdr, err = parseReader(cfg.Reader); err != nil {
		return
	}
	c, err := sqs_common.GetCredentials(cfg.Credentials_Type, cfg.ID, cfg.Secret)
	if err != nil {
		return nil, err
	}
	if sess, err = cfg.AuthConfig.getSession(cfg, c); err != nil {
		err = fmt.Errorf("Failed to create S3 session %w", err)
		return
	}

	br = &BucketReader{
		BucketConfig: cfg,
		session:      sess,
		svc:          s3.New(sess),
		filter:       filter,
		src:          cfg.srcOverride(),
		rdr:          rdr,
		//TODO FIXME prefixFilter: resolvePrefixFilter(cfg.FileFilters),
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

func (br *BucketReader) Test(ctx context.Context) error {
	//list the objects in the bucket
	req := s3.ListObjectsV2Input{
		Bucket:  aws.String(br.Bucket_Name),
		MaxKeys: aws.Int64(1), //just need one to check
	}
	return br.svc.ListObjectsV2Pages(&req, func(resp *s3.ListObjectsV2Output, lastPage bool) bool {
		return false //do not continue the scan
	})
}

// ShouldTrack just checks if we should process this file
func (br *BucketReader) ShouldTrack(obj string) (ok bool) {
	//first check if this is in our filters
	ok = br.filter.match(obj)
	return //nope
}

// Process reads the object in and processes its contents
func (br *BucketReader) Process(obj *s3.Object, ctx context.Context) (sz int64, s3rtt, rtt time.Duration, err error) {
	return ProcessContext(obj, ctx, br.svc, br.Bucket_Name, br.rdr, br.TG, br.src, br.Tag, br.Proc, br.MaxLineSize)
}

func (br *BucketReader) ManualScan(lg *log.Logger, ctx context.Context, ot *objectTracker, queue chan<- *s3.Object) (err error) {
	lg.Info("manual scan started", log.KV("bucket", br.Name))

	//list the objects in the bucket
	req := s3.ListObjectsV2Input{
		Bucket: aws.String(br.Bucket_Name),
	}
	if br.prefixFilter != `` {
		req.Prefix = aws.String(br.prefixFilter)
	}

	var count uint64

	var lerr error
	objListHandler := func(resp *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, item := range resp.Contents {
			select {
			case queue <- item:
				count++
			case <-ctx.Done():
				return false
			}
			if ctx.Err() != nil {
				return false
			}
		}
		return true
	}

	if err = br.svc.ListObjectsV2Pages(&req, objListHandler); err == nil {
		err = lerr
	}

	lg.Info("manual scan completed", log.KV("bucket", br.Name), log.KV("object_count", count))
	return
}

func (br *BucketReader) worker(lg *log.Logger, ctx context.Context, ot *objectTracker, queue <-chan *s3.Object, wg *sync.WaitGroup) {
	lg.Info("manual scan worker started", log.KV("bucket", br.Name))
	defer wg.Done()

	var processed, alreadyProcessed, skipped, errored uint64

	for item := range queue {
		if item == nil {
			return
		}

		//do a quick check for stupidity
		if item.Size == nil || item.LastModified == nil || item.Key == nil {
			skipped++
			continue
		}
		sz, lm, key := *item.Size, *item.LastModified, *item.Key
		if sz == 0 || !br.ShouldTrack(key) {
			skipped++
			continue //skip empty objects or things we should not track
		}
		//lookup the object in the objectTracker
		state, ok := ot.Get(br.Bucket_Name, key)
		if ok && state.Updated.Equal(lm) {
			alreadyProcessed++
			continue //already handled this
		}

		//ok, lets process this thing
		if objsz, s3rtt, rtt, err := br.Process(item, ctx); err != nil {
			br.Logger.Error("failed to process object",
				log.KV("name", br.Name),
				log.KV("object", key),
				log.KV("tag", br.TagName),
				log.KVErr(err))
			errored++
			continue
		} else {
			br.Logger.Info("consumed object",
				log.KV("name", br.Name),
				log.KV("object", key),
				log.KV("tag", br.TagName),
				log.KV("s3-rtt", s3rtt),
				log.KV("rtt", rtt),
				log.KV("size", objsz))
			processed++
		}
		state = trackedObjectState{
			Updated: lm,
			Size:    sz,
		}
		err := ot.Set(br.Bucket_Name, key, state, false)
		if err != nil {
			br.Logger.Error("failed to update state",
				log.KV("name", br.Name),
				log.KV("object", key),
				log.KVErr(err))
		}
		if ctx.Err() != nil {
			break
		}
	}
	lg.Info("manual scan worker completed",
		log.KV("bucket", br.Name),
		log.KV("num_processed", processed),
		log.KV("num_already_processed", alreadyProcessed),
		log.KV("num_skipped", skipped),
		log.KV("num_errored", errored))
}

func (ac *AuthConfig) validate() (err error) {
	if ac.Region == `` {
		err = errors.New("missing region")
		return
	}

	//make sure max retries is sane
	if ac.MaxRetries <= 0 || ac.MaxRetries > maxMaxRetries {
		ac.MaxRetries = defaultMaxRetries
	}

	//check if we have an Endpoint specified
	// this is for non-AWS S3 endpoints, we require a little more config here
	if ac.Endpoint != `` {
		//user is explicitely setting a URL, so make sure they didn't set a region or ARN
		if ac.Bucket_ARN != `` {
			err = errors.New("Endpoint and Bucket-ARN are mutually exclusive")
			return
		}
		if ac.Bucket_Name == `` {
			err = errors.New("Bucket-Name is required when using custom Endpoint")
			return
		}
	} else {
		//ok, we MUST have an ARN
		if ac.Bucket_ARN == `` {
			if ac.Bucket_URL != `` {
				//handle some REALLY old pre-alpha code that specified as URL instead of ARN
				ac.Bucket_ARN = ac.Bucket_URL
			} else {
				err = errors.New("missing Bucket-ARN")
				return
			}
		}
		//make sure the Bucket Name is good
		if ac.Bucket_Name == `` {
			if ac.Bucket_Name, err = getBucketName(ac.Bucket_ARN); err != nil {
				return
			}
		}
	}

	// we can potentially talk to something now
	return
}

func (bc BucketConfig) Log(vals ...interface{}) {
	if bc.Logger == nil || len(vals) == 0 {
		return
	}
	bc.Logger.Info(fmt.Sprint(vals...))
}

func (ac *AuthConfig) getSession(lgr aws.Logger, c *credentials.Credentials) (sess *session.Session, err error) {
	//prevalidate first
	if err = ac.validate(); err != nil {
		return
	}

	cfg := aws.Config{
		MaxRetries:  aws.Int(ac.MaxRetries),
		Credentials: c,
		DisableSSL:  aws.Bool(ac.Disable_TLS),
		Region:      aws.String(ac.Region),
		Logger:      lgr,
	}
	if ac.Endpoint != `` {
		//using a custom endpoint, wire that up
		cfg.Endpoint = aws.String(ac.Endpoint)
		cfg.S3ForcePathStyle = aws.Bool(ac.S3_Force_Path_Style)
	} else {
		//use ARN and potentially a Region
		if ac.Region == `` {
			cfg.S3UseARNRegion = aws.Bool(true)
		}
	}
	sess, err = session.NewSession(&cfg)
	return
}
