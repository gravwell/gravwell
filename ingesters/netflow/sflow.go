/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"

	// TODO Remove in final version
	"os"
	"time"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
	//
	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/sflow"
)

// See `sFlowRcvrMaximumDatagramSize` in https://sflow.org/sflow_version_5.txt , page 17-18.
// 1400 + 648 to spare
const defaultDatagramSize = 2048

type SFlowV5Handler struct {
	bindConfig
	mtx   *sync.Mutex
	c     *net.UDPConn
	ready bool
}

func NewSFlowV5Handler(c bindConfig) (*SFlowV5Handler, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	if !c.ignoreTS {
		lg.Warn("Ignore_Timestamp=false has no effect in sflow collector")
	}

	return &SFlowV5Handler{
		bindConfig: c,
		mtx:        &sync.Mutex{},
	}, nil
}

func (s *SFlowV5Handler) String() string {
	return `sFlowV5`
}

func (s *SFlowV5Handler) Listen(addr string) (err error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.c != nil {
		err = ErrAlreadyListening
		return
	}
	var a *net.UDPAddr
	if a, err = net.ResolveUDPAddr("udp", addr); err != nil {
		return
	}
	if s.c, err = net.ListenUDP("udp", a); err == nil {
		s.ready = true
	}
	return
}

func (s *SFlowV5Handler) Close() error {
	if s == nil {
		return ErrAlreadyClosed
	}
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.ready = false
	return s.c.Close()
}

func (s *SFlowV5Handler) Start(id int) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if !s.ready || s.c == nil {
		fmt.Println(s.ready, s.c)
		return ErrNotReady
	}
	if id < 0 {
		return errors.New("invalid id")
	}
	go s.routine(id)
	return nil
}

func (s *SFlowV5Handler) routine(id int) {
	defer s.wg.Done()
	defer delConn(id)
	var addr *net.UDPAddr
	var err error
	var dSize int
	tbuf := make([]byte, defaultDatagramSize)
	for {
		dSize, addr, err = s.c.ReadFromUDP(tbuf)
		if err != nil {
			return
		}

		decoder := sflow.NewDecoder(bytes.NewReader(tbuf))
		dgram, err := decoder.Decode()
		if err != nil {
			debug.Out("could not parse datagram: %+v\n", err)

			// TODO  Remove this shit, it's just for debugging
			timestamp := time.Now().Unix()
			filename := fmt.Sprintf("sflow_%d.bin", timestamp)
			if writeErr := os.WriteFile(filename, tbuf[:dSize], 0644); writeErr != nil {
				debug.Out("failed to write datagram to file %s: %+v\n", filename, writeErr)
			} else {
				debug.Out("wrote unparseable datagram to %s\n", filename)
			}
			//

			continue //there isn't much we can do about bad packets...
		}

		// TODO  Remove this shit, it's just for debugging
		for _, sample := range dgram.Samples {
			sampleSuffix := ""
			recordSuffix := ""
			sampleFormat := sample.GetHeader().Format

			// Check if sample is unknown
			if _, ok := sample.(*datagram.UnknownSample); ok {
				sampleSuffix = fmt.Sprintf("_unknownsample_%d_", sampleFormat)
				// Write file for unknown sample and skip record processing
				timestamp := time.Now().Unix()
				filename := fmt.Sprintf("sflow%s%d.bin", sampleSuffix, timestamp)
				if writeErr := os.WriteFile(filename, tbuf[:dSize], 0644); writeErr != nil {
					debug.Out("failed to write unknown sample (format %d) to file %s: %+v\n", sampleFormat, filename, writeErr)
				} else {
					debug.Out("wrote unknown sample (format %d) datagram to %s\n", sampleFormat, filename)
				}
				continue // Skip to next sample, can't decode records from unknown sample
			}

			// Check for unknown records within samples
			var records []datagram.Record
			switch s := sample.(type) {
			case *datagram.FlowSample:
				records = s.Records
			case *datagram.FlowSampleExpanded:
				records = s.Records
			case *datagram.CounterSample:
				records = s.Records
			case *datagram.CounterSampleExpanded:
				records = s.Records
			default:
				panic("Clearly I made a terrible mistake here")
			}

			// Collect all unknown record formats
			var unknownFormats []uint32
			for _, record := range records {
				if _, ok := record.(*datagram.UnknownRecord); ok {
					unknownFormats = append(unknownFormats, record.GetHeader().Format)
				}
			}

			// Write file if there are any unknown records, with all formats in the filename
			if len(unknownFormats) > 0 {
				recordSuffix = "_unknownrecord"
				for _, f := range unknownFormats {
					recordSuffix += fmt.Sprintf("_%d", f)
				}
				recordSuffix += "_"
				timestamp := time.Now().Unix()
				filename := fmt.Sprintf("sflow%s%d.bin", recordSuffix, timestamp)
				if writeErr := os.WriteFile(filename, tbuf[:dSize], 0644); writeErr != nil {
					debug.Out("failed to write unknown records (formats %v) to file %s: %+v\n", unknownFormats, filename, writeErr)
				} else {
					debug.Out("wrote unknown records (formats %v) datagram to %s\n", unknownFormats, filename)
				}
			}
		}
		//

		lbuf := make([]byte, dSize)
		copy(lbuf, tbuf[:dSize])
		e := &entry.Entry{
			Tag: s.tag,
			SRC: addr.IP,
			// TODO  Likely here we will make the datagram type have some method to "best effort" guess this shit, depending on the sample
			// sflow does not have a timestamp for when the packet was sent.
			TS:   entry.Now(),
			Data: lbuf,
		}
		s.ch <- e
	}
}
