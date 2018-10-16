/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"bytes"
	"container/list"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gravwell/ingest/entry"
)

var (
	ErrAllConnsDown          = errors.New("All connections down")
	ErrNotRunning            = errors.New("Not running")
	ErrNotReady              = errors.New("Not ready to start")
	ErrTagNotFound           = errors.New("Tag not found")
	ErrTagMapInvalid         = errors.New("Tag map invalid")
	ErrNoTargets             = errors.New("No connections specified")
	ErrConnectionTimeout     = errors.New("Connection timeout")
	ErrSyncTimeout           = errors.New("Sync timeout")
	ErrEmptyAuth             = errors.New("Ingest key is empty")
	ErrEmergencyListOverflow = errors.New("Emergency list overflow")
	ErrTimeout               = errors.New("Timed out waiting for ingesters")
	ErrWriteTimeout          = errors.New("Timed out waiting to write entry")

	errNotImp = errors.New("Not implemented yet")
)

const (
	empty   muxState = 0
	running muxState = 1
	closed  muxState = 2

	defaultChannelSize   int           = 64
	defaultRetryTime     time.Duration = 10 * time.Second
	recycleTimeout       time.Duration = time.Second
	maxEmergencyListSize int           = 256
	unknownAddr          string        = `unknown`
)

type muxState int

type Target struct {
	Address string
	Secret  string
}

type TargetError struct {
	Address string
	Error   error
}

type IngestMuxer struct {
	//connHot, and connDead have atomic operations
	//its important that these are aligned on 8 byte boundries
	//or it will panic on 32bit architectures
	connHot         int32 //how many connections are functioning
	connDead        int32 //how many connections are dead
	mtx             *sync.RWMutex
	sig             *sync.Cond
	igst            []*IngestConnection
	tagTranslators  []*tagTrans
	dests           []Target
	errDest         []TargetError
	tags            []string
	tagMap          map[string]int
	pubKey          string
	privKey         string
	verifyCert      bool
	eChan           chan *entry.Entry
	bChan           chan []*entry.Entry
	eq              *emergencyQueue
	dieChan         chan bool
	upChan          chan bool
	errChan         chan error
	wg              *sync.WaitGroup
	state           muxState
	logLevel        gll
	cacheEnabled    bool
	cache           *IngestCache
	cacheWg         *sync.WaitGroup
	cacheFileBacked bool
	cacheRunning    bool
	cacheError      error
	cacheSignal     chan bool
	name            string
}

type UniformMuxerConfig struct {
	Destinations []string
	Tags         []string
	Auth         string
	PublicKey    string
	PrivateKey   string
	VerifyCert   bool
	ChannelSize  int
	EnableCache  bool
	CacheConfig  IngestCacheConfig
	LogLevel     string
	IngesterName string
}

type MuxerConfig struct {
	Destinations []Target
	Tags         []string
	PublicKey    string
	PrivateKey   string
	VerifyCert   bool
	ChannelSize  int
	EnableCache  bool
	CacheConfig  IngestCacheConfig
	LogLevel     string
	IngesterName string
}

func NewUniformMuxer(c UniformMuxerConfig) (*IngestMuxer, error) {
	return newUniformIngestMuxerEx(c)
}

func NewMuxer(c MuxerConfig) (*IngestMuxer, error) {
	return newIngestMuxer(c)
}

// NewIngestMuxer creates a new muxer that will automatically distribute entries amongst the clients
func NewUniformIngestMuxer(dests, tags []string, authString, pubKey, privKey, remoteKey string) (*IngestMuxer, error) {
	return NewUniformIngestMuxerExt(dests, tags, authString, pubKey, privKey, remoteKey, defaultChannelSize)
}

func NewUniformIngestMuxerExt(dests, tags []string, authString, pubKey, privKey, remoteKey string, chanSize int) (*IngestMuxer, error) {
	c := UniformMuxerConfig{
		Destinations: dests,
		Tags:         tags,
		Auth:         authString,
		PublicKey:    pubKey,
		PrivateKey:   privKey,
		ChannelSize:  chanSize,
	}
	return newUniformIngestMuxerEx(c)
}

func newUniformIngestMuxerEx(c UniformMuxerConfig) (*IngestMuxer, error) {
	if len(c.Auth) == 0 {
		return nil, ErrEmptyAuth
	}
	destinations := make([]Target, len(c.Destinations))
	for i := range c.Destinations {
		destinations[i].Address = c.Destinations[i]
		destinations[i].Secret = c.Auth
	}
	if len(destinations) == 0 {
		return nil, ErrNoTargets
	}
	cfg := MuxerConfig{
		Destinations: destinations,
		Tags:         c.Tags,
		PublicKey:    c.PublicKey,
		PrivateKey:   c.PrivateKey,
		VerifyCert:   c.VerifyCert,
		ChannelSize:  c.ChannelSize,
		EnableCache:  c.EnableCache,
		CacheConfig:  c.CacheConfig,
		LogLevel:     c.LogLevel,
		IngesterName: c.IngesterName,
	}
	return newIngestMuxer(cfg)
}

