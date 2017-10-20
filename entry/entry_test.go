/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry

import (
	"fmt"
	"net"
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	ts := Now()
	ts.Sec--
	e := &Entry{
		TS:   ts,
		Tag:  EntryTag(0x1234),
		SRC:  net.ParseIP("DEAD::BEEF"),
		Data: make([]byte, 0x99),
	}
	//encode the header
	b := make([]byte, ENTRY_HEADER_SIZE)
	if err := e.EncodeHeader(b); err != nil {
		t.Fatal(err)
	}
	var e2 Entry
	n, err := e2.DecodeHeader(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Data) != n {
		t.Fatal("Data length extracted is incorrect")
	}
	if e.TS != e2.TS {
		t.Fatal(fmt.Errorf("TS mismatch: %v != %v", e.TS, e2.TS))
	}
	if e.Tag != e2.Tag {
		t.Fatal(fmt.Errorf("Tag mismatch: %x != %x", e.Tag, e2.Tag))
	}
	if len(e.Data) != n {
		t.Fatal(fmt.Errorf("datalen mismatch: %v != %v", len(e.Data), n))
	}
}

func BenchmarkDecode(b *testing.B) {
	b.StopTimer()
	header := make([]byte, ENTRY_HEADER_SIZE)
	e := Entry{
		TS:  Now(),
		SRC: net.ParseIP("DEAD::BEEF"),
		Tag: 0x1337,
	}
	if err := e.EncodeHeader(header); err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.DecodeHeader(header); err != nil {
			b.Fatal(err)
		}
		if e.Tag != 0x1337 {
			b.Fatal("bad tag")
		}
	}
}

func BenchmarkDataCopy(b *testing.B) {
	b.StopTimer()
	buff := make([]byte, 4096)
	for i := 0; i < len(buff); i++ {
		buff[i] = byte(i % 0xff)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		x := make([]byte, len(buff))
		copy(x, buff)
		if len(x) != len(buff) || &x[0] == &buff[0] {
			b.Fatal("uuuh, yeah")
		}
	}
}

func BenchmarkDataAppend(b *testing.B) {
	b.StopTimer()
	buff := make([]byte, 4096)
	for i := 0; i < len(buff); i++ {
		buff[i] = byte(i % 0xff)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		x := append([]byte(nil), buff...)
		if len(x) != len(buff) || &x[0] == &buff[0] {
			b.Fatal("uuuh, yeah")
		}
	}
}
