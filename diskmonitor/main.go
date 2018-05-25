/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/ingesters/version"
)

const (
	sysPath = `/sys/class/block/`
)

var (
	disk          = flag.String("disk", "sda2", "Disk/partition to be monitored")
	sectSize      = flag.Int("sector-size", 512, "Disk Sector size")
	tagName       = flag.String("tag-name", "diskstats", "Tag name assigned to ingested data")
	clearHeadHole = flag.String("clear-conns", "", "Comma deliminated Server:Port pair specifying a connection")
	pipeHeadHole  = flag.String("pipe-conn", "", "path specifying a named pie connection")
	ingestSecret  = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	period        = flag.String("period", "3s", "Duration between disk samples")
	ver           = flag.Bool("version", false, "Print version information and exit")

	dst        []string
	tags       []string
	sampleFreq time.Duration
	dpath      string
	ssize      uint64
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	if *disk == `` {
		log.Fatal("Disk requried")
	}
	if *sectSize == 0 || (*sectSize%512) != 0 {
		log.Fatal("Invalid disk sector size")
	}
	if *tagName == `` {
		log.Fatal("tag-name required")
	} else {
		if strings.ContainsAny(*tagName, ingest.FORBIDDEN_TAG_SET) {
			log.Fatal("Invalid tag")
		}
		tags = append(tags, *tagName)
	}
	if *clearHeadHole != "" {
		bits := strings.Split(*clearHeadHole, ",")
		for _, bit := range bits {
			bit = strings.TrimSpace(bit)
			dst = append(dst, fmt.Sprintf("tcp://%s", bit))
		}
	}
	if *pipeHeadHole != "" {
		bits := strings.Split(*pipeHeadHole, ",")
		for _, bit := range bits {
			bit = strings.TrimSpace(bit)
			dst = append(dst, fmt.Sprintf("pipe://%s", bit))
		}
	}
	if len(dst) == 0 {
		log.Fatal("No destinations provided")
	}

	if *period != "" {
		var err error
		sampleFreq, err = time.ParseDuration(*period)
		if err != nil {
			log.Fatal("Failed to parse sample frequency", err)
		}
	} else {
		fmt.Println("Defaulting to 1 frequency")
		sampleFreq = time.Second
	}
	if *ingestSecret == "" {
		log.Fatal("Ingest secret must be provided")
	}

	dpath = filepath.Join(sysPath, *disk, `stat`)
	ssize = uint64(*sectSize)
}

type Reading struct {
	Host string
	Disk string
	Data diskStats
}

func main() {
	//test that the disk stats path exists
	if st, err := os.Stat(dpath); err != nil {
		log.Fatal(fmt.Sprintf("Failed to open %s: %v", dpath, err))
	} else if !st.Mode().IsRegular() {
		log.Fatal(fmt.Sprintf("%s is not a regular file", dpath))
	}

	//get the hostname
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal("Failed to get hostname", err)
	} else if hostname == `` {
		log.Fatal("Hostname is empty")
	}

	dm, err := newDiskMonitor(dpath)
	if err != nil {
		log.Fatal(err)
	}
	defer dm.Close()
	qch := make(chan bool, 1)
	samples := make(chan diskSample, 8)

	go sampleRoutine(dm, sampleFreq, samples, qch)

	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: dst,
		Tags:         tags,
		Auth:         *ingestSecret,
		LogLevel:     "WARN",
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		log.Fatal("Failed to create new uniform ingester", err)
	}
	defer igst.Close()
	if err := igst.Start(); err != nil {
		log.Fatal("failed to start ingester: ", err)
	}

	if err := igst.WaitForHot(0); err != nil {
		log.Fatal("Timedout waiting for backed:", err)
	}

	tag, err := igst.GetTag(*tagName)
	if err != nil {
		log.Fatal("Failed to get tag", *tagName, err)
	}

	for smp := range samples {
		bts, err := json.Marshal(Reading{Host: hostname, Disk: *disk, Data: smp.st})
		if err != nil {
			log.Println("Failed to marshal", err)
			qch <- true
		}
		e := &entry.Entry{
			TS:   smp.ts,
			Data: bts,
			Tag:  tag,
		}
		if err := igst.WriteEntry(e); err != nil {
			log.Println("Failed to write entry", err)
			break
		}

	}
	if err := igst.Sync(time.Second); err != nil {
		fmt.Println("Failed to sync", err)
	}

}

type diskSample struct {
	st diskStats
	ts entry.Timestamp
}

