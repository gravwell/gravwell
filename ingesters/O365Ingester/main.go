/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"sync"
	"syscall"
	"time"

	"github.com/floren/o365"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/o365_ingest.conf`
)

var (
	configLoc      = flag.String("config-file", defaultConfigLoc, "Location of configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	lg             *log.Logger
	tracker        *stateTracker

	ErrInvalidStateFile = errors.New("State file exists and is not a regular file")
	ErrFailedSeek       = errors.New("Failed to seek to the start of the states file")
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr: %v\n", err)
		} else {
			lg.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}
}

type event struct {
	Id string
}

func main() {
	cfg, err := GetConfig(*configLoc)
	if err != nil {
		lg.Fatal("Failed to get configuration: %v", err)
	}
	if len(cfg.Global.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Global.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Global.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Global.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Global.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.Fatal("Failed to get tags from configuration: %v", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("Failed to get backend targets from configuration: %s", err)
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	//fire up the ingesters
	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	ingestConfig := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.Global.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		LogLevel:           cfg.LogLevel(),
		Logger:             lg,
		IngesterName:       "Office 365",
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		CacheDepth:         cfg.Global.Cache_Depth,
		CachePath:          cfg.Global.Ingest_Cache_Path,
		CacheSize:          cfg.Global.Max_Ingest_Cache,
		CacheMode:          cfg.Global.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Global.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		lg.Fatal("Failed build our ingest system: %v", err)
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.FatalCode(0, "Failed start our ingest system: %v", err)
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
	}
	debugout("Successfully connected to ingesters\n")

	tracker, err = NewTracker(cfg.Global.State_Store_Location, 48*time.Hour, igst)
	if err != nil {
		lg.Fatal("Failed to initialize state file: %v", err)
	}
	tracker.Start()

	// get the src we'll attach to entries
	var src net.IP
	if cfg.Global.Source_Override != `` {
		// global override
		src = net.ParseIP(cfg.Global.Source_Override)
		if src == nil {
			lg.Fatal("Global Source-Override is invalid")
		}
	}

	var wg sync.WaitGroup

	// Instantiate the client
	ocfg := o365.DefaultConfig
	ocfg.ClientID = cfg.Global.Client_ID
	ocfg.ClientSecret = cfg.Global.Client_Secret
	ocfg.DirectoryID = cfg.Global.Directory_ID
	ocfg.TenantDomain = cfg.Global.Tenant_Domain
	ocfg.ContentTypes = cfg.ContentTypes()
	o, err := o365.New(ocfg)
	if err != nil {
		lg.Fatal("Failed to get new client: %v", err)
	}

	// Make sure there are subscriptions for all our requested content types
	err = o.EnableSubscriptions()
	if err != nil {
		lg.Fatal("Failed to enable subscriptions: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// For each content type we're interested in, launch a
	// goroutine to read entries from Office 365 maintenance API
	running := true
	for k, v := range cfg.ContentType {
		go func(name string, ct contentType) {
			debugout("Started reader for content type %v\n", ct.Content_Type)
			wg.Add(1)
			defer wg.Done()

			// figure out which tag we're using
			tag, err := igst.GetTag(ct.Tag_Name)
			if err != nil {
				lg.Fatal("Can't resolve tag %v: %v", ct.Tag_Name, err)
			}

			procset, err := cfg.Preprocessor.ProcessorSet(igst, ct.Preprocessor)
			if err != nil {
				lg.Fatal("Preprocessor construction error: %v", err)
			}

			// set up time extraction rules
			tcfg := timegrinder.Config{
				EnableLeftMostSeed: true,
			}
			tg, err := timegrinder.NewTimeGrinder(tcfg)
			if err != nil {
				ct.Ignore_Timestamps = true
			}
			if ct.Assume_Local_Timezone {
				tg.SetLocalTime()
			}
			if ct.Timezone_Override != `` {
				err = tg.SetTimezone(ct.Timezone_Override)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to set timezone to %v: %v\n", ct.Timezone_Override, err)
					return
				}
			}

			// we'll do a sliding window, they warn it can take a long time for some logs to show up
			for running {
				end := time.Now()
				start := end.Add(-24 * time.Hour)

				content, err := o.ListAvailableContent(ct.Content_Type, start, end)
				if err != nil {
					lg.Error("Failed to list content type %v: %v", ct.Content_Type, err)
					time.Sleep(10 * time.Second)
					continue
				}

				var uri, contentId string
				var ok bool
				var ent *entry.Entry
				var events []json.RawMessage
				var eventUnpacked event
				for _, item := range content {
					contentId, ok = item["contentId"]
					if !ok {
						continue
					}

					// CHECK IF ALREADY SEEN
					if tracker.IdExists(contentId) {
						continue
					}
					debugout("extracting %v\n", contentId)

					uri, ok = item["contentUri"]
					if !ok {
						continue
					}
					result, err := o.GetContent(uri)
					if err != nil {
						continue
					}

					// Dumb fact: each item may have multiple events
					err = json.Unmarshal(result, &events)
					if err != nil {
						continue
					}
					for _, evt := range events {
						err = json.Unmarshal(evt, &eventUnpacked)
						if err != nil {
							continue
						}
						if tracker.IdExists(eventUnpacked.Id) {
							continue
						}
						ent = &entry.Entry{
							Data: []byte(evt),
							Tag:  tag,
							SRC:  src,
						}
						if ct.Ignore_Timestamps {
							ent.TS = entry.Now()
						} else {
							ts, ok, err := tg.Extract(ent.Data)
							if !ok || err != nil {
								// something went wrong, switch to using the current time
								ct.Ignore_Timestamps = true
								ent.TS = entry.Now()
							} else {
								ent.TS = entry.FromStandard(ts)
							}
						}

						// now write the entry
						if err := procset.ProcessContext(ent, ctx); err != nil {
							lg.Warn("Failed to handle entry: %v", err)
						}
						// Add the Id to the temporary map
						tracker.RecordId(eventUnpacked.Id, time.Now())
					}
					// Add the contentId to the temporary map
					tracker.RecordId(contentId, time.Now())

				}
				time.Sleep(5 * time.Second)
			}
			if err := procset.Close(); err != nil {
				lg.Error("Failed to close processor set: %v", err)
			}

		}(k, *v)
	}

	//register quit signals so we can die gracefully
	utils.WaitForQuit()

	go func() {
		time.Sleep(time.Second)
		cancel()
	}()

	running = false
	wg.Wait()

	// Write the final state info
	tracker.Close()
}

func debugout(format string, args ...interface{}) {
	if !*verbose {
		return
	}
	fmt.Printf(format, args...)
}
