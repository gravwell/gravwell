/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/gravwell/filewatch/v3"
	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/entry"
	"github.com/gravwell/timegrinder/v3"
)

const (
	defaultEntryChannelSize int = 1024
)

var (
	dbgLogger debugLogger
)

type mainService struct {
	secret    string
	timeout   time.Duration
	tags      []string
	conns     []string
	flocs     map[string]FollowType
	igst      *ingest.IngestMuxer
	tg        *timegrinder.TimeGrinder
	wtchr     *filewatch.WatchManager
	entCh     chan *entry.Entry
	cachePath string
	logLevel  string
}

func NewService(cfg *cfgType) (*mainService, error) {
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
	//fire up the watch manager
	wtcher, err := filewatch.NewWatcher(cfg.StatePath())
	if err != nil {
		return nil, err
	}

	var cachePath string
	if cfg.CacheEnabled() {
		cachePath = cfg.CachePath()
	}

	debugout("Watching %d Directories\n", len(cfg.Follower))
	return &mainService{
		timeout:   cfg.Timeout(),
		secret:    cfg.Secret(),
		tags:      tags,
		conns:     conns,
		flocs:     cfg.Followers(), //this copies the map
		entCh:     make(chan *entry.Entry, defaultEntryChannelSize),
		wtchr:     wtcher,
		cachePath: cachePath,
		logLevel:  cfg.LogLevel(),
	}, nil
}

func (m *mainService) Close() (err error) {
	if err = m.shutdown(); err == nil {
		infoout("%s stopped", serviceName)
	}
	return
}

func (m *mainService) shutdown() error {
	var rerr error
	if err := m.wtchr.Close(); err != nil {
		return err
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
				//shutdown the watchers to get the consumer routine to exit
				consumerClose <- true
				break loop
			default:
				errorout("Got invalid control request #%d", c)
			}
		case err := <-consumerErr:
			if err != nil {
				errorout("FileFollow consumer error: %v", err)
				errno = 1000
				ssec = true
			}
			break loop
		}
	}
	infoout("%s stopping", serviceName)
	changes <- svc.Status{State: svc.StopPending}
	wg.Wait()
	return
}

func (m *mainService) consumerRoutine(errC chan error, closeC chan bool, wg *sync.WaitGroup) {
	var err error
	defer wg.Done()
	if err = m.init(); err != nil {
		errorout("Failed to start: %v", err)
		errC <- err
		return
	}
	debugout("Consumer routine loop running\n")
consumerLoop:
	for {
		select {
		case evt, ok := <-m.entCh:
			if !ok {
				break consumerLoop
			} else if err = m.igst.WriteEntry(evt); err != nil {
				break consumerLoop
			}
		case <-closeC:
			break consumerLoop
		}
	}
	errC <- err
}

func (m *mainService) init() error {
	//check that there is something to load up and watch
	if len(m.flocs) == 0 {
		return errors.New("No watch locations specified")
	}

	//fire up the ingesters
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: m.conns,
		Tags:         m.tags,
		Auth:         m.secret,
		LogLevel:     m.logLevel,
	}
	if m.cachePath != `` {
		ingestConfig.EnableCache = true
		ingestConfig.CacheConfig.FileBackingLocation = m.cachePath
	}

	debugout("Starting ingester connections ")
	igst, err := ingest.NewUniformMuxer(ingestConfig)
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

	//build up the handlers
	for k, v := range m.flocs {
		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			errorout("Failed to resolve tag \"%s\" for %s: %v\n", v.Tag_Name, k, err)
			return err
		}
		//create our handler for this watcher
		cfg := filewatch.LogHandlerConfig{
			Tag:           tag,
			IgnoreTS:      v.Ignore_Timestamps,
			AssumeLocalTZ: v.Assume_Local_Timezone,
			Logger:        dbgLogger,
		}
		lh, err := filewatch.NewLogHandler(cfg, m.entCh)
		if err != nil {
			errorout("Failed to generate handler: %v", err)
			return err
		}
		c := filewatch.WatchConfig{
			ConfigName: k,
			BaseDir:    v.Base_Directory,
			FileFilter: v.File_Filter,
			Hnd:        lh,
		}
		if err := m.wtchr.Add(c); err != nil {
			errorout("Failed to add watch directory for %s (%s): %v\n",
				v.Base_Directory, v.File_Filter, err)
			m.wtchr.Close()
			return err
		}
	}
	m.wtchr.SetLogger(m.igst)
	return m.wtchr.Start()
}

func debugPrint(f string, args ...interface{}) {
	infoout(f, args...)
}
