/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
	"github.com/gravwell/gravwell/v3/winevent"
)

const (
	eventSampleInterval       = 250 * time.Millisecond
	eventChannelRetryInterval = 10 * time.Second
	exitTimeout               = 3 * time.Second

	//don't ask, this took some digging, not well documented
	//this is the error number that comes back when a channel disappears
	ERROR_EVT_CHANNEL_NOT_FOUND uintptr = 15007
)

var (
	versionOverride string = `3.3.11`
)

type eventSrc struct {
	params winevent.EventStreamParams
	h      *winevent.EventStreamHandle
	proc   *processors.ProcessorSet
	tag    entry.EntryTag
}

type mainService struct {
	cfg          *CfgType
	secret       string
	timeout      time.Duration
	ignoreTS     bool
	tags         []string
	conns        []string
	bookmarkPath string
	streams      []winevent.EventStreamParams
	enableCache  bool
	cachePath    string
	cacheSize    int
	igstLogLevel string
	uuid         string
	label        string
	src          net.IP
	ctx          context.Context
	lmt          int64
	deadStreams  map[string]winevent.EventStreamParams

	bmk            *winevent.BookmarkHandler
	evtSrcs        map[string]eventSrc
	igst           *ingest.IngestMuxer
	tg             *timegrinder.TimeGrinder
	pp             processors.ProcessorConfig
	shutdownCalled bool
}

func NewService(cfg *CfgType) (*mainService, error) {
	//populate items from our config
	tags, err := cfg.Tags()
	if err != nil {
		return nil, fmt.Errorf("Failed to get tags from configuration: %v", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		return nil, fmt.Errorf("Failed to get backend targets from configuration: %v", err)
	}
	debugout("Acquired tags and targets\n")

	lmt, err := cfg.Global.RateLimit()
	if err != nil {
		return nil, fmt.Errorf("Failed to get rate limit from configuration: %w\n", err)
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	chanConf, err := cfg.Streams()
	if err != nil {
		return nil, fmt.Errorf("Failed to get a valid list of event channel configurations: %v", err)
	}
	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		return nil, errors.New("Couldn't read ingester UUID")
	}
	debugout("Parsed %d streams\n", len(chanConf))
	return &mainService{
		cfg:          cfg, //set the config so we can shove push it via the ingest channel
		timeout:      cfg.Timeout(),
		secret:       cfg.Secret(),
		tags:         tags,
		conns:        conns,
		ignoreTS:     cfg.IgnoreTimestamps(),
		bookmarkPath: cfg.BookmarkPath(),
		streams:      chanConf,
		enableCache:  cfg.EnableCache(),
		cachePath:    cfg.LocalFileCachePath(),
		cacheSize:    cfg.CacheSize(),
		igstLogLevel: cfg.LogLevel(),
		uuid:         id.String(),
		label:        cfg.Global.Label,
		pp:           cfg.Preprocessor,
		lmt:          lmt,
		evtSrcs:      map[string]eventSrc{},
		deadStreams:  map[string]winevent.EventStreamParams{},
	}, nil
}

func (m *mainService) Close() (err error) {
	err = m.shutdown()
	lg.Info("service is closing", log.KVErr(err))
	return
}

func (m *mainService) shutdown() error {
	if m.shutdownCalled {
		return nil // already shtudown
	}
	var rerr error
	//close any service handles that happen to be open
	for _, e := range m.evtSrcs {
		last := e.h.Last()
		name := e.h.Name()
		if err := e.h.Close(); err != nil {
			lg.Error("failed to close event source", log.KV("name", name), log.KVErr(err))
			rerr = fmt.Errorf("Failed to close %s: %v", name, err)
			continue
		}
		if m.bmk != nil {
			lg.Info("shutdown - Updating bookmark", log.KV("name", name), log.KV("last", last))
			if err := m.bmk.Update(name, last); err != nil {
				lg.Error("failed to add bookmark", log.KV("name", name), log.KVErr(err))
				rerr = fmt.Errorf("Failed to add bookmark for %s: %v", name, err)
			}
		}
	}
	m.evtSrcs = nil

	//close the bookmark handler if its open
	if m.bmk != nil {
		if err := m.bmk.Close(); err != nil {
			lg.Error("failed to close bookmark", log.KVErr(err))
			rerr = fmt.Errorf("Failed to close bookmark: %v", err)
		}
	}
	if m.igst != nil {
		if err := m.igst.Sync(time.Second); err != nil {
			lg.Error("failed to sync ingest muxer", log.KVErr(err))
			rerr = fmt.Errorf("Failed to sync the ingest muxer: %v", err)
		} else {
			if err := m.igst.Close(); err != nil {
				lg.Error("failed to close ingest muxer", log.KVErr(err))
				rerr = fmt.Errorf("Failed to close the ingest muxer: %v", err)
			} else {
				m.igst = nil
			}
		}
	}
	m.shutdownCalled = true
	return rerr
}

func (m *mainService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	//let the system know we are up and ready to accept commands
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	var cancel context.CancelFunc
	m.ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	consumerErr := make(chan error, 1)
	consumerClose := make(chan bool, 1)
	defer close(consumerClose)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go m.consumerRoutine(consumerErr, consumerClose, &wg)
loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				//not sure why this is sent twice, but ok
				//its in the example from official golang libs
				changes <- c.CurrentStatus
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				consumerClose <- true
				lg.Info("service stopping")
				break loop
			default:
				lg.Error("got invalid control request", log.KV("request", c))
			}
		case err := <-consumerErr:
			if err != nil {
				lg.Error("event consumer error", log.KVErr(err))
				errno = 1000
				ssec = true
			} else {
				lg.Info("event consumer stopping", log.KVErr(err))
			}
			break loop
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	lg.Info("service transitioned to StopPending, waiting for consumer")
	waitTimeout(&wg, cancel, exitTimeout) //wait with a timeout

	//ok, shutdown the ingester
	if err := m.shutdown(); err != nil {
		lg.Error("service shutdown error", log.KVErr(err))
	} else {
		lg.Info("service transitioned to Stopped")
	}
	changes <- svc.Status{State: svc.Stopped}
	return
}

