/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
	"github.com/gravwell/gravwell/v3/winevent"
)

const (
	eventSampleInterval = 250 * time.Millisecond
	exitTimeout         = 3 * time.Second
)

var (
	versionOverride string = `3.3.11`
)

type eventSrc struct {
	h    *winevent.EventStreamHandle
	proc *processors.ProcessorSet
	tag  entry.EntryTag
}

type mainService struct {
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
	src          net.IP
	ctx          context.Context
	lmt          int64

	bmk     *winevent.BookmarkHandler
	evtSrcs []eventSrc
	igst    *ingest.IngestMuxer
	tg      *timegrinder.TimeGrinder
	pp      processors.ProcessorConfig
}

func NewService(cfg *winevent.CfgType) (*mainService, error) {
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
		pp:           cfg.Preprocessor,
		lmt:          lmt,
	}, nil
}

func (m *mainService) Close() (err error) {
	err = m.shutdown()
	infoout("Service is closing with %v\n", err)
	return
}

func (m *mainService) shutdown() error {
	var rerr error
	//close any service handles that happen to be open
	for _, e := range m.evtSrcs {
		last := e.h.Last()
		name := e.h.Name()
		if err := e.h.Close(); err != nil {
			rerr = fmt.Errorf("Failed to close %s: %v", name, err)
			errorout("%s", rerr)
			continue
		}
		if m.bmk != nil {
			infoout("shutdown - Updating bookmark %s to %d\n", name, last)
			if err := m.bmk.Update(name, last); err != nil {
				rerr = fmt.Errorf("Failed to add bookmark for %s: %v", name, err)
				errorout("%s", rerr)
			}
		}
	}
	m.evtSrcs = nil

	//close the bookmark handler if its open
	if m.bmk != nil {
		if err := m.bmk.Close(); err != nil {
			rerr = fmt.Errorf("Failed to close bookmark: %v", err)
			errorout("%s", rerr)
		}
	}
	if m.igst != nil {
		if err := m.igst.Sync(time.Second); err != nil {
			rerr = fmt.Errorf("Failed to sync the ingest muxer: %v", err)
			errorout("%s", rerr)
		} else {
			if err := m.igst.Close(); err != nil {
				rerr = fmt.Errorf("Failed to close the ingest muxer: %v", err)
				errorout("%s", rerr)
			} else {
				m.igst = nil
			}
		}
	}
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
				infoout("Service interrogate returning %v\n", c.CurrentStatus)
			case svc.Stop, svc.Shutdown:
				consumerClose <- true
				infoout("Service stopping\n")
				break loop
			default:
				errorout("Got invalid control request #%d\n", c)
			}
		case err := <-consumerErr:
			if err != nil {
				errorout("Event consumer error: %v\n", err)
				errno = 1000
				ssec = true
			} else {
				infoout("Event consumer stopping with %v\n", err)
			}
			break loop
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	infoout("Service transitioned to StopPending, waiting for consumer\n")
	waitTimeout(&wg, cancel, exitTimeout) //wait with a timeout
	changes <- svc.Status{State: svc.Stopped}
	infoout("Service transitioned to Stopped\n")
	return
}

//waitTimeout will wait up to to duration on the wait group, then it just fires a
func waitTimeout(wg *sync.WaitGroup, cf func(), to time.Duration) {
	ch := make(chan struct{})
	go func(c chan struct{}) {
		defer close(c)
		wg.Wait()
	}(ch)
	select {
	case <-ch:
	case <-time.After(to):
		errorout("Timed out waiting for ingest routine to exit, cancelling\n")
		cf() //cancel the context and wait on the waitgroup channel
		<-ch
	}
}

func (m *mainService) consumerRoutine(errC chan error, closeC chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	if err := m.init(); err != nil {
		errorout("Failed to start: %v", err)
		errC <- err
		return
	}
	tkr := time.Tick(eventSampleInterval)

consumerLoop:
	for {
		select {
		case <-tkr:
			if nev, err := m.consumeEvents(); err != nil {
				errorout("Failed to consume events: %v", err)
				errC <- err
				return
			} else if nev {
				if err := m.bmk.Sync(); err != nil {
					errorout("Failed to sync bookmark: %v", err)
					errC <- err
					return
				}
				break
			}
		case <-closeC:
			infoout("Consumer exiting\n")
			break consumerLoop
		}
	}
	if err := m.bmk.Sync(); err != nil {
		errorout("Failed to sync bookmark: %v", err)
		errC <- err
	}
	infoout("Consumer exiting\n")
}

