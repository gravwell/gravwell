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
	"math/rand"
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
	tagName      = flag.String("tag-name", "", "Tag name for ingested data")
	clearConns   = flag.String("clear-conns", "", "Comma-separated server:port list of cleartext targets")
	tlsConns     = flag.String("tls-conns", "", "Comma-separated server:port list of TLS connections")
	pipeConns    = flag.String("pipe-conns", "", "Comma-separated list of paths for named pipe connection")
	tlsNoVerify  = flag.Bool("insecure-no-tls-validate", false, "optionally disable remote TLS validation on ciphertext connections")
	ingestSecret = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	ingestTenant = flag.String("ingest-tenant", "", "Ingest tenant ID, blank for system tenant")
	compression  = flag.Bool("compression", false, "Enable ingest compression")
	entryCount   = flag.Int("entry-count", 100, "Number of entries to generate")
	streaming    = flag.Bool("stream", false, "Stream entries in")
	rawTCPConn   = flag.String("raw-tcp-connection", "", "Deliver line broken entries over a TCP connection instead of gravwell protocol")
	rawUDPConn   = flag.String("raw-udp-connection", "", "Deliver line broken entries over a UDP connection instead of gravwell protocol")
	hecTarget    = flag.String("hec-target", "", "Target a HEC endpoint")
	hecModeRaw   = flag.Bool("hec-mode-raw", false, "Send events to the raw HEC endpoint")
	span         = flag.String("duration", "1h", "Total Duration")
	srcOverride  = flag.String("source-override", "", "Source override value")
	status       = flag.Bool("status", false, "show ingest rates as we run")
	startTime    = flag.String("start-time", "", "optional starting timestamp for entries, must be RFC3339 format")
	chaos        = flag.Bool("chaos-mode", false, "Chaos mode causes the generator to not do multiline HTTP uploads and sometimes send crazy timestamps")
	chaosWorkers = flag.Int("chaos-mode-workers", 8, "Maximum number of workers when in chaos mode")
	tsPsychoMode = flag.Bool("time-is-an-illusion", false, "Ingest with worst-case timestamp ordering (this is a chaos-mode flag)")
)

var (
	parsed bool
	cfg    GeneratorConfig
)

type GeneratorConfig struct {
	ok                    bool
	modeRawTCP            bool
	modeRawUDP            bool
	modeHEC               bool
	modeHECRaw            bool
	ChaosTimestamps       bool
	ChaosMode             bool
	ChaosWorkers          int
	Raw                   string
	HEC                   string
	Streaming             bool
	Compression           bool
	Tag                   string
	ConnSet               []string
	Auth                  string
	Tenant                string
	Count                 uint64
	Duration              time.Duration
	Start                 time.Time
	SRC                   net.IP
	Logger                *log.Logger
	LogLevel              log.Level
	InsecureNoTLSValidate bool
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
	gc.InsecureNoTLSValidate = *tlsNoVerify
	gc.ChaosTimestamps = *tsPsychoMode
	gc.ChaosMode = *chaos
	if gc.ChaosWorkers = *chaosWorkers; gc.ChaosWorkers <= 0 {
		gc.ChaosWorkers = 1
	}

	if *rawTCPConn != `` {
		if _, _, err = net.SplitHostPort(*rawTCPConn); err != nil {
			err = fmt.Errorf("invalid raw-tcp-connection string %q - %v", *rawTCPConn, err)
			return
		}
		gc.modeRawTCP = true
		gc.Raw = *rawTCPConn
		gc.ok = true
		gc.Tag = entry.DefaultTagName
		return
	} else if *rawUDPConn != `` {
		if _, _, err = net.SplitHostPort(*rawUDPConn); err != nil {
			err = fmt.Errorf("invalid raw-udp-connection string %q - %v", *rawUDPConn, err)
			return
		}
		gc.modeRawUDP = true
		gc.Raw = *rawUDPConn
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
	if *entryCount < 0 {
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
	} else if gc.modeRawTCP {
		if conn, err = newRawConn(gc, to); err == nil {
			src, err = conn.SourceIP()
		}
		return
	} else if gc.modeRawUDP {
		if conn, err = newRawUDPConn(gc, to); err == nil {
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
		VerifyCert:    !gc.InsecureNoTLSValidate,
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
	var tsg tsGenerator
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
	if cfg.ChaosTimestamps {
		tsg = newChaosTSGenerator(cfg.Count, cfg.Duration, cfg.Start)
	} else {
		tsg = newSequentialTSGenerator(cfg.Count, cfg.Duration, cfg.Start)
	}
	for i := uint64(0); i < cfg.Count; i++ {
		ts := tsg.Get()
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
	var tsg tsGenerator
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
	//count is implied to be per second in stream mode, so set the duration as per second
	if cfg.ChaosTimestamps {
		tsg = newChaosTSGenerator(cfg.Count, time.Second, cfg.Start)
	} else {
		tsg = newSequentialTSGenerator(cfg.Count, time.Second, cfg.Start)
	}

	var ent *entry.Entry
loop:
	for !*stop {
		ts := time.Now()
		start := ts
		for i := uint64(0); i < cnt; i++ {
			ts := tsg.Get()
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
		}
		time.Sleep(time.Second - time.Since(start))
	}
	return
}

type tsGenerator interface {
	Get() time.Time
}

type sequentialTSGenerator struct {
	span time.Duration
	ts   time.Time
}

func newSequentialTSGenerator(count uint64, duration time.Duration, start time.Time) tsGenerator {
	if count == 0 {
		count = 1
	}
	sp := duration / time.Duration(count)
	var ts time.Time
	if ts = start; ts.IsZero() {
		ts = time.Now().Add(-1 * duration)
	}
	return &sequentialTSGenerator{
		span: sp,
		ts:   ts,
	}
}

func (s *sequentialTSGenerator) Get() (ts time.Time) {
	ts = s.ts
	s.ts = s.ts.Add(s.span)
	return
}

type chaosTSGenerator struct {
	tsbase  time.Time
	tsrange time.Duration
}

func newChaosTSGenerator(count uint64, duration time.Duration, start time.Time) tsGenerator {
	var ts time.Time
	if duration <= 0 {
		duration = 30 * time.Minute // no span just flail all over a 30 minute window
	}
	if ts = start; ts.IsZero() {
		ts = time.Now()
	}
	return &chaosTSGenerator{
		tsbase:  ts,
		tsrange: duration,
	}
}

func (c *chaosTSGenerator) Get() (ts time.Time) {
	ts = c.tsbase.Add(time.Duration(rand.Int63n(int64(c.tsrange))))
	return
}
