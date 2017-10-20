/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"errors"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/boltdb/bolt"

	"github.com/gravwell/ingest/entry"
)

const (
	defaultTickInterval = time.Second
	defaultCacheSize    = 1024 * 1024 * 4 //4MB
)

var (
	dbTimeout    time.Duration = 100 * time.Millisecond
	dbMmapSize   int           = defaultCacheSize
	dbOpenMode   os.FileMode   = 0660         //user and group R/W but nothing for other
	dbBucketName []byte        = []byte(`ic`) //only one bucket, so keep it simple

	ErrActiveHotBlocks        = errors.New("There are active hotblocks, close pitched data")
	ErrNoActiveDB             = errors.New("No active database")
	ErrNoKey                  = errors.New("No key set on entry block")
	ErrCacheAlreadyRunning    = errors.New("Cache already running")
	ErrCacheNotRunning        = errors.New("Cache is not running")
	ErrBucketMissing          = errors.New("Cache bucket is missing")
	ErrCannotSyncWhileRunning = errors.New("Cannot sync while running")
	ErrCannotPopWhileRunning  = errors.New("Cannot pop while running")
	ErrInvalidKey             = errors.New("Key bytes are invalid")
	ErrBoltLockFailed         = errors.New("Failed to acquire lock for ingest cache.  The file is locked by another process")
)

type IngestCacheConfig struct {
	FileBackingLocation string
	TickInterval        time.Duration
	MemoryCacheSize     uint64
}

type IngestCache struct {
	mtx          *sync.Mutex
	fileBacked   bool   //whether we are going to push to a file when there are no outputs available
	storeLoc     string //location of boltDB
	storedBlocks int
	count        uint64
	cacheSize    uint64
	maxCacheSize uint64
	hotBlocks    map[entry.EntryKey]*entry.EntryBlock
	currKey      entry.EntryKey
	currBlock    *entry.EntryBlock //just a pointer into the hotBlocks map value, NOT A COPY
	db           *bolt.DB
	err          error
	running      bool
	wg           sync.WaitGroup
	stCh         chan bool
}

// NewIngestCache creates a ingest cache and gets a handle on the store if specified.
// If the store can't be opened when asked for, we return an error
func NewIngestCache(c IngestCacheConfig) (*IngestCache, error) {
	var fileBacked bool
	var db *bolt.DB
	var blockCount int
	var count uint64
	if c.FileBackingLocation != `` {
		fileBacked = true
		//attempt to open the bolt database
		dbConfig := bolt.Options{
			InitialMmapSize: dbMmapSize,
			Timeout:         dbTimeout,
		}
		var err error
		db, err = bolt.Open(c.FileBackingLocation, dbOpenMode, &dbConfig)
		if err != nil {
			if err == bolt.ErrTimeout {
				return nil, ErrBoltLockFailed
			}
			return nil, err
		}
		//create our bucket in case it doesn't exist
		err = db.Update(func(t *bolt.Tx) error {
			if _, err := t.CreateBucketIfNotExists(dbBucketName); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			db.Close()
			return nil, err
		}
		blockCount = getKVCount(db)
		count, err = getEntryCount(db)
		if err != nil {
			db.Close()
			return nil, err
		}
	}
	//simple sanity check
	if db == nil && fileBacked {
		return nil, errors.New("Designated file backing did not generate storage handle")
	}

	if c.MemoryCacheSize <= 0 {
		c.MemoryCacheSize = defaultCacheSize
	}

	if c.TickInterval <= 0 {
		c.TickInterval = defaultTickInterval
	}

	//should be ready to go
	return &IngestCache{
		mtx:          &sync.Mutex{},
		fileBacked:   fileBacked,
		storeLoc:     c.FileBackingLocation,
		maxCacheSize: c.MemoryCacheSize,
		hotBlocks:    map[entry.EntryKey]*entry.EntryBlock{},
		db:           db,
		storedBlocks: blockCount,
		count:        count,
		stCh:         make(chan bool, 1),
	}, nil
}

// Close flushes hot blocks to the store and closes the cache
func (ic *IngestCache) Close() error {
	ic.mtx.Lock()
	defer ic.mtx.Unlock()
	if ic.db == nil {
		if len(ic.hotBlocks) != 0 {
			return ErrActiveHotBlocks
		}
		return nil //nothing to flush and no DB to flush to
	}
	if err := ic.flushHotBlocks(); err != nil {
		return err
	}
	if err := ic.db.Close(); err != nil {
		return err
	}
	return nil
}

// HotBlocks returns the number of blocks currently held in memory
func (ic *IngestCache) HotBlocks() int {
	return len(ic.hotBlocks)
}

// StoredBlocks returns the total number of blocks held in the store
func (ic *IngestCache) StoredBlocks() int {
	ic.mtx.Lock()
	defer ic.mtx.Unlock()
	return ic.storedBlocks
}

func getKVCount(db *bolt.DB) int {
	var blocks int
	//we can get the stats for the entire DB because there is only one bucket
	if err := db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(dbBucketName)
		if bkt == nil {
			return ErrBucketMissing
		}
		blocks = bkt.Stats().KeyN
		return nil
	}); err != nil {
		return 0
	}
	return blocks
}

