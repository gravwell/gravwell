/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
)

const (
	/* 34 = 4 + 8 + 8 + 2 + 16 */
	ENTRY_HEADER_SIZE int      = 34
	SRC_SIZE          int      = 16
	IPV4_SRC_SIZE     int      = 4
	DefaultTagName    string   = `default`
	DefaultTagId      EntryTag = 0
	GravwellTagName   string   = `gravwell`
	GravwellTagId     EntryTag = 0xFFFF

	MaxDataSize          uint32 = 0x7FFFFFFF
	MaxSliceCount        uint32 = 0x3FFFFFFF
	maxSliceAllocSize    int    = 0x4000000  //if a slice is less than 64MB, do it all at once
	maxSliceTransferSize uint64 = 0xffffffff //slices can't be larger than 4GB in one transfer
)

var (
	ErrInvalidHeader     = errors.New("Invalid Entry header in decode")
	ErrInvalidBufferSize = errors.New("Invalid buffer size, too small")
	ErrFailedHeaderWrite = errors.New("Failed to write header while encoding")
	ErrFailedBodyWrite   = errors.New("Failed to write body while encoding")
	ErrFailedBodyRead    = errors.New("Failed to read body while decoding")
	ErrSliceLenTooLarge  = errors.New("Slice length is too large for encoding")
	ErrSliceSizeTooLarge = errors.New("Slice size is too large for encoding")
)

type EntryTag uint16
type EntryKey int64
type Entry struct {
	TS   Timestamp
	SRC  net.IP
	Tag  EntryTag
	Data []byte
}

func (ent *Entry) Key() EntryKey {
	return EntryKey(ent.TS.Sec)
}

func (ent *Entry) Size() uint64 {
	return uint64(len(ent.Data)) + uint64(ENTRY_HEADER_SIZE)
}

//decodeHeader copies copies the SRC buffer
func (ent *Entry) decodeHeader(buff []byte) int {
	var datasize uint32
	var ipv4 bool
	/* buffer should come formatted as follows:
	data size uint32
	TS seconds (int64)
	TS nanoseconds (int64)
	Tag (16bit)
	SRC (16 bytes)
	*/
	//TODO: force this to LittleEndian
	datasize = binary.LittleEndian.Uint32(buff)
	//check if we are an ipv4 address
	if (datasize & 0x80000000) != 0 {
		ipv4 = true
		datasize &= 0x7FFFFFFF //clear the bit
	}
	ent.TS.Decode(buff[4:])
	ent.Tag = EntryTag(binary.LittleEndian.Uint16(buff[16:]))
	if ipv4 {
		if len(ent.SRC) < IPV4_SRC_SIZE {
			ent.SRC = make([]byte, IPV4_SRC_SIZE)
		}
		copy(ent.SRC, buff[18:22])
		ent.SRC = ent.SRC[:4]
	} else {
		if len(ent.SRC) < SRC_SIZE {
			ent.SRC = make([]byte, SRC_SIZE)
		}
		copy(ent.SRC, buff[18:ENTRY_HEADER_SIZE])
	}
	return int(datasize)
}

//decodeHeaderAlt gets a direct handle on the SRC buffer
func (ent *Entry) decodeHeaderAlt(buff []byte) int {
	var datasize uint32
	var ipv4 bool
	/* buffer should come formatted as follows:
	data size uint32
	TS seconds (int64)
	TS nanoseconds (int64)
	Tag (16bit)
	SRC (16 bytes)
	*/
	//TODO: force this to LittleEndian
	datasize = binary.LittleEndian.Uint32(buff)
	//check if we are an ipv4 address
	if (datasize & 0x80000000) != 0 {
		ipv4 = true
		datasize &= 0x7FFFFFFF //clear the bit
	}
	ent.TS.Decode(buff[4:])
	ent.Tag = EntryTag(binary.LittleEndian.Uint16(buff[16:]))
	if ipv4 {
		ent.SRC = buff[18:22]
	} else {
		ent.SRC = buff[18:ENTRY_HEADER_SIZE]
	}
	return int(datasize)
}

func (ent *Entry) DecodeHeader(buff []byte) (int, error) {
	if len(buff) < ENTRY_HEADER_SIZE {
		return 0, ErrInvalidBufferSize
	}
	return ent.decodeHeader(buff), nil
}

//DecodeEntry will copy values out of the buffer to generate an entry with its own
//copies of data.  This ensures that entries don't maintain ties to blocks
//DecodeEntry assumes that a size check has already happened
func (ent *Entry) DecodeEntry(buff []byte) {
	dataSize := ent.decodeHeader(buff)
	ent.Data = append([]byte(nil), buff[ENTRY_HEADER_SIZE:ENTRY_HEADER_SIZE+int(dataSize)]...)
}

//DecodeEntryAlt doesn't copy the SRC or data out, it just references the slice handed in
//it also assumes a size check for the entry header size has occurred by the caller
func (ent *Entry) DecodeEntryAlt(buff []byte) {
	dataSize := ent.decodeHeaderAlt(buff)
	ent.Data = buff[ENTRY_HEADER_SIZE : ENTRY_HEADER_SIZE+int(dataSize)]
}

