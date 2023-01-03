/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
)

const (
	/* 34 = 4 + 8 + 8 + 2 + 16

	 */
	ENTRY_HEADER_SIZE int = 34
	SRC_SIZE          int = 16
	IPV4_SRC_SIZE     int = 4

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
	ErrDataSizeTooLarge  = errors.New("Entry data size is too large, must be < 1GB")
)

type EntryTag uint16
type EntryKey int64
type Entry struct {
	TS   Timestamp
	SRC  net.IP
	Tag  EntryTag
	Data []byte
	evb  evblock
}

func (ent *Entry) Key() EntryKey {
	return EntryKey(ent.TS.Sec)
}

// EnumeratedValues returns the slice of enumerated values, this is an accessor to prevent direct assignment
func (ent Entry) EnumeratedValues() []EnumeratedValue {
	return ent.evb.Values()
}

// ClearEnumeratedValues is a convienence function to remove all enumerated values
func (ent *Entry) ClearEnumeratedValues() {
	ent.evb.Reset()
}

func (ent *Entry) AddEnumeratedValue(ev EnumeratedValue) (err error) {
	if ev.Valid() {
		ent.evb.Add(ev)
	} else {
		err = ErrInvalid
	}
	return
}

func (ent *Entry) AddEnumeratedValueEx(name string, val interface{}) error {
	ev, err := NewEnumeratedValue(name, val)
	if err != nil {
		return err
	}
	ent.evb.Add(ev)
	return nil
}

func (ent *Entry) Size() uint64 {
	return uint64(len(ent.Data)) + uint64(ENTRY_HEADER_SIZE) + ent.evb.Size()
}

// DecodeHeader hands back a completely decoded header with direct references to the underlying data
func DecodeHeader(buff []byte) (ts Timestamp, src net.IP, tag EntryTag, hasEvs bool, datasize uint32) {
	var ipv4 bool
	/* buffer should come formatted as follows:
	data size uint32  //top 2 bits contain flags
	TS seconds (int64)
	TS nanoseconds (int64)
	Tag (16bit)
	SRC (16 bytes)
	*/

	//decode the datasize and grab the flags from the datasize
	datasize = binary.LittleEndian.Uint32(buff)
	flags := uint8(datasize >> 30)
	datasize &= flagMask // clear flags from datasize
	hasEvs = ((flags & flagEVs) != 0)

	//check if we are an ipv4 address
	if (flags & flagIPv4) != 0 {
		ipv4 = true
	}
	ts.Decode(buff[4:])
	tag = EntryTag(binary.LittleEndian.Uint16(buff[16:]))
	if ipv4 {
		src = buff[18:22]
	} else {
		src = buff[18:ENTRY_HEADER_SIZE]
	}
	return
}

// DecodeHeaderTagSec checks that the buffer is big enough for a header then ONLY extracts the tag and second component of the timestamp
// this function is used for rapidly scanning an entry header to decide if we want to decode it
// we assume the caller has already ensured that the buffer is large enough to at least contain a header
func DecodeHeaderTagSec(buff []byte) (tag EntryTag, sec int64) {
	tag = EntryTag(binary.LittleEndian.Uint16(buff[16:]))
	sec = int64(binary.LittleEndian.Uint64(buff[4:]))
	return
}

// EntrySize just decodes enough of the header to decide the actual encoded size of an entry
// this function is typically used for rapidly skipping an entry
func EntrySize(buff []byte) (n int, err error) {
	if len(buff) < ENTRY_HEADER_SIZE {
		err = ErrInvalidHeader
		return
	}
	datasize := binary.LittleEndian.Uint32(buff)
	flags := uint8(datasize >> 30)

	n = int(datasize & flagMask) // clear flags from datasize
	if len(buff) < n {
		err = ErrInvalidBufferSize
		return
	}
	if (flags & flagEVs) == 0 {
		return
	}

	//we have EVs, check the buffer again
	var hdr EVBlockHeader
	if hdr, err = DecodeEVBlockHeader(buff[n:]); err == nil {
		n += int(hdr.Size)
	}
	return
}

// DecodePartialHeader decodes only the timestamp second, tag, hasEvs, and DataSize
// this function is used for quickly scanning through entries in their encoded form
func DecodePartialHeader(buff []byte) (ts Timestamp, tag EntryTag, ipv4, hasEvs bool, datasize uint32) {
	//decode the datasize and grab the flags from the datasize
	datasize = binary.LittleEndian.Uint32(buff)
	flags := uint8(datasize >> 30)
	datasize &= flagMask // clear flags from datasize
	hasEvs = ((flags & flagEVs) != 0)
	ipv4 = ((flags & flagIPv4) != 0)
	tag = EntryTag(binary.LittleEndian.Uint16(buff[16:]))
	ts.Decode(buff[4:])
	return
}

