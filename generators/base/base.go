/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
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
	"strconv"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

var (
	tagName            = flag.String("tag-name", "", "Tag name for ingested data")
	clearConns         = flag.String("clear-conns", "", "comma separated server:port list of cleartext targets")
	tlsConns           = flag.String("tls-conns", "", "comma separated server:port list of TLS connections")
	pipeConns          = flag.String("pipe-conns", "", "comma seperated list of paths for named pipe connection")
	tlsDisableValidate = flag.Bool("insecure-tls-disable-validation", false, "Disable TLS certificate validation")
	ingestSecret       = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	entryCount         = flag.Int("entry-count", 100, "Number of entries to generate")
	startTime          = flag.String("start-time", "", "if set, start the entries at a given time")
	streaming          = flag.Bool("stream", false, "Stream entries in")
	span               = flag.String("duration", "1h", "Total Duration")
	srcOverride        = flag.String("source-override", "", "Source override value")

	Count             uint64
	Duration          time.Duration
	EntryDiffDuration time.Duration
	Streaming         bool
	ConnSet           []string
	Src               net.IP
	TagName           string
	ts                time.Time
)

func ParseFlags() error {
	if !flag.Parsed() {
		flag.Parse()
	}
	if *tagName == "" {
		return errors.New("A tag name must be specified")
	}
	if *clearConns != "" {
		for _, conn := range strings.Split(*clearConns, ",") {
			conn = config.AppendDefaultPort(strings.TrimSpace(conn), config.DefaultCleartextPort)
			if len(conn) > 0 {
				ConnSet = append(ConnSet, fmt.Sprintf("tcp://%s", conn))
			}
		}
	}
	if *tlsConns != "" {
		for _, conn := range strings.Split(*tlsConns, ",") {
			conn = config.AppendDefaultPort(strings.TrimSpace(conn), config.DefaultTLSPort)
			if len(conn) > 0 {
				ConnSet = append(ConnSet, fmt.Sprintf("tls://%s", conn))
			}
		}
	}
	if *pipeConns != "" {
		for _, conn := range strings.Split(*pipeConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				ConnSet = append(ConnSet, fmt.Sprintf("pipe://%s", conn))
			}
		}
	}
	if len(ConnSet) <= 0 {
		return fmt.Errorf("No connections were specified - We need at least one")
	}
	if *entryCount <= 0 {
		return errors.New("invalid entry count")
	}
	Count = uint64(*entryCount)
	if *span == "" {
		return errors.New("Missing duration")
	}
	var err error
	if Duration, err = getDuration(*span); err != nil {
		return err
	}

	if *startTime != `` {
		var ok bool
		var err error
		if ts, ok, err = timegrinder.Extract([]byte(*startTime)); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("Failed to extract time from %s", *startTime)
		}
	}

	TagName = *tagName
	if *srcOverride != `` {
		Src = net.ParseIP(*srcOverride)
	}
	if *streaming {
		EntryDiffDuration = Duration
		Streaming = true
	} else {
		EntryDiffDuration = Duration / time.Duration(Count)
	}
	return nil
}

func GetIngester() (igst *ingest.IngestMuxer, err error) {
	//build up processors
	cfg := ingest.UniformMuxerConfig{
		Destinations:  ConnSet,
		Tags:          []string{*tagName},
		IngesterName:  os.Args[0],
		IngesterLabel: `generator`,
		Auth:          *ingestSecret,
		VerifyCert:    !*tlsDisableValidate, //boolean suckage
	}
	if Src != nil {
		cfg.LogSourceOverride = Src
	}
	if igst, err = ingest.NewUniformMuxer(cfg); err != nil {
		return
	} else if err = igst.Start(); err != nil {
		igst = nil
		return
	} else if err = igst.WaitForHot(time.Second); err != nil {
		igst = nil
		return
	}
	return
}

func StartTime() time.Time {
	if *streaming {
		return time.Now()
	} else if !ts.IsZero() {
		return ts
	}
	return time.Now().Add(-1 * Duration)
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
