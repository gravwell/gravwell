/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package chancacher

import (
	"encoding/gob"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fmt"
	"net"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"strconv"
)

const DEFAULT_TIMEOUT = 2 * time.Second

type ChanCacheTester struct {
	V    int
	Data string
}

func (t *ChanCacheTester) Size() uint64 {
	return 1
}

func TestFlock(t *testing.T) {
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, err := NewChanCacher(2, dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	close(c.In)
	<-c.Out

	c, err = NewChanCacher(2, dir, 0)
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewChanCacher(2, dir, 0)
	if err == nil {
		t.Fatal("lock taken twice!")
	}

	close(c.In)
	<-c.Out
}

func TestBlockDepth(t *testing.T) {
	c, _ := NewChanCacher(2, "", 0)

	v := &ChanCacheTester{V: 1}

	if c.BufferSize() != 0 {
		t.Fail()
	}

	// the first 3 writes are ok (2 sitting in out, and one
	// block on a write to out)
	c.In <- v
	c.In <- v
	c.In <- v

	if c.BufferSize() != 2 {
		t.Fail()
	}

	// this should block
	select {
	case c.In <- v:
		t.Errorf("channel send should block!")
	default:
		// success
	}

	<-c.Out

	// this should not block
	select {
	case c.In <- v:
		// success
	case <-time.After(DEFAULT_TIMEOUT):
		t.Errorf("channel send should not block!")
	}

}

func TestTearDownCache(t *testing.T) {
	gob.Register(&ChanCacheTester{})

	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	for i := 0; i < 100; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	close(c.In)

	// reads on the cache are not guaranteed to be in-order, so instead we
	// count the number of times we've seen each value, and expect to see a
	// count of 1 for 0-99.
	results := make(map[int]int)

	// now we should read everything back in order
	for i := 0; i < 100; i++ {
		select {
		case v := <-c.Out:
			if v == nil {
				t.Error("nil result!")
			} else {
				results[v.(*ChanCacheTester).V]++
			}
		case <-time.After(5 * DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	// verify counts
	for i := 0; i < 100; i++ {
		count, ok := results[i]
		if !ok {
			t.Error("didn't get result:", i)
		} else if count != 1 {
			t.Errorf("mismatched count: %v: %v", i, count)
		}
	}

	// c.Out should be closed
	select {
	case _, ok := <-c.Out:
		if !ok {
			// success
		} else {
			t.Error("channel still open!")
		}
	case <-time.After(DEFAULT_TIMEOUT):
		t.Error("channel should not block!")
	}

}

func TestTearDownNoCache(t *testing.T) {
	c, _ := NewChanCacher(2, "", 0)

	v := &ChanCacheTester{V: 1}

	// the first 3 writes are ok (2 sitting in out, and one
	// block on a write to out)
	c.In <- v
	c.In <- v
	c.In <- v

	close(c.In)

	a := <-c.Out
	if a.(*ChanCacheTester).V != 1 {
		t.Fail()
	}
	a = <-c.Out
	if a.(*ChanCacheTester).V != 1 {
		t.Fail()
	}
	a = <-c.Out
	if a.(*ChanCacheTester).V != 1 {
		t.Fail()
	}

	// c.Out should be closed
	select {
	case _, ok := <-c.Out:
		if !ok {
			// success
		} else {
			t.Error("channel still open!")
		}
	case <-time.After(DEFAULT_TIMEOUT):
		t.Error("channel should not block!")
	}

}

func TestRecover(t *testing.T) {
	gob.Register(&ChanCacheTester{})

	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}

	c, _ := NewChanCacher(2, dir, 0)

	for i := 0; i < 100; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	close(c.In)
	c.Commit()
	<-c.Out

	// now create a new ChanCacher in dir and read the data out.
	defer os.RemoveAll(dir)

	c, _ = NewChanCacher(2, dir, 0)

	// reads on the cache are not guaranteed to be in-order, so instead we
	// count the number of times we've seen each value, and expect to see a
	// count of 1 for 0-99.
	results := make(map[int]int)

	// now we should read everything back in order
	for i := 0; i < 100; i++ {
		select {
		case v := <-c.Out:
			if v == nil {
				t.Error("nil result!")
			} else {
				//fmt.Println(v.(*ChanCacheTester).V)
				results[v.(*ChanCacheTester).V]++
			}
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			//t.FailNow()
		}
	}

	// verify counts
	for i := 0; i < 100; i++ {
		count, ok := results[i]
		if !ok {
			t.Error("didn't get result:", i)
		} else if count != 1 {
			t.Errorf("mismatched count: %v: %v", i, count)
		}
	}
}

func TestCommit(t *testing.T) {
	gob.Register(&ChanCacheTester{})

	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	for i := 0; i < 100; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	close(c.In)
	c.Commit()

	// c.Out should be closed
	select {
	case _, ok := <-c.Out:
		if !ok {
			// success
		} else {
			t.Error("channel still open!")
		}
	case <-time.After(DEFAULT_TIMEOUT):
		t.Error("channel should not block!")
	}
}

func TestDrain(t *testing.T) {
	gob.Register(&ChanCacheTester{})
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	for i := 0; i < 100; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	// reads on the cache are not guaranteed to be in-order, so instead we
	// count the number of times we've seen each value, and expect to see a
	// count of 1 for 0-99.
	results := make(map[int]int)

	go func() {
		// now we should read everything back in order
		for i := 0; i < 100; i++ {
			select {
			case v := <-c.Out:
				if v == nil {
					t.Error("nil result!")
				} else {
					results[v.(*ChanCacheTester).V]++
				}
			case <-time.After(DEFAULT_TIMEOUT):
				t.Error("channel should not block!")
				t.FailNow()
			}
		}
	}()

	for c.CacheHasData() {
		time.Sleep(100 * time.Millisecond)
	}
	c.Drain()

	// verify counts
	for i := 0; i < 100; i++ {
		count, ok := results[i]
		if !ok {
			t.Error("didn't get result:", i)
		} else if count != 1 {
			t.Errorf("mismatched count: %v: %v", i, count)
		}
	}
}

func TestCacheStartStop(t *testing.T) {
	gob.Register(&ChanCacheTester{})
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	for i := 0; i < 99; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	time.Sleep(100 * time.Millisecond)

	// stop the cache and make sure it blocks
	c.CacheStop()

	c.In <- &ChanCacheTester{V: 99}

	select {
	case c.In <- &ChanCacheTester{V: 100}:
		t.Error("channel should block!")
	case <-time.After(DEFAULT_TIMEOUT):
		// success
	}

	c.CacheStart()

	// reads on the cache are not guaranteed to be in-order, so instead we
	// count the number of times we've seen each value, and expect to see a
	// count of 1 for 0-99.
	results := make(map[int]int)

	// now we should read everything back in order
	for i := 0; i < 100; i++ {
		select {
		case v := <-c.Out:
			if v == nil {
				t.Error("nil result!")
			} else {
				results[v.(*ChanCacheTester).V]++
			}
		case <-time.After(5 * DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	// verify counts
	for i := 0; i < 100; i++ {
		count, ok := results[i]
		if !ok {
			t.Error("didn't get result:", i)
		} else if count != 1 {
			t.Errorf("mismatched count: %v: %v", i, count)
		}
	}
}

func TestCache(t *testing.T) {
	gob.Register(&ChanCacheTester{})
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	for i := 0; i < 100; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	// reads on the cache are not guaranteed to be in-order, so instead we
	// count the number of times we've seen each value, and expect to see a
	// count of 1 for 0-99.
	results := make(map[int]int)

	// now we should read everything back in order
	for i := 0; i < 100; i++ {
		select {
		case v := <-c.Out:
			if v == nil {
				t.Error("nil result!")
			} else {
				results[v.(*ChanCacheTester).V]++
			}
		case <-time.After(5 * DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	// verify counts
	for i := 0; i < 100; i++ {
		count, ok := results[i]
		if !ok {
			t.Error("didn't get result:", i)
		} else if count != 1 {
			t.Errorf("mismatched count: %v: %v", i, count)
		}
	}
}

func TestDetritus(t *testing.T) {
	gob.Register(&ChanCacheTester{})

	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	f, err := ioutil.TempFile(dir, "merge")
	f.Close()
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	NewChanCacher(2, dir, 0)

	_, err = os.Stat(f.Name())
	if !strings.Contains(err.Error(), "no such file") {
		t.Error("file still exists!", err)
		t.FailNow()
	}
}

func TestMerge(t *testing.T) {
	gob.Register(&ChanCacheTester{})

	staging, err := ioutil.TempDir("", "chancachertest_staging")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)

	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	for i := 0; i < 100; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	close(c.In)
	c.Commit()
	<-c.Out

	// move our data to staging
	file := "cache_a"
	fi, err := os.Stat(filepath.Join(dir, "cache_a"))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	if fi.Size() == 0 {
		file = "cache_b"
		fi, err = os.Stat(filepath.Join(dir, "cache_b"))
		if err != nil {
			t.Error(err)
			t.FailNow()
		}
		if fi.Size() == 0 {
			t.Error("no data in cache!")
			t.FailNow()
		}
	}
	err = os.Rename(filepath.Join(dir, file), filepath.Join(staging, file))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	// now do it again
	c, _ = NewChanCacher(2, dir, 0)

	for i := 100; i < 200; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	close(c.In)
	c.Commit()
	<-c.Out

	// now put the staging file back in the zero-length data file
	filedst := "cache_a"
	fi, err = os.Stat(filepath.Join(dir, "cache_a"))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	if fi.Size() != 0 {
		filedst = "cache_b"
		fi, err = os.Stat(filepath.Join(dir, "cache_b"))
		if err != nil {
			t.Error(err)
			t.FailNow()
		}
		if fi.Size() != 0 {
			t.Error("both files have data in cache!")
			t.FailNow()
		}
	}

	err = os.Rename(filepath.Join(staging, file), filepath.Join(dir, filedst))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	c, _ = NewChanCacher(2, dir, 0)

	// reads on the cache are not guaranteed to be in-order, so instead we
	// count the number of times we've seen each value, and expect to see a
	// count of 1 for 0-199.
	results := make(map[int]int)

	// now we should read everything back in order
	for i := 0; i < 200; i++ {
		select {
		case v := <-c.Out:
			if v == nil {
				t.Error("nil result!")
			} else {
				//fmt.Println(v.(*ChanCacheTester).V)
				results[v.(*ChanCacheTester).V]++
			}
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			//t.FailNow()
		}
	}

	// verify counts
	for i := 0; i < 200; i++ {
		count, ok := results[i]
		if !ok {
			t.Error("didn't get result:", i)
		} else if count != 1 {
			t.Errorf("mismatched count: %v: %v", i, count)
		}
	}
}

func TestCacheHasData(t *testing.T) {
	gob.Register(&ChanCacheTester{})
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	if c.CacheHasData() {
		t.Fail()
	}

	for i := 0; i < 100; i++ {
		select {
		case c.In <- &ChanCacheTester{V: i}:
		// success
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	if !c.CacheHasData() {
		t.Fail()
	}
}

func TestCacheMaxSize(t *testing.T) {
	gob.Register(&ChanCacheTester{})
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(0, dir, 10)

	c.In <- &ChanCacheTester{V: 1}
	c.In <- &ChanCacheTester{V: 1}
	c.In <- &ChanCacheTester{V: 1}

	select {
	case c.In <- &ChanCacheTester{V: 1}:
		t.Error("channel should block!")
		t.FailNow()
	case <-time.After(DEFAULT_TIMEOUT):
		// success
	}

	// read all 3 entries out
	<-c.Out
	<-c.Out
	<-c.Out

	if c.Size() != 0 {
		t.Errorf("Size mismatch %v != 0", c.Size())
		t.FailNow()
	}
}

// TestCacheEntries verifies that we can write entries, with EVs
// attached, and read them back out.
func TestCacheEntries(t *testing.T) {
	gob.Register(&entry.Entry{})
	gob.Register(&entry.EVBlock{})
	dir, err := os.MkdirTemp("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}

	c, _ := NewChanCacher(2, dir, 0)

	for i := 0; i < 100; i++ {
		e := &entry.Entry{
			Data: []byte(fmt.Sprintf("%d", i)),
			SRC:  net.IP([]byte{'a', 'b', 'c', 'd'}),
			TS:   entry.Now(),
		}
		e.AddEnumeratedValueEx("index", i)
		select {
		case c.In <- e:
		case <-time.After(DEFAULT_TIMEOUT):
			t.Error("channel write should not block")
			t.FailNow()
		}
	}
	close(c.In)
	c.Commit()
	<-c.Out

	defer os.RemoveAll(dir)

	c, _ = NewChanCacher(2, dir, 0)

	// reads on the cache are not guaranteed to be in-order, so instead we
	// count the number of times we've seen each value, and expect to see a
	// count of 1 for 0-99.
	results := make(map[int]int)

	// now we should read everything back in order
	for i := 0; i < 100; i++ {
		select {
		case v := <-c.Out:
			if v == nil {
				t.Error("nil result!")
			} else {
				ent := v.(*entry.Entry)
				idx, ok := ent.GetEnumeratedValue("index")
				if !ok {
					t.Fatalf("Didn't get enumerated value index: %+v", ent)
				}
				results[int(idx.(int64))]++
			}
		case <-time.After(5 * DEFAULT_TIMEOUT):
			t.Errorf("channel blocked after %d reads!", i)
			t.FailNow()
		}
	}

	// verify counts
	for i := 0; i < 100; i++ {
		count, ok := results[i]
		if !ok {
			t.Error("didn't get result:", i)
		} else if count != 1 {
			t.Errorf("mismatched count: %v: %v", i, count)
		}
	}
}

// TestCacheOldEntries ensures that we can still read old entries
// cached before the enumerated value fields were exported.
func TestCacheOldEntries(t *testing.T) {
	gob.Register(&entry.Entry{})
	dir, err := os.MkdirTemp("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	// Now copy in the existing stuff
	cpF := func(name string) {
		if bts, err := os.ReadFile(filepath.Join("old-entries-cache", name)); err != nil {
			t.Fatal(err)
		} else {
			if err := os.WriteFile(filepath.Join(dir, name), bts, 0666); err != nil {
				t.Fatal(err)
			}
		}
	}
	cpF("cache_a")
	cpF("cache_b")
	cpF("lock")
	c, _ := NewChanCacher(2, dir, 0)

	// reads on the cache are not guaranteed to be in-order, so instead we
	// count the number of times we've seen each value, and expect to see a
	// count of 1 for 0-99.
	results := make(map[int]int)

	// now we should read everything back in order
	for i := 0; i < 100; i++ {
		select {
		case v := <-c.Out:
			if v == nil {
				t.Error("nil result!")
			} else {
				ent := v.(*entry.Entry)
				if idx, err := strconv.ParseInt(string(ent.Data), 10, 64); err != nil {
					t.Fatalf("failed to parse entry's data as int: %v, %+v", err, ent)
				} else {
					results[int(idx)]++
				}
			}
		case <-time.After(5 * DEFAULT_TIMEOUT):
			t.Error("channel should not block!")
			t.FailNow()
		}
	}

	// verify counts
	for i := 0; i < 100; i++ {
		count, ok := results[i]
		if !ok {
			t.Error("didn't get result:", i)
		} else if count != 1 {
			t.Errorf("mismatched count: %v: %v", i, count)
		}
	}
}

func TestSpam(t *testing.T) {
	gob.Register(&ChanCacheTester{})
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(100, dir, 1024*1024*1000)

	go func() {
		for i := 0; i < 5000000; i++ {
			c.In <- &ChanCacheTester{V: 1, Data: "adsfasdfjas;ldfkja;lkefjwl;kfjawlekfja;lwkefj;alwkefj;lawkefj;alkefj;akwfj;akwfje;aklefj;akef;aklwfe;alkwfje;lakwfj;akfej;akfe;akefj;awejfa;effefa;edflkj"}
		}
	}()

	for i := 0; i < 5000000; i++ {
		v := <-c.Out
		if v.(*ChanCacheTester).V != 1 {
			t.Fatal("what")
		}
	}

}

func BenchmarkReference(b *testing.B) {
	out := make(chan int)
	in := make(chan int)

	go func() {
		for _ = range out {
		}
	}()

	go func() {
		for v := range in {
			out <- v
		}
	}()

	for i := 0; i < 100000; i++ {
		in <- 1
	}
	close(in)
}

func BenchmarkUnbufferedSmall(b *testing.B) {
	c, _ := NewChanCacher(0, "", 0)

	v := &ChanCacheTester{V: 1}

	// something to consume
	go func() {
		for _ = range c.Out {
		}
	}()

	for i := 0; i < 100000; i++ {
		c.In <- v
	}

	c.Drain()
}

func BenchmarkBufferedSmall(b *testing.B) {
	c, _ := NewChanCacher(10, "", 0)

	v := &ChanCacheTester{V: 1}

	// something to consume
	go func() {
		for _ = range c.Out {
		}
	}()

	for i := 0; i < 100000; i++ {
		c.In <- v
	}

	c.Drain()
}

func BenchmarkBufferedLarge(b *testing.B) {
	c, _ := NewChanCacher(1000000, "", 0)

	v := &ChanCacheTester{V: 1}

	// something to consume
	go func() {
		for _ = range c.Out {
		}
	}()

	for i := 0; i < 10000000; i++ {
		c.In <- v
	}

	c.Drain()
}

func BenchmarkCacheBlocked(b *testing.B) {
	gob.Register(&ChanCacheTester{})
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	COUNT := 10000000

	for i := 0; i < COUNT; i++ {
		c.In <- &ChanCacheTester{V: i}
	}

	for i := 0; i < COUNT; i++ {
		<-c.Out
	}
}

func BenchmarkCacheStreaming(b *testing.B) {
	gob.Register(&ChanCacheTester{})
	dir, err := ioutil.TempDir("", "chancachertest")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c, _ := NewChanCacher(2, dir, 0)

	COUNT := 10000000

	// something to consume
	go func() {
		for _ = range c.Out {
		}
	}()

	for i := 0; i < COUNT; i++ {
		c.In <- &ChanCacheTester{V: i}
	}

	c.Drain()
}
