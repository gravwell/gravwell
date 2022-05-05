/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
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
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	DefaultMaxBuffer = 8 * 1024 * 1024 // Buffer up to 8 MB when looking for a regular expression.
)

type regexHandlerConfig struct {
	name             string
	defTag           entry.EntryTag
	ignoreTimestamps bool
	setLocalTime     bool
	timezoneOverride string
	src              net.IP
	wg               *sync.WaitGroup
	formatOverride   string
	proc             *processors.ProcessorSet
	ctx              context.Context
	regex            string
	timeFormats      config.CustomTimeFormat
	trimWhitespace   bool
	maxBuffer        int
}

func startRegexListeners(cfg *cfgType, igst *ingest.IngestMuxer, wg *sync.WaitGroup, f *flusher, ctx context.Context) error {
	var err error
	//short circuit out on empty
	if len(cfg.RegexListener) == 0 {
		return nil
	}

	for k, v := range cfg.RegexListener {
		rhc := regexHandlerConfig{
			name:             k,
			wg:               wg,
			ignoreTimestamps: v.Ignore_Timestamps,
			setLocalTime:     v.Assume_Local_Timezone,
			timezoneOverride: v.Timezone_Override,
			ctx:              ctx,
			timeFormats:      cfg.TimeFormat,
			regex:            v.Regex,
			trimWhitespace:   v.Trim_Whitespace,
			maxBuffer:        v.Max_Buffer,
		}
		if rhc.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("preprocessor error", log.KVErr(err))
		}
		f.Add(rhc.proc)
		if _, err = regexp.Compile(v.Regex); err != nil {
			return err
		}
		if v.Source_Override != `` {
			rhc.src = net.ParseIP(v.Source_Override)
			if rhc.src == nil {
				return fmt.Errorf("RegexListener %v invalid source override \"%s\"", k, v.Source_Override)
			}
		} else if cfg.Source_Override != `` {
			// global override
			rhc.src = net.ParseIP(cfg.Source_Override)
			if rhc.src == nil {
				return fmt.Errorf("global source override \"%s\" is invalid", cfg.Source_Override)
			}
		}
		//resolve default tag
		if rhc.defTag, err = igst.GetTag(v.Tag_Name); err != nil {
			return err
		}

		//check format override
		if v.Timestamp_Format_Override != `` {
			if err = timegrinder.ValidateFormatOverride(v.Timestamp_Format_Override); err != nil {
				return fmt.Errorf("%s Invalid timestamp override \"%s\": %v\n", k, v.Timestamp_Format_Override, err)
			}
			rhc.formatOverride = v.Timestamp_Format_Override
		}

		tp, str, err := translateBindType(v.Bind_String)
		if err != nil {
			lg.FatalCode(0, "invalid bind", log.KV("bindstring", v.Bind_String), log.KVErr(err))
		}

		if tp.TCP() {
			//get the socket
			addr, err := net.ResolveTCPAddr("tcp", v.Bind_String)
			if err != nil {
				return fmt.Errorf("%s Bind-String \"%s\" is invalid: %v\n", k, v.Bind_String, err)
			}
			l, err := net.ListenTCP("tcp", addr)
			if err != nil {
				return fmt.Errorf("%s Failed to listen on \"%s\": %v\n", k, addr, err)
			}
			connID := addConn(l)
			//start the acceptor
			wg.Add(1)
			go regexAcceptor(l, connID, igst, rhc, tp)
		} else if tp.TLS() {
			config := &tls.Config{
				MinVersion: tls.VersionTLS12,
			}

			config.Certificates = make([]tls.Certificate, 1)
			config.Certificates[0], err = tls.LoadX509KeyPair(v.Cert_File, v.Key_File)
			if err != nil {
				lg.Fatal("failed to load certificate", log.KV("certfile", v.Cert_File), log.KV("keyfile", v.Key_File), log.KVErr(err))
			}
			//get the socket
			addr, err := net.ResolveTCPAddr("tcp", str)
			if err != nil {
				lg.FatalCode(0, "invalid Bind-String", log.KV("bindstring", v.Bind_String), log.KV("regexlistener", k), log.KVErr(err))
			}
			l, err := tls.Listen("tcp", addr.String(), config)
			if err != nil {
				lg.FatalCode(0, "failed to listen via TLS", log.KV("address", addr), log.KV("regexlistener", k), log.KVErr(err))
			}
			connID := addConn(l)
			//start the acceptor
			wg.Add(1)
			go regexAcceptor(l, connID, igst, rhc, tp)
		}

	}
	debugout("Started %d regex listeners\n", len(cfg.RegexListener))
	return nil
}

func regexAcceptor(lst net.Listener, id int, igst *ingest.IngestMuxer, cfg regexHandlerConfig, tp bindType) {
	defer cfg.wg.Done()
	defer delConn(id)
	defer lst.Close()
	var failCount int
	for {
		conn, err := lst.Accept()
		if err != nil {
			//i hate this... is there no damn error check that just says its closed or not?
			if strings.Contains(err.Error(), "closed") {
				break
			}
			failCount++
			lg.Info("failed to accept connection", log.KV("readertype", `regex`), log.KV("mode", tp.String()), log.KVErr(err))
			if failCount > 3 {
				break
			}
			continue
		}
		debugout("Accepted %v connection from %s in regex mode\n", tp.String(), conn.RemoteAddr())
		lg.Info("accepted connection", log.KV("address", conn.RemoteAddr()), log.KV("readertype", `regex`), log.KV("mode", tp), log.KV("listener", cfg.name))
		failCount = 0
		go regexConnHandler(conn, cfg, igst)
	}
	return
}

