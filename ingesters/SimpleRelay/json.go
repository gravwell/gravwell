/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/timegrinder"

	"github.com/buger/jsonparser"
)

type jsonHandlerConfig struct {
	name             string
	defTag           entry.EntryTag
	tags             map[string]entry.EntryTag
	ignoreTimestamps bool
	setLocalTime     bool
	timezoneOverride string
	src              net.IP
	wg               *sync.WaitGroup
	formatOverride   string
	flds             []string
	proc             *processors.ProcessorSet
	ctx              context.Context
	timeFormats      config.CustomTimeFormat
	maxObjectSize    int64
	disableCompact   bool
}

func startJSONListeners(cfg *cfgType, igst *ingest.IngestMuxer, wg *sync.WaitGroup, f *flusher, ctx context.Context) error {
	var err error
	//short circuit out on empty
	if len(cfg.JSONListener) == 0 {
		return nil
	}

	for k, v := range cfg.JSONListener {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("JSONListener %s configuration is invalid: %w", k, err)
		}
		jhc := jsonHandlerConfig{
			name:             k,
			wg:               wg,
			tags:             map[string]entry.EntryTag{},
			ignoreTimestamps: v.Ignore_Timestamps,
			setLocalTime:     v.Assume_Local_Timezone,
			timezoneOverride: v.Timezone_Override,
			ctx:              ctx,
			timeFormats:      cfg.TimeFormat,
			maxObjectSize:    int64(v.Max_Object_Size),
			disableCompact:   v.Disable_Compact,
		}
		if jhc.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("preprocessor error", log.KVErr(err))
		}
		f.Add(jhc.proc)
		if jhc.flds, err = v.GetJsonFields(); err != nil {
			return err
		}
		if v.Source_Override != `` {
			jhc.src = net.ParseIP(v.Source_Override)
			if jhc.src == nil {
				return fmt.Errorf("JSONListener %v invalid source override \"%s\"", k, v.Source_Override)
			}
		} else if cfg.Source_Override != `` {
			// global override
			jhc.src = net.ParseIP(cfg.Source_Override)
			if jhc.src == nil {
				return fmt.Errorf("global source override \"%s\" is invalid", cfg.Source_Override)
			}
		}
		//resolve default tag
		if jhc.defTag, err = igst.GetTag(v.Default_Tag); err != nil {
			return err
		}

		//resolve all the other tags
		tms, err := v.TagMatchers()
		if err != nil {
			return err
		}
		for _, tm := range tms {
			tg, err := igst.GetTag(tm.Tag)
			if err != nil {
				return err
			}
			jhc.tags[tm.Value] = tg
		}
		//check format override
		if v.Timestamp_Format_Override != `` {
			if err = timegrinder.ValidateFormatOverride(v.Timestamp_Format_Override); err != nil {
				return fmt.Errorf("%s Invalid timestamp override \"%s\": %v\n", k, v.Timestamp_Format_Override, err)
			}
			jhc.formatOverride = v.Timestamp_Format_Override
		}

		tp, str, err := translateBindType(v.Bind_String)
		if err != nil {
			lg.FatalCode(0, "invalid bind", log.KV("bindstring", v.Bind_String), log.KVErr(err))
		}

		if tp.TCP() {
			//get the socket
			addr, err := net.ResolveTCPAddr("tcp", str)
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
			go jsonAcceptor(l, connID, igst, jhc, tp)
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
				lg.FatalCode(0, "invalid Bind-String", log.KV("bindstring", v.Bind_String), log.KV("jsonlistener", k), log.KVErr(err))
			}
			l, err := tls.Listen("tcp", addr.String(), config)
			if err != nil {
				lg.FatalCode(0, "failed to listen via TLS", log.KV("address", addr), log.KV("jsonlistener", k), log.KVErr(err))
			}
			connID := addConn(l)
			//start the acceptor
			wg.Add(1)
			go jsonAcceptor(l, connID, igst, jhc, tp)
		} else if tp.UDP() {
			addr, err := net.ResolveUDPAddr(tp.String(), str)
			if err != nil {
				lg.FatalCode(0, "invalid Bind-String", log.KV("bindstring", v.Bind_String), log.KV("listener", k), log.KVErr(err))
			}
			l, err := net.ListenUDP(tp.String(), addr)
			if err != nil {
				lg.FatalCode(0, "failed to listen via udp", log.KV("address", addr), log.KV("listener", k), log.KVErr(err))
			}
			connID := addConn(l)
			wg.Add(1)
			go jsonAcceptorUDP(l, connID, igst, jhc)

		}

	}
	debugout("Started %d json listeners\n", len(cfg.JSONListener))
	return nil
}

func jsonAcceptor(lst net.Listener, id int, igst *ingest.IngestMuxer, cfg jsonHandlerConfig, tp bindType) {
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
			fmt.Fprintf(os.Stderr, "Failed to accept %v connection: %v\n", tp.String(), err)
			if failCount > 3 {
				break
			}
			continue
		}
		debugout("Accepted %v connection from %s in json mode\n", tp.String(), conn.RemoteAddr())
		lg.Info("accepted connection", log.KV("address", conn.RemoteAddr()), log.KV("readertype", `json`), log.KV("mode", tp), log.KV("listener", cfg.name))
		failCount = 0
		go jsonConnHandler(conn, cfg, igst)
	}
	return
}

