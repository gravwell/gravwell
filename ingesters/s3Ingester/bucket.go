package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/buger/jsonparser"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	maxMaxRetries      = 10
	defaultMaxRetries  = 3
	defaultMaxLineSize = 4 * 1024 * 1024
	defaultRegion      = `us-east-1`
)

type AuthConfig struct {
	ID                  string `json:"-"` //do not ship this as part of a config report
	Secret              string `json:"-"` //do not ship this as part of a config report
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
	Verbose        bool
	MaxLineSize    int
	Reader         string //defaults to line
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
	if sess, err = cfg.AuthConfig.getSession(cfg); err != nil {
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
func (br *BucketReader) Process(obj *s3.Object) (err error) {
	return br.ProcessContext(obj, nil)
}

func (br *BucketReader) ProcessContext(obj *s3.Object, ctx context.Context) (err error) {
	var r *s3.GetObjectOutput
	r, err = br.svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(br.Bucket_Name),
		Key:    obj.Key,
	})
	if err != nil {
		return
	}
	defer r.Body.Close()
	switch br.rdr {
	case lineReader:
		err = br.processLinesContext(ctx, r.Body)
	case cloudtrailReader:
		err = br.processCloudtrailContext(ctx, r.Body)
	default:
		err = errors.New("no reader set")
	}
	return
}

func (br *BucketReader) ManualScan(ctx context.Context, ot *objectTracker) (err error) {
	//list the objects in the bucket
	req := s3.ListObjectsV2Input{
		Bucket: aws.String(br.Bucket_Name),
	}
	if br.prefixFilter != `` {
		req.Prefix = aws.String(br.prefixFilter)
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
			state, ok := ot.Get(br.Bucket_Name, key)
			if ok && state.Updated.Equal(lm) {
				continue //already handled this
			}

			//ok, lets process this thing
			if lerr = br.Process(item); lerr != nil {
				br.Logger.Error("failed to process object",
					log.KV("name", br.Name),
					log.KV("object", key),
					log.KVErr(err))
				return false //quit the scan
			} else {
				br.Logger.Info("consumed object",
					log.KV("name", br.Name),
					log.KV("object", key),
					log.KV("size", sz))
			}
			state = trackedObjectState{
				Updated: lm,
				Size:    sz,
			}
			lerr = ot.Set(br.Bucket_Name, key, state, false)
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

func (br *BucketReader) processLinesContext(ctx context.Context, rdr io.Reader) (err error) {
	sc := bufio.NewScanner(rdr)
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

func (br *BucketReader) processCloudtrailContext(ctx context.Context, rdr io.Reader) (err error) {
	var obj json.RawMessage
	dec := json.NewDecoder(rdr)

	var cberr error
	cb := func(val []byte, vt jsonparser.ValueType, off int, lerr error) {
		if lerr != nil {
			cberr = lerr
			return
		}
		var bts []byte
		// if our record is an object try to grab a handle on the eventTime member
		// if not, just take the whole thing, this is an optimization to process timestamps
		if vt == jsonparser.Object {
			if eventTime, err := jsonparser.GetString(val, `eventTime`); err == nil {
				bts = []byte(eventTime)
			} else {
				bts = val // could not match, just set to whole thing and let TG do its thing
			}
		} else {
			bts = val
		}
		ts, ok, _ := br.TG.Extract(bts)
		if !ok {
			ts = time.Now()
		}
		ent := entry.Entry{
			TS:   entry.FromStandard(ts),
			SRC:  br.src,                      //may be nil, ingest muxer will handle if it is
			Data: append([]byte(nil), val...), //scanner re-uses the buffer
			Tag:  br.Tag,
		}
		if ctx != nil {
			cberr = br.Proc.ProcessContext(&ent, ctx)
		} else {
			cberr = br.Proc.Process(&ent)
		}
		return
	}

	for {
		var recordarray []byte
		var dt jsonparser.ValueType
		if err = dec.Decode(&obj); err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
		if recordarray, dt, _, err = jsonparser.Get([]byte(obj), `Records`); err != nil {
			err = fmt.Errorf("failed to find Records array in cloudtrail log: %v", err)
			break
		} else if dt != jsonparser.Array {
			err = fmt.Errorf("Records member is an invalid type: %v", dt)
			break
		}
		if _, err = jsonparser.ArrayEach(recordarray, cb); err != nil {
			break
		} else if cberr != nil {
			err = cberr
			break
		}
	}
	return
}

func (ac *AuthConfig) validate() (err error) {
	// ID and secret are required
	if ac.ID == `` {
		err = errors.New("missing ID")
		return
	} else if ac.Secret == `` {
		err = errors.New("missing secret")
		return
	} else if ac.Region == `` {
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

func (ac *AuthConfig) getSession(lgr aws.Logger) (sess *session.Session, err error) {
	//prevalidate first
	if err = ac.validate(); err != nil {
		return
	}
	cfg := aws.Config{
		MaxRetries:  aws.Int(ac.MaxRetries),
		Credentials: credentials.NewStaticCredentials(ac.ID, ac.Secret, ``),
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
