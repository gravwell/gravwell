/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

var (
	tagName      = flag.String("tag-name", "reddit", "Tag name for ingested data")
	pipeConns    = flag.String("pipe-conn", "", "Path to pipe connection")
	clearConns   = flag.String("clear-conns", "", "comma seperated server:port list of cleartext targets")
	ingestSecret = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	timeoutSec   = flag.Int("timeout", 1, "Connection timeout in seconds")
	connSet      []string
	timeout      time.Duration

	flushCommentShard time.Duration = 10 * time.Second
)

type ingestWriter struct {
	igst          *ingest.IngestMuxer
	commentCache  map[uint64][]Comment
	flushDuration time.Duration
	ready         bool
	tag           entry.EntryTag
	src           net.IP
	emptyTimes    int
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	verbose = *verboseFlag
	timeout = time.Second * time.Duration(*timeoutSec)

	if *tagName == "" {
		log.Fatal("A tag name must be specified\n")
	} else {
		//verify that the tag name is valid
		*tagName = strings.TrimSpace(*tagName)
		if strings.ContainsAny(*tagName, ingest.FORBIDDEN_TAG_SET) {
			log.Fatal("Forbidden characters in tag\n")
		}
	}
	if *clearConns != "" {
		for _, conn := range strings.Split(*clearConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("tcp://%s", conn))
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
}

func NewIngestWriter() (iw *ingestWriter, err error) {
	var igst *ingest.IngestMuxer
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: connSet,
		Tags:         []string{*tagName},
		Auth:         *ingestSecret,
		IngesterName: "reddit",
		LogLevel:     "INFO",
	}
	igst, err = ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		return
	}
	if err = igst.Start(); err != nil {
		return
	}
	if err = igst.WaitForHot(2 * time.Second); err != nil {
		fmt.Printf("Timedout waiting for backend connections: %v\n", err)
		return
	}

	iw = &ingestWriter{
		igst:         igst,
		commentCache: make(map[uint64][]Comment, 64),
	}
	return
}

func (iw *ingestWriter) Close() error {
	if err := iw.igst.Sync(time.Second); err != nil {
		return err
	}
	if err := iw.igst.Close(); err != nil {
		return err
	}
	iw.ready = false
	return nil
}

func (iw *ingestWriter) AddComment(c Comment) {
	v := iw.commentCache[c.CreatedUTC]
	v = append(v, c)
	iw.commentCache[c.CreatedUTC] = v
}

func (iw *ingestWriter) Flush() error {
	//check if we have any hot indexers
	hot, err := iw.igst.Hot()
	if err != nil {
		return err
	}
	if hot == 0 {
		return nil //do nothing
	}
	if !iw.ready {
		//we have a hot ingester but no tag or src, correct that
		tag, err := iw.igst.GetTag(*tagName)
		if err != nil {
			return err
		}
		src, err := iw.igst.SourceIP()
		if err != nil {
			return err
		}
		iw.tag = tag
		iw.src = src
		iw.ready = true
	}

	//we are good to push some entries
	toFlush := uint64(time.Now().Add(-1 * flushCommentShard).UTC().Unix())
	if len(iw.commentCache) == 0 {
		iw.emptyTimes++
	}
	for k := range iw.commentCache {
		if k < toFlush {
			v := iw.commentCache[k]
			if err := iw.pushComments(v); err != nil {
				return err
			}
			delete(iw.commentCache, k)
			if *verboseFlag {
				log.Println("Flushed", len(v), "Comments at", k)
			}
		}
	}
	return nil
}

func (iw *ingestWriter) pushComments(cms []Comment) error {
	for i := range cms {
		v, err := json.Marshal(cms[i])
		if err != nil {
			return err
		}
		e := &entry.Entry{
			SRC:  iw.src,
			Tag:  iw.tag,
			TS:   entry.FromStandard(time.Unix(int64(cms[i].CreatedUTC), 0)),
			Data: v,
		}
		if err := iw.igst.WriteEntry(e); err != nil {
			return err
		}
	}
	return nil
}