func jsonAcceptorUDP(conn *net.UDPConn, id int, igst *ingest.IngestMuxer, cfg jsonHandlerConfig) {
	defer cfg.wg.Done()
	defer delConn(id)
	defer conn.Close()

	buff := make([]byte, 16*1024) //local buffer that should be big enough for even the largest UDP packets
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
	for {
		n, raddr, err := conn.ReadFromUDP(buff)
		if err != nil {
			break
		}
		if n == 0 {
			continue
		}
		if raddr == nil {
			continue
		}
		if n > len(buff) {
			continue
		}
		var rip net.IP
		if cfg.src == nil {
			rip = raddr.IP
		} else {
			rip = cfg.src
		}
		// get a local logger up that will always add some more info
		ll := log.NewLoggerWithKV(lg, log.KV("json-listener", cfg.name), log.KV("remoteaddress", rip.String()))
		handleJSONStream(bytes.NewReader(buff[0:]), cfg, rip, tg, ll)
	}

}

func jsonConnHandler(c net.Conn, cfg jsonHandlerConfig, igst *ingest.IngestMuxer) {
	cfg.wg.Add(1)
	id := addConn(c)
	defer cfg.wg.Done()
	defer delConn(id)
	defer c.Close()
	var rip net.IP
	var lip net.IP // just used for logging
	var tg *timegrinder.TimeGrinder

	if ipstr, _, err := net.SplitHostPort(c.RemoteAddr().String()); err != nil {
		lg.Error("failed to get host from remote addr", log.KV("remoteaddress", c.RemoteAddr().String()), log.KVErr(err))
		return
	} else if lip = net.ParseIP(ipstr); lip == nil {
		lg.Error("failed to get remote address", log.KV("remoteaddress", ipstr))
		return
	}

	if cfg.src == nil {
		rip = lip //use the logging remote ip that we resolved above
	} else {
		rip = cfg.src
	}
	// get a local logger up that will always add some more info
	ll := log.NewLoggerWithKV(lg, log.KV("json-listener", cfg.name), log.KV("remoteaddress", lip.String()))

	if !cfg.ignoreTimestamps {
		var err error
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
		}
		tg, err = timegrinder.NewTimeGrinder(tcfg)
		if err != nil {
			ll.Error("failed to get a handle on the timegrinder", log.KVErr(err))
			return
		} else if err = cfg.timeFormats.LoadFormats(tg); err != nil {
			ll.Error("failed to load custom time formats", log.KVErr(err))
			return
		}
		if cfg.setLocalTime {
			tg.SetLocalTime()
		}
		if cfg.timezoneOverride != `` {
			if err = tg.SetTimezone(cfg.timezoneOverride); err != nil {
				ll.Error("failed to set timezone", log.KV("timezone", cfg.timezoneOverride), log.KVErr(err))
				return
			}
		}
		if cfg.formatOverride != `` {
			if err = tg.SetFormatOverride(cfg.formatOverride); err != nil {
				ll.Error("Failed to load format override", log.KV("override", cfg.formatOverride), log.KVErr(err))
				return
			}
		}
	}

	if err := handleJSONStream(c, cfg, rip, tg, ll); err != nil {
		ll.Error("JSON Stream handler error", log.KVErr(err))
	}
	return
}

func handleJSONStream(rdr io.Reader, cfg jsonHandlerConfig, rip net.IP, tg *timegrinder.TimeGrinder, ll *log.KVLogger) error {
	var ts entry.Timestamp
	var ok bool

	//initialize our tag to the default tag
	tag := cfg.defTag

	dec, err := utils.NewJsonLimitedDecoder(rdr, cfg.maxObjectSize)
	if err != nil {
		ll.Error("Failed to create limited json decoder", log.KVErr(err))
		return err
	}
consumerLoop:
	for {
		var obj json.RawMessage
		if err := dec.Decode(&obj); err != nil {
			// check if limited reader is exhausted so that we can throw a better error
			if errors.Is(err, utils.ErrOversizedObject) {
				ll.Error("oversized json object", log.KV("max-size", cfg.maxObjectSize))
			} else if errors.Is(err, io.EOF) {
				ll.Info("client disconnected")
				break consumerLoop //break out of the main loop
			} else {
				//just a plain old error
				ll.Error("invalid json object", log.KV("max-size", cfg.maxObjectSize), log.KVErr(err))
			}
			return err // we pretty much have to just hang up
		}
		//we have a message, compact it
		if cfg.disableCompact {
			// just trip obvious garbage
			obj = json.RawMessage(bytes.Trim([]byte(obj), "\n\r\t "))
		} else {
			obj = compactObject(obj)
		}
		if len(obj) == 0 {
			continue
		}
		data := []byte(obj)

		//get the timestamp
		if cfg.ignoreTimestamps {
			ts = entry.Now()
		} else {
			if extracted, ok, err := tg.Extract(data); err != nil {
				ll.Error("catastrophic timegrinder failure", log.KVErr(err))
				return err
			} else if ok {
				ts = entry.FromStandard(extracted)
			} else {
				ts = entry.Now()
			}
		}

		//try to derive a tag out if we have matchers
		if len(cfg.flds) > 0 {
			if s, err := jsonparser.GetString(data, cfg.flds...); err != nil {
				tag = cfg.defTag
			} else if tag, ok = cfg.tags[s]; !ok {
				tag = cfg.defTag
			}
		}
		ent := &entry.Entry{
			SRC:  rip,
			TS:   ts,
			Tag:  tag,
			Data: data,
		}
		cfg.proc.ProcessContext(ent, cfg.ctx)
	}
	return nil
}

func compactObject(obj json.RawMessage) (r json.RawMessage) {
	bb := bytes.NewBuffer(nil)
	if err := json.Compact(bb, obj); err == nil {
		r = bb.Bytes()
	} else {
		r = obj
	}
	return
}
