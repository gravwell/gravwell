/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gravwell/ingest/entry"
)

const (
	headerSize int = 24
	recordSize int = 48
)

var (
	ErrHeaderTooShort      = errors.New("Buffer to small for Netflow V5 header")
	ErrInvalidCount        = errors.New("V5 record count is invalid")
	ErrInvalidRecordBuffer = errors.New("Buffer too small for V5 record")
	ErrInvalidFlowType     = errors.New("Not a valid Netflow V5 flow")
	ErrAlreadyListening    = errors.New("Already listening")
	ErrAlreadyClosed       = errors.New("Already closed")
	ErrNotReady            = errors.New("Not Ready")
)

type NFv5 struct {
	NFv5Header
	Recs [30]NFv5Record
}

type NFv5Header struct {
	Version        uint16
	Count          uint16
	Uptime         uint32
	Sec            uint32
	Nsec           uint32
	Sequence       uint32
	EngineType     byte
	EngineID       byte
	SampleMode     byte
	SampleInterval uint16
}

type NFv5Record struct {
	ipbuff      [12]byte
	Src         net.IP
	Dst         net.IP
	Next        net.IP
	Input       uint16
	Output      uint16
	Pkts        uint32
	Octets      uint32
	UptimeFirst uint32
	UptimeLast  uint32
	SrcPort     uint16
	DstPort     uint16
	Pad         byte
	Flags       byte
	Protocol    byte
	ToS         byte
	SrcAs       uint16
	DstAs       uint16
	SrcMask     byte
	DstMask     byte
	Pad2        uint16
}

func u16(v []byte) (x uint16) {
	x = (uint16(v[0]) << 8) | uint16(v[1])
	return
}

func u32(v []byte) (x uint32) {
	x = (uint32(v[3]) << 24) | (uint32(v[2]) << 16) | (uint32(v[1]) << 8) | uint32(v[0])
	return
}

// DecodeAlt uses the golang standard method of extracting items using the binary package
func (h *NFv5Header) Decode(b []byte) error {
	if len(b) < headerSize {
		return ErrHeaderTooShort
	}
	h.Version = binary.BigEndian.Uint16(b)
	h.Count = binary.BigEndian.Uint16(b[2:4])
	h.Uptime = binary.BigEndian.Uint32(b[4:8])
	h.Sec = binary.BigEndian.Uint32(b[8:12])
	h.Nsec = binary.BigEndian.Uint32(b[12:16])
	h.Sequence = binary.BigEndian.Uint32(b[16:20])
	h.EngineType = b[20]
	h.EngineID = b[21]
	h.SampleMode = b[22] >> 6
	h.SampleInterval = binary.BigEndian.Uint16(b[22:24]) & 0x3fff
	return nil
}

func (h *NFv5Header) DecodeAlt(b []byte) error {
	if len(b) < headerSize {
		return ErrHeaderTooShort
	}
	h.Version = (uint16(b[0]) << 8) | uint16(b[1])
	h.Count = (uint16(b[2]) << 8) | uint16(b[3])
	h.Uptime = (uint32(b[4]) << 24) | (uint32(b[5]) << 16) | (uint32(b[6]) << 8) | uint32(b[7])
	h.Sec = (uint32(b[8]) << 24) | (uint32(b[9]) << 16) | (uint32(b[10]) << 8) | uint32(b[11])
	h.Nsec = (uint32(b[12]) << 24) | (uint32(b[13]) << 16) | (uint32(b[14]) << 8) | uint32(b[15])
	h.Sequence = (uint32(b[16]) << 24) | (uint32(b[17]) << 16) | (uint32(b[18]) << 8) | uint32(b[19])
	h.EngineType = b[20]
	h.EngineID = b[21]
	h.SampleMode = b[22] >> 6
	h.SampleInterval = (uint16(b[22]) << 8) | uint16(b[23])&0x3fff
	return nil
}

func (h *NFv5Header) Read(rdr io.Reader) error {
	b := make([]byte, headerSize)
	if n, err := rdr.Read(b); err != nil {
		return err
	} else if n != headerSize {
		return ErrHeaderTooShort
	}
	return h.Decode(b)
}

func (nf *NFv5) ValidateSize(b []byte) (n int, err error) {
	if len(b) < headerSize {
		err = ErrHeaderTooShort
		return
	}
	//check the version
	if binary.BigEndian.Uint16(b) != 5 {
		err = ErrInvalidFlowType
		return
	}
	n = int(binary.BigEndian.Uint16(b[2:]))*recordSize + headerSize
	if len(b) < n {
		n = -1
		err = ErrInvalidRecordBuffer
		return
	}
	return
}

