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

	"github.com/gravwell/gravwell/v3/ingesters/utils"
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

	var pms []*processManager
	for i := range pcs {
		pm, err := NewProcessManager(pcs[i])
		if err != nil {
			log.Fatal(err)
		}
		pms = append(pms, pm)
	}
	lg.Info("Starting %d processes", len(pms))
	for _, p := range pms {
		if err := p.Start(); err != nil {
			log.Fatal(err)
		}
	}

	//register for signals so we can die gracefully
	utils.WaitForQuit()

	lg.Info("Received shutdown signal, stopping %d children", len(pms))
	for _, p := range pms {
		if err := p.Close(); err != nil {
			log.Fatal(err)
		}
	}

}
