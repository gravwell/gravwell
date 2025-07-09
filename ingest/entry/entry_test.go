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
	"encoding/gob"
	"fmt"
	"math/rand"
	"net"
	"testing"
	"time"
)

const (
	fuzzCorpusSize = 16
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
	if _, err := e.EncodeHeader(b); err != nil {
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
	} else if nn, err := EntrySize(buff); err != nil {
		t.Fatal(err)
	} else if nn != n {
		t.Fatalf("EntrySize disagrees: %d != %d", n, nn)
	}

	//encode using a writer and compare buffers
	bb := bytes.NewBuffer(nil)
	if n, err := e.EncodeWriter(bb); err != nil {
		t.Fatal(err)
	} else if bb.Len() != n {
		t.Fatalf("encoded writer size is incorrect: %d != %d", n, len(buff))
	} else if nn, err := EntrySize(bb.Bytes()); err != nil {
		t.Fatal(err)
	} else if nn != n {
		t.Fatalf("EntrySize disagrees: %d != %d", n, nn)
	}

	//check that the encoded output was the same
	if !bytes.Equal(bb.Bytes(), buff) {
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
	} else if nn, err := EntrySize(buff); err != nil {
		t.Fatal(err)
	} else if nn != n {
		t.Fatalf("EntrySize disagrees: %d != %d", n, nn)
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
	} else if nn, err := EntrySize(bb.Bytes()); err != nil {
		t.Fatal(err)
	} else if nn != n {
		t.Fatalf("EntrySize disagrees: %d != %d", n, nn)
	}

	//check that the encoded output was the same
	if !bytes.Equal(bb.Bytes(), buff) {
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

func TestGOBEncodeDecodeEnumeratedValues(t *testing.T) {
	ts := Now()
	e := &Entry{
		TS:   ts,
		Tag:  EntryTag(0x1234),
		SRC:  net.ParseIP("DEAD::BEEF"),
		Data: make([]byte, 0x99),
	}
	addAllEvs(e)

	bb := bytes.NewBuffer(nil)
	genc := gob.NewEncoder(bb)
	//encode single entry using pointer
	if err := genc.Encode(e); err != nil {
		t.Fatalf("failed to gob encode single entry as pointer: %v", err)
	} else if err = genc.Encode(*e); err != nil {
		t.Fatalf("failed to gob encode single entry: %v", err)
	}

	var out1, out2 Entry
	gdec := gob.NewDecoder(bb)
	if err := gdec.Decode(&out1); err != nil {
		t.Fatalf("failed to gob decode entry that went in as a pointer: %v", err)
	} else if err = gdec.Decode(&out2); err != nil {
		t.Fatalf("failed to gob decode entry: %v", err)
	}

	//check that we got things back out cleanly
	if err := e.Compare(&out1); err != nil {
		t.Fatalf("decoded gob entry did not come out the same: %v", err)
	} else if e.Compare(&out2); err != nil {
		t.Fatalf("decoded gob entry did not come out the same: %v", err)
	} else if out1.Compare(&out2); err != nil {
		t.Fatalf("decoded entries did not come out the same: %v", err)
	}
}

func TestGOBEncodeDecodeEnumeratedValueSlices(t *testing.T) {
	ts := Now()
	ents := make([]Entry, 128)
	for i := range ents {
		e := Entry{
			TS:   ts,
			Tag:  EntryTag(0x1234),
			SRC:  net.ParseIP("DEAD::BEEF"),
			Data: make([]byte, 0x99),
		}
		addAllEvs(&e)
		ents[i] = e
	}

	bb := bytes.NewBuffer(nil)
	genc := gob.NewEncoder(bb)
	//encode single entry using pointer
	if err := genc.Encode(ents); err != nil {
		t.Fatalf("failed to gob encode slice of entries: %v", err)
	}

	var out []Entry
	gdec := gob.NewDecoder(bb)
	if err := gdec.Decode(&out); err != nil {
		t.Fatalf("failed to gob decode slice of entries: %v", err)
	}

	if len(ents) != len(out) {
		t.Fatalf("decoded len is wrong: %d != %d", len(ents), len(out))
	}
	for i := range ents {
		//check that we got things back out cleanly
		if err := ents[i].Compare(&out[i]); err != nil {
			t.Fatalf("decoded gob entry #%d did not come out the same: %v", i, err)
		}
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

func FuzzDecodeHeaderNoEvs(f *testing.F) {
	ips := []net.IP{
		net.ParseIP("DEAD::BEEF"),
		net.ParseIP("192.168.1.1"),
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < fuzzCorpusSize; i++ {
		e := &Entry{
			TS:   Now(),
			Tag:  EntryTag((i*10 + i) % 0x10000),
			SRC:  ips[i%2],
			Data: make([]byte, i*1000+i),
		}
		r.Read(e.Data)
		//encode the header
		b := make([]byte, ENTRY_HEADER_SIZE)
		if _, err := e.EncodeHeader(b); err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, orig []byte) {
		var e2 Entry
		n, hasEvs, err := e2.DecodeHeader(orig)
		if err != nil {
			t.Log(err)
		} else if hasEvs {
			t.Log("has EVs, it should not")
		} else if n != len(orig) {
			t.Logf("length is bad: %d != %d", n, len(orig))
		}

	})
}

func FuzzDecodeHeaderWithEvs(f *testing.F) {
	ips := []net.IP{
		net.ParseIP("FEED:FEBE:DEAD:BEEF:"),
		net.ParseIP("255.255.255.254"),
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < fuzzCorpusSize; i++ {
		e := &Entry{
			TS:   Now(),
			Tag:  EntryTag((i*11 + i) % 0x10000),
			SRC:  ips[i%2],
			Data: make([]byte, i*1234+i),
		}
		e.AddEnumeratedValueEx(`n1`, true)
		r.Read(e.Data)
		//encode the header
		b := make([]byte, ENTRY_HEADER_SIZE)
		if _, err := e.EncodeHeader(b); err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, orig []byte) {
		var e2 Entry
		n, hasEvs, err := e2.DecodeHeader(orig)
		if err != nil {
			t.Log(err)
		} else if !hasEvs {
			t.Log("has EVs, it should not")
		} else if n != len(orig) {
			t.Logf("length is bad: %d != %d", n, len(orig))
		}
	})
}

func FuzzDecodeEntryNoEvs(f *testing.F) {
	ips := []net.IP{
		net.ParseIP("DEAD::BEEF"),
		net.ParseIP("192.168.1.1"),
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < fuzzCorpusSize; i++ {
		e := &Entry{
			TS:   Now(),
			Tag:  EntryTag((i*10 + i) % 0x10000),
			SRC:  ips[i%2],
			Data: make([]byte, i*1000+i),
		}
		r.Read(e.Data)
		buff, err := encodeEntry(e)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(buff)
	}
	f.Fuzz(func(t *testing.T, orig []byte) {
		var e2 Entry
		if _, err := e2.Decode(orig); err != nil {
			t.Log(err)
		}
	})
}

func FuzzDecodeEntryWithEvs(f *testing.F) {
	ips := []net.IP{
		net.ParseIP("FEED:FEBE:DEAD:BEEF:"),
		net.ParseIP("255.255.255.254"),
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < fuzzCorpusSize; i++ {
		e := &Entry{
			TS:   Now(),
			Tag:  EntryTag((i*11 + i) % 0x10000),
			SRC:  ips[i%2],
			Data: make([]byte, i*1234+i),
		}
		r.Read(e.Data)

		if err := addAllEvs(e); err != nil {
			f.Fatal(err)
		}
		buff, err := encodeEntry(e)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(buff)
	}
	f.Fuzz(func(t *testing.T, orig []byte) {
		var e2 Entry
		if _, err := e2.Decode(orig); err != nil {
			t.Log(err)
		}
	})
}

func encodeEntry(e *Entry) ([]byte, error) {
	bb := bytes.NewBuffer(nil)
	if _, err := e.EncodeWriter(bb); err != nil {
		return nil, err
	}
	return bb.Bytes(), nil
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
