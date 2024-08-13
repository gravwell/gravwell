/*************************************************************************
 *
 * Gravwell - "Consume all the things!"
 *
 * ________________________________________________________________________
 *
 * Copyright 2019 - All Rights Reserved
 * Gravwell Inc <legal@gravwell.io>
 * ________________________________________________________________________
 *
 * NOTICE:  This code is part of the Gravwell project and may not be shared,
 * published, sold, or otherwise distributed in any from without the express
 * written consent of its owners.
 *
 **************************************************************************/

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/jsonparser"

	gravwelldebug "github.com/gravwell/gravwell/v3/debug"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/shodan_ingest.conf`
	fullFirehose     = `https://stream.shodan.io/shodan/banners?key=%s`
	privateFirehose  = `https://stream.shodan.io/shodan/alert?key=%s`

	appName   = `shodan`
	userAgent = `Gravwell/Shodan_Ingester`
)

var (
	confLoc        = flag.String("config", defaultConfigLoc, "Location of configuration file")
	confdLoc       = flag.String("config-overlays", ``, "Location for configuration overlay files")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")

	lg      *log.Logger
	debugOn bool
)

type shodanStream struct {
	apiKey           string
	tagName          string
	moduleTagPrefix  string
	extractedModules map[string]bool
	extractAll       bool
	tag              entry.EntryTag
	batching         bool
	eChan            chan *entry.Entry
	die              chan bool
	firehose         bool
}

func init() {
	go gravwelldebug.HandleDebugSignals("shodan")
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	validate.ValidateIngesterConfig(GetConfig, *confLoc, *confdLoc)
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	lg.SetAppname(appName)
	if *stderrOverride != `` {
		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			log.PrintOSInfo(fout)
			//file created, dup it
			if err := syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 3); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}
	debugOn = *verbose
}

func main() {
	debug.SetTraceback("all")
	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.FatalCode(0, "Failed to get configuration: ", log.KVErr(err))
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "Failed to get tags from configuration: ", log.KVErr(err))
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "Failed to get backend targets from configuration: ", log.KVErr(err))
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID")
	}
	//fire up the ingesters
	ingestConfig := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.Global.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		LogLevel:           cfg.LogLevel(),
		IngesterName:       appName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Global.Label,
		CacheDepth:         cfg.Global.Cache_Depth,
		CachePath:          cfg.Global.Ingest_Cache_Path,
		CacheSize:          cfg.Global.Max_Ingest_Cache,
		CacheMode:          cfg.Global.Cache_Mode,
		Logger:             lg,
		LogSourceOverride:  net.ParseIP(cfg.Global.Log_Source_Override),
	}

	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed build our ingest system: %v\n", err)
		return
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if cfg.Global.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed start our ingest system: %v\n", err)
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set configuration for ingester state messages\n")
		return
	}

	var streams []shodanStream
	for name, acct := range cfg.ShodanAccount {
		if acct == nil {
			igst.Error("Invalid account, nil struct.", log.KV(`account`, name))
			lg.FatalCode(0, "Invalid account, nil struct.")
		}
		if acct.API_Key == "" {
			igst.Error("invalid API key for account", log.KV(`account`, name))
			lg.FatalCode(0, "invalid API key for account", log.KV(`account`, name))
		}
		tagid, err := igst.GetTag(acct.Tag_Name)
		if err != nil {
			igst.Error("invalid API key for account", log.KV(`account`, name))
			lg.FatalCode(0, "Failed to resolve tag", log.KV("tag", acct.Tag_Name), log.KV(`name`, name), log.KVErr(err))
		}
		newacct := shodanStream{
			apiKey:     acct.API_Key,
			tagName:    acct.Tag_Name,
			extractAll: acct.Extract_All_Modules,
			tag:        tagid,
			batching:   cfg.Global.Batching,
			eChan:      make(chan *entry.Entry, 2048),
			die:        make(chan bool, 1),
			firehose:   acct.Full_Firehose,
		}
		if acct.Module_Tags_Prefix != `` {
			newacct.moduleTagPrefix = acct.Module_Tags_Prefix
		}
		newacct.extractedModules = make(map[string]bool)
		for _, mod := range acct.Extracted_Modules {
			newacct.extractedModules[mod] = true
		}
		streams = append(streams, newacct)
	}

	//register quit signals so we can die gracefully
	quitSig := make(chan os.Signal, 1)
	signal.Notify(quitSig, os.Interrupt)

	var wg sync.WaitGroup

	for _, stream := range streams {
		wg.Add(1)
		go stream.streamReader(&wg, igst)
		go stream.shodanIngester(igst)
	}
	<-quitSig

	for _, stream := range streams {
		stream.die <- true
	}
	wg.Wait()
}

