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
