/*************************************************************************
* Copyright 2017 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/

package main

import (
	"flag"
	"log"

	il "github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	defConfigLoc string = `/opt/gravwell/etc/manager.cfg`
)

var (
	cfgFlag = flag.String("config-override", "", "Override config file path")
	cfgFile string
)

func init() {
	cfgFile = defConfigLoc
	flag.Parse()
	if *cfgFlag != `` {
		cfgFile = *cfgFlag
	}
}

func main() {
	c, err := GetConfig(cfgFile)
	if err != nil {
		log.Fatal("Failed to open config file", cfgFile, err)
	}

	lg, err := c.GetLogger()
	if err != nil {
		log.Fatal("Failed to get logger", err)
	}

	pcs := c.ProcessConfigs(lg)
	if len(pcs) == 0 {
		log.Fatal("No processes specified")
	}

	//check if there is an init command, if so, run it
	if cmd, err := c.GetInitCommand(); err != nil {
		log.Fatalf("Invalid Init-Command %v\n", err)
	} else if cmd != nil {
		if err = cmd.Run(); err != nil {
			log.Fatalf("Init-Command returned error %v\n", err)
		}
	}

	var pms []*processManager
	for i := range pcs {
		pm, err := NewProcessManager(pcs[i])
		if err != nil {
			log.Fatal(err)
		}
		pms = append(pms, pm)
	}
	lg.Info("starting processes", il.KV("count", len(pms)))
	for _, p := range pms {
		if err := p.Start(); err != nil {
			log.Fatal(err)
		}
	}

	//register for signals so we can die gracefully
	utils.WaitForQuit()

	lg.Info("received shutdown signal, stopping children", il.KV("count", len(pms)))
	for _, p := range pms {
		if err := p.Close(); err != nil {
			log.Fatal(err)
		}
	}

}
