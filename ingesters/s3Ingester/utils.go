package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/buger/jsonparser"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	lineReader       reader = `line`
	cloudtrailReader reader = `cloudtrail`
)

var (
	ErrUnknownReader = errors.New("Unknown reader")
)

type reader string

type matcher struct {
	patterns []string
}

func newMatcher(patterns []string) (*matcher, error) {
	for _, v := range patterns {
		if _, err := doublestar.Match(v, `foobar`); err != nil {
			return nil, fmt.Errorf("file pattern %q is invalid %v", v, err)
		}
	}
	return &matcher{
		patterns: patterns,
	}, nil
}

func (m *matcher) match(name string) (matched bool) {
	if m == nil {
		return
	}
	if m.patterns == nil {
		matched = true
	} else {
		for _, v := range m.patterns {
			if ok, err := doublestar.Match(v, name); err == nil && ok {
				matched = true
				break
			}
		}
	}
	return
}

func (m *matcher) addPattern(v string) (err error) {
	if _, err = filepath.Match(v, `foobar`); err == nil {
		m.patterns = append(m.patterns, v)
	}
	return
}

type objectTracker struct {
	sync.Mutex
	flushed   bool
	statePath string
	states    map[string]bucketObjects
}

type bucketObjects map[string]trackedObjectState

type trackedObjectState struct {
	Updated time.Time
	Size    int64
}

func NewObjectTracker(pth string) (ot *objectTracker, err error) {
	if pth == `` {
		err = errors.New("invalid path")
		return
	}
	states := map[string]bucketObjects{}
	var fin *os.File
	if fin, err = os.Open(pth); err != nil {
		if !os.IsNotExist(err) {
			return
		}
		//all good, just empty
		err = nil
	} else if err = json.NewDecoder(fin).Decode(&states); err != nil {
		fin.Close()
		err = fmt.Errorf("state file is corrupt %w", err)
		return
	} else if err = fin.Close(); err != nil {
		err = fmt.Errorf("failed to close state file %w", err)
		return
	}
	ot = &objectTracker{
		flushed:   true,
		statePath: pth,
		states:    states,
	}
	return
}

func (ot *objectTracker) Flush() (err error) {
	ot.Lock()
	if ot.flushed { //no need to flush
		ot.Unlock()
		return
	}
	bb := bytes.NewBuffer(nil)
	if err = json.NewEncoder(bb).Encode(ot.states); err == nil {
		tpath := ot.statePath + `.temp`
		if err = os.WriteFile(tpath, bb.Bytes(), 0660); err == nil {
			if err = os.Rename(tpath, ot.statePath); err != nil {
				err = fmt.Errorf("failed to update state file with temporary file: %w", err)
			} else {
				ot.flushed = true
			}
			//else all good

		} else {
			err = fmt.Errorf("failed to write temporary state file %w", err)
		}
	} else {
		err = fmt.Errorf("failed to encode states %w", err)
	}
	ot.Unlock()
	return
}

func (ot *objectTracker) Set(bucket, obj string, state trackedObjectState, forceFlush bool) (err error) {
	ot.Lock()
	bkt, ok := ot.states[bucket]
	if !ok || bkt == nil {
		bkt = bucketObjects{}
	}
	bkt[obj] = state
	ot.states[bucket] = bkt
	ot.flushed = false
	ot.Unlock()
	if forceFlush {
		err = ot.Flush()
	}
	return
}

func (ot *objectTracker) Get(bucket, obj string) (state trackedObjectState, ok bool) {
	var bkt bucketObjects
	ot.Lock()
	if bkt, ok = ot.states[bucket]; ok && bkt != nil {
		state, ok = bkt[obj]
	}
	ot.Unlock()
	return
}

func parseReader(v string) (reader, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch reader(v) {
	case ``: //empty means line
		return lineReader, nil
	case lineReader:
		return lineReader, nil
	case cloudtrailReader:
		return cloudtrailReader, nil
	}
	return ``, ErrUnknownReader
}

