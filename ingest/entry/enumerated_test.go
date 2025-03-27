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
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEnumeratedBasics(t *testing.T) {
	//check names
	if _, err := NewEnumeratedValue(`testing`, float64(22)); err != nil {
		t.Fatalf("Failed to handle good name: %v", err)
	} else if _, err = NewEnumeratedValue(strings.Repeat("A", MaxEvNameLength+10), 22); err != ErrInvalidName {
		t.Fatal("Failed to catch bad names", err)
	} else if _, err = NewEnumeratedValue(``, "stuff"); err != ErrInvalidName {
		t.Fatal("Failed to catch bad names", err)
	}

	//check data
	if _, err := NewEnumeratedValue(`testing`, "stuff"); err != nil {
		t.Fatalf("Failed to handle simple good string")
	} else if _, err = NewEnumeratedValue(`testing`, []byte("stuff")); err != nil {
		t.Fatalf("Failed to handle simple good byte array")
	} else if _, err = NewEnumeratedValue(`testing`, []byte("")); err != nil {
		t.Fatalf("Failed to handle simple good byte array")
	} else if _, err = NewEnumeratedValue(`testing`, ""); err != nil {
		t.Fatalf("Failed to handle simple good byte array")
	} else if _, err = NewEnumeratedValue(`testing`, nil); err != ErrUnknownType {
		t.Fatal("Failed to catch nil as unknown type", err)
	} else if _, err = NewEnumeratedValue(`testing`, EnumeratedData{}); err != ErrUnknownType {
		t.Fatal("Failed to catch struct as unknown type", err)
	}

	//check some bad data
	if _, err := NewEnumeratedValue(`testing`, strings.Repeat("A", MaxEvDataLength+1)); err == nil {
		t.Fatalf("Failed to catch oversized string")
	} else if _, err = NewEnumeratedValue(`testing`, make([]byte, MaxEvDataLength+1)); err == nil {
		t.Fatalf("Failed to catch oversized byte")
	} else if _, err = NewEnumeratedValue(`testing`, net.IP([]byte("x"))); err == nil {
		t.Fatalf("Failed to catch bad IP address")
	} else if _, err = NewEnumeratedValue(`testing`, net.HardwareAddr(make([]byte, 44))); err == nil {
		t.Fatalf("Failed to catch bad HardwareAddr address")
	}
}

