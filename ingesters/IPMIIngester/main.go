/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// getSDR and getSEL both derived from https://github.com/k-sone/ipmigo/tree/master/examples

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/utils/caps"
	"github.com/gravwell/gravwell/v3/ingesters/version"

	"github.com/k-sone/ipmigo"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/ipmi.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/ipmi.conf.d`
	ingesterName      = `IPMI`
)

var (
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	confdLoc       = flag.String("config-overlays", defaultConfigDLoc, "Location for configuration overlay files")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	ver            = flag.Bool("version", false, "Print the version information and exit")

	lg   *log.Logger
	igst *ingest.IngestMuxer

	ipmiConns map[string]*handlerConfig
)

const PERIOD = 10 * time.Second // used for cooldown between device connection errors

type handlerConfig struct {
	target           string
	username         string
	password         string
	tag              entry.EntryTag
	src              net.IP
	wg               *sync.WaitGroup
	proc             *processors.ProcessorSet
	ctx              context.Context
	client           *ipmigo.Client
	SELIDs           map[uint16]bool
	ignoreTimestamps bool
	rate             int
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)

	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	lg.SetAppname(ingesterName)
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("failed to dup stderr", log.KVErr(err))
		} else {
			lg.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		fp := filepath.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			log.PrintOSInfo(fout)
			//file created, dup it
			if err := syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 0); err != nil {
				fout.Close()
				lg.FatalCode(0, "failed to dup2 stderr", log.KVErr(err))
			}
		}
	}
}

func main() {
	debug.SetTraceback("all")

	// config setup
	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.FatalCode(0, "failed to get configuration", log.KVErr(err))
		return
	}

	if len(cfg.Global.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Global.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "failed to open log file", log.KV("path", cfg.Global.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Global.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
				lg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Global.Log_Level), log.KVErr(err))
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "failed to get tags from configuration", log.KVErr(err))
		return
	}
	conns, err := cfg.Global.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
		return
	}

	lmt, err := cfg.Global.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.Global.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Global.Secret(),
		VerifyCert:         !cfg.Global.InsecureSkipTLSVerification(),
		IngesterName:       ingesterName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Global.Label,
		RateLimitBps:       lmt,
		Logger:             lg,
		CacheDepth:         cfg.Global.Cache_Depth,
		CachePath:          cfg.Global.Ingest_Cache_Path,
		CacheSize:          cfg.Global.Max_Ingest_Cache,
		CacheMode:          cfg.Global.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Global.Log_Source_Override),
	}
	igst, err = ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
		return
	}
	defer igst.Close()
	if cfg.Global.SelfIngest() {
		lg.AddRelay(igst)
	}

	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
		return
	}

	if err := igst.WaitForHot(cfg.Global.Timeout()); err != nil {
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Global.Timeout()), log.KVErr(err))
		return
	}

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "failed to set configuration for ingester state messages", log.KVErr(err))
	}

	//check capabilities so we can scream and throw a potential warning upstream
	if !caps.Has(caps.NET_BIND_SERVICE) {
		lg.Warn("missing capability", log.KV("capability", "NET_BIND_SERVICE"), log.KV("warning", "may not be able to bind to service ports"))
	}

	// fire up IPMI handlers

	var wg sync.WaitGroup
	ipmiConns = make(map[string]*handlerConfig)
	ctx, cancel := context.WithCancel(context.Background())

	for k, v := range cfg.IPMI {
		var src net.IP

		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Source-Override is invalid", log.KV("sourceoverride", v.Source_Override), log.KV("listener", k))
			}
		} else if cfg.Global.Source_Override != `` {
			// global override
			src = net.ParseIP(cfg.Global.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Global Source-Override is invalid", log.KV("sourceoverride", cfg.Global.Source_Override))
			}
		}

		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("listener", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		for _, x := range v.Target {
			hcfg := &handlerConfig{
				target:           x,
				username:         v.Username,
				password:         v.Password,
				tag:              tag,
				src:              src,
				wg:               &wg,
				ctx:              ctx,
				SELIDs:           make(map[uint16]bool),
				ignoreTimestamps: v.Ignore_Timestamps,
				rate:             v.Rate,
			}

			if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
				lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
			}

			ipmiConns[k+x] = hcfg
		}
	}

	for _, v := range ipmiConns {
		go v.run()
	}

	// listen for signals so we can close gracefully

	utils.WaitForQuit()

	cancel()

	lg.Info("IPMI ingester exiting", log.KV("ingesteruuid", id))
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}

func (h *handlerConfig) run() {
	var err error

	for {
		h.client, err = ipmigo.NewClient(ipmigo.Arguments{
			Version:       ipmigo.V2_0,
			Address:       h.target,
			Username:      h.username,
			Password:      h.password,
			CipherSuiteID: 3,
		})
		if err != nil {
			lg.Error("failed to connect", log.KV("address", h.target), log.KVErr(err))
			time.Sleep(PERIOD)
			continue
		}

		if err := h.client.Open(); err != nil {
			lg.Error("failed to connect", log.KV("address", h.target), log.KVErr(err))
			time.Sleep(PERIOD)
			continue
		}

		// successful connection, spin on getting records

		for {
			// grab all SDR records and throw them as a single entry
			sdr, err := h.getSDR()
			if err != nil {
				lg.Error("get SDR error", log.KVErr(err))
				h.client.Close()
				break
			} else {
				ent := &entry.Entry{
					SRC:  h.src,
					TS:   entry.Now(),
					Tag:  h.tag,
					Data: sdr,
				}

				if err = h.proc.ProcessContext(ent, h.ctx); err != nil {
					lg.Error("failed to send entry", log.KVErr(err))
				}
			}

			// grab all SEL records that we haven't already seen
			sel, err := h.getSEL()
			if err != nil {
				lg.Error("get SEL error", log.KVErr(err))
				h.client.Close()
				break
			} else {
				// throw them as individual entries
				for _, v := range sel {
					b, err := json.Marshal(v)
					if err != nil {
						lg.Error("encoding SEL record error", log.KVErr(err))
						continue
					}

					var ts entry.Timestamp
					if h.ignoreTimestamps {
						ts = entry.Now()
					} else {
						switch s := v.Data.(type) {
						case *ipmigo.SELEventRecord:
							ts = entry.UnixTime(int64((&s.Timestamp).Value), 0)
						case *ipmigo.SELTimestampedOEMRecord:
							ts = entry.UnixTime(int64((&s.Timestamp).Value), 0)
						default:
							// other types just don't have a timestamp
							ts = entry.Now()
						}
					}

					ent := &entry.Entry{
						SRC:  h.src,
						TS:   ts,
						Tag:  h.tag,
						Data: b,
					}

					if err = h.proc.ProcessContext(ent, h.ctx); err != nil {
						lg.Error("failed to send entry", log.KVErr(err))
					}
				}
			}

			time.Sleep(time.Duration(h.rate) * time.Second)
		}
	}
}

type tSDR struct {
	Type   string
	Target string
	Data   map[string]*tSDRData
}

type tSDRData struct {
	Type    string
	Reading string
	Units   string
	Status  string
}

func (h *handlerConfig) getSDR() ([]byte, error) {
	var data = &tSDR{
		Type:   "SDR",
		Target: h.target,
		Data:   make(map[string]*tSDRData),
	}

	records, err := ipmigo.SDRGetRecordsRepo(h.client, func(id uint16, t ipmigo.SDRType) bool {
		return t == ipmigo.SDRTypeFullSensor || t == ipmigo.SDRTypeCompactSensor
	})
	if err != nil {
		return nil, fmt.Errorf("Failed to read SDR values on target %v: %w", h.target, err)
	}
	for _, r := range records {
		// Get sensor reading
		var run, num uint8
		switch s := r.(type) {
		case *ipmigo.SDRFullSensor:
			run = s.OwnerLUN
			num = s.SensorNumber
		case *ipmigo.SDRCompactSensor:
			run = s.OwnerLUN
			num = s.SensorNumber
		}
		gsr := &ipmigo.GetSensorReadingCommand{
			RsLUN:        run,
			SensorNumber: num,
		}
		err, ok := h.client.Execute(gsr).(*ipmigo.CommandError)
		if err != nil && !ok {
			return nil, fmt.Errorf("Get SDR record on target %v failed: %w", h.target, err)
		}

		// Output sensor reading
		var convf func(uint8) float64
		var analog, threshold bool
		var sname, stype string
		units, reading, status := "discrete", "n/a", "n/a"

		switch s := r.(type) {
		case *ipmigo.SDRFullSensor:
			convf = func(r uint8) float64 { return s.ConvertSensorReading(r) }
			analog = s.IsAnalogReading()
			threshold = s.IsThresholdBaseSensor()
			sname = s.SensorID()
			stype = s.SensorType.String()
			if analog {
				units = s.UnitString()
			}
		case *ipmigo.SDRCompactSensor:
			analog = false
			threshold = false
			sname = s.SensorID()
			stype = s.SensorType.String()
		}

		if err != nil {
			status = err.CompletionCode.String()
		} else {
			if gsr.IsValid() {
				if analog {
					if threshold {
						status = string(gsr.ThresholdStatus())
					}
					reading = fmt.Sprintf("%.2f", convf(gsr.SensorReading))
				} else {
					reading = fmt.Sprintf("0x%02x", gsr.SensorReading)
				}
			}
		}

		data.Data[sname] = &tSDRData{
			Type:    stype,
			Reading: reading,
			Units:   units,
			Status:  status,
		}
	}

	return json.Marshal(data)
}

type tSEL struct {
	Target      string
	Type        string
	Data        ipmigo.SELRecord
	Description string
}

func (h *handlerConfig) getSEL() ([]*tSEL, error) {
	// Get total count
	_, total, err := ipmigo.SELGetEntries(h.client, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("Failed to get SEL entries on target %v: %w", h.target, err)
	}

	selrecords, total, err := ipmigo.SELGetEntries(h.client, 0, total)
	if err != nil {
		return nil, fmt.Errorf("Failed to get SEL entries on target %v: %w", h.target, err)
	}

	// only return records we haven't already seen by ID. If you restart
	// the ingester, well you're going to reingest ALL SEL events that
	// haven't already been cleared.
	var ret []*tSEL
	for _, v := range selrecords {
		if _, ok := h.SELIDs[v.ID()]; !ok {
			r := &tSEL{
				Target: h.target,
				Type:   "SEL",
				Data:   v,
			}
			switch s := v.(type) {
			case *ipmigo.SELEventRecord:
				r.Description = s.Description()
			}
			ret = append(ret, r)
			h.SELIDs[v.ID()] = true
		}
	}

	return ret, nil
}
