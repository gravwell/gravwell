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

	"github.com/gravwell/ingest/v3/entry"
	"github.com/gravwell/timegrinder/v3"
)

func rfc5424ConnHandlerTCP(c net.Conn, cfg handlerConfig) {
	cfg.wg.Add(1)
	id := addConn(c)
	defer cfg.wg.Done()
	defer delConn(id)
	defer c.Close()
	var rip net.IP
	debugout("new connection from %v", c.RemoteAddr().String())

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
		FormatOverride:     cfg.formatOverride,
	}
	tg, err := timegrinder.NewTimeGrinder(tcfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get a handle on the timegrinder: %v\n", err)
		return
	}

	if cfg.setLocalTime {
		tg.SetLocalTime()
	}
	re := regexp.MustCompile(`\n<\d{1,3}>`)

	s := bufio.NewScanner(c)
	s.Buffer(make([]byte, initDataSize), maxDataSize)
	splitter := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		debugout("data = %v", string(data))
		idx := re.FindIndex(data)
		if idx == nil || len(idx) != 2 {
			if atEOF {
				token = data
				err = bufio.ErrFinalToken
			}
			return //ask for more data
		}
		//check if the first index is zero, if so, then we rerun
		if idx[0] > 0 {
			advance = idx[0]
			token = data[:advance]
			return
		}
		//at the start, so scan again
		idx2 := re.FindIndex(data[idx[1]:])
		if idx2 == nil || len(idx2) != 2 {
			if atEOF {
				token = data
				err = bufio.ErrFinalToken
			}
			return //ask for more data
		}
		advance = idx[1] + idx2[0]
		token = data[:advance]
		return
	}
	s.Split(splitter)
	for s.Scan() {
		data := bytes.Trim(s.Bytes(), "\n\r\t ")
		debugout("Scanning TCP input %s\n", string(data))
		if len(data) == 0 {
			continue
		}
		if err := handleLog(data, rip, cfg.ignoreTimestamps, cfg.tag, cfg.ch, tg); err != nil {
			return
		}
	}
}

func rfc5424ConnHandlerUDP(c *net.UDPConn, cfg handlerConfig) {
	buff := make([]byte, 16*1024) //local buffer that should be big enough for even the largest UDP packets
	tcfg := timegrinder.Config{
		EnableLeftMostSeed: true,
		FormatOverride:     cfg.formatOverride,
	}
	tg, err := timegrinder.NewTimeGrinder(tcfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get a handle on the timegrinder: %v\n", err)
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

	var rip net.IP
	for {
		n, raddr, err := c.ReadFromUDP(buff)
		if err != nil {
			break
		}
		if n > 0 {
			if raddr == nil {
				continue
			}
			if n > len(buff) {
				continue
			}
			if cfg.src == nil {
				rip = raddr.IP
			} else {
				rip = cfg.src
			}
			handleRFC5424Packet(append([]byte(nil), buff[:n]...), rip, cfg.ch, cfg.ignoreTimestamps, cfg.tag, tg)
		}
	}

}

//we can be very very fast on this one by just manually scanning the buffer
func handleRFC5424Packet(buff []byte, ip net.IP, ch chan *entry.Entry, ignoreTS bool, tag entry.EntryTag, tg *timegrinder.TimeGrinder) {
	var idx []int
	var idx2 []int
	re := regexp.MustCompile(`\n<\d{1,3}>`)
	debugout("Scanning UDP packet %s\n", string(buff))
	for len(buff) > 0 {
		if idx = re.FindIndex(buff); idx == nil || len(idx) != 2 {
			handleLog(bytes.TrimSpace(buff), ip, ignoreTS, tag, ch, tg)
			return
		}
		if idx[0] == 0 {
			//at the beginning, rescan
			if idx2 = re.FindIndex(buff[idx[1]:]); idx2 == nil || len(idx2) != 2 {
				//nothing, send it out
				handleLog(bytes.TrimSpace(buff), ip, ignoreTS, tag, ch, tg)
				return
			}
			//got it send log and update buff
			end := idx[1] + idx2[0]
			handleLog(bytes.TrimSpace(buff[0:end]), ip, ignoreTS, tag, ch, tg)
			buff = buff[end:]
			continue
		}
		handleLog(bytes.TrimSpace(buff[0:idx[0]]), ip, ignoreTS, tag, ch, tg)
		buff = buff[idx[0]:]
	}
}