func (ic *IngestCache) getStoredBlocks() int {
	if !ic.fileBacked || ic.db == nil {
		return 0
	}
	return getKVCount(ic.db)
}

// MemoryCacheSize returns how much is held in memory
func (ic *IngestCache) MemoryCacheSize() uint64 {
	return ic.cacheSize
}

//Count returns the number of entries held, including in the storage system
func (ic *IngestCache) Count() uint64 {
	return ic.count
}

// Sync flushes all hot blocks to the data store.  If we an in memory only cache
// then we throw an error
func (ic *IngestCache) Sync() error {
	ic.mtx.Lock()
	defer ic.mtx.Unlock()
	if ic.running {
		return ErrCannotSyncWhileRunning
	}
	return ic.flushHotBlocks() //ONLY call this externally when we aren't running
}

func (ic *IngestCache) Start(eChan chan *entry.Entry, bChan chan []*entry.Entry) error {
	ic.mtx.Lock()
	defer ic.mtx.Unlock()
	if ic.running {
		return ErrCacheAlreadyRunning
	}
	started := make(chan error)
	ic.wg.Add(1)
	go ic.routine(eChan, bChan, started)
	if err := <-started; err != nil {
		return err
	}
	ic.running = true
	return nil
}

func (ic *IngestCache) Stop() error {
	ic.mtx.Lock()
	if !ic.running {
		ic.mtx.Unlock()
		return ErrCacheNotRunning
	}
	ic.mtx.Unlock()
	//try to send the stop signal
	select {
	case ic.stCh <- true:
	default:
	}
	//wait for the closure
	ic.wg.Wait()
	return ic.err
}

func (ic *IngestCache) routine(echan chan *entry.Entry, bchan chan []*entry.Entry, started chan error) {
	defer ic.wg.Done()
	started <- nil

routineLoop:
	for {
		select {
		case ent, ok := <-echan:
			if !ok {
				break routineLoop
			}
			if ic.addEntry(ent) && ic.fileBacked {
				//we need to trim the cache to the store
				if err := ic.trimMemoryCache(); err != nil {
					ic.err = err
					break routineLoop
				}
			}
			ic.count++
		case set, ok := <-bchan:
			if !ok {
				break routineLoop
			}
			for _, e := range set {
				if e == nil {
					continue
				}
				if ic.addEntry(e) && ic.fileBacked {
					//we need to trim the cache to the store
					if err := ic.trimMemoryCache(); err != nil {
						ic.err = err
						break routineLoop
					}
				}
				ic.count++
			}
		case _ = <-ic.stCh:
			break routineLoop
		}
	}
	ic.running = false
}

// addEntry will add an entry to the cache and returns a boolean
// indicating whether we need to push some blocks to the store
func (ic *IngestCache) addEntry(ent *entry.Entry) bool {
	k := ent.Key()
	if k != ic.currKey {
		//see if we can find the block in the hotblock set
		blk, ok := ic.hotBlocks[k]
		if !ok {
			blk = &entry.EntryBlock{}
			ic.hotBlocks[k] = blk
		}
		ic.currBlock = blk
		ic.currKey = k
	}
	//at this point the currBlock points at the right key
	ic.currBlock.Add(ent)
	ic.cacheSize += ent.Size()
	if ic.cacheSize >= ic.maxCacheSize {
		return true
	}
	return false
}

