/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"bytes"
	"container/list"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/chancacher"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/version"
)

const (
	CacheModeAlways = `always`
	CacheModeFail   = `fail`
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
	ErrInvalidEntry          = errors.New("Invalid entry value")
	ErrTooManyTags           = errors.New("All tag IDs exhausted, too many tags")
	ErrUnknownTag            = errors.New("Invalid tag value")

	errNotImp = errors.New("Not implemented yet")
)

const (
	mb               = 1024 * 1024
	empty   muxState = 0
	running muxState = 1
	closed  muxState = 2

	// these are only used when there isn't a cache enabled
	defaultIngestChanDepth = 128
	maxIngestChanDepth     = 4096

	defaultRetryTime time.Duration = 10 * time.Second //how quickly we attempt to reconnect
	maxRetryTime     time.Duration = 5 * time.Minute  // maximum interval on reconnects after repeated failures
	recycleTimeout   time.Duration = time.Second
	unknownAddr      string        = `unknown`
	waitTickerDur    time.Duration = 50 * time.Millisecond

	ingesterStateUpdateInterval    = 30 * time.Second //if the only thing changing is general ingest we will update this often
	maxIngesterStateUpdateInterval = 5 * time.Minute  //throw an update this often no matter what

	connectionShutdownSyncTimeout     = 10 * time.Second
	connectionTimerSyncTimeout        = 60 * time.Second //sometimes disks are stupid, give indexers a long time
	connectionTimerSyncTimeoutBackoff = 60 * time.Second // if we couldn't flush within 60s give the indexer 60s to collect itself
)

var (
	ConstraintSpecials = []rune{'|', '{', '}', '(', ')', ';', '=', '<', '!', '>', '~', '%', '^', '&', '"', '.', ':', ',', '/'}
)

type muxState int

type Target struct {
	Address string
	Tenant  string
	Secret  string
}

type TargetError struct {
	Address string
	Error   error
}

type dittoBlock struct {
	ents []entry.Entry
	cb   func(error)
}

type IngestMuxer struct {
	cfg StreamConfiguration //stream configuration
	ctx context.Context
	cf  context.CancelFunc
	//connHot, and connDead have atomic operations
	//its important that these are aligned on 8 byte boundaries
	//or it will panic on 32bit architectures
	connHot              int32 //how many connections are functioning
	connDead             int32 //how many connections are dead
	mtx                  *sync.RWMutex
	sig                  *sync.Cond
	igst                 []*IngestConnection
	tagTranslators       []*tagTrans
	dests                []Target
	errDest              []TargetError
	tc                   tagMaskTracker
	tags                 []string
	tagMap               map[string]entry.EntryTag
	pubKey               string
	privKey              string
	verifyCert           bool
	eChan                chan interface{}
	eChanOut             chan interface{}
	bChan                chan interface{}
	bChanOut             chan interface{}
	dittoChan            chan dittoBlock
	eq                   *emergencyQueue
	writeBarrier         chan bool
	upChan               chan bool
	errChan              chan error
	wg                   *sync.WaitGroup
	state                muxState
	hostname             string
	appname              string
	lgr                  Logger
	cacheEnabled         bool
	cachePath            string
	cacheSize            int
	cache                *chancacher.ChanCacher
	bcache               *chancacher.ChanCacher
	cacheAlways          bool
	name                 string
	version              string
	uuid                 string
	rateParent           *parent
	logSourceOverride    net.IP
	ingesterState        IngesterState
	ingesterStateUpdated bool         //ingesterState has been updated (usually a child member)
	logbuff              *EntryBuffer // for holding logs until we can push them
	start                time.Time    // when the muxer was started
	attacher             *attach.Attacher
	attachActive         bool
	minVersion           uint16
}

type UniformMuxerConfig struct {
	config.IngestStreamConfig
	Destinations      []string
	Tags              []string
	Tenant            string
	Auth              string
	PublicKey         string
	PrivateKey        string
	VerifyCert        bool
	CacheDepth        int
	CachePath         string
	CacheSize         int
	CacheMode         string
	LogLevel          string // deprecated, no longer used
	Logger            Logger
	IngesterName      string
	IngesterVersion   string
	IngesterUUID      string
	IngesterLabel     string
	RateLimitBps      int64
	LogSourceOverride net.IP
	Attach            attach.AttachConfig
	MinVersion        uint16 // minimum API version of indexers
}

type MuxerConfig struct {
	config.IngestStreamConfig
	Destinations      []Target
	Tags              []string
	PublicKey         string
	PrivateKey        string
	VerifyCert        bool
	CacheDepth        int
	CachePath         string
	CacheSize         int
	CacheMode         string
	LogLevel          string // deprecated, no longer used
	Logger            Logger
	IngesterName      string
	IngesterVersion   string
	IngesterUUID      string
	IngesterLabel     string
	RateLimitBps      int64
	LogSourceOverride net.IP
	Attach            attach.AttachConfig
	MinVersion        uint16 // minimum API version of indexers
}

func NewUniformMuxer(c UniformMuxerConfig) (*IngestMuxer, error) {
	return newUniformIngestMuxerEx(c)
}

func NewMuxer(c MuxerConfig) (*IngestMuxer, error) {
	return newIngestMuxer(c)
}

// NewIngestMuxer creates a new muxer that will automatically distribute entries amongst the clients
func NewUniformIngestMuxer(dests, tags []string, authString, pubKey, privKey, remoteKey string) (*IngestMuxer, error) {
	return NewUniformIngestMuxerExt(dests, tags, authString, pubKey, privKey, remoteKey, config.CACHE_DEPTH_DEFAULT)
}

func NewUniformIngestMuxerExt(dests, tags []string, authString, pubKey, privKey, remoteKey string, cacheDepth int) (*IngestMuxer, error) {
	c := UniformMuxerConfig{
		Destinations: dests,
		Tags:         tags,
		Auth:         authString,
		PublicKey:    pubKey,
		PrivateKey:   privKey,
		CacheDepth:   cacheDepth,
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
		destinations[i].Tenant = c.Tenant
	}
	if len(destinations) == 0 {
		return nil, ErrNoTargets
	}
	if len(c.Tags) > int(entry.MaxTagId) {
		return nil, ErrTooManyTags
	}
	cfg := MuxerConfig{
		IngestStreamConfig: c.IngestStreamConfig,
		Destinations:       destinations,
		Tags:               c.Tags,
		PublicKey:          c.PublicKey,
		PrivateKey:         c.PrivateKey,
		VerifyCert:         c.VerifyCert,
		CachePath:          c.CachePath,
		CacheSize:          c.CacheSize,
		CacheMode:          c.CacheMode,
		CacheDepth:         c.CacheDepth,
		LogLevel:           c.LogLevel,
		IngesterName:       c.IngesterName,
		IngesterVersion:    c.IngesterVersion,
		IngesterUUID:       c.IngesterUUID,
		IngesterLabel:      c.IngesterLabel,
		RateLimitBps:       c.RateLimitBps,
		Logger:             c.Logger,
		LogSourceOverride:  c.LogSourceOverride,
		Attach:             c.Attach,
		MinVersion:         c.MinVersion,
	}
	return newIngestMuxer(cfg)
}

func NewIngestMuxer(dests []Target, tags []string, pubKey, privKey string) (*IngestMuxer, error) {
	return NewIngestMuxerExt(dests, tags, pubKey, privKey, config.CACHE_DEPTH_DEFAULT)
}

func NewIngestMuxerExt(dests []Target, tags []string, pubKey, privKey string, cacheDepth int) (*IngestMuxer, error) {
	c := MuxerConfig{
		Destinations: dests,
		Tags:         tags,
		PublicKey:    pubKey,
		PrivateKey:   privKey,
		CacheDepth:   cacheDepth,
	}

	return newIngestMuxer(c)
}