func (m *mainService) init() error {
	if !m.ignoreTS {
		tg, err := timegrinder.NewTimeGrinder(timegrinder.Config{})
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
		IngesterName:    "winevent",
		IngesterVersion: version.GetVersion(),
		IngesterUUID:    m.uuid,
		RateLimitBps:    m.lmt,
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
		errorout("Failed to wait for hot ingester connections: %v\n", err)
		return err
	}
	m.igst = igst
	hot, err := igst.Hot()
	if err != nil {
		errorout("Failed to get hot connection count: %v\n", err)
		return err
	}
	infoout("Ingester established %d connections\n", hot)

	var evtSrcs []eventSrc
	for _, c := range m.streams {
		tag, err := igst.GetTag(c.TagName)
		if err != nil {
			return fmt.Errorf("Failed to translate tag %s: %v", c.TagName, err)
		}
		last, err := bmk.Get(c.Name)
		if err != nil {
			return fmt.Errorf("Failed to get bookmark for %s: %v", c.Name, err)
		}
		pproc, err := m.pp.ProcessorSet(igst, c.Preprocessor)
		if err != nil {
			return fmt.Errorf("Preprocessor construction error: %v", err)
		}
		evt, err := winevent.NewStream(c, last)
		if err != nil {
			return fmt.Errorf("Failed to create new eventStream(%s) on Channel %s: %v", c.Name, c.Channel, err)
		}
		infoout("Started stream %s at recordID %d\n", c.Name, last)
		msg := fmt.Sprintf("starting stream %s on channel %s at recordID %d, ingesting to tag %s.", c.Name, c.Channel, last, c.TagName)
		if c.ReachBack != 0 {
			msg += fmt.Sprintf(" Reachback is %v.", c.ReachBack)
		}
		if len(c.Providers) != 0 {
			msg += fmt.Sprintf(" Providers: %v.", c.Providers)
		}
		if c.Levels != `` {
			msg += fmt.Sprintf(" Allowed levels: %v.", c.Levels)
		}
		if c.EventIDs != `` {
			msg += fmt.Sprintf(" Recording only the following EventIDs: %v.", c.EventIDs)
		}
		igst.Info(msg)
		evtSrcs = append(evtSrcs, eventSrc{h: evt, proc: pproc, tag: tag})
	}
	if len(evtSrcs) == 0 {
		return fmt.Errorf("Failed to load event handles: %v", err)
	}
	m.evtSrcs = evtSrcs
	m.src = nil

	return nil
}

func (m *mainService) consumeEvents() (bool, error) {
	//we have an IP and some hot connections, do stuff
	//service events
	var consumed bool
	for _, eh := range m.evtSrcs {
		if nev, err := m.serviceEventStream(eh, m.src); err != nil {
			return consumed, err
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
			errorout("Failed to service event stream %s: %v\n", eh.h.Name(), err)
			if err = eh.h.Reset(); err != nil {
				errorout("Failed to reset event stream %s: %v\n", eh.h.Name(), err)
				return
			} else {
				warnout("Reset event stream %s\n", eh.h.Name())
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
		warnout("Event stream %s warning %q\n", eh.h.Name(), warn)
	}
	var first, last uint64

	for i, e := range ents {
		var ts entry.Timestamp
		var ok bool
		var lts time.Time
		if !m.ignoreTS {
			lts, ok, err = m.tg.Extract(e.Buff)
			if err != nil {
				errorout("Failed to extract TS: %v\n", err)
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
			warnout("Failed to Process event: %v\n", err)
			return
		}
		if err = m.bmk.Update(eh.h.Name(), e.ID); err != nil {
			errorout("Failed to update bookmark for %s: %v\n", eh.h.Name(), err)
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
