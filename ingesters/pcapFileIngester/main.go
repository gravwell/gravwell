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
	"runtime"
	"runtime/debug"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingesters/args"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"

	"github.com/google/gopacket"
	pcap "github.com/google/gopacket/pcapgo"
)

const (
	throwHintSize  uint64 = 1024 * 1024 * 4
	throwBlockSize int    = 4096
)

var (
	pcapFile   = flag.String("pcap-file", "", "Path to the pcap file")
	tsOverride = flag.Bool("ts-override", false, "Override the timestamps and start them at now")
	simIngest  = flag.Bool("no-ingest", false, "Do not ingest the packets, just read the pcap file")
	srcOvr     = flag.String("source-override", "", "Override source with address, hash, or integeter")
	ver        = flag.Bool("version", false, "Print the version information and exit")

	pktCount uint64
	pktSize  uint64
	simulate bool
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}

	if *pcapFile == `` {
		fmt.Printf("A PCAP file is required\n")
		os.Exit(-1)
	}

	simulate = *simIngest
}

func main() {
	debug.SetTraceback("all")
	a, err := args.Parse()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid arguments: %v\n", err)
		return
	}
	//get a handle on the pcap file
	ph, err := newPacketReader(*pcapFile, 16*1024*1024)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open pcap file: %v\n", err)
		return
	}
	defer ph.Close()

	//fire up the ingesters
	igCfg := ingest.UniformMuxerConfig{
		Destinations: a.Conns,
		Tags:         a.Tags,
		Auth:         a.IngestSecret,
		PublicKey:    a.TLSPublicKey,
		PrivateKey:   a.TLSPrivateKey,
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
	if err := igst.WaitForHot(a.Timeout); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
	}

	//get the TagID for our default tag
	tag, err := igst.GetTag(a.Tags[0])
	if err != nil {
		fmt.Printf("Failed to look up tag %s: %v\n", a.Tags[0], err)
		os.Exit(-1)
	}

	entChan := make(chan []*entry.Entry, 64)
	errChan := make(chan error, 1)

	start := time.Now()

	if !simulate {
		go readPackets(ph, igst, tag, entChan, errChan)

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
		if err := simulatePacketRead(ph); err != nil {
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

func readPackets(hnd *packetHandle, igst *ingest.IngestMuxer, tag entry.EntryTag, entChan chan []*entry.Entry, errChan chan error) {
	//get the src
	src, err := igst.SourceIP()
	if err != nil {
		errChan <- err
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

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

	close(entChan)
	close(errChan)
	fmt.Println("Last packet at", ts.Format(time.RFC3339))
}

func simulatePacketRead(hnd *packetHandle) (err error) {
	var ts entry.Timestamp
	var first bool
	var base entry.Timestamp
	var diff time.Duration
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	//set the bpf filter
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
	fmt.Println("Last packet at", ts.Format(time.RFC3339))
	return
}

type packetHandle struct {
	fi     io.ReadCloser
	ngMode bool
	hnd    *pcap.Reader
	nghnd  *pcap.NgReader
}

func newPacketReader(pth string, buff int) (ph *packetHandle, err error) {
	var fi io.ReadCloser
	var hnd *pcap.Reader
	var nghnd *pcap.NgReader
	if fi, err = utils.OpenBufferedFileReader(pth, buff); err != nil {
		return
	}

	//attempt to open a standard reader
	if hnd, err = pcap.NewReader(fi); err == nil {
		ph = &packetHandle{
			fi:  fi,
			hnd: hnd,
		}
		return
	}

	//failed, retry as an ng reader
	if err = fi.Close(); err != nil {
		return
	} else if fi, err = utils.OpenBufferedFileReader(pth, buff); err != nil {
		return
	}

	if nghnd, err = pcap.NewNgReader(fi, pcap.NgReaderOptions{}); err != nil {
		return
	}
	ph = &packetHandle{
		fi:     fi,
		nghnd:  nghnd,
		ngMode: true,
	}
	return
}

func (ph *packetHandle) Close() error {
	return ph.fi.Close()
}

func (ph *packetHandle) ReadPacketData() (data []byte, ci gopacket.CaptureInfo, err error) {
	if ph.ngMode {
		data, ci, err = ph.nghnd.ReadPacketData()
	} else {
		data, ci, err = ph.hnd.ReadPacketData()
	}
	return
}