func newIngestMuxer(c MuxerConfig) (*IngestMuxer, error) {
	if len(c.Tags) > int(entry.MaxTagId) {
		return nil, ErrTooManyTags
	}
	localTags := make([]string, 0, len(c.Tags))
	for i := range c.Tags {
		if err := CheckTag(c.Tags[i]); err != nil {
			return nil, fmt.Errorf("Invalid tag %q %v", c.Tags[i], err)
		}
		localTags = append(localTags, c.Tags[i])
	}
	if c.Logger == nil {
		c.Logger = log.NewDiscardLogger()
	}

	// connect up the chancacher
	var cache *chancacher.ChanCacher
	var bcache *chancacher.ChanCacher
	var eIn, eOut, bIn, bOut chan interface{}

	var err error
	if c.CachePath != "" {
		cache, err = chancacher.NewChanCacher(c.CacheDepth, filepath.Join(c.CachePath, "e"), mb*c.CacheSize)
		if err != nil {
			return nil, err
		}
		bcache, err = chancacher.NewChanCacher(c.CacheDepth, filepath.Join(c.CachePath, "b"), mb*c.CacheSize)
		if err != nil {
			return nil, err
		}
		if c.CacheMode == CacheModeFail {
			cache.CacheStop()
			bcache.CacheStop()
		}
		eIn, eOut = cache.In, cache.Out
		bIn, bOut = bcache.In, bcache.Out
	} else {
		// no cache active, just plumb a channel all the way through
		depth := c.CacheDepth
		if depth <= 0 {
			depth = defaultIngestChanDepth
		} else if depth > maxIngestChanDepth {
			depth = maxIngestChanDepth
		}
		eChan := make(chan interface{}, depth)
		bChan := make(chan interface{}, depth)
		eIn = eChan
		eOut = eChan
		bIn = bChan
		bOut = bChan
	}

	id := uuid.Nil
	if c.IngesterUUID != `` {
		if id, err = uuid.Parse(c.IngesterUUID); err != nil {
			return nil, fmt.Errorf("failed to parse ingester UUID %w", err)
		}
	}
	atch, err := attach.NewAttacher(c.Attach, id)
	if err != nil {
		return nil, fmt.Errorf("failed to generate attacher %w", err)
	}

	// It's possible that the configuration, and therefore tag names and
	// order, changed between runs of a muxer, and there is data in a cache
	// that's recovering. The cache has tag IDs from the /previous/ run, so
	// we read the old translations (that were previously saved) and then
	// add our tags to them. If the old tag map doesn't exist, then it's
	// anyone's guess where those entries might end up. Those are the
	// breaks.
	tagMap := make(map[string]entry.EntryTag)
	if c.CachePath != "" {
		tagMap, err = readTagCache(c.CachePath)
		if err != nil {
			return nil, err
		}
	}

	var taglist []string

	// tag IDs can be all over the place, so we start from the largest tag
	// ID in the returned map + 1
	var tagNext entry.EntryTag
	if len(tagMap) != 0 {
		for k, v := range tagMap {
			taglist = append(taglist, k)
			if v > tagNext {
				tagNext = v
			}
		}
		tagNext++
	}

	// add any new tags to that list
	for _, v := range localTags {
		if _, ok := tagMap[v]; !ok {
			taglist = append(taglist, v)
			tagMap[v] = entry.EntryTag(tagNext)
			tagNext++
		}
	}

	if c.CachePath != "" {
		writeTagCache(tagMap, c.CachePath)
	}

	var p *parent
	if c.RateLimitBps > 0 {
		p = newParent(c.RateLimitBps, 0)
	}

	// figure out our hostname
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "(cannot determine hostname)"
	}

	// Initialize the state
	state := IngesterState{
		UUID:       c.IngesterUUID,
		Name:       c.IngesterName,
		Label:      c.IngesterLabel,
		Hostname:   hostname,
		Version:    c.IngesterVersion,
		CacheState: c.CacheMode,
		LastSeen:   time.Now(),
		Children:   make(map[string]IngesterState),
		Tags:       c.Tags,
	}

	var ci *CircularIndex
	if ci, err = NewCircularIndex(4096); err != nil {
		return nil, err
	}
	logbuff := &EntryBuffer{
		ci:   ci,
		buff: make([]entry.Entry, 4096),
	}

	var tc tagMaskTracker
	for _, v := range tagMap {
		tc.add(v)
	}

	ctx, cf := context.WithCancel(context.Background())

	return &IngestMuxer{
		cfg:               getStreamConfig(c.IngestStreamConfig),
		ctx:               ctx,
		cf:                cf,
		dests:             c.Destinations,
		tc:                tc,
		tags:              taglist,
		tagMap:            tagMap,
		pubKey:            c.PublicKey,
		privKey:           c.PrivateKey,
		verifyCert:        c.VerifyCert,
		mtx:               &sync.RWMutex{},
		wg:                &sync.WaitGroup{},
		state:             empty,
		lgr:               c.Logger,
		hostname:          c.Logger.Hostname(),
		appname:           c.Logger.Appname(),
		eChan:             eIn,
		eChanOut:          eOut,
		bChan:             bIn,
		bChanOut:          bOut,
		dittoChan:         make(chan dittoBlock), // synchronous as hell
		eq:                newEmergencyQueue(),
		writeBarrier:      make(chan bool),
		upChan:            make(chan bool, 1),
		errChan:           make(chan error, len(c.Destinations)),
		cache:             cache,
		bcache:            bcache,
		cacheEnabled:      c.CachePath != "",
		cacheSize:         mb * c.CacheSize,
		cachePath:         c.CachePath,
		cacheAlways:       strings.ToLower(c.CacheMode) == CacheModeAlways,
		name:              c.IngesterName,
		version:           c.IngesterVersion,
		uuid:              c.IngesterUUID,
		rateParent:        p,
		logSourceOverride: c.LogSourceOverride,
		ingesterState:     state,
		logbuff:           logbuff,
		attacher:          atch,
		attachActive:      atch.Active(),
		minVersion:        c.MinVersion,
	}, nil
}

func readTagCache(p string) (map[string]entry.EntryTag, error) {
	ret := make(map[string]entry.EntryTag)
	path := filepath.Join(p, "tagcache")
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		return ret, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to open tagcache: %w", err)
	}
	defer f.Close()

	dec := gob.NewDecoder(f)
	err = dec.Decode(&ret)
	if err != nil {
		return nil, fmt.Errorf("Could not decode tagcache: %w", err)
	}

	return ret, nil
}

func writeTagCache(t map[string]entry.EntryTag, p string) error {
	path := filepath.Join(p, "tagcache")

	var b bytes.Buffer

	enc := gob.NewEncoder(&b)
	err := enc.Encode(&t)
	if err != nil {
		return err
	}
	return atomicFileWrite(path, b.Bytes(), 0660)
}

// Start starts the connection process. This will return immediately, and does
// not mean that connections are ready. Callers should call WaitForHot immediately after
// to wait for the connections to be ready.
func (im *IngestMuxer) Start() error {
	im.mtx.Lock()
	defer im.mtx.Unlock()
	if im.state != empty || len(im.igst) != 0 {
		return ErrNotReady
	}
	//if we have a cache enabled in always mode, fire it up now
	if im.cacheEnabled && im.cacheAlways {
		im.cache.CacheStart()
		im.bcache.CacheStart()
	}

	//fire up the ingest routines
	im.igst = make([]*IngestConnection, len(im.dests))
	im.tagTranslators = make([]*tagTrans, len(im.dests))
	im.wg.Add(len(im.dests))
	im.connDead = int32(len(im.dests))
	for i := 0; i < len(im.dests); i++ {
		go im.connRoutine(i)
	}
	im.start = time.Now()
	im.state = running

	return nil
}

