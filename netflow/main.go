/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

const (
	headerSize int = 24
	recordSize int = 48
)

var (
	ErrHeaderTooShort = errors.New("Buffer to small for Netflow V5 header")
	ErrInvalidCount   = errors.New("V5 record count is invalid")
)

func main() {
	adr, err := net.ResolveUDPAddr("udp", "0.0.0.0:2055")
	if err != nil {
		log.Fatal(err)
	}
	uc, err := net.ListenUDP("udp", adr)
	if err != nil {
		log.Fatal("ListenUDP error", err)
	}
	if err = handlenfv5(uc); err != nil {
		log.Println("v5 error", err)
	}
	if err = uc.Close(); err != nil {
		log.Fatal("Close error", err)
	}
}

func handlenfv5(c *net.UDPConn) (err error) {
	var n int
	var addr *net.UDPAddr
	var nf nfv5
	b := make([]byte, 4096) //way oversized
	for {
		if n, addr, err = c.ReadFromUDP(b); err != nil {
			return
		}
		if n == 0 {
			continue
		}
		if err = nf.Decode(b[0:n]); err != nil {
			return
		}
		fmt.Printf("%v ", addr)
		nf.Print(os.Stdout)
	}
}

type nfv5 struct {
	nfv5Header
	Recs [30]nfv5Record
}

type nfv5Header struct {
	Version      uint16
	Count        uint16
	Uptime       uint32
	Sec          uint32
	Nsec         uint32
	Sequence     uint32
	EngineType   byte
	EngineID     byte
	ModeInterval uint16
}

type nfv5Record struct {
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

func (h *nfv5Header) Decode(b []byte) error {
	if len(b) < headerSize {
		return ErrHeaderTooShort
	}
	bb := bytes.NewBuffer(b)
	return h.Read(bb)
}

func (h *nfv5Header) Read(rdr io.Reader) error {
	return binary.Read(rdr, binary.BigEndian, h)
}

func (nf *nfv5) Decode(b []byte) (err error) {
	bb := bytes.NewBuffer(b)
	if err = nf.nfv5Header.Read(bb); err != nil {
		return
	}
	if nf.Count == 0 || nf.Count > 30 {
		return ErrInvalidCount
	}
	if len(b) != (headerSize + (int(nf.Count) * recordSize)) {
		err = fmt.Errorf("Invalid record size: %d != %d", len(b), (int(nf.Count) * recordSize))
		return
	}
	for i := uint16(0); i < nf.Count; i++ {
		if err = nf.Recs[i].Read(bb); err != nil {
			return
		}
	}
	return
}

func (nr *nfv5Record) Read(rdr io.Reader) (err error) {
	//read out the IPS
	b := make([]byte, 12)
	var n int
	if n, err = rdr.Read(b); err != nil {
		return
	} else if n != 12 {
		err = fmt.Errorf("Failed to read IPs: %d != 12", n)
	}
	nr.Src = net.IP(b[0:4])
	nr.Dst = net.IP(b[4:8])
	nr.Next = net.IP(b[8:12])

	//DEBUG THROW AWAY
	b = make([]byte, recordSize-12)
	binary.Read(rdr, binary.BigEndian, b)
	return
}

func (nf *nfv5) Print(wtr io.Writer) (err error) {
	_, err = fmt.Fprintf(wtr, "Netflow V%d %v %v %d\n", nf.Version,
		time.Duration(nf.Uptime)*time.Millisecond,
		time.Unix(int64(nf.Sec), int64(nf.Nsec)), nf.Sequence)
	if err != nil {
		return
	}
	for i := uint16(0); i < nf.Count; i++ {
		_, err = fmt.Fprintf(wtr, "\t%s %s %s\n", nf.Recs[i].Src, nf.Recs[i].Dst, nf.Recs[i].Next)
		if err != nil {
			return
		}
	}
	return
}