// ARN is designed to try and figure out the bucket name from either a pure bucket name
// bucket HTTP url, or amazon ARN specification
// Examples include:
// https://<bucketname>.s3.amazonaws.com
// arn:aws:s3:::<bucketname>
// <bucketname>
// <bucketname>.s3.amazonaws.com
// s3://<bucketname>
// http(s)://[host]/<bucketname>/more/path/variables
func getBucketName(v string) (bucketName string, err error) {
	//properly formed ARN
	if strings.HasPrefix(v, `arn:aws:s3`) {
		var vv arn.ARN
		if vv, err = arn.Parse(v); err != nil {
			return
		} else if vv.Service != `s3` {
			err = fmt.Errorf("invalid ARN service %s", vv.Service)
			return
		}
		bucketName = vv.Resource
		return
	}
	//check for a URL scheme
	if strings.Contains(v, `://`) {
		//parse as URL and extract the starting name
		var uri *url.URL
		if uri, err = url.Parse(v); err != nil {
			return
		} else if uri == nil {
			err = fmt.Errorf("invalid bucket URL or ARN: %s", v)
			return
		}
		switch uri.Scheme {
		case `s3`:
			bucketName = uri.Host
			return
		case `http`:
			fallthrough
		case `https`:
			//potentially move port
			var host string
			if host, _, err = net.SplitHostPort(uri.Host); err != nil {
				host = uri.Host
				err = nil
			}
			if strings.HasSuffix(host, `.s3.amazonaws.com`) {
				bucketName = strings.TrimSuffix(uri.Host, `.s3.amazonaws.com`)
			} else {
				//try to resolve the bucket based on the URL path
				//grab the first element of the path
				if bucketName = basePath(uri.Path); bucketName == `` {
					err = fmt.Errorf("Failed to extract the Bucket for URL %q", v)
				}
			}
			return
		default:
			err = errors.New("Unknown ARN scheme")
			return
		}
	} else {
		//good luck
		if strings.HasSuffix(v, `.s3.amazonaws.com`) {
			v = strings.TrimSuffix(v, `.s3.amazonaws.com`)
		}
		bucketName = v
	}

	return
}

func basePath(orig string) (s string) {
	if s = path.Clean(orig); s == `.` {
		s = ``
		return
	}
	for {
		dir, file := path.Split(s)
		if dir == `.` || dir == `/` || dir == `` {
			s = file
			break
		} else if s = path.Clean(dir); s == `.` {
			s = ``
			break
		}
	}
	return
}

var (
	awsUrlRegex = regexp.MustCompile(`s3[-\.]?([a-zA-Z\-0-9]+)?\.amazonaws\.com`)
)

func ProcessContext(obj *s3.Object, ctx context.Context, svc *s3.S3, bucket string, rdr reader, tg *timegrinder.TimeGrinder, src net.IP, tag entry.EntryTag, proc *processors.ProcessorSet, maxLineSize int) (err error) {
	var r *s3.GetObjectOutput
	r, err = svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    obj.Key,
	})
	if err != nil {
		return
	}
	defer r.Body.Close()

	switch rdr {
	case lineReader:
		err = processLinesContext(ctx, r.Body, maxLineSize, tg, src, tag, proc)
	case cloudtrailReader:
		err = processCloudtrailContext(ctx, r.Body, tg, src, tag, proc)
	default:
		err = errors.New("no reader set")
	}
	return
}

func processLinesContext(ctx context.Context, rdr io.Reader, maxLineSize int, tg *timegrinder.TimeGrinder, src net.IP, tag entry.EntryTag, proc *processors.ProcessorSet) (err error) {
	sc := bufio.NewScanner(rdr)
	sc.Buffer(nil, maxLineSize)
	for sc.Scan() {
		bts := sc.Bytes()
		if len(bts) == 0 {
			continue
		}
		ts, ok, _ := tg.Extract(bts)
		if !ok {
			ts = time.Now()
		}
		ent := entry.Entry{
			TS:   entry.FromStandard(ts),
			SRC:  src,                         //may be nil, ingest muxer will handle if it is
			Data: append([]byte(nil), bts...), //scanner re-uses the buffer
			Tag:  tag,
		}
		if ctx != nil {
			err = proc.ProcessContext(&ent, ctx)
		} else {
			err = proc.Process(&ent)
		}
		if err != nil {
			return //just leave
		}
	}

	return
}

func processCloudtrailContext(ctx context.Context, rdr io.Reader, tg *timegrinder.TimeGrinder, src net.IP, tag entry.EntryTag, proc *processors.ProcessorSet) (err error) {
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
		ts, ok, _ := tg.Extract(bts)
		if !ok {
			ts = time.Now()
		}
		ent := entry.Entry{
			TS:   entry.FromStandard(ts),
			SRC:  src,                         //may be nil, ingest muxer will handle if it is
			Data: append([]byte(nil), val...), //scanner re-uses the buffer
			Tag:  tag,
		}
		if ctx != nil {
			cberr = proc.ProcessContext(&ent, ctx)
		} else {
			cberr = proc.Process(&ent)
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

func logSnsKeyDecode(lg *log.Logger, keytype string, buckets, keys []string) {
	if len(buckets) != len(keys) {
		lg.Info("successfully decoded messages", log.KV("type", keytype), log.KV("buckets", buckets), log.KV("keys", keys))
	} else {
		for i := 0; i < len(buckets); i++ {
			lg.Info("successfully decoded message", log.KV("type", keytype), log.KV("bucket", buckets[i]), log.KV("key", keys[i]))
		}
	}
	return
}