func NewIngestMuxer(dests []Target, tags []string, pubKey, privKey string) (*IngestMuxer, error) {
	return NewIngestMuxerExt(dests, tags, pubKey, privKey, defaultChannelSize)
}

func NewIngestMuxerExt(dests []Target, tags []string, pubKey, privKey string, chanSize int) (*IngestMuxer, error) {
	c := MuxerConfig{
		Destinations: dests,
		Tags:         tags,
		PublicKey:    pubKey,
		PrivateKey:   privKey,
		ChannelSize:  chanSize,
	}
	return newIngestMuxer(c)
}

func newIngestMuxer(c MuxerConfig) (*IngestMuxer, error) {
	localTags := make([]string, 0, len(c.Tags))
	for i := range c.Tags {
		localTags = append(localTags, c.Tags[i])
	}

	//generate our tag map, the tag map is used only for quick tag lookup/translation by routines
	tagMap := make(map[string]int, len(localTags))
	for i, v := range localTags {
		tagMap[v] = i
	}

	//if the cache is enabled, attempt to fire it up
	var cache *IngestCache
	var cacheSig chan bool
	var err error
	if c.EnableCache {
		cache, err = NewIngestCache(c.CacheConfig)
		if err != nil {
			return nil, err
		}
		cacheSig = make(chan bool, 1)
	}

	if c.ChannelSize <= 0 {
		c.ChannelSize = defaultChannelSize
	}

	return &IngestMuxer{
		dests:           c.Destinations,
		tags:            localTags,
		tagMap:          tagMap,
		pubKey:          c.PublicKey,
		privKey:         c.PrivateKey,
		verifyCert:      c.VerifyCert,
		mtx:             &sync.RWMutex{},
		wg:              &sync.WaitGroup{},
		state:           empty,
		logLevel:        logLevel(c.LogLevel),
		eChan:           make(chan *entry.Entry, c.ChannelSize),
		bChan:           make(chan []*entry.Entry, c.ChannelSize),
		eq:              newEmergencyQueue(),
		dieChan:         make(chan bool, len(c.Destinations)),
		upChan:          make(chan bool, 1),
		errChan:         make(chan error, len(c.Destinations)),
		cache:           cache,
		cacheEnabled:    c.EnableCache,
		cacheWg:         &sync.WaitGroup{},
		cacheFileBacked: c.CacheConfig.FileBackingLocation != ``,
		cacheSignal:     cacheSig,
		name:            c.IngesterName,
	}, nil
}

//Start starts the connection process. This will return immediately, and does
//not mean that connections are ready. Callers should call WaitForHot immediately after
//to wait for the connections to be ready.
func (im *IngestMuxer) Start() error {
	im.mtx.Lock()
	defer im.mtx.Unlock()
	if im.state != empty || len(im.igst) != 0 {
		return ErrNotReady
	}
	//fire up the cache if its in use
	if im.cacheEnabled {
		im.cacheWg.Add(1)
		im.cacheRunning = true
		go im.cacheRoutine()
	}

	//fire up the ingest routines
	im.igst = make([]*IngestConnection, len(im.dests))
	im.tagTranslators = make([]*tagTrans, len(im.dests))
	im.wg.Add(len(im.dests))
	im.connDead = int32(len(im.dests))
	for i := 0; i < len(im.dests); i++ {
		go im.connRoutine(i)
	}
	im.state = running
	return nil
}

