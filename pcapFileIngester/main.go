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
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
)

const (
	throwHintSize  uint64 = 1024 * 1024 * 4
	throwBlockSize int    = 4096
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
	simIngest     = flag.Bool("no-ingest", false, "Do not ingest the packets, just read the pcap file")
	connSet       []string
	timeout       time.Duration

	pktCount uint64
	pktSize  uint64
	simulate bool
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
	simulate = *simIngest
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
	errChan := make(chan error, 1)

	//listen for signals so we can close gracefully
	sch := make(chan os.Signal, 1)
	signal.Notify(sch, os.Interrupt)
	start := time.Now()

	if !simulate {
		go packetReader(hnd, igst, tag, entChan, errChan)

	mainLoop:
		for {
			select {
			case err, ok := <-errChan:
				if !ok {
					break mainLoop
				}
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
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
	} else {
		if err := simulatePacketRead(hnd); err != nil {
			fmt.Println("failed to Simulate packet read", err)
		}
	}
	if err := igst.Sync(10 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to close ingester: %v\n", err)
	}
	dur := time.Since(start)
	fmt.Printf("Completed in %v (%s)\n", dur, ingest.HumanSize(pktSize))
	fmt.Printf("Total Count: %s\n", ingest.HumanCount(pktCount))
	fmt.Printf("Entry Rate: %s\n", ingest.HumanEntryRate(pktCount, dur))
	fmt.Printf("Ingest Rate: %s\n", ingest.HumanRate(pktSize, dur))
}

func packetReader(hnd *pcap.Handle, igst *ingest.IngestMuxer, tag entry.EntryTag, entChan chan []*entry.Entry, errChan chan error) {
	//get the src
	src, err := igst.SourceIP()
	if err != nil {
		errChan <- err
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	//set the bpf filter
	if *bpfFilter != `` {
		if err := hnd.SetBPFFilter(*bpfFilter); err != nil {
			errChan <- err
			return
		}
	}
	var sec int64
	var lSize uint64
	var ts entry.Timestamp
	var first bool
	var base entry.Timestamp
	var diff time.Duration
	var dt []byte
	var ci gopacket.CaptureInfo
	var blk []*entry.Entry

	//get packet src
	for {
		if dt, ci, err = hnd.ReadPacketData(); err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
		ts = entry.FromStandard(ci.Timestamp)
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
		//check if we should throw
		if sec != ts.Sec || len(blk) >= throwBlockSize || lSize >= throwHintSize {
			if len(blk) > 0 {
				entChan <- blk
				blk = nil
				lSize = 0
			}
		}
		blk = append(blk, &entry.Entry{
			TS:   ts,
			SRC:  src,
			Tag:  tag,
			Data: dt,
		})
		lSize += uint64(len(dt))
		sec = ts.Sec
	}
	if len(blk) > 0 {
		entChan <- blk
	}

	hnd.Close()
	close(entChan)
	close(errChan)
	fmt.Println("Last packet at", ts.Format(time.RFC3339))
}

func simulatePacketRead(hnd *pcap.Handle) (err error) {
	var ts entry.Timestamp
	var first bool
	var base entry.Timestamp
	var diff time.Duration
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	//set the bpf filter
	if *bpfFilter != `` {
		if err = hnd.SetBPFFilter(*bpfFilter); err != nil {
			return
		}
	}
	var dt []byte
	var ci gopacket.CaptureInfo
	for {
		if dt, ci, err = hnd.ReadPacketData(); err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
		ts = entry.FromStandard(ci.Timestamp)
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
		pktSize += uint64(len(dt))
		pktCount++
	}
	hnd.Close()
	fmt.Println("Last packet at", ts.Format(time.RFC3339))
	return
}