// Close the connection
func (im *IngestMuxer) Close() error {
	// Inform the world that we're done.
	im.Info("Ingester exiting", log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
	im.Sync(time.Second) // attempt to sync with a fast timeout, we don't really care about errors here

	im.mtx.Lock()
	if im.state == closed {
		im.mtx.Unlock()
		return nil
	}
	im.state = closed

	//just close the channel, this will immediately abort all pending writes and serve to block new ones
	close(im.writeBarrier)

	im.cf() // call our cancel function that will kick the main context and get writeRelay routines started shutting down

	//we MUST unlock the mutex while we wait so that if a connection
	//goes into an errors state it can lock the mutex to adjust the errDest
	im.mtx.Unlock()
	im.wg.Wait()

	im.mtx.Lock()
	defer im.mtx.Unlock()

	//drain the emergency queue into the channels IF the cache is enabled
	if im.cacheEnabled {
		//tell the cache that it needs to start pushing to disk
		//this is safe to call multiple times (in case ingestConnections already died)
		im.cache.CacheStart()
		im.bcache.CacheStart()

		//drain the emergency queue into the cache
		for im.eq.len() > 0 {
			if ent, block, ok := im.eq.pop(); ok {
				if ent != nil {
					im.eChan <- ent
				}
				if len(block) > 0 {
					im.bChan <- block
				}
			}
		}
	}

	//close inputs, signalling that we want everything to really really shutdown
	close(im.eChan)
	close(im.bChan)

	// commit any outstanding data to disk, if the backing path is enabled.
	if im.cacheEnabled {
		im.cache.Commit()
		im.bcache.Commit()
		// If BOTH caches are empty, we can delete the stored tag map
		if im.cache.Size() == 0 && im.bcache.Size() == 0 {
			path := filepath.Join(im.cachePath, "tagcache")
			os.Remove(path)
		}
	}

	//everyone is dead, clean up
	close(im.upChan)
	return nil
}

func (im *IngestMuxer) ingesterStateDirty() (dirty bool) {
	im.mtx.RLock()
	if len(im.ingesterState.Tags) != len(im.tags) {
		dirty = true
	} else if im.ingesterStateUpdated {
		dirty = true
	} else if im.cacheEnabled {
		sz := uint64(im.cache.Size()) + uint64(im.bcache.Size())
		if im.ingesterState.CacheSize != sz {
			dirty = true
		}
	}
	im.mtx.RUnlock()
	return
}

func (im *IngestMuxer) getIngesterState(lastPush time.Time, lastEntryCount uint64) (s IngesterState, shouldPush bool) {
	gap := time.Since(lastPush)
	//check if it has been long enough that we push no matter what or the state is dirty and we need push
	if gap > maxIngesterStateUpdateInterval || im.ingesterStateDirty() || (im.ingesterState.Entries != lastEntryCount && gap > ingesterStateUpdateInterval) {
		shouldPush = true
	} else {
		return //nothing new in the ingester state, just return
	}
	im.mtx.Lock()

	// update the cache stats real quick
	if im.cacheEnabled {
		im.ingesterState.CacheSize = uint64(im.cache.Size())
		im.ingesterState.CacheSize += uint64(im.bcache.Size())
	}
	im.ingesterState.Uptime = time.Since(im.start)
	im.ingesterState.Tags = im.tags

	// The ingesterState object is of type ingest.IngesterState which contains a map of children.
	// You must make a deep copy (which is what Copy does) if you are going to concurrently read and write it.
	// When pushing the ingester state we want to make a complete copy with the lock held and then do the potentially
	// long write operation WITHOUT the lock held.
	s = im.ingesterState.Copy()

	im.mtx.Unlock()

	return
}

// deprecated, no longer used, each ingest connection routine will throw its own state at its own pace
/*
func (im *IngestMuxer) stateReportRoutine() {
	var lastPush time.Time
	var lastEntryCount uint64
	for im.state == running {
		if s, shouldPush, err := im.getTrimmedState(lastPush, lastEntryCount); err != nil || !shouldPush {
			continue
		} else {
			for _, v := range im.igst {
				if v != nil {
					// we don't fuss over the return value
					v.SendIngesterState(s)
				}
			}
			//update our last push time and the number of entries at the time of our last push
			lastPush = time.Now()
			lastEntryCount = im.ingesterState.Entries
		}
		time.Sleep(ingesterStateUpdateInterval)
	}
}
*/

func (im *IngestMuxer) getTrimmedState(lastPush time.Time, lastEntryCount uint64) (s IngesterState, shouldPush bool, err error) {
	//check if we should push an ingester state out either due to max time duration or because it was updated
	if s, shouldPush = im.getIngesterState(lastPush, lastEntryCount); shouldPush {
		//SendIngesterState throws a full sync and then pushes a potentially very large
		//configuration block. DO NOT HOLD THE LOCK on the entire muxer when this is happening
		//or you will most likely starve the ingest muxer.
		var sz uint32
		if sz, err = s.EncodedSize(); err != nil {
			return
		} else if sz > maxIngestStateSize {
			ogSize := sz
			s.trimChildConfigs()
			if sz, err = s.EncodedSize(); err != nil {
				return
			} else if sz > maxIngestStateSize {
				s.trimChildren(64)
				if sz, err = s.EncodedSize(); err != nil {
					return
				} else if sz > maxIngestStateSize {
					//log an error stating that we could not make it work
					im.Error("Failed to send ingester state, too large",
						log.KV("original-size", ogSize), log.KV("post-trim-size", sz))
					err = fmt.Errorf("failed to send ingester state, too large: post trim %d > %d", sz, maxIngestStateSize)
				}
			}
		}
	}
	return
}

// returns true if a write to the muxer will block
func (im *IngestMuxer) WillBlock() bool {
	nHot, err := im.Hot()
	if err == ErrNotRunning {
		return true // we dead jim
	} else if nHot > 0 {
		return false //writer is alive
	} else if !im.cacheEnabled {
		return true // no writers alive and cache is not enabled
	}

	// cache is enabled here
	if im.cache.Size() >= im.cacheSize {
		return true
	} else if im.bcache.Size() >= im.cacheSize {
		return true
	}

	return false
}

func (im *IngestMuxer) SetRawConfiguration(obj interface{}) (err error) {
	if obj == nil {
		return
	}
	var msg []byte
	if msg, err = json.Marshal(obj); err != nil {
		return
	}
	im.mtx.Lock()
	im.ingesterState.Configuration = json.RawMessage(msg)
	im.ingesterStateUpdated = true
	im.mtx.Unlock()
	return
}

func (im *IngestMuxer) SetMetadata(obj interface{}) (err error) {
	if obj == nil {
		return
	}
	var msg []byte
	if msg, err = json.Marshal(obj); err != nil {
		return
	}
	im.mtx.Lock()
	im.ingesterState.Metadata = json.RawMessage(msg)
	im.ingesterStateUpdated = true
	im.mtx.Unlock()
	return
}

func (im *IngestMuxer) RegisterChild(k string, v IngesterState) {
	im.mtx.Lock()
	v.LastSeen = time.Now() // if its being registered, we want to update its state
	im.ingesterState.Children[k] = v
	im.ingesterStateUpdated = true
	im.mtx.Unlock()
}

func (im *IngestMuxer) UnregisterChild(k string) {
	im.mtx.Lock()
	delete(im.ingesterState.Children, k)
	im.ingesterStateUpdated = true
	im.mtx.Unlock()
}

// LookupTag will reverse a tag id into a name, this operation is more expensive than a straight lookup
// Users that expect to translate a tag repeatedly should maintain their own tag map
func (im *IngestMuxer) LookupTag(tg entry.EntryTag) (name string, ok bool) {
	im.mtx.RLock()
	for k, v := range im.tagMap {
		if v == tg {
			name = k
			ok = true
			break
		}
	}
	im.mtx.RUnlock()
	return
}

// KnownTags will return a string slice of tags that the muxer actively knows about
func (im *IngestMuxer) KnownTags() (tgs []string) {
	im.mtx.RLock()
	tgs = make([]string, 0, len(im.tagMap))
	for k := range im.tagMap {
		tgs = append(tgs, k)
	}
	im.mtx.RUnlock()
	return
}

// NegotiateTag will attempt to lookup a tag name in the negotiated set
// The the tag name has not already been negotiated, the muxer will contact
// each indexer and negotiate it.  This call can potentially block and fail
func (im *IngestMuxer) NegotiateTag(name string) (tg entry.EntryTag, err error) {
	if err = CheckTag(name); err != nil {
		return
	}

	im.mtx.Lock()
	defer im.mtx.Unlock()
	if len(im.tagMap) >= int(entry.MaxTagId) {
		err = ErrTooManyTags
		return
	}

	if tag, ok := im.tagMap[name]; ok {
		// tag already exists, just return it
		tg = tag
		return
	}

	// update the tag list and map
	im.tags = append(im.tags, name)
	im.ingesterState.Tags = im.tags
	im.ingesterStateUpdated = true

	var tagNext entry.EntryTag
	for _, v := range im.tagMap {
		if v > tagNext {
			tagNext = v
		}
	}
	tg = entry.EntryTag(tagNext + 1)
	im.tagMap[name] = tg
	im.tc.add(tg)

	// update the tag cache
	if im.cachePath != "" {
		writeTagCache(im.tagMap, im.cachePath)
	}

	for k, v := range im.igst {
		if v != nil {
			if im.tagTranslators[k] != nil {
				//check if this translator already knows about this tag
				if !im.tagTranslators[k].hasTag(tg) {
					if lerr := im.tagTranslators[k].registerTagForNegotiation(name, tg); lerr != nil {
						// on error set the return error
						err = lerr
						v.Close()
					}
				}
			} else {
				v.Close()
			}
		}
	}
	return
}

func (im *IngestMuxer) Sync(to time.Duration) error {
	return im.SyncContext(context.Background(), to)
}

func (im *IngestMuxer) SyncContext(ctx context.Context, to time.Duration) error {
	if atomic.LoadInt32(&im.connHot) == 0 && !im.cacheEnabled {
		return ErrAllConnsDown
	}
	ts := time.Now()
	im.mtx.Lock()
	defer im.mtx.Unlock()
	// always sleep for 10ms so that we give the chancacher a chance to pull from one and put it on the other
	// a SyncContext is ALWAYS going to sleep for at least 10ms, this is NOT a free operation
	// this sleep is crucial because we need the runtime to basically break out and schedule the chancacher
	// otherwise its super easy to be in a situation where that routine has pulled an entry off the input channel
	// and is holding while it waits to put it on the output channel while in passthrough mode
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
		if len(im.eChanOut) == 0 && len(im.bChanOut) == 0 && len(im.eChan) == 0 && len(im.bChan) == 0 {
			// all pipelines are empty
			break
		}
		if im.connHot == 0 {
			return ErrAllConnsDown
		}
		//only check for a timeout if to is greater than zero.  A zero value or negative value means no timeout
		if to > 0 && time.Since(ts) > to {
			return ErrTimeout
		}
	}

	timeLeft := to - time.Since(ts)
	if timeLeft <= 0 {
		return ErrTimeout
	}

	//check for the simple case of a single indexer
	if len(im.igst) == 1 {
		var err error
		if ig := im.igst[0]; ig != nil {
			err = ig.syncTimeout(timeLeft)
		} else {
			err = ErrAllConnsDown
		}
		return err
	}

	//DO NOT CLOSE the channel unless we get all of them back
	total := len(im.igst)
	ch := make(chan error, total)
	tmr := time.NewTimer(timeLeft + time.Second) //there will be some slop here
	defer tmr.Stop()

	// do a parallel sync
	var count int
	for _, v := range im.igst {
		go func(ig *IngestConnection, ech chan error) {
			if ig == nil {
				ech <- nil
			} else {
				ech <- ig.syncTimeout(timeLeft)
			}
		}(v, ch)
		count++
	}

	//now go read them all
	var good int
	var down int
	var timeout bool
	var lastErr error
loop:
	for count > 0 {
		select {
		case err := <-ch:
			count--
			if err == nil {
				good++
			} else {
				//this will merge errors and even return nil if we need to
				lastErr = mergeError(lastErr, err)
				down++
			}
		case <-tmr.C:
			timeout = true
			break loop
		}
	}
	if (good + down) == total {
		//every single routine responded, its safe to close the channel
		close(ch)
	}
	if good == total {
		return nil //all good
	} else if down == total {
		return ErrAllConnsDown
	} else if timeout {
		// if its a timeout, throw the constant timeout error so that the caller can detect it and do the right thing
		return ErrTimeout
	}
	return lastErr
}