// Close the connection
func (im *IngestMuxer) Close() error {
	// Inform the world that we're done.
	im.Info("Ingester %v exiting\n", im.name)
	im.Sync(time.Second)

	var ok bool
	//there is a chance that we are fully blocked with another async caller
	//writing to the channel, so we set the state to closed and check if we need to
	//discard some items from the channel
	if atomic.LoadInt32(&im.connHot) == 0 && !im.cacheRunning {
		//no connections are hot, and there is no cache
		//closeing is GOING to pitch entries, so... it is what it is...
		//clear the channels
	consumer:
		for {
			select {
			case _, ok = <-im.eChan:
				if !ok {
					break consumer
				}
			case _, ok = <-im.bChan:
				if !ok {
					break consumer
				}
			default:
				break consumer
			}
		}
	}

	im.mtx.Lock()
	if im.state == closed {
		im.mtx.Unlock()
		return nil
	}
	im.state = closed

	//just close the channel, that will be a permenent signal for everything to close
	close(im.dieChan)

	//we MUST unlock the mutex while we wait so that if a connection
	//goes into an errors state it can lock the mutex to adjust the errDest
	im.mtx.Unlock()

	//wait for everyone to quit
	im.wg.Wait()

	//if the cache is in use, signal for it to terminate and wait
	if im.cacheRunning && im.cacheSignal != nil {
		close(im.cacheSignal)
		im.cacheWg.Wait()
	}

	im.mtx.Lock()
	defer im.mtx.Unlock()

	//close the echan now that all the routines have closed
	close(im.eChan)
	close(im.bChan)

	//sync the cache and close it
	if im.cacheEnabled && im.cache != nil {
		if im.cacheFileBacked {
			// pull all outstanding items from each ingester connection and the channel
			// and shove them into the cache, then sync it
			for i := range im.igst {
				if im.igst[i] == nil {
					continue //skip nil ingesters, these SHOULDN'T be nil
				}
				ents := im.igst[i].outstandingEntries()
				for i := range ents {
					if ents[i] == nil {
						continue
					}
					im.cache.addEntry(ents[i])
				}
			}
			//clean out the entry channel too
			for e := range im.eChan {
				if e == nil {
					continue
				}
				im.cache.addEntry(e)
			}
			//clean out the entry block channel too
			for b := range im.bChan {
				if b == nil {
					continue
				}
				for _, e := range b {
					if e == nil {
						continue
					}
					im.cache.addEntry(e)
				}
			}

			//if we are file backed, sync the backing cache
			if err := im.cache.Sync(); err != nil {
				im.mtx.Unlock()
				return err
			}
		}
		if err := im.cache.Close(); err != nil {
			im.mtx.Unlock()
			return err
		}
	}

	//everyone is dead, clean up
	close(im.upChan)
	return nil
}

func (im *IngestMuxer) NegotiateTag(name string) (tg entry.EntryTag, err error) {
	im.mtx.Lock()
	defer im.mtx.Unlock()

	if err = CheckTag(name); err != nil {
		return
	}

	if tag, ok := im.tagMap[name]; ok {
		// tag already exists, just return it
		tg = entry.EntryTag(tag)
		return
	}

	// update the tag list and map
	im.tags = append(im.tags, name)
	for i, v := range im.tags {
		im.tagMap[v] = i
	}
	tg = entry.EntryTag(im.tagMap[name])

	for k, v := range im.igst {
		if v != nil {
			remoteTag, err := v.NegotiateTag(name)
			if err != nil {
				// something went wrong, kill it and let it re-initialize
				v.Close()
				continue
			}
			if im.tagTranslators[k] != nil {
				err = im.tagTranslators[k].RegisterTag(tg, remoteTag)
				if err != nil {
					v.Close()
				}
			} else {
				v.Close()
			}
		}
	}
	return
}

func (im *IngestMuxer) Sync(to time.Duration) error {
	if atomic.LoadInt32(&im.connHot) == 0 && !im.cacheRunning {
		return ErrAllConnsDown
	}
	ts := time.Now()
	im.mtx.Lock()
	for len(im.eChan) > 0 || len(im.bChan) > 0 {
		time.Sleep(10 * time.Millisecond)
		if im.connHot == 0 {
			im.mtx.Unlock()
			return ErrAllConnsDown
		}
		if time.Since(ts) > to {
			im.mtx.Unlock()
			return ErrTimeout
		}
	}

	var count int
	for _, v := range im.igst {
		if v != nil {
			if err := v.Sync(); err != nil {
				if err == ErrNotRunning {
					count++
				}
			}
		}
	}
	im.mtx.Unlock()
	if count == len(im.igst) {
		return ErrAllConnsDown
	}
	return nil
}

// WaitForHot waits until at least one connection goes into the hot state
// The timout duration parameter is an optional timeout, if zero, it waits
// indefinitely
func (im *IngestMuxer) WaitForHot(to time.Duration) error {
	var toch <-chan time.Time
	if to > 0 {
		tmr := time.NewTimer(to)
		toch = tmr.C
		defer tmr.Stop()
	}

	//wait for one of them to hit
mainLoop:
	for {
		select {
		case <-im.upChan:
			im.Info("Ingester %v has gone hot", im.name)
			break mainLoop
		case <-toch:
			//if we have a hot, filebacked cache, then endpoints are go for ingest
			if im.cacheRunning && im.cacheError == nil && im.cacheFileBacked {
				return nil
			}
			return ErrConnectionTimeout
		case err := <-im.errChan:
			//lock the mutex and check if all our connections failed
			im.mtx.Lock()
			if len(im.errDest) == len(im.dests) {
				im.mtx.Unlock()
				return errors.New("All connections failed " + err.Error())
			}
			im.mtx.Unlock()
			continue
		}
	}
	return nil //somone came up
}

