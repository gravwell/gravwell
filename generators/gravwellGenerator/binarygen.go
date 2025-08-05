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
	"math/rand"
	"time"
	"unsafe"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

func finBinary(ent *entry.Entry) {
	ent.AddEnumeratedValueEx(`_type`, "binary")
}

// format int16:beuint16:int32:beuint32:int64:beuint64:float32:befloat64:IPv4:string
func genDataBinary(ts time.Time) []byte {
	bb := bytes.NewBuffer(make([]byte, 128))
	bb.Reset()
	//16 bits
	if err := binary.Write(bb, binary.LittleEndian, int16(rand.Intn(0xffff))); err != nil {
		return bb.Bytes()
	}
	if err := binary.Write(bb, binary.BigEndian, uint16(rand.Intn(0xffff))); err != nil {
		return bb.Bytes()
	}
	//32 bits
	if err := binary.Write(bb, binary.LittleEndian, rand.Int31()); err != nil {
		return bb.Bytes()
	}
	if err := binary.Write(bb, binary.BigEndian, rand.Uint32()); err != nil {
		return bb.Bytes()
	}
	//64 bits
	if err := binary.Write(bb, binary.LittleEndian, rand.Int63()); err != nil {
		return bb.Bytes()
	}
	if err := binary.Write(bb, binary.BigEndian, rand.Uint64()); err != nil {
		return bb.Bytes()
	}
	//floats
	if err := binary.Write(bb, binary.LittleEndian, rand.Float32()); err != nil {
		return bb.Bytes()
	}
	if err := binary.Write(bb, binary.BigEndian, rand.Float64()); err != nil {
		return bb.Bytes()
	}
	ip := make([]byte, 4)
	*(*uint32)(unsafe.Pointer(&ip[0])) = rand.Uint32()
	ip[0] = 10
	if _, err := bb.Write([]byte(ip)); err != nil {
		return bb.Bytes()
	}
	if _, err := bb.WriteString(rd.FullName(rand.Intn(2))); err != nil {
		return bb.Bytes()
	}
	return bb.Bytes()
}
