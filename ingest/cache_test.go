/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"crypto/md5"
	"os"
	"testing"
	"time"

	"github.com/gravwell/ingest/v3/entry"
)

const (
	tstLoc       string = `/tmp/cache_test.db`
	memCacheSize uint64 = 1024 * 1024 //1MB
)

var (
	defConfig = IngestCacheConfig{
		FileBackingLocation: tstLoc,
		MemoryCacheSize:     memCacheSize,
	}
	memConfig = IngestCacheConfig{
		MemoryCacheSize: memCacheSize,
	}
)

func TestNewCache(t *testing.T) {
	ic, err := NewIngestCache(defConfig)
	if err != nil {
		t.Fatal(err)
	}

	if err := ic.Close(); err != nil {
		t.Fatal(err)
	}
	clean(t)
}

func TestStartStop(t *testing.T) {
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ic, err := NewIngestCache(defConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Close(); err != nil {
		t.Fatal(err)
	}
	clean(t)
}

func TestFeeder(t *testing.T) {
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ts := entry.Now()
	key := ts.Sec

	ic, err := NewIngestCache(defConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	//feed until we would blow our in memory size with a single key
	var sz uint64
	for {
		ent := makeEntryWithKey(key)
		if (sz + ent.Size()) >= memCacheSize {
			//if we would blow the in memory cache, bail
			break
		}
		sz += ent.Size()
		//send the entry down
		echan <- ent
	}

	//wait up to 1 second for the MemoryCacheSize to hit what we expect
	//we do this because the cache is async
	for i := 0; i < 100; i++ {
		if ic.MemoryCacheSize() == sz {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ic.MemoryCacheSize() != sz {
		t.Fatal("Timed out waiting for cache size", sz, ic.MemoryCacheSize())
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Close(); err != nil {
		t.Fatal(err)
	}
	clean(t)
}

func TestFeederBlock(t *testing.T) {
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ts := entry.Now()
	key := ts.Sec
	ic, err := NewIngestCache(defConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	//feed until we would blow our in memory size with a single key
	var sz uint64
mainLoop:
	for {
		blk := []*entry.Entry{}
		for i := 0; i < 256; i++ {
			ent := makeEntryWithKey(key)
			if (sz + ent.Size()) >= memCacheSize {
				//if we would blow the in memory cache, bail
				if len(blk) > 0 {
					bchan <- blk
				}
				break mainLoop
			}
			sz += ent.Size()
			blk = append(blk, ent)
		}
		if len(blk) > 0 {
			bchan <- blk
		}
	}

	//wait up to 1 second for the MemoryCacheSize to hit what we expect
	//we do this because the cache is async
	for i := 0; i < 100; i++ {
		if ic.MemoryCacheSize() == sz {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ic.MemoryCacheSize() != sz {
		t.Fatal("Timed out waiting for cache size", sz, ic.MemoryCacheSize())
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Close(); err != nil {
		t.Fatal(err)
	}
	clean(t)
}

func TestFeederMem(t *testing.T) {
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ts := entry.Now()
	key := ts.Sec
	ic, err := NewIngestCache(memConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	//feed until we would blow our in memory size with a single key
	var sz uint64
	for {
		ent := makeEntryWithKey(key)
		if (sz + ent.Size()) >= memCacheSize {
			//if we would blow the in memory cache, bail
			break
		}
		sz += ent.Size()
		//send the entry down
		echan <- ent
	}

	//wait up to 1 second for the MemoryCacheSize to hit what we expect
	//we do this because the cache is async
	for i := 0; i < 100; i++ {
		if ic.MemoryCacheSize() == sz {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ic.MemoryCacheSize() != sz {
		t.Fatal("Timed out waiting for cache size", sz, ic.MemoryCacheSize())
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Close(); err == nil {
		t.Fatal("Did not get an error when closing with active data")
	}
	clean(t)
}

//feed keys in the worst possible manner, cycling
func TestMultiFeeder(t *testing.T) {
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ts := entry.Now()
	key := ts.Sec
	ic, err := NewIngestCache(defConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	keys := make([]int64, 64)
	for i := range keys {
		keys[i] = key
		key--
	}

	//feed until we would blow our in memory size with a single key
	var sz uint64
	var i int
	for {
		ent := makeEntryWithKey(keys[i%len(keys)])
		if (sz + ent.Size()) >= memCacheSize {
			//if we would blow the in memory cache, bail
			break
		}
		sz += ent.Size()
		//send the entry down
		echan <- ent
		i++
	}

	//wait up to 1 second for the MemoryCacheSize to hit what we expect
	//we do this because the cache is async
	for i := 0; i < 100; i++ {
		if ic.MemoryCacheSize() == sz {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ic.MemoryCacheSize() != sz {
		t.Fatal("Timed out waiting for cache size", sz, ic.MemoryCacheSize())
	}

	if ic.HotBlocks() != len(keys) {
		t.Fatal("Failed to generate appropriate number of blocks")
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Close(); err != nil {
		t.Fatal(err)
	}
	clean(t)
}

//feed keys in the worst possible manner, cycling
func TestMultiFeederMem(t *testing.T) {
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ts := entry.Now()
	key := ts.Sec
	ic, err := NewIngestCache(memConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	keys := make([]int64, 64)
	for i := range keys {
		keys[i] = key
		key--
	}

	//feed until we would blow our in memory size with a single key
	var sz uint64
	var i int
	for {
		ent := makeEntryWithKey(keys[i%len(keys)])
		if (sz + ent.Size()) >= memCacheSize {
			//if we would blow the in memory cache, bail
			break
		}
		sz += ent.Size()
		//send the entry down
		echan <- ent
		i++
	}

	//wait up to 1 second for the MemoryCacheSize to hit what we expect
	//we do this because the cache is async
	for i := 0; i < 100; i++ {
		if ic.MemoryCacheSize() == sz {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ic.MemoryCacheSize() != sz {
		t.Fatal("Timed out waiting for cache size", sz, ic.MemoryCacheSize())
	}

	if ic.HotBlocks() != len(keys) {
		t.Fatal("Failed to generate appropriate number of blocks")
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Close(); err == nil {
		t.Fatal("Failed to catch error on close with hot blocks")
	}
	clean(t)
}

//feed keys in the worst possible manner, cycling, but also forcing some items out to disk
func TestMultiFeederOverflow(t *testing.T) {
	mp := make(map[[16]byte]*entry.Entry, 128)
	makeAndPushMulti(t, (memCacheSize * 2), mp)
	clean(t)
}

func makeAndPushMulti(t *testing.T, pushSize uint64, mp map[[16]byte]*entry.Entry) {
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ts := entry.Now()
	key := ts.Sec
	ic, err := NewIngestCache(defConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	keys := make([]int64, 64)
	for i := range keys {
		keys[i] = key
		key--
	}
	keyc := uint64(len(keys))
	keym := map[uint64]bool{}

	//feed until we would blow our in memory size with a single key
	var sz uint64
	var cnt uint64
	for {
		i := cnt % keyc
		keym[i] = true
		ent := makeEntryWithKey(keys[i])
		if (sz + ent.Size()) >= pushSize {
			//bail on 2X the memcacheSize
			break
		}
		sz += ent.Size()
		//send the entry down
		echan <- ent
		cnt++
		mp[hashEntry(ent)] = ent
	}

	//wait up to 1 second for the MemoryCacheSize to hit what we expect
	//we do this because the cache is async
	for i := 0; i < 100; i++ {
		if ic.Count() == cnt {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ic.Count() != cnt {
		t.Fatal("Timed out waiting for entries to hit", ic.Count(), cnt)
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Sync(); err != nil {
		t.Fatal(err)
	}

	if ic.HotBlocks() != 0 {
		t.Fatal("Active blocks after sync")
	}

	if ic.MemoryCacheSize() != 0 {
		t.Fatal("blocks in memory after sync")
	}

	if ic.StoredBlocks() != len(keym) {
		t.Fatal("Failed to generate appropriate number of blocks", ic.StoredBlocks(), len(keym))
	}

	if err := ic.Close(); err != nil {
		t.Fatal(err)
	}
}

//feed keys in the worst possible manner, cycling, but also forcing some items out to disk
func TestMultiFeederOverflowMem(t *testing.T) {
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ts := entry.Now()
	key := ts.Sec
	ic, err := NewIngestCache(memConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	keys := make([]int64, 64)
	for i := range keys {
		keys[i] = key
		key--
	}
	keyc := uint64(len(keys))
	keym := map[uint64]bool{}

	//feed until we would blow our in memory size with a single key
	var sz uint64
	var cnt uint64
	for {
		i := cnt % keyc
		keym[i] = true
		ent := makeEntryWithKey(keys[i])
		if (sz + ent.Size()) >= (memCacheSize * 2) {
			//bail on 2X the memcacheSize
			break
		}
		sz += ent.Size()
		//send the entry down
		echan <- ent
		cnt++
	}

	//wait up to 1 second for the MemoryCacheSize to hit what we expect
	//we do this because the cache is async
	for i := 0; i < 100; i++ {
		if ic.Count() == cnt {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ic.Count() != cnt {
		t.Fatal("Timed out waiting for entries to hit", ic.Count(), cnt)
	}

	if ic.MemoryCacheSize() != sz {
		t.Fatal("block size is invalid")
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Sync(); err == nil {
		t.Fatal("Failed to detect error on sync with no file backing")
	}

	if err := ic.Close(); err == nil {
		t.Fatal("Failed to detect error on close with hot blocks")
	}
	clean(t)
}

func TestRetrieve(t *testing.T) {
	mp := make(map[[16]byte]*entry.Entry, 128)
	makeAndPushMulti(t, (memCacheSize * 2), mp)

	pullmp := make(map[[16]byte]*entry.Entry, 128)
	ic, err := NewIngestCache(defConfig)
	if err != nil {
		t.Fatal(err)
	}

	//pull all the entries back out and park them in a new map
	for {
		blk, err := ic.PopBlock()
		if err != nil {
			t.Fatal(err)
		}
		if blk == nil {
			break
		}
		for i := 0; i < blk.Count(); i++ {
			ent := blk.Entry(i)
			if ent == nil {
				t.Fatal("Saw nil entry")
			}
			pullmp[hashEntry(ent)] = ent
		}
	}

	if err := ic.Close(); err != nil {
		t.Fatal(err)
	}

	if len(mp) != len(pullmp) {
		t.Fatal("Returned entry count doesn't match", len(mp), len(pullmp))
	}

	//ensure the two maps are a perfect union
	for k, _ := range mp {
		if _, ok := pullmp[k]; !ok {
			t.Fatal("Entry not found")
		}
	}

	clean(t)
}

func TestCacheSaveAndRestore(t *testing.T) {
}

func TestClean(t *testing.T) {
	clean(t)
}

func clean(t *testing.T) {
	if err := os.RemoveAll(tstLoc); err != nil {
		t.Fatal(err)
	}
}

func hashEntry(ent *entry.Entry) [16]byte {
	buff, _ := ent.MarshallBytes()
	return md5.Sum(buff)
}
