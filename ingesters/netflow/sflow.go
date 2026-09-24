/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// NOTE  Leaving this commented out until we officially support sflow internally
//
//	After which, we just have to uncomment the sflow ingester code

package main

// import (
// 	"bytes"
// 	"errors"
// 	"fmt"
// 	"net"
// 	"sync"
//
// 	"github.com/gravwell/gravwell/v3/debug"
// 	"github.com/gravwell/gravwell/v3/ingest/entry"
// 	"github.com/gravwell/sflow"
// )

// See `sFlowRcvrMaximumDatagramSize` in https://sflow.org/sflow_version_5.txt , page 17-18.
// 1400 + 648 to spare
// const defaultDatagramSize = 2048
//
// type SFlowV5Handler struct {
// 	bindConfig
// 	mtx   *sync.Mutex
// 	c     *net.UDPConn
// 	ready bool
// }
//
// func NewSFlowV5Handler(c bindConfig) (*SFlowV5Handler, error) {
// 	if err := c.Validate(); err != nil {
// 		return nil, err
// 	}
//
// 	if !c.ignoreTS {
// 		lg.Warn("Ignore_Timestamp=false has no effect in sflow collector")
// 	}
//
// 	return &SFlowV5Handler{
// 		bindConfig: c,
// 		mtx:        &sync.Mutex{},
// 	}, nil
// }
//
// func (s *SFlowV5Handler) String() string {
// 	return `sFlowV5`
// }
//
// func (s *SFlowV5Handler) Listen(addr string) (err error) {
// 	s.mtx.Lock()
// 	defer s.mtx.Unlock()
// 	if s.c != nil {
// 		err = ErrAlreadyListening
// 		return
// 	}
// 	var a *net.UDPAddr
// 	if a, err = net.ResolveUDPAddr("udp", addr); err != nil {
// 		return
// 	}
// 	if s.c, err = net.ListenUDP("udp", a); err == nil {
// 		s.ready = true
// 	}
// 	return
// }
//
// func (s *SFlowV5Handler) Close() error {
// 	if s == nil {
// 		return ErrAlreadyClosed
// 	}
// 	s.mtx.Lock()
// 	defer s.mtx.Unlock()
// 	s.ready = false
// 	return s.c.Close()
// }
//
// func (s *SFlowV5Handler) Start(id int) error {
// 	s.mtx.Lock()
// 	defer s.mtx.Unlock()
// 	if !s.ready || s.c == nil {
// 		fmt.Println(s.ready, s.c)
// 		return ErrNotReady
// 	}
// 	if id < 0 {
// 		return errors.New("invalid id")
// 	}
// 	go s.routine(id)
// 	return nil
// }
//
// func (s *SFlowV5Handler) routine(id int) {
// 	defer s.wg.Done()
// 	defer delConn(id)
// 	var addr *net.UDPAddr
// 	var err error
// 	var dSize int
// 	tbuf := make([]byte, defaultDatagramSize)
// 	for {
// 		dSize, addr, err = s.c.ReadFromUDP(tbuf)
// 		if err != nil {
// 			return
// 		}
//
// 		decoder := sflow.NewDecoder(bytes.NewReader(tbuf))
// 		dgram, err := decoder.Decode()
// 		if err != nil {
// 			debug.Out("could not parse datagram: %+v\n", err)
// 			continue //there isn't much we can do about bad packets...
// 		}
//
// 		lbuf := make([]byte, dSize)
// 		copy(lbuf, tbuf[:dSize])
// 		e := &entry.Entry{
// 			Tag: s.tag,
// 			SRC: addr.IP,
// 			// TODO  Make sure to throw a warning if ignoreTS was set for the sflow collector
// 			// sflow does not have a timestamp for when the packet was sent.
// 			TS:   entry.Now(),
// 			Data: lbuf,
// 		}
// 		s.ch <- e
// 	}
// }
