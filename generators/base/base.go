/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package base

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

var (
	tagName         = flag.String("tag-name", "", "Tag name for ingested data")
	clearConns      = flag.String("clear-conns", "", "Comma-separated server:port list of cleartext targets")
	tlsConns        = flag.String("tls-conns", "", "Comma-separated server:port list of TLS connections")
	pipeConns       = flag.String("pipe-conns", "", "Comma-separated list of paths for named pipe connection")
	tlsRemoteVerify = flag.String("tls-remote-verify", "", "Path to remote public key to verify against")
	ingestSecret    = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	ingestTenant    = flag.String("ingest-tenant", "", "Ingest tenant ID, blank for system tenant")
	compression     = flag.Bool("compression", false, "Enable ingest compression")
	entryCount      = flag.Int("entry-count", 100, "Number of entries to generate")
	streaming       = flag.Bool("stream", false, "Stream entries in")
	rawConn         = flag.String("raw-connection", "", "Deliver line broken entries over a TCP connection instead of gravwell protocol")
	hecTarget       = flag.String("hec-target", "", "Target a HEC endpoint")
	hecModeRaw      = flag.Bool("hec-mode-raw", false, "Send events to the raw HEC endpoint")
	span            = flag.String("duration", "1h", "Total Duration")
	srcOverride     = flag.String("source-override", "", "Source override value")
	status          = flag.Bool("status", false, "show ingest rates as we run")
	startTime       = flag.String("start-time", "", "optional starting timestamp for entries, must be RFC3339 format")
)

var (
	parsed bool
	cfg    GeneratorConfig
)

type GeneratorConfig struct {
	ok          bool
	modeRaw     bool
	modeHEC     bool
	modeHECRaw  bool
	Raw         string
	HEC         string
	Streaming   bool
	Compression bool
	Tag         string
	ConnSet     []string
	Auth        string
	Tenant      string
	Count       uint64
	Duration    time.Duration
	Start       time.Time
	SRC         net.IP
	Logger      *log.Logger
	LogLevel    log.Level
}

func GetGeneratorConfig(defaultTag string) (gc GeneratorConfig, err error) {
	if !parsed {
		flag.Parse()
		parsed = true
	}
	if cfg.ok {
		return cfg, nil
	}
	gc.LogLevel = log.OFF
	gc.Streaming = *streaming
	gc.Count = uint64(*entryCount)
	if *span == "" {
		err = errors.New("Missing duration")
		return
	}
	if gc.Duration, err = getDuration(*span); err != nil {
		err = fmt.Errorf("invalid duration %s %w", *span, err)
		return
	}
	if gc.Start, err = getStartTime(*startTime); err != nil {
		err = fmt.Errorf("invalid start-time %s %w", *startTime, err)
		return
	}

	if *rawConn != `` {
		if _, _, err = net.SplitHostPort(*rawConn); err != nil {
			err = fmt.Errorf("invalid raw connection string %q - %v", *rawConn, err)
			return
		}
		gc.modeRaw = true
		gc.Raw = *rawConn
		gc.ok = true
		gc.Tag = entry.DefaultTagName
		return
	}
	gc.Compression = *compression
	if gc.Tag = *tagName; gc.Tag == `` {
		if gc.Tag = defaultTag; gc.Tag == `` {
			err = errors.New("A tag name must be specified")
			return

		}
	}
	if err = ingest.CheckTag(gc.Tag); err != nil {
		return
	}

	if gc.Auth = *ingestSecret; gc.Auth == `` {
		err = errors.New("Ingest auth is missing")
		return
	}
	gc.Tenant = *ingestTenant

	if *hecTarget != `` {
		gc.modeHEC = true
		gc.modeHECRaw = *hecModeRaw
		gc.HEC = *hecTarget
		gc.ok = true
		return
	}

	var connSet []string

	if *clearConns != "" {
		for _, conn := range strings.Split(*clearConns, ",") {
			conn = config.AppendDefaultPort(strings.TrimSpace(conn), config.DefaultCleartextPort)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("tcp://%s", conn))
			}
		}
	}
	if *tlsConns != "" {
		for _, conn := range strings.Split(*tlsConns, ",") {
			conn = config.AppendDefaultPort(strings.TrimSpace(conn), config.DefaultTLSPort)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("tls://%s", conn))
			}
		}
	}
	if *pipeConns != "" {
		for _, conn := range strings.Split(*pipeConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("pipe://%s", conn))
			}
		}
	}
	if len(connSet) <= 0 {
		err = errors.New("No connections were specified. We need at least one")
		return
	}
	gc.ConnSet = connSet
	if *entryCount <= 0 {
		err = errors.New("invalid entry count")
		return
	}
	if *srcOverride != `` {
		if src := net.ParseIP(*srcOverride); src == nil {
			gc.SRC = src
		}
	}
	gc.ok = true
	cfg = gc
	return
}

