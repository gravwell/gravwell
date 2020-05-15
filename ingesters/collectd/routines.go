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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"

	"collectd.org/api"
	"collectd.org/network"
)

const (
	empty   runState = iota
	ready   runState = iota
	running runState = iota
	done    runState = iota
	closed  runState = iota
)

type runState int

type collConfig struct {
	wg          *sync.WaitGroup
	igst        *ingest.IngestMuxer
	pl          passlookup
	seclevel    network.SecurityLevel
	defTag      entry.EntryTag
	overrides   map[string]entry.EntryTag
	srcOverride net.IP
	src         net.IP // used if srcOverride is not set
	proc        *processors.ProcessorSet
}

func (bc collConfig) Validate() error {
	if bc.wg == nil {
		return errors.New("nil wait group")
	} else if bc.igst == nil {
		return errors.New("Nil ingest muxer")
	} else if bc.proc == nil {
		return errors.New("Nil processor set")
	}
	return nil
}

type collectdInstance struct {
	collConfig
	sync.Mutex
	srv            network.Server
	state          runState
	errCh          chan error
	useOverrides   bool
	useSrcOverride bool
}

func newCollectdInstance(cc collConfig, laddr *net.UDPAddr) (*collectdInstance, error) {
	if err := cc.Validate(); err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP(`udp`, laddr)
	if err != nil {
		return nil, err
	}
	//build out server for each listener
	srv := network.Server{
		Conn:           conn,
		SecurityLevel:  cc.seclevel,
		PasswordLookup: cc.pl,
	}

	//we have some self referencing circles here, but the call chain shouldn't ever let it be expressed
	ci := &collectdInstance{
		collConfig:     cc,
		srv:            srv,
		state:          ready,
		useOverrides:   len(cc.overrides) > 0,
		useSrcOverride: len(cc.srcOverride) > 0,
	}
	ci.srv.Writer = ci
	return ci, nil
}

func (ci *collectdInstance) Start() error {
	ci.Lock()
	defer ci.Unlock()
	if ci.state != ready {
		return errors.New("Not ready")
	}
	ci.state = running
	ci.errCh = make(chan error, 1)
	go ci.routine(ci.errCh)
	return nil
}

func (ci *collectdInstance) routine(ch chan error) {
	ch <- ci.srv.ListenAndWrite(context.Background())
	ci.Lock()
	ci.state = done
	ci.Unlock()
}

func (ci *collectdInstance) Close() (err error) {
	ci.Lock()
	ch := ci.errCh
	if ci.state != running && ci.state != done {
		ci.Unlock()
		err = errors.New("Not running")
		return
	} else if ci.srv.Conn != nil {
		ci.srv.Conn.Close()
	}
	ci.Unlock()

	if err != nil {
		ci.proc.Close()
		return
	} else if ch != nil {
		if err = <-ch; err == nil {
			err = ci.proc.Close()
		} else {
			ci.proc.Close()
		}
	}
	return
}

func (ci *collectdInstance) Write(ctx context.Context, vl *api.ValueList) error {
	var tag entry.EntryTag
	var src net.IP
	dts, err := marshalJSON(vl)
	if err != nil {
		return err
	}
	if ci.useOverrides {
		var ok bool
		if tag, ok = ci.overrides[vl.Plugin]; !ok {
			tag = ci.defTag
		}
	} else {
		tag = ci.defTag
	}
	if ci.useSrcOverride {
		src = ci.srcOverride
	} else {
		// if src is set, use it, otherwise ask the ingester
		if ci.src.IsUnspecified() {
			if src, err = ci.igst.SourceIP(); err == nil {
				ci.src = src
			}
		}
		src = ci.src // worst case we send 0.0.0.0
	}
	ts := entry.FromStandard(vl.Time)
	for i := range dts {
		ent := &entry.Entry{
			TS:   ts,
			Tag:  tag,
			SRC:  src,
			Data: dts[i],
		}
		if err := ci.proc.Process(ent); err != nil {
			return err
		}
	}
	return nil
}

type dumbprinter struct {
}

func (df dumbprinter) Write(ctx context.Context, vl *api.ValueList) error {
	if dts, err := marshalJSON(vl); err != nil {
		return err
	} else {
		for i := range dts {
			fmt.Println(string(dts[i]))
		}
	}
	return nil
}

type jvl struct {
	Host           string        `json:"host,omitempty"`
	Plugin         string        `json:"plugin,omitempty"`
	PluginInstance string        `json:"plugin_instance,omitempty"`
	Type           string        `json:"type,omitempty"`
	TypeInstance   string        `json:"type_instance,omitempty"`
	Value          api.Value     `json:"value,omitempty"`
	DS             string        `json:"dsname,omitemtpy"`
	Time           time.Time     `json:"time"`
	Interval       time.Duration `json:"interval"`
}

func marshalJSON(vl *api.ValueList) (dts [][]byte, err error) {
	if vl == nil {
		err = errors.New("empty value list")
		return
	}
	v := jvl{
		Host:           vl.Host,
		Plugin:         vl.Plugin,
		PluginInstance: vl.PluginInstance,
		Type:           vl.Type,
		TypeInstance:   vl.TypeInstance,
		Time:           vl.Time,
		Interval:       vl.Interval,
	}
	for i := range vl.Values {
		var dt []byte
		v.Value = vl.Values[i]
		v.DS = vl.DSName(i)
		if dt, err = json.Marshal(v); err != nil {
			return
		}
		dts = append(dts, dt)
	}
	return
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}