// decodeHeader copies copies the SRC buffer
func (ent *Entry) decodeHeader(buff []byte) (int, bool) {
	var hasEvs bool
	var datasize uint32
	var src net.IP
	ent.TS, src, ent.Tag, hasEvs, datasize = DecodeHeader(buff)
	ent.SRC = append(net.IP(nil), src...)
	return int(datasize), hasEvs
}

// decodeHeaderAlt gets a direct handle on the SRC buffer
func (ent *Entry) decodeHeaderAlt(buff []byte) (int, bool) {
	var hasEvs bool
	var datasize uint32
	ent.TS, ent.SRC, ent.Tag, hasEvs, datasize = DecodeHeader(buff)
	return int(datasize), hasEvs
}

func (ent *Entry) DecodeHeader(buff []byte) (int, bool, error) {
	if len(buff) < ENTRY_HEADER_SIZE {
		return 0, false, ErrInvalidBufferSize
	}
	dataLen, hasEvs := ent.decodeHeader(buff)
	return dataLen, hasEvs, nil
}

// DecodeEntry will copy values out of the buffer to generate an entry with its own
// copies of data.  This ensures that entries don't maintain ties to blocks
// DecodeEntry assumes that a size check has already happened
// You probably want Decode
func (ent *Entry) DecodeEntry(buff []byte) (err error) {
	dataSize, hasEvs := ent.decodeHeader(buff)
	ent.Data = append([]byte(nil), buff[ENTRY_HEADER_SIZE:ENTRY_HEADER_SIZE+int(dataSize)]...)
	if hasEvs {
		_, err = ent.evb.Decode(append([]byte(nil), buff[:ENTRY_HEADER_SIZE+int(dataSize)]...))
	}
	return
}

// DecodeEntryAlt doesn't copy the SRC or data out, it just references the slice handed in
// it also assumes a size check for the entry header size has occurred by the caller
// You probably want DecodeAlt
func (ent *Entry) DecodeEntryAlt(buff []byte) (err error) {
	dataSize, hasEvs := ent.decodeHeaderAlt(buff)
	ent.Data = buff[ENTRY_HEADER_SIZE : ENTRY_HEADER_SIZE+int(dataSize)]
	if hasEvs {
		buff = buff[:ENTRY_HEADER_SIZE+int(dataSize)]
		_, err = ent.evb.Decode(buff)
	}
	return
}

// Decode completely decodes an entry and returns the number of bytes consumed from a buffer
// This is useful for iterating over entries in a raw buffer.
// Decode will decode the entire entry and all of its EVs, copying all bytes so that
// the caller can re-use the underlying buffer
func (ent *Entry) Decode(buff []byte) (int, error) {
	var off int
	dataSize, hasEvs, err := ent.DecodeHeader(buff)
	if err != nil {
		return -1, err
	}
	off += ENTRY_HEADER_SIZE
	if buff = buff[ENTRY_HEADER_SIZE:]; len(buff) < dataSize {
		return -1, ErrInvalidBufferSize
	}
	ent.Data = append([]byte(nil), buff[:int(dataSize)]...)
	buff = buff[dataSize:]
	off += int(dataSize)
	if hasEvs {
		var n int
		if n, err = ent.evb.Decode(buff); err == nil {
			off += n
		}
	}
	return off, err
}

// DecodeAlt completely decodes an entry and returns the number of bytes consumed from a buffer
// This is useful for iterating over entries in a raw buffer.
// This decode method directly references the underlying buffer, callers cannot re-use the buffer
// if the entry and/or its EVs will be used
func (ent *Entry) DecodeAlt(buff []byte) (int, error) {
	var off int
	if len(buff) < ENTRY_HEADER_SIZE {
		return 0, ErrInvalidBufferSize
	}
	dataSize, hasEvs := ent.decodeHeaderAlt(buff)
	off += ENTRY_HEADER_SIZE
	if buff = buff[ENTRY_HEADER_SIZE:]; len(buff) < dataSize {
		return 0, ErrInvalidBufferSize
	}
	ent.Data = buff[:int(dataSize)]
	buff = buff[dataSize:]
	off += int(dataSize)
	if hasEvs {
		n, err := ent.evb.DecodeAlt(buff)
		if err != nil {
			return 0, err
		}
		off += n
	}
	return off, nil
}