// waitTimeout will wait up to to duration on the wait group, then it just fires a
func waitTimeout(wg *sync.WaitGroup, cf func(), to time.Duration) {
	ch := make(chan struct{})
	go func(c chan struct{}) {
		defer close(c)
		wg.Wait()
	}(ch)
	select {
	case <-ch:
	case <-time.After(to):
		lg.Error("timed out waiting for ingest routine to exit, cancelling")
		cf() //cancel the context and wait on the waitgroup channel
		<-ch
	}
}

func (m *mainService) consumerRoutine(errC chan error, closeC chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	if err := m.init(); err != nil {
		lg.Error("gailed to start consumer routine", log.KVErr(err))
		errC <- err
		return
	}

	// if this routine exits the process is exiting so we don't really worry about
	// the leaky ticker from time.Tick
	tkr := time.Tick(eventSampleInterval)
	rebuildTkr := time.Tick(eventChannelRetryInterval)

consumerLoop:
	for {
		select {
		case <-rebuildTkr:
			//make a best effort to fire up streams that could not be built at startup
			//this can happen when channels are configured that are not available
			//like sysmon, we retry to grab them periodically
			if len(m.deadStreams) > 0 {
				m.retryDeadStreams()
			}
		case <-tkr:
			if nev, err := m.consumeEvents(); err != nil {
				lg.Error("failed to consume events", log.KVErr(err))
				errC <- err
				return
			} else if nev {
				if err := m.bmk.Sync(); err != nil {
					lg.Error("failed to sync bookmark", log.KVErr(err))
					errC <- err
					return
				}
				break
			}
		case <-closeC:
			lg.Info("Consumer exiting")
			break consumerLoop
		}
	}
	if err := m.bmk.Sync(); err != nil {
		lg.Error("failed to sync bookmark", log.KVErr(err))
		errC <- err
	}
	lg.Info("Consumer exiting\n")
}

