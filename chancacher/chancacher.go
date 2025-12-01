/*************************************************************************
 * Copyright 2020 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package chancacher implements a pipeline of channels (in->out) that
// provides internal buffering (via a simple buffered channel), and caching
// data to disk.
package chancacher

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

var (
	ErrInvalidCachePath = errors.New("Invalid cache path")
)

// MaxDepth specifies the maximum channel depth, which is also used when the channel
// depth is set to 0. We could set this to MaxInt but we'd likely just run out of
// memory without a clean way to triage. It's best to just enforce a sensible maximum.
const MaxDepth = 1000000

// A ChanCacher is a pipeline of channels with a variable-sized internal
// buffer. The buffer can also cache to disk. The user is expected to connect
// ChanCacher.In and ChanCacher.Out.
type ChanCacher struct {
	In      chan interface{}
	Out     chan interface{}
	runDone bool
	maxSize int

	cachePath      string
	cache          bool
	cacheR         *fileCounter
	cacheW         *fileCounter
	cacheEnc       *gob.Encoder
	cacheModified  bool
	cacheLock      sync.Mutex
	cacheReading   bool
	cachePaused    chan bool
	cacheDone      chan bool
	cacheAck       chan bool
	cacheIsDone    bool
	cacheCommitted bool

	fileLock *flock.Flock

	lgr log.IngestLogger
}

// CacheDirPerm permission on cache directories
const CacheDirPerm = 0750

// CacheFilePerm permissions on cache files
const CacheFilePerm = 0640

// CacheFlagPermissions permissions on cache files when opening
const CacheFlagPermissions = os.O_CREATE | os.O_RDWR

// NewChanCacher creates a new ChanCacher with maximum depth, and optional backing file.
// If maxDepth == 0, the ChanCacher will be unbuffered. If maxDepth == -1, the
// ChanCacher depth will be set to MaxDepth. To enable a backing store,
// provide a path to backingPath. chancachers create two files using this
// prefix named cache_a and cache_b.
//
// The maxSize argument sets the maximum amount of disk commit, in bytes.
//
// When a new ChanCacher is made, if cachePath points to existing cache files,
// the ChanCacher will immediately attempt to drain them from disk. In this
// way, you can recover data sent to disk on a crash or previous use of
// Commit().
func NewChanCacher(maxDepth int, cachePath string, maxSize int, lgr log.IngestLogger) (*ChanCacher, error) {
	if cachePath != "" {
		if fi, err := os.Stat(cachePath); err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			//just a not-exist error, we will fix this later
		} else if !fi.IsDir() {
			//exists but is not a directory, this is an error
			return nil, fmt.Errorf("Cache Path %q is not a directory: %w", cachePath, ErrInvalidCachePath)
		}
	}

	// as close to infinite as possible...
	if maxDepth == -1 || maxDepth > MaxDepth {
		maxDepth = MaxDepth
	}
	c := &ChanCacher{
		In:          make(chan interface{}),
		Out:         make(chan interface{}, maxDepth),
		cachePath:   cachePath,
		cache:       cachePath != "",
		cachePaused: make(chan bool),
		cacheDone:   make(chan bool),
		cacheAck:    make(chan bool),
		maxSize:     maxSize,
		lgr:         lgr,
	}

	// we start the cache unpaused, and because of go idioms, we have to
	// make the channel in order for "closed" states to work - we can't
	// just leave it initiated...
	close(c.cachePaused)

	if c.cache {
		var err error

		err = os.MkdirAll(c.cachePath, CacheDirPerm)
		if err != nil {
			return nil, err
		}

		rPath := filepath.Join(c.cachePath, "cache_a")
		wPath := filepath.Join(c.cachePath, "cache_b")

		// remove old merge_* files if they exist. It's possible to
		// kill an ingester before we have a chance to remove it after
		// merging, so we just do a little housekeeping ourselves.
		detritus, err := filepath.Glob(filepath.Join(c.cachePath, "merge*"))
		if err != nil {
			return nil, err
		}
		for _, v := range detritus {
			os.Remove(v)
		}

		// check if we need to merge
		var sizeR, sizeW int64
		fi, err := os.Stat(rPath)
		if err == nil {
			sizeR = fi.Size()
		}
		fi, err = os.Stat(wPath)
		if err == nil {
			sizeW = fi.Size()
		}

		// if only one file has data in it, just shuffle the files
		// around. If both have data, merge. If neither have data, no
		// action is needed.
		if sizeW != 0 && sizeR == 0 {
			err := os.Rename(wPath, rPath)
			if err != nil {
				return nil, err
			}
		} else if sizeW != 0 && sizeR != 0 {
			err := merge(rPath, wPath)
			if err != nil {
				return nil, err
			}
		}

		// set a lock for these files
		c.fileLock = flock.New(filepath.Join(c.cachePath, "lock"))
		locked, err := c.fileLock.TryLock()
		if err != nil {
			return nil, err
		}
		if !locked {
			return nil, fmt.Errorf("could not get file lock!")
		}

		// create r and w files
		quarantineFolder := "quarantine"
		r, err := openCache(rPath, quarantineFolder, c.lgr)
		if err != nil {
			return nil, err
		}

		w, err := openCache(wPath, quarantineFolder, c.lgr)
		if err != nil {
			return nil, err
		}

		if c.cacheR, err = NewFileCounter(r); err != nil {
			return nil, err
		}
		if c.cacheW, err = NewFileCounter(w); err != nil {
			return nil, err
		}

		c.cacheEnc = gob.NewEncoder(c.cacheW)

		// if the write cache data data in it already (recover), then
		// mark the cache as modified.
		fi, err = c.cacheW.Stat()
		if err != nil {
			return nil, err
		}
		if fi.Size() != 0 {
			c.cacheModified = true
		}

		go c.cacheHandler()
	}
	go c.run()
	return c, nil
}

// run connects in->out channels, watching the depth on out. When out is full,
// we block on reads from in. Optionally, we redirect input to a backing store
// with gob, and continue reading from in indefinitely. When the backing store
// is enabled, we end up plumbing in->cache->out.
func (c *ChanCacher) run() {
	for v := range c.In {
		select {
		case c.Out <- v:
		default:
			// The buffer is full. If we're not caching, just
			// block on putting the value into the buffer
			if !c.cache {
				c.Out <- v
			} else {
				// select on putting the value into out and
				// checking the paused state. This allows us to
				// block until the cache unpauses or the buffer
				// drains, whichever comes first.
				select {
				case c.Out <- v:
				case <-c.cachePaused:
					c.cacheValue(v)
				}
			}
		}
	}

	c.runDone = true

	if c.cache {
		// closing c.In stops reading input, but we allow the cache to drain
		// before closing c.Out.
		for c.CacheHasData() && !c.cacheCommitted {
			time.Sleep(100 * time.Millisecond)
		}

		// stop cacheHandler()
		c.finishCache()

		// verify the cache reader has stopped trying to write to c.Out
		<-c.cacheAck

		c.fileLock.Unlock()
	}

	// Buffered channels allow reading data until they're empty, even if
	// close, so we just close and move on.
	close(c.Out)
}

func (c *ChanCacher) cacheHandler() {
	// the main cache loop. We read from R, putting data into out directly
	// until R is drained. Once R is drained, wait for W to have data and
	// for run() to signal that we can swap buffers.
	c.cacheReading = true
	for {
		var err error

		dec := gob.NewDecoder(c.cacheR)
		var v interface{}
		for {
			err = dec.Decode(&v)
			if err != nil {
				break
			}
			if v == nil {
				continue
			}

			c.Out <- v
		}
		if err != io.EOF {
			c.lgr.Error("Unexpected error while parsing cache", log.KVErr(err))
		}

		c.cacheReading = false
		c.cacheR.Seek(0, 0)
		c.cacheR.Truncate(0)

		// This is the only place where CacheHasData() will return false

		select {
		case <-c.cacheDone:
			close(c.cacheAck)
			return
		default:
		}

		// Wait for W to have data.
		for !c.cacheModified {
			select {
			case <-c.cacheDone:
				close(c.cacheAck)
				return
			case <-time.After(time.Second):
			}
		}

		// swap caches
		c.cacheLock.Lock()
		c.cacheR, c.cacheW = c.cacheW, c.cacheR
		c.cacheR.Seek(0, 0)
		c.cacheEnc = gob.NewEncoder(c.cacheW)
		c.cacheModified = false
		c.cacheReading = true
		c.cacheLock.Unlock()
	}
}

func (c *ChanCacher) cacheValue(v interface{}) {
	if v == nil {
		return
	}
	for c.maxSize != 0 && c.Size() >= c.maxSize {
		time.Sleep(100 * time.Millisecond)
	}

	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	if err := c.cacheEnc.Encode(&v); err != nil {
		c.lgr.Error("failed to encode value into cache", log.KV("value", v), log.KVErr(err))
	}
	c.cacheModified = true
}

// CacheHasData returns if the cache has outstanding data not written to the output channel.
func (c *ChanCacher) CacheHasData() bool {
	return c.cacheModified || c.cacheReading
}

// BufferSize returns the number of elements on the internal buffer.
func (c *ChanCacher) BufferSize() int {
	return len(c.Out)
}

// CacheStart enables a stopped cache.
func (c *ChanCacher) CacheStart() {
	if !c.cache {
		return
	}
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	select {
	case <-c.cachePaused:
	default:
		close(c.cachePaused)
	}
}

// CacheStop stops a running cache. Calling Stop() will prevent the ChanCacher from
// writing any new data to the backing file, but will not stop it from reading
// (draining) the cache to the output channel.
func (c *ChanCacher) CacheStop() {
	if !c.cache {
		return
	}
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	select {
	case <-c.cachePaused:
		c.cachePaused = make(chan bool)
	default:
	}
}

// Drain blocks until the internal buffer is empty. It's possible that new data
// is still being consumed, so care should be taken when using Drain(). You
// probably don't want to use Drain(), but instead close ChanCacher.In and wait
// for the ChanCacher.Out to close, which does carry guarantees that the
// internal buffers and cache are fully drained.
func (c *ChanCacher) Drain() {
	for len(c.Out) != 0 {
		time.Sleep(100 * time.Millisecond)
	}
}

// Commit drains the buffer to the backing file and shuts down the cache.
// Commit should be called after closing the input channel if the buffer needs
// to be saved. Commit will block until the In channel is closed. The
// ChanCacher will not close the output channel until it's empty, so a typical
// production would look like:
//
//	close(c.In)
//	drainSomeDataFrom(c.Out)
//
//	// commit the rest of my data to disk
//	c.Commit()
//
//	// c.Out is now closed
//
// Once Commit() is called, draining the cache cannot be restarted, though
// writing to the cache will still work. Commit should only be used for teardown
// scenarios.
func (c *ChanCacher) Commit() {
	if !c.cache {
		c.cacheCommitted = true
		return
	}

	c.finishCache()

	// read from out and write back to the cache
	readerStopped := false
	for !c.runDone || len(c.Out) != 0 || !readerStopped {
		select {
		case <-c.cacheAck:
			readerStopped = true
		case v := <-c.Out:
			if v != nil {
				c.cacheValue(v)
			}
		}
	}

	c.cacheR.Sync()
	c.cacheW.Sync()
	c.cacheR.Close()
	c.cacheW.Close()
	if c.fileLock != nil {
		c.fileLock.Unlock()
	}

	c.cacheCommitted = true
}

func (c *ChanCacher) finishCache() {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()

	if !c.cacheIsDone {
		close(c.cacheDone)
		c.cacheIsDone = true
	}
}

// Size returns the number of bytes committed to disk. This does not include data in
// the in-memory buffer.
func (c *ChanCacher) Size() int {
	return c.cacheR.Count() + c.cacheW.Count()
}

// Merge two gob encoded files into a single file. Paths a and b are specified,
// with the resulting file in a.
func merge(a, b string) error {
	fa, err := os.Open(a)
	if err != nil {
		return err
	}
	defer fa.Close()

	fb, err := os.Open(b)
	if err != nil {
		return err
	}
	defer fb.Close()

	t, err := os.CreateTemp(filepath.Dir(a), "merge")
	if err != nil {
		return err
	}
	defer t.Close()
	defer os.Remove(t.Name())

	enc := gob.NewEncoder(t)

	adec := gob.NewDecoder(fa)
	var v interface{}
	for {
		err = adec.Decode(&v)
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		if v == nil {
			continue
		}
		err = enc.Encode(&v)
		if err != nil {
			return err
		}
	}

	bdec := gob.NewDecoder(fb)
	for {
		err = bdec.Decode(&v)
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		if v == nil {
			continue
		}
		err = enc.Encode(&v)
		if err != nil {
			return err
		}
	}

	// remove a, b
	fa.Close()
	os.Remove(a)
	fb.Close()
	os.Remove(b)

	// and move our temporary file to a
	t.Close()
	return os.Rename(t.Name(), a)
}

// Attempt to open / create a cache file. Will move cache under `quarantineFolder`,
// inside `cPath`, if cache is already present in `cPath` and cannot be opened or parsed.
// Returns file handler to the cache file.
func openCache(cPath, quarantineFolder string, lgr log.IngestLogger) (*os.File, error) {
	c, err := os.OpenFile(cPath, CacheFlagPermissions, CacheFilePerm)
	if err != nil {
		lgr.Error("Failed to open cache file", log.KV("cache", cPath), log.KVErr(err))

		if errors.Is(err, os.ErrPermission) {
			return quarantineCache(cPath, quarantineFolder, lgr)
		}

		return nil, err
	}

	// Validate that the cache is readable / not corrupted
	if err = validateCache(c); err != nil {
		c.Close()

		lgr.Error("Cannot parse cache file", log.KV("cache", cPath), log.KVErr(err))

		return quarantineCache(cPath, quarantineFolder, lgr)
	}
	return c, nil
}

// Moves file in `cPath` to a `quarantineDir` folder.
// Creates a new file in `cPath` and returns handle on it.
// File moved to quarantineDir will follow naming convention:
// `{quarantineDir}/{cacheBaseName}.{1,2,3...}`
func quarantineCache(cPath, quarantineFolder string, lgr log.IngestLogger) (*os.File, error) {
	cDir := filepath.Dir(cPath)
	quarantineDir := filepath.Join(cDir, quarantineFolder)

	err := os.MkdirAll(quarantineDir, CacheDirPerm)
	if err != nil {
		lgr.Error("Failed to create quarantine dir", log.KV("quarantineDir", quarantineDir), log.KVErr(err))
		return nil, err
	}

	cName := filepath.Base(cPath)
	quarantineFilePathBase := filepath.Join(quarantineDir, cName)

	// Check if quarantine caches already exist
	qCaches, err := filepath.Glob(fmt.Sprintf("%s.*", quarantineFilePathBase))
	if err != nil {
		lgr.Error("Could not read quarantine directory", log.KV("quarantineDir", quarantineDir), log.KVErr(err))

		return nil, err
	}

	newCPath := getQuarantineCacheName(quarantineFilePathBase, qCaches)

	lgr.Error("Moving cache to quarantine file", log.KV("cache", cPath), log.KV("quarantineFile", newCPath))
	if err = os.Rename(cPath, newCPath); err != nil {
		lgr.Error("Failed to quarantine cache", log.KV("cache", cPath), log.KV("quarantineFile", newCPath), log.KVErr(err))
		return nil, err
	}

	res, err := os.OpenFile(cPath, CacheFlagPermissions, CacheFilePerm)
	if err != nil {
		lgr.Error("Failed to open new cache file", log.KV("cache", cPath), log.KVErr(err))
		return nil, err
	}

	return res, nil
}

func getQuarantineCacheName(quarantineFilePathBase string, matches []string) string {
	if len(matches) == 0 {
		return fmt.Sprintf("%s.1", quarantineFilePathBase)
	}

	var maxVal int
	for _, m := range matches {
		extRaw := filepath.Ext(m)
		val, err := strconv.Atoi(extRaw[1:])

		if err != nil {
			continue
		}

		maxVal = max(maxVal, val)
	}

	return fmt.Sprintf("%s.%d", quarantineFilePathBase, maxVal+1)
}

func validateCache(c *os.File) error {
	gdec := gob.NewDecoder(c)

	var err error
	var v any
	for {
		err = gdec.Decode(&v)
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
	}

	_, err = c.Seek(0, io.SeekStart)

	return err
}