// Hot returns how many connections are functioning
func (im *IngestMuxer) Hot() (int, error) {
	im.mtx.Lock()
	defer im.mtx.Unlock()
	if im.state != running {
		return -1, ErrNotRunning
	}
	return int(atomic.LoadInt32(&im.connHot)), nil
}

// unload cache will attempt to push out to the ingest connection
// the returned boolean indicates whether we were able to entirely unload the cache
// the cache MUST be stopped when we call this function
// we are potentially bypassing the channel and adding directly into it
func (im *IngestMuxer) unloadCache() (bool, error) {
	//attempt to pull all our entries from the cache and push them through the entry channel
	//this is used when a connection goes hot, we pull from our cache and drop them into channel
	//for the muxer to fire at indexers
	for {
		//pop a block and attempt to push into the ingest routine
		blk, err := im.cache.PopBlock()
		if err != nil {
			return false, err
		}
		if blk == nil {
			break //no more blocks
		}
		ents := blk.Entries()
		select {
		case im.bChan <- ents:
		case _, ok := <-im.cacheSignal:
			//push things back into the cache if we have zero connections or
			// the cacheSignal channel closed
			v := atomic.LoadInt32(&im.connHot)
			//if !ok || atomic.LoadInt32(&im.connHot) == 0 {
			if !ok || v == 0 {
				//push the block items back into the cache and bail
				im.cache.addBlock(ents)
				return false, nil //we need a transition
			}
		}
	}
	return true, nil
}

func (im *IngestMuxer) cacheRoutine() {
	defer im.cacheWg.Done()
	var cacheActive bool

	//when the cache is fired up, we ALWAYS start
	//that way we are garunteed to be able to consume entries
	if err := im.cache.Start(im.eChan, im.bChan); err != nil {
		im.cacheError = err
		im.cacheRunning = false
		return
	}
	cacheActive = true

mainLoop:
	for {
		if _, ok := <-im.cacheSignal; !ok {
			break mainLoop
		}
		//we have been signaled about a start or stop
		if atomic.LoadInt32(&im.connHot) > 0 {
			if cacheActive == true {
				//a connection just went hot, stop the cache and
				//attempt to dump entries out to the connection
				cacheActive = false
				if err := im.cache.Stop(); err != nil {
					im.cacheError = err
					break mainLoop
				}
				//attempt to unload the cache
				emptied, err := im.unloadCache()
				if err != nil {
					im.cacheError = err
					break mainLoop
				}
				if !emptied {
					//the cache couldn't empty due to ingesters disconnecting
					//fire it back up and continue our loop
					cacheActive = true
					if err := im.cache.Start(im.eChan, im.bChan); err != nil {
						im.cacheError = err
						break mainLoop
					}
				}
			}
			//we were not active and another ingester came online, do nothing
		} else {
			//no hot connections
			if cacheActive == false {
				//we just transitioned into no active ingest links
				//and the cache is not active, get it fired up and rolling
				cacheActive = true
				if err := im.cache.Start(im.eChan, im.bChan); err != nil {
					im.cacheError = err
					break mainLoop
				}
			}
		}
	}

	//check if we need to stop the cache on our way out
	if cacheActive {
		if err := im.cache.Stop(); err != nil {
			im.cacheError = err
		}
		cacheActive = false
	}
	im.cacheRunning = false
}

//goHot is a convienence function used by routines when they become active
func (im *IngestMuxer) goHot() {
	atomic.AddInt32(&im.connDead, -1)
	//attempt a single on going hot, but don't block
	//increment the hot counter
	if atomic.AddInt32(&im.connHot, 1) == 1 {
		im.stopCache()
	}
	select {
	case im.upChan <- true:
	default:
	}
}

func (im *IngestMuxer) startCache() {
	if im.cacheRunning {
		//try to tell the cache about the need to fire back up
		//if we can't send the signal, then the cache routine is busy.
		//this is fine, because the cache routine will test the hot count
		//in its loop and do the right thing
		select {
		case im.cacheSignal <- true: //true means an ingester stopped
		default:
		}

	}
}

func (im *IngestMuxer) stopCache() {
	if im.cacheRunning {
		//try to tell the cache about the stoppage
		//if we can't send the signal, then the cache routine is busy
		//this is fine, because the cache routine will test the hot count
		//in its loop and do the right thing
		select {
		case im.cacheSignal <- false: //false means an ingester started
		default:
		}
	}
}