// EncodeHeader Encodes the header into the buffer for the file transport think file indexer
// EncodeHeader returns a boolean indicating if EVs are marked
func (ent *Entry) EncodeHeader(buff []byte) (bool, error) {
	if len(buff) < ENTRY_HEADER_SIZE {
		return false, ErrInvalidBufferSize
	} else if len(ent.Data) > int(MaxDataSize) {
		return false, ErrDataSizeTooLarge
	}
	/* buffer should come formatted as follows in littleendian format:
	data size (uint32)
	TS seconds (int64)
	TS nanoseconds (uint32)
	Tag (16bit)
	SRC (16 bytes)
	*/
	var hasEvs bool
	var flags uint8
	if len(ent.SRC) == IPV4_SRC_SIZE {
		flags |= flagIPv4
	}
	if ent.evb.Populated() {
		flags |= flagEVs
		hasEvs = true
	}
	binary.LittleEndian.PutUint32(buff, uint32(len(ent.Data)))
	buff[3] |= (flags << 6) //mask in the flags
	ent.TS.Encode(buff[4:16])
	binary.LittleEndian.PutUint16(buff[16:], uint16(ent.Tag))
	copy(buff[18:ENTRY_HEADER_SIZE], ent.SRC)
	return hasEvs, nil
}

func (ent *Entry) Encode(buff []byte) (int, error) {
	hasEvs, err := ent.EncodeHeader(buff)
	if err != nil {
		return -1, err
	}
	if len(buff) < (len(ent.Data) + ENTRY_HEADER_SIZE) {
		return -1, ErrInvalidBufferSize
	}
	copy(buff[ENTRY_HEADER_SIZE:], ent.Data)
	r := len(ent.Data) + ENTRY_HEADER_SIZE
	if hasEvs {
		n, err := ent.evb.EncodeBuffer(buff[r:])
		if err != nil {
			return -1, err
		}
		r += n
	}
	return r, nil
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

func (ent *Entry) EncodeWriter(wtr io.Writer) (int, error) {
	headerBuff := make([]byte, ENTRY_HEADER_SIZE)
	hasEvs, err := ent.EncodeHeader(headerBuff)
	if err != nil {
		return -1, err
	}
	n, err := wtr.Write(headerBuff)
	if err != nil {
		return -1, err
	} else if n != ENTRY_HEADER_SIZE {
		return -1, ErrFailedHeaderWrite
	} else if err = writeAll(wtr, ent.Data); err != nil {
		return -1, err
	} else {
		n += len(ent.Data)
	}
	if hasEvs {
		nn, err := ent.evb.EncodeWriter(wtr)
		if err != nil {
			return -1, err
		}
		n += nn
	}
	return n, err
}

func (ent *Entry) EVCount() int {
	return ent.evb.Count()
}

func (ent *Entry) EVSize() int {
	return int(ent.evb.Size())
}

func (ent *Entry) EVEncodeWriter(wtr io.Writer) (int, error) {
	return ent.evb.EncodeWriter(wtr)
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
		if _, err := es[i].EncodeWriter(wtr); err != nil {
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
	n, hasEvs := ent.decodeHeader(headerBuff)
	if n <= 0 || n > (int(MaxDataSize)-ENTRY_HEADER_SIZE) {
		return ErrInvalidHeader
	}
	ent.Data = make([]byte, n)
	if err := readAll(rdr, ent.Data); err != nil {
		return err
	}
	if hasEvs {
		_, err := ent.evb.DecodeReader(rdr)
		return err
	} else {
		ent.evb.Reset()
	}
	return nil
}

func (ent *Entry) ReadEVs(rdr io.Reader) error {
	_, err := ent.evb.DecodeReader(rdr)
	return err
}

func (ent *Entry) MarshallBytes() ([]byte, error) {
	buff := make([]byte, ent.Size())
	if _, err := ent.Encode(buff); err != nil {
		return nil, err
	}
	return buff, nil
}

// DeepCopy provides a complete copy of an entry, this is REALLY expensive, so make sure its worth it
func (ent *Entry) DeepCopy() (c Entry) {
	c.TS = ent.TS
	if len(ent.SRC) > 0 {
		c.SRC = append(net.IP(nil), ent.SRC...)
	}
	c.Tag = ent.Tag
	if len(ent.Data) > 0 {
		c.Data = append([]byte(nil), ent.Data...)
	}
	c.evb = ent.evb.DeepCopy()
	return
}

func (ent *Entry) Compare(v *Entry) error {
	if ent == nil {
		if v == nil {
			return nil
		}
		return errors.New("mismatched nil")
	} else if v == nil {
		return errors.New("mismatched nil")
	}
	if ent.TS != v.TS {
		return errors.New("differing timestamp")
	} else if ent.Tag != v.Tag {
		return errors.New("differing tags")
	} else if bytes.Compare(ent.SRC, v.SRC) != 0 {
		return errors.New("differeing source values")
	} else if bytes.Compare(ent.Data, v.Data) != 0 {
		return errors.New("differing data")
	}
	return ent.evb.Compare(v.evb)
}
