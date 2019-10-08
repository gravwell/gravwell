/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/entry"
	"github.com/gravwell/ingesters/v3/version"
	"github.com/gravwell/timegrinder/v3"
	"github.com/gravwell/winevent/v3"
)

var (
	ErrRecordIDNotFound = errors.New("Failed to find RecordID in event")

	eventRecordRegex  = regexp.MustCompile("<EventRecordID>([0-9]+)</EventRecordID>")
	eventRecordPrefix = []byte(`<EventRecordID>`)
	eventRecordSuffix = []byte(`</EventRecordID>`)
)

type eventSrc struct {
	h   *winevent.EventStreamHandle
	tag entry.EntryTag
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
	igstLogLevel string
	uuid         string

	bmk     *winevent.BookmarkHandler
	evtSrcs []eventSrc
	igst    *ingest.IngestMuxer
	tg      *timegrinder.TimeGrinder
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
		igstLogLevel: cfg.LogLevel(),
		uuid:         id.String(),
	}, nil
}

func (m *mainService) Close() error {
	return m.shutdown()
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
			if err := m.bmk.Update(name, last); err != nil {
				rerr = fmt.Errorf("Failed to add bookmark for %s: %v", name, err)
				errorout("%s", rerr)
			}
		}
	}
	m.evtSrcs = nil

	//close the bookmark handler if its open
	if m.bmk != nil && m.bmk.Open() {
		if err := m.bmk.Sync(); err != nil {
			rerr = fmt.Errorf("Failed to sync bookmark: %v", err)
			errorout("%s", rerr)
		}
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
				break loop
			default:
				errorout("Got invalid control request #%d", c)
			}
		case err := <-consumerErr:
			if err != nil {
				errorout("Event consumer error: %v", err)
				errno = 1000
				ssec = true
			}
			break loop
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	wg.Wait()
	return
}

func (m *mainService) consumerRoutine(errC chan error, closeC chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	if err := m.init(); err != nil {
		errorout("Failed to start: %v", err)
		errC <- err
		return
	}
	tkr := time.Tick(500 * time.Millisecond)

consumerLoop:
	for {
		select {
		case <-tkr:
			for {
				if cont, err := m.consumeEvents(); err != nil {
					errorout("Failed to consume events: %v", err)
					errC <- err
					return
				} else if !cont {
					break
				}
			}
		case <-closeC:
			break consumerLoop
		}
	}
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
	debugout("Created bookmark\n")

	//fire up the ingesters
	//fire up the ingesters
	igCfg := ingest.UniformMuxerConfig{
		Destinations:    m.conns,
		Tags:            m.tags,
		Auth:            m.secret,
		LogLevel:        m.igstLogLevel,
		IngesterName:    "winevent",
		IngesterVersion: version.GetVersion(),
		IngesterUUID:    m.uuid,
	}
	if m.enableCache {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = m.cachePath
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
	if err := igst.WaitForHot(m.timeout); err != nil {
		return err
	}
	m.igst = igst
	hot, err := igst.Hot()
	if err != nil {
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

		evt, err := winevent.NewStream(c, last)
		if err != nil {
			return fmt.Errorf("Failed to create new eventStream(%s) on Channel %s: %v", c.Name, c.Channel, err)
		}
		debugout("Started stream %s at recordID %d\n", c.Name, last)
		evtSrcs = append(evtSrcs, eventSrc{h: evt, tag: tag})
	}
	if len(evtSrcs) == 0 {
		return fmt.Errorf("Failed to load event handles: %v", err)
	}
	m.evtSrcs = evtSrcs
	return nil
}

func (m *mainService) consumeEvents() (bool, error) {
	//if we can't get an IP then the muxer is probably fully disconnected
	//just chill
	ip, _ := m.igst.SourceIP()
	if ip == nil {
		return false, nil
	}

	//check on how many active connections we have, if there are none, don't
	//pull from the event muxer
	hotConns, err := m.igst.Hot()
	if err != nil {
		return false, err
	}
	if hotConns <= 0 {
		return false, nil
	}
	var again bool

	//we have an IP and some hot connections, do stuff
	//service events
	for _, eh := range m.evtSrcs {
		threw, err := m.serviceEventStreamChunk(eh, ip)
		if err != nil {
			return false, err
		}
		if threw {
			again = true
		}
		if err := m.bmk.Sync(); err != nil {
			return false, err
		}
	}
	return again, nil
}

func (m *mainService) serviceEventStreamChunk(eh eventSrc, ip net.IP) (bool, error) {
	ents, err := eh.h.Read()
	if err != nil {
		return false, err
	}
	for _, e := range ents {
		var ts entry.Timestamp
		var ok bool
		var lts time.Time
		if !m.ignoreTS {
			lts, ok, err = m.tg.Extract(e)
			if err != nil {
				return false, err
			}
			ts = entry.FromStandard(lts)
		}
		if !ok {
			ts = entry.Now()
		}
		recordID, err := extractRecordID(e)
		if err != nil {
			return false, err
		}
		eh.h.SetLast(recordID)
		ent := &entry.Entry{
			SRC:  ip,
			TS:   ts,
			Tag:  eh.tag,
			Data: e,
		}
		if err := m.igst.WriteEntry(ent); err != nil {
			return false, err
		}
		if err := m.bmk.Update(eh.h.Name(), recordID); err != nil {
			return false, err
		}
	}
	if len(ents) > 0 {
		debugout("Pulled %d events from %s\n", len(ents), eh.h.Name())
	}
	return len(ents) > 0, nil
}

func extractRecordID(buf []byte) (uint64, error) {
	x := eventRecordRegex.Find(buf)
	if len(x) == 0 {
		return 0, ErrRecordIDNotFound
	}
	x = bytes.TrimPrefix(bytes.TrimSuffix(x, eventRecordSuffix), eventRecordPrefix)
	v, err := strconv.ParseUint(string(x), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("Failed to get recordID from %s: %v", x, err)
	}
	return v, nil
}