// streamReader reads from the Shodan stream and places individual records on
// a channel for ingestion.
func (shodan *shodanStream) streamReader(wg *sync.WaitGroup, igst *ingest.IngestMuxer) {
	tagMap := make(map[string]entry.EntryTag)

	cli := &http.Client{}
	defer wg.Done()
	defer close(shodan.eChan)
outerLoop:
	for {
		debugout("Connecting...")
		var url string
		if shodan.firehose {
			url = fmt.Sprintf(fullFirehose, shodan.apiKey)
		} else {
			url = fmt.Sprintf(privateFirehose, shodan.apiKey)
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			lg.Error("can't create HTTP request", log.KVErr(err))
			igst.Error("can't create HTTP request", log.KVErr(err))
			time.Sleep(1 * time.Second)
			continue
		}
		req.Header.Set("User-Agent", userAgent)

		res, err := cli.Do(req)
		if err != nil {
			lg.Error("can't execute HTTP request", log.KVErr(err))
			igst.Error("can't execute HTTP request", log.KVErr(err))
			time.Sleep(1 * time.Second)
			continue
		}
		defer res.Body.Close()
		debugout("Connected.\n")
		buf := bufio.NewReader(res.Body)
		for {
			select {
			case <-shodan.die:
				debugout("Got die message, bailing out.\n")
				break outerLoop
			default:
				bts, err := buf.ReadBytes('\n')
				if err != nil {
					lg.Error("couldn't read from HTTP", log.KVErr(err))
					igst.Error("couldn't read from HTTP", log.KVErr(err))
					time.Sleep(1 * time.Second)
					continue outerLoop
				}
				if bts = bytes.Trim(bts, "\n\r\t "); len(bts) == 0 {
					//skip empty byte entries
					continue
				}
				if shodan.moduleTagPrefix == `` {
					// Ingesting everything under a single tag
					shodan.eChan <- &entry.Entry{
						TS:   entry.Now(),
						SRC:  nil,
						Tag:  shodan.tag,
						Data: bts,
					}
				} else {
					// Break out based on _shodan.module
					bt, _, _, err := jsonparser.Get(bts, "_shodan", "module")
					var tag entry.EntryTag
					if err != nil || bt == nil {
						lg.Info("Couldn't parse module from entry, ingesting under default tag")
						tag = shodan.tag
					} else {
						modName := string(bt)
						if _, ok := shodan.extractedModules[modName]; ok || shodan.extractAll {
							// If the module was on the list of modules we wish to
							// extract, build a tag for it
							tagString := shodan.moduleTagPrefix + modName
							// Attempt to look this up in the existing tags
							if tg, ok := tagMap[tagString]; ok {
								tag = tg
							} else {
								tg, err = igst.NegotiateTag(tagString)
								if err != nil {
									lg.Info("Could not negotiate a new tag, ingesting under default tag", log.KV("tag", tagString))
									tg = shodan.tag
								} else {
									tagMap[tagString] = tg
								}
								tag = tg
							}
						} else {
							tag = shodan.tag
						}
					}
					shodan.eChan <- &entry.Entry{
						TS:   entry.Now(),
						SRC:  nil,
						Tag:  tag,
						Data: bts,
					}
				}
			}
		}
	}
	return
}

// shodanIngester pulls individual JSON records from a Shodan reader and ingests them.
func (shodan *shodanStream) shodanIngester(igst *ingest.IngestMuxer) {
	if shodan.batching {
		var lastTS entry.Timestamp
		blk := []*entry.Entry{}
		for e := range shodan.eChan {
			if e.TS.Sec == lastTS.Sec {
				blk = append(blk, e)
			} else if e.TS.Sec > lastTS.Sec {
				// window changed, write out the block
				if err := igst.WriteBatch(blk); err != nil {
					lg.Error("failed to write entry block", log.KVErr(err))
				}
				// start the new block
				blk = []*entry.Entry{e}
				lastTS = e.TS
			} else {
				// some weird-ass timestamp, just ingest it by itself
				lg.Warn("Unexpectedly got timestamp predating previous entry timestamp", log.KV("last-ts", lastTS), log.KV("ts", e.TS))
				if err := igst.WriteEntry(e); err != nil {
					lg.Error("failed to write entry", log.KVErr(err))
				}
			}
		}
	} else {
		for e := range shodan.eChan {
			if err := igst.WriteEntry(e); err != nil {
				lg.Error("failed to write entry", log.KVErr(err))
			}
		}
	}
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}
