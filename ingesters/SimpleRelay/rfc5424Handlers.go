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
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	// the default amount of time we will pump the RFC5424 TCP reader
	// we have to do this because the last entry read is held until the connection drops
	// or we get a new entry, so we will periodically pump the reader
	defaultReaderPumpInterval time.Duration = 5 * time.Second
	maxDataStaleTime          time.Duration = 10 * time.Second
)

func rfc5424ConnHandlerTCP(c net.Conn, cfg handlerConfig) {
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
	//wrap our connection in a read pumper so that we can force the scanner to wake up periodcally
	//this lets us detect a message that doesn't have a terminator and has been sitting in the buffer
	//for a while.  Overall this is a way to enable the SimpleRelay ingster to detect the "last log message" and push it once its been sitting for a while
	pumper := newReadTimeoutPumper(c, defaultReaderPumpInterval)
	s := bufio.NewScanner(pumper)
	s.Buffer(make([]byte, initDataSize), maxDataSize)
	splitter := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		idx, sz := rfc5424StartIndex(data)
		if idx == -1 {
			if atEOF {
				token = data
				err = bufio.ErrFinalToken
			} else if len(data) >= maxRFCSize {
				//we are oversized, just throw what we have
				advance = maxRFCSize
				token = data[0:advance]
			} else {
				//ask for more data
				if len(data) > 0 && pumper.lastRead() > maxDataStaleTime {
					//throw what we have, its been sitting for a while
					advance = len(data)
					token = data
					return
				}
				//asking for more data
			}
			return
		}
		if idx > 0 {
			advance = idx //advance to start the match
			token = data[:advance]
			return
		}
		//at the start, so scan again
		idx2, _ := rfc5424StartIndex(data[idx+sz:]) //advance past the min size
		if idx2 == -1 {
			if atEOF {
				token = data
				err = bufio.ErrFinalToken
			} else {
				//ask for more data
				if len(data) > 0 && pumper.lastRead() > maxDataStaleTime {
					//throw what we have, its been sitting for a while
					advance = len(data)
					token = data
					return
				}
				//asking for more data
			}
			return
		}
		advance = sz + idx2
		token = data[:advance]
		return
	}
	s.Split(splitter)
	for s.Scan() {
		data := bytes.TrimSpace(s.Bytes())
		if cfg.dropPriority {
			data = dropPriority(data)
		}
		if len(data) == 0 {
			continue
		}
		data = bytes.Clone(data) // the scanner re-uses bytes, so we have to clone
		if ent, err := handleLog(data, rip, cfg.ignoreTimestamps, cfg.tag, tg); err != nil {
			return
		} else if err = cfg.proc.ProcessContext(ent, cfg.ctx); err != nil {
			return
		}
	}
}

func dropPriority(buff []byte) []byte {
	//scoot past the '>'
	if prioIdx := bytes.IndexByte(buff, '>'); prioIdx > 1 {
		buff = buff[prioIdx+1:]
	}
	return buff
}

func rfc5424ConnHandlerUDP(c *net.UDPConn, cfg handlerConfig) {
	buff := make([]byte, 16*1024) //local buffer that should be big enough for even the largest UDP packets
	tcfg := timegrinder.Config{
		EnableLeftMostSeed: true,
	}
	tg, err := timegrinder.NewTimeGrinder(tcfg)
	if err != nil {
		lg.Error("Failed to get a handle on the timegrinder", log.KVErr(err))
		return
	} else if err = cfg.timeFormats.LoadFormats(tg); err != nil {
		lg.Error("Failed to load custom time formats", log.KVErr(err))
		return
	}

	if cfg.setLocalTime {
		tg.SetLocalTime()
	}
	if cfg.timezoneOverride != `` {
		err = tg.SetTimezone(cfg.timezoneOverride)
		if err != nil {
			lg.Error("Failed to load timezeone override", log.KV("override", cfg.timezoneOverride), log.KVErr(err))
			return
		}
	}
	if cfg.formatOverride != `` {
		if err = tg.SetFormatOverride(cfg.formatOverride); err != nil {
			lg.Error("Failed to load format override", log.KV("override", cfg.formatOverride), log.KVErr(err))
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
			handleRFC5424Packet(append([]byte(nil), buff[:n]...), rip, cfg.ignoreTimestamps, cfg.dropPriority, cfg.tag, tg, cfg.proc, cfg.ctx)
		}
	}

}

