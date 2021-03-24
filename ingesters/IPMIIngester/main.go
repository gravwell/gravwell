/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"

	"github.com/k-sone/ipmigo"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/ipmi.conf`
	ingesterName     = `IPMI`
)

var (
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	ver            = flag.Bool("version", false, "Print the version information and exit")

	lg   *log.Logger
	igst *ingest.IngestMuxer

	ipmiConns map[string]*handlerConfig
)

const PERIOD = 10 * time.Second

type handlerConfig struct {
	target   string
	username string
	password string
	tag      entry.EntryTag
	src      net.IP
	wg       *sync.WaitGroup
	proc     *processors.ProcessorSet
	ctx      context.Context
	client   *ipmigo.Client
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	var fp string
	var err error
	if *stderrOverride != `` {
		fp = filepath.Join(`/dev/shm/`, *stderrOverride)
	}
	cb := func(w io.Writer) {
		version.PrintVersion(w)
		ingest.PrintVersion(w)
	}
	if lg, err = log.NewStderrLoggerEx(fp, cb); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get stderr logger: %v\n", err)
		os.Exit(-1)
	}
}

func main() {
	debug.SetTraceback("all")

	cfg, err := GetConfig(*confLoc)
	if err != nil {
		lg.FatalCode(0, "Failed to get configuration: %v\n", err)
		return
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
		lg.FatalCode(0, "Failed to get tags from configuration: %v\n", err)
		return
	}
	conns, err := cfg.Global.Targets()
	if err != nil {
		lg.FatalCode(0, "Failed to get backend targets from configuration: %v\n", err)
		return
	}

	lmt, err := cfg.Global.RateLimit()
	if err != nil {
		lg.FatalCode(0, "Failed to get rate limit from configuration: %v\n", err)
		return
	}

	//fire up the ingesters
	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.Global.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Global.Secret(),
		LogLevel:           cfg.Global.LogLevel(),
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
		lg.Fatal("Failed build our ingest system: %v\n", err)
		return
	}

	defer igst.Close()

	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start our ingest system: %v\n", err)
		return
	}

	if err := igst.WaitForHot(cfg.Global.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
		return
	}

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "Failed to set configuration for ingester state messages\n")
	}

	var wg sync.WaitGroup

	ipmiConns = make(map[string]*handlerConfig)

	ctx, cancel := context.WithCancel(context.Background())

	for k, v := range cfg.IPMI {
		var src net.IP

		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Listener %v invalid source override, \"%s\" is not an IP address", k, v.Source_Override)
			}
		} else if cfg.Global.Source_Override != `` {
			// global override
			src = net.ParseIP(cfg.Global.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Global Source-Override is invalid")
			}
		}

		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("Failed to resolve tag \"%s\" for %s: %v\n", v.Tag_Name, k, err)
		}

		hcfg := &handlerConfig{
			target:   v.Target,
			username: v.Username,
			password: v.Password,
			tag:      tag,
			src:      src,
			wg:       &wg,
			ctx:      ctx,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("Preprocessor failure: %v", err)
		}

		ipmiConns[k] = hcfg
	}

	for _, v := range ipmiConns {
		go v.run()
	}

	//listen for signals so we can close gracefully
	utils.WaitForQuit()

	cancel()

	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		lg.Error("Failed to close: %v\n", err)
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
			lg.Error("Failed to connect to %v: %w", h.target, err)
			time.Sleep(PERIOD)
			continue
		}

		if err := h.client.Open(); err != nil {
			lg.Error("Failed to connect to %v: %w", h.target, err)
			time.Sleep(PERIOD)
			continue
		}
		defer h.client.Close()

		for {
			// grab all SDR records
			sdr, err := h.getSDR()
			if err != nil {
				lg.Error("%v", err)
			} else {
				fmt.Println(string(sdr))
			}

			// grab all SEL events
			sel, err := h.getSEL()
			if err != nil {
				lg.Error("%v", err)
			} else {
				fmt.Println(string(sel))
			}

			time.Sleep(PERIOD)
		}
	}
}

type tSDR struct {
	Type string
	Data []*tSDRData
}

type tSDRData struct {
	Name    string
	Type    string
	Reading string
	Units   string
	Status  string
}

func (h *handlerConfig) getSDR() ([]byte, error) {
	var data []*tSDRData

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

		data = append(data, &tSDRData{
			Name:    sname,
			Type:    stype,
			Reading: reading,
			Units:   units,
			Status:  status,
		})
	}

	ret := &tSDR{
		Type: "SDR",
		Data: data,
	}

	return json.Marshal(ret)
}

type tSEL struct {
	Type string
	Data []ipmigo.SELRecord
}

func (h *handlerConfig) getSEL() ([]byte, error) {
	// Get total count
	_, total, err := ipmigo.SELGetEntries(h.client, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("Failed to get SEL entries on target %v: %w", h.target, err)
	}

	// Get latest 10 events
	count := 10
	offset := 0
	if total > count {
		offset = total - count
	}
	selrecords, total, err := ipmigo.SELGetEntries(h.client, offset, count)
	if err != nil {
		return nil, fmt.Errorf("Failed to get SEL entries on target %v: %w", h.target, err)
	}

	ret := &tSEL{
		Type: "SEL",
		Data: selrecords,
	}

	return json.Marshal(ret)
}
