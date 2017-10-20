/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"testing"
)

const (
	testSize            int      = 1024
	RANDOM_BUFF_SIZE_MB int      = 8
	MB                  int      = 1024 * 1024
	DEFAULT_SEARCH_TAG  EntryTag = 0
)

var (
	key      int64
	ts       Timestamp
	randBuff []byte
	source   net.IP
)

func init() {
	ts = Now()
	key = ts.Sec
	source = net.ParseIP("DEAD:BEEF:a:b:c:d:CAFE:DEED")
	randBuff = makeRandBuff(MB * RANDOM_BUFF_SIZE_MB)
}

func makeRandBuff(sz int) []byte {
	b := make([]byte, sz)

	for i := 0; i < (RANDOM_BUFF_SIZE_MB * MB); i++ {
		b[i] = byte(rand.Intn(0xff))
	}
	return b
}

func init() {

}

func TestCreateBlock(t *testing.T) {
	var sz uint64
	aeb := NewActiveEntryBlock(key)
	if aeb == nil {
		t.Fatal("active entry block is nil")
	}
	for i := 0; i < testSize; i++ {
		e, err := genRandomEntry()
		if err != nil {
			t.Fatal(err)
		}
		e.TS.Sec = key
		if err := aeb.Add(&e); err != nil {
			t.Fatal(err)
		}
		sz += e.Size()
	}

	buff, err := aeb.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(buff)) != (sz + EntryBlockHeaderSize) {
		t.Fatal("Bad resulting buff size", len(buff), sz)
	}

	var aeb2 ActiveEntryBlock
	if err := aeb2.Decode(buff); err != nil {
		t.Fatal(err)
	}
	if aeb.size != aeb2.size || aeb.size == 0 {
		t.Fatal(fmt.Sprintf("encode/decode sizes don't match %d != %d", aeb.size, aeb2.size))
	}
	if len(aeb.entries) != len(aeb2.entries) || len(aeb.entries) == 0 {
		t.Fatal(fmt.Sprintf("encode/decode counts don't match %d != %d",
			len(aeb.entries), len(aeb2.entries)))
	}
	for i := range aeb.entries {
		if err := compareEntry(aeb.entries[i], aeb2.entries[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCreateEBlock(t *testing.T) {
	var eb EntryBlock
	var sz uint64

	for i := 0; i < testSize; i++ {
		e, err := genRandomEntry()
		if err != nil {
			t.Fatal(err)
		}
		e.TS.Sec = key
		eb.Add(&e)
		sz += e.Size()
	}

	buff, err := eb.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(buff)) != (sz + EntryBlockHeaderSize) {
		t.Fatal("Bad resulting buff size")
	}

	var eb2 EntryBlock
	if err := eb2.Decode(buff); err != nil {
		t.Fatal(err)
	}
	if eb.size != eb2.size || eb.size == 0 {
		t.Fatal(fmt.Sprintf("encode/decode sizes don't match %d != %d", eb.size, eb2.size))
	}
	if len(eb.entries) != len(eb2.entries) || len(eb.entries) == 0 {
		t.Fatal(fmt.Sprintf("encode/decode counts don't match %d != %d",
			len(eb.entries), len(eb2.entries)))
	}
	for i := range eb.entries {
		if err := compareEntry(eb.entries[i], eb2.entries[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEBlockAppend(t *testing.T) {
	var eb EntryBlock
	var sz uint64

	for i := 0; i < testSize; i++ {
		e, err := genRandomEntry()
		if err != nil {
			t.Fatal(err)
		}
		e.TS.Sec = key
		eb.Add(&e)
		sz += e.Size()
	}

	buff, err := eb.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(buff)) != (sz + EntryBlockHeaderSize) {
		t.Fatal("Bad resulting buff size")
	}

	var eb2 EntryBlock
	for i := 0; i < testSize; i++ {
		e, err := genRandomEntry()
		if err != nil {
			t.Fatal(err)
		}
		e.TS.Sec = key
		eb2.Add(&e)
		sz += e.Size()
	}

	buff, err = eb2.EncodeAppend(buff)
	if err != nil {
		t.Fatal(err)
	}

	if uint64(len(buff)) != (sz + EntryBlockHeaderSize) {
		t.Fatal("Bad resulting buffer after append")
	}

	var ebf EntryBlock
	if err := ebf.Decode(buff); err != nil {
		t.Fatal(err)
	}
	if (eb.size+eb2.size) != ebf.size || ebf.size == 0 {
		t.Fatal(fmt.Sprintf("encode/decode sizes don't match %d != %d", (eb.size + eb2.size), ebf.size))
	}
	if (len(eb.entries)+len(eb2.entries)) != len(ebf.entries) || len(ebf.entries) == 0 {
		t.Fatal(fmt.Sprintf("encode/decode counts don't match %d != %d",
			len(eb.entries)+len(eb2.entries), len(ebf.entries)))
	}
	eb.Merge(&eb2)
	if eb.size != ebf.size {
		t.Fatal("Final size mismatch", eb.size, ebf.size)
	}

	for i := range eb.entries {
		if err := compareEntry(eb.entries[i], ebf.entries[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func genRandomEntry() (Entry, error) {
	size := rand.Intn(1024) + 1024
	offset := rand.Intn(RANDOM_BUFF_SIZE_MB*MB - size)

	if len(source) != 16 {
		return Entry{}, errors.New("Source is not valid")
	}
	return Entry{ts, source, DEFAULT_SEARCH_TAG, randBuff[offset : offset+size]}, nil
}

func compareEntry(a, b *Entry) error {
	if a == nil || b == nil {
		return errors.New("nil entry")
	}
	if a.TS != b.TS {
		return errors.New(fmt.Sprintf("TS mismatch %v != %v", a.TS, b.TS))
	}
	if a.Tag != b.Tag {
		return errors.New(fmt.Sprintf("Tag mismatch %d != %d", a.Tag, b.Tag))
	}
	if !a.SRC.Equal(b.SRC) {
		return errors.New(fmt.Sprintf("Src mismatch %s != %s", a.SRC, b.SRC))
	}
	if len(a.Data) != len(b.Data) {
		return errors.New(fmt.Sprintf("Data len mismatch %d != %d", len(a.Data), len(b.Data)))
	}
	for i := range a.Data {
		if a.Data[i] != b.Data[i] {
			return errors.New(fmt.Sprintf("Data mismatch [%d] %x != %x", i, a.Data[i], b.Data[i]))
		}
	}
	return nil
}