//EncodeHeader Encodes the header into the buffer for the file transport
//think file indexer
func (ent *Entry) EncodeHeader(buff []byte) error {
	if len(buff) < ENTRY_HEADER_SIZE {
		return ErrInvalidBufferSize
	}
	/* buffer should come formatted as follows in littleendian format:
	data size (uint32)
	TS seconds (int64)
	TS nanoseconds (uint32)
	Tag (16bit)
	SRC (16 bytes)
	*/
	//TODO: force this to LittleEndian
	if len(ent.SRC) == IPV4_SRC_SIZE {
		binary.LittleEndian.PutUint32(buff, uint32(len(ent.Data))|0x80000000)
	} else {
		binary.LittleEndian.PutUint32(buff, uint32(len(ent.Data)))
	}
	ent.TS.Encode(buff[4:16])
	binary.LittleEndian.PutUint16(buff[16:], uint16(ent.Tag))
	copy(buff[18:ENTRY_HEADER_SIZE], ent.SRC)
	return nil
}

func (ent *Entry) Encode(buff []byte) error {
	if err := ent.EncodeHeader(buff); err != nil {
		return err
	}
	if len(buff) < (len(ent.Data) + ENTRY_HEADER_SIZE) {
		return ErrInvalidBufferSize
	}
	copy(buff[ENTRY_HEADER_SIZE:], ent.Data)
	return nil
}

func writeAll(wtr io.Writer, buff []byte) error {
	var written int
	for written < len(buff) {
		n, err := wtr.Write(buff[written:])
		if err != nil {
			return err
		}
		if n <= 0 {
			return ErrFailedBodyWrite
		}
		written += n
	}
	return nil
}

func readAll(rdr io.Reader, buff []byte) error {
	var r int
	for r < len(buff) {
		n, err := rdr.Read(buff[r:])
		if err != nil {
			return err
		}
		if n <= 0 {
			return ErrFailedBodyRead
		}
		r += n
	}
	return nil
}

func (ent *Entry) EncodeWriter(wtr io.Writer) error {
	headerBuff := make([]byte, ENTRY_HEADER_SIZE)
	if err := ent.EncodeHeader(headerBuff); err != nil {
		return err
	}
	if n, err := wtr.Write(headerBuff); err != nil {
		return err
	} else if n != ENTRY_HEADER_SIZE {
		return ErrFailedHeaderWrite
	}
	return writeAll(wtr, ent.Data)
}

type EntrySlice []Entry

func (es EntrySlice) EncodeWriter(wtr io.Writer) error {
	if len(es) > int(MaxSliceCount) {
		return ErrSliceLenTooLarge
	}
	sz := es.Size()
	if sz > maxSliceTransferSize {
		return ErrSliceSizeTooLarge
	}
	//write the count as a little endian uint32
	if err := binary.Write(wtr, binary.LittleEndian, uint32(len(es))); err != nil {
		return err
	}
	//write the count as a little endian uint32
	if err := binary.Write(wtr, binary.LittleEndian, uint32(sz)); err != nil {
		return err
	}
	for i := range es {
		if err := es[i].EncodeWriter(wtr); err != nil {
			return err
		}
	}
	return nil
}

func (es *EntrySlice) DecodeReader(rdr io.Reader) error {
	var l uint32
	var sz uint32
	//write the count as a little endian uint32
	if err := binary.Read(rdr, binary.LittleEndian, &l); err != nil {
		return err
	}
	if l > MaxSliceCount {
		return ErrSliceLenTooLarge
	}
	//write the count as a little endian uint32
	if err := binary.Read(rdr, binary.LittleEndian, &sz); err != nil {
		return err
	}

	*es = make(EntrySlice, int(l))
	for i := range *es {
		if err := (*es)[i].DecodeReader(rdr); err != nil {
			return err
		}
	}
	return nil
}

func (es *EntrySlice) Size() uint64 {
	sz := uint64(8) //uint32 len and size header

	for i := range *es {
		sz += (*es)[i].Size()
	}
	return sz
}

func (ent *Entry) DecodeReader(rdr io.Reader) error {
	headerBuff := make([]byte, ENTRY_HEADER_SIZE)
	if err := readAll(rdr, headerBuff); err != nil {
		return err
	}
	n := ent.decodeHeader(headerBuff)
	if n <= 0 || n > (int(MaxDataSize)-ENTRY_HEADER_SIZE) {
		return ErrInvalidHeader
	}
	ent.Data = make([]byte, n)
	if err := readAll(rdr, ent.Data); err != nil {
		return err
	}
	return nil
}

func (ent *Entry) MarshallBytes() ([]byte, error) {
	buff := make([]byte, len(ent.Data)+ENTRY_HEADER_SIZE)
	if err := ent.EncodeHeader(buff); err != nil {
		return nil, err
	}
	if len(buff) < (len(ent.Data) + ENTRY_HEADER_SIZE) {
		return nil, ErrInvalidBufferSize
	}
	copy(buff[ENTRY_HEADER_SIZE:], ent.Data)
	return buff, nil
}

// DeepCopy provides a complete copy of an entry, this is REALLY expensive, so make sure its worth it
func (ent *Entry) DeepCopy() (c Entry) {
	c.TS = ent.TS
	c.SRC = append(net.IP(nil), ent.SRC...)
	c.Tag = ent.Tag
	c.Data = append([]byte(nil), ent.Data...)
	return
}
