/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

var (
	connClosers map[int]closer
	connId      int
	mtx         sync.Mutex
)

type closer interface {
	Close() error
}

type handlerConfig struct {
	tag              entry.EntryTag
	lrt              readerType
	ignoreTimestamps bool
	setLocalTime     bool
	timezoneOverride string
	src              net.IP
	wg               *sync.WaitGroup
	formatOverride   string
	proc             *processors.ProcessorSet
	ctx              context.Context
}

func startSimpleListeners(cfg *cfgType, igst *ingest.IngestMuxer, wg *sync.WaitGroup, f *flusher, ctx context.Context) error {
	//short circuit out on empty
	if len(cfg.Listener) == 0 {
		return nil
	}

	//fire up our simple backends
	for k, v := range cfg.Listener {
		var src net.IP
		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Listener %v invalid source override, \"%s\" is not an IP address", k, v.Source_Override)
			}
		} else if cfg.Source_Override != `` {
			// global override
			src = net.ParseIP(cfg.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Global Source-Override is invalid")
			}
		}
		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("Failed to resolve tag \"%s\" for %s: %v\n", v.Tag_Name, k, err)
		}
		tp, str, err := translateBindType(v.Bind_String)
		if err != nil {
			lg.FatalCode(0, "Invalid bind string \"%s\": %v\n", v.Bind_String, err)
		}
		lrt, err := translateReaderType(v.Reader_Type)
		if err != nil {
			lg.FatalCode(0, "Invalid reader type \"%s\": %v\n", v.Reader_Type, err)
		}
		hcfg := handlerConfig{
			tag:              tag,
			lrt:              lrt,
			ignoreTimestamps: v.Ignore_Timestamps,
			setLocalTime:     v.Assume_Local_Timezone,
			timezoneOverride: v.Timezone_Override,
			src:              src,
			wg:               wg,
			formatOverride:   v.Timestamp_Format_Override,
			ctx:              ctx,
		}
		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("Preprocessor failure: %v", err)
		}
		if err != nil {
			lg.Fatal("Preprocessor construction error: %v", err)
		}
		f.Add(hcfg.proc)
		if tp.TCP() {
			//get the socket
			addr, err := net.ResolveTCPAddr(tp.String(), str)
			if err != nil {
				lg.FatalCode(0, "Bind-String \"%s\" for %s is invalid: %v\n", v.Bind_String, k, err)
			}
			l, err := net.ListenTCP(tp.String(), addr)
			if err != nil {
				lg.FatalCode(0, "Failed to listen on \"%s\" via %s for %s: %v\n", addr, tp.String(), k, err)
			}
			connID := addConn(l)
			//start the acceptor
			wg.Add(1)
			go acceptor(l, connID, igst, hcfg, tp)
		} else if tp.TLS() {
			config := &tls.Config{
				MinVersion: tls.VersionTLS12,
			}

			config.Certificates = make([]tls.Certificate, 1)
			config.Certificates[0], err = tls.LoadX509KeyPair(v.Cert_File, v.Key_File)
			if err != nil {
				lg.Fatal("Certificate load fail: %v", err)
			}
			//get the socket
			addr, err := net.ResolveTCPAddr("tcp", str)
			if err != nil {
				lg.FatalCode(0, "Bind-String \"%s\" for %s is invalid: %v\n", v.Bind_String, k, err)
			}
			l, err := tls.Listen("tcp", addr.String(), config)
			if err != nil {
				lg.FatalCode(0, "Failed to listen on \"%s\" via TLS for %s: %v\n", addr, k, err)
			}
			connID := addConn(l)
			//start the acceptor
			wg.Add(1)
			go acceptor(l, connID, igst, hcfg, tp)
		} else if tp.UDP() {
			addr, err := net.ResolveUDPAddr(tp.String(), str)
			if err != nil {
				lg.FatalCode(0, "Bind-String \"%s\" for %s is invalid: %v\n", v.Bind_String, k, err)
			}
			l, err := net.ListenUDP(tp.String(), addr)
			if err != nil {
				lg.FatalCode(0, "Failed to listen on \"%s\" via %s for %s: %v\n", addr, tp.String(), k, err)
			}
			connID := addConn(l)
			wg.Add(1)
			go acceptorUDP(l, connID, hcfg)
		}
	}
	debugout("Started %d listeners\n", len(cfg.Listener))
	return nil
}

func acceptor(lst net.Listener, id int, igst *ingest.IngestMuxer, cfg handlerConfig, tp bindType) {
	var failCount int
	defer cfg.wg.Done()
	defer delConn(id)
	defer lst.Close()
	for {
		conn, err := lst.Accept()
		if err != nil {
			//i hate this... is there no damn error check that just says its closed or not?
			if strings.Contains(err.Error(), "closed") {
				break
			}
			failCount++
			fmt.Fprintf(os.Stderr, "Failed to accept TCP connection: %v\n", err)
			if failCount > 3 {
				break
			}
			continue
		}
		debugout("Accepted %v connection from %s in %v mode\n", conn.RemoteAddr(), cfg.lrt, tp.String())
		igst.Info("accepted %v connection from %s in %v mode", conn.RemoteAddr(), cfg.lrt, tp.String())
		failCount = 0
		switch cfg.lrt {
		case lineReader:
			go lineConnHandlerTCP(conn, cfg)
		case rfc5424Reader:
			go rfc5424ConnHandlerTCP(conn, cfg)
		default:
			fmt.Fprintf(os.Stderr, "Invalid reader type on connection\n")
			return
		}
	}
}

func acceptorUDP(conn *net.UDPConn, id int, cfg handlerConfig) {
	defer cfg.wg.Done()
	defer delConn(id)
	defer conn.Close()
	//read packets off
	switch cfg.lrt {
	case lineReader:
		lineConnHandlerUDP(conn, cfg)
	case rfc5424Reader:
		rfc5424ConnHandlerUDP(conn, cfg)
	default:
		fmt.Fprintf(os.Stderr, "Invalid reader type on connection\n")
		return
	}
}

func handleLog(b []byte, ip net.IP, ignoreTS bool, tag entry.EntryTag, tg *timegrinder.TimeGrinder) (ent *entry.Entry, err error) {
	if len(b) == 0 {
		return
	}
	var ok bool
	var ts entry.Timestamp
	var extracted time.Time
	if !ignoreTS {
		if extracted, ok, err = tg.Extract(b); err != nil {
			return
		}
		if ok {
			ts = entry.FromStandard(extracted)
		}
	}
	if !ok {
		ts = entry.Now()
	}
	//debugout("GOT (%v) %s\n", ts, string(b))
	ent = &entry.Entry{
		SRC:  ip,
		TS:   ts,
		Tag:  tag,
		Data: b,
	}
	return
}

func addConn(c closer) int {
	mtx.Lock()
	connId++
	id := connId
	connClosers[connId] = c
	mtx.Unlock()
	return id
}

func delConn(id int) {
	mtx.Lock()
	delete(connClosers, id)
	mtx.Unlock()
}

func connCount() int {
	mtx.Lock()
	defer mtx.Unlock()
	return len(connClosers)
}
