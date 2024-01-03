/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	maxRFCSize = 100000 //100KB
)

var (
	rfc6587RE = regexp.MustCompile(`\d{1,5} `) //we can handle up to 99999B or about 100KB
)

func findGoodRFC6587Header(b []byte) (number, start, end int) {
	var idxs []int
	var offset int

	for len(b) > 0 {
		if idxs = rfc6587RE.FindIndex(b); idxs == nil || len(idxs) != 2 {
			start, end = -1, -1
			return //did not find it
		}
		hdrnum := b[idxs[0] : idxs[1]-1]

		//try to decode the numbers
		n, err := strconv.ParseUint(string(hdrnum), 10, 32)
		if err == nil && n > 0 && n <= maxRFCSize {
			number = int(n)
			start = offset + idxs[0]
			end = offset + idxs[1]
			return // all good
		}
		offset += idxs[1] //set offset to the end of the potential header
		b = b[idxs[1]:]   //advance b in case we have to loop
	}
	//nothing hit, return 0 and -1
	number, start, end = 0, -1, -1
	return
}

func rfc6587ConnHandlerTCP(c net.Conn, cfg handlerConfig) {
	cfg.wg.Add(1)
	id := addConn(c)
	defer cfg.wg.Done()
	defer delConn(id)
	defer c.Close()
	var rip net.IP
	debugout("new connection from %v\n", c.RemoteAddr().String())

	if cfg.src == nil {
		ipstr, _, err := net.SplitHostPort(c.RemoteAddr().String())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get host from rmote addr \"%s\": %v\n", c.RemoteAddr().String(), err)
			return
		}
		if rip = net.ParseIP(ipstr); rip == nil {
			fmt.Fprintf(os.Stderr, "Failed to get remote addr from \"%s\"\n", ipstr)
			return
		}
	} else {
		rip = cfg.src
	}

	tcfg := timegrinder.Config{
		EnableLeftMostSeed: true,
	}
	tg, err := timegrinder.NewTimeGrinder(tcfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get a handle on the timegrinder: %v\n", err)
		return
	} else if err = cfg.timeFormats.LoadFormats(tg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load custom time formats: %v\n", err)
		return
	}

	if cfg.setLocalTime {
		tg.SetLocalTime()
	}
	if cfg.timezoneOverride != `` {
		err = tg.SetTimezone(cfg.timezoneOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to set timezone to %v: %v\n", cfg.timezoneOverride, err)
			return
		}
	}
	if cfg.formatOverride != `` {
		if err = tg.SetFormatOverride(cfg.formatOverride); err != nil {
			lg.Error("Failed to load format override", log.KV("override", cfg.formatOverride), log.KVErr(err))
			return
		}
	}
	s := bufio.NewScanner(c)
	s.Buffer(make([]byte, initDataSize), maxDataSize)
	splitter := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		byteCount, start, end := findGoodRFC6587Header(data)
		if start == -1 || end == -1 {
			//did not find it, ask for more data
			if atEOF {
				token = data
				err = bufio.ErrFinalToken
			} else if len(data) >= maxRFCSize {
				//we are oversized, just throw what we have
				advance = maxRFCSize
				token = data[0:advance]
			}
			return //ask for more data
		}

		//if the starting offset is NOT zero, chuck what we have up to that point
		if start > 0 {
			advance = start
			if token = bytes.TrimLeft(data[:start], "\n\r\x00"); len(token) == 0 {
				token = nil //just shitcan it... probably a silent framing byte(s)
			}
			return
		}

		if start > len(data) || end > len(data) {
			//just ask for more data
			return
		}

		//start is at beginning of line, check if we have enough data in hand to satisfy the number specified
		if byteCount > len(data[end:]) {
			//ask for more data
			return
		}
		token = data[end:] //just advance to make this easy

		//we have enough, do some cleanin
		token = token[0:byteCount]

		//no to eat potential framing bytes, because this is LOOSLY specified
		advance = end + byteCount
		for {
			if advance == len(data) || strings.IndexByte("\n\r\x00", data[advance]) == -1 {
				break
			}
			//keep eating
			advance++
		}
		return
	}
	s.Split(splitter)
	for s.Scan() {
		data := bytes.Trim(s.Bytes(), "\n\r\t \x00")
		if cfg.dropPriority {
			data = dropPriority(data)
		}
		if len(data) == 0 {
			continue
		}
		data = bytes.Clone(data) // we have to copy due to the scanner reusing its underlying buffer
		if ent, err := handleLog(data, rip, cfg.ignoreTimestamps, cfg.tag, tg); err != nil {
			return
		} else if err = cfg.proc.ProcessContext(ent, cfg.ctx); err != nil {
			return
		}
	}
}