func sampleRoutine(dm *diskMonitor, freq time.Duration, outch chan diskSample, quit chan bool) {
	tckr := time.NewTicker(freq)
	defer tckr.Stop()

	for {
		select {
		case _ = <-tckr.C:
			ts := entry.Now()
			smp, err := dm.Sample()
			if err != nil {
				log.Println("Failed to sample", err)
				continue
			}
			outch <- diskSample{st: smp, ts: ts}
		case _ = <-quit:
			close(outch)
			return
		}
	}
}

type diskStats struct {
	ReadIOs      uint64
	ReadMerges   uint64
	ReadSectors  uint64
	ReadTicks    uint64
	WriteIOs     uint64
	WriteMerges  uint64
	WriteSectors uint64
	WriteTicks   uint64
	InFlight     uint64
	IOTicks      uint64
	TimeInQueue  uint64
}

type diskMonitor struct {
	fin  *os.File
	last diskStats
}

func newDiskMonitor(p string) (*diskMonitor, error) {
	fin, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	var ds diskStats
	if err := ds.Load(fin); err != nil {
		fin.Close()
		return nil, err
	}
	return &diskMonitor{
		fin:  fin,
		last: ds,
	}, nil
}

func (dm *diskMonitor) Close() error {
	if err := dm.fin.Close(); err != nil {
		return err
	}
	return nil
}

func (dm *diskMonitor) Sample() (ds diskStats, err error) {
	var curr diskStats
	//seek to the start with fin
	if _, err = dm.fin.Seek(0, 0); err != nil {
		return
	}
	if err = curr.Load(dm.fin); err != nil {
		return
	}
	ds = curr.Sub(dm.last)
	dm.last = curr
	return
}

func (ds *diskStats) Load(in io.Reader) (err error) {
	var bts []byte
	if bts, err = ioutil.ReadAll(in); err != nil {
		return
	}
	if len(bts) == 0 {
		err = errors.New("Failed to read disk stats, empty read")
		return
	}
	flds := bytes.Fields(bts)
	if len(flds) != 11 {
		err = errors.New("Malformed disk stats data. Invalid field count")
		return
	}
	//parse each field
	if ds.ReadIOs, err = strconv.ParseUint(string(flds[0]), 10, 64); err != nil {
		return
	}
	if ds.ReadMerges, err = strconv.ParseUint(string(flds[1]), 10, 64); err != nil {
		return
	}
	if ds.ReadSectors, err = strconv.ParseUint(string(flds[2]), 10, 64); err != nil {
		return
	}
	if ds.ReadTicks, err = strconv.ParseUint(string(flds[3]), 10, 64); err != nil {
		return
	}
	if ds.WriteIOs, err = strconv.ParseUint(string(flds[4]), 10, 64); err != nil {
		return
	}
	if ds.WriteMerges, err = strconv.ParseUint(string(flds[5]), 10, 64); err != nil {
		return
	}
	if ds.WriteSectors, err = strconv.ParseUint(string(flds[6]), 10, 64); err != nil {
		return
	}
	if ds.WriteTicks, err = strconv.ParseUint(string(flds[7]), 10, 64); err != nil {
		return
	}
	if ds.InFlight, err = strconv.ParseUint(string(flds[8]), 10, 64); err != nil {
		return
	}
	if ds.IOTicks, err = strconv.ParseUint(string(flds[9]), 10, 64); err != nil {
		return
	}
	if ds.TimeInQueue, err = strconv.ParseUint(string(flds[10]), 10, 64); err != nil {
		return
	}
	return
}

func (ds diskStats) Sub(s diskStats) (df diskStats) {
	if ds.ReadIOs > s.ReadIOs {
		df.ReadIOs = ds.ReadIOs - s.ReadIOs
	}
	if ds.ReadMerges > s.ReadMerges {
		df.ReadMerges = ds.ReadMerges - s.ReadMerges
	}
	if ds.ReadSectors > s.ReadSectors {
		df.ReadSectors = ds.ReadSectors - s.ReadSectors
	}
	if ds.ReadTicks > s.ReadTicks {
		df.ReadTicks = ds.ReadTicks - s.ReadTicks
	}
	if ds.WriteIOs > s.WriteIOs {
		df.WriteIOs = ds.WriteIOs - s.WriteIOs
	}
	if ds.WriteMerges > s.WriteMerges {
		df.WriteMerges = ds.WriteMerges - s.WriteMerges
	}
	if ds.WriteSectors > s.WriteSectors {
		df.WriteSectors = ds.WriteSectors - s.WriteSectors
	}
	if ds.WriteTicks > s.WriteTicks {
		df.WriteTicks = ds.WriteTicks - s.WriteTicks
	}
	if ds.IOTicks > s.IOTicks {
		df.IOTicks = ds.IOTicks - s.IOTicks
	}
	if ds.TimeInQueue > s.TimeInQueue {
		df.TimeInQueue = ds.TimeInQueue - s.TimeInQueue
	}
	return
}