// we can be very very fast on this one by just manually scanning the buffer
func handleRFC5424Packet(buff []byte, ip net.IP, ignoreTS, dropPrio bool, tag entry.EntryTag, tg *timegrinder.TimeGrinder, proc *processors.ProcessorSet, ctx context.Context) {
	var idx []int
	var idx2 []int
	var token []byte
	re := regexp.MustCompile(`<\d{1,3}>`)
	for len(buff) > 0 {
		if idx = re.FindIndex(buff); idx == nil || len(idx) != 2 {
			//did not find our header at all, just throw the buff up stream
			token = bytes.TrimSpace(buff)
			if dropPrio {
				token = dropPriority(token)
			}
			if ent, err := handleLog(token, ip, ignoreTS, tag, tg); err != nil {
				return
			} else if err = proc.ProcessContext(ent, ctx); err != nil {
				return
			}
			return
		}
		if idx[0] == 0 {
			//at the beginning, rescan to find end
			if idx2 = re.FindIndex(buff[idx[1]:]); idx2 == nil || len(idx2) != 2 {
				//not found, this is the end of our input, throw it all
				token = bytes.TrimSpace(buff)
				if dropPrio {
					token = dropPriority(token)
				}
				if ent, err := handleLog(token, ip, ignoreTS, tag, tg); err != nil {
					return
				} else if err = proc.ProcessContext(ent, ctx); err != nil {
					return
				}
				return
			}
			end := idx[1] + idx2[0] //remeber to add original offset
			//got it send log and update buff
			token = buff[0:end]
			buff = buff[end:]
			token = bytes.TrimSpace(token)
			if dropPrio {
				token = dropPriority(token)
			}
			if ent, err := handleLog(token, ip, ignoreTS, tag, tg); err != nil {
				return
			} else if err = proc.ProcessContext(ent, ctx); err != nil {
				return
			}
		} else {
			//not at the start, just chuck what we have up front
			token = buff[0:idx[0]]
			buff = buff[idx[0]:]

			token = bytes.TrimSpace(token)
			if dropPrio {
				token = dropPriority(token)
			}
			if ent, err := handleLog(token, ip, ignoreTS, tag, tg); err != nil {
				return
			} else if err = proc.ProcessContext(ent, ctx); err != nil {
				return
			}
		}
	}
}

var sepStart = []byte{'\n', '<'}

const sepEnd = byte('>')

// rfc5424StartIndex is just a high speed version of the regular expression "\n<\d{1,3}>"
// its job is to find that sequence in the byte stream and return and index and the size of what it found.
// Implementing this by hand gave us a nice memory reduction and speed bump in very high throughput scenarios
// If the sequence is NOT found we return -1, 0.  Largest sz possible should be 6
func rfc5424StartIndex(buf []byte) (idx, sz int) {
	var sidx int
	var digits int
	for len(buf) > 0 {
		if sidx = bytes.Index(buf, sepStart); sidx == -1 {
			idx = -1
			return
		}
		off := sidx + 2
		left := len(buf) - off
		if left == 0 {
			idx = -1
			return
		}
		for digits = 0; digits < 4 && digits < left; digits++ {
			if bt := buf[off+digits]; bt < 0x30 || bt > 0x39 {
				break
			}
		}
		if digits > 0 && digits < 4 && digits < left && buf[off+digits] == sepEnd {
			//got it
			idx += sidx
			sz = 3 + digits
			return
		}
		//wong count of digits or missing end seperator, update buff and move on
		off += digits
		buf = buf[off:]
		idx += off
	}
	//if we hit here, its bad
	return -1, 0
}

type readTimeoutPumper struct {
	conn    net.Conn
	timeout time.Duration
	last    time.Time
}

func newReadTimeoutPumper(conn net.Conn, timeout time.Duration) *readTimeoutPumper {
	if timeout <= 0 {
		timeout = defaultReaderPumpInterval
	}
	return &readTimeoutPumper{
		conn:    conn,
		timeout: timeout,
	}
}

func (rtp *readTimeoutPumper) lastRead() time.Duration {
	if rtp.last.IsZero() {
		return 0
	}
	return time.Since(rtp.last)
}

func (rtp *readTimeoutPumper) Read(buff []byte) (n int, err error) {
	//set timeout
	if err = rtp.conn.SetReadDeadline(time.Now().Add(rtp.timeout)); err != nil {
		return
	}
	n, err = rtp.conn.Read(buff)
	if err != nil {
		// clear timeout not matter what
		rtp.conn.SetReadDeadline(time.Time{})
		if isTimeout(err) {
			//only pump the artificial newline if we read nothing and there is a buffer
			if n <= 0 && len(buff) > 0 {
				n = 1
				buff[0] = 0xA // this will get trimmed off
			}
			err = nil // if its just a timeout, clear the error
		}
	} else {
		//clear timeout but set error
		err = rtp.conn.SetReadDeadline(time.Time{})
		// got an actual read, so update last timestamp
		rtp.last = time.Now()
	}
	return
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	} else if nerr, ok := err.(net.Error); ok {
		return nerr.Timeout()
	}
	return false
}
