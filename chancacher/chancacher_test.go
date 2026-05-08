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
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const DEFAULT_TIMEOUT = 2 * time.Second

var defaultLogger log.IngestLogger

type ChanCacheTester struct {
	V    int
	Data string
}

func (t *ChanCacheTester) Size() uint64 {
	return 1
}

func TestMain(m *testing.M) {
	defaultLogger = log.NoLogger()
	gob.Register(&ChanCacheTester{})
	os.Exit(m.Run())
}

func TestFlock(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	c, err := NewChanCacher(2, dir, 0, defaultLogger)
	if err != nil {
		t.Fatal(err)
	}
	close(c.In)
	<-c.Out

	c, err = NewChanCacher(2, dir, 0, defaultLogger)
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewChanCacher(2, dir, 0, defaultLogger)
	if err == nil {
		t.Fatal("lock taken twice!")
	}

	close(c.In)
	<-c.Out
}

func TestBlockDepth(t *testing.T) {
	c, _ := NewChanCacher(2, "", 0, defaultLogger)

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
	dir := t.TempDir()
	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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
	c, _ := NewChanCacher(2, "", 0, defaultLogger)

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
	dir := t.TempDir()
	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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

	c, _ = NewChanCacher(2, dir, 0, defaultLogger)

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
	dir := t.TempDir()

	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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
	dir := t.TempDir()

	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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
	errch := make(chan error, 1)
	go func(ec chan error) {
		defer close(ec)
		// now we should read everything back in order
		for i := 0; i < 100; i++ {
			select {
			case v := <-c.Out:
				if v == nil {
					ec <- errors.New("nil result!")
					return
				} else {
					results[v.(*ChanCacheTester).V]++
				}
			case <-time.After(DEFAULT_TIMEOUT):
				ec <- errors.New("channel should not block!")
				return
			}
		}
	}(errch)

	for c.CacheHasData() {
		time.Sleep(100 * time.Millisecond)
	}
	c.Drain()

	if err := <-errch; err != nil {
		t.Fatal(err)
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

func TestCacheStartStop(t *testing.T) {
	dir := t.TempDir()

	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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
	dir := t.TempDir()

	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "merge")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	NewChanCacher(2, dir, 0, defaultLogger)

	if _, err = os.Stat(f.Name()); err == nil {
		t.Fatal("failed to get an error on statting merge file")
	}
	if !strings.Contains(err.Error(), "no such file") {
		t.Error("file still exists!", err)
		t.FailNow()
	}
}

func TestMerge(t *testing.T) {
	staging := t.TempDir()
	dir := t.TempDir()

	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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
	c, _ = NewChanCacher(2, dir, 0, defaultLogger)

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

	c, _ = NewChanCacher(2, dir, 0, defaultLogger)

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
	dir := t.TempDir()

	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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
	dir := t.TempDir()

	c, _ := NewChanCacher(0, dir, 10, defaultLogger)

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
	dir := t.TempDir()
	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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

	c, _ = NewChanCacher(2, dir, 0, defaultLogger)

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
				ent, ok := v.(*entry.Entry)
				if !ok {
					t.Fatalf("Failed to cast %T to *entry.Entry", v)
				}
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
	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

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

func Test_validateCache(t *testing.T) {
	cacheDir, err := os.MkdirTemp("", "chancachertest")
	if err != nil {
		t.Fatalf("could not create cacheDir: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	quarantineFolder := "quarantine"
	quarantineFile := filepath.Join(cacheDir, quarantineFolder, "cache-a.1")
	cacheFileName := filepath.Join(cacheDir, "cache-a")

	// Non-existent file should return nil (nothing to validate)
	err = validateCache(cacheFileName, quarantineFolder, defaultLogger)
	if err != nil {
		t.Fatalf("validateCache should return nil for non-existent file: %v", err)
	}

	initialCacheHandler, err := os.Create(cacheFileName)
	if err != nil {
		t.Fatalf("could not create initial cache file: %v", err)
	}
	initialCacheStats, err := initialCacheHandler.Stat()
	if err != nil {
		t.Fatalf("could not get stats of initial cache file: %v", err)
	}

	// Trigger permission error flow
	if err := initialCacheHandler.Chmod(0000); err != nil {
		t.Fatalf("could not change permissions on initial cache file: %v", err)
	}
	initialCacheHandler.Close()

	err = validateCache(cacheFileName, quarantineFolder, defaultLogger)
	if err != nil {
		t.Fatalf("validateCache should return nil after quarantining: %v", err)
	}

	// Verify original file is gone
	if _, err := os.Stat(cacheFileName); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("original cache file should have been moved to quarantine")
	}

	quarantineFileStats, err := os.Stat(quarantineFile)
	if err != nil {
		t.Fatalf("could not get quarantine file stats: %v", err)
	}

	if !os.SameFile(quarantineFileStats, initialCacheStats) {
		t.Fatal("initial file was modified when moved to quarantine location")
	}

	if err = os.RemoveAll(quarantineFile); err != nil {
		t.Fatalf("could not clean up quarantine file: %v", err)
	}

	// Trigger some other error flow (file is a directory)
	if err := os.Mkdir(cacheFileName, CacheDirPerm); err != nil {
		t.Fatalf("could not create initial cache file as directory: %v", err)
	}

	err = validateCache(cacheFileName, quarantineFolder, defaultLogger)
	if err == nil {
		t.Fatalf("validateCache should return error when cache path is a directory")
	}

	// Trigger validation error flow
	if err := os.RemoveAll(cacheFileName); err != nil {
		t.Fatalf("could not remove initial cache dir: %v", err)
	}
	corruptedCacheHandler, err := os.OpenFile(cacheFileName, CacheFlagPermissions, CacheFilePerm)
	if err != nil {
		t.Fatalf("could not create mock corrupted cache file: %v", err)
	}
	if _, err := corruptedCacheHandler.WriteString("notvalid"); err != nil {
		t.Fatalf("could not write mock corrupted cache file: %v", err)
	}
	corruptedCacheStats, err := corruptedCacheHandler.Stat()
	if err != nil {
		t.Fatalf("could not get stats on mock corrupted cache file: %v", err)
	}
	corruptedCacheHandler.Close()

	err = validateCache(cacheFileName, quarantineFolder, defaultLogger)
	if err != nil {
		t.Fatalf("validateCache should return nil after quarantining corrupted cache: %v", err)
	}

	// Verify original file is gone
	if _, err := os.Stat(cacheFileName); !os.IsNotExist(err) {
		t.Fatal("corrupted cache file should have been moved to quarantine")
	}

	quarantineFileStats, err = os.Stat(quarantineFile)
	if err != nil {
		t.Fatalf("could not get quarantine file stats: %v", err)
	}

	if !os.SameFile(corruptedCacheStats, quarantineFileStats) {
		t.Fatalf("corrupted cache was not quarantined")
	}

	if err = os.RemoveAll(quarantineFile); err != nil {
		t.Fatalf("could not clean up quarantine file: %v", err)
	}
}

func Test_quarantineCache(t *testing.T) {
	cacheDir, err := os.MkdirTemp("", "chancachertest")
	if err != nil {
		t.Fatalf("could not create cacheDir: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	quarantineFolder := "quarantine"
	quarantineDir := filepath.Join(cacheDir, quarantineFolder)
	cacheFileName := filepath.Join(cacheDir, "cache-a")
	initialCacheHandler, err := os.Create(cacheFileName)
	if err != nil {
		t.Fatalf("could not create initial cache file: %v", err)
	}
	defer initialCacheHandler.Close()

	initialCacheStats, err := initialCacheHandler.Stat()
	if err != nil {
		t.Fatalf("could not get initial cache file stats: %v", err)
	}

	err = quarantineCache(cacheFileName, quarantineFolder, defaultLogger)
	expectedQuarantineLocation := filepath.Join(quarantineDir, "cache-a.1")
	if err != nil {
		t.Fatal(err)
	}

	// Verify original file is gone
	if _, err := os.Stat(cacheFileName); !os.IsNotExist(err) {
		t.Fatalf("original cache file should have been moved to quarantine")
	}

	quarantinedStat, err := os.Stat(expectedQuarantineLocation)
	if err != nil {
		t.Fatalf("cache file wasn't quarantined: %v", err)
	}

	if !os.SameFile(quarantinedStat, initialCacheStats) {
		t.Fatal("initial file was modified when moved to quarantine location")
	}
}

func Test_getQuarantineCacheName(t *testing.T) {
	baseName := filepath.Join(os.TempDir(), "chancachertest", "quarantine", "cachetest")

	tests := []struct {
		caseName    string
		matches     []string
		expectedRes string
	}{
		{
			"empty quarantine folder",
			[]string{},
			fmt.Sprintf("%s.1", baseName),
		},
		{
			"unrelated file in dir",
			[]string{fmt.Sprintf("%s.backup", baseName)},
			fmt.Sprintf("%s.1", baseName),
		},
		{
			"some files, inserts at last position",
			[]string{

				fmt.Sprintf("%s.1", baseName),
				fmt.Sprintf("%s.2", baseName),
				fmt.Sprintf("%s.3", baseName),
				fmt.Sprintf("%s.4", baseName),
				fmt.Sprintf("%s.5", baseName),
			},
			fmt.Sprintf("%s.6", baseName),
		},
		{
			"one file, inserts at last position",
			[]string{fmt.Sprintf("%s.999", baseName)},
			fmt.Sprintf("%s.1000", baseName),
		},
		{
			"several files, some unrelated, inserts at last position",
			[]string{
				fmt.Sprintf("%s.19", baseName),
				fmt.Sprintf("%s.backup2", baseName),
				fmt.Sprintf("%s.backup", baseName),
				fmt.Sprintf("%s.999", baseName),
			},
			fmt.Sprintf("%s.1000", baseName),
		},
	}

	for _, test := range tests {
		if result := getQuarantineCacheName(baseName, test.matches); result != test.expectedRes {
			t.Fatalf("%s: unexpected name %s, expected %s", test.caseName, test.expectedRes, result)
		}
	}
}

func TestSpam(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewChanCacher(100, dir, 1024*1024*1000, defaultLogger)

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

// TestCycleConnection tests that multiple ChanCachers can be created and destroyed
// sequentially using the same cache directory, without losing any cached entries.
// This simulates a scenario where an ingester cycles through connections repeatedly
// because of some faultyness and ensures the cache properly persists and recovers data
// across these cycles.
func TestCycleConnection(t *testing.T) {
	dir := t.TempDir()

	const cycles = 10
	const entriesPerCycle = 100

	for i := 0; i < cycles; i++ {
		c, err := NewChanCacher(2, dir, 0, defaultLogger)
		if err != nil {
			t.Fatalf("cycle %d: failed to create ChanCacher: %v", i, err)
		}

		for j := 0; j < entriesPerCycle; j++ {
			select {
			case c.In <- &ChanCacheTester{V: i*entriesPerCycle + j, Data: fmt.Sprintf("cycle %d entry %d", i, j)}:
			case <-time.After(DEFAULT_TIMEOUT):
				t.Fatalf("cycle %d: channel write should not block for entry %d", i, j)
			}
		}

		close(c.In)
		c.Commit()
		<-c.Out
	}

	// Create a final ChanCacher to read all entries back
	c, err := NewChanCacher(2, dir, 0, defaultLogger)
	if err != nil {
		t.Fatalf("final: failed to create ChanCacher: %v", err)
	}

	results := make(map[int]int)
	totalExpected := cycles * entriesPerCycle

	for i := 0; i < totalExpected; i++ {
		select {
		case v := <-c.Out:
			if v == nil {
				t.Errorf("received nil at position %d", i)
			} else {
				results[v.(*ChanCacheTester).V]++
			}
		case <-time.After(5 * DEFAULT_TIMEOUT):
			t.Fatalf("channel read blocked after %d entries (expected %d)", i, totalExpected)
		}
	}

	// Verify we got all entries exactly once
	for i := 0; i < totalExpected; i++ {
		count, ok := results[i]
		if !ok {
			t.Errorf("missing entry %d", i)
		} else if count != 1 {
			t.Errorf("entry %d appeared %d times (expected 1)", i, count)
		}
	}

	// Clean up
	close(c.In)
	<-c.Out
}

func BenchmarkReference(b *testing.B) {
	out := make(chan int)
	in := make(chan int)

	go func() {
		for range out {
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
	c, _ := NewChanCacher(0, "", 0, defaultLogger)

	v := &ChanCacheTester{V: 1}

	// something to consume
	go func() {
		for range c.Out {
		}
	}()

	for i := 0; i < 100000; i++ {
		c.In <- v
	}

	c.Drain()
}

func BenchmarkBufferedSmall(b *testing.B) {
	c, _ := NewChanCacher(10, "", 0, defaultLogger)

	v := &ChanCacheTester{V: 1}

	// something to consume
	go func() {
		for range c.Out {
		}
	}()

	for i := 0; i < 100000; i++ {
		c.In <- v
	}

	c.Drain()
}

func BenchmarkBufferedLarge(b *testing.B) {
	c, _ := NewChanCacher(1000000, "", 0, defaultLogger)

	v := &ChanCacheTester{V: 1}

	// something to consume
	go func() {
		for range c.Out {
		}
	}()

	for i := 0; i < 10000000; i++ {
		c.In <- v
	}

	c.Drain()
}

func BenchmarkCacheBlocked(b *testing.B) {
	dir := b.TempDir()
	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

	COUNT := 10000000

	for i := 0; i < COUNT; i++ {
		c.In <- &ChanCacheTester{V: i}
	}

	for i := 0; i < COUNT; i++ {
		<-c.Out
	}
}

func BenchmarkCacheStreaming(b *testing.B) {
	dir := b.TempDir()
	c, _ := NewChanCacher(2, dir, 0, defaultLogger)

	COUNT := 10000000

	// something to consume
	go func() {
		for range c.Out {
		}
	}()

	for i := 0; i < COUNT; i++ {
		c.In <- &ChanCacheTester{V: i}
	}

	c.Drain()
}