type GeneratorConn interface {
	Close() error
	GetTag(string) (entry.EntryTag, error)
	NegotiateTag(string) (entry.EntryTag, error)
	LookupTag(entry.EntryTag) (string, bool)
	WaitForHot(time.Duration) error
	Write(entry.Timestamp, entry.EntryTag, []byte) error
	WriteBatch([]*entry.Entry) error
	WriteEntry(*entry.Entry) error
	Sync(time.Duration) error
	SourceIP() (net.IP, error)
}

func NewIngestMuxer(name, guid string, gc GeneratorConfig, to time.Duration) (conn GeneratorConn, src net.IP, err error) {
	if !gc.ok {
		err = errors.New("config is invalid")
		return
	} else if gc.modeRaw {
		if conn, err = newRawConn(gc, to); err == nil {
			src, err = conn.SourceIP()
		}
		return
	} else if gc.modeHEC {
		if conn, err = newHecConn(name, gc, to); err == nil {
			src, err = conn.SourceIP()
		}
		return
	}
	if name == `` {
		name = os.Args[0]
	}
	if guid == `` {
		guid = uuid.New().String()
	} else if _, err = uuid.Parse(guid); err != nil {
		return
	}
	lgr := gc.Logger
	if lgr == nil {
		lgr = log.NewDiscardLogger()
	}
	if to < 0 {
		to = time.Second
	}

	umc := ingest.UniformMuxerConfig{
		Destinations:  gc.ConnSet,
		Tags:          []string{gc.Tag},
		Auth:          gc.Auth,
		Tenant:        gc.Tenant,
		IngesterName:  name,
		IngesterUUID:  guid,
		IngesterLabel: `generator`,
		Logger:        lgr,
		LogLevel:      gc.LogLevel.String(),
		IngestStreamConfig: config.IngestStreamConfig{
			Enable_Compression: gc.Compression,
		},
	}
	var igst *ingest.IngestMuxer
	if igst, err = ingest.NewUniformMuxer(umc); err != nil {
		return
	} else if err = igst.Start(); err != nil {
		igst.Close()
		return
	} else if err = igst.WaitForHot(to); err != nil {
		igst.Close()
		return
	}
	if gc.SRC != nil {
		src = gc.SRC
	} else if src, err = igst.SourceIP(); err != nil {
		igst.Close()
	}

	if err == nil {
		conn = igst
	}

	return
}

type dursuffix struct {
	suffix string
	mult   time.Duration
}

func getDuration(v string) (d time.Duration, err error) {
	v = strings.ToLower(strings.TrimSpace(v))
	dss := []dursuffix{
		dursuffix{suffix: `s`, mult: time.Second},
		dursuffix{suffix: `m`, mult: time.Minute},
		dursuffix{suffix: `h`, mult: time.Hour},
		dursuffix{suffix: `d`, mult: 24 * time.Hour},
		dursuffix{suffix: `w`, mult: 24 * 7 * time.Hour},
	}
	for _, ds := range dss {
		if strings.HasSuffix(v, ds.suffix) {
			v = strings.TrimSuffix(v, ds.suffix)
			var x int64
			if x, err = strconv.ParseInt(v, 10, 64); err != nil {
				return
			}
			if x <= 0 {
				err = errors.New("Duration must be > 0")
				return
			}
			d = time.Duration(x) * ds.mult
			return
		}
	}
	err = errors.New("Unknown duration suffix")
	return
}

