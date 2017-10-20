/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"gravwell/oss/ingest/entry"
	"testing"
	"time"
)

const (
	testMuxerFeederCount int = 4096
)

var (
	testCfg = UniformMuxerConfig{
		Destinations: []string{`tcp://127.1.1.1:55555`, `tcp://127.5.5.5:55555`},
		Tags:         []string{`testA`, `testB`},
		Auth:         `badauth`,
		ChannelSize:  1024,
		EnableCache:  true,
		CacheConfig: IngestCacheConfig{
			FileBackingLocation: tstLoc,
			MemoryCacheSize:     memCacheSize,
			TickInterval:        time.Second,
		},
	}
)

func TestNewMuxerCache(t *testing.T) {
	im, err := NewUniformMuxer(testCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := im.Close(); err != nil {
		t.Fatal(err)
	}
	clean(t)
}

func TestNewMuxerCacheStart(t *testing.T) {
	im, err := NewUniformMuxer(testCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := im.Start(); err != nil {
		t.Fatal(err)
	}
	if err := im.Close(); err != nil {
		t.Fatal(err)
	}
	clean(t)
}

func TestNewMuxerCacheStartAndWait(t *testing.T) {
	im, err := NewUniformMuxer(testCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := im.Start(); err != nil {
		t.Fatal(err)
	}
	if err := im.WaitForHot(time.Second); err != nil {
		t.Fatal(err)
	}
	if err := im.Close(); err != nil {
		t.Fatal(err)
	}
	clean(t)
}

func TestNewMuxerCacheFeed(t *testing.T) {
	mp := make(map[[16]byte]*entry.Entry, 128)
	pullmp := make(map[[16]byte]*entry.Entry, 128)
	ts := entry.Now()
	key := ts.Sec

	im, err := NewUniformMuxer(testCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := im.Start(); err != nil {
		t.Fatal(err)
	}
	if err := im.WaitForHot(time.Second); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < testMuxerFeederCount; i++ {
		ent := makeEntryWithKey(key)
		if err := im.WriteEntry(ent); err != nil {
			t.Fatal(err)
		}
		mp[hashEntry(ent)] = ent
	}

	if err := im.Close(); err != nil {
		t.Fatal(err)
	}

	//open the cache system with a pure ingest cache object
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

func TestNewMuxerCacheMultiFeed(t *testing.T) {
	mp := make(map[[16]byte]*entry.Entry, 128)
	pullmp := make(map[[16]byte]*entry.Entry, 128)
	ts := entry.Now()
	key := ts.Sec

	im, err := NewUniformMuxer(testCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := im.Start(); err != nil {
		t.Fatal(err)
	}
	if err := im.WaitForHot(time.Second); err != nil {
		t.Fatal(err)
	}

	keys := make([]int64, 64)
	for i := range keys {
		keys[i] = key
		key--
	}

	for i := 0; i < testMuxerFeederCount; i++ {
		ent := makeEntryWithKey(keys[i%len(keys)])
		if err := im.WriteEntry(ent); err != nil {
			t.Fatal(err)
		}
		mp[hashEntry(ent)] = ent
	}

	if err := im.Close(); err != nil {
		t.Fatal(err)
	}

	//open the cache system with a pure ingest cache object
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

func TestMuxerClean(t *testing.T) {
	clean(t)
}
