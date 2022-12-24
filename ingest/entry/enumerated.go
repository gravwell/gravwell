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
	"fmt"
	"io"
)

const (
	evHeaderLen     = 8                           // 8 bytes of header
	MaxEvNameLength = 1024                        // absolutely bonkers for a name, but constants are scary
	MaxEvDataLength = (63*1024 - evHeaderLen - 1) // are you attaching a NOVEL?
	MaxEvSize       = 0xFFFF                      // this so we can fit the EV header which contains a uint16 length

)

var (
	ErrInvalidName              = errors.New("invalid enumerated value name")
	ErrInvalid                  = errors.New("invalid enumerated value")
	ErrCorruptedEnumeratedValue = errors.New("enumerated value buffer is corrupt")
	ErrTruncatedEnumeratedValue = errors.New("enumerated value buffer is truncated")
)

// type expressed for documentation, we hand jam everything for speed here
type evheader struct {
	totalLen uint16
	nameLen  uint16
	dataLen  uint16
	dataType uint8
	delim    uint8
}

type EnumeratedValue struct {
	Name  string
	Value EnumeratedData
}

// NewEnumeratedvalue will take the data interface and make a best effort to figure out
// what type it is being given and shove it into this encoding
// this is the slowest method for creating an enumerated value, use the native types
func NewEnumeratedValue(name string, data interface{}) (ev EnumeratedValue, err error) {
	if len(name) == 0 || len(name) > MaxEvNameLength {
		err = ErrInvalidName
		return
	}
	// we attempt to support the set of known types, if we can't figure it out
	// we call the stringer on the data portion and stuff it in as a string (TypeUnicode)
	ev.Name = name
	ev.Value, err = InferEnumeratedData(data)
	return
}

// Implement the stringer for Enumerated Values just in case
func (ev EnumeratedValue) String() string {
	return ev.Name + ":" + ev.Value.String()
}

// Valid is a helper function that will indicate if an enumerated value is valid
// to be valid the enumerated value name must be populated and less than MaxEvNameLength
// and the enumerated data must be valid
func (ev EnumeratedValue) Valid() bool {
	if l := len(ev.Name); l == 0 || l > MaxEvNameLength || !ev.Value.Valid() {
		fmt.Println(l, l > MaxEvNameLength, ev.Value.evtype)
		return false
	}
	return true
}

func (ev EnumeratedValue) Size() int {
	return len(ev.Name) + len(ev.Value.data) + evHeaderLen
}

// Encode will pack the enumerated value into a byte slice.  Invalid EVs return nil
func (ev EnumeratedValue) Encode() []byte {
	if !ev.Valid() {
		return nil
	}
	r := make([]byte, evHeaderLen+len(ev.Name)+len(ev.Value.data))

	//drop the header
	binary.LittleEndian.PutUint16(r, uint16(ev.Size()))
	binary.LittleEndian.PutUint16(r[2:], uint16(len(ev.Name)))
	binary.LittleEndian.PutUint16(r[4:], uint16(len(ev.Value.data)))
	r[6] = ev.Value.evtype
	r[7] = 0

	//drop the name
	copy(r[evHeaderLen:evHeaderLen+len(ev.Name)], []byte(ev.Name))

	//drop the data
	copy(r[evHeaderLen+len(ev.Name):], ev.Value.data)

	return r
}

// EncodeWriter will encode an enumerated value into a writer
func (ev EnumeratedValue) EncodeWriter(w io.Writer) error {
	if !ev.Valid() {
		return ErrInvalid
	}
	r := make([]byte, evHeaderLen)
	//drop the header
	binary.LittleEndian.PutUint16(r, uint16(ev.Size()))
	binary.LittleEndian.PutUint16(r[2:], uint16(len(ev.Name)))
	binary.LittleEndian.PutUint16(r[4:], uint16(len(ev.Value.data)))
	r[6] = ev.Value.evtype
	r[7] = 0
	if err := writeAll(w, r); err != nil {
		return err //failed to write header
	} else if err = writeAll(w, []byte(ev.Name)); err != nil {
		return err //failed to write name
	}
	return writeAll(w, ev.Value.data)
}

// Decode will attempt to decode an enumerated value from a byte slice, if the Encode will pack the enumerated value into a byte slice.  Invalid EVs return nil
func (ev *EnumeratedValue) Decode(r []byte) (err error) {
	var h evheader
	if h, err = decodeHeader(r); err != nil {
		return
	}
	if len(r) < int(h.totalLen) {
		return ErrTruncatedEnumeratedValue
	}
	ev.Name = string(r[evHeaderLen : evHeaderLen+h.nameLen])
	ev.Value.data = r[evHeaderLen+h.nameLen:]
	ev.Value.evtype = h.dataType

	if !ev.Valid() {
		err = ErrCorruptedEnumeratedValue
	}
	return
}

func (ev *EnumeratedValue) DecodeReader(r io.Reader) error {
	var h evheader

	//read out the header
	buff := make([]byte, evHeaderLen)
	if err := readAll(r, buff); err != nil {
		return err
	} else if h, err = decodeHeader(buff); err != nil {
		fmt.Println("Failed to decode header", err)
		return err
	}

	//read out the name
	buff = make([]byte, h.nameLen)
	if err := readAll(r, buff); err != nil {
		return err
	}
	ev.Name = string(buff)

	ev.Value.evtype = h.dataType
	ev.Value.data = make([]byte, h.dataLen)
	if err := readAll(r, ev.Value.data); err != nil {
		return err
	}
	if !ev.Valid() {
		return ErrCorruptedEnumeratedValue
	}
	return nil //all good
}

func decodeHeader(r []byte) (h evheader, err error) {
	//make sure we can at least grab a header
	if len(r) < evHeaderLen {
		err = ErrTruncatedEnumeratedValue
		return
	} else if r[7] != 0 {
		err = ErrCorruptedEnumeratedValue
		return
	}
	if h.totalLen = binary.LittleEndian.Uint16(r); h.totalLen > MaxEvSize {
		err = ErrCorruptedEnumeratedValue
		return
	}
	if h.nameLen = binary.LittleEndian.Uint16(r[2:]); h.nameLen == 0 || h.nameLen > MaxEvNameLength {
		err = ErrCorruptedEnumeratedValue
		return
	}
	if h.dataLen = binary.LittleEndian.Uint16(r[4:]); h.dataLen > MaxEvDataLength {
		err = ErrCorruptedEnumeratedValue
		return
	}
	h.dataType = r[6]

	//check purported lengths
	if (h.nameLen + h.dataLen + evHeaderLen) != h.totalLen {
		err = ErrCorruptedEnumeratedValue
	}
	return
}

// Compare is a helper function to do comparisons and get errors out describing what is not the same
func (ev EnumeratedValue) Compare(ev2 EnumeratedValue) (err error) {
	//make sure its identical to what went in
	if ev.Name != ev2.Name {
		return fmt.Errorf("Names do not")
	} else if ev.Value.evtype != ev2.Value.evtype {
		return fmt.Errorf("evtypes do not match: %d != %d", ev.Value.evtype, ev2.Value.evtype)
	} else if !bytes.Equal(ev.Value.data, ev2.Value.data) {
		return fmt.Errorf("data buffers do not match")
	}
	return nil
}
