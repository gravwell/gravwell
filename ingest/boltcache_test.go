package ingest

import (
	"encoding/gob"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/chancacher"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	tstLoc       string = `/tmp/cache_test.db`
	memCacheSize uint64 = 1024 * 1024 //1MB
)

func TestTransition(t *testing.T) {
	gob.Register(&entry.Entry{})

	// build an old cache
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)
	ts := entry.Now()

	tmp, err := ioutil.TempFile("", "bolttest")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	ic, err := NewIngestCache(IngestCacheConfig{
		FileBackingLocation: tmp.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// write out some fake tags
	if err := ic.UpdateStoredTagList([]string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}

	if err := ic.Start(echan, bchan); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		ent := &entry.Entry{
			TS:   ts,
			SRC:  net.ParseIP("127.0.0.1"),
			Tag:  entry.EntryTag(0),
			Data: []byte("test data"),
		}
		echan <- ent
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Close(); err != nil {
		t.Fatal(err)
	}

	// build a MuxerConfig and transition
	err = boltTransition(MuxerConfig{
		CachePath: tmp.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// tmp is now a directory bcause of the transition
	defer os.RemoveAll(tmp.Name())

	// read back data with chancacher
	cc, err := chancacher.NewChanCacher(0, filepath.Join(tmp.Name(), "e"), 0)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		select {
		case ee := <-cc.Out:
			e := ee.(*entry.Entry)
			if e.TS != ts || e.SRC.String() != "127.0.0.1" || e.Tag != entry.EntryTag(0) || string(e.Data) != "test data" {
				t.Fatalf("mismatched entry: %v", e)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout!")
		}
	}
	close(cc.In)

	// verify tags
	tagMap, err := readTagCache(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}

	if tagMap["a"] != 0 || tagMap["b"] != 1 || tagMap["c"] != 2 {
		t.Fatalf("invalid tagMap: %v", tagMap)
	}
}

func TestEmptyTransition(t *testing.T) {
	gob.Register(&entry.Entry{})

	// build an empty old cache
	echan := make(chan *entry.Entry, 8)
	bchan := make(chan []*entry.Entry, 8)

	tmp, err := ioutil.TempFile("", "bolttest")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	ic, err := NewIngestCache(IngestCacheConfig{
		FileBackingLocation: tmp.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// write out some fake tags
	if err := ic.UpdateStoredTagList([]string{"a", "b", "c"}); err != nil {
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

	// build a MuxerConfig and transition
	err = boltTransition(MuxerConfig{
		CachePath: tmp.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// tmp is now a directory bcause of the transition
	defer os.RemoveAll(tmp.Name())

	// read back data with chancacher
	cc, err := chancacher.NewChanCacher(0, filepath.Join(tmp.Name(), "e"), 0)
	if err != nil {
		t.Fatal(err)
	}

	close(cc.In)

	// verify tags
	tagMap, err := readTagCache(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}

	if len(tagMap) != 0 {
		t.Fatalf("invalid tagMap: %v", tagMap)
	}
}