//goDead is a convienence function used by routines when they become dead
func (im *IngestMuxer) goDead() {
	//increment the hot counter
	if atomic.AddInt32(&im.connHot, -1) == 0 {
		im.startCache()
	}
	atomic.AddInt32(&im.connDead, 1)
}

// Dead returns how many connections are currently dead
func (im *IngestMuxer) Dead() (int, error) {
	im.mtx.Lock()
	defer im.mtx.Unlock()
	if im.state != running {
		return -1, ErrNotRunning
	}
	return int(im.connDead), nil
}

// Size returns the total number of specified connections, hot or dead
func (im *IngestMuxer) Size() (int, error) {
	im.mtx.Lock()
	defer im.mtx.Unlock()
	if im.state != running {
		return -1, ErrNotRunning
	}
	return len(im.dests), nil
}

// GetTag pulls back an intermediary tag id
// the intermediary tag has NO RELATION to the backend servers tag mapping
// it is used to speed along tag mappings
func (im *IngestMuxer) GetTag(tag string) (entry.EntryTag, error) {
	tg, ok := im.tagMap[tag]
	if !ok {
		return 0, ErrTagNotFound
	}
	return entry.EntryTag(tg), nil
}

// WriteEntry puts an entry into the queue to be sent out by the first available
// entry writer routine, if all routines are dead, THIS WILL BLOCK once the
// channel fills up.  We figure this is a natural "wait" mechanism
func (im *IngestMuxer) WriteEntry(e *entry.Entry) error {
	if e == nil {
		return nil
	}
	im.mtx.RLock()
	defer im.mtx.RUnlock()
	if im.state != running {
		return ErrNotRunning
	}
	im.eChan <- e
	return nil
}

// WriteEntryAttempt attempts to put an entry into the queue to be sent out
// of the first available writer routine.  This write is opportunistic and contains
// a timeout.  It is therefor every expensive and shouldn't be used for normal writes
// The typical use case is via the gravwell_log calls
func (im *IngestMuxer) WriteEntryTimeout(e *entry.Entry, d time.Duration) (err error) {
	tmr := time.NewTimer(d)
	if e == nil {
		return
	}
	im.mtx.RLock()
	defer im.mtx.RUnlock()
	if im.state != running {
		err = ErrNotRunning
		return
	}
	select {
	case im.eChan <- e:
	case _ = <-tmr.C:
		err = ErrWriteTimeout
	}
	return
}

// WriteBatch puts a slice of entries into the queue to be sent out by the first
// available entry writer routine.  The entry writer routines will consume the
// entire slice, so extremely large slices will go to a single indexer.
func (im *IngestMuxer) WriteBatch(b []*entry.Entry) error {
	if len(b) == 0 {
		return nil
	}
	im.mtx.RLock()
	defer im.mtx.RUnlock()
	if im.state != running {
		return ErrNotRunning
	}
	im.bChan <- b
	return nil
}

// Write puts together the arguments to create an entry and writes it
// to the queue to be sent out by the first available
// entry writer routine, if all routines are dead, THIS WILL BLOCK once the
// channel fills up.  We figure this is a natural "wait" mechanism
func (im *IngestMuxer) Write(tm entry.Timestamp, tag entry.EntryTag, data []byte) error {
	e := &entry.Entry{
		Data: data,
		TS:   tm,
		Tag:  tag,
	}
	return im.WriteEntry(e)
}

//connFailed will put the destination in a failed state and inform the muxer
func (im *IngestMuxer) connFailed(dst string, err error) {
	im.mtx.Lock()
	defer im.mtx.Unlock()
	im.errDest = append(im.errDest, TargetError{
		Address: dst,
		Error:   err,
	})
	im.errChan <- err
}

type connSet struct {
	ig  *IngestConnection
	tt  *tagTrans
	dst string
	src net.IP
}

//keep attempting to get a new connection set that we can actually write to
func (im *IngestMuxer) getNewConnSet(csc chan connSet, connFailure chan bool, orig bool) (nc connSet, ok bool) {
	if !orig {
		//try to send, if we can't just roll on
		select {
		case connFailure <- true:
		default:
		}
	}
	for {
		if nc, ok = <-csc; !ok {
			return
		}
		//attempt to clear the emergency queue and throw at our new connection
		if !im.eq.clear(nc.ig, nc.tt) || nc.ig.Sync() != nil {
			//try to send, if we can't just roll on
			select {
			case connFailure <- true:
			default:
			}
			ok = false
			continue
		}
		//ok, we synced, pass things back
		if orig {
			im.Info("connected to %v", nc.dst)
		} else {
			im.Info("re-connected to %v", nc.dst)
		}
		break
	}
	return
}

