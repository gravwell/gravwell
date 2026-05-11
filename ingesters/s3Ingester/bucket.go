package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/sqs_common"
	"github.com/gravwell/gravwell/v3/timegrinder"
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

// loadConfig builds an aws.Config from the AuthConfig, applying any credentials,
// retry limits, as well as TLS settings. S3-specific options like paht style, ARN region,
// and endpoint are handled separately via s3ClientOpts so this config can be reused for any
// AWS-based service client.
func (ac *AuthConfig) loadConfig(ctx context.Context, c aws.CredentialsProvider) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(ac.Region),
		config.WithRetryMaxAttempts(ac.MaxRetries),
	}
	if c != nil {
		opts = append(opts, config.WithCredentialsProvider(c))
	}
	if ac.Disable_TLS {
		opts = append(opts, config.WithHTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

// s3ClientOpts returns S3-specific client options from the AuthConfig.
// These are passed to s3.NewFromConfig and cannot be expressed as global
// aws.Config options.
func (ac *AuthConfig) s3ClientOpts() []func(*s3.Options) {
	var opts []func(*s3.Options)
	if ac.Endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = new(ac.Endpoint)
			o.UsePathStyle = ac.S3_Force_Path_Style
		})
	} else if ac.Region == "" {
		// UseARNRegion lets the s3 client use the region embedded in a bucket
		// ARN rather than the config region.
		// This isn't likely to be hit, but just playing defensively here.
		opts = append(opts, func(o *s3.Options) {
			o.UseARNRegion = true
		})
	}
	return opts
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
	AttachMetadata   bool
}

type BucketReader struct {
	BucketConfig
	prefixFilter string
	svc          s3Handler
	filter       *matcher
	tg           timegrinder.TimeGrinder
	src          net.IP
	rdr          reader
}

func NewBucketReader(ctx context.Context, cfg BucketConfig) (br *BucketReader, err error) {
	var rdr reader
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

	awsCfg, err := cfg.AuthConfig.loadConfig(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("load auth config: %w", err)
	}

	br = &BucketReader{
		BucketConfig: cfg,
		svc:          s3.NewFromConfig(awsCfg, cfg.AuthConfig.s3ClientOpts()...),
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
	paginator := s3.NewListObjectsV2Paginator(br.svc, &s3.ListObjectsV2Input{
		Bucket:  new(br.Bucket_Name),
		MaxKeys: new(int32(1)),
	})
	_, err := paginator.NextPage(ctx)
	return err
}

// ShouldTrack just checks if we should process this file
func (br *BucketReader) ShouldTrack(obj string) (ok bool) {
	//first check if this is in our filters
	ok = br.filter.match(obj)
	return //nope
}

// Process reads the object in and processes its contents
func (br *BucketReader) Process(obj types.Object, ctx context.Context) (sz int64, s3rtt, rtt time.Duration, err error) {
	return ProcessContext(
		obj,
		ctx,
		br.svc,
		br.Bucket_Name,
		br.rdr,
		br.TG,
		br.src,
		br.Tag,
		br.Proc,
		br.MaxLineSize,
		br.AttachMetadata,
	)
}

func (br *BucketReader) ManualScan(
	lg *log.Logger,
	ctx context.Context,
	ot *objectTracker,
	queue chan<- types.Object,
) (err error) {
	lg.Info("manual scan started", log.KV("bucket", br.Name))

	//list the objects in the bucket
	req := s3.ListObjectsV2Input{
		Bucket: new(br.Bucket_Name),
	}
	if br.prefixFilter != `` {
		req.Prefix = new(br.prefixFilter)
	}

	var count uint64

	paginator := s3.NewListObjectsV2Paginator(br.svc, &req)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("handle page: %w", err)
		}
		for _, item := range page.Contents {
			select {
			case queue <- item:
				count++
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	lg.Info("manual scan completed", log.KV("bucket", br.Name), log.KV("object_count", count))
	return
}

func (br *BucketReader) worker(
	lg *log.Logger,
	ctx context.Context,
	ot *objectTracker,
	queue <-chan types.Object,
	wg *sync.WaitGroup,
) {
	lg.Info("manual scan worker started", log.KV("bucket", br.Name))
	defer wg.Done()

	var processed, alreadyProcessed, skipped, errored uint64

	for item := range queue {
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
