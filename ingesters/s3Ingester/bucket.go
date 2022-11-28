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
)

type AuthConfig struct {
	ID         string
	Secret     string
	Region     string
	Bucket_URL string `json:"-"` //DEPRECATED, DO NOT USE this is an artifact from initial version, my bad
	Bucket_ARN string // Amazon ARN (should be JUST the bucket ARN
	MaxRetries int
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
	arn     string
	session *session.Session
	svc     *s3.S3
	filter  *matcher
	tg      timegrinder.TimeGrinder
	src     net.IP
	rdr     reader
}

func NewBucketReader(cfg BucketConfig) (br *BucketReader, err error) {
	var arn string
	var rdr reader
	if err = cfg.validate(); err != nil {
		return
	} else if arn, err = getARN(cfg.AuthConfig.Bucket_ARN); err != nil {
		return
	}
	var filter *matcher
	if filter, err = newMatcher(cfg.FileFilters); err != nil {
		return
	}
	if rdr, err = parseReader(cfg.Reader); err != nil {
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
		arn:          arn,
		session:      sess,
		svc:          svc,
		filter:       filter,
		src:          cfg.srcOverride(),
		rdr:          rdr,
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
		return
	} else if ac.ID == `` {
		err = errors.New("missing ID")
		return
	} else if ac.Secret == `` {
		err = errors.New("missing secret")
		return
	}

	if ac.Bucket_ARN == `` && ac.Bucket_URL != `` {
		ac.Bucket_ARN = ac.Bucket_URL
	}
	if ac.Bucket_ARN == `` {
		err = errors.New("missing bucket ARN")
		return
	} else if _, err = getARN(ac.Bucket_ARN); err != nil {
		return
	}

	if ac.MaxRetries <= 0 || ac.MaxRetries > maxMaxRetries {
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
		Bucket: aws.String(br.arn),
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
		Bucket: aws.String(br.arn),
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
			state, ok := ot.Get(br.arn, key)
			if ok && state.Updated.Equal(lm) {
				continue //already handled this
			}

			//ok, lets process this thing
			if lerr = br.Process(item); lerr != nil {
				br.Logger.Error("failed to process object",
					log.KV("bucket", br.Name),
					log.KV("arn", br.arn),
					log.KV("object", key),
					log.KVErr(err))
				return false //quit the scan
			} else {
				br.Logger.Info("consumed object",
					log.KV("bucket", br.Name),
					log.KV("arn", br.arn),
					log.KV("object", key),
					log.KV("size", sz))
			}
			state = trackedObjectState{
				Updated: lm,
				Size:    sz,
			}
			lerr = ot.Set(br.arn, key, state, false)
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