func (m *mainService) init() error {
	if !m.ignoreTS {
		window, err := m.cfg.Global.GlobalTimestampWindow()
		if err != nil {
			return err
		}
		tcfg := timegrinder.Config{
			TSWindow: window,
		}
		tg, err := timegrinder.NewTimeGrinder(tcfg)
		if err != nil {
			return fmt.Errorf("Failed to create new timegrinder: %v", err)
		}
		m.tg = tg
	}
	bmk, err := winevent.NewBookmark(m.bookmarkPath)
	if err != nil {
		return fmt.Errorf("Failed to create a bookmark at %s: %v", m.bookmarkPath, err)
	}
	m.bmk = bmk
	debugout("Opened bookmark\n")

	//fire up the ingesters
	igCfg := ingest.UniformMuxerConfig{
		Destinations:    m.conns,
		Tags:            m.tags,
		Auth:            m.secret,
		LogLevel:        m.igstLogLevel,
		IngesterName:    ingesterName,
		IngesterVersion: version.GetVersion(),
		IngesterUUID:    m.uuid,
		RateLimitBps:    m.lmt,
	}
	if m.cfg != nil {
		igCfg.Attach = m.cfg.Attach
	}
	//igCfg.IngesterVersion = versionOverride
	if m.enableCache {
		igCfg.CacheMode = ingest.CacheModeAlways
		igCfg.CachePath = m.cachePath
		igCfg.CacheSize = m.cacheSize
	}
	debugout("Starting ingester connections")
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		return fmt.Errorf("Failed build our ingest system: %v", err)
	}
	if err := igst.Start(); err != nil {
		return fmt.Errorf("Failed start our ingest system: %v", err)
	}
	debugout("Started ingester stream\n")
	if err := igst.WaitForHotContext(m.ctx, m.timeout); err != nil {
		lg.Error("failed to wait for hot ingester connections", log.KVErr(err))
		return err
	}
	m.igst = igst
	hot, err := igst.Hot()
	if err != nil {
		lg.Error("failed to get hot connection count", log.KVErr(err))
		return err
	}
	//add ourselves as self ingesting
	if m.cfg.Global.SelfIngest() {
		lg.AddRelay(igst)
	}
	// prepare the configuration we're going to send upstream
	if m.cfg != nil {
		if err = igst.SetRawConfiguration(m.cfg.RawConfig()); err != nil {
			lg.Error("failed to set configuration for ingester state messages", log.KVErr(err))
			return err
		}
	}

	lg.Info("Ingester connection established", log.KV("hot-connections", hot))

	for _, c := range m.streams {
		evt, fatal, err := m.initStream(c, true) //tell the init function to honk about errors
		if err != nil {
			if fatal {
				return err
			}
			//non fatal error, throw the stream on the dead stream stack
			m.deadStreams[c.Name] = c
			continue
		}
		m.evtSrcs[c.Name] = evt
	}
	if len(m.evtSrcs) == 0 {
		return fmt.Errorf("Failed to load event handles: %v", err)
	}
	m.src = nil

	return nil
}

func (m *mainService) initStream(c winevent.EventStreamParams, errReport bool) (eventSrc, bool, error) {
	tag, err := m.igst.GetTag(c.TagName)
	if err != nil {
		//failing to get the tag is fatal
		return eventSrc{}, true, fmt.Errorf("Failed to translate tag %s: %v", c.TagName, err)
	}
	last, err := m.bmk.Get(c.Name)
	if err != nil {
		//failing to get the bookmark is fatal
		return eventSrc{}, true, fmt.Errorf("Failed to get bookmark for %s: %v", c.Name, err)
	}
	pproc, err := m.pp.ProcessorSet(m.igst, c.Preprocessor)
	if err != nil {
		//failing to create the preprocessor set is fatal
		return eventSrc{}, true, fmt.Errorf("Preprocessor construction error: %v", err)
	}

	var evt *winevent.EventStreamHandle
	if evt, err = winevent.NewStream(c, last); err != nil {
		//failing to create a new event stream is NOT fatal, this could be due to a few things
		//for example if they asked for the sysmon stream but did not install sysmon this will fail
		//they could also have configured a stream and then uninstalled the system that provides that stream
		//just honk about it via errorout and throw the eventSrc name onto the dead channel list
		//we will try again later
		if errReport {
			lg.Error("failed to create new eventStream", log.KV("name", c.Name), log.KV("channel", c.Channel), log.KVErr(err))
		}
		return eventSrc{}, false, err
	}
	params := []rfc5424.SDParam{
		log.KV("stream", c.Name),
		log.KV("channel", c.Channel),
		log.KV("tag", c.TagName),
	}
	if c.ReachBack != 0 {
		params = append(params, log.KV("reachback", fmt.Sprintf("%v", c.ReachBack)))
	}
	if len(c.Providers) != 0 {
		params = append(params, log.KV("providers", fmt.Sprintf("%v", c.Providers)))
	}
	if c.Levels != `` {
		params = append(params, log.KV("levels", fmt.Sprintf("%v", c.Levels)))
	}
	if c.EventIDs != `` {
		params = append(params, log.KV("eventIDs", fmt.Sprintf("%v", c.EventIDs)))
	}
	lg.Info("starting stream", params...)
	return eventSrc{params: c, h: evt, proc: pproc, tag: tag}, false, nil //all good
}

