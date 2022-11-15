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
	"os"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/gravwell/gravwell/v3/filewatch"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	defaultEntryChannelSize int = 1024
)

var (
	dbgLogger debugLogger
)

type mainService struct {
	secret      string
	timeout     time.Duration
	tags        []string
	conns       []string
	flocs       map[string]follower
	igst        *ingest.IngestMuxer
	timeFormats config.CustomTimeFormat
	wtchr       *filewatch.WatchManager
	pp          processors.ProcessorConfig
	procs       []*processors.ProcessorSet
	srcOverride string
	logLevel    string
	uuid        string
	label       string
	ctx         context.Context
	cacheDepth  int
	cachePath   string
	cacheSize   int
	cacheMode   string
	lmt         int64
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
	lmt, err := cfg.RateLimit()
	if err != nil {
		return nil, fmt.Errorf("Failed to get rate limit from configuration: %w\n", err)
	}

	debugout("Acquired tags and targets\n")
	//fire up the watch manager
	wtchr, err := filewatch.NewWatcher(cfg.StatePath())
	if err != nil {
		return nil, err
	}
	//pass in the ingest muxer to the file watcher so it can throw info and errors down the muxer chan
	wtchr.SetMaxFilesWatched(cfg.Max_Files_Watched)

	id, ok := cfg.IngesterUUID()
	if !ok {
		return nil, errors.New("Couldn't read ingester UUID")
	}

	debugout("Watching %d Directories\n", len(cfg.Follower))
	return &mainService{
		timeout:     cfg.Timeout(),
		secret:      cfg.Secret(),
		tags:        tags,
		conns:       conns,
		flocs:       cfg.Followers(), //this copies the map
		wtchr:       wtchr,
		timeFormats: cfg.TimeFormat,
		pp:          cfg.Preprocessor,
		logLevel:    cfg.LogLevel(),
		uuid:        id.String(),
		label:       cfg.Label,
		srcOverride: cfg.Source_Override,
		cacheDepth:  cfg.Cache_Depth,
		cachePath:   cfg.Ingest_Cache_Path,
		cacheSize:   cfg.Max_Ingest_Cache,
		cacheMode:   cfg.Cache_Mode,
		lmt:         lmt,
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
		for _, v := range m.procs {
			if v != nil {
				if err := v.Close(); err != nil {
					err = fmt.Errorf("Failed to close preprocessor: %v", err)
					errorout("%s", err)
				}
			}
		}
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
	var cancel context.CancelFunc
	m.ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	if quit, err := m.initWithCancel(m.ctx, cancel, r, changes); err != nil {
		ssec = true
		errno = 1000
		errorout("Failed to initialize the service: %v", err)
		changes <- svc.Status{State: svc.StopPending}
		return
	} else if quit {
		infoout("%s stopping", serviceName)
		changes <- svc.Status{State: svc.StopPending}
		return
	}

loop:
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			//not sure why this is sent twice, but ok
			//its in the example from official golang libs
			changes <- c.CurrentStatus
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			//shutdown the watchers to get the consumer routine to exit
			cancel()
			break loop
		default:
			errorout("Got invalid control request #%d", c)
			break loop
		}
	}
	infoout("%s stopping", serviceName)
	changes <- svc.Status{State: svc.StopPending}
	return
}

func (m *mainService) initWithCancel(ctx context.Context, cf context.CancelFunc, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (quit bool, err error) {
	ret := make(chan error, 1)

	go func(ctx context.Context, rc chan error) {
		rc <- m.init(ctx)
		close(rc)
	}(ctx, ret)

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
				cf()
				if err = <-ret; err == context.Canceled {
					err = nil
				}
				quit = true
				break loop
			default:
				errorout("Got invalid control request #%d", c) //ignore this here
			}
		case err = <-ret:
			if err != nil {
				quit = true
				if err == context.Canceled {
					err = nil //don't report this
				}
			}
			break loop
		}
	}
	return
}