// addBlock will add a slice of entries back into the cache
// this is just a convienence wrapper
func (ic *IngestCache) addBlock(blk []*entry.Entry) bool {
	var trimRequired bool
	for i := range blk {
		if blk[i] != nil {
			if ic.addEntry(blk[i]) {
				trimRequired = true
			}
		}
	}
	return trimRequired
}

func (ic *IngestCache) trimMemoryCache() error {
	for k, v := range ic.hotBlocks {
		if v == ic.currBlock && len(ic.hotBlocks) != 1 {
			continue //skip the hotblock
		}
		//grab the block and attempt to push it to the store
		if err := ic.pushBlock(k, v); err != nil {
			return err
		}
		delete(ic.hotBlocks, k)
		ic.cacheSize -= v.Size()
		if ic.cacheSize < ic.maxCacheSize {
			break
		}
	}
	if len(ic.hotBlocks) == 0 {
		ic.cacheSize = 0
		ic.currKey = 0
		ic.currBlock = nil
	}
	return nil
}

// flushHotBlocks pushes all our hot blocks to the store
func (ic *IngestCache) flushHotBlocks() error {
	if len(ic.hotBlocks) == 0 {
		//nothing to flush
		return nil
	}
	if ic.db == nil {
		//db isn't hot, can't flush
		return ErrNoActiveDB
	}

	//iterate over the hot blocks, pushing to DB
	for k, v := range ic.hotBlocks {
		if err := ic.pushBlock(k, v); err != nil {
			return err
		}
		ic.cacheSize -= v.Size()
		delete(ic.hotBlocks, k)
	}

	//there should be no hot blocks, remove current keys and block
	ic.currKey = 0
	ic.currBlock = nil
	ic.cacheSize = 0
	return nil
}

// getBlockBuff pulls a block, if the block does not exist we return nil
// an error is only returned on an actual DB error
func (ic *IngestCache) getBlockBuff(key []byte) ([]byte, error) {
	var buff []byte
	if err := ic.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(dbBucketName)
		if bkt == nil {
			return ErrBucketMissing
		}
		lBuff := bkt.Get(key)
		if lBuff == nil {
			return nil
		}
		buff = append([]byte(nil), lBuff...)
		return nil
	}); err != nil {
		return nil, err
	}
	return buff, nil
}

// pushBlock will attempt to pull the current block, merge the incoming block,
// and write it to the store
func (ic *IngestCache) pushBlock(key entry.EntryKey, blk *entry.EntryBlock) error {
	var newBlock bool
	//some basic sanity checks
	if blk == nil || blk.Size() == 0 {
		return nil //no reason to push anything
	}
	//check our key, if one isn't set, try to pull it from the block
	if key == 0 {
		if blk.Key() == 0 {
			return ErrNoKey
		}
		key = entry.EntryKey(blk.Key()) //set the key
	}

	dbKey := makeKey(key)
	buff, err := ic.getBlockBuff(dbKey)
	if err != nil {
		return err
	}
	if buff == nil {
		newBlock = true
	}
	//pull current block with this key from the store and append our current block
	buff, err = blk.EncodeAppend(buff)
	if err != nil {
		return err
	}
	if err := ic.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(dbBucketName)
		if bkt == nil {
			return ErrBucketMissing
		}
		return bkt.Put(dbKey, buff)
	}); err != nil {
		return err
	}
	if newBlock {
		ic.storedBlocks++
	}
	return nil
}

// PopBlock will pop any available block and hand it back
// the block is entirely removed from the cache
func (ic *IngestCache) PopBlock() (*entry.EntryBlock, error) {
	ic.mtx.Lock()
	defer ic.mtx.Unlock()
	if ic.running {
		return nil, ErrCannotPopWhileRunning
	}
	if !ic.fileBacked {
		return ic.popHotBlock(), nil
	}
	key, blk, err := ic.popStoreBlock()
	if err != nil {
		return nil, err
	}
	if key != 0 {
		blk = ic.popAndMergeHotBlock(key, blk)
	} else {
		blk = ic.popHotBlock()
	}
	if blk == nil {
		err = ic.compactDb()
	}
	return blk, err
}