func (im *IngestMuxer) writeRelayRoutine(csc chan connSet, connFailure chan bool) {
	tkr := time.NewTicker(time.Second)
	defer tkr.Stop()
	defer close(connFailure)

	//grab our first conn set
	var tnc connSet
	var nc connSet
	var ok bool
	var err error
	if nc, ok = im.getNewConnSet(csc, connFailure, true); !ok {
		return
	}

	eC := im.eChan
	bC := im.bChan

inputLoop:
	for {
		select {
		case e, ok := <-eC:
			if !ok {
				eC = nil
				if bC == nil {
					return
				}
				continue
			}
			if e == nil {
				continue
			}
			e.Tag = nc.tt.Translate(e.Tag)
			if len(e.SRC) == 0 {
				e.SRC = nc.src
			}
			//handle the entry
			if err = nc.ig.WriteEntry(e); err != nil {
				im.recycleEntries(e, nil, nc.tt)
				if nc, ok = im.getNewConnSet(csc, connFailure, false); !ok {
					break inputLoop
				}
			}
		case b, ok := <-bC:
			if !ok {
				bC = nil
				if eC == nil {
					return
				}
				continue
			}
			if b == nil {
				continue
			}
			for i := range b {
				if b[i] != nil {
					b[i].Tag = nc.tt.Translate(b[i].Tag)
					if len(b[i].SRC) == 0 {
						b[i].SRC = nc.src
					}
				}
			}
			if err = nc.ig.WriteBatchEntry(b); err != nil {
				im.recycleEntries(nil, b, nc.tt)
				if nc, ok = im.getNewConnSet(csc, connFailure, false); !ok {
					break inputLoop
				}
			}
		case tnc, ok = <-csc: //in case we get an unexpected new connection
			if !ok {
				nc.ig.Sync()
				nc.ig.Close()
				//attempt to sync with current ngst and then bail
				break inputLoop
			}
			nc = tnc //just an update
		case <-tkr.C:
			//periodically check the emergency queue and sync
			if !im.eq.clear(nc.ig, nc.tt) || nc.ig.Sync() != nil {
				if nc, ok = im.getNewConnSet(csc, connFailure, false); !ok {
					break inputLoop
				}
			}
		}
	}
}

//the routine that manages
func (im *IngestMuxer) connRoutine(igIdx int) {
	var src net.IP
	defer im.wg.Done()
	if igIdx >= len(im.igst) || igIdx >= len(im.dests) {
		//this SHOULD NEVER HAPPEN.  Bail
		im.connFailed(unknownAddr, errors.New("Invalid ingester index on muxer"))
		return
	}
	dst := im.dests[igIdx]
	if im.igst[igIdx] != nil {
		//this SHOULD NEVER HAPPEN.  Bail
		im.connFailed(dst.Address, errors.New("Ingester already populated for destination in muxer"))
		return
	}

	//attempt connection
	igst, tt, err := im.getConnection(dst)
	if err != nil {
		//add ourselves to the errDest list and exit
		im.connFailed(dst.Address, err)
		return
	}
	if igst == nil {
		im.connFailed(dst.Address, errors.New("Nil connection"))
		return
	}
	src, err = igst.Source()
	if err != nil {
		im.connFailed(dst.Address, err)
		return
	}
	im.igst[igIdx] = igst
	im.tagTranslators[igIdx] = &tt
	im.goHot()

	connErrNotif := make(chan bool, 1)
	ncc := make(chan connSet, 1)
	defer close(ncc)

	go im.writeRelayRoutine(ncc, connErrNotif)

	//send the first connection set
	ncc <- connSet{
		dst: dst.Address,
		src: src,
		ig:  igst,
		tt:  &tt,
	}

	//loop, trying to grab entries, or dying
	for {
		select {
		case _ = <-im.dieChan:
			igst.Close()
			im.goDead()
			im.connFailed(dst.Address, errors.New("Closed"))
			return //this will close the ncc, causing the relay routine to close

		case _, ok := <-connErrNotif:
			//then close our ingest connection
			//if it throws an error we don't care, and cant do anything about it
			igst.Close()
			if !ok {
				//this means that the relay function bailed, close the connection and bail
				return
			}
			im.goDead() //let the world know of our failures
			im.igst[igIdx] = nil
			im.tagTranslators[igIdx] = nil

			//pull any entrys out of the ingest connection and put them into the emergency queue
			ents := igst.outstandingEntries()
			im.recycleEntries(nil, ents, &tt)

			//attempt to get the connection rolling again
			igst, tt, err = im.getConnection(dst)
			if err != nil {
				im.connFailed(dst.Address, err)
				return //we are done
			}
			if igst == nil {
				im.connFailed(dst.Address, errors.New("Nil connection"))
				return
			}
			if err := im.Error("lost connection to %v", dst.Address); err != nil {
				igst.Close()
				continue //retry...
			}
			//get the source fired back up
			src, err = igst.Source()
			if err != nil {
				igst.Close()
				im.connFailed(dst.Address, err)
				return
			}

			im.igst[igIdx] = igst
			im.tagTranslators[igIdx] = &tt
			im.goHot()
			ncc <- connSet{
				dst: dst.Address,
				src: src,
				ig:  igst,
				tt:  &tt,
			}
		}
	}
}