// WaitForHot waits until at least one connection goes into the hot state
// The timeout duration parameter is an optional timeout, if zero, it waits
// indefinitely
func (im *IngestMuxer) WaitForHot(to time.Duration) error {
	return im.WaitForHotContext(context.Background(), to)
}

func (im *IngestMuxer) WaitForHotContext(ctx context.Context, to time.Duration) error {
	if cnt, err := im.Hot(); err != nil {
		return err
	} else if cnt > 0 {
		return nil
	}
	//if we have a cache enabled in always mode, just short circuit out
	if im.cacheEnabled && im.cacheAlways {
		return nil
	}

	//no connections are up, wait for them
	tckDur := waitTickerDur
	if to > 0 && to < tckDur {
		tckDur = to
	}
	tckr := time.NewTicker(tckDur)
	defer tckr.Stop()
	ts := time.Now()

	//wait for one of them to hit
mainLoop:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-im.upChan:
			im.Info("Ingester gone hot", log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
			break mainLoop
		case <-tckr.C:
			//check if connections are hot
			if cnt, err := im.Hot(); err != nil {
				return err
			} else if cnt > 0 {
				return nil //connection went hot
			}

			if to == 0 {
				continue // no timeout, wait forever
			} else if time.Since(ts) < to {
				//we haven't hit our timeout yet, just continue
				continue
			}

			return ErrConnectionTimeout
		case err := <-im.errChan:
			//lock the mutex and check if all our connections failed
			im.mtx.RLock()
			if len(im.errDest) == len(im.dests) {
				im.mtx.RUnlock()
				return errors.New("All connections failed " + err.Error())
			}
			im.mtx.RUnlock()
			continue
		}
	}
	return nil //someone came up
}

// Hot returns how many connections are functioning
func (im *IngestMuxer) Hot() (int, error) {
	im.mtx.RLock()
	defer im.mtx.RUnlock()
	if im.state != running {
		return -1, ErrNotRunning
	}
	return int(atomic.LoadInt32(&im.connHot)), nil
}

// goHot is a convenience function used by routines when they become active
func (im *IngestMuxer) goHot() {
	atomic.AddInt32(&im.connDead, -1)
	//attempt a single on going hot, but don't block
	//increment the hot counter
	if atomic.AddInt32(&im.connHot, 1) == 1 {
		// if the cache is enabled AND we are not in always cache mode stop things
		if im.cacheEnabled && !im.cacheAlways {
			im.cache.CacheStop()
			im.bcache.CacheStop()
		}
	}
	select {
	case im.upChan <- true:
	default:
	}
}

// goDead is a convenience function used by routines when they become dead
func (im *IngestMuxer) goDead() {
	//decrement the hot counter
	if atomic.AddInt32(&im.connHot, -1) == 0 {
		// if the cache is enabled AND we are not in always cache mode start things
		if im.cacheEnabled && !im.cacheAlways {
			im.cache.CacheStart()
			im.bcache.CacheStart()
		}
	}
	atomic.AddInt32(&im.connDead, 1)
}

// Dead returns how many connections are currently dead
func (im *IngestMuxer) Dead() (int, error) {
	im.mtx.RLock()
	defer im.mtx.RUnlock()
	if im.state != running {
		return -1, ErrNotRunning
	}
	return int(im.connDead), nil
}

// Size returns the total number of specified connections, hot or dead
func (im *IngestMuxer) Size() (int, error) {
	im.mtx.RLock()
	defer im.mtx.RUnlock()
	if im.state != running {
		return -1, ErrNotRunning
	}
	return len(im.dests), nil
}

// GetTag pulls back an intermediary tag id
// the intermediary tag has NO RELATION to the backend servers tag mapping
// it is used to speed along tag mappings
func (im *IngestMuxer) GetTag(tag string) (tg entry.EntryTag, err error) {
	var ok bool
	im.mtx.RLock()
	if tg, ok = im.tagMap[tag]; !ok {
		err = ErrTagNotFound
	}
	im.mtx.RUnlock()
	return
}

// WriteEntry puts an entry into the queue to be sent out by the first available
// entry writer routine, if all routines are dead, THIS WILL BLOCK once the
// channel fills up.  We figure this is a natural "wait" mechanism
func (im *IngestMuxer) WriteEntry(e *entry.Entry) error {
	if e == nil {
		return nil
	} else if len(e.Data) > MAX_ENTRY_SIZE {
		return ErrOversizedEntry
	} else if e.Tag != entry.GravwellTagId && !im.tc.has(e.Tag) {
		return ErrUnknownTag
	}
	if im.state != running {
		return ErrNotRunning
	}
	if im.attachActive {
		im.attacher.Attach(e)
	}
	select {
	case im.eChan <- e:
	case <-im.writeBarrier:
		return ErrNotRunning
	}
	im.ingesterState.Entries++
	im.ingesterState.Size += uint64(len(e.Data))
	return nil
}

// WriteEntryContext puts an entry into the queue to be sent out by the first available
// entry writer routine, if all routines are dead, THIS WILL BLOCK once the
// channel fills up.  We figure this is a natural "wait" mechanism
// if not using a context, use WriteEntry as it is faster due to the lack of a select
func (im *IngestMuxer) WriteEntryContext(ctx context.Context, e *entry.Entry) error {
	if e == nil {
		return nil
	} else if len(e.Data) > MAX_ENTRY_SIZE {
		return ErrOversizedEntry
	} else if e.Tag != entry.GravwellTagId && !im.tc.has(e.Tag) {
		return ErrUnknownTag
	}
	if im.state != running {
		return ErrNotRunning
	}
	if im.attachActive {
		im.attacher.Attach(e)
	}
	select {
	case im.eChan <- e:
		im.ingesterState.Entries++
		im.ingesterState.Size += uint64(len(e.Data))
	case <-ctx.Done():
		return ctx.Err()
	case <-im.writeBarrier:
		return ErrNotRunning
	}
	return nil
}