func (m *mainService) retryDeadStreams() error {
	if m == nil || len(m.deadStreams) == 0 {
		return nil //thing to try
	}

	for k, c := range m.deadStreams {
		evt, fatal, err := m.initStream(c, false) //we already honked, don't keep honking
		if err != nil {
			if fatal {
				return err
			}
			//non fatal error, just continue
			continue
		}
		//success, remove from dead streams and add to our set of streams
		delete(m.deadStreams, k)
		m.evtSrcs[c.Name] = evt

		/* TODO/FIXME issue #428
		params := []rfc5424.SDParam{
			log.KV("stream", c.Name),
			log.KV("channel", c.Channel),
			log.KV("tag", c.TagName),
		}
		if c.ReachBack != 0 {
			params = append(params, log.KV("reachback", fmt.Sprintf("%v", c.ReachBack)))
		}
		if len(c.Providers) != 0 {
			params = append(params, log.KV("providers", fmt.Sprintf("%v", c.Providers)))
		}
		if c.Levels != `` {
			params = append(params, log.KV("levels", fmt.Sprintf("%v", c.Levels)))
		}
		if c.EventIDs != `` {
			params = append(params, log.KV("eventIDs", fmt.Sprintf("%v", c.EventIDs)))
		}
		m.lgr.Info("recovered stream", params...)
		*/
	}
	return nil
}

func (m *mainService) consumeEvents() (bool, error) {
	//we have an IP and some hot connections, do stuff
	//service events
	var consumed bool
	for k, eh := range m.evtSrcs {
		if nev, err := m.serviceEventStream(eh, m.src); err != nil {
			if isStreamDoesNotExist(err) {
				//close the event stream
				eh.h.Close()
				eh.proc.Close()
				//remove it from the hot set
				delete(m.evtSrcs, k)

				//add to the dead streams
				m.deadStreams[k] = eh.params
				//handled, continue on your way
			} else {
				//some other error, bail
				return consumed, err
			}
		} else if nev {
			consumed = true
		}
	}
	return consumed, nil
}

func (m *mainService) serviceEventStream(eh eventSrc, ip net.IP) (nev bool, err error) {
	var hit bool
	full := true
	//feed from the stream until we don't get any entries out
	for full {
		if hit, full, err = m.serviceEventStreamChunk(eh, ip); err != nil {
			lg.Error("failed to service event stream", log.KV("name", eh.h.Name()), log.KVErr(err))
			if err = eh.h.Reset(); err != nil {
				lg.Error("failed to reset event stream", log.KV("name", eh.h.Name()), log.KVErr(err))
				return
			} else {
				lg.Warn("reset event stream", log.KV("name", eh.h.Name()))
			}
			return
		} else if hit {
			nev = true
		}
		select {
		case <-m.ctx.Done():
			full = false
		default:
			//do nothing
			continue
		}
	}
	return
}

func (m *mainService) serviceEventStreamChunk(eh eventSrc, ip net.IP) (hit, fullRead bool, err error) {
	var ents []winevent.RenderedEvent
	var warn error
	if ents, fullRead, warn, err = eh.h.Read(); err != nil {
		return
	} else if warn != nil {
		lg.Warn("event stream warning", log.KV("name", eh.h.Name()), log.KV("warning", warn))
	}
	var first, last uint64

	for i, e := range ents {
		var ts entry.Timestamp
		var ok bool
		var lts time.Time
		if !m.ignoreTS {
			lts, ok, err = m.tg.Extract(e.Buff)
			if err != nil {
				lg.Error("failed to extract TS", log.KVErr(err))
				return
			}
			ts = entry.FromStandard(lts)
		}
		if !ok {
			ts = entry.Now()
		}
		ent := &entry.Entry{
			SRC:  ip,
			TS:   ts,
			Tag:  eh.tag,
			Data: e.Buff,
		}
		if err = eh.proc.ProcessContext(ent, m.ctx); err != nil {
			lg.Warn("failed to Process event", log.KVErr(err))
			return
		}
		if err = m.bmk.Update(eh.h.Name(), e.ID); err != nil {
			lg.Error("failed to update bookmark", log.KV("name", eh.h.Name()), log.KVErr(err))
			return
		}
		if i == 0 {
			first = e.ID
		}
		last = e.ID
	}
	if len(ents) > 0 {
		hit = true
		debugout("Pulled %d events from %s [%d - %d]\n", len(ents), eh.h.Name(), first, last)
	}
	return
}

// isStreamDoesNotExist is looking for the syscall Errno which indicates a stream has disapeared
// the error message states that the file handle is invalid and then punts out a message that appears
// to change between versions of windows, but the errno "so far" appears consistent.
// The error number is defined in winerror.h.
func isStreamDoesNotExist(err error) bool {
	if err != nil {
		if syscallErr, ok := err.(syscall.Errno); ok {
			return uintptr(syscallErr) == ERROR_EVT_CHANNEL_NOT_FOUND
		}
	}
	return false
}
