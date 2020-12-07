/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	ErrInvalidRemoteISEHeader = errors.New("Failed to match remote ISE header")
	ErrInvalidRemoteISESeq    = errors.New("Invalid multipart message sequence")
)

const (
	CiscoISEProcessor string = `cisco_ise`

	defaultMultipartMaxBuffer = 8 * 1024 * 1024 //8MB, which is ALOT
)

type CiscoISEConfig struct {
	Passthrough_Misses          bool
	Enable_Multipart_Reassembly bool
	Max_Multipart_Buffer        uint
	Max_Multipart_Latency       string
	maxLatency                  time.Duration
}

func CiscoISELoadConfig(vc *config.VariableConfig) (c CiscoISEConfig, err error) {
	if err = vc.MapTo(&c); err != nil {
		return
	}
	if c.Max_Multipart_Latency != `` {
		if c.maxLatency, err = time.ParseDuration(c.Max_Multipart_Latency); err != nil {
			err = fmt.Errorf("Invalid Max-Multipart-Latency %q: %v", c.Max_Multipart_Latency, err)
		}
	}

	return
}

func NewCiscoISEProcessor(cfg CiscoISEConfig) (*CiscoISE, error) {
	return &CiscoISE{
		CiscoISEConfig: cfg,
	}, nil
}

type CiscoISE struct {
	CiscoISEConfig
}