func regexConnHandler(c net.Conn, cfg regexHandlerConfig, igst *ingest.IngestMuxer) {
	cfg.wg.Add(1)
	id := addConn(c)
	defer cfg.wg.Done()
	defer delConn(id)
	defer c.Close()
	var rip net.IP

	if cfg.src == nil {
		ipstr, _, err := net.SplitHostPort(c.RemoteAddr().String())
		if err != nil {
			lg.Error("failed to get host from remote addr", log.KV("remoteaddress", c.RemoteAddr().String()), log.KVErr(err))
			return
		}
		if rip = net.ParseIP(ipstr); rip == nil {
			lg.Error("failed to get remote address", log.KV("remoteaddress", ipstr))
			return
		}
	} else {
		rip = cfg.src
	}

	out := make(chan *entry.Entry, 128)
	go regexLoop(c, cfg, rip, out)

	for ent := range out {
		cfg.proc.ProcessContext(ent, cfg.ctx)
	}
}

func regexLoop(c io.Reader, cfg regexHandlerConfig, rip net.IP, out chan *entry.Entry) {
	var tg *timegrinder.TimeGrinder
	var ts entry.Timestamp
	var regex *regexp.Regexp
	var err error
	var ok bool

	if regex, err = regexp.Compile(cfg.regex); err != nil {
		// will never happen (we always check the regex first)
		return
	}
	prefixIndex := regex.SubexpIndex("prefix")
	suffixIndex := regex.SubexpIndex("suffix")

	if cfg.maxBuffer == 0 {
		cfg.maxBuffer = DefaultMaxBuffer
	}

	if !cfg.ignoreTimestamps {
		var err error
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
		}
		tg, err = timegrinder.NewTimeGrinder(tcfg)
		if err != nil {
			lg.Error("failed to get a handle on the timegrinder", log.KVErr(err))
			return
		} else if err = cfg.timeFormats.LoadFormats(tg); err != nil {
			lg.Error("failed to load custom time formats", log.KVErr(err))
			return
		}
		if cfg.setLocalTime {
			tg.SetLocalTime()
		}
		if cfg.timezoneOverride != `` {
			err = tg.SetTimezone(cfg.timezoneOverride)
			if err != nil {
				lg.Error("failed to set timezone", log.KV("timezone", cfg.timezoneOverride), log.KVErr(err))
				return
			}
		}
		if cfg.formatOverride != `` {
			if err = tg.SetFormatOverride(cfg.formatOverride); err != nil {
				lg.Error("Failed to load format override", log.KV("override", cfg.formatOverride), log.KVErr(err))
				return
			}
		}
	}

	defer close(out)
	rd := make([]byte, 1024)
	bio := bufio.NewReader(c)
	var buf bytes.Buffer // we read from the connection into this buffer
	var data []byte
	var prefix, suffix []byte
	var done bool
	var n int
	for !done {
		// we build the entry data into this buffer.
		// make a new one on each loop so we're safe to call Bytes() at the end
		// read a bunch of bytes off the connection
		if n, err = bio.Read(rd); err != nil && err != io.EOF {
			lg.Error("error reading from regex connection, ingesting partial entry and exiting", log.KVErr(err))
			done = true
		} else if err == io.EOF {
			lg.Info("regex connection saw EOF, finishing up")
			done = true
		} else if n == 0 {
			continue
		}
		if n > 0 {
			buf.Write(rd[:n])
		}

		// now try and match the regex as many times as we can on whatever's in the buffer
		for buf.Len() > 0 {
			var entryData bytes.Buffer
			entryData.Write(prefix)
			b := buf.Bytes()
			match := regex.FindIndex(b)
			// if there's no match, continue, *unless* we saw EOF in which case just send it
			if match != nil {
				// if there is a match, grab the bytes preceding it
				entryData.Write(buf.Next(match[0]))
				// read the actual contents of the match
				contents := buf.Next(match[1] - match[0])
				// see if there's any prefix/suffix stuff
				parts := regex.FindSubmatch(contents)
				if prefixIndex >= 0 && len(parts) >= prefixIndex {
					prefix = parts[prefixIndex]
				} else {
					prefix = []byte{} // shouldn't happen
				}
				if suffixIndex >= 0 && len(parts) >= suffixIndex {
					suffix = parts[suffixIndex]
				} else {
					suffix = []byte{} // shouldn't happen
				}
				entryData.Write(suffix)
			} else if done {
				// there was no match, but we saw EOF, so we're going to write whatever we have
				entryData.Write(buf.Bytes())
				buf.Reset()
			} else {
				// no match, no EOF
				// If we've exceeded the max buffer size, just write out the entry in progress. It's
				// not very nice that way we don't discard data.
				if buf.Len() > cfg.maxBuffer {
					entryData.Write(buf.Next(cfg.maxBuffer))
				} else {
					break
				}
			}

			// We might get here and find that there's no data in the buffer. For instance, if you're trying to match multi-line
			// syslog messages by just matching on the priority + date part at the beginning, we're going to start out with an empty
			// "entry" because the very first thing we see is the delimiter.
			if entryData.Len() == 0 {
				continue
			}

			data = entryData.Bytes()
			//get the timestamp
			if !cfg.ignoreTimestamps {
				var extracted time.Time
				extracted, ok, err = tg.Extract(data)
				if err != nil {
					lg.Error("catastrophic timegrinder failure", log.KVErr(err))
					return
				} else if ok {
					ts = entry.FromStandard(extracted)
				}
			}
			if !ok {
				ts = entry.Now()
			}
			if cfg.trimWhitespace {
				data = bytes.TrimSpace(data)
			}
			ent := &entry.Entry{
				SRC:  cfg.src,
				TS:   ts,
				Tag:  cfg.defTag,
				Data: data,
			}

			out <- ent
		}
	}
}
