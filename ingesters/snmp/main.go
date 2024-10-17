/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gosnmp/gosnmp"
	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/snmp.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/snmp.conf.d`
	ingesterName      = `SNMP Ingester`
	appName           = `snmp`

	testTimeout time.Duration = 3 * time.Second
)

// TrapRecord gets built up from the received trap,
// then encoded to JSON for ingest.
type TrapRecord struct {
	Sender          string
	Community       string `json:",omitempty"`
	ContextEngineID string `json:",omitempty"`
	ContextName     string `json:",omitempty"`
	TrapOID         string `json:",omitempty"`
	Variables       []SnmpVariable
}

// SnmpVariable represents one OID->value mapping
// from the trap. A trap may contain multiple
// variables.
type SnmpVariable struct {
	OID        string
	Value      interface{}
	Type       gosnmp.Asn1BER
	TypeString string
}

func main() {
	go debug.HandleDebugSignals(ingesterName)
	var wg sync.WaitGroup
	var cfg *cfgType
	ibc := base.IngesterBaseConfig{
		IngesterName:                 ingesterName,
		AppName:                      appName,
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

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

	exitCtx, exitFn := context.WithCancel(context.Background())

	var traps []*gosnmp.TrapListener
	for name, lcfg := range cfg.Listener {
		var tag entry.EntryTag
		var proc *processors.ProcessorSet
		if tag, err = igst.GetTag(lcfg.Tag_Name); err != nil {
			ib.Logger.FatalCode(0, "failed to get established tag",
				log.KV("tag", lcfg.Tag_Name),
				log.KV("listener", name), log.KVErr(err))
		} else if proc, err = cfg.Preprocessor.ProcessorSet(igst, lcfg.Preprocessor); err != nil {
			ib.Logger.FatalCode(0, "preprocessor failure",
				log.KV("listener", name), log.KVErr(err))
		}

		l := gosnmp.NewTrapListener()
		l.Params = &gosnmp.GoSNMP{
			Transport:          "udp",
			Version:            lcfg.getSnmpVersion(),
			Timeout:            time.Duration(2) * time.Second,
			Retries:            3,
			ExponentialTimeout: true,
			MaxOids:            gosnmp.MaxOids,
			Community:          lcfg.Community,
			//Logger:             gosnmp.NewLogger(glog.New(os.Stdout, "", 0)),
		}
		if l.Params.Version == gosnmp.Version3 {
			l.Params.SecurityParameters = &gosnmp.UsmSecurityParameters{
				UserName:                 lcfg.Username,
				AuthenticationProtocol:   lcfg.getAuthProto(),
				AuthenticationPassphrase: lcfg.Auth_Passphrase,
				PrivacyProtocol:          lcfg.getPrivacyProto(),
				PrivacyPassphrase:        lcfg.Privacy_Passphrase,
				//Logger:                   gosnmp.NewLogger(glog.New(os.Stdout, "", 0)),
			}
			l.Params.MsgFlags = lcfg.getMsgFlags()
			l.Params.SecurityModel = gosnmp.UserSecurityModel
		}
		traps = append(traps, l)

		// Set up the callback
		cb := func(s *gosnmp.SnmpPacket, u *net.UDPAddr) {
			if s == nil || u == nil {
				return
			}
			if l.Params.Version == gosnmp.Version3 && 0x3&s.MsgFlags != 0x3&l.Params.MsgFlags {
				ib.Logger.Warn("dropping trap due to invalid msgflags",
					log.KV("received-flags", 0x3&s.MsgFlags),
					log.KV("expected-flags", 0x3&l.Params.MsgFlags),
					log.KV("client", u.IP.String()))
				return
			} else if l.Params.Version == gosnmp.Version2c && l.Params.Community != "" && s.Community != l.Params.Community {
				ib.Logger.Warn("dropping trap due to invalid community",
					log.KV("received-community", s.Community),
					log.KV("received-community", l.Params.Community),
					log.KV("client", u.IP.String()))
				return
			}
			ent := entry.Entry{
				TS:  entry.Now(),
				SRC: u.IP,
				Tag: tag,
			}
			r := TrapRecord{
				Sender:    u.IP.String(),
				Community: s.Community,
			}
			for i := range s.Variables {
				if s.Variables[i].Name == ".1.3.6.1.6.3.1.1.4.1.0" {
					r.TrapOID, _ = s.Variables[i].Value.(string)
				}
				v := SnmpVariable{
					OID:        s.Variables[i].Name,
					Value:      s.Variables[i].Value,
					Type:       s.Variables[i].Type,
					TypeString: s.Variables[i].Type.String(),
				}
				r.Variables = append(r.Variables, v)
			}
			var err error
			if ent.Data, err = json.Marshal(r); err != nil {
				// Skip it, I guess
				return
			}
			proc.ProcessContext(&ent, exitCtx)
		}
		l.OnNewTrap = cb

		wg.Add(1)
		go func(bind string) {
			defer wg.Done()
			l.Listen(bind)
		}(lcfg.Bind_String)
	}

	ib.Debug("Running\n")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	exitFn()

	// Close all the traps so goroutines exit
	for i := range traps {
		traps[i].Close()
	}

	// wait for graceful shutdown
	wg.Wait()

	if err := igst.Sync(time.Second); err != nil {
		ib.Logger.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		ib.Logger.Error("failed to close", log.KVErr(err))
	}
}