func getStartTime(v string) (t time.Time, err error) {
	if v != `` {
		t, err = time.Parse(time.RFC3339, v)
	}
	return
}

type DataGen func(time.Time) []byte
type Finalizer func(*entry.Entry)

func OneShot(conn GeneratorConn, tag entry.EntryTag, src net.IP, cfg GeneratorConfig, dg DataGen, f Finalizer) (totalCount, totalBytes uint64, err error) {
	if dg == nil || cfg.Count == 0 || cfg.Duration < 0 {
		err = errors.New("invalid parameters")
		return
	}
	if src == nil {
		if src, err = conn.SourceIP(); err != nil {
			err = fmt.Errorf("Failed to get source ip: %w", err)
			return
		}
	}
	if *status {
		su, _ := newStatusUpdater(&totalCount, &totalBytes)
		su.Start()
		defer su.Stop()
	}
	sp := cfg.Duration / time.Duration(cfg.Count)
	var ts time.Time
	if ts = cfg.Start; ts.IsZero() {
		ts = time.Now().Add(-1 * cfg.Duration)
	}
	for i := uint64(0); i < cfg.Count; i++ {
		ent := &entry.Entry{
			TS:  entry.FromStandard(ts),
			Tag: tag,
			SRC: src,
		}
		if dg != nil {
			ent.Data = dg(ts)
		}
		if f != nil {
			f(ent)
		}
		if err = conn.WriteEntry(ent); err != nil {
			break
		}
		ts = ts.Add(sp)
		totalBytes += uint64(ent.Size())
		totalCount++
	}
	return
}

func Stream(conn GeneratorConn, tag entry.EntryTag, src net.IP, cfg GeneratorConfig, dg DataGen, f Finalizer) (totalCount, totalBytes uint64, err error) {
	var stop bool
	r := make(chan error, 1)
	go func(ret chan error, stp *bool) {
		var err error
		totalCount, totalBytes, err = streamRunner(conn, tag, src, cfg.Count, stp, dg, f)
		r <- err
	}(r, &stop)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	select {
	case _ = <-c:
		stop = true
		select {
		case err = <-r:
		case _ = <-time.After(3 * time.Second):
			err = errors.New("Timed out waiting for exit")
		}
	case err = <-r:
	}
	return
}

func streamRunner(conn GeneratorConn, tag entry.EntryTag, src net.IP, cnt uint64, stop *bool, dg DataGen, f Finalizer) (totalCount, totalBytes uint64, err error) {
	if dg == nil || conn == nil || stop == nil {
		err = errors.New("invalid parameters")
		return
	}
	if src == nil {
		if src, err = conn.SourceIP(); err != nil {
			err = fmt.Errorf("Failed to get source ip: %w", err)
			return
		}
	}
	sp := time.Second / time.Duration(cnt)
	var ent *entry.Entry
loop:
	for !*stop {
		ts := time.Now()
		start := ts
		for i := uint64(0); i < cnt; i++ {
			ent = &entry.Entry{
				TS:  entry.FromStandard(ts),
				Tag: tag,
				SRC: src,
			}
			if dg != nil {
				ent.Data = dg(ts)
			}
			if f != nil {
				f(ent)
			}
			if err = conn.WriteEntry(ent); err != nil {
				break loop
			}
			totalBytes += uint64(ent.Size())
			totalCount++
			ts = ts.Add(sp)
		}
		time.Sleep(time.Second - time.Since(start))
	}
	return
}