//we don't want to fully block here, so we attempt to push back on the channel
//and listen for a die signal
func (im *IngestMuxer) recycleEntries(e *entry.Entry, ents []*entry.Entry, tt *tagTrans) {
	//reset the tags to the globally translatable set
	//this operation is expensive
	if len(ents) > 0 {
		for i := range ents {
			if ents[i] != nil {
				ents[i].Tag = tt.Reverse(ents[i].Tag)
			}
		}
	}

	//we wait for up to one second to push values onto feeder channels
	//if nothing eats them by then, we drop them into the emergency queue
	//and bail out
	tmr := time.NewTimer(recycleTimeout)
	defer tmr.Stop()

	// try the single entry
	if e != nil {
		e.Tag = tt.Reverse(e.Tag)
		select {
		case _ = <-tmr.C:
			if err := im.eq.push(e, ents); err != nil {
				//FIXME - throw a fit about this via some logging, aight?
				return
			}
			//timer expired, reset it in case we have a block too
			tmr.Reset(0)
		case im.eChan <- e:
		case _ = <-im.dieChan:
			return
		}
	}
	//try block entry
	if len(ents) > 0 {
		select {
		case _ = <-tmr.C:
			if err := im.eq.push(nil, ents); err != nil {
				//FIXME - throw a fit about this
				return
			}
		case im.bChan <- ents:
		case _ = <-im.dieChan:
			return
		}
	}
	return
}

//fatal connection errors is looking for errors which are non-recoverable
//Recoverable errors are related to timeouts, refused connections, and read errors
func isFatalConnError(err error) bool {
	if err == nil {
		return false
	}
	switch err {
	case ErrMalformedDestination:
		fallthrough
	case ErrInvalidConnectionType:
		fallthrough
	case ErrFailedAuthHashGen:
		fallthrough
	case ErrInvalidCerts:
		fallthrough
	case ErrForbiddenTag:
		fallthrough
	case ErrFailedParseLocalIP:
		fallthrough
	case ErrEmptyTag:
		return true
	}
	return false
}

func (im *IngestMuxer) getConnection(tgt Target) (ig *IngestConnection, tt tagTrans, err error) {
loop:
	for {
		//attempt a connection, timeouts are built in to the IngestConnection
		if ig, err = InitializeConnection(tgt.Address, tgt.Secret, im.tags, im.pubKey, im.privKey, im.verifyCert); err != nil {
			if isFatalConnError(err) {
				break loop
			}
			//non-fatal, sleep and continue
			select {
			case _ = <-time.After(defaultRetryTime):
			case _ = <-im.dieChan:
				//told to exit, just bail
				return nil, nil, errors.New("Muxer closing")
			}
			continue
		}
		//no error, attempt to do a tag translation
		//we have a good connection, build our tag map
		if tt, err = im.newTagTrans(ig); err != nil {
			ig.Close()
			ig = nil
			tt = nil
			return
		}
		break
	}
	return
}

func (im *IngestMuxer) newTagTrans(igst *IngestConnection) (tagTrans, error) {
	tt := tagTrans(make([]entry.EntryTag, len(im.tagMap)))
	if len(tt) == 0 {
		return nil, ErrTagMapInvalid
	}
	for k, v := range im.tagMap {
		if v > len(tt) {
			return nil, ErrTagMapInvalid
		}
		tg, ok := igst.GetTag(k)
		if !ok {
			return nil, ErrTagNotFound
		}
		tt[v] = tg
	}
	return tt, nil
}

// SourceIP is a convienence function used to pull back a source value
func (im *IngestMuxer) SourceIP() (net.IP, error) {
	var ip net.IP
	im.mtx.Lock()
	defer im.mtx.Unlock()
	if im.connHot == 0 || len(im.igst) == 0 {
		return ip, errors.New("No active connections")
	}
	var set bool
	var wasErr bool
	for _, ig := range im.igst {
		if ig == nil {
			continue
		}
		lip, err := ig.Source()
		if err != nil {
			wasErr = true
			continue
		}
		if bytes.Compare(lip, localSrc) == 0 {
			continue
		}
		ip = lip
		set = true
	}
	if set {
		return ip, nil
	}
	if !wasErr {
		//this means there were no errors, we just have a local connection
		//this can happen
		return localSrc, nil
	}
	//just straight up couldn't get it
	return ip, errors.New("Failed to get remote connection")
}

