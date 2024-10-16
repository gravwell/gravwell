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

	errNotImp = errors.New("Not implemented yet")
)

const (
	mb               = 1024 * 1024
	empty   muxState = 0
	running muxState = 1
	closed  muxState = 2

	defaultRetryTime     time.Duration = 10 * time.Second //how quickly we attempt to reconnect
	maxRetryTime         time.Duration = 5 * time.Minute  // maximum interval on reconnects after repeated failures
	recycleTimeout       time.Duration = time.Second
	maxEmergencyListSize int           = 64
	unknownAddr          string        = `unknown`
	waitTickerDur        time.Duration = 50 * time.Millisecond

	ingesterStateUpdateInterval    = 10 * time.Second
	maxIngesterStateUpdateInterval = 5 * time.Minute
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

type IngestMuxer struct {
	cfg StreamConfiguration //stream configuration
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
	tags                 []string
	tagMap               map[string]entry.EntryTag
	pubKey               string
	privKey              string
	verifyCert           bool
	eChan                chan interface{}
	eChanOut             chan interface{}
	bChan                chan interface{}
	bChanOut             chan interface{}
	eq                   *emergencyQueue
	dieChan              chan bool
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
	} else {
		cache, err = chancacher.NewChanCacher(c.CacheDepth, "", 0)
		if err != nil {
			return nil, err
		}
		bcache, err = chancacher.NewChanCacher(c.CacheDepth, "", 0)
		if err != nil {
			return nil, err
		}
	}

	if c.CacheMode == CacheModeFail {
		cache.CacheStop()
		bcache.CacheStop()
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
	var taglist []string
	tagMap := make(map[string]entry.EntryTag)
	if c.CachePath != "" {
		tagMap, err = readTagCache(c.CachePath)
		if err != nil {
			return nil, err
		}
	}

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

	return &IngestMuxer{
		cfg:               getStreamConfig(c.IngestStreamConfig),
		dests:             c.Destinations,
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
		eChan:             cache.In,
		eChanOut:          cache.Out,
		bChan:             bcache.In,
		bChanOut:          bcache.Out,
		eq:                newEmergencyQueue(),
		dieChan:           make(chan bool, len(c.Destinations)),
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
	// start the state report goroutine
	go im.stateReportRoutine()

	return nil
}

// Close the connection
func (im *IngestMuxer) Close() error {
	// Inform the world that we're done.
	im.Info("Ingester exiting", log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
	time.Sleep(500 * time.Millisecond)

	im.mtx.Lock()
	if im.state == closed {
		im.mtx.Unlock()
		return nil
	}
	im.state = closed

	//just close the channel, that will be a permanent signal for everything to close
	close(im.dieChan)

	//we MUST unlock the mutex while we wait so that if a connection
	//goes into an errors state it can lock the mutex to adjust the errDest
	im.mtx.Unlock()

	//wait for everyone to quit
	im.wg.Wait()

	im.mtx.Lock()
	defer im.mtx.Unlock()

	close(im.eChan)
	close(im.bChan)

	// commit any outstanding data to disk, if the backing path is enabled.
	im.cache.Commit()
	im.bcache.Commit()

	// If BOTH caches are empty, we can delete the stored tag map
	if im.cacheEnabled && im.cache.Size() == 0 && im.bcache.Size() == 0 {
		path := filepath.Join(im.cachePath, "tagcache")
		os.Remove(path)
	}

	//everyone is dead, clean up
	close(im.upChan)
	return nil
}

func (im *IngestMuxer) ingesterStateDirty() (dirty bool) {
	im.mtx.RLock()
	if im.ingesterState.CacheSize != uint64(im.cache.Size()) {
		dirty = true
	} else if len(im.ingesterState.Tags) != len(im.tags) {
		dirty = true
	} else if im.ingesterStateUpdated {
		dirty = true
	}
	im.mtx.RUnlock()
	return
}

func (im *IngestMuxer) getIngesterState(lastPush time.Time, lastEntryCount uint64) (s IngesterState, shouldPush bool) {
	//check if it has been long enough that we push no matter what or the state is dirty and we need push
	if time.Since(lastPush) > maxIngesterStateUpdateInterval || im.ingesterStateDirty() || im.ingesterState.Entries != lastEntryCount {
		shouldPush = true
	} else {
		return //nothing new in the ingester state, just return
	}
	im.mtx.Lock()

	// update the cache stats real quick
	im.ingesterState.CacheSize = uint64(im.cache.Size())
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

func (im *IngestMuxer) stateReportRoutine() {
	var lastPush time.Time
	var lastEntryCount uint64
	for im.state == running {
		//check if we should push an ingester state out either due to max time duration or because it was updated
		if s, shouldPush := im.getIngesterState(lastPush, lastEntryCount); shouldPush {
			//SendIngesterState throws a full sync and then pushes a potentially very large
			//configuration block. DO NOT HOLD THE LOCK on the entire muxer when this is happening
			//or you will most likely starve the ingest muxer.

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

// returns true if a write to the muxer will block
func (im *IngestMuxer) WillBlock() bool {
	nHot, err := im.Hot()
	if err == ErrNotRunning {
		return true
	} else if nHot > 0 {
		return false
	}

	if !im.cacheEnabled {
		return true
	} else if im.cache.Size() >= im.cacheSize {
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
	im.tagMap[name] = entry.EntryTag(tagNext + 1)

	tg = im.tagMap[name]

	// update the tag cache
	if im.cachePath != "" {
		writeTagCache(im.tagMap, im.cachePath)
	}

	for k, v := range im.igst {
		if v != nil {
			remoteTag, err := v.NegotiateTag(name)
			if err != nil {
				if err == ErrNotRunning {
					// This is basically a
					// non-issue, we'll just make
					// sure the connection is
					// closed and when it comes
					// back automatically, the new
					// tag will be included in the
					// initialization.
					im.Info("NegotiateTag called on non-running ingest connection, skipping",
						log.KV("indexer", v.conn.RemoteAddr()),
						log.KV("tag", name), log.KV("error", err),
						log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
				} else {
					// Some other error... we'll
					// log at a higher level, then
					// again just close the conna
					// nd move on.
					im.Warn("NegotiateTag was unsuccessful, reconnecting",
						log.KV("indexer", v.conn.RemoteAddr()),
						log.KV("tag", name), log.KV("error", err),
						log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))

				}
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
	return im.SyncContext(context.Background(), to)
}

func (im *IngestMuxer) SyncContext(ctx context.Context, to time.Duration) error {
	if atomic.LoadInt32(&im.connHot) == 0 && !im.cacheEnabled {
		return ErrAllConnsDown
	}
	ts := time.Now()
	im.mtx.Lock()
	for len(im.eChanOut) > 0 || len(im.bChanOut) > 0 {
		if err := ctx.Err(); err != nil {
			im.mtx.Unlock()
			return err
		}
		time.Sleep(10 * time.Millisecond)
		if im.connHot == 0 {
			im.mtx.Unlock()
			return ErrAllConnsDown
		}
		//only check for a timeout if to is greater than zero.  A zero value or negative value means no timeout
		if to > 0 && time.Since(ts) > to {
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
		if !im.cacheAlways {
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
		if !im.cacheAlways {
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
	}
	if im.state != running {
		return ErrNotRunning
	}
	if im.attachActive {
		im.attacher.Attach(e)
	}
	im.eChan <- e
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
	im.bChan <- b
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
	}
	e := &entry.Entry{
		Data: data,
		TS:   tm,
		Tag:  tag,
	}
	return im.WriteEntryContext(ctx, e)
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

type connSet struct {
	ig  *IngestConnection
	tt  *tagTrans
	dst string
	src net.IP
}

// keep attempting to get a new connection set that we can actually write to
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
			im.Info("connected", log.KV("indexer", nc.dst), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
		} else {
			im.Info("re-connected", log.KV("indexer", nc.dst), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
		}
		break
	}
	return
}

func tickerInterval() time.Duration {
	//return a time between 750 and 1250 milliseconds
	return time.Duration(750+rand.Int63n(500)) * time.Millisecond
}

func (im *IngestMuxer) shouldSched() bool {
	//if pipelines are empty, schedule ourselves so that we can get a better distribution of entries
	return len(im.igst) > 1 && im.cache.BufferSize() == 0 && im.bcache.BufferSize() == 0
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
	if nc, ok = im.getNewConnSet(csc, connFailure, true); !ok {
		return
	}

	eC := im.eChanOut
	bC := im.bChanOut

inputLoop:
	for {
		select {
		case _ = <-im.dieChan:
			nc.ig.Sync()
			nc.ig.Close()
			return
		case ee, ok := <-eC:
			if !ok {
				eC = nil
				if bC == nil {
					return
				}
				continue
			}
			if ee == nil {
				continue
			}

			e := ee.(*entry.Entry)

			ttag, ok = nc.tt.Translate(e.Tag)
			if !ok {
				// If the ingest muxer has no idea what this tag is, drop it and notify
				if name, ok := im.LookupTag(e.Tag); !ok {
					im.Error("Got entry tagged with completely unknown intermediate tag, dropping it", log.KV("tagvalue", e.Tag), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
					continue inputLoop
				} else {
					im.Info("Got entry with new tag, need to renegotiate connection", log.KV("tag", name), log.KV("tagvalue", e.Tag), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
					// Could not translate, but it's a valid tag the muxer has seen before.
					// We need to push this to the equeue and reconnect
					// so we get the correct tag set.
					// DO NOT reverse translate, muxer knows about the tag
					im.recycleEntry(e)
					if nc, ok = im.getNewConnSet(csc, connFailure, false); !ok {
						break inputLoop
					}
					continue inputLoop
				}
			}
			e.Tag = ttag

			if len(e.SRC) == 0 {
				e.SRC = nc.src
			}
			if err = nc.ig.WriteEntry(e); err != nil {
				e.Tag = nc.tt.Reverse(e.Tag)
				im.recycleEntry(e)
				if nc, ok = im.getNewConnSet(csc, connFailure, false); !ok {
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
				if eC == nil {
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
					ttag, ok = nc.tt.Translate(b[i].Tag)
					if !ok {
						if name, ok := im.LookupTag(b[i].Tag); !ok {
							im.Error("Got entry tagged with completely unknown intermediate tag, dropping it", log.KV("tagvalue", b[i].Tag), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
							// first, reverse anything we've translated already
							for j := 0; j < i; j++ {
								b[j].Tag = nc.tt.Reverse(b[j].Tag)
							}
							im.recycleEntryBatch(b[:i]) //recycle and save what we can
						} else {
							im.Info("Got entry with new tag, need to renegotiate connection", log.KV("tag", name), log.KV("tagvalue", b[i].Tag), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
							// Could not translate! We need to push this to the equeue and reconnect
							// so we get the correct tag set.

							// first, reverse anything we've translated already
							for j := 0; j < i; j++ {
								b[j].Tag = nc.tt.Reverse(b[j].Tag)
							}
							im.recycleEntryBatch(b)
							if nc, ok = im.getNewConnSet(csc, connFailure, false); !ok {
								break inputLoop
							}
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
					b[i].Tag = nc.tt.Reverse(b[i].Tag)
				}
				im.recycleEntryBatch(b[n:])
				if nc, ok = im.getNewConnSet(csc, connFailure, false); !ok {
					break inputLoop
				}
			}
			//hack to get better distribution across connections in an muxer
			if im.shouldSched() {
				runtime.Gosched()
			}
		case tnc, ok = <-csc: //in case we get an unexpected new connection
			if !ok {
				nc.ig.Sync()
				nc.ig.Close()
				//attempt to sync with current ngst and then bail
				break inputLoop
			}
			nc = tnc //just an update
		case <-tmr.C:
			//periodically check the emergency queue and sync
			if !im.eq.clear(nc.ig, nc.tt) || nc.ig.Sync() != nil {
				if nc, ok = im.getNewConnSet(csc, connFailure, false); !ok {
					break inputLoop
				}
			}
			tmr.Reset(tickerInterval())
		}
	}
}

// the routine that manages
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
	var tt tagTrans
	var err error
	connErrNotif := make(chan bool, 1)
	ncc := make(chan connSet, 1)
	defer close(ncc)

	go im.writeRelayRoutine(ncc, connErrNotif)

	connErrNotif <- true

	//loop, trying to grab entries, or dying
	for {
		select {
		case _, ok := <-connErrNotif:
			if !ok {
				//this means that the relay function bailed
				if igst != nil {
					igst.Close()
				}
				im.goDead()
				im.connFailed(dst.Address, errors.New("Closed"))
				return
			}

			if igst != nil {
				im.Warn("reconnecting", log.KV("indexer", dst.Address), log.KV("ingester", im.name), log.KV("ingesteruuid", im.uuid))
				igst.Close()
				im.goDead() //let the world know of our failures
				im.igst[igIdx] = nil
				im.tagTranslators[igIdx] = nil

				//pull any entries out of the ingest connection and put them into the emergency queue
				ents := igst.outstandingEntries()
				for i := range ents {
					if ents[i] != nil {
						ents[i].Tag = tt.Reverse(ents[i].Tag)
					}
				}
				im.recycleEntryBatch(ents)
			}

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

			//get the source fired back up
			src, err = igst.Source()
			if err != nil {
				igst.Close()
				im.connFailed(dst.Address, err)
				return
			}

			im.mtx.Lock()
			im.igst[igIdx] = igst
			im.tagTranslators[igIdx] = &tt
			im.mtx.Unlock()

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
		if err := im.eq.push(nil, ents); err != nil {
			//FIXME - throw a fit about this
		}
	case im.bChan <- ents:
	}
	return
}

func (im *IngestMuxer) recycleEntry(ent *entry.Entry) {
	if ent == nil {
		return
	}

	//we wait for up to one second to push values onto feeder channels
	//if nothing eats them by then, we drop them into the emergency queue
	//and bail out
	tmr := time.NewTimer(recycleTimeout)
	defer tmr.Stop()

	select {
	case _ = <-tmr.C:
		if err := im.eq.push(ent, nil); err != nil {
			//FIXME - throw a fit about this
		}
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
	case _ = <-im.dieChan:
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

func (im *IngestMuxer) getConnection(tgt Target) (ig *IngestConnection, tt tagTrans, err error) {
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
		if ig, err = initConnection(tgt, im.tags, im.pubKey, im.privKey, im.verifyCert); err != nil {
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
			case _ = <-im.dieChan:
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

func (im *IngestMuxer) newTagTrans(igst *IngestConnection) (tagTrans, error) {
	tt := tagTrans(make([]entry.EntryTag, len(im.tagMap)))
	if len(tt) == 0 {
		return nil, ErrTagMapInvalid
	}
	for k, v := range im.tagMap {
		if int(v) > len(tt) {
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
	var ttag entry.EntryTag
	for {
		e, blk, populated := eq.pop()
		if !populated {
			ok = true
			break
		}
		if e != nil {
			ttag, ok = tt.Translate(e.Tag)
			if !ok {
				// could not translate, push it back on the queue and bail
				eq.push(e, blk)
				return
			}
			e.Tag = ttag
			if err := igst.WriteEntry(e); err != nil {
				//reset the tag
				e.Tag = tt.Reverse(e.Tag)

				//push the entries back into the queue
				if err := eq.push(e, blk); err != nil {
					//FIXME - log this?
				}

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
					ttag, ok = tt.Translate(blk[i].Tag)
					if !ok {
						// could not translate, push it back on the queue and bail
						// first we need to reverse the ones we have already translated, ugh
						for j := 0; j < i; j++ {
							blk[j].Tag = tt.Reverse(blk[j].Tag)
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
						blk[i].Tag = tt.Reverse(blk[i].Tag)
					}
				}
				if err := eq.push(e, blk); err != nil {
					//FIXME - log this?
				}
				ok = false
				break
			}
		}
	}
	return
}

type tagTrans []entry.EntryTag

// Translate translates a local tag to a remote tag.  Senders should not use this function
func (tt tagTrans) Translate(t entry.EntryTag) (entry.EntryTag, bool) {
	//check if this is the gravwell and if soo, pass it on through
	if t == entry.GravwellTagId {
		return t, true
	}
	//if this is a tag we have not negotiated, set it to the first one we have
	//we are assuming that its an error, but we still want the entry
	if int(t) >= len(tt) {
		return tt[0], false
	}
	return tt[t], true
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

func getStreamConfig(cfg config.IngestStreamConfig) (sc StreamConfiguration) {
	if cfg.Enable_Compression {
		sc.Compression = CompressSnappy
	}
	return
}
