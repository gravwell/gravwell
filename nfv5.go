/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package netflow

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	HeaderSize int = 24
	RecordSize int = 48
)

var (
	ErrHeaderTooShort      = errors.New("Buffer to small for Netflow V5 header")
	ErrInvalidCount        = errors.New("V5 record count is invalid")
	ErrInvalidRecordBuffer = errors.New("Buffer too small for V5 record")
	ErrInvalidFlowType     = errors.New("Not a valid Netflow V5 flow")
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
	Bytes       uint32
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

// Decode uses the golang standard method of extracting items using the binary package
func (h *NFv5Header) Decode(b []byte) error {
	if len(b) < HeaderSize {
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

// Encode encodes a NFv5Header into a byte array
func (h *NFv5Header) Encode() (b []byte) {
	b = make([]byte, HeaderSize)
	h.encode(b)
	return
}

func (h *NFv5Header) encode(b []byte) (err error) {
	if len(b) < HeaderSize {
		return ErrHeaderTooShort
	}
	binary.BigEndian.PutUint16(b[0:2], h.Version)
	binary.BigEndian.PutUint16(b[2:4], h.Count)
	binary.BigEndian.PutUint32(b[4:8], h.Uptime)
	binary.BigEndian.PutUint32(b[8:12], h.Sec)
	binary.BigEndian.PutUint32(b[12:16], h.Nsec)
	binary.BigEndian.PutUint32(b[16:20], h.Sequence)
	b[20] = h.EngineType
	b[21] = h.EngineID
	h.SampleMode = b[22] >> 6
	h.SampleInterval = binary.BigEndian.Uint16(b[22:24]) & 0x3fff
	binary.BigEndian.PutUint16(b[22:24], (h.SampleInterval&0x3fff)|(uint16(h.SampleMode)<<6))
	return
}

//decodeAlt decodes by hand with the assumption that we are operating on a LittleEndian machine
//the code is slower and not used, but is left here anyway
func (h *NFv5Header) decodeAlt(b []byte) error {
	if len(b) < HeaderSize {
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
	b := make([]byte, HeaderSize)
	if n, err := rdr.Read(b); err != nil {
		return err
	} else if n != HeaderSize {
		return ErrHeaderTooShort
	}
	return h.Decode(b)
}

func (h *NFv5Header) Write(wtr io.Writer) error {
	b := make([]byte, HeaderSize)
	if err := h.encode(b); err != nil {
		return err
	} else if n, err := wtr.Write(b); err != nil {
		return err
	} else if n != HeaderSize {
		return ErrHeaderTooShort
	}
	return nil
}

func (nf *NFv5) ValidateSize(b []byte) (n int, err error) {
	if len(b) < HeaderSize {
		err = ErrHeaderTooShort
		return
	}
	//check the version
	if binary.BigEndian.Uint16(b) != 5 {
		err = ErrInvalidFlowType
		return
	}
	n = int(binary.BigEndian.Uint16(b[2:]))*RecordSize + HeaderSize
	if len(b) < n {
		n = -1
		err = ErrInvalidRecordBuffer
		return
	}
	return
}

func (nf *NFv5) Decode(b []byte) (err error) {
	if len(b) < HeaderSize {
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
	if len(b) != (HeaderSize + (int(nf.Count) * RecordSize)) {
		err = fmt.Errorf("Invalid record size: %d != %d", len(b), (int(nf.Count) * RecordSize))
		return
	}
	b = b[HeaderSize:]
	for i := uint16(0); i < nf.Count; i++ {
		if err = nf.Recs[i].Decode(b); err != nil {
			return
		}
		b = b[RecordSize:]
	}
	return
}

func (nf *NFv5) Encode() (b []byte, err error) {
	if nf.Version != 5 {
		err = ErrInvalidFlowType
		return
	}
	if nf.Count == 0 || nf.Count > 30 {
		err = ErrInvalidCount
		return
	}
	b = make([]byte, (HeaderSize + (int(nf.Count) * RecordSize)))
	if err = nf.NFv5Header.encode(b); err != nil {
		return
	}
	p := b[HeaderSize:]
	for i := uint16(0); i < nf.Count; i++ {
		if err = nf.Recs[i].encode(p); err != nil {
			return
		}
		p = p[RecordSize:]
	}
	return
}

// decodeAlt uses the decoding methods that don't use the binary package
// it is slower and assumes the host is a LittleEndian machine, don't use it
func (nf *NFv5) decodeAlt(b []byte) (err error) {
	if len(b) < HeaderSize {
		err = ErrHeaderTooShort
		return
	}
	if err = nf.NFv5Header.decodeAlt(b); err != nil {
		return
	}
	if nf.Count == 0 || nf.Count > 30 {
		return ErrInvalidCount
	}
	if len(b) != (HeaderSize + (int(nf.Count) * RecordSize)) {
		err = fmt.Errorf("Invalid record size: %d != %d", len(b), (int(nf.Count) * RecordSize))
		return
	}
	b = b[HeaderSize:]
	for i := uint16(0); i < nf.Count; i++ {
		if err = nf.Recs[i].decodeAlt(b); err != nil {
			return
		}
		b = b[RecordSize:]
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
	b := make([]byte, int(nf.Count)*RecordSize)
	if n, err := rdr.Read(b); err != nil {
		return err
	} else if n != len(b) {
		return ErrInvalidRecordBuffer
	}
	for i := uint16(0); i < nf.Count; i++ {
		if err := nf.Recs[i].Decode(b); err != nil {
			return err
		}
		b = b[RecordSize:]
	}
	return nil
}

// Write a netflow 4 entry to a stream writer
func (nf *NFv5) Write(wtr io.Writer) error {
	b := make([]byte, HeaderSize+int(nf.Count)*RecordSize)
	if err := nf.NFv5Header.encode(b); err != nil {
		return err
	}
	if nf.Count == 0 || nf.Count > 30 {
		return ErrInvalidCount
	}
	p := b[HeaderSize:]
	for i := uint16(0); i < nf.Count; i++ {
		if err := nf.Recs[i].encode(p); err != nil {
			return err
		}
		p = p[RecordSize:]
	}
	if n, err := wtr.Write(b); err != nil {
		return err
	} else if n != len(b) {
		return ErrInvalidRecordBuffer
	}

	return nil
}

// Read a netflow 4 record from a stream reader
func (nr *NFv5Record) Read(rdr io.Reader) error {
	//read out the IPS
	b := make([]byte, RecordSize)
	if n, err := rdr.Read(b); err != nil {
		return err
	} else if n != RecordSize {
		return ErrInvalidRecordBuffer
	}
	return nr.Decode(b)
}

// Write a netflow 4 record to a stream writer
func (nr *NFv5Record) Write(wtr io.Writer) error {
	//read out the IPS
	b := make([]byte, RecordSize)
	if err := nr.encode(b); err != nil {
		return err
	} else if n, err := wtr.Write(b); err != nil {
		return err
	} else if n != RecordSize {
		return ErrInvalidRecordBuffer
	}
	return nil
}

// Decode pulls a record out of the provided buffer
// no pointers are held on the buffer, so it can be reused
func (nr *NFv5Record) Decode(b []byte) error {
	if len(b) < RecordSize {
		return ErrInvalidRecordBuffer
	}

	copy(nr.ipbuff[0:12], b[0:12])
	nr.Src = net.IP(nr.ipbuff[0:4])
	nr.Dst = net.IP(nr.ipbuff[4:8])
	nr.Next = net.IP(nr.ipbuff[8:12])

	nr.Input = binary.BigEndian.Uint16(b[12:14])
	nr.Output = binary.BigEndian.Uint16(b[14:16])
	nr.Pkts = binary.BigEndian.Uint32(b[16:20])
	nr.Bytes = binary.BigEndian.Uint32(b[20:24])
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

// encode writes a record into a buffer
func (nr *NFv5Record) encode(b []byte) error {
	if len(b) < RecordSize {
		return ErrInvalidRecordBuffer
	}
	if len(nr.Src) == 4 {
		copy(b[0:4], []byte(nr.Src))
	}
	if len(nr.Dst) == 4 {
		copy(b[4:8], []byte(nr.Dst))
	}
	if len(nr.Next) == 4 {
		copy(b[8:12], []byte(nr.Next))
	}

	binary.BigEndian.PutUint16(b[12:14], nr.Input)
	binary.BigEndian.PutUint16(b[14:16], nr.Output)
	binary.BigEndian.PutUint32(b[16:20], nr.Pkts)
	binary.BigEndian.PutUint32(b[20:24], nr.Bytes)
	binary.BigEndian.PutUint32(b[24:28], nr.UptimeFirst)
	binary.BigEndian.PutUint32(b[28:32], nr.UptimeLast)
	binary.BigEndian.PutUint16(b[32:34], nr.SrcPort)
	binary.BigEndian.PutUint16(b[34:36], nr.DstPort)
	b[36] = nr.Pad
	b[37] = nr.Flags
	b[38] = nr.Protocol
	b[39] = nr.ToS
	binary.BigEndian.PutUint16(b[40:42], nr.SrcAs)
	binary.BigEndian.PutUint16(b[42:44], nr.DstAs)
	b[44] = nr.SrcMask
	b[45] = nr.DstMask
	binary.BigEndian.PutUint16(b[46:48], nr.Pad2)
	return nil
}

func (nr *NFv5Record) decodeAlt(b []byte) error {
	if len(b) < RecordSize {
		return ErrInvalidRecordBuffer
	}
	copy(nr.ipbuff[0:12], b[0:12])
	nr.Src = net.IP(nr.ipbuff[0:4])
	nr.Dst = net.IP(nr.ipbuff[4:8])
	nr.Next = net.IP(nr.ipbuff[8:12])

	nr.Input = (uint16(b[12]) << 8) | uint16(b[13])
	nr.Output = (uint16(b[14]) << 8) | uint16(b[15])
	nr.Pkts = (uint32(b[16]) << 24) | (uint32(b[17]) << 16) | (uint32(b[18]) << 8) | uint32(b[19])
	nr.Bytes = (uint32(b[20]) << 24) | (uint32(b[21]) << 16) | (uint32(b[22]) << 8) | uint32(b[23])
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
