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
	"testing"
)

func TestEnumeratedValueBlockEncodeDecode(t *testing.T) {
	var evb EVBlock

	for i := 0; i < 32; i++ {
		if ev, err := NewEnumeratedValue(fmt.Sprintf("ev%d", i), int64(i)); err != nil {
			t.Error(err)
		} else {
			evb.Add(ev)
		}
	}

	if !evb.Populated() {
		t.Fatal("evb reported as not populated")
	} else if err := evb.Valid(); err != nil {
		t.Fatal("evb reported not valid", err)
	} else if len(evb.evs) != 32 {
		t.Fatalf("evb has wrong count: %d != 32", len(evb.evs))
	}

	//encode and decode
	buff, err := evb.Encode()
	if err != nil {
		t.Fatal(err)
	}

	bb := bytes.NewBuffer(nil)
	if _, err = evb.EncodeWriter(bb); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buff, bb.Bytes()) {
		t.Fatalf("encoding methods do not match")
	}

	//decode using each method
	var teb EVBlock
	if _, err = teb.Decode(buff); err != nil {
		t.Fatal(err)
	} else if err = teb.Compare(evb); err != nil {
		t.Fatal(err)
	} else if _, err = teb.DecodeReader(bb); err != nil {
		t.Fatal(err)
	} else if err = teb.Compare(evb); err != nil {
		t.Fatal(err)
	} else if len(teb.evs) != 32 {
		t.Fatalf("invalid ev count: %d != 32", len(teb.evs))
	}
}

func TestEnumeratedValueBlockDuplicateAdd(t *testing.T) {
	var evs []EnumeratedValue
	var evb EVBlock
	for i := 0; i < 32; i++ {
		if ev, err := NewEnumeratedValue(fmt.Sprintf("ev%d", i), int64(i)); err != nil {
			t.Error(err)
		} else {
			evs = append(evs, ev)
		}
	}

	evb.AddSet(evs)
	if !evb.Populated() {
		t.Fatal("evb reported as not populated")
	} else if err := evb.Valid(); err != nil {
		t.Fatal("evb reported not valid", err)
	} else if evb.Count() != 32 {
		t.Fatalf("evb has wrong count: %d != 32", evb.Count())
	}

	//add a whole new set
	evs = []EnumeratedValue{}
	for i := 0; i < 32; i++ {
		if ev, err := NewEnumeratedValue(fmt.Sprintf("ev%d", i), fmt.Sprintf("stuff?%d", i)); err != nil {
			t.Error(err)
		} else {
			evs = append(evs, ev)
		}
	}
	evb.AddSet(evs)

	//now add a single
	if ev, err := NewEnumeratedValue(`ev0`, float64(3.14)); err != nil {
		t.Error(err)
	} else {
		evb.Add(ev)
		evs[0] = ev
	}

	//check the size
	if !evb.Populated() {
		t.Fatal("evb reported as not populated")
	} else if err := evb.Valid(); err != nil {
		t.Fatal("evb reported not valid", err)
	} else if evb.Count() != 32 {
		t.Fatalf("evb has wrong count: %d != 32", evb.Count())
	}

	//grab the set and check a few
	evs2 := evb.Values()
	if len(evs2) != evb.Count() {
		t.Fatalf("Value count is wrong: %d != %d", len(evs2), evb.Count())
	}
	for i := range evs {
		s1 := evs[i].String()
		s2 := evs2[i].String()
		if s1 != s2 {
			t.Fatalf("enumerated value %d does not match: %s != %s", i, s1, s2)
		}
	}
}

func TestEnumeratedValueBlockEmptyAdd(t *testing.T) {
	var evs []EnumeratedValue
	var evb EVBlock
	evb.AddSet(evs)
	if evb.Populated() {
		t.Fatal("evb reported as populated")
	} else if err := evb.Valid(); err != nil {
		t.Fatal("evb reported not valid", err)
	} else if evb.Count() != 0 {
		t.Fatalf("evb has wrong count: %d != 0", evb.Count())
	}
	if buff, err := evb.Encode(); err != nil {
		t.Fatal(err)
	} else if len(buff) != 0 {
		t.Fatal("encoded an empty evb to non-empty buff")
	}
	buff := make([]byte, 1024)
	if n, err := evb.EncodeBuffer(buff); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Fatalf("EncodeBuffer returned something on empty evb: %d != 0", n)
	}
}
