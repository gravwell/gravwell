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
	"strings"
	"sync"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/timegrinder"

	"github.com/buger/jsonparser"
)

type jsonHandlerConfig struct {
	ch               chan *entry.Entry
	defTag           entry.EntryTag
	tags             map[string]entry.EntryTag
	ignoreTimestamps bool
	setLocalTime     bool
	src              net.IP
	wg               *sync.WaitGroup
	formatOverride   int
	flds             []string
}

func startJSONListeners(cfg *cfgType, igst *ingest.IngestMuxer, ch chan *entry.Entry, wg *sync.WaitGroup) error {
	var err error
	//short circuit out on empty
	if len(cfg.JSONListener) == 0 {
		return nil
	}

	for k, v := range cfg.JSONListener {
		jhc := jsonHandlerConfig{
			ch:               ch,
			wg:               wg,
			tags:             map[string]entry.EntryTag{},
			ignoreTimestamps: v.Ignore_Timestamps,
			setLocalTime:     v.Assume_Local_Timezone,
		}
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
		if jhc.formatOverride, err = v.TimestampOverride(); err != nil {
			return fmt.Errorf("%s Invalid timestamp override \"%s\": %v\n", k, v.Timestamp_Format_Override, err)
		}

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
		go jsonAcceptor(l, connID, igst, jhc)

	}
	debugout("Started %d json listeners\n", len(cfg.JSONListener))
	return nil
}

func jsonAcceptor(lst net.Listener, id int, igst *ingest.IngestMuxer, cfg jsonHandlerConfig) {
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
			fmt.Fprintf(os.Stderr, "Failed to accept TCP connection: %v\n", err)
			if failCount > 3 {
				break
			}
			continue
		}
		debugout("Accepted TCP connection from %s in json mode\n", conn.RemoteAddr())
		igst.Info("accepted TCP connection from %s in json mode\n", conn.RemoteAddr())
		failCount = 0
		go jsonConnHandler(conn, cfg)
	}
	return
}

func jsonConnHandler(c net.Conn, cfg jsonHandlerConfig) {
	cfg.wg.Add(1)
	id := addConn(c)
	defer cfg.wg.Done()
	defer delConn(id)
	defer c.Close()
	var rip net.IP
	var ts entry.Timestamp
	var tg *timegrinder.TimeGrinder
	var tag entry.EntryTag
	var ok bool

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

	if !cfg.ignoreTimestamps {
		var err error
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
			FormatOverride:     cfg.formatOverride,
		}
		tg, err = timegrinder.NewTimeGrinder(tcfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get a handle on the timegrinder: %v\n", err)
			return
		}
		if cfg.setLocalTime {
			tg.SetLocalTime()
		}

	}
	bio := bufio.NewReader(c)
	for {
		//get the data entry and clean it a bit
		data, err := bio.ReadBytes('\n')
		if err != nil {
			break
		}
		if data = bytes.Trim(data, "\n\r\t "); len(data) == 0 {
			continue
		}
		//get the timestamp
		if !cfg.ignoreTimestamps {
			var extracted time.Time
			extracted, ok, err = tg.Extract(data)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Catastrophic timegrinder failure: %v\n", err)
				return
			} else if ok {
				ts = entry.FromStandard(extracted)
			}
		}
		if !ok {
			ts = entry.Now()
		}
		//try to derive a tag out
		if s, err := jsonparser.GetString(data, cfg.flds...); err != nil {
			tag = cfg.defTag
		} else if tag, ok = cfg.tags[s]; !ok {
			tag = cfg.defTag
		}
		cfg.ch <- &entry.Entry{
			SRC:  cfg.src,
			TS:   ts,
			Tag:  tag,
			Data: data,
		}
	}
}