func (m *mainService) init(ctx context.Context) error {
	//check that there is something to load up and watch
	if len(m.flocs) == 0 {
		return errors.New("No watch locations specified")
	}

	//fire up the ingesters
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations:    m.conns,
		Tags:            m.tags,
		Auth:            m.secret,
		LogLevel:        m.logLevel,
		IngesterName:    "winfilefollow",
		IngesterVersion: version.GetVersion(),
		IngesterUUID:    m.uuid,
		IngesterLabel:   m.label,
		RateLimitBps:    m.lmt,
		CacheDepth:      m.cacheDepth,
		CachePath:       m.cachePath,
		CacheSize:       m.cacheSize,
		CacheMode:       m.cacheMode,
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
	if err := igst.WaitForHotContext(m.ctx, m.timeout); err != nil {
		return err
	}
	m.igst = igst
	hot, err := igst.Hot()
	if err != nil {
		return err
	}
	infoout("Ingester established %d connections\n", hot)
	m.wtchr.SetLogger(igst)

	var src net.IP
	if m.srcOverride != "" {
		// global override
		if src = net.ParseIP(m.srcOverride); src == nil {
			errorout("Global Source-Override is invalid")
			return err
		}
	} else {
		//it is ok to set src to nil, this can and will fail sometimes, ingest muxer will correct on ingest
		src, _ = igst.SourceIP()
	}

	//build up the handlers
	for k, val := range m.flocs {
		pproc, err := m.pp.ProcessorSet(igst, val.Preprocessor)
		if err != nil {
			errorout("Preprocessor construction error: %v", err)
			return err
		}
		m.procs = append(m.procs, pproc)
		//get the tag for this listener
		tag, err := igst.GetTag(val.Tag_Name)
		if err != nil {
			errorout("Failed to resolve tag \"%s\" for %s: %v\n", val.Tag_Name, k, err)
			return err
		}
		tsFmtOverride, err := val.TimestampOverride()
		if err != nil {
			errorout("Invalid timestamp override \"%s\": %v\n", val.Timestamp_Format_Override, err)
			return err
		}

		//create our handler for this watcher
		cfg := filewatch.LogHandlerConfig{
			TagName:                 val.Tag_Name,
			Tag:                     tag,
			Src:                     src,
			IgnoreTS:                val.Ignore_Timestamps,
			AssumeLocalTZ:           val.Assume_Local_Timezone,
			IgnorePrefixes:          val.Ignore_Line_Prefix,
			IgnoreGlobs:             val.Ignore_Glob,
			TimestampFormatOverride: tsFmtOverride,
			Logger:                  dbgLogger,
			TimezoneOverride:        val.Timezone_Override,
			UserTimeRegex:           val.Timestamp_Regex,
			UserTimeFormat:          val.Timestamp_Format_String,
			Ctx:                     ctx,
			TimeFormat:              m.timeFormats,
		}

		lh, err := filewatch.NewLogHandler(cfg, pproc)
		if err != nil {
			errorout("Failed to generate handler: %v", err)
			return err
		}
		c := filewatch.WatchConfig{
			ConfigName: k,
			BaseDir:    val.Base_Directory,
			FileFilter: val.File_Filter,
			Hnd:        lh,
			Recursive:  val.Recursive,
		}
		if rex, ok, err := val.TimestampDelimited(); err != nil {
			errorout("Invalid timestamp delimiter: %v\n", err)
		} else if ok {
			c.Engine = filewatch.RegexEngine
			c.EngineArgs = rex
		} else {
			c.Engine = filewatch.LineEngine
		}

		if err := m.wtchr.Add(c); err != nil {
			errorout("Failed to add watch directory for %s (%s): %v\n",
				val.Base_Directory, val.File_Filter, err)
			m.wtchr.Close()
			return err
		}
	}
	m.wtchr.SetLogger(m.igst)

	//make a new context that we can cancel to get the relay routine to quit
	ctx2, cf := context.WithCancel(ctx)
	defer cf()

	// go do the catchup for startup
	var quit bool
	debugout("Performing catchup scan\n")
	if quit, err = m.wtchr.Catchup(relayContextChannel(ctx2)); err != nil {
		debugout("Failed to perform catchup: %v\n", err)
		return err
	}
	cf() //to get the routine to shutdown
	debugout("Starting watchers\n")
	if !quit {
		if err = m.wtchr.Start(); err == nil {
			debugout("File watcher started\n")
		} else {
			errorout("Failed to start file watcher: %v\n", err)
		}
	}
	return err
}

func debugPrint(f string, args ...interface{}) {
	infoout(f, args...)
}

func relayContextChannel(ctx context.Context) (qc chan os.Signal) {
	qc = make(chan os.Signal)
	go func(c chan os.Signal, ctx context.Context) {
		<-ctx.Done()
		close(c)
	}(qc, ctx)
	return
}