func TestEnumeratedEncodeDecode(t *testing.T) {
	if err := testEVCycle(true); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(byte(0)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(int8(42)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(int16(0xbcd)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(uint16(0xdead)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(int32(0x99beef)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(uint32(0xfeedbeef)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(int64(0xdeadfeedbeef)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(uint64(0xfeeddeadfeedbeef)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(float32(3.0)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(float64(3.14159)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(`this is a string`); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle([]byte(`these are bytes`)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(net.ParseIP("192.168.1.1")); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(net.ParseIP("fe80::208c:9aff:fe6b:3904")); err != nil {
		t.Fatal(err)
	}
	if mac, err := net.ParseMAC("22:8c:9a:6b:39:04"); err != nil {
		t.Fatal(err)
	} else if err = testEVCycle(mac); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(time.Date(2022, 12, 25, 12, 13, 14, 12345, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := testEVCycle(FromStandard(time.Date(2022, 12, 25, 2, 34, 24, 98765, time.UTC))); err != nil {
		t.Fatal(err)
	}
}

func TestEnumeratedMaxSizes(t *testing.T) {
	//max data
	if ev, err := NewEnumeratedValue(`testing`, strings.Repeat("A", MaxEvDataLength)); err != nil {
		t.Fatalf("failed to create maximum EV data size: %v", err)
	} else if bts := ev.Encode(); bts == nil {
		t.Fatalf("Failed to encode with maximum ev data size")
	} else if len(bts) > MaxEvSize {
		t.Fatalf("encoded output with max ev data size is too large: %x > %x", len(bts), MaxEvSize)
	}

	//max name
	if ev, err := NewEnumeratedValue(strings.Repeat("A", MaxEvNameLength), "testing"); err != nil {
		t.Fatalf("failed to create maximum EV name size: %v", err)
	} else if bts := ev.Encode(); bts == nil {
		t.Fatalf("Failed to encode with maximum ev name size")
	} else if len(bts) > MaxEvSize {
		t.Fatalf("encoded output with max ev name size is too large: %x > %x", len(bts), MaxEvSize)
	}

	//both max name and data
	if ev, err := NewEnumeratedValue(strings.Repeat("A", MaxEvNameLength), strings.Repeat("B", MaxEvDataLength)); err != nil {
		t.Fatalf("failed to create maximum EV name and data size: %v", err)
	} else if bts := ev.Encode(); bts == nil {
		t.Fatalf("Failed to encode with maximum ev name and data size")
	} else if len(bts) > MaxEvSize {
		t.Fatalf("encoded output with max ev name and data size is too large: %x > %x", len(bts), MaxEvSize)
	}
}

func testEVCycle(a interface{}) (err error) {
	var ev EnumeratedValue
	if ev, err = NewEnumeratedValue(`testing`, a); err != nil {
		return
	} else if !ev.Valid() {
		return fmt.Errorf("EV not valid: %T %v", a, a)
	}

	ta := reflect.TypeOf(a)

	// do some hacky stuff to make sure []byte and time.Time work
	s := fmt.Sprintf("%v", a)
	if bts, ok := a.([]byte); ok {
		s = string(bts)
	} else if ts, ok := a.(time.Time); ok {
		s = FromStandard(ts).String() //convert to our internal time type
		ta = reflect.TypeOf(Timestamp{})
	}

	//test stringer
	if s != ev.Value.String() {
		return fmt.Errorf("invalid string output: %v (%v) != %v", a, s, ev.Value.String())
	}

	// test the types
	x := ev.Value.Interface()
	if x == nil {
		return fmt.Errorf("got bad response from enterface: %v", x)
	}
	if ta != reflect.TypeOf(x) {
		return fmt.Errorf("Types did not come back out properly: %v %v", ta, reflect.TypeOf(x))
	}

	//test encode/decode cycle
	var bts []byte
	if bts = ev.Encode(); bts == nil {
		return fmt.Errorf("Failed to encode %T: %v", a, err)
	} else if len(bts) > MaxEvSize {
		return fmt.Errorf("Encoded EV size is too large: %x > %x", len(bts), MaxEvSize)
	}

	bb := bytes.NewBuffer(nil)
	if _, err = ev.EncodeWriter(bb); err != nil {
		return fmt.Errorf("Failed to encode into a writer: %v", err)
	}

	//compare the two encodings
	if !bytes.Equal(bts, bb.Bytes()) {
		return fmt.Errorf("EV encoding methods did not match: %x != %x", bts, bb.Bytes())
	}

	//decode using a buffer
	var ev2 EnumeratedValue
	if n, err := ev2.Decode(bts); err != nil {
		return fmt.Errorf("failed to decode from buffer: %v", err)
	} else if n != len(bts) {
		return fmt.Errorf("Decode byte count invalid: %x != %x", n, len(bts))
	} else if err = ev.Compare(ev2); err != nil {
		return fmt.Errorf("Encode/Decode mismatch: %v", err)
	}

	//decode using a reader
	var ev3 EnumeratedValue
	l := bb.Len()
	if n, err := ev3.DecodeReader(bb); err != nil {
		return fmt.Errorf("failed to decode from reader: %v", err)
	} else if n != l {
		return fmt.Errorf("Decode byte count invalid: %x != %x", n, l)
	} else if err = ev3.Compare(ev2); err != nil {
		return fmt.Errorf("Encode/Decode mismatch: %v", err)
	} else if err = ev3.Compare(ev); err != nil {
		return fmt.Errorf("Encode/Decode mismatch: %v", err)
	}

	//decode with Alt interface
	var ev4 EnumeratedValue
	if n, err := ev4.Decode(bts); err != nil {
		return fmt.Errorf("failed to decode from buffer: %v", err)
	} else if n != len(bts) {
		return fmt.Errorf("Decode byte count invalid: %x != %x", n, len(bts))
	} else if err = ev.Compare(ev4); err != nil {
		return fmt.Errorf("Encode/Decode mismatch: %v", err)
	}

	return
}

func TestEnumeratedStringTail(t *testing.T) {
	tgt := strings.Repeat("A", MaxEvDataLength)
	orig := strings.Repeat("FOOBAR", 100) + tgt
	ed := StringEnumDataTail(orig)
	if len(tgt) != len(ed.String()) {
		t.Fatalf("bad return size: %d != %d", len(tgt), len(ed.String()))
	}
}

func TestEnumeratedJSON(t *testing.T) {
	// test with just an enumerated data
	ed, err := InferEnumeratedData("hello")
	if err != nil {
		t.Fatal(err)
	}
	var ned EnumeratedData
	bts, err := ed.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err = ned.UnmarshalJSON(bts); err != nil {
		t.Fatal(err)
	}
	if ed.String() != ned.String() {
		t.Fatalf("JSON encode/decode error %q != %q", ed.String(), ned.String())
	}

	// do with a full up enumerated value
	ev, err := NewEnumeratedValue("foobar", float64(3.14159))
	if err != nil {
		t.Fatal(err)
	}
	if bts, err = json.Marshal(ev); err != nil {
		t.Fatal(err)
	}
	var nev EnumeratedValue
	if err = json.Unmarshal(bts, &nev); err != nil {
		t.Fatal(err)
	}

	if ev.String() != nev.String() {
		t.Fatalf("JSON encode/decode error %q != %q", ev.String(), nev.String())
	}
}

func TestEntryJSON(t *testing.T) {
	ent := Entry{
		TS:   Now(),
		Tag:  1,
		Data: []byte("hello"),
		SRC:  net.ParseIP("dead:beef::feed:febe"),
	}
	x := []interface{}{
		float32(3.14),
		float64(1.6180),
		"hello",
		net.ParseIP("192.168.1.1"),
		1234567890,
	}
	for i, v := range x {
		if ev, err := NewEnumeratedValue(fmt.Sprintf("name%d", i), v); err != nil {
			t.Fatal(err)
		} else if err = ent.AddEnumeratedValue(ev); err != nil {
			t.Fatal(err)
		}
	}
	bts, err := json.Marshal(ent)
	if err != nil {
		t.Fatal(err)
	}
	var nent Entry
	if err = json.Unmarshal(bts, &nent); err != nil {
		t.Fatal(err)
	}

	if !ent.TS.Equal(nent.TS) {
		t.Fatalf("json encode/decode TS failure: %v != %v", ent.TS, nent.TS)
	} else if ent.Tag != nent.Tag {
		t.Fatalf("json encode/decode Tag failure: %v != %v", ent.Tag, nent.Tag)
	} else if !ent.SRC.Equal(nent.SRC) {
		t.Fatalf("json encode/decode SRC failure: %v != %v", ent.SRC, nent.SRC)
	} else if !bytes.Equal(ent.Data, nent.Data) {
		t.Fatalf("json encode/decode Data failure %d %d", len(ent.Data), len(nent.Data))
	} else if ent.EVB.Count() != len(x) {
		t.Fatalf("Original EVBlock count is bad: %d != %d", ent.EVB.Count(), len(x))
	} else if nent.EVB.Count() != len(x) {
		t.Fatalf("post decode EVBlock count is bad: %d != %d", nent.EVB.Count(), len(x))
	}

	for i := range x {
		name := fmt.Sprintf("name%d", i)
		if ev, ok := nent.EVB.Get(name); !ok {
			t.Fatalf("Failed to get %s", name)
		} else if ev.Value.String() != fmt.Sprintf("%v", x[i]) {
			t.Fatalf("EV %d %s %s != %v", i, name, ev.Value.String(), x[i])
		}
	}

	// do it again with empty EVs
	ent.EVB = EVBlock{}
	if bts, err = json.Marshal(ent); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(bts, &nent); err != nil {
		t.Fatal(err)
	}
	if ent.EVB.Count() != 0 || nent.EVB.Count() != 0 {
		t.Fatalf("Invalid empty EV block counts: %d %d", ent.EVB.Count(), ent.EVB.Count())
	}
}

func FuzzEVEncodeDecode(f *testing.F) {
	if err := addFuzzSample(f, true); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, byte(0)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, int8(42)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, int16(0xbcd)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, uint16(0xdead)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, int32(0x99beef)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, uint32(0xfeedbeef)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, int64(0xdeadfeedbeef)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, uint64(0xfeeddeadfeedbeef)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, float32(3.0)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, float64(3.14159)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, `this is a string`); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, []byte(`these are bytes`)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, net.ParseIP("192.168.1.1")); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, net.ParseIP("fe80::208c:9aff:fe6b:3904")); err != nil {
		f.Fatal(err)
	}
	if mac, err := net.ParseMAC("22:8c:9a:6b:39:04"); err != nil {
		f.Fatal(err)
	} else if err = addFuzzSample(f, mac); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, time.Date(2022, 12, 25, 12, 13, 14, 12345, time.UTC)); err != nil {
		f.Fatal(err)
	}
	if err := addFuzzSample(f, FromStandard(time.Date(2022, 12, 25, 2, 34, 24, 98765, time.UTC))); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, buff []byte) {
		var ev EnumeratedValue
		if _, err := ev.Decode(buff); err != nil {
			t.Log(err)
		}
	})
}

func addFuzzSample(f *testing.F, val interface{}) error {
	if buff, err := encodeEv(val); err != nil {
		return err
	} else {
		f.Add(buff)
	}
	return nil
}

func encodeEv(a interface{}) (buff []byte, err error) {
	var ev EnumeratedValue
	name := fmt.Sprintf("test %T %v", a, a)
	if ev, err = NewEnumeratedValue(name, a); err == nil {
		if !ev.Valid() {
			err = fmt.Errorf("EV not valid: %T %v", a, a)
		} else {
			buff = ev.Encode()
		}
	}
	return
}