func (nf *NFv5) Decode(b []byte) (err error) {
	if len(b) < headerSize {
		err = ErrHeaderTooShort
		return
	}
	if err = nf.NFv5Header.Decode(b); err != nil {
		return
	}
	if nf.Version != 5 {
		err = ErrInvalidFlowType
		return
	}
	if nf.Count == 0 || nf.Count > 30 {
		return ErrInvalidCount
	}
	if len(b) != (headerSize + (int(nf.Count) * recordSize)) {
		err = fmt.Errorf("Invalid record size: %d != %d", len(b), (int(nf.Count) * recordSize))
		return
	}
	b = b[headerSize:]
	for i := uint16(0); i < nf.Count; i++ {
		if err = nf.Recs[i].Decode(b); err != nil {
			return
		}
		b = b[recordSize:]
	}
	return
}

func (nf *NFv5) DecodeAlt(b []byte) (err error) {
	if len(b) < headerSize {
		err = ErrHeaderTooShort
		return
	}
	if err = nf.NFv5Header.DecodeAlt(b); err != nil {
		return
	}
	if nf.Count == 0 || nf.Count > 30 {
		return ErrInvalidCount
	}
	if len(b) != (headerSize + (int(nf.Count) * recordSize)) {
		err = fmt.Errorf("Invalid record size: %d != %d", len(b), (int(nf.Count) * recordSize))
		return
	}
	b = b[headerSize:]
	for i := uint16(0); i < nf.Count; i++ {
		if err = nf.Recs[i].DecodeAlt(b); err != nil {
			return
		}
		b = b[recordSize:]
	}
	return
}

// Read a netflow 4 entry from a stream reader
func (nf *NFv5) Read(rdr io.Reader) error {
	if err := nf.NFv5Header.Read(rdr); err != nil {
		return err
	}
	if nf.Count == 0 || nf.Count > 30 {
		return ErrInvalidCount
	}
	b := make([]byte, int(nf.Count)*recordSize)
	if n, err := rdr.Read(b); err != nil {
		return err
	} else if n != len(b) {
		return ErrInvalidRecordBuffer
	}
	for i := uint16(0); i < nf.Count; i++ {
		if err := nf.Recs[i].Decode(b); err != nil {
			return err
		}
		b = b[recordSize:]
	}
	return nil
}

// Read a netflow 4 record from a stream reader
func (nr *NFv5Record) Read(rdr io.Reader) error {
	//read out the IPS
	b := make([]byte, recordSize)
	if n, err := rdr.Read(b); err != nil {
		return err
	} else if n != recordSize {
		return ErrInvalidRecordBuffer
	}
	return nr.Decode(b)
}

// Decode pulls a record out of the provided buffer
// no pointers are held on the buffer, so it can be reused
func (nr *NFv5Record) Decode(b []byte) error {
	if len(b) < recordSize {
		return ErrInvalidRecordBuffer
	}

	copy(nr.ipbuff[0:12], b[0:12])
	nr.Src = net.IP(nr.ipbuff[0:4])
	nr.Dst = net.IP(nr.ipbuff[4:8])
	nr.Next = net.IP(nr.ipbuff[8:12])

	nr.Input = binary.BigEndian.Uint16(b[12:14])
	nr.Output = binary.BigEndian.Uint16(b[14:16])
	nr.Pkts = binary.BigEndian.Uint32(b[16:20])
	nr.Octets = binary.BigEndian.Uint32(b[20:24])
	nr.UptimeFirst = binary.BigEndian.Uint32(b[24:28])
	nr.UptimeLast = binary.BigEndian.Uint32(b[28:32])
	nr.SrcPort = binary.BigEndian.Uint16(b[32:34])
	nr.DstPort = binary.BigEndian.Uint16(b[34:36])
	nr.Pad = b[36]
	nr.Flags = b[37]
	nr.Protocol = b[38]
	nr.ToS = b[39]
	nr.SrcAs = binary.BigEndian.Uint16(b[40:42])
	nr.DstAs = binary.BigEndian.Uint16(b[42:44])
	nr.SrcMask = b[44]
	nr.DstMask = b[45]
	nr.Pad2 = binary.BigEndian.Uint16(b[46:48])
	return nil
}