// WriteEntryTimeout attempts to put an entry into the queue to be sent out
// of the first available writer routine.  This write is opportunistic and contains
// a timeout.  It is therefor every expensive and shouldn't be used for normal writes
// The typical use case is via the gravwell_log calls
func (im *IngestMuxer) WriteEntryTimeout(e *entry.Entry, d time.Duration) (err error) {
	if e == nil {
		return
	} else if len(e.Data) > MAX_ENTRY_SIZE {
		return ErrOversizedEntry
	} else if e.Tag != entry.GravwellTagId && !im.tc.has(e.Tag) {
		return ErrUnknownTag
	}
	if im.state != running {
		return ErrNotRunning
	}
	if im.attachActive {
		im.attacher.Attach(e)
	}
	tmr := time.NewTimer(d)
	select {
	case im.eChan <- e:
		im.ingesterState.Entries++
		im.ingesterState.Size += uint64(len(e.Data))
	case _ = <-tmr.C:
		err = ErrWriteTimeout
	case <-im.writeBarrier:
		err = ErrNotRunning
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
	//scan the entries
	for i := range b {
		if b == nil {
			return ErrInvalidEntry
		} else if len(b[i].Data) > MAX_ENTRY_SIZE {
			return ErrOversizedEntry
		} else if b[i].Tag != entry.GravwellTagId && !im.tc.has(b[i].Tag) {
			return ErrUnknownTag
		}
	}
	im.mtx.RLock()
	runok := im.state == running
	im.mtx.RUnlock()
	if !runok {
		return ErrNotRunning
	}
	if im.attachActive {
		for _, e := range b {
			im.attacher.Attach(e)
		}
	}
	select {
	case im.bChan <- b:
	case <-im.writeBarrier:
		return ErrNotRunning
	}
	im.ingesterState.Entries += uint64(len(b))
	for i := range b {
		im.ingesterState.Size += uint64(len(b[i].Data))
	}
	return nil
}

// WriteBatchContext puts a slice of entries into the queue to be sent out by the first
// available entry writer routine.  The entry writer routines will consume the
// entire slice, so extremely large slices will go to a single indexer.
// if a cancellation context isn't needed, use WriteBatch
func (im *IngestMuxer) WriteBatchContext(ctx context.Context, b []*entry.Entry) error {
	if len(b) == 0 {
		return nil
	}
	//scan the entries
	for i := range b {
		if b == nil {
			return ErrInvalidEntry
		} else if len(b[i].Data) > MAX_ENTRY_SIZE {
			return ErrOversizedEntry
		} else if b[i].Tag != entry.GravwellTagId && !im.tc.has(b[i].Tag) {
			return ErrUnknownTag
		}
	}

	im.mtx.RLock()
	runok := im.state == running
	im.mtx.RUnlock()
	if !runok {
		return ErrNotRunning
	}

	if im.attachActive {
		for _, e := range b {
			im.attacher.Attach(e)
		}
	}
	select {
	case im.bChan <- b:
		im.ingesterState.Entries += uint64(len(b))
		for i := range b {
			im.ingesterState.Size += uint64(len(b[i].Data))
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-im.writeBarrier:
		return ErrNotRunning
	}
	return nil
}

// Write puts together the arguments to create an entry and writes it
// to the queue to be sent out by the first available
// entry writer routine, if all routines are dead, THIS WILL BLOCK once the
// channel fills up.  We figure this is a natural "wait" mechanism
func (im *IngestMuxer) Write(tm entry.Timestamp, tag entry.EntryTag, data []byte) error {
	if len(data) > MAX_ENTRY_SIZE {
		return ErrOversizedEntry
	} else if tag != entry.GravwellTagId && !im.tc.has(tag) {
		return ErrUnknownTag
	}
	e := &entry.Entry{
		Data: data,
		TS:   tm,
		Tag:  tag,
	}
	return im.WriteEntry(e)
}

// WriteContext puts together the arguments to create an entry and writes it
// to the queue to be sent out by the first available
// entry writer routine, if all routines are dead, THIS WILL BLOCK once the
// channel fills up.  We figure this is a natural "wait" mechanism
// if the context isn't needed use Write instead
func (im *IngestMuxer) WriteContext(ctx context.Context, tm entry.Timestamp, tag entry.EntryTag, data []byte) error {
	if len(data) > MAX_ENTRY_SIZE {
		return ErrOversizedEntry
	} else if tag != entry.GravwellTagId && !im.tc.has(tag) {
		return ErrUnknownTag
	}
	e := &entry.Entry{
		Data: data,
		TS:   tm,
		Tag:  tag,
	}
	return im.WriteEntryContext(ctx, e)
}

// DittoWriteContext does a Ditto write, which is specifically
// intended to duplicate blocks of entries from one indexer to one or
// more destination indexers. This function will not return until the
// recipient has indicated that the entries are written to disk.
func (im *IngestMuxer) DittoWriteContext(ctx context.Context, b []entry.Entry) error {
	var err error
	var wg sync.WaitGroup
	for i := range b {
		if b[i].Tag != entry.GravwellTagId && !im.tc.has(b[i].Tag) {
			return ErrUnknownTag
		}
	}
	cb := func(e error) {
		err = e
		wg.Done()
	}
	wg.Add(1)
	db := dittoBlock{
		ents: b,
		cb:   cb,
	}
	select {
	case im.dittoChan <- db:
		// Now wait for the callback to be called
		wg.Wait()
	case <-ctx.Done():
		return ctx.Err()
	case <-im.writeBarrier:
		return ErrNotRunning
	}
	// The callback will have set err
	return err
}

// connFailed will put the destination in a failed state and inform the muxer
func (im *IngestMuxer) connFailed(dst string, err error) {
	im.mtx.Lock()
	defer im.mtx.Unlock()
	im.errDest = append(im.errDest, TargetError{
		Address: dst,
		Error:   err,
	})
	im.errChan <- err
}

// keep attempting to get a new connection set that we can actually write to
func (im *IngestMuxer) getNewConnSet(csc chan connSet, connFailure chan bool, orig, shouldSleep bool) (nc connSet, ok bool) {
	if !orig {
		//try to send, if we can't just roll on
		select {
		case connFailure <- shouldSleep:
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
			case connFailure <- shouldSleep:
			default:
			}
			ok = false
			continue
		}
		//ok, we synced, pass things back
		if orig {
			im.Info("connected", log.KV("indexer", nc.dst), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
		} else {
			im.Info("re-connected", log.KV("indexer", nc.dst), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
		}
		break
	}
	return
}

func tickerInterval() time.Duration {
	//return a time between 1500 and 3000 milliseconds
	return time.Duration(1500+rand.Int63n(1500)) * time.Millisecond
}

func (im *IngestMuxer) shouldSched() (ok bool) {
	//if pipelines are empty, schedule ourselves so that we can get a better distribution of entries
	if x := len(im.igst); x == 1 {
		//only one connection, do not schedule ever
		return
	}
	//there is more than one connection
	if im.cacheEnabled {
		//check what the cache says
		ok = im.cache.BufferSize() == 0 && im.bcache.BufferSize() == 0
	} else {
		//no cache, so just check the channels
		ok = len(im.eChanOut) == 0 && len(im.bChanOut) == 0
	}
	return
}

func (im *IngestMuxer) writeRelayRoutine(csc chan connSet, connFailure chan bool) {
	tmr := time.NewTimer(tickerInterval())
	defer tmr.Stop()
	defer close(connFailure)

	//grab our first conn set
	var tnc connSet
	var nc connSet
	var ok bool
	var err error
	var ttag entry.EntryTag
	if nc, ok = im.getNewConnSet(csc, connFailure, true, false); !ok {
		return
	}

	eC := im.eChanOut
	bC := im.bChanOut
	dC := im.dittoChan // not cached

	var lastStatePushEntryCount uint64
	var lastStatePush time.Time

inputLoop:
	for {
		select {
		case _ = <-im.ctx.Done():
			//the caller will detect that we exited and will take care of getting outstanding entries
			/*
				if !im.cacheEnabled {
					//attempt to sync, if that completes without error then try to drain the channels
					//attempt to drain input channels
				}
			*/
			im.syncAndCloseConnection(nc)
			return
		case db, ok := <-dC:
			if !ok {
				dC = nil
				if eC == nil && bC == nil {
					return
				}
				continue
			}

			// translate tags
			for i := range db.ents {
				if ttag, err = nc.translateTag(db.ents[i].Tag); err != nil {
					// We're going to consider this a fatal error. This
					// is a ditto session, you need to know what tags are
					// in the block before you send it, and you better have those
					// negotiated and ready to rock.
					db.cb(fmt.Errorf("Block contained entry with unexpected tag, aborting %w", err))
					continue inputLoop
				}
				db.ents[i].Tag = ttag

				if len(db.ents[i].SRC) == 0 {
					db.ents[i].SRC = nc.src
				}
			}

			// If there is any error at all, we will kick it back up the chain for the original
			// caller to deal with. We aren't going to recycle & retry, because the whole point
			// is to be very deliberate about making sure things get to disk.
			if err = nc.ig.WriteDittoBlock(db.ents); err != nil {
				db.cb(err)
				im.syncAndCloseConnection(nc)
				if nc, ok = im.getNewConnSet(csc, connFailure, false, false); !ok {
					break inputLoop
				}
				continue inputLoop
			}
			// and fire the callback so it knows we're done
			db.cb(nil)

			// let somebody else have a turn
			runtime.Gosched()
		case ee, ok := <-eC:
			if !ok {
				eC = nil
				if bC == nil && dC == nil {
					return
				}
				continue
			}
			if ee == nil {
				continue
			}

			e := ee.(*entry.Entry)

			if ttag, err = nc.translateTag(e.Tag); err != nil {
				// If the ingest muxer has no idea what this tag is, drop it and notify
				if name, ok := im.LookupTag(e.Tag); !ok {
					//we have controls in the muxer to prevent this, this shouldn't actually be possible
					im.Error("Got entry tagged with completely unknown intermediate tag, dropping it",
						log.KV("tagvalue", e.Tag),
						log.KV("ingester", im.name),
						log.KV("ingesteruuid", im.uuid),
						log.KVErr(err),
					)
				} else {
					im.Info("Got entry with new tag, need to renegotiate connection",
						log.KV("tag", name),
						log.KV("tagvalue", e.Tag),
						log.KV("ingester", im.name),
						log.KV("ingesteruuid", im.uuid),
						log.KVErr(err),
					)
					// Could not translate, but it's a valid tag the muxer has seen before.
					// We need to push this to the equeue and reconnect
					// so we get the correct tag set.
					// DO NOT reverse translate, muxer knows about the tag
					im.recycleEntry(e)
				}
				im.syncAndCloseConnection(nc)
				if nc, ok = im.getNewConnSet(csc, connFailure, false, false); !ok {
					break inputLoop
				}
				continue inputLoop
			}
			e.Tag = ttag

			if len(e.SRC) == 0 {
				e.SRC = nc.src
			}
			if err = nc.ig.WriteEntry(e); err != nil {
				e.Tag = nc.tt.reverse(e.Tag)
				im.recycleEntry(e)
				im.syncAndCloseConnection(nc)
				if nc, ok = im.getNewConnSet(csc, connFailure, false, false); !ok {
					break inputLoop
				}
				continue inputLoop
			}
			//hack to get better distribution across connections in an muxer
			if im.shouldSched() {
				runtime.Gosched()
			}
		case bb, ok := <-bC:
			if !ok {
				bC = nil
				if eC == nil && dC == nil {
					return
				}
				continue
			}
			if bb == nil {
				continue
			}

			b := bb.([]*entry.Entry)
			for i := range b {
				if b[i] != nil {
					if ttag, err = nc.translateTag(b[i].Tag); err != nil {
						if name, ok := im.LookupTag(b[i].Tag); !ok {
							//we have controls in the muxer to prevent this, this shouldn't actually be possible
							im.Error("Got entry tagged with completely unknown intermediate tag, dropping it",
								log.KV("tagvalue", b[i].Tag),
								log.KV("ingester", im.name),
								log.KV("ingesteruuid", im.uuid),
								log.KVErr(err),
							)
							//discard this entry, this isn't real and there is no way to get here
							b[i] = nil //this is safe, we check for this everywhere
							// first, reverse anything we've translated already
							for j := 0; j < i; j++ {
								b[j].Tag = nc.tt.reverse(b[j].Tag)
							}
							im.recycleEntryBatch(b) //recycle and save what we can
						} else {
							im.Info("Got entry with new tag, need to renegotiate connection",
								log.KV("tag", name),
								log.KV("tagvalue", b[i].Tag),
								log.KV("ingester", im.name),
								log.KV("ingesteruuid", im.uuid),
								log.KVErr(err),
							)
							// Could not translate! We need to push this to the equeue and reconnect
							// so we get the correct tag set.

							// first, reverse anything we've translated already
							for j := 0; j < i; j++ {
								b[j].Tag = nc.tt.reverse(b[j].Tag)
							}
							im.recycleEntryBatch(b)
						}
						im.syncAndCloseConnection(nc)
						if nc, ok = im.getNewConnSet(csc, connFailure, false, false); !ok {
							break inputLoop
						}
						continue inputLoop
					}
					b[i].Tag = ttag

					if len(b[i].SRC) == 0 {
						b[i].SRC = nc.src
					}
				}
			}
			var n int
			if n, err = nc.ig.writeBatchEntry(b); err != nil {
				for i := n; i < len(b); i++ {
					b[i].Tag = nc.tt.reverse(b[i].Tag)
				}
				im.recycleEntryBatch(b[n:])
				im.syncAndCloseConnection(nc)
				if nc, ok = im.getNewConnSet(csc, connFailure, false, false); !ok {
					break inputLoop
				}
			}
			//hack to get better distribution across connections in an muxer
			if im.shouldSched() {
				runtime.Gosched()
			}
		case tnc, ok = <-csc: //in case we get an unexpected new connection
			//because this is unexpected
			//we need to take care of the outstanding entry extraction and cycling back into
			//the emergency queue ourselves
			im.syncAndCloseConnection(nc)
			if !ok {
				//this is basically a shutdown signal
				break inputLoop
			}
			nc = tnc //just an update
		case <-tmr.C:
			//first we sync to make sure that this connection is even alive
			if err := nc.ig.syncTimeout(connectionTimerSyncTimeout); err != nil {
				nc.ig.closeTimeout(closeTimeout)
				im.recycleConnection(nc)

				//if we bombed because a sync timed out, we need to sleep a bit and let the indexer collect itself
				//this could be because the indexer is getting smashed, or it could be because a disk failed and writes
				//are stalling.  Lots of reasons this could happen, absolutely none of them good.
				shouldSleep := err == context.DeadlineExceeded

				if nc, ok = im.getNewConnSet(csc, connFailure, false, shouldSleep); !ok {
					break inputLoop
				}
			}

			//then we potentially throw the state block
			if s, shouldPush, err := im.getTrimmedState(lastStatePush, lastStatePushEntryCount); err == nil && shouldPush {
				if err := nc.ig.SendIngesterState(s); err != nil {
					//this is failure, recycle entries and reset the connection
					im.syncAndCloseConnection(nc)
					if nc, ok = im.getNewConnSet(csc, connFailure, false, false); !ok {
						break inputLoop
					}
				} else {
					lastStatePush = time.Now()
					lastStatePushEntryCount = s.Entries
				}
			}

			//then we try to clear the emergency queue
			if !im.eq.clear(nc.ig, nc.tt) {
				//treat this as failure, sync and close the connection
				im.syncAndCloseConnection(nc)
				if nc, ok = im.getNewConnSet(csc, connFailure, false, false); !ok {
					break inputLoop
				}
			}

			tmr.Reset(tickerInterval())
		}
	}
}

func (im *IngestMuxer) syncAndCloseConnection(nc connSet) {
	nc.ig.syncTimeout(connectionShutdownSyncTimeout)
	nc.ig.Close()
	im.recycleConnection(nc)
}

func (im *IngestMuxer) recycleConnection(nc connSet) {
	ents := nc.ig.outstandingEntries()
	for i := range ents {
		if ents[i] != nil {
			ents[i].Tag = nc.tt.reverse(ents[i].Tag)
		}
	}
	im.recycleEntryBatch(ents)
	return
}

// connRoutine starts up the entry relay routine, then sits waiting to
// be notified about connection issues. Connection issues are handled
// by reconnecting to the indexers and recycling any outstanding
// entries back into the emergency queue, then sending the connection
// info to the entry relay routine for use.
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

	var igst *IngestConnection
	var tt *tagTrans
	var err error
	connErrNotif := make(chan bool, 1)
	ncc := make(chan connSet, 1)
	defer close(ncc)

	go im.writeRelayRoutine(ncc, connErrNotif)

	connErrNotif <- false // no sleep, get on it

	//loop, trying to grab entries, or dying
	for {
		shouldSleep, ok := <-connErrNotif
		//whether this is a bounce or a straight shutdown, close the ingest connection
		//grab all outstanding entries and shove to the emergency queue
		//if there is a cache enabled we will drop it into there when the muxer shuts down
		if igst != nil {
			igst.Close()
			im.goDead() //let the world know of our failures
			im.igst[igIdx] = nil
			im.tagTranslators[igIdx] = nil

			//pull any entries out of the ingest connection and put them into the emergency queue
			ents := igst.outstandingEntries()
			for i := range ents {
				if ents[i] != nil {
					ents[i].Tag = tt.reverse(ents[i].Tag)
				}
			}
			im.recycleEntryBatch(ents)
		}

		if !ok {
			// relay routine exited, just leave
			im.connFailed(dst.Address, errors.New("Closed"))
			return
		}
		if shouldSleep {
			im.quitableSleep(connectionTimerSyncTimeoutBackoff)
		}

		//attempt to get the connection rolling again
		im.Warn("reconnecting",
			log.KV("indexer", dst.Address),
			log.KV("ingester", im.name),
			log.KV("ingesteruuid", im.uuid))

		igst, tt, err = im.getConnection(dst)
		if err != nil {
			im.connFailed(dst.Address, err)
			return //we are done
		}
		if igst == nil {
			//nil connection is catastrophic, just leave
			im.connFailed(dst.Address, errors.New("Nil connection"))
			return
		}

		//get the source fired back up
		src, err = igst.Source()
		if err != nil {
			igst.Close()
			im.connFailed(dst.Address, err)
			return
		}

		im.mtx.Lock()
		im.igst[igIdx] = igst
		im.tagTranslators[igIdx] = tt
		im.mtx.Unlock()

		im.goHot()
		ncc <- connSet{
			dst: dst.Address,
			src: src,
			ig:  igst,
			tt:  tt,
		}
	}
}

func (im *IngestMuxer) recycleEntryBatch(ents []*entry.Entry) {
	if len(ents) == 0 {
		return
	}

	//we wait for up to one second to push values onto feeder channels
	//if nothing eats them by then, we drop them into the emergency queue
	//and bail out
	tmr := time.NewTimer(recycleTimeout)
	defer tmr.Stop()

	select {
	case _ = <-tmr.C:
		im.eq.push(nil, ents)
	case im.bChan <- ents:
	}
	return
}

func (im *IngestMuxer) recycleEntry(ent *entry.Entry) {
	if ent == nil {
		return
	} else if len(im.dests) == 1 || atomic.LoadInt32(&im.connHot) == 0 {
		// no one can help us, just shove it in
		im.eq.push(ent, nil)
		return
	}

	//we wait for up to one second to push values onto feeder channels
	//if nothing eats them by then, we drop them into the emergency queue
	//and bail out
	tmr := time.NewTimer(recycleTimeout)
	defer tmr.Stop()

	select {
	case _ = <-tmr.C:
		im.eq.push(ent, nil)
	case im.eChan <- ent:
	}
	return
}

// fatal connection errors is looking for errors which are non-recoverable
// Recoverable errors are related to timeouts, refused connections, and read errors
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
	case ErrTenantAuthUnsupported:
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

func (im *IngestMuxer) quitableSleep(dur time.Duration) (quit bool) {
	select {
	case _ = <-time.After(dur):
	case _ = <-im.ctx.Done():
		quit = true
	}
	return
}

func backoff(curr, max time.Duration) time.Duration {
	if curr <= 0 {
		return defaultRetryTime
	}
	if curr = curr * 2; curr > max {
		curr = max
	}
	return curr
}

func (im *IngestMuxer) getConnection(tgt Target) (ig *IngestConnection, tt *tagTrans, err error) {
	//initialize our retryDuration to zero, first call will set it to the default and then start backing off
	var retryDuration time.Duration
loop:
	for {
		//attempt a connection, timeouts are built in to the IngestConnection
		im.Info("initializing connection",
			log.KV("indexer", tgt.Address),
			log.KV("ingester", im.name),
			log.KV("version", version.GetVersion()),
			log.KV("ingesteruuid", im.uuid))
		im.mtx.RLock()
		if ig, err = initConnection(tgt, im.tags, im.pubKey, im.privKey, im.verifyCert, im.ctx); err != nil {
			im.mtx.RUnlock()
			if isFatalConnError(err) {
				im.Error("fatal connection error",
					log.KV("indexer", tgt.Address),
					log.KV("ingester", im.name),
					log.KV("version", version.GetVersion()),
					log.KV("ingesteruuid", im.uuid),
					log.KVErr(err))
				break loop
			}
			im.Warn("connection error",
				log.KV("indexer", tgt.Address),
				log.KV("ingester", im.name),
				log.KV("version", version.GetVersion()),
				log.KV("ingesteruuid", im.uuid),
				log.KVErr(err))
			//non-fatal, sleep and continue
			retryDuration = backoff(retryDuration, maxRetryTime)
			if im.quitableSleep(retryDuration) {
				//told to exit, just bail
				return nil, nil, errors.New("Muxer closing")
			}
			continue
		}
		// Make sure the version is new enough
		if ig.ew.serverVersion < im.minVersion {
			im.mtx.RUnlock()
			im.Warn("indexer server version is less than specified minimum API level, refusing to connect",
				log.KV("indexer", tgt.Address),
				log.KV("ingester", im.name),
				log.KV("version", version.GetVersion()),
				log.KV("ingesteruuid", im.uuid),
				log.KV("server-version", ig.ew.serverVersion),
				log.KV("min-version", im.minVersion))
			//non-fatal, sleep and continue
			retryDuration = backoff(retryDuration, maxRetryTime)
			if im.quitableSleep(retryDuration) {
				//told to exit, just bail
				return nil, nil, errors.New("Muxer closing")
			}
			continue
		}
		im.Info("connection established, completing negotiation and requesting approval to ingest",
			log.KV("indexer", tgt.Address),
			log.KV("ingester", im.name),
			log.KV("version", version.GetVersion()),
			log.KV("ingesteruuid", im.uuid))
		if im.rateParent != nil {
			ig.ew.setConn(im.rateParent.newThrottleConn(ig.ew.conn))
		}

		//no error, attempt to do a tag translation
		//we have a good connection, build our tag map
		if tt, err = im.newTagTrans(ig); err != nil {
			ig.Close()
			ig = nil
			tt = nil
			im.mtx.RUnlock()
			im.Error("fatal connection error, failed to get get tag translation map",
				log.KV("indexer", tgt.Address),
				log.KV("ingester", im.name),
				log.KV("version", version.GetVersion()),
				log.KV("ingesteruuid", im.uuid),
				log.KVErr(err))
			//non-fatal, sleep and continue
			retryDuration = backoff(retryDuration, maxRetryTime)
			if im.quitableSleep(retryDuration) {
				//told to exit, just bail
				return nil, nil, errors.New("Muxer closing")
			}
			continue
		}
		im.mtx.RUnlock()

		// set the info
		if lerr := ig.IdentifyIngester(im.name, im.version, im.uuid); lerr != nil {
			im.Error("Failed to identify ingester",
				log.KV("indexer", tgt.Address),
				log.KV("ingester", im.name),
				log.KV("version", version.GetVersion()),
				log.KV("ingesteruuid", im.uuid),
				log.KVErr(lerr))
			//non-fatal, sleep and continue
			retryDuration = backoff(retryDuration, maxRetryTime)
			if im.quitableSleep(retryDuration) {
				//told to exit, just bail
				return nil, nil, errors.New("Muxer closing")
			}
			continue
		}

		for {
			select {
			case _ = <-im.ctx.Done():
				return
			default:
			}
			ok, lerr := ig.IngestOK()
			if lerr != nil {
				im.Error("IngestOK query failed",
					log.KV("indexer", tgt.Address),
					log.KV("ingester", im.name),
					log.KV("version", version.GetVersion()),
					log.KV("ingesteruuid", im.uuid),
					log.KVErr(lerr))
				ig.Close()
				//non-fatal, sleep and continue
				retryDuration = backoff(retryDuration, maxRetryTime)
				if im.quitableSleep(retryDuration) {
					//told to exit, just bail
					return nil, nil, errors.New("Muxer closing")
				}
				continue loop
			}
			if ok {
				break
			}
			im.Warn("indexer does not yet allow ingest, sleeping",
				log.KV("indexer", tgt.Address),
				log.KV("ingester", im.name),
				log.KV("version", version.GetVersion()),
				log.KV("ingesteruuid", im.uuid))
			im.quitableSleep(10 * time.Second)
		}

		if lerr := ig.ew.ConfigureStream(im.cfg); lerr != nil {
			im.Warn("failed to configure stream",
				log.KV("indexer", tgt.Address),
				log.KV("ingester", im.name),
				log.KV("version", version.GetVersion()),
				log.KV("ingesteruuid", im.uuid),
				log.KVErr(lerr))
			ig.Close()
			//non-fatal, sleep and continue
			retryDuration = backoff(retryDuration, maxRetryTime)
			if im.quitableSleep(retryDuration) {
				//told to exit, just bail
				return nil, nil, errors.New("Muxer closing")
			}
			continue
		}

		im.Info("successfully connected with ingest OK",
			log.KV("indexer", tgt.Address),
			log.KV("ingester", im.name),
			log.KV("version", version.GetVersion()),
			log.KV("ingesteruuid", im.uuid))
		break
	}
	return
}

func (im *IngestMuxer) newTagTrans(igst *IngestConnection) (*tagTrans, error) {
	tt := &tagTrans{
		active: make([]entry.EntryTag, len(im.tagMap)),
	}
	if len(tt.active) == 0 {
		return nil, ErrTagMapInvalid
	}
	for k, v := range im.tagMap {
		if int(v) > len(tt.active) {
			return nil, ErrTagMapInvalid
		}
		tg, ok := igst.GetTag(k)
		if !ok {
			return nil, ErrTagNotFound
		}
		tt.active[v] = tg
	}
	return tt, nil
}

// SourceIP is a convenience function used to pull back a source value
func (im *IngestMuxer) SourceIP() (net.IP, error) {
	var ip net.IP
	im.mtx.RLock()
	defer im.mtx.RUnlock()
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
func (eq *emergencyQueue) push(e *entry.Entry, ents []*entry.Entry) {
	if e == nil && len(ents) == 0 {
		return
	}
	ems := emStruct{
		e:    e,
		ents: ents,
	}
	eq.mtx.Lock()
	eq.lst.PushBack(ems)
	eq.mtx.Unlock()
}

func (eq *emergencyQueue) len() (r int) {
	eq.mtx.Lock()
	r = eq.lst.Len()
	eq.mtx.Unlock()
	return
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
		//THROW A FIT!  This should not be possible
		return
	}
	e = elm.e
	ents = elm.ents
	return
}

func (eq *emergencyQueue) clear(igst *IngestConnection, tt *tagTrans) (ok bool) {
	//iterate on the emergency queue attempting to write elements to the remote side
	var ttag entry.EntryTag
	for {
		e, blk, populated := eq.pop()
		if !populated {
			ok = true
			break
		}
		if e != nil {
			ttag, ok = tt.translate(e.Tag)
			if !ok {
				// could not translate, push it back on the queue and bail
				eq.push(e, blk)
				return
			}
			e.Tag = ttag
			if err := igst.WriteEntry(e); err != nil {
				//reset the tag
				e.Tag = tt.reverse(e.Tag)

				//push the entries back into the queue
				eq.push(e, blk)

				//return our failure
				ok = false
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
					ttag, ok = tt.translate(blk[i].Tag)
					if !ok {
						// could not translate, push it back on the queue and bail
						// first we need to reverse the ones we have already translated, ugh
						for j := 0; j < i; j++ {
							blk[j].Tag = tt.reverse(blk[j].Tag)
						}
						eq.push(e, blk)
						return
					}
					blk[i].Tag = ttag
				}
			}
			if err := igst.WriteBatchEntry(blk); err != nil {
				//reverse the tags and push back into queue
				for i := range blk {
					if blk[i] != nil {
						blk[i].Tag = tt.reverse(blk[i].Tag)
					}
				}
				eq.push(e, blk)
				ok = false
				break
			}
		}
	}
	return
}

type connSet struct {
	ig  *IngestConnection
	tt  *tagTrans
	dst string
	src net.IP
}

func (nc connSet) translateTag(t entry.EntryTag) (rt entry.EntryTag, err error) {
	var ok bool
	if rt, ok = nc.tt.translate(t); ok {
		return
	}

	if len(nc.tt.toNegotiate) == 0 {
		err = ErrUnknownTag
		return
	}

	//ok, go negotiate all the tags, but grab a local copy to avoid races
	toNeg := nc.tt.toNegotiate
	for _, v := range toNeg {
		if rt, err = nc.ig.NegotiateTag(v.name); err != nil {
			return
		} else if err = nc.tt.registerTag(v.local, rt); err != nil {
			return
		}
		nc.tt.clearToNegotiate(1)
	}
	// all tags negotiated, try to translate again
	if rt, ok = nc.tt.translate(t); !ok {
		err = ErrUnknownTag
	}

	return
}

type unNegotiatedTag struct {
	local entry.EntryTag
	name  string
}

type tagTrans struct {
	sync.Mutex
	toNegotiate []unNegotiatedTag
	active      []entry.EntryTag
}

// Translate translates a local tag to a remote tag.  Senders should not use this function
func (tt *tagTrans) translate(t entry.EntryTag) (entry.EntryTag, bool) {
	//check if this is the gravwell and if soo, pass it on through
	if t == entry.GravwellTagId {
		return t, true
	}
	//if this is a tag we have not negotiated, set it to the first one we have
	//we are assuming that its an error, but we still want the entry, so send it to the default well
	if int(t) >= len(tt.active) {
		return 0, false //fire it at the default tag constant
	}
	return tt.active[t], true
}

func (tt *tagTrans) hasTag(t entry.EntryTag) bool {
	if t == entry.GravwellTagId {
		return true
	} else if int(t) < len(tt.active) {
		return true
	}
	return false
}

func (tt *tagTrans) registerTag(local entry.EntryTag, remote entry.EntryTag) error {
	if int(local) != len(tt.active) {
		// this means the local tag numbers got out of sync and something is bad
		return errors.New("Cannot register tag, local tag out of sync with tag translator")
	}

	//check if we have exhausted the number of tags
	if len(tt.active) >= int(entry.MaxTagId) {
		return ErrTooManyTags
	}

	// lock our tag set for the update
	tt.Lock()
	//registering a new tag
	tt.active = append(tt.active, remote)
	tt.Unlock()
	return nil
}

func (tt *tagTrans) clearToNegotiate(cnt int) {
	if cnt <= 0 {
		return
	}
	tt.Lock()
	if cnt < len(tt.toNegotiate) {
		// someone registered something while we were negotiating, so just chop off what we know about
		tt.toNegotiate = tt.toNegotiate[cnt:]
	} else {
		tt.toNegotiate = nil
	}
	tt.Unlock()
}

func (tt *tagTrans) registerTagForNegotiation(name string, local entry.EntryTag) error {
	if err := CheckTag(name); err != nil {
		return err
	} else if len(tt.active) >= int(entry.MaxTagId) {
		return ErrTooManyTags
	}
	tt.Lock()
	tt.toNegotiate = append(tt.toNegotiate, unNegotiatedTag{
		name:  name,
		local: local,
	})
	tt.Unlock()
	return nil
}

// Reverse translates a remote tag back to a local tag
// this is ONLY used when a connection dies while holding unconfirmed entries
// this operation is stupid expensive, so... be gracious
func (tt *tagTrans) reverse(t entry.EntryTag) entry.EntryTag {
	//check if this is gravwell and if soo, pass it on through
	if t == entry.GravwellTagId {
		return t
	}
	for i := range tt.active {
		if tt.active[i] == t {
			return entry.EntryTag(i)
		}
	}
	return 0
}

func getStreamConfig(cfg config.IngestStreamConfig) (sc StreamConfiguration) {
	if cfg.Enable_Compression {
		sc.Compression = CompressSnappy
	}
	return
}
