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
	"fmt"
	"net"
	"testing"
	"time"
)

func TestEncodeDecodeHeader(t *testing.T) {
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
	n, hasEvs, err := e2.DecodeHeader(b)
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
	if hasEvs {
		t.Fatal("we don't have evs, but header thought so")
	}
}

// Test encoding and decoding without any EVs
func TestEncodeDecodeBasic(t *testing.T) {
	ts := Now()
	e := &Entry{
		TS:   ts,
		Tag:  EntryTag(0x1234),
		SRC:  net.ParseIP("DEAD::BEEF"),
		Data: make([]byte, 0x99),
	}

	//make a buffer big enough
	buff := make([]byte, e.Size())
	if n, err := e.Encode(buff); err != nil {
		t.Fatal(err)
	} else if n != len(buff) { //check that the reported size matches the encoded size
		t.Fatalf("encoded buffer size is incorrect: %d != %d", n, len(buff))
	}

	//encode using a writer and compare buffers
	bb := bytes.NewBuffer(nil)
	if n, err := e.EncodeWriter(bb); err != nil {
		t.Fatal(err)
	} else if bb.Len() != n {
		t.Fatalf("encoded writer size is incorrect: %d != %d", n, len(buff))
	}

	//check that the encoded output was the same
	if bytes.Compare(bb.Bytes(), buff) != 0 {
		t.Fatalf("encoded output types do not match")
	}

	//decode using both methods
	var e2 Entry
	if _, err := e2.Decode(buff); err != nil {
		t.Fatal(err)
	} else if err = e2.Compare(e); err != nil {
		t.Fatal(err)
	}

	var e3 Entry
	if err := e3.DecodeReader(bb); err != nil {
		t.Fatal(err)
	} else if err = e3.Compare(e); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeDecodeEnumeratedValues(t *testing.T) {
	ts := Now()
	e := &Entry{
		TS:   ts,
		Tag:  EntryTag(0x1234),
		SRC:  net.ParseIP("DEAD::BEEF"),
		Data: make([]byte, 0x99),
	}
	addAllEvs(e)

	//make a buffer big enough
	buff := make([]byte, e.Size())
	if n, err := e.Encode(buff); err != nil {
		t.Fatal(err)
	} else if n != len(buff) { //check that the reported size matches the encoded size
		t.Fatalf("encoded buffer size is incorrect: %d != %d", n, len(buff))
	}

	e = &Entry{
		TS:   ts,
		Tag:  EntryTag(0x1234),
		SRC:  net.ParseIP("DEAD::BEEF"),
		Data: make([]byte, 0x99),
	}
	addAllEvs(e)

	//encode using a writer and compare buffers
	bb := bytes.NewBuffer(nil)
	if n, err := e.EncodeWriter(bb); err != nil {
		t.Fatal(err)
	} else if bb.Len() != n {
		t.Fatalf("encoded writer size is incorrect: %d != %d", n, len(buff))
	}

	//check that the encoded output was the same
	if bytes.Compare(bb.Bytes(), buff) != 0 {
		t.Fatalf("encoded output types do not match")
	}

	//decode using both methods
	var e2 Entry
	if _, err := e2.Decode(buff); err != nil {
		t.Fatal(err)
	} else if err = e2.Compare(e); err != nil {
		t.Fatal(err)
	}

	var e3 Entry
	if err := e3.DecodeReader(bb); err != nil {
		t.Fatal(err)
	} else if err = e3.Compare(e); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkDecode(b *testing.B) {
	b.StopTimer()
	bts := make([]byte, ENTRY_HEADER_SIZE+1024)
	data := bts[ENTRY_HEADER_SIZE:]
	for i := range data {
		data[i] = byte(i)
	}
	e := Entry{
		TS:   Now(),
		SRC:  net.ParseIP("DEAD::BEEF"),
		Tag:  0x1337,
		Data: data,
	}
	if _, err := e.Encode(bts); err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		e.DecodeEntry(bts)
		if e.Tag != 0x1337 {
			b.Fatal("bad tag")
		}
	}
}

func BenchmarkDecodeAlt(b *testing.B) {
	b.StopTimer()
	bts := make([]byte, ENTRY_HEADER_SIZE+1024)
	data := bts[ENTRY_HEADER_SIZE:]
	for i := range data {
		data[i] = byte(i)
	}
	e := Entry{
		TS:   Now(),
		SRC:  net.ParseIP("DEAD::BEEF"),
		Tag:  0x1337,
		Data: data,
	}
	if _, err := e.Encode(bts); err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		e.DecodeEntryAlt(bts)
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

// adds an EV of every type
func addAllEvs(ent *Entry) (err error) {
	if err = ent.AddEnumeratedValueEx(`n1`, true); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n2`, byte(0)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n3`, int8(42)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n4`, int16(0xbcd)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n5`, uint16(0xdead)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n6`, int32(0x99beef)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n7`, uint32(0xfeedbeef)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n8`, int64(0xdeadfeedbeef)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n9`, uint64(0xfeeddeadfeedbeef)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n10`, float32(3.0)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n11`, float64(3.14159)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n12`, `this is a string`); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n13`, []byte(`these are bytes`)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n14`, net.ParseIP("192.168.1.1")); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n15`, net.ParseIP("fe80::208c:9aff:fe6b:3904")); err != nil {
		return
	}
	if mac, err := net.ParseMAC("22:8c:9a:6b:39:04"); err != nil {
		return err
	} else if err = ent.AddEnumeratedValueEx(`n16`, mac); err != nil {
		return err
	}
	if err = ent.AddEnumeratedValueEx(`n17`, time.Date(2022, 12, 25, 12, 13, 14, 12345, time.UTC)); err != nil {
		return
	}
	if err = ent.AddEnumeratedValueEx(`n18`, FromStandard(time.Date(2022, 12, 25, 2, 34, 24, 98765, time.UTC))); err != nil {
		return
	}
	return
}
