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
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
)

const (
	streamBlock = 10
)

func throw(igst *ingest.IngestMuxer, tag entry.EntryTag, cnt uint64, dur time.Duration) (err error) {
	sp := dur / time.Duration(cnt)
	ts := time.Now().Add(-1 * dur)
	var dt []byte
	for i := uint64(0); i < cnt; i++ {
		if dt, err = genData(); err != nil {
			return
		}
		if err = igst.WriteEntry(&entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			SRC:  src,
			Data: dt,
		}); err != nil {
			return
		}
		ts = ts.Add(sp)
		totalBytes += uint64(len(dt))
	}
	return
}

func stream(igst *ingest.IngestMuxer, tag entry.EntryTag, cnt uint64) (err error) {
	var blksize uint64
	if cnt < streamBlock {
		blksize = 1
	} else {
		blksize = streamBlock
	}
	sp := time.Second / time.Duration((cnt / blksize))
	var dt []byte

mainLoop:
	for {
		for i := uint64(0); i < blksize; i++ {
			ts := time.Now()
			if dt, err = genData(); err != nil {
				break mainLoop
			}
			if err = igst.WriteEntry(&entry.Entry{
				TS:   entry.FromStandard(ts),
				Tag:  tag,
				SRC:  src,
				Data: dt,
			}); err != nil {
				break mainLoop
			}
			totalBytes += uint64(len(dt))
		}
		time.Sleep(sp)
	}
	return
}

//format int16:beuint16:int32:beuint32:int64:beuint64:float32:befloat64:IPv4:string
func genData() ([]byte, error) {
	bb := bytes.NewBuffer(make([]byte, 128))
	bb.Reset()
	//16 bits
	if err := binary.Write(bb, binary.LittleEndian, int16(rand.Intn(0xffff))); err != nil {
		return nil, err
	}
	if err := binary.Write(bb, binary.BigEndian, uint16(rand.Intn(0xffff))); err != nil {
		return nil, err
	}
	//32 bits
	if err := binary.Write(bb, binary.LittleEndian, rand.Int31()); err != nil {
		return nil, err
	}
	if err := binary.Write(bb, binary.BigEndian, rand.Uint32()); err != nil {
		return nil, err
	}
	//64 bits
	if err := binary.Write(bb, binary.LittleEndian, rand.Int63()); err != nil {
		return nil, err
	}
	if err := binary.Write(bb, binary.BigEndian, rand.Uint64()); err != nil {
		return nil, err
	}
	//floats
	if err := binary.Write(bb, binary.LittleEndian, rand.Float32()); err != nil {
		return nil, err
	}
	if err := binary.Write(bb, binary.BigEndian, rand.Float64()); err != nil {
		return nil, err
	}
	ip := make([]byte, 4)
	*(*uint32)(unsafe.Pointer(&ip[0])) = rand.Uint32()
	ip[0] = 10
	if _, err := bb.Write([]byte(ip)); err != nil {
		return nil, err
	}
	if _, err := bb.WriteString(rd.FullName(rand.Intn(2))); err != nil {
		return nil, err
	}
	return bb.Bytes(), nil
}
