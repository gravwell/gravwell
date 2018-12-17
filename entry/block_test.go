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

func TestCreateBlock(t *testing.T) {
	var sz uint64
	eb := NewEntryBlock(nil, 0)
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
		t.Fatal("Bad resulting buff size", len(buff), sz)
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

func TestPeelBlock(t *testing.T) {
	var sz uint64
	eb := NewEntryBlock(nil, 0)
	for i := 0; i < 16*1024; i++ {
		e, err := genRandomEntry()
		if err != nil {
			t.Fatal(err)
		}
		e.TS.Sec = key
		eb.Add(&e)
		sz += e.Size()
	}
	if sz != eb.Size() {
		t.Fatal("Invalid entry block size")
	}

	for eb.Count() > 0 {
		sz := eb.Size()
		cnt := eb.Count()

		neb := eb.Peel(rand.Intn(1024))
		if (neb.Size() + eb.Size()) != sz {
			t.Fatal("peel size mismatch")
		}
		if (neb.Count() + eb.Count()) != cnt {
			t.Fatal("peel count mismatch")
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

func TestCreateBlockSizeInfer(t *testing.T) {
	var sz uint64
	var ents []Entry
	var entps []*Entry
	for i := 0; i < testSize; i++ {
		e, err := genRandomEntry()
		if err != nil {
			t.Fatal(err)
		}
		e.TS.Sec = key
		ents = append(ents, e)
		entps = append(entps, &e)
		sz += e.Size()
	}
	eb := NewEntryBlockNP(ents, 0)
	if eb.Size() != sz {
		t.Fatal("Did not infer size correctly")
	}
	eb2 := NewEntryBlock(entps, 0)
	if eb2.Size() != sz {
		t.Fatal("Did not infer size correctly with pointer based block")
	}
}

func TestDeepCopyBlock(t *testing.T) {
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

	eb2 := eb.DeepCopy()
	if eb.Len() != eb2.Len() {
		t.Fatal(fmt.Sprintf("len mismatch: %d != %d", eb.Len(), eb2.Len()))
	}
	if eb.Size() != eb2.Size() {
		t.Fatal(fmt.Sprintf("Size mismatch: %d != %d", eb.Size(), eb2.Size()))
	}
	if eb.key != eb2.key {
		t.Fatal(fmt.Sprintf("Key mismatch: %d != %d", eb.key, eb2.key))
	}

	for i := range eb.entries {
		if err := compareEntry(eb.entries[i], eb2.entries[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewDeepBlock(t *testing.T) {
	var eb EntryBlock
	var set []*Entry
	var sz uint64

	for i := 0; i < testSize; i++ {
		e, err := genRandomEntry()
		if err != nil {
			t.Fatal(err)
		}
		e.TS.Sec = key
		eb.Add(&e)
		sz += e.Size()
		set = append(set, &e)
	}

	eb2 := NewDeepCopyEntryBlock(set, sz)

	if eb.Len() != eb2.Len() {
		t.Fatal(fmt.Sprintf("len mismatch: %d != %d", eb.Len(), eb2.Len()))
	}
	if eb.Size() != eb2.Size() {
		t.Fatal(fmt.Sprintf("Size mismatch: %d != %d", eb.Size(), eb2.Size()))
	}
	if eb.key != eb2.key {
		t.Fatal(fmt.Sprintf("Key mismatch: %d != %d", eb.key, eb2.key))
	}

	for i := range eb.entries {
		if err := compareEntry(eb.entries[i], eb2.entries[i]); err != nil {
			t.Fatal(err)
		}
	}

	if eb2.Size() != sz {
		t.Fatal(fmt.Sprintf("invalid size: %d != %d", eb2.Size(), sz))
	}
}

func TestNewDeepBlockNoSize(t *testing.T) {
	var eb EntryBlock
	var set []*Entry
	var sz uint64

	for i := 0; i < testSize; i++ {
		e, err := genRandomEntry()
		if err != nil {
			t.Fatal(err)
		}
		e.TS.Sec = key
		eb.Add(&e)
		sz += e.Size()
		set = append(set, &e)
	}

	eb2 := NewDeepCopyEntryBlock(set, 0)

	if eb.Len() != eb2.Len() {
		t.Fatal(fmt.Sprintf("len mismatch: %d != %d", eb.Len(), eb2.Len()))
	}
	if eb.Size() != eb2.Size() {
		t.Fatal(fmt.Sprintf("Size mismatch: %d != %d", eb.Size(), eb2.Size()))
	}
	if eb.key != eb2.key {
		t.Fatal(fmt.Sprintf("Key mismatch: %d != %d", eb.key, eb2.key))
	}

	for i := range eb.entries {
		if err := compareEntry(eb.entries[i], eb2.entries[i]); err != nil {
			t.Fatal(err)
		}
	}

	if eb2.Size() != sz {
		t.Fatal(fmt.Sprintf("invalid size: %d != %d", eb2.Size(), sz))
	}
}

func TestDeepBroken(t *testing.T) {
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

	//break the key and size
	eb.key = 1234890987
	eb.size = 0xdeadbeef

	eb2 := eb.DeepCopy()
	if eb.Len() != eb2.Len() {
		t.Fatal(fmt.Sprintf("len mismatch: %d != %d", eb.Len(), eb2.Len()))
	}
	if eb.Size() == eb2.Size() {
		t.Fatal(fmt.Sprintf("Size not corrected: %d == %d", eb.Size(), eb2.Size()))
	}
	if eb.key == eb2.key {
		t.Fatal(fmt.Sprintf("Key not corrected: %d == %d", eb.key, eb2.key))
	}

	for i := range eb.entries {
		if err := compareEntry(eb.entries[i], eb2.entries[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeepEmpty(t *testing.T) {
	var eb EntryBlock
	//break the key and size
	eb.key = 1234890987
	eb.size = 0xdeadbeef

	eb2 := eb.DeepCopy()
	if eb.Len() != eb2.Len() {
		t.Fatal(fmt.Sprintf("len mismatch: %d != %d", eb.Len(), eb2.Len()))
	}
	if eb.Size() == eb2.Size() {
		t.Fatal(fmt.Sprintf("Size not corrected: %d == %d", eb.Size(), eb2.Size()))
	}
	if eb2.Key() != 0 {
		t.Fatal(fmt.Sprintf("Key not corrected: %d != 0", eb2.key))
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