type emStruct struct {
	e    *entry.Entry
	ents []*entry.Entry
}

type emergencyQueue struct {
	mtx *sync.Mutex
	lst *list.List
}

func newEmergencyQueue() *emergencyQueue {
	return &emergencyQueue{
		mtx: &sync.Mutex{},
		lst: list.New(),
	}
}

// emergencyPush is a last ditch effort to store
// items into a list of entries or blocks.  This should only be invoked when
// we are under very heavy load and have no indexer connections.  As a result
// the channels are all full and we can't recycle entries back into the feeders
// we this ingest connection disconnects.  Instead we push into this queue
// when new ingest connections become active, they will always attempt to feed from
// this queue before going to the channels.  This is essentially a deadlock fix.
func (eq *emergencyQueue) push(e *entry.Entry, ents []*entry.Entry) error {
	if e == nil && len(ents) == 0 {
		return nil
	}
	ems := emStruct{
		e:    e,
		ents: ents,
	}
	eq.mtx.Lock()
	if eq.lst.Len() > maxEmergencyListSize {
		eq.mtx.Unlock()
		return ErrEmergencyListOverflow
	}
	eq.lst.PushBack(ems)
	eq.mtx.Unlock()
	return nil
}

// emergencyPop checks to see if there are any values on the emergency list
// waiting to be ingested.  New routines should go to this list FIRST
func (eq *emergencyQueue) pop() (e *entry.Entry, ents []*entry.Entry, ok bool) {
	var elm emStruct
	eq.mtx.Lock()
	defer eq.mtx.Unlock()
	if eq.lst.Len() == 0 {
		//nothing here, bail
		return
	}
	el := eq.lst.Front()
	if el == nil {
		return
	}
	eq.lst.Remove(el) //its valid, remove it
	elm, ok = el.Value.(emStruct)
	if !ok {
		//shit?  FIXME - THROW A FIT
		return
	}
	e = elm.e
	ents = elm.ents
	return
}

func (eq *emergencyQueue) clear(igst *IngestConnection, tt *tagTrans) (ok bool) {
	//iterate on the emergency queue attempting to write elements to the remote side
	for {
		e, blk, populated := eq.pop()
		if !populated {
			ok = true
			break
		}
		if e != nil {
			e.Tag = tt.Translate(e.Tag)
			if err := igst.WriteEntry(e); err != nil {
				//reset the tag
				e.Tag = tt.Reverse(e.Tag)

				//push the entries back into the queue
				if err := eq.push(e, blk); err != nil {
					//FIXME - log this?
				}

				//return our failure
				break
			}
			//all is good set e to nil in case we can't write the block
			e = nil
		}
		if len(blk) > 0 {
			//translate tags, SRC is always fixed up on pulling from the channel
			//so no need to check or set here
			for i := range blk {
				if blk[i] != nil {
					blk[i].Tag = tt.Translate(blk[i].Tag)
				}
			}
			if err := igst.WriteBatchEntry(blk); err != nil {
				//reverse the tags and push back into queue
				for i := range blk {
					if blk[i] != nil {
						blk[i].Tag = tt.Reverse(blk[i].Tag)
					}
				}
				if err := eq.push(e, blk); err != nil {
					//FIXME - log this?
				}
				break
			}
		}
	}
	return
}

type tagTrans []entry.EntryTag

// Translate translates a local tag to a remote tag.  Senders should not use this function
func (tt tagTrans) Translate(t entry.EntryTag) entry.EntryTag {
	//check if this is the gravwell and if soo, pass it on through
	if t == entry.GravwellTagId {
		return t
	}
	//if this is a tag we have not negotiated, set it to the first one we have
	//we are assuming that its an error, but we still want the entry
	if int(t) >= len(tt) {
		return tt[0]
	}
	return tt[t]
}

func (tt *tagTrans) RegisterTag(local entry.EntryTag, remote entry.EntryTag) error {
	if int(local) != len(*tt) {
		// this means the local tag numbers got out of sync and something is bad
		return errors.New("Cannot register tag, local tag out of sync with tag translator")
	}
	*tt = append(*tt, remote)
	return nil
}

// Reverse translates a remote tag back to a local tag
// this is ONLY used when a connection dies while holding unconfirmed entries
// this operation is stupid expensive, so... be gracious
func (tt tagTrans) Reverse(t entry.EntryTag) entry.EntryTag {
	//check if this is gravwell and if soo, pass it on through
	if t == entry.GravwellTagId {
		return t
	}
	for i := range tt {
		if tt[i] == t {
			return entry.EntryTag(i)
		}
	}
	return 0
}