func (nr *NFv5Record) DecodeAlt(b []byte) error {
	if len(b) < recordSize {
		return ErrInvalidRecordBuffer
	}
	copy(nr.ipbuff[0:12], b[0:12])
	nr.Src = net.IP(nr.ipbuff[0:4])
	nr.Dst = net.IP(nr.ipbuff[4:8])
	nr.Next = net.IP(nr.ipbuff[8:12])

	nr.Input = (uint16(b[12]) << 8) | uint16(b[13])
	nr.Output = (uint16(b[14]) << 8) | uint16(b[15])
	nr.Pkts = (uint32(b[16]) << 24) | (uint32(b[17]) << 16) | (uint32(b[18]) << 8) | uint32(b[19])
	nr.Octets = (uint32(b[20]) << 24) | (uint32(b[21]) << 16) | (uint32(b[22]) << 8) | uint32(b[23])
	nr.UptimeFirst = (uint32(b[24]) << 24) | (uint32(b[25]) << 16) | (uint32(b[26]) << 8) | uint32(b[27])
	nr.UptimeLast = (uint32(b[28]) << 24) | (uint32(b[29]) << 16) | (uint32(b[30]) << 8) | uint32(b[31])
	nr.SrcPort = (uint16(b[32]) << 8) | uint16(b[33])
	nr.DstPort = (uint16(b[34]) << 8) | uint16(b[35])
	nr.Flags = b[37]
	nr.Protocol = b[38]
	nr.ToS = b[39]
	nr.SrcAs = (uint16(b[40]) << 8) | uint16(b[41])
	nr.DstAs = (uint16(b[42]) << 8) | uint16(b[43])
	nr.SrcMask = b[44]
	nr.DstMask = b[45]
	return nil
}

// TODO - fill this out
func (nf *NFv5) String() (s string) {
	s = fmt.Sprintf("Netflow V%d %v %v %d\n", nf.Version,
		time.Duration(nf.Uptime)*time.Millisecond,
		time.Unix(int64(nf.Sec), int64(nf.Nsec)), nf.Sequence)
	for i := uint16(0); i < nf.Count; i++ {
		s += fmt.Sprintf("\t%s %s %s\n", nf.Recs[i].Src, nf.Recs[i].Dst, nf.Recs[i].Next)
	}
	return
}

type NetflowV5Handler struct {
	bindConfig
	mtx   *sync.Mutex
	c     *net.UDPConn
	ready bool
}

func NewNetflowV5Handler(c bindConfig) (*NetflowV5Handler, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	return &NetflowV5Handler{
		bindConfig: c,
		mtx:        &sync.Mutex{},
	}, nil
}

func (n *NetflowV5Handler) String() string {
	return `NetflowV5`
}

func (n *NetflowV5Handler) Listen(s string) (err error) {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	if n.c != nil {
		err = ErrAlreadyListening
		return
	}
	var a *net.UDPAddr
	if a, err = net.ResolveUDPAddr("udp", s); err != nil {
		return
	}
	if n.c, err = net.ListenUDP("udp", a); err == nil {
		n.ready = true
	}
	return
}

func (n *NetflowV5Handler) Close() error {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	if n == nil {
		return ErrAlreadyClosed
	}
	n.ready = false
	return n.c.Close()
}

func (n *NetflowV5Handler) Start(id int) error {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	if !n.ready || n.c == nil {
		fmt.Println(n.ready, n.c)
		return ErrNotReady
	}
	if id < 0 {
		return errors.New("invalid id")
	}
	go n.routine(id)
	return nil
}

func (n *NetflowV5Handler) routine(id int) {
	defer n.wg.Done()
	defer delConn(id)
	var nf NFv5
	var l int
	var addr *net.UDPAddr
	var err error
	var ts entry.Timestamp
	tbuff := make([]byte, headerSize+(30*recordSize))
	for {
		if l, addr, err = n.c.ReadFromUDP(tbuff); err != nil {
			return
		}
		if l, err = nf.ValidateSize(tbuff); err != nil {
			continue //there isn't much we can do about bad packets...
		}
		lbuff := make([]byte, l)
		copy(lbuff, tbuff[0:l])
		if n.ignoreTS {
			ts = entry.Now()
		} else {
			ts = entry.UnixTime(int64(binary.BigEndian.Uint32(lbuff[8:12])), int64(binary.BigEndian.Uint32(lbuff[12:16])))
		}
		e := &entry.Entry{
			Tag:  n.tag,
			SRC:  addr.IP,
			TS:   ts,
			Data: lbuff,
		}
		n.ch <- e
	}
}
