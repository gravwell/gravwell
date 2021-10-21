/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/generators/ipgen"
	"github.com/gravwell/gravwell/v3/ingest"
)

var (
	v4gen *ipgen.V4Gen
	v6gen *ipgen.V6Gen
)

func init() {
	var err error
	v4gen, err = ipgen.RandomWeightedV4Generator(3)
	if err != nil {
		log.Fatalf("Failed to instantiate v4 generator: %v\n", err)
	}
	v6gen, err = ipgen.RandomWeightedV6Generator(30)
	if err != nil {
		log.Fatalf("Failed to instantiate v6 generator: %v\n", err)
	}
}

func main() {
	var igst *ingest.IngestMuxer
	var totalBytes uint64
	var totalCount uint64
	var src net.IP
	cfg, err := base.GetGeneratorConfig(`syslog`)
	if err != nil {
		log.Fatal(err)
	}
	if igst, src, err = base.NewIngestMuxer(`sysloggenerator`, ``, cfg, time.Second); err != nil {
		log.Fatal(err)
	}
	tag, err := igst.GetTag(cfg.Tag)
	if err != nil {
		log.Fatalf("Failed to lookup tag %s: %v", cfg.Tag, err)
	}
	start := time.Now()

	if !cfg.Streaming {
		if totalCount, totalBytes, err = base.OneShot(igst, tag, src, cfg.Count, cfg.Duration, genData); err != nil {
			log.Fatal("Failed to throw entries ", err)
		}
	} else {
		if totalCount, totalBytes, err = base.Stream(igst, tag, src, cfg.Count, genData); err != nil {
			log.Fatal("Failed to stream entries ", err)
		}
	}

	if err = igst.Sync(time.Second); err != nil {
		log.Fatal("Failed to sync ingest muxer ", err)
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

func genData(ts time.Time) []byte {
	sev := rand.Intn(24)
	fac := rand.Intn(7)
	prio := (sev << 3) | fac
	return []byte(fmt.Sprintf("<%d>1 %s %s %s %d - %s %s",
		prio, ts.Format(tsFormat), getHost(), getApp(), rand.Intn(0xffff), genStructData(), rd.Paragraph()))
}

func genStructData() string {
	return fmt.Sprintf(`[%s source-address="%s" source-port=%d destination-address="%s" destination-port=%d useragent="%s"]`, rd.Email(), v4gen.IP().String(), 0x2000+rand.Intn(0xffff-0x2000), v4gen.IP().String(), 1+rand.Intn(2047), rd.UserAgentString())
}
