/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/config"
)

var (
	tagName    = flag.String("tag-name", "binary", "Tag name for ingested data")
	clearConns = flag.String("clear-conns", "172.17.0.2:4023,172.17.0.3:4023,172.17.0.4:4023,172.17.0.5:4023",
		"comma seperated server:port list of cleartext targets")
	tlsConns        = flag.String("tls-conns", "", "comma seperated server:port list of TLS connections")
	pipeConns       = flag.String("pipe-conns", "", "comma seperated list of paths for named pie connection")
	tlsPublicKey    = flag.String("tls-public-key", "", "Path to TLS public key")
	tlsPrivateKey   = flag.String("tls-private-key", "", "Path to TLS private key")
	tlsRemoteVerify = flag.String("tls-remote-verify", "", "Path to remote public key to verify against")
	ingestSecret    = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	entryCount      = flag.Int("entry-count", 100, "Number of entries to generate")
	streaming       = flag.Bool("stream", false, "Stream entries in")
	span            = flag.String("duration", "1h", "Total Duration")
	srcOverride     = flag.String("source-override", "", "Source override value")
	count           uint64
	totalBytes      uint64
	totalCount      uint64
	duration        time.Duration
	connSet         []string
	src             net.IP
)

func init() {
	flag.Parse()
	if *tagName == "" {
		log.Fatal("A tag name must be specified\n")
	}
	if *clearConns != "" {
		for _, conn := range strings.Split(*clearConns, ",") {
			conn = config.AppendDefaultPort(strings.TrimSpace(conn), config.DefaultCleartextPort)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("tcp://%s", conn))
			}
		}
	}
	if *tlsConns != "" {
		if *tlsPublicKey == "" || *tlsPrivateKey == "" {
			log.Fatal("Public/private keys required for TLS connection\n")
		}
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
		log.Fatal("No connections were specified\nWe need at least one\n")
	}
	if *entryCount <= 0 {
		log.Fatal("invalid entry count")
	}
	count = uint64(*entryCount)
	if *span == "" {
		log.Fatal("Missing duration")
	}
	var err error
	if duration, err = getDuration(*span); err != nil {
		log.Fatal(err)
	}
	if *srcOverride != `` {
		src = net.ParseIP(*srcOverride)
	} else {
		src = net.ParseIP("192.168.1.1")
	}
}

func main() {
	var err error
	//build up processors
	igst, err := ingest.NewUniformIngestMuxer(connSet, []string{*tagName}, *ingestSecret, *tlsPublicKey, *tlsPrivateKey, *tlsRemoteVerify)
	if err := igst.Start(); err != nil {
		log.Fatal(err)
	}
	if err := igst.WaitForHot(time.Second); err != nil {
		log.Fatalf("ERROR: Timed out waiting for active connection due to %v\n", err)
	}
	//get the TagID for our default tag
	tag, err := igst.GetTag(*tagName)
	if err != nil {
		log.Fatalf("Failed to look up tag %s: %v\n", *tagName, err)
	}
	start := time.Now()

	if !*streaming {
		if err = throw(igst, tag, count, duration); err != nil {
			log.Fatal("Failed to throw entries ", err)
		}
	} else {
		var stop bool
		r := make(chan error, 1)
		go func(ret chan error, stp *bool) {
			ret <- stream(igst, tag, count, stp)
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
		if err != nil {
			log.Fatal("Failed to stream entries ", err)
		}
	}

	if err = igst.Close(); err != nil {
		log.Fatal("Failed to close ingest muxer ", err)
	}

	durr := time.Since(start)
	if err == nil {
		fmt.Printf("Completed in %v (%s)\n", durr, ingest.HumanSize(totalBytes))
		fmt.Printf("Total Count: %s\n", ingest.HumanCount(totalCount))
		fmt.Printf("Entry Rate: %s\n", ingest.HumanEntryRate(totalCount, durr))
		fmt.Printf("Ingest Rate: %s\n", ingest.HumanRate(totalBytes, durr))
	}
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