func (p *CiscoISE) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(CiscoISEConfig); ok {
		p.CiscoISEConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (p *CiscoISE) Process(ent *entry.Entry) (rset []*entry.Entry, err error) {
	return
}

func (p *CiscoISE) Close() error {
	return nil
}

type multipartAssembler struct {
	max        uint64 //maximum buffer size
	total      uint64 //total number of bytes in the buffer
	maxLatency time.Duration
	oldest     time.Time
	tracker    map[remoteISEHeaderSource]*messageSequence
}

func newMultipartAssembler(maxBuff uint64, maxLatency time.Duration) *multipartAssembler {
	if maxBuff == 0 {
		maxBuff = defaultMultipartMaxBuffer
	}
	//just make sure its sane
	if maxLatency <= 0 {
		maxLatency = 0
	}
	return &multipartAssembler{
		max:        maxBuff,
		tracker:    map[remoteISEHeaderSource](*messageSequence){},
		maxLatency: maxLatency,
		oldest:     time.Now(), //just initialize to now
	}
}

func (ma *multipartAssembler) add(msg remoteISE) (val string, ejected, bad bool) {
	src := msg.remoteISEHeaderSource
	//check if we have an existing message
	if v, ok := ma.tracker[src]; ok {
		sz := v.size
		//v is a pointer, don't need to assign back in
		if ejected, bad = v.add(msg); bad {
			//something is wrong
			return
		} else if ejected {
			//this is the final message, eject it
			val = v.finalize()
			delete(ma.tracker, src)
			ma.total -= sz
		} else {
			ma.total += uint64(len(msg.body)) //add in the size
		}
	} else {
		if msi, ok := newMessageSequence(msg); !ok {
			bad = true
			return
		} else {
			ma.tracker[src] = msi
			ma.total += msi.size
		}
	}
	return
}

func (ma *multipartAssembler) shouldFlush() bool {
	if ma.total >= ma.max {
		return true
	} else if ma.maxLatency > 0 {
		if time.Since(ma.oldest) > ma.maxLatency {
			return true //there is something that needs to be purged
		}
	}
	return false
}

func (ma *multipartAssembler) flush(force bool) (outputs []string) {
	var cutoff time.Time
	var checkTime bool
	if ma.maxLatency > 0 {
		checkTime = true
		cutoff = time.Now().Add(-1 * ma.maxLatency)
	}

	var oldest time.Time
	for k, v := range ma.tracker {
		if force || (checkTime && v.last.Before(cutoff)) {
			outputs = append(outputs, v.finalize())
			delete(ma.tracker, k)
		} else if oldest.IsZero() || v.last.Before(oldest) {
			oldest = v.last
		}
	}
	if oldest.IsZero() {
		oldest = time.Now()
	}
	ma.oldest = oldest
	return
}

type messageSequence struct {
	remoteISEHeaderSource
	size   uint64
	total  uint16
	curr   uint16
	bodies []string
	last   time.Time
}

// newMessageSequence generates a new message sequence from a given remoteISE
func newMessageSequence(msg remoteISE) (ms *messageSequence, ok bool) {
	if msg.total == 0 || msg.seq >= msg.total {
		return // this is bad mmmkay
	}
	bodies := make([]string, msg.total)
	bodies[msg.seq] = msg.body
	ms = &messageSequence{
		remoteISEHeaderSource: msg.remoteISEHeaderSource,
		total:                 msg.total,
		curr:                  1, //we got the first one
		size:                  uint64(len(msg.body) + len(msg.host) + len(msg.cat)),
		bodies:                bodies,
		last:                  time.Now(),
	}
	ok = true
	return
}

// add takes in a remoteISE message and adds it to the tracked set
// we return done==true if this is the final message and this is ready to be finalized
// if we get a remote message with an ID that doesn't make sense, we return bad==true
func (ms *messageSequence) add(msg remoteISE) (done, bad bool) {
	if msg.seq >= uint16(len(ms.bodies)) {
		//bad index, just reject it
		bad = true
		return
	}
	ms.bodies[msg.seq] = msg.body
	ms.curr += 1
	ms.size += uint64(len(msg.body))
	if ms.curr == uint16(len(ms.bodies)) {
		//we got them all
		done = true
	} else {
		//update the hit
		ms.last = time.Now()
	}
	return
}

func (ms *messageSequence) finalize() (r string) {
	for i := range ms.bodies {
		r += ms.bodies[i]
	}
	return
}

var (
	//setup remote header extraction RX (see https://www.cisco.com/c/en/us/td/docs/security/ise/syslog/Cisco_ISE_Syslogs/m_IntrotoSyslogs.pdf page 4)
	remoteHeaderRx          = regexp.MustCompile(`^(?P<ts>\S+\s\d+\s\d+\:\d+\:\d+)\s(?P<host>\S+)\s(?P<cat>\S+)\s(?P<msgid>\d+)\s(?P<total>\d+)\s(?P<seq>\d+)\s(?P<body>.+)$`)
	remoteHeaderRxParts int = 8
)

// remoteISEHeaderSource is the sub structure that represents a specific message category
type remoteISEHeaderSource struct {
	host string
	cat  string
	id   uint32
}

// remoteISEHeader is the greater structure representing a multipart message coming form a given host
type remoteISE struct {
	remoteISEHeaderSource
	total uint16
	seq   uint16
	body  string
}

func (rim *remoteISE) Parse(val string) (err error) {
	var v uint64
	r := remoteHeaderRx.FindStringSubmatch(val)
	if len(r) != (remoteHeaderRxParts) {
		err = ErrInvalidRemoteISEHeader
		return
	}
	//skip past the complete match and grab the string values, we are skipping the timestamp on multipart messages
	r = r[2:]
	rim.host = r[0]
	rim.cat = r[1]
	//id uint32/3
	if v, err = strconv.ParseUint(r[2], 10, 32); err != nil {
		err = fmt.Errorf("%w %v", ErrInvalidRemoteISEHeader, err)
		return
	}
	rim.id = uint32(v)
	//total uint16/4
	if v, err = strconv.ParseUint(r[3], 10, 16); err != nil {
		err = fmt.Errorf("%w %v", ErrInvalidRemoteISEHeader, err)
		return
	}
	rim.total = uint16(v)
	//seq   uint16/5
	if v, err = strconv.ParseUint(r[4], 10, 16); err != nil {
		err = fmt.Errorf("%w %v", ErrInvalidRemoteISEHeader, err)
		return
	}
	rim.seq = uint16(v)
	rim.body = r[5]
	//double check the sequence and total for sanity
	if rim.seq > rim.total {
		err = fmt.Errorf("%w sequence %d > total %d", ErrInvalidRemoteISESeq, rim.seq, rim.total)
	}
	return
}
