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
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"

	"github.com/gravwell/ipmigo"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/ipmi.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/ipmi.conf.d`
	ingesterName      = `IPMI`
)

var (
	lg *log.Logger

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

func main() {
	var cfg *cfgType
	ibc := base.IngesterBaseConfig{
		IngesterName:                 ingesterName,
		AppName:                      ingesterName,
		DefaultConfigLocation:        defaultConfigLoc,
		DefaultConfigOverlayLocation: defaultConfigDLoc,
		GetConfigFunc:                GetConfig,
	}
	ib, err := base.Init(ibc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get configuration %v\n", err)
		return
	} else if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v %v\n", err, cfg == nil)
		return
	}
	lg = ib.Logger

	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		ib.Logger.FatalCode(0, "could not read ingester UUID")
	}
	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

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
	ib.AnnounceShutdown()

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
			Version:        ipmigo.V2_0,
			Address:        h.target,
			Username:       h.username,
			Password:       h.password,
			PrivilegeLevel: ipmigo.PrivilegeUser,
			CipherSuiteID:  3,
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