// compactDb is a hacky method of "compacting" the boltDB
// this is clusterfuck bolt does not support compacting the file
// so when we hit zero, delete the damn file...
// its a hack, and I hate it... but it is what it is....
func (ic *IngestCache) compactDb() error {
	if err := ic.db.Sync(); err != nil {
		return err
	}
	if err := ic.db.Close(); err != nil {
		return err
	}
	if err := os.Remove(ic.storeLoc); err != nil {
		return err
	}
	dbConfig := bolt.Options{
		InitialMmapSize: dbMmapSize,
		Timeout:         dbTimeout,
	}

	db, err := bolt.Open(ic.storeLoc, dbOpenMode, &dbConfig)
	if err != nil {
		return err
	}
	//create our bucket in case it doesn't exist
	if err := db.Update(func(t *bolt.Tx) error {
		if _, err := t.CreateBucketIfNotExists(dbBucketName); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	ic.storedBlocks = 0
	ic.count = 0
	ic.db = db
	return nil
}

func (ic *IngestCache) popStoreBlock() (key entry.EntryKey, blk *entry.EntryBlock, err error) {
	var tblk entry.EntryBlock
	if err = ic.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(dbBucketName)
		if bkt == nil {
			return ErrBucketMissing
		}
		c := bkt.Cursor()
		for kb, vb := c.First(); kb != nil && vb != nil; kb, vb = c.Next() {
			c.Delete() //removing, one way or another
			key, err = getKey(kb)
			if err != nil {
				continue
			}
			//attempt to decode it
			if err := tblk.Decode(append([]byte(nil), vb...)); err != nil {
				continue
			}
			blk = &tblk
			break //got it
		}
		return nil
	}); err != nil {
		blk = nil
		return
	}
	return
}

func (ic *IngestCache) popAndMergeHotBlock(key entry.EntryKey, blk *entry.EntryBlock) *entry.EntryBlock {
	b, ok := ic.hotBlocks[key]
	if !ok {
		return blk
	}
	b.Merge(blk)
	return b
}

func (ic *IngestCache) popHotBlock() (blk *entry.EntryBlock) {
	for k, v := range ic.hotBlocks {
		if k == ic.currKey {
			if len(ic.hotBlocks) == 1 {
				ic.currKey = 0
				ic.currBlock = nil
			} else {
				//skip the current block if there are others
				continue
			}
		}
		delete(ic.hotBlocks, k)
		ic.count -= uint64(v.Count())
		return v
	}
	return nil //nothing to pop
}

type iterator func([]byte, []byte) error

func iterateEntries(db *bolt.DB, f iterator) error {
	return db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(dbBucketName)
		if bkt == nil {
			return ErrBucketMissing
		}
		return bkt.ForEach(f)
	})
}

func deleteKeys(db *bolt.DB, keys [][]byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(dbBucketName)
		if bkt == nil {
			return ErrBucketMissing
		}
		for _, k := range keys {
			if err := bkt.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

func getEntryCount(db *bolt.DB) (uint64, error) {
	var deleteList [][]byte
	var count uint64
	if err := iterateEntries(db, func(k, v []byte) error {
		var blk entry.EntryBlock
		if _, err := getKey(k); err != nil {
			//could not decode the key, just add to delete list and move on
			deleteList = append(deleteList, k)
			return nil
		}
		if err := blk.Decode(v); err != nil {
			deleteList = append(deleteList, k)
			return nil
		}
		count += uint64(blk.Count())
		return nil
	}); err != nil {
		return 0, err
	}
	if err := deleteKeys(db, deleteList); err != nil {
		return 0, err
	}
	return count, nil
}

func makeKey(k entry.EntryKey) (v []byte) {
	v = make([]byte, 8)
	*(*entry.EntryKey)(unsafe.Pointer(&v[0])) = k
	return
}

func getKey(v []byte) (k entry.EntryKey, err error) {
	if len(v) != 8 {
		err = ErrInvalidKey
		return
	}
	k = *(*entry.EntryKey)(unsafe.Pointer(&v[0]))
	return
}
