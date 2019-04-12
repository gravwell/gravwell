/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
)

const (
	throwHintSize uint64 = 1024 * 1024 * 4
)

var (
	tagName       = flag.String("tag-name", "default", "Tag name for ingested data")
	clearConns    = flag.String("clear-conns", "", "comma seperated server:port list of cleartext targets")
	tlsConns      = flag.String("tls-conns", "", "comma seperated server:port list of TLS connections")
	pipeConns     = flag.String("pipe-conns", "", "comma seperated list of paths for named pie connection")
	tlsPublicKey  = flag.String("tls-public-key", "", "Path to TLS public key")
	tlsPrivateKey = flag.String("tls-private-key", "", "Path to TLS private key")
	ingestSecret  = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	timeoutSec    = flag.Int("timeout", 1, "Connection timeout in seconds")
	pcapFile      = flag.String("pcap-file", "", "Path to the pcap file")
	bpfFilter     = flag.String("bpf-filter", "", "BPF filter to apply to pcap file")
	tsOverride    = flag.Bool("ts-override", false, "Override the timestamps and start them at now")
	connSet       []string
	timeout       time.Duration

	pktCount uint64
	pktSize  uint64
)

func init() {
	flag.Parse()
	if *timeoutSec <= 0 {
		fmt.Printf("Invalid timeout\n")
		os.Exit(-1)
	}
	timeout = time.Second * time.Duration(*timeoutSec)

	if *pcapFile == `` {
		fmt.Printf("A PCAP file is required\n")
		os.Exit(-1)
	}

	if *ingestSecret == `` {
		fmt.Printf("No ingest secret specified\n")
		os.Exit(-1)
	}

	if *tagName == "" {
		fmt.Printf("A tag name must be specified\n")
		os.Exit(-1)
	} else {
		//verify that the tag name is valid
		*tagName = strings.TrimSpace(*tagName)
		if strings.ContainsAny(*tagName, ingest.FORBIDDEN_TAG_SET) {
			fmt.Printf("Forbidden characters in tag\n")
			os.Exit(-1)
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
	if *tlsConns != "" {
		if *tlsPublicKey == "" || *tlsPrivateKey == "" {
			fmt.Printf("Public/private keys required for TLS connection\n")
			os.Exit(-1)
		}
		for _, conn := range strings.Split(*tlsConns, ",") {
			conn = strings.TrimSpace(conn)
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
		fmt.Printf("No connections were specified\nWe need at least one\n")
		os.Exit(-1)
	}
}

func main() {
	//get a handle on the pcap file
	hnd, err := pcap.OpenOffline(*pcapFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open pcap file: %v\n", err)
		return
	}

	//fire up the ingesters
	igCfg := ingest.UniformMuxerConfig{
		Destinations: connSet,
		Tags:         []string{*tagName},
		Auth:         *ingestSecret,
		PublicKey:    *tlsPublicKey,
		PrivateKey:   *tlsPrivateKey,
		LogLevel:     `INFO`,
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed build our ingest system: %v\n", err)
		return
	}
	if err := igst.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed start our ingest system: %v\n", err)
		return
	}
	if err := igst.WaitForHot(timeout); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
	}

	//get the TagID for our default tag
	tag, err := igst.GetTag(*tagName)
	if err != nil {
		fmt.Printf("Failed to look up tag %s: %v\n", *tagName, err)
		os.Exit(-1)
	}

	entChan := make(chan []*entry.Entry, 64)
	quitChan := make(chan bool, 1)
	errChan := make(chan error, 1)

	//listen for signals so we can close gracefully
	sch := make(chan os.Signal, 1)
	signal.Notify(sch, os.Interrupt)

	go packetReader(hnd, igst, tag, quitChan, entChan, errChan)

mainLoop:
	for {
		select {
		case err, ok := <-errChan:
			if !ok {
				break mainLoop
			}
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			break mainLoop
		case <-sch:
			fmt.Println("Breaking due to sigint")
			quitChan <- true
			break mainLoop
		case blk, ok := <-entChan:
			if !ok || len(blk) == 0 {
				break mainLoop
			}
			for i := range blk {
				pktCount++
				pktSize += uint64(len(blk[i].Data))
			}
			if err := igst.WriteBatch(blk); err != nil {
				fmt.Println("failed to write entry", err)
				break mainLoop
			}
		}
	}
	if err := igst.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to close ingester: %v\n", err)
	}
}

func packetReader(hnd *pcap.Handle, igst *ingest.IngestMuxer, tag entry.EntryTag, quitChan chan bool, entChan chan []*entry.Entry, errChan chan error) {
	//get the src
	src, err := igst.SourceIP()
	if err != nil {
		errChan <- err
		return
	}

	//set the bpf filter
	if *bpfFilter != `` {
		if err := hnd.SetBPFFilter(*bpfFilter); err != nil {
			errChan <- err
			return
		}
	}
	var lSize uint64
	var blk []*entry.Entry
	var ts entry.Timestamp
	var first bool
	var base entry.Timestamp
	var diff time.Duration

	//get packet src
	pktSrc := gopacket.NewPacketSource(hnd, hnd.LinkType())
	for pkt := range pktSrc.Packets() {
		if m := pkt.Metadata(); m != nil {
			ts = entry.FromStandard(m.Timestamp)
		} else {
			ts = entry.Now()
		}
		if !first {
			first = true
			base = ts
			diff = entry.Now().Sub(base)
			if *tsOverride {
				fmt.Println("First packet starts at", ts.Add(diff).Format(time.RFC3339))
			} else {
				fmt.Println("First packet starts at", ts.Format(time.RFC3339))
			}
		}
		if *tsOverride {
			ts = ts.Add(diff)
		}
		dt := pkt.Data()
		blk = append(blk, &entry.Entry{
			TS:   ts,
			SRC:  src,
			Tag:  tag,
			Data: dt,
		})
		lSize += uint64(len(dt))

		if lSize >= throwHintSize {
			entChan <- blk
			blk = nil
			lSize = 0
		}
	}
	if len(blk) > 0 {
		entChan <- blk
	}

	hnd.Close()
	if err := igst.Sync(time.Second); err != nil {
		errChan <- err
	} else {
		close(entChan)
		close(errChan)
	}
	fmt.Println("Last packet at", ts.Format(time.RFC3339))
}
