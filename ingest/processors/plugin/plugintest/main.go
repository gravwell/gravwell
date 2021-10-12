/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	//"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingest/processors/plugin"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

var (
	configPath = flag.String("config-path", "", "Path to the plugin configuration")
	dataPath   = flag.String("data-path", "", "Optional path to data export file")
	fmtF       = flag.String("import-format", "", "Set the import file format manually")
	debug      = flag.Bool("debug", true, "Print each entry as its processed")
)

func main() {
	flag.Parse()
	var rdr utils.ReimportReader
	var plugin_data []byte
	var vc *config.VariableConfig
	var err error
	if *configPath == `` {
		fmt.Println("missing config-path")
		os.Exit(1)
	}
	if config_data, err := ioutil.ReadFile(*configPath); err != nil {
		fmt.Printf("Failed to load plugin config file %q: %v\n", *configPath, err)
		os.Exit(1)
	} else if vc, err = loadPluginConfig(config_data); err != nil {
		fmt.Printf("Failed to load plugin config: %v\n", err)
		os.Exit(1)
	} else if val, err := vc.GetString("plugin_path"); err != nil {
		fmt.Printf("config file does not specify Plugin-Path, please fix it\n")
		os.Exit(1)
	} else if plugin_data, err = ioutil.ReadFile(val); err != nil {
		fmt.Printf("Failed to load plugin %q: %v\n", val, err)
		os.Exit(1)
	}

	if *dataPath != `` {
		th := &testTagHandler{}
		var format string
		var fin *os.File
		if format, err = utils.GetImportFormat(*fmtF, *dataPath); err != nil {
			fmt.Printf("%v, please set -import-format\n", err)
			os.Exit(1)
		} else if fin, err = os.Open(*dataPath); err != nil {
			fmt.Printf("Failed to open data file %s: %v\n", *dataPath, err)
			os.Exit(1)
		} else if rdr, err = utils.GetImportReader(format, fin, th); err != nil {
			fmt.Printf("Failed to determine data format: %v\n", err)
			os.Exit(1)
		}
		defer fin.Close()
	}
	//load the plugin machine
	pp, err := plugin.NewPluginProgram(plugin_data)
	if err != nil {
		fmt.Printf("Failed to load plugin:\n%v\n", err)
		os.Exit(1)
	} else if err = pp.Run(time.Second); err != nil {
		fmt.Printf("Failed to execute plugin: %v\n", err)
		os.Exit(1)
	} else if err = pp.Config(vc, plugin.NewTestTagger()); err != nil {
		fmt.Printf("Failed to execute plugin config: %v\n", err)
		os.Exit(1)
	}

	if rdr == nil {
		fmt.Println("no data file provided")
	} else {
		//start iterating through the data file providing entries to the plugin
		// we randomly provide blocks and single entries
		for ents, err := popEnts(rdr); ents != nil && err == nil; ents, err = popEnts(rdr) {
			if set, err := pp.Process(ents); err != nil {
				fmt.Printf("plugin returned error: %v\n", err)
				break
			} else if *debug {
				for _, ent := range set {
					fmt.Printf("%v\t%s\n", ent.TS, string(ent.Data))
				}
			}
		}
		if err != nil {
			fmt.Println("data file contains invalid data, reader go", err)
		}
	}

	if err = pp.Close(); err != nil {
		fmt.Printf("Failed to close plugin: %v\n", err)
		os.Exit(1)
	}
}

func popEnts(rdr utils.ReimportReader) (ents []*entry.Entry, err error) {
	cnt := rand.Intn(16)
	for i := 0; i < cnt; i++ {
		var ent *entry.Entry
		if ent, err = rdr.ReadEntry(); err == nil {
			ents = append(ents, ent)
		} else if err == io.EOF {
			err = nil
			return
		} else {
			return
		}
	}
	return
}

func loadPluginConfig(cnt []byte) (r *config.VariableConfig, err error) {
	var cfg testConfig
	//load it up and make sure there is exactly one preprocessor config defined
	if err = config.LoadConfigBytes(&cfg, cnt); err != nil {
		return
	} else if cfg.count() != 1 {
		err = fmt.Errorf("plugin config does not contain exactly one plugin configuration: count %d", len(cfg.Preprocessor))
		return
	}
	//grab the preprocessor and check that the defined preprocessor is of type plugin
	ptype, vc, ok := cfg.pop()
	if !ok || vc == nil {
		err = fmt.Errorf("failed to pull plugin configuration")
		return
	} else if ptype != "plugin" {
		err = fmt.Errorf("Configuration stanza is of the wrong type: %q != plugin", ptype)
		return
	}
	r = vc
	return
}

type testConfig struct {
	Preprocessor map[string]*config.VariableConfig
}

func (tt testConfig) count() int {
	return len(tt.Preprocessor)
}

func (tt testConfig) pop() (ptype string, vc *config.VariableConfig, ok bool) {
	for k, v := range tt.Preprocessor {
		if v != nil {
			vc = v
			ok = true
			var err error
			if ptype, err = vc.GetString("type"); err != nil {
				ptype = strings.TrimSpace(strings.ToLower(ptype))
				delete(tt.Preprocessor, k)
				break
			}
		}
	}
	return
}

type testTagHandler struct {
	overrideTag bool
	override    entry.EntryTag
	mp          map[string]entry.EntryTag
}

func (tth *testTagHandler) OverrideTags(v entry.EntryTag) {
	tth.overrideTag = true
	tth.override = v
}

func (tth *testTagHandler) GetTag(v string) (r entry.EntryTag, err error) {
	if err = ingest.CheckTag(v); err != nil {
		return
	}
	var ok bool
	if r, ok = tth.mp[v]; !ok {
		r = entry.EntryTag(len(tth.mp))
		if tth.mp == nil {
			tth.mp = map[string]entry.EntryTag{}
		}
		tth.mp[v] = r
	}
	return
}
